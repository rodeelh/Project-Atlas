package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/features"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/memory"
	"atlas-runtime-go/internal/mind"
	"atlas-runtime-go/internal/skills"
	"atlas-runtime-go/internal/storage"
)

// MessageAttachment is a file attached to a message. Data is raw base64 (no
// data-URL prefix). MimeType is e.g. "image/jpeg", "image/png", "application/pdf".
type MessageAttachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// MessageRequest is the JSON body expected by POST /message.
// Note: the web client sends "conversationId" (lowercase 'd') to match the
// contracts.ts interface, so the JSON tag uses the same casing.
type MessageRequest struct {
	Message        string              `json:"message"`
	ConversationID string              `json:"conversationId,omitempty"`
	Platform       string              `json:"platform,omitempty"`
	Attachments    []MessageAttachment `json:"attachments,omitempty"`
}

// MessageResponse is the JSON body returned by POST /message.
// Matches the contracts.ts MessageResponse interface.
type MessageResponse struct {
	Conversation struct {
		ID       string        `json:"id"`
		Messages []MessageItem `json:"messages"`
	} `json:"conversation"`
	Response struct {
		AssistantMessage string `json:"assistantMessage,omitempty"`
		Status           string `json:"status"`
		ErrorMessage     string `json:"errorMessage,omitempty"`
	} `json:"response"`
}

// MessageItem is a single message in a conversation.
type MessageItem struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// Service handles chat message processing: stores messages, calls the AI
// provider, emits SSE events, and returns the final MessageResponse.
// EngineAutoStarter is satisfied by engine.Manager. Defined here as a minimal
// interface to avoid a direct import of the engine package into chat.
type EngineAutoStarter interface {
	IsRunning() bool
	LoadedModel() string
	Start(modelName string, port int, ctxSize int, kvCacheQuant string) error
	WaitUntilReady(port int, timeout time.Duration) error
	RecordActivity()
}

type Service struct {
	db          *storage.DB
	cfgStore    *config.Store
	broadcaster *Broadcaster
	registry    *skills.Registry
	engine      EngineAutoStarter // optional — nil when Engine LM not in use
	routerEngine EngineAutoStarter // optional — nil when router not in use
}

// NewService returns a ready chat Service.
func NewService(db *storage.DB, cfgStore *config.Store, bc *Broadcaster, reg *skills.Registry) *Service {
	return &Service{db: db, cfgStore: cfgStore, broadcaster: bc, registry: reg}
}

// SetEngineManager wires in the primary Engine LM manager so the chat service
// can auto-start the model when a message arrives and the engine isn't running.
func (s *Service) SetEngineManager(e EngineAutoStarter) {
	s.engine = e
}

// SetRouterEngineManager wires in the tool-router Engine LM manager so the chat
// service can auto-start the router when tool selection mode is "llm".
func (s *Service) SetRouterEngineManager(e EngineAutoStarter) {
	s.routerEngine = e
}

// broadcasterEmitter adapts *Broadcaster to agent.Emitter.
// This avoids a circular import between agent ↔ chat.
type broadcasterEmitter struct {
	bc *Broadcaster
}

func (be *broadcasterEmitter) Emit(convID string, e agent.EmitEvent) {
	be.bc.Emit(convID, SSEEvent{
		Type:           e.Type,
		Content:        e.Content,
		Role:           e.Role,
		ConversationID: e.ConvID,
		ToolName:       e.ToolName,
		ToolCallID:     e.ToolCallID,
		ApprovalID:     e.ApprovalID,
		Arguments:      e.Arguments,
		Error:          e.Error,
		Status:         e.Status,
	})
}

func (be *broadcasterEmitter) Finish(convID string) {
	be.bc.Finish(convID)
}

// buildUserContent converts the message text and any attachments into the
// content value for the user OAIMessage.
//
//   - No attachments → plain string (no change to existing behaviour).
//   - Attachments present → []map[string]any content-parts array using the
//     OpenAI image_url format. Gemini's OpenAI-compat endpoint accepts the same
//     format. The Anthropic path in provider.go converts these parts when it
//     builds the Anthropic request.
//
// Images are embedded for cloud providers (OpenAI, Anthropic, Gemini) only.
// Call sites are responsible for handling LM Studio separately (degradation).
// PDFs are always embedded for all providers that accept them.
func buildUserContent(text string, attachments []MessageAttachment) any {
	if len(attachments) == 0 {
		return text
	}
	var parts []map[string]any
	if text != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	for _, a := range attachments {
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + a.MimeType + ";base64," + a.Data,
			},
		})
	}
	return parts
}

// hasImageAttachments reports whether any attachment is an image (not a PDF).
func hasImageAttachments(attachments []MessageAttachment) bool {
	for _, a := range attachments {
		if strings.HasPrefix(a.MimeType, "image/") {
			return true
		}
	}
	return false
}

