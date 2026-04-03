// Package telegram implements the Telegram Bot API long-polling bridge for Atlas.
package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

const (
	apiBase           = "https://api.telegram.org/bot"
	maxChunk          = 3500
	maxAttachmentSize = 20 * 1024 * 1024 // 20 MB
	eyesEmoji         = "👀"
	checkEmoji        = "✅"
	errorEmoji        = "❌"
)

// Attachment is an inbound file alongside a Telegram message.
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

// ApprovalResolver resolves a pending approval by tool call ID.
type ApprovalResolver func(toolCallID string, approved bool) error

// ── Telegram API structs ──────────────────────────────────────────────────────

type tgUpdate struct {
	UpdateID      int64             `json:"update_id"`
	Message       *tgMessage        `json:"message"`
	CallbackQuery *tgCallbackQuery  `json:"callback_query"`
}

type tgMessage struct {
	MessageID int64          `json:"message_id"`
	From      *tgUser        `json:"from"`
	Chat      tgChat         `json:"chat"`
	Text      string         `json:"text"`
	Caption   string         `json:"caption"`
	Photo     []tgPhotoSize  `json:"photo"`
	Document  *tgDocument    `json:"document"`
	Location  *tgLocation    `json:"location"`
}

type tgPhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size"`
}

type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int    `json:"file_size"`
}

type tgLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type tgChat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

type tgInlineKeyboard struct {
	InlineKeyboard [][]tgInlineKeyboardButton `json:"inline_keyboard"`
}

type tgInlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type tgBotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// ── Bridge ────────────────────────────────────────────────────────────────────

// Bridge implements Telegram HTTP long-polling.
type Bridge struct {
	token            string
	db               *storage.DB
	cfgFn            func() config.RuntimeConfigSnapshot
	handler          ChatHandler
	client           *http.Client

	mu               sync.Mutex
	offset           int64
	connected        bool
	lastErr          string
	botName          string
	approvalResolver ApprovalResolver

	stopCh chan struct{}
	doneCh chan struct{}
}

