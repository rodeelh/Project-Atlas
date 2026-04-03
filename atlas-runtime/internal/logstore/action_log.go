// action_log.go — structured action log entries extending the existing sink.
//
// ActionLogEntry records a single skill execution with enough context to:
//   - diagnose failures in production workflows
//   - audit mutable operations (what changed, when, on what target)
//   - reconstruct the execution path of a multi-tool agent turn
//
// WriteAction() maps an ActionLogEntry onto a regular logstore.Entry so that
// all log consumers (GET /logs, in-memory ring) see one unified stream.
// Action-specific fields are promoted into the Entry.Metadata map.
package logstore

import (
	"strconv"
	"strings"
)

// ActionLogEntry is a structured record of a single skill/tool execution.
// All fields are optional except ToolName, ActionClass, and Outcome.
type ActionLogEntry struct {
	// Identity
	ToolName    string // e.g. "applescript.reminders_write"
	ActionClass string // "read", "local_write", "destructive_local", etc.
	ConvID      string // short conv ID for correlation (empty if not in a conversation)

	// Input — always redacted before logging
	InputSummary string // compact representation of args with secrets removed

	// Execution
	Success   bool
	ElapsedMs int64 // wall-clock milliseconds
	DryRun    bool  // true when execution was simulated

	// Output
	Outcome   string   // one-line human outcome (e.g. "Created reminder: Buy milk")
	Artifacts []string // notable artifact strings (IDs, URLs, paths, created item names)
	Warnings  []string // non-fatal issues surfaced during execution
	Errors    []string // error messages if Success==false
}

// WriteAction records a structured ActionLogEntry to the global sink.
// The entry is stored as a regular logstore.Entry with action fields
// promoted to Metadata so that GET /logs returns the full picture.
func WriteAction(e ActionLogEntry) {
	level := "info"
	if !e.Success {
		level = "error"
	}

	meta := map[string]string{
		"tool":       e.ToolName,
		"class":      e.ActionClass,
		"success":    boolStr(e.Success),
		"elapsed_ms": strconv.FormatInt(e.ElapsedMs, 10),
	}
	if e.ConvID != "" {
		meta["conv"] = e.ConvID
	}
	if e.DryRun {
		meta["dry_run"] = "true"
	}
	if e.InputSummary != "" {
		meta["input"] = e.InputSummary
	}
	if len(e.Artifacts) > 0 {
		meta["artifacts"] = strings.Join(e.Artifacts, ", ")
	}
	if len(e.Warnings) > 0 {
		meta["warnings"] = strings.Join(e.Warnings, "; ")
	}
	if len(e.Errors) > 0 {
		meta["errors"] = strings.Join(e.Errors, "; ")
	}

	msg := e.ToolName + ": " + e.Outcome
	if e.DryRun {
		msg = "[dry-run] " + msg
	}

	global.Write(level, msg, meta)
}

// NewActionEntry constructs an ActionLogEntry with the common fields shared
// across all skill executions. Callers add tool-specific artifacts and warnings
// before calling WriteAction.
func NewActionEntry(
	toolName, actionClass, convID, inputSummary string,
	success bool,
	elapsedMs int64,
	dryRun bool,
	outcome string,
) ActionLogEntry {
	return ActionLogEntry{
		ToolName:     toolName,
		ActionClass:  actionClass,
		ConvID:       convID,
		InputSummary: inputSummary,
		Success:      success,
		ElapsedMs:    elapsedMs,
		DryRun:       dryRun,
		Outcome:      outcome,
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
