package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ── ActionClass confirmation policy ──────────────────────────────────────────

func TestDefaultNeedsConfirmation(t *testing.T) {
	cases := []struct {
		class ActionClass
		want  bool
	}{
		{ActionClassRead, false},
		{ActionClassLocalWrite, false},
		{ActionClassDestructiveLocal, true},
		{ActionClassExternalSideEffect, true},
		{ActionClassSendPublishDelete, true},
		{"unknown_class", true}, // safe fallback
	}
	for _, c := range cases {
		got := DefaultNeedsConfirmation(c.class)
		if got != c.want {
			t.Errorf("DefaultNeedsConfirmation(%q) = %v, want %v", c.class, got, c.want)
		}
	}
}

// ── ToolResult constructors ───────────────────────────────────────────────────

func TestOKResult(t *testing.T) {
	r := OKResult("Created reminder: Buy milk", map[string]any{"name": "Buy milk"})
	if !r.Success {
		t.Error("OKResult should have Success=true")
	}
	if r.Summary != "Created reminder: Buy milk" {
		t.Errorf("unexpected summary: %q", r.Summary)
	}
	if r.Artifacts["name"] != "Buy milk" {
		t.Error("artifacts not set correctly")
	}
}

func TestOKResultNilArtifacts(t *testing.T) {
	r := OKResult("all good", nil)
	if !r.Success {
		t.Error("success should be true")
	}
	if r.Artifacts != nil {
		t.Error("nil artifacts should remain nil")
	}
}

func TestErrResult(t *testing.T) {
	err := fmt.Errorf("osascript exit 1")
	r := ErrResult("create reminder 'Buy milk'", "AppleScript execution", false, err)

	if r.Success {
		t.Error("ErrResult should have Success=false")
	}
	if r.Artifacts["attempted"] != "create reminder 'Buy milk'" {
		t.Errorf("attempted not set: %v", r.Artifacts["attempted"])
	}
	if r.Artifacts["failure_at"] != "AppleScript execution" {
		t.Errorf("failure_at not set: %v", r.Artifacts["failure_at"])
	}
	if r.Artifacts["partial_change"] != false {
		t.Errorf("partial_change should be false: %v", r.Artifacts["partial_change"])
	}
	if r.Artifacts["error_detail"] != "osascript exit 1" {
		t.Errorf("error_detail not set: %v", r.Artifacts["error_detail"])
	}
	if len(r.NextActions) == 0 {
		t.Error("ErrResult should include a retry hint in NextActions")
	}
}

func TestErrResultPartialChange(t *testing.T) {
	r := ErrResult("write file", "flush", true, fmt.Errorf("disk full"))
	if r.Artifacts["partial_change"] != true {
		t.Error("partial_change should be true")
	}
	// Retry hint should warn about partial state.
	if len(r.NextActions) == 0 {
		t.Error("should have retry hint")
	}
	hint := r.NextActions[0]
	if !contains(hint, "partial") {
		t.Errorf("retry hint should mention partial: %q", hint)
	}
}

func TestDryRunResult(t *testing.T) {
	r := DryRunResult("would create reminder 'Buy milk'", "create reminder name=Buy milk", "Buy milk")
	if !r.Success {
		t.Error("DryRunResult should succeed")
	}
	if !r.DryRun {
		t.Error("DryRunResult should have DryRun=true")
	}
	if !contains(r.Summary, "[DRY RUN]") {
		t.Errorf("DryRunResult summary should contain [DRY RUN]: %q", r.Summary)
	}
	if r.Artifacts["note"] == "" {
		t.Error("DryRunResult should have a note artifact")
	}
}

// ── FormatForModel ────────────────────────────────────────────────────────────

func TestFormatForModel(t *testing.T) {
	r := OKResult("Created note: Meeting notes", map[string]any{"title": "Meeting notes", "body_chars": 42})
	s := r.FormatForModel()

	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("FormatForModel returned invalid JSON: %v\n%s", err, s)
	}
	if out["success"] != true {
		t.Errorf("success field wrong: %v", out["success"])
	}
	if out["summary"] != "Created note: Meeting notes" {
		t.Errorf("summary field wrong: %v", out["summary"])
	}
}

func TestFormatForModelDryRun(t *testing.T) {
	r := DryRunResult("would do something", "do something with x=1", "target")
	s := r.FormatForModel()

	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["dry_run"] != true {
		t.Errorf("dry_run field should be true: %v", out["dry_run"])
	}
}

// ── Dry-run context ───────────────────────────────────────────────────────────

func TestWithDryRun(t *testing.T) {
	ctx := context.Background()
	if IsDryRun(ctx) {
		t.Error("fresh context should not be dry-run")
	}
	dryCtx := WithDryRun(ctx)
	if !IsDryRun(dryCtx) {
		t.Error("context after WithDryRun should be dry-run")
	}
	// Original context must be unaffected.
	if IsDryRun(ctx) {
		t.Error("original context should remain unchanged")
	}
}

// ── MutationSummary ───────────────────────────────────────────────────────────

func TestNewMutation(t *testing.T) {
	m := NewMutation("created", "reminder 'Buy milk'", "", "Buy milk")
	if m.Operation != "created" {
		t.Errorf("operation: %q", m.Operation)
	}
	if m.Diff != "" {
		t.Error("diff should be empty when before is empty")
	}
	if m.Timestamp == "" {
		t.Error("timestamp should be set")
	}
}

