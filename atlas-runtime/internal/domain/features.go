package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/chat"
	"atlas-runtime-go/internal/creds"
	"atlas-runtime-go/internal/customskills"
	"atlas-runtime-go/internal/features"
	"atlas-runtime-go/internal/forge"
	"atlas-runtime-go/internal/skills"
	"atlas-runtime-go/internal/storage"
)

// forgeAIProvider adapts agent.ProviderConfig to the forge.AIProvider interface.
// Keeping this adapter in the domain layer means forge never needs to import agent,
// breaking the agent → skills → forge → agent import cycle.
type forgeAIProvider struct {
	cfg agent.ProviderConfig
}

func (f forgeAIProvider) CallNonStreaming(ctx context.Context, system, user string) (string, error) {
	messages := []agent.OAIMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	reply, _, _, err := agent.CallAINonStreamingExported(ctx, f.cfg, messages, nil)
	if err != nil {
		return "", err
	}
	if s, ok := reply.Content.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", reply.Content), nil
}

// FeaturesDomain handles skills, automations, workflows, dashboards, forge, and api-validation.
type FeaturesDomain struct {
	supportDir string
	db         *storage.DB
	chatSvc    *chat.Service
	bc         *chat.Broadcaster
	forgeSvc   *forge.Service
	fsRootsMu  sync.Mutex // guards read-modify-write on go-fs-roots.json
}

// NewFeaturesDomain creates the FeaturesDomain.
func NewFeaturesDomain(supportDir string, db *storage.DB, chatSvc *chat.Service, bc *chat.Broadcaster, forgeSvc *forge.Service) *FeaturesDomain {
	return &FeaturesDomain{
		supportDir: supportDir,
		db:         db,
		chatSvc:    chatSvc,
		bc:         bc,
		forgeSvc:   forgeSvc,
	}
}

// Register mounts all feature routes.
func (d *FeaturesDomain) Register(r chi.Router) {
	// Skills
	r.Get("/skills", d.listSkills)
	r.Get("/skills/custom", d.listCustomSkills)
	r.Post("/skills/install", d.installCustomSkill)
	r.Get("/skills/file-system/roots", d.listFsRoots)
	r.Post("/skills/file-system/roots", d.addFsRoot)
	r.Post("/skills/file-system/roots/{id}/remove", d.removeFsRoot)
	r.Post("/skills/file-system/pick-folder", d.pickFsFolder)
	r.Post("/skills/{id}/enable", d.enableSkill)
	r.Post("/skills/{id}/disable", d.disableSkill)
	r.Post("/skills/{id}/validate", d.validateSkill)
	r.Delete("/skills/{id}", d.removeCustomSkill)

	// API Validation
	r.Get("/api-validation/history", d.apiValidationHistory)

	// Automations
	r.Get("/automations", d.listAutomations)
	r.Post("/automations", d.createAutomation)
	r.Get("/automations/file", d.getAutomationsFile)
	r.Put("/automations/file", d.putAutomationsFile)
	r.Get("/automations/{id}", d.getAutomation)
	r.Put("/automations/{id}", d.updateAutomation)
	r.Delete("/automations/{id}", d.deleteAutomation)
	r.Get("/automations/{id}/runs", d.getAutomationRuns)
	r.Post("/automations/{id}/enable", d.enableAutomation)
	r.Post("/automations/{id}/disable", d.disableAutomation)
	r.Post("/automations/{id}/run", d.runAutomation)

	// Workflows
	r.Get("/workflows", d.listWorkflows)
	r.Post("/workflows", d.createWorkflow)
	r.Get("/workflows/runs", d.listWorkflowRuns)
	r.Post("/workflows/runs/{runID}/approve", d.approveWorkflowRun)
	r.Post("/workflows/runs/{runID}/deny", d.denyWorkflowRun)
	r.Get("/workflows/{id}", d.getWorkflow)
	r.Put("/workflows/{id}", d.updateWorkflow)
	r.Delete("/workflows/{id}", d.deleteWorkflow)
	r.Get("/workflows/{id}/runs", d.getWorkflowRuns)
	r.Post("/workflows/{id}/run", d.runWorkflow)

	// Dashboards — read routes native; mutating routes deferred to V1.0 rewrite
	r.Get("/dashboards/proposals", d.listDashboardProposals)
	r.Post("/dashboards/proposals", d.dashboardsDeferred)
	r.Post("/dashboards/install", d.dashboardsDeferred)
	r.Post("/dashboards/reject", d.dashboardsDeferred)
	r.Get("/dashboards/installed", d.listInstalledDashboards)
	r.Delete("/dashboards/installed", d.dashboardsDeferred)
	r.Post("/dashboards/access", d.dashboardsDeferred)
	r.Post("/dashboards/pin", d.dashboardsDeferred)
	r.Post("/dashboards/widgets/execute", d.dashboardsDeferred)

	// Forge
	r.Get("/forge/researching", d.forgeResearching)
	r.Get("/forge/proposals", d.forgeProposals)
	r.Post("/forge/proposals", d.forgePropose)
	r.Get("/forge/installed", d.forgeInstalled)
	r.Post("/forge/installed/{skillID}/uninstall", d.forgeUninstall)
	r.Post("/forge/proposals/{id}/install", d.forgeInstall)
	r.Post("/forge/proposals/{id}/install-enable", d.forgeInstallEnable)
	r.Post("/forge/proposals/{id}/reject", d.forgeReject)
}