// maxSystemPromptRunes is the total rune budget for the assembled system prompt.
// ~8000 runes ≈ 2000 tokens. When the total exceeds this budget, lower-priority
// blocks (diary, then skills, then memories) are trimmed. MIND.md identity is
// always included in full.
const maxSystemPromptRunes = 8000

// buildSystemPrompt assembles the system prompt for each agent turn with
// budget-aware allocation. Blocks are added in priority order; if the total
// exceeds maxSystemPromptRunes, lower-priority blocks are trimmed.
//
// Priority (highest first):
//  1. MIND.md content (identity, personality, user model)
//  2. Recalled memories (relevance-scored for current turn)
//  3. SKILLS.md context (matched routines)
//  4. Diary (last 3 days — trimmed first when over budget)
// mindAlwaysSections lists MIND.md section headers that are always injected.
// Operational sections that directly affect Atlas's behaviour every turn.
var mindAlwaysSections = map[string]bool{
	"## Who I Am":              true,
	"## What Matters Right Now": true,
	"## Working Style":         true,
	"## My Understanding of You": true,
	"## Today's Read":          true,
}

// mindContextualKeywords maps contextual section headers to trigger phrases.
// A contextual section is only injected when the user message matches at least
// one of its keywords. This keeps MIND.md lean for routine operational turns.
var mindContextualKeywords = map[string][]string{
	"## Our Story":             {"story", "history", "how long", "when did", "how we", "remember when"},
	"## Active Theories":       {"theory", "theor", "hypothesis", "pattern", "notice", "suspect"},
	"## What I'm Curious About": {"curious", "wonder", "question", "speculate"},
	"## Patterns I've Noticed": {"pattern", "habit", "tend to", "always", "notice", "recurring"},
}

// selectiveMindContent filters a full MIND.md to only the sections relevant
// for this turn. Always-sections are always included. Contextual sections are
// included only when the user message contains at least one trigger keyword.
// Returns the original content unmodified if parsing fails or no sections found.
func selectiveMindContent(content, userMessage string) string {
	lower := strings.ToLower(userMessage)
	lines := strings.Split(content, "\n")

	var out []string
	var currentHeader string
	var currentBody []string
	included := false

	flush := func() {
		if currentHeader == "" {
			return
		}
		if !included {
			return
		}
		out = append(out, currentHeader)
		out = append(out, currentBody...)
	}

	// Collect the pre-header title block (document header, metadata line).
	var titleLines []string
	i := 0
	for i < len(lines) {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			break
		}
		titleLines = append(titleLines, lines[i])
		i++
	}

	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			currentHeader = trimmed
			currentBody = nil
			if mindAlwaysSections[currentHeader] {
				included = true
			} else if kws, ok := mindContextualKeywords[currentHeader]; ok {
				included = false
				for _, kw := range kws {
					if strings.Contains(lower, kw) {
						included = true
						break
					}
				}
			} else {
				// Unknown section — include by default so new sections aren't silently dropped.
				included = true
			}
		} else if currentHeader != "" {
			currentBody = append(currentBody, lines[i])
		}
	}
	flush()

	if len(out) == 0 {
		return content // parsing produced nothing — return full content as safe fallback
	}

	result := strings.TrimRight(strings.Join(titleLines, "\n"), "\n")
	if len(out) > 0 {
		result += "\n\n" + strings.TrimSpace(strings.Join(out, "\n"))
	}
	return strings.TrimSpace(result)
}