// New creates a new Telegram bridge.
func New(token string, db *storage.DB, cfgFn func() config.RuntimeConfigSnapshot, handler ChatHandler) *Bridge {
	return &Bridge{
		token:   token,
		db:      db,
		cfgFn:   cfgFn,
		handler: handler,
		client:  &http.Client{Timeout: 45 * time.Second},
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// SetApprovalResolver sets the function that resolves inline approval callbacks.
func (b *Bridge) SetApprovalResolver(fn ApprovalResolver) {
	b.mu.Lock()
	b.approvalResolver = fn
	b.mu.Unlock()
}

// Start begins the polling loop in a background goroutine.
func (b *Bridge) Start() {
	go b.run()
}

// Stop signals the polling loop to stop and waits for it to exit.
func (b *Bridge) Stop() {
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
	<-b.doneCh
}

// Connected returns true if the bridge is actively polling.
func (b *Bridge) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// BotName returns the validated bot username (empty until connected).
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

// ── Main loop ─────────────────────────────────────────────────────────────────

func (b *Bridge) run() {
	defer close(b.doneCh)

	b.deleteWebhook()

	// FIX #7: stop immediately if getMe fails — bad token = infinite 401 loop.
	name, ok := b.getMe()
	if !ok {
		b.mu.Lock()
		b.lastErr = "getMe failed — check bot token"
		b.mu.Unlock()
		logstore.Write("error", "Telegram bridge: getMe failed — bridge stopped", map[string]string{"platform": "telegram"})
		return
	}

	b.mu.Lock()
	b.botName = name
	b.connected = true
	b.lastErr = ""
	b.mu.Unlock()
	logstore.Write("info", "Telegram bridge connected: @"+name, map[string]string{"platform": "telegram"})

	// FIX #6: register bot command menu with Telegram.
	b.setMyCommands()

	cfg := b.cfgFn()
	baseBackoff := time.Duration(cfg.TelegramPollingRetryBaseSeconds) * time.Second
	if baseBackoff <= 0 {
		baseBackoff = 2 * time.Second
	}
	backoff := baseBackoff
	maxBackoff := 30 * time.Second
	pollTimeout := cfg.TelegramPollingTimeoutSeconds
	if pollTimeout <= 0 {
		pollTimeout = 30
	}

	for {
		select {
		case <-b.stopCh:
			b.mu.Lock()
			b.connected = false
			b.mu.Unlock()
			logstore.Write("info", "Telegram bridge stopped", map[string]string{"platform": "telegram"})
			return
		default:
		}

		updates, err := b.getUpdates(pollTimeout)
		if err != nil {
			b.mu.Lock()
			b.lastErr = err.Error()
			b.mu.Unlock()
			logstore.Write("error", "Telegram poll error: "+err.Error(), map[string]string{"platform": "telegram"})
			select {
			case <-b.stopCh:
				return
			case <-time.After(backoff):
				backoff = minDur(backoff*2, maxBackoff)
			}
			continue
		}
		backoff = baseBackoff

		for _, u := range updates {
			b.handleUpdate(u)
		}
	}
}

// ── Update dispatch ───────────────────────────────────────────────────────────

func (b *Bridge) handleUpdate(u tgUpdate) {
	// FIX #2: handle inline approval button callbacks.
	if u.CallbackQuery != nil {
		b.handleCallbackQuery(*u.CallbackQuery)
		return
	}

	if u.Message == nil {
		return
	}
	msg := u.Message
	chatID := msg.Chat.ID
	cfg := b.cfgFn()

	if !b.isAllowed(msg.From, chatID, cfg) {
		logstore.Write("warn", fmt.Sprintf("Telegram: rejected chat=%d", chatID), map[string]string{"platform": "telegram"})
		return
	}

	// FIX #1: handle photo and document attachments instead of silently dropping them.
	if len(msg.Photo) > 0 || msg.Document != nil {
		b.handleAttachment(chatID, msg.MessageID, msg.From, msg)
		return
	}

	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}
	if text == "" {
		return
	}

	// FIX #10: respect TelegramCommandPrefix from config.
	cmdPrefix := cfg.TelegramCommandPrefix
	if cmdPrefix == "" {
		cmdPrefix = "/"
	}
	if strings.HasPrefix(text, cmdPrefix) {
		b.handleCommand(chatID, msg.MessageID, text, cfg)
		return
	}

	b.handleIncoming(chatID, msg.MessageID, msg.From, text)
}

func (b *Bridge) isAllowed(from *tgUser, chatID int64, cfg config.RuntimeConfigSnapshot) bool {
	if len(cfg.TelegramAllowedUserIDs) == 0 && len(cfg.TelegramAllowedChatIDs) == 0 {
		return true
	}
	for _, id := range cfg.TelegramAllowedChatIDs {
		if id == chatID {
			return true
		}
	}
	if from != nil {
		for _, id := range cfg.TelegramAllowedUserIDs {
			if id == from.ID {
				return true
			}
		}
	}
	return false
}

// ── Message handling ──────────────────────────────────────────────────────────

// handleIncoming acknowledges receipt and routes a text message to the agent.
func (b *Bridge) handleIncoming(chatID, msgID int64, from *tgUser, text string) {
	// Context-aware inbound reaction — only one fires per message.
	switch {
	case reactWithLove(text):
		b.sendReaction(chatID, msgID, "❤")
	case reactWithShock(text):
		b.sendReaction(chatID, msgID, "🤯")
	case reactWithProcessing(text):
		b.sendReaction(chatID, msgID, eyesEmoji)
	// Conversational messages get no inbound reaction.
	}
	b.sendChatAction(chatID, "typing")
	b.processText(chatID, msgID, from, text, nil)
}

// processText calls the Atlas handler, updates the session, and delivers the reply.
// attachments carries any inbound images or documents for vision analysis.
func (b *Bridge) processText(chatID, msgID int64, from *tgUser, text string, attachments []Attachment) {
	session, err := b.db.FetchTelegramSession(chatID)
	if err != nil {
		logstore.Write("error", "Telegram: fetch session: "+err.Error(), map[string]string{"platform": "telegram"})
	}

	convID := ""
	if session != nil {
		convID = session.ActiveConversationID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	reply, newConvID, err := b.handler(ctx, BridgeRequest{Text: text, ConvID: convID, Platform: "telegram", Attachments: attachments})
	if err != nil {
		logstore.Write("error", "Telegram: handler error: "+err.Error(), map[string]string{"platform": "telegram"})
		b.sendReaction(chatID, msgID, errorEmoji)
		b.sendMessage(chatID, "An error occurred. Please try again.")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var userID *int64
	if from != nil {
		userID = &from.ID
	}
	row := storage.TelegramSessionRow{
		ChatID:               chatID,
		UserID:               userID,
		ActiveConversationID: newConvID,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastMessageID:        &msgID,
	}
	if session != nil {
		row.CreatedAt = session.CreatedAt
	}
	if upsertErr := b.db.UpsertTelegramSession(row); upsertErr != nil {
		logstore.Write("error", "Telegram: upsert session: "+upsertErr.Error(), map[string]string{"platform": "telegram"})
	}

	// ✅ only when the message was an action request (proxy for "tools ran").
	if reactWithProcessing(text) {
		b.sendReaction(chatID, msgID, checkEmoji)
	}

	// FIX #3: send any image/file artifacts referenced in the reply.
	for _, fp := range extractFilePaths(reply) {
		if isImageExt(strings.ToLower(filepath.Ext(fp))) {
			b.sendPhoto(chatID, fp)
		} else {
			b.sendDocument(chatID, fp)
		}
	}

	for _, chunk := range chunkText(markdownToHTML(reply), maxChunk) {
		b.sendMessage(chatID, chunk)
	}
}

// ── Attachment handling ───────────────────────────────────────────────────────

// handleAttachment downloads an inbound photo or document, base64-encodes it,
// and passes it directly to the model for vision analysis.
// Attachments are always active work — always react 👀 while processing (matches Swift).
func (b *Bridge) handleAttachment(chatID, msgID int64, from *tgUser, msg *tgMessage) {
	b.sendReaction(chatID, msgID, eyesEmoji)
	b.sendChatAction(chatID, "typing")

	var fileID, fileName, mimeType string
	isImage := false

	if len(msg.Photo) > 0 {
		// Pick the largest available resolution.
		largest := msg.Photo[0]
		for _, p := range msg.Photo[1:] {
			if p.FileSize > largest.FileSize {
				largest = p
			}
		}
		fileID = largest.FileID
		fileName = fmt.Sprintf("photo_%d.jpg", msgID)
		mimeType = "image/jpeg"
		isImage = true
	} else if msg.Document != nil {
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
		if fileName == "" {
			ext := mimeToExt(msg.Document.MimeType)
			fileName = fmt.Sprintf("document_%d%s", msgID, ext)
		}
		mimeType = msg.Document.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		isImage = strings.HasPrefix(mimeType, "image/")
		// Enforce 20 MB size limit before downloading.
		if msg.Document.FileSize > maxAttachmentSize {
			b.sendReaction(chatID, msgID, errorEmoji)
			b.sendMessage(chatID, "Attachment exceeds the 20 MB size limit.")
			return
		}
	}

	if fileID == "" {
		return
	}

	// Resolve the Telegram file path.
	tgPath, err := b.getFilePath(fileID)
	if err != nil {
		logstore.Write("error", "Telegram: getFile: "+err.Error(), map[string]string{"platform": "telegram"})
		b.sendReaction(chatID, msgID, errorEmoji)
		b.sendMessage(chatID, "Could not retrieve the attachment.")
		return
	}

	// Download into memory (for vision) and also save to disk (for agent filesystem access).
	attDir := filepath.Join(config.SupportDir(), "TelegramAttachments",
		fmt.Sprintf("chat-%d", chatID), fmt.Sprintf("message-%d", msgID))
	localPath := filepath.Join(attDir, sanitizeFilename(fileName))

	fileBytes, err := b.downloadTelegramFileBytes(tgPath, localPath)
	if err != nil {
		logstore.Write("error", "Telegram: download file: "+err.Error(), map[string]string{"platform": "telegram"})
		b.sendReaction(chatID, msgID, errorEmoji)
		b.sendMessage(chatID, "Could not download the attachment.")
		return
	}

	// Build caption / agent prompt.
	agentText := msg.Caption
	isPDF := mimeType == "application/pdf"
	if agentText == "" {
		switch {
		case isImage:
			agentText = "Please analyse this image."
		case isPDF:
			agentText = "Please read and summarise this document."
		default:
			agentText = fmt.Sprintf("A file was attached: %s (saved to %s). Please process it.", fileName, localPath)
		}
	} else if !isImage && !isPDF {
		// For other binary types, append the local path — model can't see them directly.
		agentText = fmt.Sprintf("%s\n\n[File saved to: %s]", agentText, localPath)
	}

	// Pass images and PDFs directly to the model via the attachments channel.
	// Other binary types (zip, exe, etc.) are referenced by path in agentText only.
	var attachments []Attachment
	if isImage || mimeType == "application/pdf" {
		attachments = []Attachment{{
			Filename: fileName,
			MimeType: mimeType,
			Data:     base64.StdEncoding.EncodeToString(fileBytes),
		}}
	}

	b.processText(chatID, msgID, from, agentText, attachments)
}

// ── Callback query handling ───────────────────────────────────────────────────

// handleCallbackQuery processes inline keyboard button taps (approval approve/deny).
func (b *Bridge) handleCallbackQuery(q tgCallbackQuery) {
	b.answerCallbackQuery(q.ID, "") // dismiss the spinner immediately

	data := q.Data
	var toolCallID string
	var approved bool

	switch {
	case strings.HasPrefix(data, "approve:"):
		toolCallID = strings.TrimPrefix(data, "approve:")
		approved = true
	case strings.HasPrefix(data, "deny:"):
		toolCallID = strings.TrimPrefix(data, "deny:")
		approved = false
	default:
		return
	}

	b.mu.Lock()
	resolver := b.approvalResolver
	b.mu.Unlock()

	chatID := int64(0)
	if q.Message != nil {
		chatID = q.Message.Chat.ID
	}

	if resolver == nil {
		if chatID != 0 {
			b.sendMessage(chatID, "Approval handling is not configured. Use the web UI.")
		}
		return
	}

	if err := resolver(toolCallID, approved); err != nil {
		logstore.Write("error", "Telegram: resolve approval: "+err.Error(), map[string]string{"platform": "telegram"})
		if chatID != 0 {
			b.sendMessage(chatID, "Could not resolve approval: "+err.Error())
		}
		return
	}

	if chatID != 0 {
		action := "Approved ✅"
		if !approved {
			action = "Denied ❌"
		}
		b.sendMessage(chatID, action)
	}
}

// ── Command handling ──────────────────────────────────────────────────────────

func (b *Bridge) handleCommand(chatID, msgID int64, text string, cfg config.RuntimeConfigSnapshot) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	// Strip bot username suffix (e.g. /start@MyBot → /start).
	cmdRaw := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])

	// FIX #10: normalize to "/" prefix for switch matching.
	prefix := cfg.TelegramCommandPrefix
	if prefix == "" {
		prefix = "/"
	}
	cmd := "/" + strings.TrimPrefix(cmdRaw, prefix)

	personaName := cfg.PersonaName
	if personaName == "" {
		personaName = "Atlas"
	}

	switch cmd {
	case "/start":
		b.sendMessage(chatID, fmt.Sprintf(
			"<b>%s</b> is ready.\n\nSend me a message to get started. Use /help to see available commands.",
			personaName))

	case "/help":
		b.sendMessage(chatID, fmt.Sprintf(
			"<b>%s Commands</b>\n\n"+
				"/start — greeting\n"+
				"/help — show this message\n"+
				"/status — runtime status\n"+
				"/approvals — list pending approvals\n"+
				"/automations — list scheduled automations\n"+
				"/run &lt;name&gt; — trigger an automation\n"+
				"/reset — start a new conversation\n\n"+
				"Just send a message to chat with %s.",
			personaName, personaName))

	case "/status":
		b.sendReaction(chatID, msgID, eyesEmoji)
		b.sendChatAction(chatID, "typing")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, _, err := b.handler(ctx, BridgeRequest{Text: "What is your current status? Give a brief one-line summary.", Platform: "telegram"})
		if err != nil {
			b.sendMessage(chatID, "Status: running.")
			return
		}
		b.sendMessage(chatID, markdownToHTML(reply))

	case "/reset":
		session, err := b.db.FetchTelegramSession(chatID)
		if err == nil && session != nil {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			row := storage.TelegramSessionRow{
				ChatID:               chatID,
				UserID:               session.UserID,
				ActiveConversationID: "",
				CreatedAt:            session.CreatedAt,
				UpdatedAt:            now,
			}
			b.db.UpsertTelegramSession(row) //nolint:errcheck
		}
		b.sendMessage(chatID, "Conversation reset. Send a message to start fresh.")

	case "/approvals":
		// FIX #2: show inline approve/deny buttons per pending approval.
		pending, err := b.db.ListPendingApprovals(3)
		if err != nil || len(pending) == 0 {
			count := b.db.CountPendingApprovals()
			if count == 0 {
				b.sendMessage(chatID, "No pending approvals.")
			} else {
				b.sendMessage(chatID, fmt.Sprintf("%d pending approval(s). Check the Atlas web UI to review.", count))
			}
			return
		}
		var sb strings.Builder
		total := b.db.CountPendingApprovals()
		sb.WriteString(fmt.Sprintf("<b>%d Pending Approval(s)</b>\n\n", total))
		keyboard := make([][]tgInlineKeyboardButton, 0, len(pending))
		for i, r := range pending {
			sb.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, r.Summary))
			keyboard = append(keyboard, []tgInlineKeyboardButton{
				{Text: "✅ Approve", CallbackData: "approve:" + r.ToolCallID},
				{Text: "❌ Deny", CallbackData: "deny:" + r.ToolCallID},
			})
		}
		if total > len(pending) {
			sb.WriteString(fmt.Sprintf("\n...and %d more. Check the Atlas web UI for the full list.", total-len(pending)))
		}
		b.sendMessageWithKeyboard(chatID, sb.String(), &tgInlineKeyboard{InlineKeyboard: keyboard})

	case "/automations":
		// FIX #5: list scheduled automations via agent handler.
		b.sendChatAction(chatID, "typing")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reply, _, err := b.handler(ctx, BridgeRequest{
			Text:     "List all scheduled automations (GREMLINS) with their names and schedules. Be concise.",
			Platform: "telegram",
		})
		if err != nil {
			b.sendMessage(chatID, "Could not retrieve automations.")
			return
		}
		b.sendMessage(chatID, markdownToHTML(reply))

	case "/run":
		// FIX #5: trigger automation by name.
		if len(parts) < 2 {
			b.sendMessage(chatID, "Usage: /run &lt;automation name or ID&gt;")
			return
		}
		automationName := strings.Join(parts[1:], " ")
		b.sendReaction(chatID, msgID, eyesEmoji)
		b.sendChatAction(chatID, "typing")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		reply, _, err := b.handler(ctx, BridgeRequest{
			Text:     fmt.Sprintf("Run the automation named or with ID %q now.", automationName),
			Platform: "telegram",
		})
		if err != nil {
			b.sendReaction(chatID, msgID, errorEmoji)
			b.sendMessage(chatID, "Could not run automation.")
			return
		}
		b.sendReaction(chatID, msgID, checkEmoji)
		for _, chunk := range chunkText(markdownToHTML(reply), maxChunk) {
			b.sendMessage(chatID, chunk)
		}

	default:
		b.sendMessage(chatID, "Unknown command. Use /help to see available commands.")
	}
}

