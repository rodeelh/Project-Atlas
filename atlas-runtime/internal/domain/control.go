package domain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/engine"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/runtime"
	"atlas-runtime-go/internal/storage"
)

// ControlDomain handles runtime control and configuration routes.
//
// Routes owned:
//
//	GET    /status
//	GET    /logs
//	GET    /config
//	PUT    /config
//	GET    /onboarding
//	PUT    /onboarding
//	GET    /models
//	GET    /models/available
//	POST   /models/refresh
//	GET    /api-keys
//	POST   /api-keys
//	POST   /api-keys/invalidate-cache
//	DELETE /api-keys
//	GET    /link-preview
type ControlDomain struct {
	cfgStore   *config.Store
	runtimeSvc *runtime.Service
	db         *storage.DB
	engineMgr  *engine.Manager // for atlas_engine model list in model selector
}

// NewControlDomain creates a ControlDomain.
func NewControlDomain(cfgStore *config.Store, runtimeSvc *runtime.Service, db *storage.DB, mgr *engine.Manager) *ControlDomain {
	d := &ControlDomain{cfgStore: cfgStore, runtimeSvc: runtimeSvc, db: db, engineMgr: mgr}
	d.migrateBundleCustomKeys()
	return d
}

// migrateBundleCustomKeys moves keys that were previously stored under
// customSecrets (because of a provider-ID mismatch) into their proper
// top-level fields.  Safe to run every startup — no-ops if already clean.
func (d *ControlDomain) migrateBundleCustomKeys() {
	m, ok := readRawBundle()
	if !ok {
		return // can't read bundle — skip migration
	}
	customs, _ := m["customSecrets"].(map[string]interface{})
	if customs == nil {
		return
	}
	changed := false

	// braveSearch → braveSearchAPIKey
	if v, ok := customs["braveSearch"].(string); ok && v != "" {
		existing, _ := m["braveSearchAPIKey"].(string)
		if existing == "" {
			m["braveSearchAPIKey"] = v
			delete(customs, "braveSearch")
			changed = true
		}
	}
	// finnhub → finnhubAPIKey
	if v, ok := customs["finnhub"].(string); ok && v != "" {
		existing, _ := m["finnhubAPIKey"].(string)
		if existing == "" {
			m["finnhubAPIKey"] = v
			delete(customs, "finnhub")
			changed = true
		}
	}
	// slackBot → slackBotToken (web UI provider ID mismatch)
	if v, ok := customs["slackBot"].(string); ok && v != "" {
		existing, _ := m["slackBotToken"].(string)
		if existing == "" {
			m["slackBotToken"] = v
			delete(customs, "slackBot")
			changed = true
		}
	}

	if changed {
		m["customSecrets"] = customs
		_ = writeRawBundle(m)
	}
}

// Register mounts all control routes on the given router.
func (d *ControlDomain) Register(r chi.Router) {
	r.Get("/status", d.getStatus)
	r.Get("/logs", d.getLogs)
	r.Get("/config", d.getConfig)
	r.Put("/config", d.putConfig)
	r.Get("/onboarding", d.getOnboarding)
	r.Put("/onboarding", d.putOnboarding)
	r.Get("/models", d.getModels)
	r.Get("/models/available", d.getModelsAvailable)
	r.Post("/models/refresh", d.postModelsRefresh)
	r.Get("/api-keys", d.getAPIKeys)
	r.Post("/api-keys", d.postAPIKeys)
	r.Post("/api-keys/invalidate-cache", d.postAPIKeysInvalidateCache)
	r.Delete("/api-keys", d.deleteAPIKeys)
	r.Get("/link-preview", d.getLinkPreview)
}

// ── Status ────────────────────────────────────────────────────────────────────

func (d *ControlDomain) getStatus(w http.ResponseWriter, r *http.Request) {
	convCount := d.db.CountConversations()
	tokensIn, tokensOut := agent.GetSessionTokens()
	status := d.runtimeSvc.GetStatus(convCount, tokensIn, tokensOut)
	status.PendingApprovalCount = d.db.CountPendingApprovals()
	writeJSON(w, http.StatusOK, status)
}

func (d *ControlDomain) getLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 200
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}
	entries := logstore.Global().Entries(limit)
	if entries == nil {
		entries = []logstore.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// ── Config ────────────────────────────────────────────────────────────────────

func (d *ControlDomain) getConfig(w http.ResponseWriter, r *http.Request) {
	snap := d.cfgStore.Load()
	writeJSON(w, http.StatusOK, snap)
}

func (d *ControlDomain) putConfig(w http.ResponseWriter, r *http.Request) {
	prev := d.cfgStore.Load()

	// Start from the existing config so partial updates (sending only a few
	// fields) don't zero out everything else.
	next := prev
	if !decodeJSON(w, r, &next) {
		return
	}

	// Clamp multi-agent bounds (same as Swift).
	if next.MaxParallelAgents < 2 {
		next.MaxParallelAgents = 2
	}
	if next.MaxParallelAgents > 5 {
		next.MaxParallelAgents = 5
	}
	if next.WorkerMaxIterations < 1 {
		next.WorkerMaxIterations = 1
	}
	if next.WorkerMaxIterations > 10 {
		next.WorkerMaxIterations = 10
	}

	restartRequired := next.RuntimePort != prev.RuntimePort

	if err := d.cfgStore.Save(next); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":          next,
		"restartRequired": restartRequired,
	})
}