func buildSystemPrompt(cfg config.RuntimeConfigSnapshot, db *storage.DB, supportDir, userMessage string) string {
	budget := maxSystemPromptRunes

	// Load MIND.md and apply selective section filtering.
	// Always-sections (Who I Am, Working Style, etc.) are always injected.
	// Contextual sections (Our Story, Active Theories, etc.) are included only
	// when the user message contains a relevant trigger keyword.
	base := cfg.BaseSystemPrompt
	if data, err := os.ReadFile(filepath.Join(supportDir, "MIND.md")); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			base = selectiveMindContent(s, userMessage)
		}
	}

	// Load optional blocks.
	skillsBlock := mind.SkillsContext(userMessage, supportDir)
	diary := features.DiaryContext(supportDir, 3)

	var mems []storage.MemoryRow
	limit := cfg.MaxRetrievedMemoriesPerTurn
	if limit > 0 {
		mems, _ = db.RelevantMemories(userMessage, limit)
	}

	// Build memories text.
	var memText string
	if len(mems) > 0 {
		var mb strings.Builder
		for _, m := range mems {
			mb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", m.Category, m.Title, m.Content))
		}
		memText = mb.String()

		// Track which memories were retrieved for reinforcement.
		ids := make([]string, len(mems))
		for i, m := range mems {
			ids[i] = m.ID
		}
		go db.UpdateLastRetrieved(ids)
	}

	// Calculate rune costs (including XML tags + separators).
	identityCost := len([]rune(base)) + 40   // <atlas_identity>\n...\n</atlas_identity>
	memCost := len([]rune(memText)) + 50      // \n\n<recalled_memories>\n...
	skillsCost := len([]rune(skillsBlock)) + 40
	diaryCost := len([]rune(diary)) + 35

	total := identityCost + memCost + skillsCost + diaryCost

	// Trim from lowest priority up until we're within budget.

	// Trim diary first (lowest priority).
	if total > budget && diary != "" {
		allowed := budget - (identityCost + memCost + skillsCost)
		if allowed < 100 {
			diary = ""
			diaryCost = 0
		} else {
			runes := []rune(diary)
			if len(runes) > allowed {
				diary = string(runes[:allowed])
				diaryCost = allowed + 35
			}
		}
		total = identityCost + memCost + skillsCost + diaryCost
	}

	// Trim skills next.
	if total > budget && skillsBlock != "" {
		allowed := budget - (identityCost + memCost + diaryCost)
		if allowed < 100 {
			skillsBlock = ""
			skillsCost = 0
		} else {
			runes := []rune(skillsBlock)
			if len(runes) > allowed {
				skillsBlock = string(runes[:allowed])
				skillsCost = allowed + 40
			}
		}
		total = identityCost + memCost + skillsCost + diaryCost
	}

	// Trim memories last (reduce count, don't truncate content mid-sentence).
	if total > budget && memText != "" {
		for len(mems) > 1 && total > budget {
			mems = mems[:len(mems)-1]
			var mb strings.Builder
			for _, m := range mems {
				mb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", m.Category, m.Title, m.Content))
			}
			memText = mb.String()
			memCost = len([]rune(memText)) + 50
			total = identityCost + memCost + skillsCost + diaryCost
		}
	}

	// Assemble final prompt.
	var sb strings.Builder
	sb.Grow(total + 100)

	sb.WriteString("<atlas_identity>\n")
	sb.WriteString(base)
	sb.WriteString("\n</atlas_identity>")

	if skillsBlock != "" {
		sb.WriteString("\n\n<skills_context>\n")
		sb.WriteString(skillsBlock)
		sb.WriteString("\n</skills_context>")
	}

	if diary != "" {
		sb.WriteString("\n\n<recent_diary>\n")
		sb.WriteString(diary)
		sb.WriteString("\n</recent_diary>")
	}

	if memText != "" {
		sb.WriteString("\n\n<recalled_memories>\n")
		sb.WriteString(memText)
		sb.WriteString("</recalled_memories>")
	}

	return sb.String()
}

