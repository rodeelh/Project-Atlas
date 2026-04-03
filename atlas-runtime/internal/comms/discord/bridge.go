// Package discord implements the Discord Gateway WebSocket bridge for Atlas.
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

const (
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	// Intents: GUILDS(0) | GUILD_MESSAGES(9) | GUILD_MESSAGE_CONTENT(15) | DIRECT_MESSAGES(12)
	intents = (1 << 0) | (1 << 9) | (1 << 15) | (1 << 12)
	apiBase = "https://discord.com/api/v10"
	maxChunk = 1900
)

// Attachment is an inbound file alongside a Discord message.
// Data is raw base64 (no data-URL prefix).
type Attachment struct {
	Filename string
	MimeType string
	Data     string
}

// BridgeRequest is the unified request passed to the Atlas handler.
// Mirrors comms.BridgeRequest — add fields here when chat.MessageRequest grows.
type BridgeRequest struct {
	Text        string
	ConvID      string
	Platform    string
	Attachments []Attachment
}

// ChatHandler routes a BridgeRequest to the Atlas agent loop.
// Returns (assistantReply, conversationID, error).
type ChatHandler func(ctx context.Context, req BridgeRequest) (string, string, error)

// Bridge implements the Discord Gateway WebSocket bridge.
type Bridge struct {
	token   string
	db      *storage.DB
	cfgFn   func() config.RuntimeConfigSnapshot
	handler ChatHandler
	client  *http.Client

	mu        sync.Mutex
	connected bool
	lastErr   string
	botID     string
	botName   string

	stopCh chan struct{}
	doneCh chan struct{}
}

// New creates a new Discord bridge.
func New(token string, db *storage.DB, cfgFn func() config.RuntimeConfigSnapshot, handler ChatHandler) *Bridge {
	return &Bridge{
		token:   token,
		db:      db,
		cfgFn:   cfgFn,
		handler: handler,
		client:  &http.Client{Timeout: 15 * time.Second},
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start begins the gateway loop in a background goroutine.
func (b *Bridge) Start() {
	go b.run()
}

// Stop signals the loop to stop and waits for it to exit.
func (b *Bridge) Stop() {
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
	<-b.doneCh
}

// Connected returns the live connection state.
func (b *Bridge) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// BotName returns the connected bot username.
func (b *Bridge) BotName() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.botName
}

// LastError returns the most recent error string.
func (b *Bridge) LastError() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastErr
}

// ── Gateway types ─────────────────────────────────────────────────────────────

type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int            `json:"s"`
	T  *string         `json:"t"`
}

type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type identifyData struct {
	Token      string         `json:"token"`
	Properties map[string]any `json:"properties"`
	Intents    int            `json:"intents"`
}

type messageCreateEvent struct {
	ID        string        `json:"id"`
	ChannelID string        `json:"channel_id"`
	GuildID   string        `json:"guild_id"`
	Author    discordUser   `json:"author"`
	Content   string        `json:"content"`
	Mentions  []discordUser `json:"mentions"`
	Type      int           `json:"type"`
}

type discordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

type readyEvent struct {
	User discordUser `json:"user"`
}

// ── Main loop ─────────────────────────────────────────────────────────────────

func (b *Bridge) run() {
	defer close(b.doneCh)

	backoff := 2 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-b.stopCh:
			b.mu.Lock()
			b.connected = false
			b.mu.Unlock()
			logstore.Write("info", "Discord bridge stopped", map[string]string{"platform": "discord"})
			return
		default:
		}

		err := b.connect()
		if err != nil {
			b.mu.Lock()
			b.lastErr = err.Error()
			b.connected = false
			b.mu.Unlock()
			logstore.Write("error", "Discord bridge error: "+err.Error(), map[string]string{"platform": "discord"})
			select {
			case <-b.stopCh:
				return
			case <-time.After(backoff):
				backoff = minDur(backoff*2, maxBackoff)
			}
			continue
		}
		backoff = 2 * time.Second
	}
}

