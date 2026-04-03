package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/engine"
)

// EngineDomain handles all /engine/* routes for Engine LM.
type EngineDomain struct {
	mgr      *engine.Manager
	routerMgr *engine.Manager // Phase 3 tool router — separate port, small model
	cfgStore *config.Store
}

// NewEngineDomain creates an EngineDomain.
func NewEngineDomain(mgr *engine.Manager, routerMgr *engine.Manager, cfgStore *config.Store) *EngineDomain {
	return &EngineDomain{mgr: mgr, routerMgr: routerMgr, cfgStore: cfgStore}
}

// Register mounts engine routes on the given router.
func (d *EngineDomain) Register(r chi.Router) {
	r.Get("/engine/status", d.getStatus)
	r.Get("/engine/models", d.getModels)
	r.Post("/engine/start", d.postStart)
	r.Post("/engine/stop", d.postStop)
	r.Post("/engine/models/download", d.postDownload)
	r.Post("/engine/update", d.postUpdate)
	r.Delete("/engine/models/{name}", d.deleteModel)

	// Tool router (Phase 3) — dedicated small model on a second port.
	r.Get("/engine/router/status", d.getRouterStatus)
	r.Post("/engine/router/start", d.postRouterStart)
	r.Post("/engine/router/stop", d.postRouterStop)
}

// GET /engine/status
func (d *EngineDomain) getStatus(w http.ResponseWriter, r *http.Request) {
	cfg := d.cfgStore.Load()
	s := d.mgr.Status(cfg.AtlasEnginePort)
	if s.Running {
		s.LastTPS = d.mgr.FetchDecodeTPS(s.Port)
		s.ContextTokens = d.mgr.FetchContextTokens(s.Port)
	}
	writeJSON(w, http.StatusOK, s)
}

// GET /engine/models
func (d *EngineDomain) getModels(w http.ResponseWriter, r *http.Request) {
	models, err := d.mgr.ListModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if models == nil {
		models = []engine.ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}

// POST /engine/start   {"model": "gemma-3-4b.gguf", "port": 11985, "ctxSize": 8192}
func (d *EngineDomain) postStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model   string `json:"model"`
		Port    int    `json:"port"`
		CtxSize int    `json:"ctxSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	cfg := d.cfgStore.Load()
	port := req.Port
	if port == 0 {
		port = cfg.AtlasEnginePort
	}
	if port == 0 {
		port = 11985
	}
	ctxSize := req.CtxSize
	if ctxSize <= 0 {
		ctxSize = cfg.AtlasEngineCtxSize
	}
	if err := d.mgr.Start(req.Model, port, ctxSize, cfg.AtlasEngineKVCacheQuant); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d.mgr.Status(port))
}

// POST /engine/stop
func (d *EngineDomain) postStop(w http.ResponseWriter, r *http.Request) {
	if err := d.mgr.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg := d.cfgStore.Load()
	writeJSON(w, http.StatusOK, d.mgr.Status(cfg.AtlasEnginePort))
}

// POST /engine/models/download   {"url": "https://...", "filename": "model.gguf"}
// Streams SSE events: start → progress* → done | error
func (d *EngineDomain) postDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" || req.Filename == "" {
		writeError(w, http.StatusBadRequest, "url and filename are required")
		return
	}
	// Basic URL validation.
	if _, err := url.ParseRequestURI(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.(http.Flusher)

	emit := func(eventType string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		if hasFlusher {
			flusher.Flush()
		}
	}

	emit("start", map[string]any{"filename": req.Filename})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	err := d.mgr.DownloadModel(ctx, req.URL, req.Filename, func(downloaded, total int64) {
		pct := 0.0
		if total > 0 {
			pct = float64(downloaded) / float64(total) * 100
		}
		emit("progress", map[string]any{
			"downloaded": downloaded,
			"total":      total,
			"percent":    pct,
		})
	})

	if err != nil {
		emit("error", map[string]any{"message": err.Error()})
		return
	}

	models, _ := d.mgr.ListModels()
	if models == nil {
		models = []engine.ModelInfo{}
	}
	emit("done", map[string]any{"filename": req.Filename, "models": models})
}

// POST /engine/update   {"version": "b8641"}
// Streams SSE events: start → progress* → done | error
// Stops the running server before downloading, so the binary can be replaced.
func (d *EngineDomain) postUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Version == "" {
		req.Version = "b8641" // pinned default
	}

	// Capture running state before stopping so we can restart after update.
	prevModel := d.mgr.LoadedModel()
	cfg := d.cfgStore.Load()
	prevPort := cfg.AtlasEnginePort
	if prevPort == 0 {
		prevPort = 11985
	}

	// Stop the running server so the binary can be replaced on disk.
	_ = d.mgr.Stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.(http.Flusher)
	emit := func(eventType string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
		if hasFlusher {
			flusher.Flush()
		}
	}

	emit("start", map[string]any{"version": req.Version})

	err := d.mgr.DownloadBinary(req.Version, func(downloaded, total int64) {
		pct := 0.0
		if total > 0 {
			pct = float64(downloaded) / float64(total) * 100
		}
		emit("progress", map[string]any{
			"downloaded": downloaded,
			"total":      total,
			"percent":    pct,
		})
	})

	if err != nil {
		emit("error", map[string]any{"message": err.Error()})
		return
	}

	// Restart with the previously loaded model if one was running.
	if prevModel != "" {
		ctxSize := cfg.AtlasEngineCtxSize
		if ctxSize <= 0 {
			ctxSize = 8192
		}
		_ = d.mgr.Start(prevModel, prevPort, ctxSize, cfg.AtlasEngineKVCacheQuant)
	}

	emit("done", map[string]any{
		"version":    req.Version,
		"restarted":  prevModel != "",
		"status":     d.mgr.Status(prevPort),
	})
}

// DELETE /engine/models/{name}
func (d *EngineDomain) deleteModel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "model name required")
		return
	}
	if err := d.mgr.DeleteModel(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	models, err := d.mgr.ListModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if models == nil {
		models = []engine.ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}

// ── Tool router (Phase 3) ─────────────────────────────────────────────────────

// GET /engine/router/status
func (d *EngineDomain) getRouterStatus(w http.ResponseWriter, r *http.Request) {
	cfg := d.cfgStore.Load()
	port := cfg.AtlasEngineRouterPort
	if port == 0 {
		port = 11986
	}
	writeJSON(w, http.StatusOK, d.routerMgr.Status(port))
}

// POST /engine/router/start   {"model": "gemma-4-2b-it-Q4_K_M.gguf"}
func (d *EngineDomain) postRouterStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := d.cfgStore.Load()
	model := req.Model
	if model == "" {
		model = cfg.AtlasEngineRouterModel
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	port := cfg.AtlasEngineRouterPort
	if port == 0 {
		port = 11986
	}
	// Router needs enough context for the full tool-selection prompt (~3K tokens).
	// Use the configured ctx size with a 4096 floor; never inherit the hardcoded 2048 default.
	routerCtxSize := cfg.AtlasEngineCtxSize
	if routerCtxSize < 4096 {
		routerCtxSize = 4096
	}
	if err := d.routerMgr.Start(model, port, routerCtxSize, cfg.AtlasEngineKVCacheQuant); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d.routerMgr.Status(port))
}

// POST /engine/router/stop
func (d *EngineDomain) postRouterStop(w http.ResponseWriter, r *http.Request) {
	cfg := d.cfgStore.Load()
	port := cfg.AtlasEngineRouterPort
	if port == 0 {
		port = 11986
	}
	if err := d.routerMgr.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d.routerMgr.Status(port))
}
