package skills

import (
	"context"
	"encoding/json"
	"testing"
)

// newTestRegistry creates a Registry with a minimal set of stubs for testing.
// It does not require a database or support directory.
func newTestRegistry() *Registry {
	r := &Registry{entries: make(map[string]SkillEntry)}

	// Read skill
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.read"},
		PermLevel:   "read",
		ActionClass: ActionClassRead,
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "read result", nil
		},
	})

	// Local write skill
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.write_local"},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "created item", nil
		},
	})

	// Destructive local skill
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.destroy_local"},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "deleted", nil
		},
	})

	// External side effect
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.external"},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "opened url", nil
		},
	})

	// Send / publish / delete
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.send"},
		PermLevel:   "execute",
		ActionClass: ActionClassSendPublishDelete,
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "sent", nil
		},
	})

	// FnResult skill
	r.register(SkillEntry{
		Def:         ToolDef{Name: "test.rich_write"},
		PermLevel:   "execute",
		ActionClass: ActionClassLocalWrite,
		FnResult: func(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
			if IsDryRun(ctx) {
				return DryRunResult("would create item", "create item x=1", "item"), nil
			}
			return OKResult("Created item", map[string]any{"id": "abc"}), nil
		},
	})

	return r
}

// ── NeedsApproval — ActionClass-driven policy ─────────────────────────────────

func TestNeedsApproval_Read(t *testing.T) {
	r := newTestRegistry()
	if r.NeedsApproval("test.read") {
		t.Error("read actions should never require approval")
	}
}

func TestNeedsApproval_LocalWrite(t *testing.T) {
	r := newTestRegistry()
	if r.NeedsApproval("test.write_local") {
		t.Error("local_write should not require approval by default")
	}
}

func TestNeedsApproval_DestructiveLocal(t *testing.T) {
	r := newTestRegistry()
	if !r.NeedsApproval("test.destroy_local") {
		t.Error("destructive_local should require approval")
	}
}

func TestNeedsApproval_External(t *testing.T) {
	r := newTestRegistry()
	if !r.NeedsApproval("test.external") {
		t.Error("external_side_effect should require approval")
	}
}

func TestNeedsApproval_Send(t *testing.T) {
	r := newTestRegistry()
	if !r.NeedsApproval("test.send") {
		t.Error("send_publish_delete should require approval")
	}
}

func TestNeedsApproval_Unknown(t *testing.T) {
	r := newTestRegistry()
	if !r.NeedsApproval("unknown.action") {
		t.Error("unknown actions should default to requiring approval")
	}
}

func TestNeedsApproval_OAINameEncoding(t *testing.T) {
	r := newTestRegistry()
	// AI sends back the OAI-safe name with __ instead of .
	if r.NeedsApproval("test__read") {
		t.Error("OAI-encoded read action should be auto-approved")
	}
	if !r.NeedsApproval("test__send") {
		t.Error("OAI-encoded send action should require approval")
	}
}

// ── GetActionClass ────────────────────────────────────────────────────────────

func TestGetActionClass(t *testing.T) {
	r := newTestRegistry()
	cases := []struct {
		action string
		want   ActionClass
	}{
		{"test.read", ActionClassRead},
		{"test.write_local", ActionClassLocalWrite},
		{"test.destroy_local", ActionClassDestructiveLocal},
		{"test.external", ActionClassExternalSideEffect},
		{"test.send", ActionClassSendPublishDelete},
		{"unknown.action", ActionClassExternalSideEffect}, // safe fallback
	}
	for _, c := range cases {
		got := r.GetActionClass(c.action)
		if got != c.want {
			t.Errorf("GetActionClass(%q) = %q, want %q", c.action, got, c.want)
		}
	}
}

// ── Execute — normal path ─────────────────────────────────────────────────────

func TestExecute_FnSuccess(t *testing.T) {
	r := newTestRegistry()
	result, err := r.Execute(context.Background(), "test.read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %v", result.Summary)
	}
	if result.Summary != "read result" {
		t.Errorf("summary: %q", result.Summary)
	}
}