// ── Deferred placeholders ─────────────────────────────────────────────────────

// dashboardsDeferred returns a clear not-implemented response for dashboard
// mutating routes that are planned for the V1.0 rewrite.
func (d *FeaturesDomain) dashboardsDeferred(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented,
		"Dashboard AI planning and widget execution are deferred to the V1.0 rewrite. "+
			"Read routes (GET /dashboards/proposals, GET /dashboards/installed) are available.")
}

// ── Skills ────────────────────────────────────────────────────────────────────

func (d *FeaturesDomain) listSkills(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, features.ListSkills(d.supportDir))
}

func (d *FeaturesDomain) listFsRoots(w http.ResponseWriter, r *http.Request) {
	roots, err := skills.LoadFsRoots(d.supportDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read fs roots: "+err.Error())
		return
	}
	if roots == nil {
		roots = []skills.FsRoot{}
	}
	writeJSON(w, http.StatusOK, roots)
}

func (d *FeaturesDomain) addFsRoot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	d.fsRootsMu.Lock()
	defer d.fsRootsMu.Unlock()
	roots, err := skills.LoadFsRoots(d.supportDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read fs roots: "+err.Error())
		return
	}
	newRoot := skills.FsRoot{ID: skills.NewFsRootID(), Path: body.Path}
	roots = append(roots, newRoot)
	if err := skills.SaveFsRoots(d.supportDir, roots); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save fs roots: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, newRoot)
}