// ── Onboarding ────────────────────────────────────────────────────────────────

type onboardingStatus struct {
	Completed bool `json:"completed"`
}

func (d *ControlDomain) getOnboarding(w http.ResponseWriter, r *http.Request) {
	snap := d.cfgStore.Load()
	writeJSON(w, http.StatusOK, onboardingStatus{Completed: snap.OnboardingCompleted})
}

func (d *ControlDomain) putOnboarding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Completed bool `json:"completed"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	snap := d.cfgStore.Load()
	snap.OnboardingCompleted = req.Completed
	if err := d.cfgStore.Save(snap); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, onboardingStatus{Completed: req.Completed})
}

// ── Models ────────────────────────────────────────────────────────────────────

func (d *ControlDomain) getModels(w http.ResponseWriter, r *http.Request) {
	snap := d.cfgStore.Load()
	writeJSON(w, http.StatusOK, map[string]any{
		"activeAIProvider":            snap.ActiveAIProvider,
		"selectedOpenAIPrimaryModel": snap.SelectedOpenAIPrimaryModel,
		"selectedOpenAIFastModel":    snap.SelectedOpenAIFastModel,
		"selectedAnthropicModel":     snap.SelectedAnthropicModel,
		"selectedAnthropicFastModel": snap.SelectedAnthropicFastModel,
		"selectedGeminiModel":        snap.SelectedGeminiModel,
		"selectedGeminiFastModel":    snap.SelectedGeminiFastModel,
		"selectedLMStudioModel":      snap.SelectedLMStudioModel,
		"selectedLMStudioModelFast":  snap.SelectedLMStudioModelFast,
		"selectedOllamaModel":        snap.SelectedOllamaModel,
		"selectedOllamaModelFast":    snap.SelectedOllamaModelFast,
		"selectedAtlasEngineModel":     snap.SelectedAtlasEngineModel,
		"selectedAtlasEngineModelFast": snap.SelectedAtlasEngineModelFast,
		"lastRefreshed":              nil,
	})
}

func (d *ControlDomain) getModelsAvailable(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	cfg := d.cfgStore.Load()
	bundle, _ := readRawBundle()

	result := d.fetchModelsForProvider(provider, cfg, bundle)
	writeJSON(w, http.StatusOK, result)
}

func (d *ControlDomain) postModelsRefresh(w http.ResponseWriter, r *http.Request) {
	// Re-fetch available models for the active provider and return them.
	cfg := d.cfgStore.Load()
	bundle, _ := readRawBundle()
	result := d.fetchModelsForProvider(cfg.ActiveAIProvider, cfg, bundle)
	writeJSON(w, http.StatusOK, result)
}

// ── Model fetching ─────────────────────────────────────────────────────────────

// modelRecord matches the web UI's AIModelRecord TypeScript interface.
type modelRecord struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	IsFast      bool   `json:"isFast"`
}

// fetchModelsForProvider fetches the available model list for the given provider,
// returning a ModelSelectorInfo-shaped map for the web UI.
//
// The resolved primaryModel and fastModel values mirror the fallback chain in
// chat/keychain.go resolveProvider() exactly, so the UI always shows the model
// that will actually be used — even when the user hasn't made an explicit selection.
func (d *ControlDomain) fetchModelsForProvider(provider string, cfg config.RuntimeConfigSnapshot, bundle map[string]interface{}) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)

	switch provider {
	case "openai":
		// Mirror resolveProvider: DefaultOpenAIModel → SelectedOpenAIPrimaryModel → hard default.
		primary := cfg.DefaultOpenAIModel
		if cfg.SelectedOpenAIPrimaryModel != "" {
			primary = cfg.SelectedOpenAIPrimaryModel
		}
		if primary == "" {
			primary = "gpt-4.1-mini"
		}
		fast := cfg.SelectedOpenAIFastModel
		if fast == "" {
			fast = "gpt-4.1-mini"
		}
		apiKey, _ := bundle["openAIAPIKey"].(string)
		models := fetchOpenAIModels(apiKey)
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	case "anthropic":
		primary := cfg.SelectedAnthropicModel
		if primary == "" {
			primary = "claude-haiku-4-5-20251001"
		}
		fast := cfg.SelectedAnthropicFastModel
		if fast == "" {
			fast = "claude-haiku-4-5-20251001"
		}
		apiKey, _ := bundle["anthropicAPIKey"].(string)
		models := fetchAnthropicModels(apiKey)
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	case "gemini":
		primary := cfg.SelectedGeminiModel
		if primary == "" {
			primary = "gemini-2.5-flash"
		}
		fast := cfg.SelectedGeminiFastModel
		if fast == "" {
			fast = "gemini-2.5-flash"
		}
		apiKey, _ := bundle["geminiAPIKey"].(string)
		models := fetchGeminiModels(apiKey)
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	case "lm_studio":
		primary := cfg.SelectedLMStudioModel
		if primary == "" {
			primary = "local-model"
		}
		fast := cfg.SelectedLMStudioModelFast
		if fast == "" {
			fast = primary
		}
		apiKey, _ := bundle["lmStudioAPIKey"].(string)
		baseURL := cfg.LMStudioBaseURL
		if baseURL == "" {
			baseURL = "http://localhost:1234"
		}
		models := fetchLMStudioModels(baseURL, apiKey)
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	case "ollama":
		primary := cfg.SelectedOllamaModel
		if primary == "" {
			primary = "llama3.2"
		}
		fast := cfg.SelectedOllamaModelFast
		if fast == "" {
			fast = primary
		}
		apiKey, _ := bundle["ollamaAPIKey"].(string)
		baseURL := cfg.OllamaBaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		models := fetchOllamaModels(baseURL, apiKey)
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	case "atlas_engine":
		// Normalize to basename — old config values may store full paths.
		primary := filepath.Base(cfg.SelectedAtlasEngineModel)
		if primary == "" || primary == "." {
			primary = ""
		}
		fast := filepath.Base(cfg.SelectedAtlasEngineModelFast)
		if fast == "" || fast == "." {
			fast = primary
		}
		// List all downloaded .gguf files — not just the currently loaded model.
		var models []modelRecord
		if d.engineMgr != nil {
			if infos, err := d.engineMgr.ListModels(); err == nil {
				for _, m := range infos {
					models = append(models, modelRecord{
						ID:          m.Name,
						DisplayName: m.Name,
						IsFast:      false,
					})
				}
			}
		}
		if models == nil {
			models = []modelRecord{}
		}
		return map[string]any{
			"primaryModel":    primary,
			"fastModel":       fast,
			"lastRefreshedAt": now,
			"availableModels": models,
		}

	default:
		return map[string]any{"availableModels": []modelRecord{}}
	}
}

// fetchOpenAIModels fetches the model list from OpenAI and returns a curated set.
func fetchOpenAIModels(apiKey string) []modelRecord {
	if apiKey == "" {
		return curatedOpenAIModels()
	}
	req, err := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return curatedOpenAIModels()
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return curatedOpenAIModels()
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return curatedOpenAIModels()
	}

	// Non-chat variant keywords — any model containing these is excluded.
	skipKeywords := []string{"audio", "realtime", "tts", "whisper", "transcrib", "search", "embed", "instruct"}
	// Fast/cheap variant keywords.
	fastKeywords := []string{"mini", "nano", "lite"}

	// Pass 1: collect chat-capable models (gpt-4.x or o-series).
	type entry struct {
		id   string
		base string // ID with date suffix stripped — used as dedup key
	}
	var candidates []entry
	seen := map[string]bool{}
	for _, m := range result.Data {
		id := m.ID
		if seen[id] {
			continue
		}
		lower := strings.ToLower(id)
		// Must start with a chat-capable family prefix (gpt-4, gpt-5, o-series …).
		if !strings.HasPrefix(lower, "gpt-") && !strings.HasPrefix(lower, "o") {
			continue
		}
		// Skip non-chat variants.
		skip := false
		for _, kw := range skipKeywords {
			if strings.Contains(lower, kw) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		seen[id] = true
		candidates = append(candidates, entry{id: id, base: openAIBaseFamily(id)})
	}

	// Pass 2: per base-family keep the shortest (canonical non-dated) ID.
	familyBest := map[string]string{} // base → best id
	for _, c := range candidates {
		prev, ok := familyBest[c.base]
		if !ok || len(c.id) < len(prev) {
			familyBest[c.base] = c.id
		}
	}

	// Pass 3: sort base families by model score (newest/most capable first).
	bases := make([]string, 0, len(familyBest))
	for b := range familyBest {
		bases = append(bases, b)
	}
	sort.Slice(bases, func(i, j int) bool {
		return openAIModelScore(bases[i]) > openAIModelScore(bases[j])
	})

	var models []modelRecord
	for _, base := range bases {
		id := familyBest[base]
		lower := strings.ToLower(id)
		isFast := false
		for _, kw := range fastKeywords {
			if strings.Contains(lower, kw) {
				isFast = true
				break
			}
		}
		models = append(models, modelRecord{
			ID:          id,
			DisplayName: openAIDisplayName(id),
			IsFast:      isFast,
		})
	}
	top := topFastAndPrimary(models, 5)
	if len(top) == 0 {
		return curatedOpenAIModels()
	}
	return top
}

func curatedOpenAIModels() []modelRecord {
	return []modelRecord{
		// Primary
		{ID: "gpt-4.1", DisplayName: "GPT-4.1", IsFast: false},
		{ID: "gpt-4o", DisplayName: "GPT-4o", IsFast: false},
		{ID: "o3", DisplayName: "O3", IsFast: false},
		{ID: "o4", DisplayName: "O4", IsFast: false},
		{ID: "gpt-4-turbo", DisplayName: "GPT-4 Turbo", IsFast: false},
		// Fast
		{ID: "gpt-4.1-mini", DisplayName: "GPT-4.1 Mini", IsFast: true},
		{ID: "gpt-4o-mini", DisplayName: "GPT-4o Mini", IsFast: true},
		{ID: "o4-mini", DisplayName: "O4 Mini", IsFast: true},
		{ID: "o3-mini", DisplayName: "O3 Mini", IsFast: true},
		{ID: "gpt-4.1-nano", DisplayName: "GPT-4.1 Nano", IsFast: true},
	}
}

// openAIBaseFamily strips trailing date suffixes (e.g. "-2024-11-20", "-20241120")
// so that dated and undated variants of the same model are grouped together.
func openAIBaseFamily(id string) string {
	// Match an 8-digit date suffix like -20241120 or a yyyy-mm-dd suffix like -2024-11-20.
	re := regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$|-\d{8}$`)
	return re.ReplaceAllString(id, "")
}

