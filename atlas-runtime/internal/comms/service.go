// Package comms implements the communications platform service for the Go runtime.
// It provides status snapshots, channel listings, and platform enable/validate operations
// sourced from config.json, the Keychain credential bundle, and the shared SQLite database.
package comms

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"atlas-runtime-go/internal/comms/discord"
	"atlas-runtime-go/internal/comms/slack"
	"atlas-runtime-go/internal/comms/telegram"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/storage"
)

// BridgeAttachment is a file attached to an inbound bridge message.
// Data is raw base64 (no data-URL prefix). MimeType is e.g. "image/jpeg", "application/pdf".
type BridgeAttachment struct {
	Filename string
	MimeType string
	Data     string
}

// BridgeRequest is the unified request type passed from any bridge to Atlas.
// It mirrors chat.MessageRequest so that every capability available in the web
// chat is automatically available across all channels. When a new field is added
// to chat.MessageRequest, add it here and map it in main.go — one place each.
type BridgeRequest struct {
	Text        string
	ConvID      string
	Platform    string
	Attachments []BridgeAttachment
}

// ChatHandler is the function type used by bridges to route messages to Atlas.
type ChatHandler func(ctx context.Context, req BridgeRequest) (string, string, error)

// ── JSON shapes that match the Swift CommunicationsSnapshot / CommunicationChannel ──

// PlatformStatus mirrors Swift CommunicationPlatformStatus (camelCase JSON tags).
type PlatformStatus struct {
	Platform             string            `json:"platform"`
	ID                   string            `json:"id"`
	Enabled              bool              `json:"enabled"`
	Connected            bool              `json:"connected"`
	Available            bool              `json:"available"`
	SetupState           string            `json:"setupState"`
	StatusLabel          string            `json:"statusLabel"`
	ConnectedAccountName *string           `json:"connectedAccountName"`
	CredentialConfigured bool              `json:"credentialConfigured"`
	BlockingReason       *string           `json:"blockingReason"`
	RequiredCredentials  []string          `json:"requiredCredentials"`
	LastError            *string           `json:"lastError"`
	LastUpdatedAt        *string           `json:"lastUpdatedAt"`
	Metadata             map[string]string `json:"metadata"`
}

// Snapshot mirrors Swift CommunicationsSnapshot.
type Snapshot struct {
	Platforms []PlatformStatus `json:"platforms"`
	Channels  []ChannelRecord  `json:"channels"`
}

// ChannelRecord mirrors Swift CommunicationChannel.
type ChannelRecord struct {
	ID                      string  `json:"id"`
	Platform                string  `json:"platform"`
	ChannelID               string  `json:"channelID"`
	ChannelName             *string `json:"channelName"`
	UserID                  *string `json:"userID"`
	ThreadID                *string `json:"threadID"`
	ActiveConversationID    string  `json:"activeConversationID"`
	CreatedAt               string  `json:"createdAt"`
	UpdatedAt               string  `json:"updatedAt"`
	LastMessageID           *string `json:"lastMessageID"`
	CanReceiveNotifications bool    `json:"canReceiveNotifications"`
}

// TelegramSession mirrors Swift TelegramSession.
type TelegramSession struct {
	ChatID                int64  `json:"chatID"`
	UserID                *int64 `json:"userID"`
	ActiveConversationID  string `json:"activeConversationID"`
	CreatedAt             string `json:"createdAt"`
	UpdatedAt             string `json:"updatedAt"`
	LastTelegramMessageID *int64 `json:"lastTelegramMessageID"`
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service provides communications platform operations for the Go runtime.
type Service struct {
	cfgStore         *config.Store
	db               *storage.DB
	handler          ChatHandler
	approvalResolver telegram.ApprovalResolver
	mu               sync.RWMutex
	tgBridge         *telegram.Bridge
	discBridge       *discord.Bridge
	slackBridge      *slack.Bridge
}

// New creates a new communications Service.
func New(cfgStore *config.Store, db *storage.DB) *Service {
	return &Service{cfgStore: cfgStore, db: db}
}

// SetChatHandler sets the handler function used by bridges to route messages to Atlas.
// Must be called before Start().
func (s *Service) SetChatHandler(h ChatHandler) {
	s.handler = h
}

// SetApprovalResolver sets the function used by the Telegram bridge to resolve inline approval buttons.
// Must be called before Start().
func (s *Service) SetApprovalResolver(fn telegram.ApprovalResolver) {
	s.approvalResolver = fn
}

// Start launches all enabled platform bridges.
func (s *Service) Start() {
	cfg := s.cfgStore.Load()
	bundle := readBundle()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startBridges(cfg, bundle)
}

// Stop shuts down all running bridges.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopBridges()
}