func (d *FeaturesDomain) removeFsRoot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d.fsRootsMu.Lock()
	defer d.fsRootsMu.Unlock()
	roots, err := skills.LoadFsRoots(d.supportDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read fs roots: "+err.Error())
		return
	}
	filtered := make([]skills.FsRoot, 0, len(roots))
	found := false
	for _, root := range roots {
		if root.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, root)
	}
	if !found {
		writeError(w, http.StatusNotFound, "root not found: "+id)
		return
	}
	if err := skills.SaveFsRoots(d.supportDir, filtered); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save fs roots: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *FeaturesDomain) pickFsFolder(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		`POSIX path of (choose folder with prompt "Select a folder to grant Atlas access to:")`)
	out, err := cmd.Output()
	if err != nil {
		// User cancelled the dialog — not an error.
		writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
		return
	}
	path := strings.TrimSpace(string(out))
	// Remove trailing slash osascript adds
	path = strings.TrimRight(path, "/")
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (d *FeaturesDomain) enableSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec := features.SetSkillState(d.supportDir, id, "enabled")
	if rec == nil {
		writeError(w, http.StatusNotFound, "skill not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (d *FeaturesDomain) disableSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec := features.SetSkillState(d.supportDir, id, "disabled")
	if rec == nil {
		writeError(w, http.StatusNotFound, "skill not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (d *FeaturesDomain) validateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Try built-in skills first.
	rec := features.ValidateSkill(d.supportDir, id, credentialCheckForSkill)
	if rec != nil {
		writeJSON(w, http.StatusOK, rec)
		return
	}

	// Fall through to forge-installed skills.
	if result := validateForgeSkill(d.supportDir, id); result != nil {
		writeJSON(w, http.StatusOK, result)
		return
	}

	writeError(w, http.StatusNotFound, "skill not found: "+id)
}

// validateForgeSkill checks whether all requiredSecrets for an installed forge
// skill are present in the Keychain bundle's customSecrets map.
// Returns nil if the skill ID is not found in the forge-installed list.
func validateForgeSkill(supportDir, skillID string) map[string]any {
	installed := forge.ListInstalled(supportDir)
	var rec map[string]any
	for _, r := range installed {
		if id, _ := r["id"].(string); id == skillID {
			rec = r
			break
		}
	}
	if rec == nil {
		return nil
	}

	bundle, _ := creds.Read()
	var missing []string

	// requiredSecrets lives at the top level of the installed record.
	if secrets, ok := rec["requiredSecrets"].([]any); ok {
		for _, s := range secrets {
			name, _ := s.(string)
			if name == "" {
				continue
			}
			if bundle.CustomSecret(name) == "" {
				missing = append(missing, name)
			}
		}
	}

	valid := len(missing) == 0
	status := "passed"
	summary := "Skill is ready."
	issues := []string{}
	if !valid {
		status = "failed"
		summary = "Missing custom API keys: " + strings.Join(missing, ", ") + ". Add them in Settings → Credentials."
		issues = missing
	}

	return map[string]any{
		"id":     skillID,
		"source": "forge",
		"validation": map[string]any{
			"skillID": skillID,
			"status":  status,
			"summary": summary,
			"isValid": valid,
			"issues":  issues,
		},
	}
}

// credentialCheckForSkill returns (true, "") when the skill's required API key
// is present in the Keychain bundle, or (false, reason) when it is missing.
func credentialCheckForSkill(skillID string) (bool, string) {
	bundle := readCredentialBundle()
	switch skillID {
	case "web-research":
		if !bundle.BraveSearchKeySet {
			return false, "Brave Search API key is not configured."
		}
	case "web-search":
		if !bundle.BraveSearchKeySet {
			return false, "Brave Search API key is not configured."
		}
	case "image-generation", "vision":
		if !bundle.OpenAIKeySet {
			return false, "OpenAI API key is not configured."
		}
	}
	return true, "Skill is ready."
}

// ── Custom Skills ─────────────────────────────────────────────────────────────

// listCustomSkills returns all user-installed custom skill manifests.
func (d *FeaturesDomain) listCustomSkills(w http.ResponseWriter, r *http.Request) {
	manifests := customskills.ListManifests(d.supportDir)
	if manifests == nil {
		manifests = []customskills.CustomSkillManifest{}
	}
	writeJSON(w, http.StatusOK, manifests)
}

// installCustomSkill copies a skill from a local path into the custom skills directory.
// The target path must contain a valid skill.json and a run executable.
// After installation a daemon restart is required for the skill to become active.
func (d *FeaturesDomain) installCustomSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate source directory.
	if _, err := os.Stat(filepath.Join(body.Path, "skill.json")); err != nil {
		writeError(w, http.StatusBadRequest, "source path does not contain skill.json")
		return
	}
	if _, err := os.Stat(filepath.Join(body.Path, "run")); err != nil {
		writeError(w, http.StatusBadRequest, "source path does not contain a run executable")
		return
	}

	// Read the manifest to get the skill ID.
	data, err := os.ReadFile(filepath.Join(body.Path, "skill.json"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read skill.json: "+err.Error())
		return
	}
	var manifest struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid skill.json: "+err.Error())
		return
	}
	if manifest.ID == "" {
		writeError(w, http.StatusBadRequest, "skill.json must contain an id field")
		return
	}

	// Create the target directory.
	targetDir := filepath.Join(customskills.SkillsDir(d.supportDir), manifest.ID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create skill directory: "+err.Error())
		return
	}

	// Copy all files from source to target (top-level only; no recursive copy needed).
	if err := copySkillFiles(body.Path, targetDir); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to install skill: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      manifest.ID,
		"path":    targetDir,
		"message": "Skill installed. Restart Atlas for the skill to become active.",
	})
}