func TestNewMutationWithDiff(t *testing.T) {
	m := NewMutation("updated", "note body", "old body text\n", "new body text\n")
	if m.Diff == "" {
		t.Error("diff should be non-empty when before and after differ")
	}
	if !contains(m.Diff, "-old body") {
		t.Errorf("diff should contain deletion: %s", m.Diff)
	}
	if !contains(m.Diff, "+new body") {
		t.Errorf("diff should contain insertion: %s", m.Diff)
	}
}

func TestNewMutationIdentical(t *testing.T) {
	m := NewMutation("updated", "note", "same content", "same content")
	if m.Diff != "" {
		t.Error("diff should be empty when before == after")
	}
}

func TestMutationToArtifact(t *testing.T) {
	m := NewMutation("deleted", "gremlin 'daily-report'", "some state", "")
	a := m.ToArtifact()
	if a["operation"] != "deleted" {
		t.Errorf("operation: %v", a["operation"])
	}
	if a["target"] != "gremlin 'daily-report'" {
		t.Errorf("target: %v", a["target"])
	}
	if a["before"] != "some state" {
		t.Errorf("before: %v", a["before"])
	}
	if _, ok := a["after"]; ok {
		t.Error("after should be absent when empty")
	}
}

// ── Idempotency helpers ───────────────────────────────────────────────────────

func TestNewDuplicate(t *testing.T) {
	d := NewDuplicate("Reminder 'Buy milk' exists", "exact name match", "exact")
	if !d.IsDuplicate {
		t.Error("should be marked as duplicate")
	}
	if d.Confidence != "exact" {
		t.Errorf("confidence: %q", d.Confidence)
	}
	if len(d.Candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(d.Candidates))
	}
}

func TestNoDuplicate(t *testing.T) {
	if NoDuplicate.IsDuplicate {
		t.Error("NoDuplicate should have IsDuplicate=false")
	}
}

func TestDuplicateWarning(t *testing.T) {
	d := NewDuplicate("Reminder 'Buy milk' exists", "name match", "high")
	w := DuplicateWarning("reminder", d)
	if w == "" {
		t.Error("warning should be non-empty for a duplicate")
	}
	if !contains(w, "Buy milk") {
		t.Errorf("warning should mention the item: %q", w)
	}
}

func TestDuplicateWarningNoDuplicate(t *testing.T) {
	w := DuplicateWarning("reminder", NoDuplicate)
	if w != "" {
		t.Errorf("no warning when no duplicate: %q", w)
	}
}

// ── Input redaction ───────────────────────────────────────────────────────────

func TestRedactArgs(t *testing.T) {
	args := json.RawMessage(`{"name":"Buy milk","listName":"Shopping","apiKey":"secret123"}`)
	out := RedactArgs(args)

	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["name"] != "Buy milk" {
		t.Errorf("non-sensitive field should pass through: %v", m["name"])
	}
	if m["listName"] != "Shopping" {
		t.Errorf("non-sensitive field should pass through: %v", m["listName"])
	}
	if m["apiKey"] != "[REDACTED]" {
		t.Errorf("sensitive field should be redacted: %v", m["apiKey"])
	}
}

func TestRedactArgsSensitiveKeys(t *testing.T) {
	cases := []struct {
		key        string
		wantRedact bool
	}{
		{"password", true},
		{"api_key", true},
		{"token", true},
		{"secret", true},
		{"credential", true},
		{"access_token", true},
		{"private_key", true},
		{"name", false},
		{"title", false},
		{"body", false},
		{"appName", false},
	}

	for _, c := range cases {
		args := json.RawMessage(`{"` + c.key + `":"somevalue"}`)
		out := RedactArgs(args)
		var m map[string]any
		json.Unmarshal([]byte(out), &m) //nolint:errcheck
		val := m[c.key]
		isRedacted := val == "[REDACTED]"
		if isRedacted != c.wantRedact {
			t.Errorf("key=%q: wantRedact=%v gotRedact=%v (val=%v)", c.key, c.wantRedact, isRedacted, val)
		}
	}
}

func TestRedactArgsEmpty(t *testing.T) {
	out := RedactArgs(nil)
	if out != "{}" {
		t.Errorf("nil args should return '{}': %q", out)
	}
}

func TestRedactArgsNonObject(t *testing.T) {
	out := RedactArgs(json.RawMessage(`"just a string"`))
	if out != "[non-object args]" {
		t.Errorf("non-object JSON should return placeholder: %q", out)
	}
}

// ── wrapStringResult ──────────────────────────────────────────────────────────

func TestWrapStringResultSuccess(t *testing.T) {
	r := wrapStringResult("weather.current", "Sunny, 22°C", nil)
	if !r.Success {
		t.Error("should succeed")
	}
	if r.Summary != "Sunny, 22°C" {
		t.Errorf("summary: %q", r.Summary)
	}
}

func TestWrapStringResultError(t *testing.T) {
	err := fmt.Errorf("timeout")
	r := wrapStringResult("weather.current", "", err)
	if r.Success {
		t.Error("should fail")
	}
	if r.Artifacts["error_detail"] != "timeout" {
		t.Errorf("error_detail: %v", r.Artifacts["error_detail"])
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