func (s *Service) startBridges(cfg config.RuntimeConfigSnapshot, bundle credBundle) {
	if s.handler == nil {
		return
	}
	cfgFn := s.cfgStore.Load

	if cfg.TelegramEnabled && strVal(bundle.TelegramBotToken) != "" && s.tgBridge == nil {
		h := s.handler
		tgHandler := telegram.ChatHandler(func(ctx context.Context, req telegram.BridgeRequest) (string, string, error) {
			ba := make([]BridgeAttachment, len(req.Attachments))
			for i, a := range req.Attachments {
				ba[i] = BridgeAttachment{Filename: a.Filename, MimeType: a.MimeType, Data: a.Data}
			}
			return h(ctx, BridgeRequest{Text: req.Text, ConvID: req.ConvID, Platform: req.Platform, Attachments: ba})
		})
		b := telegram.New(strVal(bundle.TelegramBotToken), s.db, cfgFn, tgHandler)
		if s.approvalResolver != nil {
			b.SetApprovalResolver(s.approvalResolver)
		}
		s.tgBridge = b
		b.Start()
	}
	if cfg.DiscordEnabled && strVal(bundle.DiscordBotToken) != "" && s.discBridge == nil {
		h := s.handler
		discHandler := discord.ChatHandler(func(ctx context.Context, req discord.BridgeRequest) (string, string, error) {
			ba := make([]BridgeAttachment, len(req.Attachments))
			for i, a := range req.Attachments {
				ba[i] = BridgeAttachment{Filename: a.Filename, MimeType: a.MimeType, Data: a.Data}
			}
			return h(ctx, BridgeRequest{Text: req.Text, ConvID: req.ConvID, Platform: req.Platform, Attachments: ba})
		})
		b := discord.New(strVal(bundle.DiscordBotToken), s.db, cfgFn, discHandler)
		s.discBridge = b
		b.Start()
	}
	if cfg.SlackEnabled && strVal(bundle.SlackBotToken) != "" && strVal(bundle.SlackAppToken) != "" && s.slackBridge == nil {
		h := s.handler
		slackHandler := slack.ChatHandler(func(ctx context.Context, req slack.BridgeRequest) (string, string, error) {
			ba := make([]BridgeAttachment, len(req.Attachments))
			for i, a := range req.Attachments {
				ba[i] = BridgeAttachment{Filename: a.Filename, MimeType: a.MimeType, Data: a.Data}
			}
			return h(ctx, BridgeRequest{Text: req.Text, ConvID: req.ConvID, Platform: req.Platform, Attachments: ba})
		})
		b := slack.New(strVal(bundle.SlackBotToken), strVal(bundle.SlackAppToken), s.db, cfgFn, slackHandler)
		s.slackBridge = b
		b.Start()
	}
}

func (s *Service) stopBridges() {
	if s.tgBridge != nil {
		s.tgBridge.Stop()
		s.tgBridge = nil
	}
	if s.discBridge != nil {
		s.discBridge.Stop()
		s.discBridge = nil
	}
	if s.slackBridge != nil {
		s.slackBridge.Stop()
		s.slackBridge = nil
	}
}