// removeCustomSkill removes a user-installed custom skill directory.
// Only removes skills that exist in the custom skills directory.
func (d *FeaturesDomain) removeCustomSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skillDir := filepath.Join(customskills.SkillsDir(d.supportDir), id)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "custom skill not found: "+id)
		return
	}
	if err := os.RemoveAll(skillDir); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove skill: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"removed": true,
	})
}

// copySkillFiles copies all top-level files from srcDir to dstDir, preserving file modes.
func copySkillFiles(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip subdirectories
		}
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(dst, data, info.Mode()); err != nil {
			return fmt.Errorf("write %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// ── API Validation ────────────────────────────────────────────────────────────

func (d *FeaturesDomain) apiValidationHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, features.ListAPIValidationHistory(d.supportDir, limit))
}

// ── Automations ───────────────────────────────────────────────────────────────

func (d *FeaturesDomain) listAutomations(w http.ResponseWriter, r *http.Request) {
	items := features.ParseGremlins(d.supportDir)
	if items == nil {
		items = []features.GremlinItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (d *FeaturesDomain) getAutomation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, item := range features.ParseGremlins(d.supportDir) {
		if item.ID == id {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, "automation not found: "+id)
}

func (d *FeaturesDomain) getAutomationsFile(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"content": features.ReadGremlinsRaw(d.supportDir),
	})
}

func (d *FeaturesDomain) putAutomationsFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := features.WriteGremlinsRaw(d.supportDir, body.Content); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write automations file: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *FeaturesDomain) getAutomationRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	writeJSON(w, http.StatusOK, features.ListGremlinRuns(d.db, id, 100))
}

func (d *FeaturesDomain) enableAutomation(w http.ResponseWriter, r *http.Request) {
	d.setAutomationState(w, r, true)
}

func (d *FeaturesDomain) disableAutomation(w http.ResponseWriter, r *http.Request) {
	d.setAutomationState(w, r, false)
}

func (d *FeaturesDomain) setAutomationState(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := chi.URLParam(r, "id")
	items := features.ParseGremlins(d.supportDir)
	var found *features.GremlinItem
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "automation not found: "+id)
		return
	}
	if err := features.SetAutomationEnabled(d.supportDir, id, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update automation: "+err.Error())
		return
	}
	found.IsEnabled = enabled
	writeJSON(w, http.StatusOK, found)
}

// ── Workflows ─────────────────────────────────────────────────────────────────

func (d *FeaturesDomain) listWorkflows(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, features.ListWorkflowDefinitions(d.supportDir))
}

func (d *FeaturesDomain) getWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	raw := features.GetWorkflowDefinition(d.supportDir, id)
	if raw == nil {
		writeError(w, http.StatusNotFound, "workflow not found: "+id)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(raw) //nolint:errcheck
}

func (d *FeaturesDomain) listWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, features.ListWorkflowRuns(d.supportDir, ""))
}

func (d *FeaturesDomain) getWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	writeJSON(w, http.StatusOK, features.ListWorkflowRuns(d.supportDir, id))
}

// ── Automation CRUD ───────────────────────────────────────────────────────────

