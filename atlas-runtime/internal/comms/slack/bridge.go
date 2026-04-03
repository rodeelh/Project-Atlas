// Package slack implements the Slack Socket Mode WebSocket bridge for Atlas.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	connectURL = "https://slack.com/api/apps.connections.open"
	apiBase    = "https://slack.com/api"
	maxChunk   = 3000
)

// Attachment is an inbound file alongside a Slack message.
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

// Bridge implements the Slack Socket Mode bridge.
type Bridge struct {
	botToken string
	appToken string
	db       *storage.DB
	cfgFn    func() config.RuntimeConfigSnapshot
	handler  ChatHandler
	client   *http.Client

	mu        sync.Mutex
	connected bool
	lastErr   string
	teamName  string
	botID     string

	stopCh chan struct{}
	doneCh chan struct{}
}

// New creates a new Slack bridge.
func New(botToken, appToken string, db *storage.DB, cfgFn func() config.RuntimeConfigSnapshot, handler ChatHandler) *Bridge {
	return &Bridge{
		botToken: botToken,
		appToken: appToken,
		db:       db,
		cfgFn:    cfgFn,
		handler:  handler,
		client:   &http.Client{Timeout: 15 * time.Second},
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the Socket Mode loop in a background goroutine.
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

// TeamName returns the connected workspace name.
func (b *Bridge) TeamName() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.teamName
}

// LastError returns the most recent error string.
func (b *Bridge) LastError() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastErr
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
			logstore.Write("info", "Slack bridge stopped", map[string]string{"platform": "slack"})
			return
		default:
		}

		err := b.connect()
		if err != nil {
			b.mu.Lock()
			b.lastErr = err.Error()
			b.connected = false
			b.mu.Unlock()
			logstore.Write("error", "Slack bridge error: "+err.Error(), map[string]string{"platform": "slack"})
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
	wsURL, err := b.openConnection()
	if err != nil {
		return fmt.Errorf("open connection: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}
	defer conn.Close()

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

		var envelope slackEnvelope
		if err := json.Unmarshal(msg, &envelope); err != nil {
			continue
		}

		// Acknowledge every envelope immediately.
		if envelope.EnvelopeID != "" {
			ack := map[string]string{"envelope_id": envelope.EnvelopeID}
			ackData, _ := json.Marshal(ack)
			conn.WriteMessage(websocket.TextMessage, ackData) //nolint:errcheck
		}

		switch envelope.Type {
		case "hello":
			b.mu.Lock()
			b.connected = true
			b.lastErr = ""
			b.mu.Unlock()
			logstore.Write("info", "Slack bridge connected", map[string]string{"platform": "slack"})

		case "disconnect":
			return fmt.Errorf("server requested disconnect")

		case "events_api":
			if envelope.Payload != nil {
				go b.handleEventPayload(*envelope.Payload)
			}
		}
	}
}

// ── Socket Mode types ─────────────────────────────────────────────────────────

type slackEnvelope struct {
	EnvelopeID string        `json:"envelope_id"`
	Type       string        `json:"type"`
	Payload    *slackPayload `json:"payload"`
}

type slackPayload struct {
	Event slackEvent `json:"event"`
}

type slackEvent struct {
	Type        string `json:"type"`
	SubType     string `json:"subtype"`
	Text        string `json:"text"`
	UserID      string `json:"user"`
	ChannelID   string `json:"channel"`
	ThreadTS    string `json:"thread_ts"`
	TS          string `json:"ts"`
	BotID       string `json:"bot_id"`
	ChannelType string `json:"channel_type"`
}

// ── Event handling ────────────────────────────────────────────────────────────

func (b *Bridge) handleEventPayload(payload slackPayload) {
	ev := payload.Event

	// Ignore bot messages and message edits.
	if ev.BotID != "" || ev.SubType != "" {
		return
	}

	isDM := ev.ChannelType == "im"
	isMention := ev.Type == "app_mention"

	if ev.Type != "message" && !isMention {
		return
	}
	if !isDM && !isMention {
		return
	}

	text := ev.Text
	// Strip the bot mention from text.
	b.mu.Lock()
	botID := b.botID
	b.mu.Unlock()
	if botID != "" {
		text = strings.TrimSpace(strings.ReplaceAll(text, "<@"+botID+">", ""))
	}

	// Command dispatch.
	if strings.HasPrefix(text, "!") {
		b.handleCommand(ev.ChannelID, ev.TS, ev.ThreadTS, text)
		return
	}

	if text == "" {
		return
	}

	// Use thread_ts as session key for threading continuity.
	threadKey := ev.ThreadTS
	if threadKey == "" {
		threadKey = ev.TS
	}

	session, err := b.db.FetchCommSession("slack", ev.ChannelID, threadKey)
	if err != nil {
		logstore.Write("error", "Slack: fetch session: "+err.Error(), map[string]string{"platform": "slack"})
	}

	convID := ""
	if session != nil {
		convID = session.ActiveConversationID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	reply, newConvID, handleErr := b.handler(ctx, BridgeRequest{Text: text, ConvID: convID, Platform: "slack"})
	if handleErr != nil {
		logstore.Write("error", "Slack: handler error: "+handleErr.Error(), map[string]string{"platform": "slack"})
		b.postMessage(ev.ChannelID, ev.TS, "An error occurred. Please try again.")
		return
	}

	// Update session.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := storage.CommSessionRow{
		Platform:             "slack",
		ChannelID:            ev.ChannelID,
		ThreadID:             threadKey,
		ActiveConversationID: newConvID,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastMessageID:        &ev.TS,
	}
	if session != nil {
		row.CreatedAt = session.CreatedAt
	}
	if upsertErr := b.db.UpsertCommSession(row); upsertErr != nil {
		logstore.Write("error", "Slack: upsert session: "+upsertErr.Error(), map[string]string{"platform": "slack"})
	}

	// Reply in the thread.
	for _, chunk := range chunkText(reply, maxChunk) {
		b.postMessage(ev.ChannelID, ev.TS, chunk)
	}
}

func (b *Bridge) handleCommand(channelID, ts, threadTS, text string) {
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
		b.postMessage(channelID, ts, fmt.Sprintf(
			"*%s Commands*\n\n"+
				"!help — show this message\n"+
				"!status — runtime status\n"+
				"!reset — start a new conversation\n"+
				"!approvals — list pending approvals",
			personaName))

	case "!status":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, _, err := b.handler(ctx, BridgeRequest{Text: "What is your current status? Give a brief summary.", Platform: "slack"})
		if err != nil {
			b.postMessage(channelID, ts, "Status: running.")
			return
		}
		b.postMessage(channelID, ts, reply)

	case "!reset":
		threadKey := threadTS
		if threadKey == "" {
			threadKey = ts
		}
		session, _ := b.db.FetchCommSession("slack", channelID, threadKey)
		if session != nil {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			row := storage.CommSessionRow{
				Platform:             "slack",
				ChannelID:            channelID,
				ThreadID:             threadKey,
				ActiveConversationID: "",
				CreatedAt:            session.CreatedAt,
				UpdatedAt:            now,
			}
			b.db.UpsertCommSession(row) //nolint:errcheck
		}
		b.postMessage(channelID, ts, "Conversation reset.")

	case "!approvals":
		count := b.db.CountPendingApprovals()
		if count == 0 {
			b.postMessage(channelID, ts, "No pending approvals.")
		} else {
			b.postMessage(channelID, ts, fmt.Sprintf("%d pending approval(s). Check the Atlas web UI.", count))
		}
	}
}

// ── Slack API calls ───────────────────────────────────────────────────────────

func (b *Bridge) openConnection() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connectURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+b.appToken)

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if !result.OK || result.URL == "" {
		return "", fmt.Errorf("apps.connections.open failed: %s", string(body))
	}
	return result.URL, nil
}

func (b *Bridge) postMessage(channelID, threadTS, text string) {
	apiURL := apiBase + "/chat.postMessage"
	payload := map[string]any{
		"channel": channelID,
		"text":    text,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	data, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.botToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		logstore.Write("error", "Slack: postMessage: "+err.Error(), map[string]string{"platform": "slack"})
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