func openAIDisplayName(id string) string {
	replacer := strings.NewReplacer(
		"gpt-4.1-mini", "GPT-4.1 Mini",
		"gpt-4.1-nano", "GPT-4.1 Nano",
		"gpt-4.1", "GPT-4.1",
		"gpt-4o-mini", "GPT-4o Mini",
		"gpt-4o", "GPT-4o",
		"gpt-4-turbo", "GPT-4 Turbo",
		"gpt-4", "GPT-4",
		"o3-mini", "O3 Mini",
		"o3", "O3",
		"o1-mini", "O1 Mini",
		"o1-preview", "O1 Preview",
		"o1", "O1",
		"o4-mini", "O4 Mini",
	)
	// Try exact prefix match for a clean display name.
	for _, pair := range [][2]string{
		{"gpt-4.1-mini", "GPT-4.1 Mini"}, {"gpt-4.1-nano", "GPT-4.1 Nano"}, {"gpt-4.1", "GPT-4.1"},
		{"gpt-4o-mini", "GPT-4o Mini"}, {"gpt-4o", "GPT-4o"}, {"gpt-4-turbo", "GPT-4 Turbo"}, {"gpt-4", "GPT-4"},
		{"o4-mini", "O4 Mini"}, {"o3-mini", "O3 Mini"}, {"o3", "O3"},
		{"o1-mini", "O1 Mini"}, {"o1-preview", "O1 Preview"}, {"o1", "O1"},
	} {
		if strings.HasPrefix(id, pair[0]) {
			suffix := strings.TrimPrefix(id, pair[0])
			if suffix == "" {
				return pair[1]
			}
			return pair[1] + " (" + strings.TrimPrefix(suffix, "-") + ")"
		}
	}
	_ = replacer
	return id
}