func TestExecute_FnResultSuccess(t *testing.T) {
	r := newTestRegistry()
	result, err := r.Execute(context.Background(), "test.rich_write", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success: %v", result.Summary)
	}
	if result.Artifacts["id"] != "abc" {
		t.Errorf("artifact 'id' not set: %v", result.Artifacts)
	}
}

func TestExecute_UnknownAction(t *testing.T) {
	r := newTestRegistry()
	_, err := r.Execute(context.Background(), "nonexistent.action", nil)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

// ── Execute — dry-run ─────────────────────────────────────────────────────────

func TestExecute_DryRun_ReadPassesThrough(t *testing.T) {
	r := newTestRegistry()
	ctx := WithDryRun(context.Background())
	result, err := r.Execute(ctx, "test.read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Read actions execute normally in dry-run.
	if !result.Success {
		t.Error("read should succeed even in dry-run")
	}
	if result.DryRun {
		t.Error("read result should not be marked as dry-run")
	}
}

func TestExecute_DryRun_FnSkipped(t *testing.T) {
	r := newTestRegistry()
	ctx := WithDryRun(context.Background())
	result, err := r.Execute(ctx, "test.external", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-read Fn skills should be synthetically skipped.
	if !result.DryRun {
		t.Error("external action in dry-run mode should have DryRun=true")
	}
	if !result.Success {
		t.Error("dry-run result should still be successful")
	}
}

func TestExecute_DryRun_FnResultHandled(t *testing.T) {
	r := newTestRegistry()
	ctx := WithDryRun(context.Background())
	result, err := r.Execute(ctx, "test.rich_write", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// FnResult skill handles dry-run and returns DryRun=true.
	if !result.DryRun {
		t.Error("rich_write FnResult skill should return DryRun=true in dry-run mode")
	}
	if !result.Success {
		t.Error("dry-run result should be successful")
	}
}

// ── Default ActionClass from PermLevel ────────────────────────────────────────

func TestDefaultActionClass(t *testing.T) {
	cases := []struct {
		permLevel string
		want      ActionClass
	}{
		{"read", ActionClassRead},
		{"draft", ActionClassLocalWrite},
		{"execute", ActionClassExternalSideEffect},
		{"", ActionClassExternalSideEffect}, // safe default
		{"unknown", ActionClassExternalSideEffect},
	}
	for _, c := range cases {
		got := defaultActionClass(c.permLevel)
		if got != c.want {
			t.Errorf("defaultActionClass(%q) = %q, want %q", c.permLevel, got, c.want)
		}
	}
}

// ── Panic on invalid registration ────────────────────────────────────────────

func TestRegister_PanicNeitherFn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when neither Fn nor FnResult is set")
		}
	}()
	reg := &Registry{entries: make(map[string]SkillEntry)}
	reg.register(SkillEntry{Def: ToolDef{Name: "bad.skill"}})
}

func TestRegister_PanicBothFn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when both Fn and FnResult are set")
		}
	}()
	reg := &Registry{entries: make(map[string]SkillEntry)}
	reg.register(SkillEntry{
		Def: ToolDef{Name: "bad.skill"},
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", nil
		},
		FnResult: func(_ context.Context, _ json.RawMessage) (ToolResult, error) {
			return OKResult("ok", nil), nil
		},
	})
}

// ── Redact args in deferral ───────────────────────────────────────────────────

func TestRedactArgsInDeferral(t *testing.T) {
	// Verify that secrets are stripped from deferral summaries.
	args := json.RawMessage(`{"script":"echo hi","password":"hunter2"}`)
	redacted := RedactArgs(args)

	// Password should not appear in the redacted string.
	if contains(redacted, "hunter2") {
		t.Errorf("secret should be redacted: %s", redacted)
	}
	// Non-sensitive fields should still appear.
	if !contains(redacted, "echo hi") {
		t.Errorf("non-sensitive field should remain: %s", redacted)
	}
}