// ── Telegram API calls ────────────────────────────────────────────────────────

type tgGetUpdatesResp struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

func (b *Bridge) getUpdates(timeout int) ([]tgUpdate, error) {
	b.mu.Lock()
	offset := b.offset
	b.mu.Unlock()

	apiURL := fmt.Sprintf("%s%s/getUpdates?timeout=%d&offset=%d", apiBase, b.token, timeout, offset)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+10)*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result tgGetUpdatesResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse getUpdates: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates not ok: %s", string(body))
	}

	if len(result.Result) > 0 {
		b.mu.Lock()
		b.offset = result.Result[len(result.Result)-1].UpdateID + 1
		b.mu.Unlock()
	}
	return result.Result, nil
}

type tgGetMeResp struct {
	OK     bool    `json:"ok"`
	Result *tgUser `json:"result"`
}

func (b *Bridge) getMe() (string, bool) {
	apiURL := fmt.Sprintf("%s%s/getMe", apiBase, b.token)
	resp, err := b.client.Get(apiURL)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result tgGetMeResp
	if err := json.Unmarshal(body, &result); err != nil || !result.OK || result.Result == nil {
		return "", false
	}
	name := result.Result.Username
	if name == "" {
		name = result.Result.FirstName
	}
	return name, true
}