func (b *Bridge) connect() error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()

	var seq *int
	heartbeatStop := make(chan struct{})
	defer close(heartbeatStop)

	for {
		select {
		case <-b.stopCh:
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var p gatewayPayload
		if err := json.Unmarshal(msg, &p); err != nil {
			continue
		}
		if p.S != nil {
			seq = p.S
		}

		switch p.Op {
		case 10: // HELLO
			var hello helloData
			if err := json.Unmarshal(p.D, &hello); err != nil {
				return fmt.Errorf("parse HELLO: %w", err)
			}
			// Start heartbeat goroutine.
			interval := time.Duration(hello.HeartbeatInterval) * time.Millisecond
			go func() {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-heartbeatStop:
						return
					case <-b.stopCh:
						return
					case <-ticker.C:
						hb := map[string]any{"op": 1, "d": seq}
						conn.WriteJSON(hb) //nolint:errcheck
					}
				}
			}()
			// Send IDENTIFY.
			identify := gatewayPayload{
				Op: 2,
			}
			identData, _ := json.Marshal(identifyData{
				Token: "Bot " + b.token,
				Properties: map[string]any{
					"os":      "linux",
					"browser": "atlas",
					"device":  "atlas",
				},
				Intents: intents,
			})
			identify.D = identData
			if err := conn.WriteJSON(identify); err != nil {
				return fmt.Errorf("IDENTIFY: %w", err)
			}

		case 0: // Dispatch
			if p.T == nil {
				continue
			}
			switch *p.T {
			case "READY":
				var ready readyEvent
				if err := json.Unmarshal(p.D, &ready); err == nil {
					b.mu.Lock()
					b.botID = ready.User.ID
					b.botName = ready.User.Username
					b.connected = true
					b.lastErr = ""
					b.mu.Unlock()
					logstore.Write("info", "Discord bridge connected: "+ready.User.Username, map[string]string{"platform": "discord"})
				}
			case "MESSAGE_CREATE":
				var ev messageCreateEvent
				if err := json.Unmarshal(p.D, &ev); err == nil {
					go b.handleMessage(ev)
				}
			}

		case 1: // Heartbeat request
			hb := map[string]any{"op": 1, "d": seq}
			conn.WriteJSON(hb) //nolint:errcheck

		case 11: // Heartbeat ACK — no-op
		}
	}
}

// ── Message handling ──────────────────────────────────────────────────────────

func (b *Bridge) handleMessage(ev messageCreateEvent) {
	// Ignore bot messages.
	if ev.Author.Bot {
		return
	}
	// Ignore non-default message types.
	if ev.Type != 0 {
		return
	}

	b.mu.Lock()
	botID := b.botID
	b.mu.Unlock()

	isGuild := ev.GuildID != ""
	isDM := !isGuild

	// In guild: only respond when @mentioned.
	if isGuild {
		mentioned := false
		for _, u := range ev.Mentions {
			if u.ID == botID {
				mentioned = true
				break
			}
		}
		if !mentioned {
			return
		}
	}

	text := ev.Content
	// Strip @mention from guild messages.
	if isGuild && botID != "" {
		text = strings.TrimSpace(strings.ReplaceAll(text, "<@"+botID+">", ""))
		text = strings.TrimSpace(strings.ReplaceAll(text, "<@!"+botID+">", ""))
	}

	// Command dispatch.
	if strings.HasPrefix(text, "!") {
		b.handleCommand(ev.ChannelID, ev.ID, text, isDM)
		return
	}

	if text == "" {
		return
	}

	// Session lookup.
	threadID := ""
	if isDM {
		threadID = "dm"
	}
	session, err := b.db.FetchCommSession("discord", ev.ChannelID, threadID)
	if err != nil {
		logstore.Write("error", "Discord: fetch session: "+err.Error(), map[string]string{"platform": "discord"})
	}

	convID := ""
	if session != nil {
		convID = session.ActiveConversationID
	}

	// Route to Atlas.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	reply, newConvID, err := b.handler(ctx, BridgeRequest{Text: text, ConvID: convID, Platform: "discord"})
	if err != nil {
		logstore.Write("error", "Discord: handler error: "+err.Error(), map[string]string{"platform": "discord"})
		b.sendMessage(ev.ChannelID, ev.ID, "An error occurred. Please try again.")
		return
	}

	// Update session.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := storage.CommSessionRow{
		Platform:             "discord",
		ChannelID:            ev.ChannelID,
		ThreadID:             threadID,
		ActiveConversationID: newConvID,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastMessageID:        &ev.ID,
	}
	if session != nil {
		row.CreatedAt = session.CreatedAt
	}
	if upsertErr := b.db.UpsertCommSession(row); upsertErr != nil {
		logstore.Write("error", "Discord: upsert session: "+upsertErr.Error(), map[string]string{"platform": "discord"})
	}

	// Send response in chunks.
	for _, chunk := range chunkText(reply, maxChunk) {
		b.sendMessage(ev.ChannelID, ev.ID, chunk)
	}
}