func (d *FeaturesDomain) createAutomation(w http.ResponseWriter, r *http.Request) {
	var item features.GremlinItem
	if !decodeJSON(w, r, &item) {
		return
	}
	if item.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := features.AppendGremlin(d.supportDir, item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, g := range features.ParseGremlins(d.supportDir) {
		if g.Name == item.Name {
			writeJSON(w, http.StatusCreated, g)
			return
		}
	}
	writeJSON(w, http.StatusCreated, item)
}

func (d *FeaturesDomain) updateAutomation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var updates features.GremlinItem
	if !decodeJSON(w, r, &updates) {
		return
	}
	updated, err := features.UpdateGremlin(d.supportDir, id, updates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeError(w, http.StatusNotFound, "automation not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (d *FeaturesDomain) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	found, err := features.DeleteGremlin(d.supportDir, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "automation not found: "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *FeaturesDomain) runAutomation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var found *features.GremlinItem
	for _, g := range features.ParseGremlins(d.supportDir) {
		g := g
		if g.ID == id {
			found = &g
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "automation not found: "+id)
		return
	}

	if d.chatSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent loop not available")
		return
	}

	runID := newDomainUUID()
	now := time.Now().UTC()
	nowUnix := float64(now.UnixNano()) / 1e9
	convID := newDomainUUID()

	_ = d.db.SaveGremlinRun(storage.GremlinRunRow{
		RunID:          runID,
		GremlinID:      found.ID,
		StartedAt:      nowUnix,
		Status:         "running",
		ConversationID: &convID,
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		req := chat.MessageRequest{Message: found.Prompt, ConversationID: convID}
		resp, err := d.chatSvc.HandleMessage(ctx, req)
		finishedAt := float64(time.Now().UnixNano()) / 1e9
		if err != nil {
			msg := err.Error()
			d.db.UpdateGremlinRun(runID, "failed", &msg, finishedAt) //nolint:errcheck
			return
		}
		output := resp.Response.AssistantMessage
		status := "completed"
		if resp.Response.Status == "error" {
			status = "failed"
			output = resp.Response.ErrorMessage
		}
		d.db.UpdateGremlinRun(runID, status, &output, finishedAt) //nolint:errcheck
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"id":             runID,
		"gremlinID":      found.ID,
		"conversationID": convID,
		"status":         "running",
	})
}

// ── Workflow CRUD ─────────────────────────────────────────────────────────────

func (d *FeaturesDomain) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if !decodeJSON(w, r, &body) {
		return
	}
	if _, ok := body["id"]; !ok {
		body["id"] = newDomainUUID()
	}
	if _, ok := body["createdAt"]; !ok {
		body["createdAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	result, err := features.AppendWorkflowDefinition(d.supportDir, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (d *FeaturesDomain) updateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body map[string]any
	if !decodeJSON(w, r, &body) {
		return
	}
	body["id"] = id
	result, err := features.UpdateWorkflowDefinition(d.supportDir, id, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		writeError(w, http.StatusNotFound, "workflow not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d *FeaturesDomain) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	found, err := features.DeleteWorkflowDefinition(d.supportDir, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workflow not found: "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *FeaturesDomain) runWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	raw := features.GetWorkflowDefinition(d.supportDir, id)
	if raw == nil {
		writeError(w, http.StatusNotFound, "workflow not found: "+id)
		return
	}

	var def map[string]any
	if err := json.Unmarshal(raw, &def); err != nil {
		writeError(w, http.StatusInternalServerError, "corrupt workflow definition")
		return
	}

	if d.chatSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent loop not available")
		return
	}

	runID := newDomainUUID()
	convID := newDomainUUID()
	now := time.Now().UTC().Format(time.RFC3339)

	prompt, _ := def["prompt"].(string)
	if prompt == "" {
		if desc, _ := def["description"].(string); desc != "" {
			prompt = desc
		} else if name, _ := def["name"].(string); name != "" {
			prompt = "Execute workflow: " + name
		} else {
			prompt = "Execute this workflow."
		}
	}

	run := map[string]any{
		"id":             runID,
		"workflowID":     id,
		"status":         "running",
		"startedAt":      now,
		"conversationID": convID,
	}
	features.AppendWorkflowRun(d.supportDir, run) //nolint:errcheck

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		req := chat.MessageRequest{Message: prompt, ConversationID: convID}
		resp, execErr := d.chatSvc.HandleMessage(ctx, req)
		status := "completed"
		if execErr != nil || resp.Response.Status == "error" {
			status = "failed"
		}
		features.UpdateWorkflowRunStatus(d.supportDir, runID, status) //nolint:errcheck
	}()

	writeJSON(w, http.StatusAccepted, run)
}

func (d *FeaturesDomain) approveWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	result, err := features.UpdateWorkflowRunStatus(d.supportDir, runID, "approved")
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d *FeaturesDomain) denyWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	result, err := features.UpdateWorkflowRunStatus(d.supportDir, runID, "denied")
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Dashboards (read-only native; mutating deferred to V1.0) ─────────────────

