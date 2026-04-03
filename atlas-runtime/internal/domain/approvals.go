package domain

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/storage"
)

// extractToolArguments parses NormalizedInputJSON (the full agent resume state)
// and returns only the function.arguments string for the tool call matching
// toolCallID. Falls back to "{}" if parsing fails or the ID is not found.
// This prevents the full message history from leaking into approval responses.
func extractToolArguments(normalizedInputJSON, toolCallID string) string {
	var state struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(normalizedInputJSON), &state); err != nil {
		return "{}"
	}
	for _, tc := range state.ToolCalls {
		if tc.ID == toolCallID {
			if tc.Function.Arguments != "" {
				return tc.Function.Arguments
			}
			return "{}"
		}
	}
	return "{}"
}

// ApprovalsDomain handles approvals and action-policy routes natively.
//
// Routes owned:
//
//	GET    /approvals
//	POST   /approvals/:id/approve
//	POST   /approvals/:id/deny
//	GET    /action-policies
//	GET    /action-policies/:id
//	PUT    /action-policies/:id
//	POST   /action-policies/:id
type ApprovalsDomain struct {
	db         *storage.DB
	policyPath string
	mu         sync.Mutex // guards policy file reads/writes

	// OnResolve is called after an approval status is updated.
	// toolCallID is the tool_call_id; status is "approved" or "denied".
	// If nil, the callback is skipped.
	OnResolve func(toolCallID, status string)
}

// NewApprovalsDomain creates the ApprovalsDomain.
func NewApprovalsDomain(db *storage.DB, supportDir string) *ApprovalsDomain {
	return &ApprovalsDomain{
		db:         db,
		policyPath: filepath.Join(supportDir, "action-policies.json"),
	}
}

// Register mounts approvals routes on the given router.
func (d *ApprovalsDomain) Register(r chi.Router) {
	r.Get("/approvals", d.listApprovals)
	r.Post("/approvals/{id}/approve", d.approveToolCall)
	r.Post("/approvals/{id}/deny", d.denyToolCall)
	r.Get("/action-policies", d.getActionPolicies)
	r.Get("/action-policies/{id}", d.getActionPolicy)
	r.Put("/action-policies/{id}", d.setActionPolicy)
	r.Post("/action-policies/{id}", d.setActionPolicy)
}

// ── JSON shapes ───────────────────────────────────────────────────────────────

type approvalToolCall struct {
	ID               string `json:"id"`
	ToolName         string `json:"toolName"`
	ArgumentsJSON    string `json:"argumentsJSON"`
	PermissionLevel  string `json:"permissionLevel"`
	RequiresApproval bool   `json:"requiresApproval"`
	Status           string `json:"status,omitempty"`
	Timestamp        string `json:"timestamp,omitempty"`
}

type approvalJSON struct {
	ID                      string           `json:"id"`
	Status                  string           `json:"status"`
	ConversationID          *string          `json:"conversationID,omitempty"`
	DeferredExecutionID     *string          `json:"deferredExecutionID,omitempty"`
	DeferredExecutionStatus *string          `json:"deferredExecutionStatus,omitempty"`
	LastError               *string          `json:"lastError,omitempty"`
	PreviewDiff             *string          `json:"previewDiff,omitempty"`
	ToolCall                approvalToolCall `json:"toolCall"`
}

// rowToApproval converts a DeferredExecRow to the Approval JSON shape.
func rowToApproval(r storage.DeferredExecRow) approvalJSON {
	toolName := r.Summary // fallback
	if r.ActionID != nil && *r.ActionID != "" {
		toolName = *r.ActionID
	} else if r.SkillID != nil && *r.SkillID != "" {
		toolName = *r.SkillID
	}

	deferredStatus := r.Status
	approvalStatus := deferredStatusToApprovalStatus(r.Status)

	return approvalJSON{
		ID:                      r.ApprovalID,
		Status:                  approvalStatus,
		ConversationID:          r.ConversationID,
		DeferredExecutionID:     &r.DeferredID,
		DeferredExecutionStatus: &deferredStatus,
		LastError:               r.LastError,
		PreviewDiff:             r.PreviewDiff,
		ToolCall: approvalToolCall{
			ID:               r.ToolCallID,
			ToolName:         toolName,
			ArgumentsJSON:    extractToolArguments(r.NormalizedInputJSON, r.ToolCallID),
			PermissionLevel:  r.PermissionLevel,
			RequiresApproval: true,
			Status:           approvalStatus,
			Timestamp:        r.CreatedAt,
		},
	}
}