func (b *Bridge) handleCommand(channelID, refMsgID, text string, isDM bool) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])

	cfg := b.cfgFn()
	personaName := cfg.PersonaName
	if personaName == "" {
		personaName = "Atlas"
	}

	switch cmd {
	case "!help":
		b.sendMessage(channelID, refMsgID, fmt.Sprintf(
			"**%s Commands**\n\n"+
				"!help — show this message\n"+
				"!status — runtime status\n"+
				"!reset — start a new conversation\n"+
				"!approvals — list pending approvals\n\n"+
				"Mention me in a message to chat.", personaName))

	case "!status":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, _, err := b.handler(ctx, BridgeRequest{Text: "What is your current status? Give a brief summary.", Platform: "discord"})
		if err != nil {
			b.sendMessage(channelID, refMsgID, "Status: running.")
			return
		}
		b.sendMessage(channelID, refMsgID, reply)

	case "!reset":
		threadID := ""
		if isDM {
			threadID = "dm"
		}
		session, _ := b.db.FetchCommSession("discord", channelID, threadID)
		if session != nil {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			row := storage.CommSessionRow{
				Platform:             "discord",
				ChannelID:            channelID,
				ThreadID:             threadID,
				ActiveConversationID: "",
				CreatedAt:            session.CreatedAt,
				UpdatedAt:            now,
			}
			b.db.UpsertCommSession(row) //nolint:errcheck
		}
		b.sendMessage(channelID, refMsgID, "Conversation reset.")

	case "!approvals":
		count := b.db.CountPendingApprovals()
		if count == 0 {
			b.sendMessage(channelID, refMsgID, "No pending approvals.")
		} else {
			b.sendMessage(channelID, refMsgID, fmt.Sprintf("%d pending approval(s). Check the Atlas web UI.", count))
		}
	}
}

// ── Discord REST API ──────────────────────────────────────────────────────────

type discordMessageReq struct {
	Content          string         `json:"content"`
	MessageReference *discordMsgRef `json:"message_reference,omitempty"`
}

type discordMsgRef struct {
	MessageID string `json:"message_id"`
}

func (b *Bridge) sendMessage(channelID, refMsgID, text string) {
	apiURL := fmt.Sprintf("%s/channels/%s/messages", apiBase, channelID)
	payload := discordMessageReq{Content: text}
	if refMsgID != "" {
		payload.MessageReference = &discordMsgRef{MessageID: refMsgID}
	}
	data, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL,
		strings.NewReader(string(data)))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bot "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		logstore.Write("error", "Discord: sendMessage: "+err.Error(), map[string]string{"platform": "discord"})
		return
	}
	resp.Body.Close()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func chunkText(text string, maxLen int) []string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(runes) > 0 {
		end := maxLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
