package features

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ── Generic JSON file reader ──────────────────────────────────────────────────

// readJSONFile reads a JSON file and unmarshals into v. Returns false on any error.
func readJSONFile(supportDir, filename string, v any) bool {
	data, err := os.ReadFile(filepath.Join(supportDir, filename))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// ── API Validation History ────────────────────────────────────────────────────

// ListAPIValidationHistory reads api-validation-history.json and returns up to limit records.
// Returns an empty slice on any error.
func ListAPIValidationHistory(supportDir string, limit int) []json.RawMessage {
	var records []json.RawMessage
	if !readJSONFile(supportDir, "api-validation-history.json", &records) {
		return []json.RawMessage{}
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records
}

// ── Workflows ─────────────────────────────────────────────────────────────────

// ListWorkflowDefinitions reads workflow-definitions.json.
func ListWorkflowDefinitions(supportDir string) []json.RawMessage {
	var defs []json.RawMessage
	if !readJSONFile(supportDir, "workflow-definitions.json", &defs) {
		return []json.RawMessage{}
	}
	return defs
}

// GetWorkflowDefinition returns a single workflow definition by ID, or nil.
func GetWorkflowDefinition(supportDir, workflowID string) json.RawMessage {
	defs := ListWorkflowDefinitions(supportDir)
	for _, raw := range defs {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) == nil {
			var id string
			if json.Unmarshal(obj["id"], &id) == nil && id == workflowID {
				return raw
			}
		}
	}
	return nil
}

// ListWorkflowRuns reads workflow-runs.json, optionally filtered by workflowID.
func ListWorkflowRuns(supportDir, workflowID string) []json.RawMessage {
	var runs []json.RawMessage
	if !readJSONFile(supportDir, "workflow-runs.json", &runs) {
		return []json.RawMessage{}
	}
	if workflowID == "" {
		return runs
	}
	// Filter by workflowID field.
	var filtered []json.RawMessage
	for _, raw := range runs {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) == nil {
			var id string
			if json.Unmarshal(obj["workflowID"], &id) == nil && id == workflowID {
				filtered = append(filtered, raw)
			}
		}
	}
	if filtered == nil {
		return []json.RawMessage{}
	}
	return filtered
}

var workflowMu sync.Mutex

// writeJSONFile atomically writes v as JSON to supportDir/filename.
func writeJSONFile(supportDir, filename string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(supportDir, filename)
	tmp, err := os.CreateTemp(filepath.Dir(path), filename+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	return os.Rename(tmpPath, path)
}

// AppendWorkflowDefinition adds a new workflow definition and returns it with its id.
func AppendWorkflowDefinition(supportDir string, def map[string]any) (map[string]any, error) {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var defs []map[string]any
	readJSONFile(supportDir, "workflow-definitions.json", &defs) //nolint:errcheck

	defs = append(defs, def)
	if err := writeJSONFile(supportDir, "workflow-definitions.json", defs); err != nil {
		return nil, err
	}
	return def, nil
}

// UpdateWorkflowDefinition replaces the workflow with the given ID. Returns nil if not found.
func UpdateWorkflowDefinition(supportDir, id string, def map[string]any) (map[string]any, error) {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var defs []map[string]any
	readJSONFile(supportDir, "workflow-definitions.json", &defs) //nolint:errcheck

	found := false
	for i, d := range defs {
		if idStr, _ := d["id"].(string); idStr == id {
			defs[i] = def
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	if err := writeJSONFile(supportDir, "workflow-definitions.json", defs); err != nil {
		return nil, err
	}
	return def, nil
}

// DeleteWorkflowDefinition removes a workflow definition by ID. Returns false if not found.
func DeleteWorkflowDefinition(supportDir, id string) (bool, error) {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var defs []map[string]any
	readJSONFile(supportDir, "workflow-definitions.json", &defs) //nolint:errcheck

	var remaining []map[string]any
	found := false
	for _, d := range defs {
		if idStr, _ := d["id"].(string); idStr == id {
			found = true
			continue
		}
		remaining = append(remaining, d)
	}
	if !found {
		return false, nil
	}
	if remaining == nil {
		remaining = []map[string]any{}
	}
	return true, writeJSONFile(supportDir, "workflow-definitions.json", remaining)
}

// AppendWorkflowRun adds a new workflow run record.
func AppendWorkflowRun(supportDir string, run map[string]any) error {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var runs []map[string]any
	readJSONFile(supportDir, "workflow-runs.json", &runs) //nolint:errcheck
	runs = append(runs, run)
	return writeJSONFile(supportDir, "workflow-runs.json", runs)
}

// UpdateWorkflowRunStatus sets the status on an existing run. Returns error if not found.
func UpdateWorkflowRunStatus(supportDir, runID, status string) (map[string]any, error) {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var runs []map[string]any
	readJSONFile(supportDir, "workflow-runs.json", &runs) //nolint:errcheck

	var found map[string]any
	for i, r := range runs {
		if id, _ := r["id"].(string); id == runID {
			runs[i]["status"] = status
			found = runs[i]
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("workflow run not found: %s", runID)
	}
	return found, writeJSONFile(supportDir, "workflow-runs.json", runs)
}

// ── Dashboards ────────────────────────────────────────────────────────────────

// ListDashboardProposals reads dashboard-proposals.json.
func ListDashboardProposals(supportDir string) []json.RawMessage {
	var proposals []json.RawMessage
	if !readJSONFile(supportDir, "dashboard-proposals.json", &proposals) {
		return []json.RawMessage{}
	}
	return proposals
}

// ListInstalledDashboards reads dashboard-installed.json.
func ListInstalledDashboards(supportDir string) []json.RawMessage {
	var dashboards []json.RawMessage
	if !readJSONFile(supportDir, "dashboard-installed.json", &dashboards) {
		return []json.RawMessage{}
	}
	return dashboards
}