func deferredStatusToApprovalStatus(s string) string {
	switch s {
	case "pending_approval":
		return "pending"
	case "approved", "running", "completed":
		return "approved"
	case "denied":
		return "denied"
	default:
		return "pending"
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (d *ApprovalsDomain) listApprovals(w http.ResponseWriter, r *http.Request) {
	rows, err := d.db.ListAllApprovals(200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read approvals: "+err.Error())
		return
	}

	out := make([]approvalJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToApproval(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *ApprovalsDomain) approveToolCall(w http.ResponseWriter, r *http.Request) {
	d.resolveApproval(w, r, "approved")
}

func (d *ApprovalsDomain) denyToolCall(w http.ResponseWriter, r *http.Request) {
	d.resolveApproval(w, r, "denied")
}

func (d *ApprovalsDomain) resolveApproval(w http.ResponseWriter, r *http.Request, newStatus string) {
	toolCallID := chi.URLParam(r, "id")

	row, err := d.db.FetchDeferredByToolCallID(toolCallID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error: "+err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "approval not found for toolCallID: "+toolCallID)
		return
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.db.UpdateDeferredStatus(toolCallID, newStatus, updatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update approval: "+err.Error())
		return
	}

	row.Status = newStatus
	row.UpdatedAt = updatedAt
	writeJSON(w, http.StatusOK, rowToApproval(*row))

	// Log the approval resolution.
	toolName := row.Summary
	if row.ActionID != nil && *row.ActionID != "" {
		toolName = *row.ActionID
	}
	logMsg := "Approval approved: " + toolName
	if newStatus == "denied" {
		logMsg = "Approval denied: " + toolName
	}
	logstore.Write("info", logMsg, map[string]string{"toolCallID": toolCallID})

	// Notify the agent loop so it can resume the conversation.
	if d.OnResolve != nil {
		go d.OnResolve(toolCallID, newStatus)
	}
}

// ── Action policies ───────────────────────────────────────────────────────────

func (d *ApprovalsDomain) loadPolicies() (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := os.ReadFile(d.policyPath)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var policies map[string]string
	if err := json.Unmarshal(data, &policies); err != nil {
		return map[string]string{}, nil // corrupt file → empty
	}
	return policies, nil
}

func (d *ApprovalsDomain) savePolicies(policies map[string]string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := json.Marshal(policies)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(d.policyPath), "action-policies-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, d.policyPath)
}

func (d *ApprovalsDomain) getActionPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := d.loadPolicies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read policies: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policies)
}

func (d *ApprovalsDomain) getActionPolicy(w http.ResponseWriter, r *http.Request) {
	actionID := chi.URLParam(r, "id")
	policies, err := d.loadPolicies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read policies: "+err.Error())
		return
	}
	policy, ok := policies[actionID]
	if !ok {
		writeError(w, http.StatusNotFound, "no policy for action: "+actionID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"policy": policy})
}

func (d *ApprovalsDomain) setActionPolicy(w http.ResponseWriter, r *http.Request) {
	actionID := chi.URLParam(r, "id")

	var body struct {
		Policy string `json:"policy"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Policy == "" {
		writeError(w, http.StatusBadRequest, "body must be {\"policy\": \"<value>\"}")
		return
	}

	policies, err := d.loadPolicies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read policies: "+err.Error())
		return
	}
	policies[actionID] = body.Policy
	if err := d.savePolicies(policies); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save policies: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policies)
}

// Ensure ApprovalsDomain implements Handler.
var _ Handler = (*ApprovalsDomain)(nil)