func (b *Bridge) deleteWebhook() {
	apiURL := fmt.Sprintf("%s%s/deleteWebhook", apiBase, b.token)
	resp, err := b.client.Get(apiURL)
	if err == nil {
		resp.Body.Close()
	}
}

// FIX #6: register command menu in the Telegram bot UI.
func (b *Bridge) setMyCommands() {
	commands := []tgBotCommand{
		{Command: "start", Description: "Show greeting"},
		{Command: "help", Description: "List all commands"},
		{Command: "status", Description: "Show runtime status"},
		{Command: "approvals", Description: "List pending approvals"},
		{Command: "automations", Description: "List scheduled automations"},
		{Command: "run", Description: "Trigger an automation by name"},
		{Command: "reset", Description: "Start a new conversation"},
	}
	apiURL := fmt.Sprintf("%s%s/setMyCommands", apiBase, b.token)
	payload := struct {
		Commands []tgBotCommand `json:"commands"`
	}{Commands: commands}
	data, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return // best-effort
	}
	resp.Body.Close()
}

// sendMessage sends a plain text message (calls sendMessageWithKeyboard with no keyboard).
func (b *Bridge) sendMessage(chatID int64, text string) {
	b.sendMessageWithKeyboard(chatID, text, nil)
}

// sendMessageWithKeyboard sends an HTML message with an optional inline keyboard.
// FIX #4: falls back to plain text if Telegram rejects the HTML.
type tgSendMessageReq struct {
	ChatID      int64             `json:"chat_id"`
	Text        string            `json:"text"`
	ParseMode   string            `json:"parse_mode,omitempty"`
	ReplyMarkup *tgInlineKeyboard `json:"reply_markup,omitempty"`
}