// HandleMessage processes a message request end-to-end:
//  1. Resolves or creates the conversation.
//  2. Persists the user message.
//  3. Calls the AI provider via the agent loop.
//  4. Emits SSE events to the broadcaster.
//  5. Returns the final MessageResponse.
func (s *Service) HandleMessage(ctx context.Context, req MessageRequest) (MessageResponse, error) {
	cfg := s.cfgStore.Load()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Resolve conversation ID.
	convID := req.ConversationID
	if convID == "" {
		convID = newUUID()
	}

	// Ensure conversation exists.
	if err := s.db.SaveConversation(convID, now, now, nil); err != nil {
		return MessageResponse{}, fmt.Errorf("chat: save conversation: %w", err)
	}

	// Persist user message.
	userMsgID := newUUID()
	if err := s.db.SaveMessage(userMsgID, convID, "user", req.Message, now); err != nil {
		return MessageResponse{}, fmt.Errorf("chat: save user message: %w", err)
	}

	// Load conversation history for context window.
	history, err := s.db.ListMessages(convID)
	if err != nil {
		return MessageResponse{}, fmt.Errorf("chat: list messages: %w", err)
	}

	// Resolve primary provider config.
	provider, provErr := resolveProvider(cfg)
	if provErr != nil {
		errMsg := provErr.Error()
		logstore.Write("error", "Provider unavailable", map[string]string{"error": errMsg})
		s.broadcaster.Emit(convID, SSEEvent{
			Type:           "error",
			Error:          errMsg,
			ConversationID: convID,
		})
		s.broadcaster.Finish(convID)

		var resp MessageResponse
		resp.Conversation.ID = convID
		for _, m := range history {
			resp.Conversation.Messages = append(resp.Conversation.Messages, MessageItem{
				ID:        m.ID,
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: m.Timestamp,
			})
		}
		resp.Response.Status = "error"
		resp.Response.ErrorMessage = errMsg
		return resp, nil
	}

	// Auto-start Engine LM if the provider is atlas_engine and the model isn't loaded.
	// This covers the common restart-without-reload scenario: Atlas starts cold and the
	// user sends a message before manually loading the model in the Engine LM tab.
	if provider.Type == agent.ProviderAtlasEngine && s.engine != nil && !s.engine.IsRunning() {
		model := filepath.Base(cfg.SelectedAtlasEngineModel)
		if model == "" || model == "." {
			model = filepath.Base(cfg.SelectedAtlasEngineModelFast)
		}
		if model != "" && model != "." {
			port := cfg.AtlasEnginePort
			if port == 0 {
				port = 11985
			}
			ctxSize := cfg.AtlasEngineCtxSize
			if ctxSize <= 0 {
				ctxSize = 8192
			}
			logstore.Write("info", "Engine LM not running — auto-starting model", map[string]string{"model": model})
			if err := s.engine.Start(model, port, ctxSize, cfg.AtlasEngineKVCacheQuant); err != nil {
				logstore.Write("warn", "Engine LM auto-start failed", map[string]string{"error": err.Error()})
			} else if err := s.engine.WaitUntilReady(port, 90*time.Second); err != nil {
				logstore.Write("warn", "Engine LM ready-wait timed out", map[string]string{"error": err.Error()})
			}
		}
	}

	// Resolve heavy background provider for quality-sensitive background tasks
	// (memory extraction, MIND reflection, SKILLS learning). Defaults to the
	// cloud fast model; routes to Engine LM router only when AtlasEngineRouterForAll
	// is explicitly enabled. Falls back to the primary provider on any error.
	heavyBgProvider, heavyBgErr := resolveHeavyBackgroundProvider(cfg)
	if heavyBgErr != nil {
		heavyBgProvider = provider
	}

	// Local providers (LM Studio, Ollama, Engine LM) do not support image attachments.
	// Return a degradation message immediately without calling the model.
	// PDFs-only messages pass through since hasImageAttachments ignores PDFs.
	if (provider.Type == agent.ProviderLMStudio || provider.Type == agent.ProviderOllama || provider.Type == agent.ProviderAtlasEngine) && hasImageAttachments(req.Attachments) {
		const degradeMsg = "Vision is not available with local models. " +
			"Switch to OpenAI, Anthropic, or Gemini to analyse images."
		replyAt := time.Now().UTC().Format(time.RFC3339Nano)
		assistantMsgID := newUUID()
		_ = s.db.SaveMessage(assistantMsgID, convID, "assistant", degradeMsg, replyAt)
		s.broadcaster.Emit(convID, SSEEvent{Type: "token", Role: "assistant", ConversationID: convID})
		s.broadcaster.Emit(convID, SSEEvent{Type: "token", Content: degradeMsg, Role: "assistant", ConversationID: convID})
		s.broadcaster.Emit(convID, SSEEvent{Type: "done", Status: "completed", ConversationID: convID})
		s.broadcaster.Finish(convID)
		allMessages := make([]MessageItem, 0, len(history)+1)
		for _, m := range history {
			allMessages = append(allMessages, MessageItem{ID: m.ID, Role: m.Role, Content: m.Content, Timestamp: m.Timestamp})
		}
		allMessages = append(allMessages, MessageItem{ID: assistantMsgID, Role: "assistant", Content: degradeMsg, Timestamp: replyAt})
		var resp MessageResponse
		resp.Conversation.ID = convID
		resp.Conversation.Messages = allMessages
		resp.Response.AssistantMessage = degradeMsg
		resp.Response.Status = "complete"
		return resp, nil
	}

	// Build messages from history.
	// Default 15 (not 20) — 15 messages provides ample context while saving
	// ~500–1500 input tokens on active conversations.
	limit := cfg.ConversationWindowLimit
	if limit == 0 {
		limit = 15
	}

	systemPrompt := buildSystemPrompt(cfg, s.db, config.SupportDir(), req.Message)
	oaiMessages := []agent.OAIMessage{
		{Role: "system", Content: systemPrompt},
	}
	start := 0
	if len(history) > limit {
		start = len(history) - limit
	}

	// When older messages are trimmed, prepend a compact context note so the
	// model knows the conversation has prior history. This costs ~20-50 tokens
	// but prevents the model from treating the window as the full conversation.
	if start > 0 {
		var excerpts []string
		for _, m := range history[:start] {
			if m.Role == "user" && m.ID != userMsgID {
				exc := strings.Join(strings.Fields(m.Content), " ")
				if len([]rune(exc)) > 80 {
					exc = string([]rune(exc)[:80]) + "…"
				}
				excerpts = append(excerpts, exc)
			}
		}
		if len(excerpts) > 3 {
			excerpts = excerpts[len(excerpts)-3:]
		}
		if len(excerpts) > 0 {
			note := fmt.Sprintf("[%d earlier messages omitted. Recent topics: %s]",
				start, strings.Join(excerpts, " / "))
			oaiMessages = append(oaiMessages, agent.OAIMessage{Role: "user", Content: note})
			oaiMessages = append(oaiMessages, agent.OAIMessage{Role: "assistant", Content: "Understood."})
		}
	}

	// Replay history, skipping the current user message — it is appended below
	// with attachment content parts so raw base64 is never stored in SQLite.
	var historyChars int
	for _, m := range history[start:] {
		if m.ID == userMsgID {
			continue
		}
		if m.Role == "user" || m.Role == "assistant" {
			oaiMessages = append(oaiMessages, agent.OAIMessage{
				Role:    m.Role,
				Content: m.Content,
			})
			historyChars += len(m.Content)
		}
	}
	// Append current user message with attachment content parts.
	oaiMessages = append(oaiMessages, agent.OAIMessage{
		Role:    "user",
		Content: buildUserContent(req.Message, req.Attachments),
	})

	maxIter := cfg.MaxAgentIterations
	if maxIter <= 0 {
		maxIter = 5
	}
	if cfg.ActiveAIProvider == "lm_studio" && cfg.LMStudioMaxAgentIterations > 0 {
		maxIter = cfg.LMStudioMaxAgentIterations
	}
	if cfg.ActiveAIProvider == "ollama" && cfg.OllamaMaxAgentIterations > 0 {
		maxIter = cfg.OllamaMaxAgentIterations
	}
	if cfg.ActiveAIProvider == "atlas_engine" && cfg.AtlasEngineMaxAgentIterations > 0 {
		maxIter = cfg.AtlasEngineMaxAgentIterations
	}

	// Select tools based on the configured tool-selection mode.
	//   "off"       — always inject all tools (no filtering).
	//   "heuristic" — keyword/group heuristic; falls back to all tools when no
	//                 group matches. This was the original EnableSmartToolSelection=true
	//                 behaviour and remains the default.
	//   "llm"       — reserved for Phase 3 Tool Router (LLM-ranked selection);
	//                 falls back to heuristic until the router is wired in.
	//
	// Legacy: if ToolSelectionMode is empty, honour the old boolean field so
	// existing config files continue to behave as before the migration.
	toolMode := cfg.ToolSelectionMode
	if toolMode == "" {
		if cfg.EnableSmartToolSelection {
			toolMode = "heuristic"
		} else {
			toolMode = "off"
		}
	}
	var selectedTools []map[string]any
	switch toolMode {
	case "llm":
		// Auto-start the router model if it isn't running yet.
		if s.routerEngine != nil && !s.routerEngine.IsRunning() {
			routerModel := filepath.Base(cfg.AtlasEngineRouterModel)
			if routerModel != "" && routerModel != "." {
				port := cfg.AtlasEngineRouterPort
				if port == 0 {
					port = 11986
				}
				ctxSize := cfg.AtlasEngineCtxSize
				if ctxSize <= 0 {
					ctxSize = 8192
				}
				logstore.Write("info", "Tool router not running — auto-starting", map[string]string{"model": routerModel})
				if err := s.routerEngine.Start(routerModel, port, ctxSize, cfg.AtlasEngineKVCacheQuant); err != nil {
					logstore.Write("warn", "Router auto-start failed", map[string]string{"error": err.Error()})
				} else {
					_ = s.routerEngine.WaitUntilReady(port, 90*time.Second)
				}
			}
		}
		selectedTools = selectToolsWithLLM(ctx, cfg, req.Message, s.registry)
		if s.routerEngine != nil {
			s.routerEngine.RecordActivity()
		}
	case "heuristic":
		selectedTools = s.registry.SelectiveToolDefs(req.Message)
	// "off" → selectedTools stays nil → agent uses full tool list
	}

	loopCfg := agent.LoopConfig{
		Provider:      provider,
		MaxIterations: maxIter,
		SupportDir:    config.SupportDir(),
		ConvID:        convID,
		Tools:         selectedTools, // nil → loop uses full ToolDefinitions()
	}

	agentLoop := &agent.Loop{
		Skills: s.registry,
		BC:     &broadcasterEmitter{bc: s.broadcaster},
		DB:     s.db,
	}

	toolCount := len(selectedTools)
	if toolCount == 0 {
		toolCount = s.registry.ToolCount()
	}
	logstore.Write("info", fmt.Sprintf("Turn started: %s via %s (%d tools)", provider.Model, provider.Type, toolCount),
		map[string]string{"conv": convID[:8]})

	turnStart := time.Now()
	result := agentLoop.Run(ctx, loopCfg, oaiMessages, convID)

	// Reset idle timers after each turn so the model isn't ejected during active use.
	if provider.Type == agent.ProviderAtlasEngine && s.engine != nil {
		s.engine.RecordActivity()
	}

	var resp MessageResponse
	resp.Conversation.ID = convID

	switch result.Status {
	case "error":
		errMsg := "Agent loop error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		logstore.Write("error", "Turn error: "+errMsg,
			map[string]string{
				"conv":    convID[:8],
				"elapsed": fmt.Sprintf("%.1fs", time.Since(turnStart).Seconds()),
				"in":      fmt.Sprintf("%d", result.TotalUsage.InputTokens),
				"out":     fmt.Sprintf("%d", result.TotalUsage.OutputTokens),
			})
		s.broadcaster.Emit(convID, SSEEvent{
			Type:           "error",
			Error:          errMsg,
			ConversationID: convID,
		})
		s.broadcaster.Finish(convID)

		for _, m := range history {
			resp.Conversation.Messages = append(resp.Conversation.Messages, MessageItem{
				ID:        m.ID,
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: m.Timestamp,
			})
		}
		resp.Response.Status = "error"
		resp.Response.ErrorMessage = errMsg
		return resp, nil

	case "pendingApproval":
		// Emit the done event with waitingForApproval status so the web UI
		// enters awaitingResume mode. Do NOT call Finish() here — the channel
		// must stay open so Resume() can stream the continuation after approval.
		s.broadcaster.Emit(convID, SSEEvent{
			Type:           "done",
			ConversationID: convID,
			Status:         "waitingForApproval",
		})

		for _, m := range history {
			resp.Conversation.Messages = append(resp.Conversation.Messages, MessageItem{
				ID:        m.ID,
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: m.Timestamp,
			})
		}
		resp.Response.Status = "pendingApproval"
		return resp, nil

	default: // "complete"
		replyAt := time.Now().UTC().Format(time.RFC3339Nano)
		assistantText := result.FinalText

		// Persist assistant reply.
		assistantMsgID := newUUID()
		if err := s.db.SaveMessage(assistantMsgID, convID, "assistant", assistantText, replyAt); err != nil {
			return MessageResponse{}, fmt.Errorf("chat: save assistant message: %w", err)
		}

		logstore.Write("info", "Turn complete",
			map[string]string{
				"conv":     convID[:8],
				"elapsed":  fmt.Sprintf("%.1fs", time.Since(turnStart).Seconds()),
				"in":       fmt.Sprintf("%d", result.TotalUsage.InputTokens),
				"out":      fmt.Sprintf("%d", result.TotalUsage.OutputTokens),
				"sys_est":  fmt.Sprintf("~%d", len(systemPrompt)/4),
				"hist_est": fmt.Sprintf("~%d", historyChars/4),
			})

		// Post-turn background tasks use a detached context so they are not
		// canceled when the HTTP request context closes after the response is sent.
		bgCtx := context.WithoutCancel(ctx)

		// Post-turn memory extraction (non-blocking).
		// Passes provider + assistant text for LLM-based extraction alongside regex.
		go memory.ExtractAndPersist(bgCtx, cfg, heavyBgProvider, req.Message, assistantText,
			result.ToolCallSummaries, convID, s.db)

		// Post-turn MIND reflection and DIARY entry (non-blocking).
		// Skip if the assistant produced no text — a pure tool-call turn with
		// no narrative would produce a meaningless Today's Read.
		if assistantText != "" {
			turn := mind.TurnRecord{
				ConversationID:      convID,
				UserMessage:         req.Message,
				AssistantResponse:   assistantText,
				ToolCallSummaries:   result.ToolCallSummaries,
				ToolResultSummaries: result.ToolResultSummaries,
				Timestamp:           time.Now(),
			}
			mind.ReflectNonBlocking(heavyBgProvider, turn, config.SupportDir())
			mind.LearnFromTurnNonBlocking(heavyBgProvider, turn, config.SupportDir())
		}

		// Emit done event with status="completed" so the web UI can trigger
		// post-turn work (e.g. link preview fetching) gated on this status.
		s.broadcaster.Emit(convID, SSEEvent{
			Type:           "done",
			Status:         "completed",
			ConversationID: convID,
		})
		s.broadcaster.Finish(convID)

		// Build the full response message list.
		allMessages := make([]MessageItem, 0, len(history)+1)
		for _, m := range history {
			allMessages = append(allMessages, MessageItem{
				ID:        m.ID,
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: m.Timestamp,
			})
		}
		allMessages = append(allMessages, MessageItem{
			ID:        assistantMsgID,
			Role:      "assistant",
			Content:   assistantText,
			Timestamp: replyAt,
		})

		resp.Conversation.Messages = allMessages
		resp.Response.AssistantMessage = assistantText
		resp.Response.Status = "complete"
		return resp, nil
	}
}

