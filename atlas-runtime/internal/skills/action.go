// Package skills — action.go defines the centralized action classification
// model, structured tool result envelope, dry-run support, mutation summaries,
// and input redaction used across all skills.
//
// # Action classes
//
//	read                — no state change; auto-approved
//	local_write         — creates/updates local data; auto-approved by default
//	destructive_local   — deletes or hard-mutates local state; requires confirmation
//	external_side_effect— opens URLs, sends notifications, controls apps; requires confirmation
//	send_publish_delete — sends email, publishes, or deletes records; always requires confirmation
//
// # Confirmation policy
//
// DefaultNeedsConfirmation() maps ActionClass → bool. The registry's
// NeedsApproval() calls this as its primary gate, then applies any
// action-policies.json overrides on top.
//
// # Dry-run mode
//
// Callers inject dry-run via WithDryRun(ctx). Skills that support simulation
// check IsDryRun(ctx) and return DryRunResult() instead of applying side effects.
// The registry enforces a safety net: non-read actions that have no dry-run
// awareness return a synthetic DryRunResult without calling the underlying Fn.
//
// # Structured results
//
// All skill executions surface a ToolResult{Success, Summary, Artifacts,
// Warnings, NextActions}. FormatForModel() serialises this to JSON for the AI.
// OKResult / ErrResult / DryRunResult are convenience constructors.
//
// # Input redaction
//
// RedactArgs scrubs values whose keys match known sensitive patterns before
// writing to logs or approval summaries.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Action classification ─────────────────────────────────────────────────────

// ActionClass is the canonical impact classification for a skill action.
// It is the primary driver of confirmation policy — PermLevel is kept
// for backward compatibility but ActionClass takes precedence.
type ActionClass string

const (
	// ActionClassRead: pure read, no state change. Always auto-approved.
	ActionClassRead ActionClass = "read"

	// ActionClassLocalWrite: creates or updates local-only data (notes, reminders,
	// calendar events, clipboard, diary, gremlin definitions).
	// Auto-approved by default; policy file can override to always_ask.
	ActionClassLocalWrite ActionClass = "local_write"

	// ActionClassDestructiveLocal: irreversibly mutates or deletes local state
	// (kill process, quit app, delete gremlin/automation).
	// Always requires confirmation; policy file can override to auto_approve.
	ActionClassDestructiveLocal ActionClass = "destructive_local"

	// ActionClassExternalSideEffect: triggers an external interaction
	// (open URL, send notification, navigate browser, control music, generate image).
	// Always requires confirmation by default.
	ActionClassExternalSideEffect ActionClass = "external_side_effect"

	// ActionClassSendPublishDelete: sends a message (email), publishes content,
	// or deletes a remote record. Highest-risk class.
	// Always requires explicit confirmation; echoes target + action in summary.
	ActionClassSendPublishDelete ActionClass = "send_publish_delete"
)

// DefaultNeedsConfirmation returns the built-in confirmation requirement for
// the given ActionClass. This is the single source of truth for the base
// policy — individual actions can be overridden via action-policies.json.
//
//	read                → false (auto-approve)
//	local_write         → false (auto-approve; scoped, reversible)
//	destructive_local   → true
//	external_side_effect→ true
//	send_publish_delete → true
//	unknown             → true (fail-safe)
func DefaultNeedsConfirmation(ac ActionClass) bool {
	switch ac {
	case ActionClassRead, ActionClassLocalWrite:
		return false
	case ActionClassDestructiveLocal, ActionClassExternalSideEffect, ActionClassSendPublishDelete:
		return true
	}
	return true // unknown class → safe default
}

// ── Tool result envelope ──────────────────────────────────────────────────────