// Snapshot returns the full communications snapshot (platforms + channels).
func (s *Service) Snapshot() Snapshot {
	cfg := s.cfgStore.Load()
	bundle := readBundle()
	channels := s.channels()

	s.mu.RLock()
	tgConnected := s.tgBridge != nil && s.tgBridge.Connected()
	var tgAccount *string
	if s.tgBridge != nil && s.tgBridge.BotName() != "" {
		n := "@" + s.tgBridge.BotName()
		tgAccount = &n
	}
	var tgErr *string
	if s.tgBridge != nil && s.tgBridge.LastError() != "" {
		e := s.tgBridge.LastError()
		tgErr = &e
	}
	discConnected := s.discBridge != nil && s.discBridge.Connected()
	var discAccount *string
	if s.discBridge != nil && s.discBridge.BotName() != "" {
		n := s.discBridge.BotName()
		discAccount = &n
	}
	var discErr *string
	if s.discBridge != nil && s.discBridge.LastError() != "" {
		e := s.discBridge.LastError()
		discErr = &e
	}
	slackConnected := s.slackBridge != nil && s.slackBridge.Connected()
	var slackAccount *string
	if s.slackBridge != nil && s.slackBridge.TeamName() != "" {
		n := s.slackBridge.TeamName()
		slackAccount = &n
	}
	var slackErr *string
	if s.slackBridge != nil && s.slackBridge.LastError() != "" {
		e := s.slackBridge.LastError()
		slackErr = &e
	}
	s.mu.RUnlock()

	platforms := []PlatformStatus{
		s.platformStatus("telegram", cfg, bundle, tgConnected, tgAccount, tgErr),
		s.platformStatus("discord", cfg, bundle, discConnected, discAccount, discErr),
		s.platformStatus("slack", cfg, bundle, slackConnected, slackAccount, slackErr),
	}

	return Snapshot{
		Platforms: platforms,
		Channels:  channels,
	}
}

// Channels returns all communication channels from SQLite.
func (s *Service) Channels() []ChannelRecord {
	return s.channels()
}

func (s *Service) channels() []ChannelRecord {
	rows, err := s.db.ListCommunicationChannels("")
	if err != nil {
		return []ChannelRecord{}
	}
	out := make([]ChannelRecord, 0, len(rows))
	for _, r := range rows {
		tid := normalizedThreadID(r.ThreadID)
		out = append(out, ChannelRecord{
			ID:                      strings.Join([]string{r.Platform, r.ChannelID, r.ThreadID}, ":"),
			Platform:                r.Platform,
			ChannelID:               r.ChannelID,
			ChannelName:             r.ChannelName,
			UserID:                  r.UserID,
			ThreadID:                tid,
			ActiveConversationID:    r.ActiveConversationID,
			CreatedAt:               r.CreatedAt,
			UpdatedAt:               r.UpdatedAt,
			LastMessageID:           r.LastMessageID,
			CanReceiveNotifications: true,
		})
	}
	return out
}

func normalizedThreadID(raw string) *string {
	if raw == "" {
		return nil
	}
	return &raw
}

// TelegramSessions returns all known Telegram sessions from SQLite.
func (s *Service) TelegramSessions() []TelegramSession {
	rows, err := s.db.ListTelegramSessions()
	if err != nil {
		return []TelegramSession{}
	}
	out := make([]TelegramSession, 0, len(rows))
	for _, r := range rows {
		out = append(out, TelegramSession{
			ChatID:                r.ChatID,
			UserID:                r.UserID,
			ActiveConversationID:  r.ActiveConversationID,
			CreatedAt:             r.CreatedAt,
			UpdatedAt:             r.UpdatedAt,
			LastTelegramMessageID: r.LastMessageID,
		})
	}
	return out
}

// SetupValues returns existing credential values for the given platform (for pre-filling forms).
func (s *Service) SetupValues(platform string) map[string]string {
	bundle := readBundle()
	cfg := s.cfgStore.Load()
	values := map[string]string{}

	switch platform {
	case "telegram":
		if t := strVal(bundle.TelegramBotToken); t != "" {
			values["telegram"] = t
		}
	case "discord":
		if d := strVal(bundle.DiscordBotToken); d != "" {
			values["discord"] = d
		}
		if cfg.DiscordClientID != "" {
			values["discordClientID"] = cfg.DiscordClientID
		}
	case "slack":
		if b := strVal(bundle.SlackBotToken); b != "" {
			values["slackBot"] = b
		}
		if a := strVal(bundle.SlackAppToken); a != "" {
			values["slackApp"] = a
		}
	}
	return values
}