// RegenerateMind builds a context-aware prompt from the current MIND.md,
// recent conversation history, and active memories, then asks the AI to
// produce an updated MIND.md. The old file is backed up to MIND.md.bak
// before the new content is written atomically.
func (s *Service) RegenerateMind(ctx context.Context) (string, error) {
	cfg := s.cfgStore.Load()
	provider, err := resolveProvider(cfg)
	if err != nil {
		return "", err
	}

	supportDir := config.SupportDir()

	// Read the current MIND.md.
	existing := ""
	if data, readErr := os.ReadFile(filepath.Join(supportDir, "MIND.md")); readErr == nil {
		existing = strings.TrimSpace(string(data))
	}

	// Read recent memories for grounding.
	memBlock := ""
	if mems, memErr := s.db.ListMemories(20, ""); memErr == nil && len(mems) > 0 {
		var sb strings.Builder
		sb.WriteString("## Current Memories\n")
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", m.Category, m.Title, m.Content))
		}
		memBlock = sb.String()
	}

	// Read recent conversation messages for context (last 30 across all conversations).
	convBlock := ""
	if convs, convErr := s.db.ListConversations(5); convErr == nil && len(convs) > 0 {
		var sb strings.Builder
		sb.WriteString("## Recent Conversation Excerpts\n")
		for _, conv := range convs {
			msgs, msgErr := s.db.ListMessages(conv.ID)
			if msgErr != nil || len(msgs) == 0 {
				continue
			}
			// Include last 6 messages per conversation.
			start := 0
			if len(msgs) > 6 {
				start = len(msgs) - 6
			}
			for _, msg := range msgs[start:] {
				role := msg.Role
				content := msg.Content
				if content == "" {
					continue
				}
				if len(content) > 300 {
					content = content[:300] + "…"
				}
				sb.WriteString(fmt.Sprintf("[%s] %s\n", role, content))
			}
			sb.WriteString("\n")
		}
		convBlock = sb.String()
	}

	// Build the upgrade prompt.
	var promptBuilder strings.Builder
	promptBuilder.WriteString("You are updating the Atlas operator MIND.md — the self-model and system prompt for an AI operator named Atlas.\n\n")
	promptBuilder.WriteString("Your job: rewrite MIND.md so it accurately reflects the current state of the user's project and working relationship. ")
	promptBuilder.WriteString("Keep what is still true, remove what is stale, and add what the conversation history reveals.\n\n")
	promptBuilder.WriteString("Rules:\n")
	promptBuilder.WriteString("- Output only the updated MIND.md content — no explanations, no markdown fences.\n")
	promptBuilder.WriteString("- Preserve the section structure (Who I Am, What Matters Right Now, Working Style, My Understanding of the User, Patterns I've Noticed, Active Theories, Our Story).\n")
	promptBuilder.WriteString("- Replace stale entries with observations grounded in the conversation history below.\n")
	promptBuilder.WriteString("- 'Today's Read' should reflect the most recent session, not old notes.\n\n")
	if existing != "" {
		promptBuilder.WriteString("---\n## Current MIND.md\n")
		promptBuilder.WriteString(existing)
		promptBuilder.WriteString("\n\n")
	}
	if memBlock != "" {
		promptBuilder.WriteString("---\n")
		promptBuilder.WriteString(memBlock)
		promptBuilder.WriteString("\n")
	}
	if convBlock != "" {
		promptBuilder.WriteString("---\n")
		promptBuilder.WriteString(convBlock)
	}

	messages := []agent.OAIMessage{
		{Role: "user", Content: promptBuilder.String()},
	}

	reply, _, _, err := agent.CallAINonStreamingExported(ctx, provider, messages, nil)
	if err != nil {
		return "", fmt.Errorf("mind regeneration: %w", err)
	}

	contentStr, _ := reply.Content.(string)
	content := strings.TrimSpace(contentStr)
	if content == "" {
		return "", fmt.Errorf("mind regeneration: AI returned empty content")
	}

	if err := os.MkdirAll(supportDir, 0o700); err != nil {
		return "", err
	}

	mindPath := filepath.Join(supportDir, "MIND.md")
	bakPath := filepath.Join(supportDir, "MIND.md.bak")

	// Back up existing file before overwriting.
	if existing != "" {
		_ = os.WriteFile(bakPath, []byte(existing), 0o600)
	}

	// Write atomically via temp → rename.
	tmp, err := os.CreateTemp(supportDir, "MIND-*.md.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	tmp.Chmod(0o600) //nolint:errcheck
	tmp.Close()
	if err := os.Rename(tmpPath, mindPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	return content, nil
}

// ResolveProvider returns the active AI ProviderConfig from config + Keychain.
// Exported so that internal packages (e.g. forge) can reuse provider resolution
// without duplicating Keychain reading logic.
func (s *Service) ResolveProvider() (agent.ProviderConfig, error) {
	cfg := s.cfgStore.Load()
	return resolveProvider(cfg)
}

// ResolveFastProvider returns the fast-model ProviderConfig from config + Keychain.
// Used by background pipelines (forge research, MIND reflection, SKILLS learning).
// Falls back to the primary provider when no fast model is configured.
func (s *Service) ResolveFastProvider() (agent.ProviderConfig, error) {
	cfg := s.cfgStore.Load()
	return resolveFastProvider(cfg)
}

// Resume is called after an approval is resolved. It loads the deferred
// execution from the DB, executes or denies the tool call, and continues
// the agent loop to completion.
func (s *Service) Resume(toolCallID string, approved bool) {
	ctx := context.Background()

	row, err := s.db.FetchDeferredByToolCallID(toolCallID)
	if err != nil || row == nil {
		return
	}

	// Parse the saved deferral state.
	var state struct {
		Messages  []agent.OAIMessage  `json:"messages"`
		ToolCalls []agent.OAIToolCall `json:"tool_calls"`
		ConvID    string              `json:"conv_id"`
	}
	if err := json.Unmarshal([]byte(row.NormalizedInputJSON), &state); err != nil {
		return
	}

	convID := state.ConvID
	if convID == "" && row.ConversationID != nil {
		convID = *row.ConversationID
	}

	// Find the tool call in the saved state.
	var targetTC *agent.OAIToolCall
	for i := range state.ToolCalls {
		if state.ToolCalls[i].ID == toolCallID {
			targetTC = &state.ToolCalls[i]
			break
		}
	}
	if targetTC == nil {
		return
	}

	// Build the tool result message.
	var toolResult string
	if approved {
		toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, execErr := s.registry.Execute(toolCtx, targetTC.Function.Name, json.RawMessage(targetTC.Function.Arguments))
		cancel()
		if execErr != nil {
			toolResult = fmt.Sprintf("Tool execution error: %v", execErr)
		} else {
			toolResult = result.FormatForModel()
		}
	} else {
		toolResult = "Action denied by user."
	}

	// Add tool result to messages.
	messages := append(state.Messages, agent.OAIMessage{
		Role:       "tool",
		Content:    toolResult,
		ToolCallID: toolCallID,
		Name:       targetTC.Function.Name,
	})

	// Also handle other tool calls that were deferred at the same time.
	// Check for other pending approvals in this conversation.
	pending, _ := s.db.FetchDeferredsByConversationID(convID, "pending_approval")
	for _, p := range pending {
		if p.ToolCallID == toolCallID {
			continue // already handled
		}
		// Other tool calls from the same batch — add denied result.
		actionID := ""
		if p.ActionID != nil {
			actionID = *p.ActionID
		}
		messages = append(messages, agent.OAIMessage{
			Role:       "tool",
			Content:    "Action deferred (separate approval required).",
			ToolCallID: p.ToolCallID,
			Name:       actionID,
		})
	}

	cfg := s.cfgStore.Load()
	provider, provErr := resolveProvider(cfg)
	if provErr != nil {
		return
	}

	maxIter := cfg.MaxAgentIterations
	if maxIter <= 0 {
		maxIter = 5
	}
	if cfg.ActiveAIProvider == "lm_studio" && cfg.LMStudioMaxAgentIterations > 0 {
		maxIter = cfg.LMStudioMaxAgentIterations
	}
	if cfg.ActiveAIProvider == "ollama" && cfg.OllamaMaxAgentIterations > 0 {
		maxIter = cfg.OllamaMaxAgentIterations
	}
	if cfg.ActiveAIProvider == "atlas_engine" && cfg.AtlasEngineMaxAgentIterations > 0 {
		maxIter = cfg.AtlasEngineMaxAgentIterations
	}

	loopCfg := agent.LoopConfig{
		Provider:      provider,
		MaxIterations: maxIter,
		SupportDir:    config.SupportDir(),
		ConvID:        convID,
	}

	agentLoop := &agent.Loop{
		Skills: s.registry,
		BC:     &broadcasterEmitter{bc: s.broadcaster},
		DB:     s.db,
	}

	// Emit assistant_started so the web UI opens a new bubble for the resumed turn.
	s.broadcaster.Emit(convID, SSEEvent{
		Type:           "assistant_started",
		ConversationID: convID,
	})

	resumeStart := time.Now()
	result := agentLoop.Run(ctx, loopCfg, messages, convID)

	if result.Status == "complete" && result.FinalText != "" {
		replyAt := time.Now().UTC().Format(time.RFC3339Nano)
		assistantMsgID := newUUID()
		if err := s.db.SaveMessage(assistantMsgID, convID, "assistant", result.FinalText, replyAt); err != nil {
			logstore.Write("warn", "Resume: failed to persist assistant message: "+err.Error(),
				map[string]string{"conv": convID})
		}

		logstore.Write("info", "Resume complete",
			map[string]string{
				"conv":    convID[:8],
				"elapsed": fmt.Sprintf("%.1fs", time.Since(resumeStart).Seconds()),
				"in":      fmt.Sprintf("%d", result.TotalUsage.InputTokens),
				"out":     fmt.Sprintf("%d", result.TotalUsage.OutputTokens),
			})

		s.broadcaster.Emit(convID, SSEEvent{
			Type:           "done",
			Status:         "completed",
			ConversationID: convID,
		})
		s.broadcaster.Finish(convID)
	}
}