func (b *Bridge) sendMessageWithKeyboard(chatID int64, text string, keyboard *tgInlineKeyboard) {
	apiURL := fmt.Sprintf("%s%s/sendMessage", apiBase, b.token)

	send := func(payload tgSendMessageReq) bool {
		data, _ := json.Marshal(payload)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.client.Do(req)
		if err != nil {
			logstore.Write("error", "Telegram: sendMessage: "+err.Error(), map[string]string{"platform": "telegram"})
			return false
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var result struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return true // assume OK if we can't parse the response
		}
		if !result.OK {
			logstore.Write("warn", "Telegram: sendMessage rejected: "+result.Description, map[string]string{"platform": "telegram"})
		}
		return result.OK
	}

	// First attempt: HTML with keyboard.
	ok := send(tgSendMessageReq{ChatID: chatID, Text: text, ParseMode: "HTML", ReplyMarkup: keyboard})
	if ok {
		return
	}

	// Retry without keyboard (in case markup was rejected).
	if keyboard != nil {
		ok = send(tgSendMessageReq{ChatID: chatID, Text: text, ParseMode: "HTML"})
		if ok {
			return
		}
	}

	// Final fallback: plain text, no markup.
	send(tgSendMessageReq{ChatID: chatID, Text: stripHTML(text)})
}