func (d *FeaturesDomain) listDashboardProposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, features.ListDashboardProposals(d.supportDir))
}

func (d *FeaturesDomain) listInstalledDashboards(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, features.ListInstalledDashboards(d.supportDir))
}

// ── Forge ─────────────────────────────────────────────────────────────────────

func (d *FeaturesDomain) forgeResearching(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.forgeSvc.GetResearching())
}

func (d *FeaturesDomain) forgeProposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, forge.ListProposals(d.supportDir))
}

func (d *FeaturesDomain) forgePropose(w http.ResponseWriter, r *http.Request) {
	var req forge.ProposeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Description == "" {
		writeError(w, http.StatusBadRequest, "name and description are required")
		return
	}
	if d.chatSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent loop not available")
		return
	}

	// Use the fast provider for forge research — it's a background structured-output
	// call that doesn't need the full primary model's capacity.
	// Falls back to the primary provider if no fast model is configured.
	fastProvider, err := d.chatSvc.ResolveFastProvider()
	if err != nil {
		// Fast provider unavailable — try primary before giving up.
		fastProvider, err = d.chatSvc.ResolveProvider()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "AI provider not configured: "+err.Error())
			return
		}
	}

	// Run research in background; return immediately with 202.
	researchID := newDomainUUID()
	now := time.Now().UTC().Format(time.RFC3339)
	item := forge.ResearchingItem{
		ID:        researchID,
		Title:     req.Name,
		Message:   "Researching \"" + req.Name + "\"…",
		StartedAt: now,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		d.forgeSvc.Propose(ctx, req, forgeAIProvider{cfg: fastProvider}) //nolint:errcheck
	}()

	writeJSON(w, http.StatusAccepted, item)
}

func (d *FeaturesDomain) forgeInstalled(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, forge.ListInstalled(d.supportDir))
}

func (d *FeaturesDomain) forgeInstall(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d.doForgeInstall(w, id, false)
}

func (d *FeaturesDomain) forgeInstallEnable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d.doForgeInstall(w, id, true)
}

func (d *FeaturesDomain) doForgeInstall(w http.ResponseWriter, id string, enable bool) {
	status := "installed"
	if enable {
		status = "enabled"
	}

	proposal := forge.UpdateProposalStatus(d.supportDir, id, status)
	if proposal == nil {
		writeError(w, http.StatusNotFound, "proposal not found: "+id)
		return
	}

	// Build and save the installed skill record.
	record := forge.BuildInstalledRecord(*proposal)
	if err := forge.SaveInstalled(d.supportDir, record); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save installed skill: "+err.Error())
		return
	}

	// Optionally enable in go-skill-states.json.
	if enable {
		features.SetForgeSkillState(d.supportDir, proposal.SkillID, "enabled")
	}

	writeJSON(w, http.StatusOK, proposal)
}

func (d *FeaturesDomain) forgeReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	proposal := forge.UpdateProposalStatus(d.supportDir, id, "rejected")
	if proposal == nil {
		writeError(w, http.StatusNotFound, "proposal not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (d *FeaturesDomain) forgeUninstall(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skillID")

	found, err := forge.DeleteInstalled(d.supportDir, skillID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to uninstall: "+err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "installed skill not found: "+skillID)
		return
	}

	// Remove from skill states so it no longer appears as enabled.
	features.SetForgeSkillState(d.supportDir, skillID, "uninstalled")

	writeJSON(w, http.StatusOK, map[string]any{
		"skillID":     skillID,
		"uninstalled": true,
	})
}

// Ensure FeaturesDomain implements Handler.
var _ Handler = (*FeaturesDomain)(nil)