// ToolResult is the standardised execution result returned by all skills.
//
// Summary is always a human-readable one-liner. Artifacts carries
// machine-readable data (created IDs, paths, URLs, before/after state,
// mutation diffs) useful for chaining, debugging, and the action log.
// Warnings lists non-fatal issues. NextActions hints at follow-up calls.
// DryRun is set when the result was synthesised without applying side effects.
type ToolResult struct {
	Success     bool           `json:"success"`
	Summary     string         `json:"summary"`
	Artifacts   map[string]any `json:"artifacts,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
	NextActions []string       `json:"next_actions,omitempty"`
	DryRun      bool           `json:"dry_run,omitempty"`
}

// FormatForModel serialises ToolResult to a JSON string suitable for returning
// to the AI model as a tool result. Falls back to Summary on marshalling error.
func (r ToolResult) FormatForModel() string {
	b, err := json.Marshal(r)
	if err != nil {
		return r.Summary
	}
	return string(b)
}

// OKResult constructs a successful ToolResult. Pass nil for artifacts if none.
func OKResult(summary string, artifacts map[string]any) ToolResult {
	return ToolResult{Success: true, Summary: summary, Artifacts: artifacts}
}

// ErrResult constructs a failed ToolResult with actionable failure detail.
//
//   - attempted: what the skill tried to do (e.g. "create calendar event 'Team sync'")
//   - where: where in execution it failed (e.g. "AppleScript execution", "arg validation")
//   - partial: whether any partial state change may have occurred
//   - underlying: the raw error for debugging (nil is fine)
func ErrResult(attempted, where string, partial bool, underlying error) ToolResult {
	artifacts := map[string]any{
		"attempted":      attempted,
		"failure_at":     where,
		"partial_change": partial,
	}
	if underlying != nil {
		artifacts["error_detail"] = underlying.Error()
	}
	var retryHint string
	if !partial {
		retryHint = "No partial change occurred — safe to retry."
	} else {
		retryHint = "A partial change may have occurred — verify state before retrying."
	}
	return ToolResult{
		Success:     false,
		Summary:     fmt.Sprintf("Failed to %s at %s: %v", attempted, where, underlying),
		Artifacts:   artifacts,
		NextActions: []string{retryHint},
	}
}

// DryRunResult constructs a ToolResult indicating dry-run simulation.
//
//   - summary: short description of what would have happened
//   - wouldHappen: detailed description of the planned action
//   - target: the target resource (e.g. email address, calendar name, app name)
func DryRunResult(summary, wouldHappen, target string) ToolResult {
	return ToolResult{
		Success: true,
		Summary: "[DRY RUN] No changes made. " + summary,
		DryRun:  true,
		Artifacts: map[string]any{
			"would_happen": wouldHappen,
			"target":       target,
			"note":         "dry_run=true — no side effect was applied",
		},
	}
}

// wrapStringResult converts the legacy (string, error) pair from a skill Fn
// into a ToolResult. Used by registry.Execute() for skills that have not yet
// been updated to return ToolResult directly.
func wrapStringResult(action, s string, err error) ToolResult {
	if err != nil {
		return ErrResult(action, "skill execution", false, err)
	}
	return OKResult(s, nil)
}

// ── Dry-run context key ───────────────────────────────────────────────────────

type dryRunKey struct{}

// WithDryRun returns a context annotated with dry-run mode enabled.
// Skills check IsDryRun(ctx) before applying side effects.
func WithDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

// IsDryRun returns true if the context has dry-run mode enabled.
func IsDryRun(ctx context.Context) bool {
	v, _ := ctx.Value(dryRunKey{}).(bool)
	return v
}

// ── Mutation summary ──────────────────────────────────────────────────────────

// MutationSummary describes a write operation's before/after state.
// Used by write skills to provide verifiable change records.
type MutationSummary struct {
	Operation string // "created", "updated", "deleted"
	Target    string // human-readable description of what changed
	Before    string // before state if available; empty string means not available
	After     string // after state
	Diff      string // unified diff if applicable (empty when not available)
	Timestamp string // RFC3339
}

// NewMutation creates a MutationSummary. Pass empty strings for before/after
// when the values are not available — the Diff field will be omitted.
// When both before and after are non-empty and differ, a unified diff is
// computed automatically.
func NewMutation(op, target, before, after string) MutationSummary {
	ms := MutationSummary{
		Operation: op,
		Target:    target,
		Before:    before,
		After:     after,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if before != "" && after != "" && before != after {
		ms.Diff = UnifiedDiff(target+"/before", target+"/after", before, after)
	}
	return ms
}

// ToArtifact converts the MutationSummary to an artifact map suitable for
// inclusion in a ToolResult's Artifacts field.
func (m MutationSummary) ToArtifact() map[string]any {
	a := map[string]any{
		"operation": m.Operation,
		"target":    m.Target,
		"timestamp": m.Timestamp,
	}
	if m.Before != "" {
		a["before"] = m.Before
	}
	if m.After != "" {
		a["after"] = m.After
	}
	if m.Diff != "" {
		a["diff"] = m.Diff
	}
	return a
}

// ── Idempotency helpers ───────────────────────────────────────────────────────

// DuplicateCandidate describes a potentially duplicate item found during an
// idempotency pre-check.
type DuplicateCandidate struct {
	Description string // human-readable existing item description
	Basis       string // what heuristic was used (e.g. "title match", "exact name match")
}

// CheckDuplicateResult is returned by duplicate-check helpers.
type CheckDuplicateResult struct {
	IsDuplicate bool
	Candidates  []DuplicateCandidate
	Confidence  string // "exact", "high", "low"
}

// NewDuplicate constructs a CheckDuplicateResult indicating a likely duplicate.
func NewDuplicate(description, basis, confidence string) CheckDuplicateResult {
	return CheckDuplicateResult{
		IsDuplicate: true,
		Confidence:  confidence,
		Candidates:  []DuplicateCandidate{{Description: description, Basis: basis}},
	}
}

// NoDuplicate returns a CheckDuplicateResult indicating no duplicate found.
var NoDuplicate = CheckDuplicateResult{IsDuplicate: false}

// DuplicateWarning formats a human-readable warning string from duplicate candidates.
func DuplicateWarning(action string, result CheckDuplicateResult) string {
	if !result.IsDuplicate || len(result.Candidates) == 0 {
		return ""
	}
	descs := make([]string, len(result.Candidates))
	for i, c := range result.Candidates {
		descs[i] = c.Description
	}
	return fmt.Sprintf("Possible duplicate %s — similar item already exists: %s (basis: %s, confidence: %s)",
		action, strings.Join(descs, "; "), result.Candidates[0].Basis, result.Confidence)
}

// ── Input redaction ───────────────────────────────────────────────────────────

// sensitiveArgKeys lists substrings that flag an argument key as sensitive.
// Values matching these keys are replaced with "[REDACTED]" in logs.
var sensitiveArgKeys = []string{
	"password", "token", "secret", "key", "credential", "auth",
	"apikey", "api_key", "access_token", "private_key", "passphrase",
}

// RedactArgs returns a compact JSON string with sensitive argument values
// replaced by "[REDACTED]". Non-object JSON is returned as "[non-object args]".
// Safe to call with nil or empty args.
func RedactArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return "{}"
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return "[non-object args]"
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[redaction error]"
	}
	return string(b)
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	for _, pat := range sensitiveArgKeys {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