// UpdatePlatform enables or disables a platform in config.json and returns the updated status.
func (s *Service) UpdatePlatform(platform string, enabled bool) (PlatformStatus, error) {
	cfg := s.cfgStore.Load()
	switch platform {
	case "telegram":
		cfg.TelegramEnabled = enabled
	case "discord":
		cfg.DiscordEnabled = enabled
	case "slack":
		cfg.SlackEnabled = enabled
	default:
		return PlatformStatus{}, fmt.Errorf("unknown platform: %s", platform)
	}
	if err := s.cfgStore.Save(cfg); err != nil {
		return PlatformStatus{}, fmt.Errorf("save config: %w", err)
	}

	bundle := readBundle()
	s.mu.Lock()
	if !enabled {
		switch platform {
		case "telegram":
			if s.tgBridge != nil {
				s.tgBridge.Stop()
				s.tgBridge = nil
			}
		case "discord":
			if s.discBridge != nil {
				s.discBridge.Stop()
				s.discBridge = nil
			}
		case "slack":
			if s.slackBridge != nil {
				s.slackBridge.Stop()
				s.slackBridge = nil
			}
		}
	} else {
		s.startBridges(cfg, bundle)
	}
	s.mu.Unlock()

	return s.platformStatus(platform, cfg, bundle, false, nil, nil), nil
}

// ValidatePlatform connects to the platform API with the given credentials (or Keychain creds
// if none are provided), stores any new credentials in the Keychain bundle on success,
// and returns the resulting platform status.
func (s *Service) ValidatePlatform(platform string, credentials map[string]string, discordClientID string) (PlatformStatus, error) {
	bundle := readBundle()
	cfg := s.cfgStore.Load()

	var (
		connected   bool
		accountName *string
		lastErr     *string
	)

	switch platform {
	case "telegram":
		token := credentials["telegram"]
		if token == "" {
			token = strVal(bundle.TelegramBotToken)
		}
		if token == "" {
			e := "No Telegram bot token configured."
			return s.platformStatus(platform, cfg, bundle, false, nil, &e), nil
		}

		ok, username, err := validateTelegram(token)
		if err != nil || !ok {
			errStr := "Telegram validation failed."
			if err != nil {
				errStr = err.Error()
			}
			return s.platformStatus(platform, cfg, bundle, false, nil, &errStr), nil
		}

		connected = true
		accountName = username

		// Persist token + enable platform.
		bundle.TelegramBotToken = &token
		writeBundle(bundle)
		cfg.TelegramEnabled = true
		if err := s.cfgStore.Save(cfg); err != nil {
			log.Printf("comms: ValidatePlatform: save config: %v", err)
		}

	case "discord":
		token := credentials["discord"]
		if token == "" {
			token = strVal(bundle.DiscordBotToken)
		}
		if discordClientID != "" {
			cfg.DiscordClientID = discordClientID
		}
		if token == "" {
			e := "No Discord bot token configured."
			return s.platformStatus(platform, cfg, bundle, false, nil, &e), nil
		}

		ok, botName, err := validateDiscord(token)
		if err != nil || !ok {
			errStr := "Discord validation failed."
			if err != nil {
				errStr = err.Error()
			}
			return s.platformStatus(platform, cfg, bundle, false, nil, &errStr), nil
		}

		connected = true
		accountName = botName

		bundle.DiscordBotToken = &token
		writeBundle(bundle)
		cfg.DiscordEnabled = true
		if discordClientID != "" {
			cfg.DiscordClientID = discordClientID
		}
		if err := s.cfgStore.Save(cfg); err != nil {
			log.Printf("comms: ValidatePlatform: save config: %v", err)
		}

	case "slack":
		botToken := credentials["slackBot"]
		appToken := credentials["slackApp"]
		if botToken == "" {
			botToken = strVal(bundle.SlackBotToken)
		}
		if appToken == "" {
			appToken = strVal(bundle.SlackAppToken)
		}
		if botToken == "" {
			e := "No Slack bot token configured."
			return s.platformStatus(platform, cfg, bundle, false, nil, &e), nil
		}

		ok, workspaceName, err := validateSlack(botToken)
		if err != nil || !ok {
			errStr := "Slack validation failed."
			if err != nil {
				errStr = err.Error()
			}
			return s.platformStatus(platform, cfg, bundle, false, nil, &errStr), nil
		}

		connected = true
		accountName = workspaceName

		bundle.SlackBotToken = &botToken
		if appToken != "" {
			bundle.SlackAppToken = &appToken
		}
		writeBundle(bundle)
		cfg.SlackEnabled = true
		if err := s.cfgStore.Save(cfg); err != nil {
			log.Printf("comms: ValidatePlatform: save config: %v", err)
		}

	default:
		return PlatformStatus{}, fmt.Errorf("unknown platform: %s", platform)
	}

	return s.platformStatus(platform, cfg, bundle, connected, accountName, lastErr), nil
}