// fetchAnthropicModels fetches the model list from Anthropic's /v1/models endpoint.
func fetchAnthropicModels(apiKey string) []modelRecord {
	if apiKey == "" {
		return curatedAnthropicModels()
	}
	req, err := http.NewRequest("GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return curatedAnthropicModels()
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return curatedAnthropicModels()
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		return curatedAnthropicModels()
	}

	fastKeywords := []string{"haiku", "flash"}
	var models []modelRecord
	for _, m := range result.Data {
		name := m.DisplayName
		if name == "" {
			name = anthropicDisplayName(m.ID)
		}
		isFast := false
		lower := strings.ToLower(m.ID)
		for _, kw := range fastKeywords {
			if strings.Contains(lower, kw) {
				isFast = true
				break
			}
		}
		models = append(models, modelRecord{ID: m.ID, DisplayName: name, IsFast: isFast})
	}
	return topFastAndPrimary(models, 5)
}

func curatedAnthropicModels() []modelRecord {
	return []modelRecord{
		// Primary
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", IsFast: false},
		{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", IsFast: false},
		{ID: "claude-sonnet-4-5-20250929", DisplayName: "Claude Sonnet 4.5", IsFast: false},
		{ID: "claude-opus-4-5-20251101", DisplayName: "Claude Opus 4.5", IsFast: false},
		{ID: "claude-opus-4-1-20250805", DisplayName: "Claude Opus 4.1", IsFast: false},
		// Fast
		{ID: "claude-haiku-4-5-20251001", DisplayName: "Claude Haiku 4.5", IsFast: true},
		{ID: "claude-haiku-4-6", DisplayName: "Claude Haiku 4.6", IsFast: true},
		{ID: "claude-3-5-haiku-20241022", DisplayName: "Claude 3.5 Haiku", IsFast: true},
		{ID: "claude-3-5-sonnet-20241022", DisplayName: "Claude 3.5 Sonnet", IsFast: false},
		{ID: "claude-3-haiku-20240307", DisplayName: "Claude 3 Haiku", IsFast: true},
	}
}

func anthropicDisplayName(id string) string {
	for _, pair := range [][2]string{
		{"claude-sonnet-4-6", "Claude Sonnet 4.6"},
		{"claude-opus-4-6", "Claude Opus 4.6"},
		{"claude-haiku-4-6", "Claude Haiku 4.6"},
		{"claude-haiku-4-5", "Claude Haiku 4.5"},
		{"claude-sonnet-4-5", "Claude Sonnet 4.5"},
		{"claude-opus-4-5", "Claude Opus 4.5"},
		{"claude-sonnet-4-1", "Claude Sonnet 4.1"},
		{"claude-opus-4-1", "Claude Opus 4.1"},
		{"claude-sonnet-4", "Claude Sonnet 4"},
		{"claude-opus-4", "Claude Opus 4"},
		{"claude-3-5-sonnet", "Claude 3.5 Sonnet"},
		{"claude-3-5-haiku", "Claude 3.5 Haiku"},
		{"claude-3-opus", "Claude 3 Opus"},
		{"claude-3-sonnet", "Claude 3 Sonnet"},
		{"claude-3-haiku", "Claude 3 Haiku"},
	} {
		if strings.HasPrefix(id, pair[0]) {
			return pair[1]
		}
	}
	return id
}

// fetchGeminiModels returns a curated static list for Gemini.
// The Gemini REST models endpoint uses a different auth scheme so we keep
// a well-known list rather than fetching dynamically.
func fetchGeminiModels(apiKey string) []modelRecord {
	if apiKey == "" {
		return curatedGeminiModels()
	}
	// Attempt to fetch via the OpenAI-compat endpoint.
	req, err := http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/openai/models", nil)
	if err != nil {
		return curatedGeminiModels()
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return curatedGeminiModels()
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		return curatedGeminiModels()
	}

	// Non-chat variant keywords — skip anything that isn't a generative text model.
	geminiSkip := []string{"tts", "audio", "embed", "image", "computer", "robotics", "live", "realtime", "native-audio"}

	var candidates []modelRecord
	for _, m := range result.Data {
		bare := strings.TrimPrefix(m.ID, "models/")
		lower := strings.ToLower(bare)
		skip := false
		for _, kw := range geminiSkip {
			if strings.Contains(lower, kw) {
				skip = true
				break
			}
		}
		if skip || !strings.HasPrefix(lower, "gemini") {
			continue
		}
		isFast := strings.Contains(lower, "flash")
		candidates = append(candidates, modelRecord{
			ID:          bare,
			DisplayName: geminiDisplayName(bare),
			IsFast:      isFast,
		})
	}

	// Sort: highest version score first; prefer shorter (more general) ID on ties.
	sort.Slice(candidates, func(i, j int) bool {
		si, sj := geminiModelScore(candidates[i].ID), geminiModelScore(candidates[j].ID)
		if si != sj {
			return si > sj
		}
		return len(candidates[i].ID) < len(candidates[j].ID)
	})

	// Deduplicate: collapse all variants of the same model family
	// (e.g. "gemini-3.1-pro-preview" and "gemini-3.1-pro-preview-customtools"
	// both map to base "gemini-3.1-pro") and keep only the first (highest scored).
	seen := map[string]bool{}
	var models []modelRecord
	for _, m := range candidates {
		base := geminiBaseFamily(m.ID)
		if seen[base] {
			continue
		}
		seen[base] = true
		models = append(models, m)
	}
	top := topFastAndPrimary(models, 5)
	if len(top) == 0 {
		return curatedGeminiModels()
	}
	return top
}

func curatedGeminiModels() []modelRecord {
	return []modelRecord{
		// Primary
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", IsFast: false},
		{ID: "gemini-2.0-pro", DisplayName: "Gemini 2.0 Pro", IsFast: false},
		{ID: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro", IsFast: false},
		{ID: "gemini-3-pro", DisplayName: "Gemini 3 Pro", IsFast: false},
		{ID: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro", IsFast: false},
		// Fast
		{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", IsFast: true},
		{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite", IsFast: true},
		{ID: "gemini-2.0-flash-001", DisplayName: "Gemini 2.0 Flash", IsFast: true},
		{ID: "gemini-2.0-flash-lite", DisplayName: "Gemini 2.0 Flash Lite", IsFast: true},
		{ID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", IsFast: true},
	}
}

func geminiDisplayName(id string) string {
	// Strip the "models/" prefix that the Google API may include.
	bare := strings.TrimPrefix(id, "models/")
	for _, pair := range [][2]string{
		{"gemini-3.1-pro", "Gemini 3.1 Pro"},
		{"gemini-3.1-flash", "Gemini 3.1 Flash"},
		{"gemini-3-pro", "Gemini 3 Pro"},
		{"gemini-3-flash", "Gemini 3 Flash"},
		{"gemini-2.5-pro", "Gemini 2.5 Pro"},
		{"gemini-2.5-flash-lite", "Gemini 2.5 Flash Lite"},
		{"gemini-2.5-flash", "Gemini 2.5 Flash"},
		{"gemini-2.0-flash-lite", "Gemini 2.0 Flash Lite"},
		{"gemini-2.0-flash", "Gemini 2.0 Flash"},
		{"gemini-2.0-pro", "Gemini 2.0 Pro"},
		{"gemini-1.5-flash-8b", "Gemini 1.5 Flash 8B"},
		{"gemini-1.5-flash", "Gemini 1.5 Flash"},
		{"gemini-1.5-pro", "Gemini 1.5 Pro"},
	} {
		if strings.HasPrefix(bare, pair[0]) {
			return pair[1]
		}
	}
	return bare
}

// ── Model helpers ────────────────────────────────────────────────────────────

// topFastAndPrimary returns up to n primary (non-fast) and n fast models from
// an already-sorted slice, preserving relative order within each group.
// Primary models are returned first, fast models second.
func topFastAndPrimary(models []modelRecord, n int) []modelRecord {
	var primary, fast []modelRecord
	for _, m := range models {
		if m.IsFast {
			if len(fast) < n {
				fast = append(fast, m)
			}
		} else {
			if len(primary) < n {
				primary = append(primary, m)
			}
		}
		if len(fast) >= n && len(primary) >= n {
			break
		}
	}
	return append(primary, fast...)
}

// ── Model sorting helpers ─────────────────────────────────────────────────────

// openAIModelScore returns a sort score for an OpenAI model ID.
// Both GPT and O-series are scaled to the same range (~200–550) so they
// naturally interleave by recency rather than one series dominating.
// No specific model IDs are referenced — new families (gpt-5, o5 …) rank automatically.
//
// Scale: gpt-4=400, gpt-4.1=410, gpt-4o(4.5)=450, gpt-5=500
//        o1=200,   o2=300,   o3=400,   o4=500,    o5=600
// Mini/nano variants score 1 point lower than their base.
func openAIModelScore(id string) int {
	lower := strings.ToLower(id)
	isFastVariant := strings.Contains(lower, "mini") || strings.Contains(lower, "nano")
	// O-series (o1, o2, o3, o4 …): score = (version+1)*100
	if len(lower) > 1 && lower[0] == 'o' && lower[1] >= '0' && lower[1] <= '9' {
		rest := lower[1:]
		if idx := strings.IndexByte(rest, '-'); idx > 0 {
			rest = rest[:idx]
		}
		var v int
		fmt.Sscanf(rest, "%d", &v)
		score := (v + 1) * 100 // o1→200, o3→400, o4→500, o5→600
		if isFastVariant {
			score--
		}
		return score
	}
	// GPT-series (gpt-4, gpt-4o, gpt-4.1, gpt-5 …): score = version*100
	if strings.HasPrefix(lower, "gpt-") {
		rest := lower[4:]
		var version float64
		if strings.HasPrefix(rest, "4o") {
			version = 4.5 // gpt-4o ≈ midpoint between 4 and 5
		} else {
			fmt.Sscanf(rest, "%f", &version)
		}
		score := int(version * 100) // gpt-4→400, gpt-4.1→410, gpt-4.5(4o)→450, gpt-5→500
		if strings.Contains(lower, "turbo") {
			score += 2
		}
		if isFastVariant {
			score--
		}
		return score
	}
	return 0
}

// geminiModelScore returns a sort score for a Gemini model ID (no "models/" prefix).
// Higher = more recent / more capable.  New families (gemini-3, gemini-4 …) rank automatically.
func geminiModelScore(id string) int {
	lower := strings.ToLower(strings.TrimPrefix(id, "models/"))
	if !strings.HasPrefix(lower, "gemini-") {
		return 0
	}
	rest := lower[7:] // "2.5-flash-lite", "3-pro", etc.
	var version float64
	fmt.Sscanf(rest, "%f", &version)
	score := int(version * 100) // 2.5→250, 2.0→200, 3.0→300
	switch {
	case strings.Contains(lower, "pro"):
		score += 3
	case strings.Contains(lower, "flash-lite"):
		score += 1
	case strings.Contains(lower, "flash"):
		score += 2
	}
	if strings.Contains(lower, "preview") {
		score-- // prefer stable over preview
	}
	return score
}

// geminiBaseFamily returns the canonical base family for a Gemini model ID,
// stripping preview/customtools/dated-release suffixes so that
// "gemini-3.1-pro-preview" and "gemini-3.1-pro-preview-customtools" both
// collapse to "gemini-3.1-pro".  The known variant types are checked in
// longest-match order so "flash-lite" is not confused with "flash".
func geminiBaseFamily(id string) string {
	bare := strings.TrimPrefix(id, "models/")
	lower := strings.ToLower(bare)
	for _, variant := range []string{"flash-lite", "flash-8b", "flash", "pro", "nano"} {
		idx := strings.Index(lower, variant)
		if idx >= 0 {
			return bare[:idx+len(variant)]
		}
	}
	return bare
}

// stripModelDate removes a trailing date suffix from a model ID so that
// "gpt-4o-2024-08-06" and "gpt-4o" map to the same base family.
// Strips segments from the right that are all-digits and have a length of
// 2 (MM/DD), 4 (YYYY), or 8 (YYYYMMDD).
func stripModelDate(id string) string {
	parts := strings.Split(id, "-")
	end := len(parts)
	for end > 1 {
		p := parts[end-1]
		if allDigits(p) && (len(p) == 2 || len(p) == 4 || len(p) == 8) {
			end--
		} else {
			break
		}
	}
	return strings.Join(parts[:end], "-")
}

func allDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// fetchLMStudioModels queries the local LM Studio server for loaded models.
func fetchLMStudioModels(baseURL, apiKey string) []modelRecord {
	base := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	req, err := http.NewRequest("GET", base+"/models", nil)
	if err != nil {
		return []modelRecord{}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return []modelRecord{}
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []modelRecord{}
	}

	var models []modelRecord
	for _, m := range result.Data {
		models = append(models, modelRecord{
			ID:          m.ID,
			DisplayName: m.ID,
			IsFast:      false,
		})
	}
	return models
}

// fetchOllamaModels queries the local Ollama server for available models.
// Ollama exposes GET /api/tags (not /v1/models) returning {"models":[{"name":"..."}]}.
func fetchOllamaModels(baseURL, apiKey string) []modelRecord {
	base := strings.TrimRight(baseURL, "/")
	// Strip /v1 if present — Ollama's tag endpoint is at the root, not under /v1.
	base = strings.TrimSuffix(base, "/v1")
	req, err := http.NewRequest("GET", base+"/api/tags", nil)
	if err != nil {
		return []modelRecord{}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return []modelRecord{}
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []modelRecord{}
	}

	var models []modelRecord
	for _, m := range result.Models {
		models = append(models, modelRecord{
			ID:          m.Name,
			DisplayName: m.Name,
			IsFast:      false,
		})
	}
	return models
}


// ── API Keys ──────────────────────────────────────────────────────────────────

// APIKeyStatus matches the contracts.ts APIKeyStatus interface.
type APIKeyStatus struct {
	OpenAIKeySet      bool     `json:"openAIKeySet"`
	OllamaKeySet      bool     `json:"ollamaKeySet"`
	TelegramTokenSet  bool     `json:"telegramTokenSet"`
	DiscordTokenSet   bool     `json:"discordTokenSet"`
	SlackBotTokenSet  bool     `json:"slackBotTokenSet"`
	SlackAppTokenSet  bool     `json:"slackAppTokenSet"`
	BraveSearchKeySet bool     `json:"braveSearchKeySet"`
	AnthropicKeySet   bool     `json:"anthropicKeySet"`
	GeminiKeySet      bool     `json:"geminiKeySet"`
	LMStudioKeySet    bool     `json:"lmStudioKeySet"`
	FinnhubKeySet     bool     `json:"finnhubKeySet"`
	CustomKeys        []string `json:"customKeys"`
}

func (d *ControlDomain) getAPIKeys(w http.ResponseWriter, r *http.Request) {
	status := readCredentialBundle()
	writeJSON(w, http.StatusOK, status)
}

func (d *ControlDomain) postAPIKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
		Name     string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "Missing 'provider' field.")
		return
	}
	if err := storeAPIKey(req.Provider, req.Key, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to store key: "+err.Error())
		return
	}
	status := readCredentialBundle()
	writeJSON(w, http.StatusOK, status)
}

func (d *ControlDomain) postAPIKeysInvalidateCache(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"invalidated": true})
}

// ── Link Preview ──────────────────────────────────────────────────────────────

type linkPreviewResult struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"imageURL,omitempty"` // matches contracts.ts LinkPreview.imageURL
}

func (d *ControlDomain) getLinkPreview(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "Missing 'url' query parameter.")
		return
	}

	result, err := fetchLinkPreview(rawURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to fetch URL: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// fetchLinkPreview fetches the URL and extracts title + Open Graph metadata.
func fetchLinkPreview(rawURL string) (linkPreviewResult, error) {
	result := linkPreviewResult{URL: rawURL}

	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("User-Agent", "Atlas/1.0 link-preview")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	httpResp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer httpResp.Body.Close()

	// Read up to 256 KB — enough to find head tags.
	body := make([]byte, 256*1024)
	n, _ := httpResp.Body.Read(body)
	html := string(body[:n])

	result.Title = extractHTMLMeta(html, "og:title")
	if result.Title == "" {
		result.Title = extractHTMLTitle(html)
	}
	result.Description = extractHTMLMeta(html, "og:description")
	if result.Description == "" {
		result.Description = extractHTMLMeta(html, "description")
	}
	result.ImageURL = extractHTMLMeta(html, "og:image")
	return result, nil
}

// extractHTMLMeta extracts <meta property="name" content="…"> or
// <meta name="name" content="…"> from raw HTML.
func extractHTMLMeta(html, name string) string {
	lower := strings.ToLower(html)
	nameAttr := `property="` + name + `"`
	if !strings.Contains(lower, strings.ToLower(nameAttr)) {
		nameAttr = `name="` + name + `"`
	}
	idx := strings.Index(lower, strings.ToLower(nameAttr))
	if idx < 0 {
		return ""
	}
	// Find content= after the property/name attribute.
	rest := html[idx:]
	ci := strings.Index(strings.ToLower(rest), `content="`)
	if ci < 0 {
		return ""
	}
	rest = rest[ci+9:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// extractHTMLTitle extracts the text inside <title>…</title>.
func extractHTMLTitle(html string) string {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<title>")
	if start < 0 {
		return ""
	}
	start += 7
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

func (d *ControlDomain) deleteAPIKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	// Body is optional — ignore decode failures.
	decodeJSONLenient(r, &req)

	if req.Name != "" {
		// Delete a named custom key from the bundle.
		if err := deleteCustomKey(req.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to delete key: "+err.Error())
			return
		}
		status := readCredentialBundle()
		writeJSON(w, http.StatusOK, status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readCredentialBundle reads the Atlas credential bundle from the macOS Keychain
// using the `security` CLI tool and returns an APIKeyStatus.
func readCredentialBundle() APIKeyStatus {
	status := APIKeyStatus{CustomKeys: []string{}}

	out, err := execSecurityInDomain(
		"find-generic-password",
		"-s", "com.projectatlas.credentials",
		"-a", "bundle",
		"-w",
	)
	if err != nil {
		return status
	}

	var bundle struct {
		OpenAIAPIKey      string            `json:"openAIAPIKey"`
		TelegramBotToken  string            `json:"telegramBotToken"`
		DiscordBotToken   string            `json:"discordBotToken"`
		SlackBotToken     string            `json:"slackBotToken"`
		SlackAppToken     string            `json:"slackAppToken"`
		BraveSearchAPIKey string            `json:"braveSearchAPIKey"`
		AnthropicAPIKey   string            `json:"anthropicAPIKey"`
		GeminiAPIKey      string            `json:"geminiAPIKey"`
		LMStudioAPIKey    string            `json:"lmStudioAPIKey"`
		OllamaAPIKey      string            `json:"ollamaAPIKey"`
		FinnhubAPIKey     string            `json:"finnhubAPIKey"`
		CustomSecrets     map[string]string `json:"customSecrets"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &bundle); err != nil {
		return status
	}

	status.OpenAIKeySet = bundle.OpenAIAPIKey != ""
	status.TelegramTokenSet = bundle.TelegramBotToken != ""
	status.DiscordTokenSet = bundle.DiscordBotToken != ""
	status.SlackBotTokenSet = bundle.SlackBotToken != ""
	status.SlackAppTokenSet = bundle.SlackAppToken != ""
	status.BraveSearchKeySet = bundle.BraveSearchAPIKey != ""
	status.AnthropicKeySet = bundle.AnthropicAPIKey != ""
	status.GeminiKeySet = bundle.GeminiAPIKey != ""
	status.LMStudioKeySet = bundle.LMStudioAPIKey != ""
	status.OllamaKeySet = bundle.OllamaAPIKey != ""
	status.FinnhubKeySet = bundle.FinnhubAPIKey != ""
	for k := range bundle.CustomSecrets {
		status.CustomKeys = append(status.CustomKeys, k)
	}

	return status
}

// readRawBundle reads the credential bundle JSON as a generic map so we can
// update individual fields without losing unrecognised keys.
// The second return value is false when the Keychain item could not be read
// (absent or access denied) — callers must not write back in that case.
func readRawBundle() (map[string]interface{}, bool) {
	out, err := execSecurityInDomain(
		"find-generic-password",
		"-s", "com.projectatlas.credentials",
		"-a", "bundle",
		"-w",
	)
	if err != nil {
		// Item absent — return empty map that is safe to write as a new item.
		// Callers that MERGE (storeAPIKey) must treat this as "item not found"
		// and proceed carefully. We return ok=true only when we successfully
		// read an existing item.
		return map[string]interface{}{}, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		return map[string]interface{}{}, false
	}
	return m, true
}

// writeRawBundle serialises the map and stores it in the Keychain, creating or
// updating the item. Uses `security add-generic-password -U` (update flag).
func writeRawBundle(m map[string]interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	_, err = execSecurityInDomain(
		"add-generic-password",
		"-U",
		"-s", "com.projectatlas.credentials",
		"-a", "bundle",
		"-w", string(data),
	)
	return err
}

// storeAPIKey writes a single credential into the Keychain bundle.
// provider values match the web UI's providerID strings.
func storeAPIKey(provider, key, name string) error {
	m, ok := readRawBundle()
	if !ok {
		// Bundle couldn't be read. Check whether the item exists at all.
		// If it doesn't exist, start a fresh bundle (first-time setup).
		// If it DOES exist but we can't read it, return an error rather than
		// wiping the bundle by writing an incomplete map.
		exists, existsErr := keychainItemExists("com.projectatlas.credentials", "bundle")
		if existsErr != nil || exists {
			return fmt.Errorf("credential bundle could not be read from Keychain — open Keychain Access and grant Atlas permission, then try again")
		}
		m = map[string]interface{}{}
	}

	switch provider {
	case "openai":
		m["openAIAPIKey"] = key
	case "anthropic":
		m["anthropicAPIKey"] = key
	case "gemini":
		m["geminiAPIKey"] = key
	case "lm_studio":
		m["lmStudioAPIKey"] = key
	case "ollama":
		m["ollamaAPIKey"] = key
	case "telegram":
		m["telegramBotToken"] = key
	case "discord":
		m["discordBotToken"] = key
	case "slack", "slackBot": // web UI sends "slackBot"
		m["slackBotToken"] = key
	case "slackApp":
		m["slackAppToken"] = key
	case "brave", "braveSearch": // web UI sends "braveSearch"
		m["braveSearchAPIKey"] = key
	case "finnhub":
		m["finnhubAPIKey"] = key
	default:
		// Custom key — stored under customSecrets[name].
		keyName := name
		if keyName == "" {
			keyName = provider
		}
		customs, _ := m["customSecrets"].(map[string]interface{})
		if customs == nil {
			customs = map[string]interface{}{}
		}
		customs[keyName] = key
		m["customSecrets"] = customs
	}

	return writeRawBundle(m)
}

// deleteCustomKey removes a custom key from the bundle's customSecrets map.
// Returns an error (without writing) if the bundle cannot be read, so we
// never accidentally overwrite the full credential bundle with an empty map.
func deleteCustomKey(name string) error {
	m, ok := readRawBundle()
	if !ok {
		return fmt.Errorf("credential bundle could not be read from Keychain — open Keychain Access and grant Atlas permission, then try again")
	}
	customs, _ := m["customSecrets"].(map[string]interface{})
	if customs != nil {
		delete(customs, name)
		m["customSecrets"] = customs
	}
	return writeRawBundle(m)
}