type tgReactionReq struct {
	ChatID    int64            `json:"chat_id"`
	MessageID int64            `json:"message_id"`
	Reaction  []tgReactionType `json:"reaction"`
}

type tgReactionType struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

func (b *Bridge) sendReaction(chatID, msgID int64, emoji string) {
	apiURL := fmt.Sprintf("%s%s/setMessageReaction", apiBase, b.token)
	payload := tgReactionReq{
		ChatID:    chatID,
		MessageID: msgID,
		Reaction:  []tgReactionType{{Type: "emoji", Emoji: emoji}},
	}
	data, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return // best-effort
	}
	resp.Body.Close()
}

// FIX #8: typing indicator.
func (b *Bridge) sendChatAction(chatID int64, action string) {
	apiURL := fmt.Sprintf("%s%s/sendChatAction", apiBase, b.token)
	payload := struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{ChatID: chatID, Action: action}
	data, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return // best-effort
	}
	resp.Body.Close()
}

// FIX #2: answer callback query to dismiss the loading spinner on inline buttons.
func (b *Bridge) answerCallbackQuery(callbackQueryID, text string) {
	apiURL := fmt.Sprintf("%s%s/answerCallbackQuery", apiBase, b.token)
	payload := struct {
		CallbackQueryID string `json:"callback_query_id"`
		Text            string `json:"text,omitempty"`
	}{CallbackQueryID: callbackQueryID, Text: text}
	data, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return // best-effort
	}
	resp.Body.Close()
}