// ── Platform status builder ───────────────────────────────────────────────────

func (s *Service) platformStatus(
	platform string,
	cfg config.RuntimeConfigSnapshot,
	bundle credBundle,
	connected bool,
	accountName *string,
	lastErr *string,
) PlatformStatus {
	var (
		enabled        bool
		credConfigured bool
		requiredCreds  []string
		blockingReason *string
	)

	switch platform {
	case "telegram":
		enabled = cfg.TelegramEnabled
		credConfigured = strVal(bundle.TelegramBotToken) != ""
		requiredCreds = []string{"telegram_bot_token"}
		if !credConfigured {
			br := "Add a Telegram bot token to finish setup."
			blockingReason = &br
		}
	case "discord":
		enabled = cfg.DiscordEnabled
		credConfigured = strVal(bundle.DiscordBotToken) != "" && cfg.DiscordClientID != ""
		requiredCreds = []string{"discord_bot_token", "discord_client_id"}
		if !credConfigured {
			br := "Add a Discord bot token and client ID to finish setup."
			blockingReason = &br
		}
	case "slack":
		enabled = cfg.SlackEnabled
		credConfigured = strVal(bundle.SlackBotToken) != ""
		requiredCreds = []string{"slack_bot_token", "slack_app_token"}
		if !credConfigured {
			br := "Add a Slack bot token to finish setup."
			blockingReason = &br
		}
	}

	setupState := computeSetupState(enabled, credConfigured, connected, lastErr)
	statusLabel := computeStatusLabel(setupState, lastErr)

	if lastErr != nil {
		blockingReason = lastErr
	}

	return PlatformStatus{
		Platform:             platform,
		ID:                   platform,
		Enabled:              enabled,
		Connected:            connected,
		Available:            true, // all three platforms are always supported; credConfigured is a separate field
		SetupState:           setupState,
		StatusLabel:          statusLabel,
		ConnectedAccountName: accountName,
		CredentialConfigured: credConfigured,
		BlockingReason:       blockingReason,
		RequiredCredentials:  requiredCreds,
		LastError:            lastErr,
		LastUpdatedAt:        nil,
		Metadata:             map[string]string{},
	}
}

func computeSetupState(enabled, credConfigured, connected bool, lastErr *string) string {
	if connected {
		return "ready"
	}
	if !credConfigured {
		return "missing_credentials"
	}
	if lastErr != nil {
		return "validation_failed"
	}
	if !enabled {
		return "not_started"
	}
	return "partial_setup"
}

func computeStatusLabel(setupState string, lastErr *string) string {
	switch setupState {
	case "ready":
		return "Ready"
	case "missing_credentials":
		return "Missing Credentials"
	case "validation_failed":
		return "Validation Failed"
	case "not_started":
		return "Not Started"
	default:
		return "Needs Setup"
	}
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