// ── File API ──────────────────────────────────────────────────────────────────

type tgGetFileResp struct {
	OK     bool `json:"ok"`
	Result struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// getFilePath resolves a Telegram file_id to a downloadable file path.
func (b *Bridge) getFilePath(fileID string) (string, error) {
	apiURL := fmt.Sprintf("%s%s/getFile?file_id=%s", apiBase, b.token, fileID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("getFile request: %w", err)
	}
	defer resp.Body.Close()
	var result tgGetFileResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || !result.OK || result.Result.FilePath == "" {
		return "", fmt.Errorf("getFile: unexpected response")
	}
	return result.Result.FilePath, nil
}

// downloadTelegramFileBytes downloads a Telegram CDN file, saves it to localPath,
// and returns the raw bytes for in-memory use (e.g. vision base64 encoding).
func (b *Bridge) downloadTelegramFileBytes(tgFilePath, localPath string) ([]byte, error) {
	dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.token, tgFilePath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	lr := &io.LimitedReader{R: resp.Body, N: maxAttachmentSize + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > maxAttachmentSize {
		return nil, fmt.Errorf("file exceeds 20 MB limit")
	}

	// Save to disk so the agent can reference the local path if needed.
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}
	return data, nil
}

// FIX #3: send a local image file to a chat.
func (b *Bridge) sendPhoto(chatID int64, filePath string) {
	b.sendFileMultipart(chatID, filePath, "sendPhoto", "photo")
}

// FIX #3: send a local document file to a chat.
func (b *Bridge) sendDocument(chatID int64, filePath string) {
	b.sendFileMultipart(chatID, filePath, "sendDocument", "document")
}

func (b *Bridge) sendFileMultipart(chatID int64, filePath, method, fieldName string) {
	f, err := os.Open(filePath)
	if err != nil {
		logstore.Write("error", "Telegram: open file for send: "+err.Error(), map[string]string{"platform": "telegram"})
		return
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	fw, err := w.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return
	}
	if _, err := io.Copy(fw, f); err != nil {
		return
	}
	w.Close()

	apiURL := fmt.Sprintf("%s%s/%s", apiBase, b.token, method)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := b.client.Do(req)
	if err != nil {
		logstore.Write("error", "Telegram: sendFile: "+err.Error(), map[string]string{"platform": "telegram"})
		return
	}
	resp.Body.Close()
}

// ── Text utilities ────────────────────────────────────────────────────────────

// markdownToHTML converts markdown-style text to Telegram HTML.
func markdownToHTML(text string) string {
	// Escape HTML special chars first (before inserting tags).
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")

	// FIX #9: handle uppercase/mixed language tags (e.g. ```Go, ```JavaScript).
	fenceRe := regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\n?(.*?)```")
	text = fenceRe.ReplaceAllStringFunc(text, func(m string) string {
		inner := fenceRe.FindStringSubmatch(m)
		if len(inner) < 2 {
			return m
		}
		return "<pre>" + strings.TrimSpace(inner[1]) + "</pre>"
	})

	inlineRe := regexp.MustCompile("`([^`]+)`")
	text = inlineRe.ReplaceAllString(text, "<code>$1</code>")

	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldRe.ReplaceAllString(text, "<b>$1</b>")

	italicRe := regexp.MustCompile(`\*(.+?)\*`)
	text = italicRe.ReplaceAllString(text, "<i>$1</i>")
	italicUnderRe := regexp.MustCompile(`_(.+?)_`)
	text = italicUnderRe.ReplaceAllString(text, "<i>$1</i>")

	strikeRe := regexp.MustCompile(`~~(.+?)~~`)
	text = strikeRe.ReplaceAllString(text, "<s>$1</s>")

	return text
}

// stripHTML removes HTML tags and un-escapes entities — used as sendMessage fallback.
func stripHTML(s string) string {
	tagRe := regexp.MustCompile(`<[^>]+>`)
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

// filePathRe matches absolute macOS file paths with sendable extensions.
var filePathRe = regexp.MustCompile(`(?i)(/(?:Users|tmp|var|Library|private)[^\s"'<>]+\.(?:jpg|jpeg|png|gif|webp|pdf|txt|md|json))`)

// extractFilePaths returns unique local file paths found in text that actually exist on disk.
func extractFilePaths(text string) []string {
	matches := filePathRe.FindAllString(text, 10)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			if _, err := os.Stat(m); err == nil {
				out = append(out, m)
			}
		}
	}
	return out
}

func isImageExt(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

// sanitizeFilename removes filesystem-unsafe characters.
func sanitizeFilename(name string) string {
	invalidRe := regexp.MustCompile(`[/\\:*?"<>|]`)
	safe := invalidRe.ReplaceAllString(name, "_")
	if safe == "" {
		safe = "file"
	}
	const maxLen = 200
	if len(safe) > maxLen {
		ext := filepath.Ext(safe)
		safe = safe[:maxLen-len(ext)] + ext
	}
	return safe
}

// mimeToExt maps common MIME types to file extensions.
func mimeToExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	default:
		return ""
	}
}

// chunkText splits text into chunks of at most maxLen runes.
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

// ── Reaction heuristics (ported from Swift TelegramBridge) ───────────────────

// reactWithLove returns true for praise / thank-you messages.
func reactWithLove(text string) bool {
	s := strings.ToLower(text)
	for _, term := range []string{
		"thank you", "thanks", "thx", "ty",
		"great job", "good job", "nice job", "well done",
		"awesome", "amazing", "fantastic", "brilliant", "excellent",
		"perfect", "love it", "love you", "you're the best", "you are the best",
		"incredible", "outstanding", "superb", "magnificent",
		"<3", "❤", "🙏",
	} {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

// reactWithShock returns true for surprise / shock expressions.
func reactWithShock(text string) bool {
	s := strings.ToLower(text)
	for _, term := range []string{
		"omg", "oh my god", "oh my gosh", "no way", "what the",
		"wtf", "wth", "holy", "shut up", "shut the",
		"i can't believe", "unbelievable", "impossible", "seriously?",
		"are you kidding", "you're joking", "no way!", "whoa", "woah",
		"mind blown", "jaw drop", "shocking", "insane", "crazy",
		"this is wild", "that's wild",
	} {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

// reactWithProcessing returns true when the message requests real action work
// (file ops, search, generation, etc.) — both an action verb AND a target must match.
func reactWithProcessing(text string) bool {
	s := strings.ToLower(text)
	verbs := []string{
		"create", "add", "schedule", "book", "set", "send", "open", "run",
		"search", "find", "look up", "fetch", "get me", "download", "upload",
		"generate", "make", "build", "write", "edit", "delete", "remove",
		"move", "copy", "rename", "summarize", "translate", "convert",
		"remind", "automate", "trigger", "execute",
	}
	targets := []string{
		"file", "folder", "document", "image", "photo", "email", "message",
		"calendar", "event", "meeting", "reminder", "note", "contact",
		"web", "website", "url", "link", "page", "result",
		"automation", "gremlin", "workflow", "script",
		"app", "application", "window",
	}
	hasVerb := false
	for _, v := range verbs {
		if strings.Contains(s, v) {
			hasVerb = true
			break
		}
	}
	if !hasVerb {
		return false
	}
	for _, t := range targets {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
