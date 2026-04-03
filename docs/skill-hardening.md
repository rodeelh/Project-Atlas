# Skill Hardening — Developer Reference

This document covers the action classification system, dry-run mode, structured tool results, logging/redaction, and idempotency patterns introduced during the skill hardening phase.

---

## Action Classes

Every skill registration carries an `ActionClass` that drives the confirmation policy and dry-run gate.

| Class | Constant | Auto-approved? | Example skills |
|---|---|---|---|
| `read` | `ActionClassRead` | Yes | weather, web search, filesystem read, info |
| `local_write` | `ActionClassLocalWrite` | Yes | reminders create, calendar create, notes create, clipboard |
| `destructive_local` | `ActionClassDestructiveLocal` | **No** | quit app, run custom AppleScript, shell script |
| `external_side_effect` | `ActionClassExternalSideEffect` | **No** | open URL, open app, send notification, image generate |
| `send_publish_delete` | `ActionClassSendPublishDelete` | **No** | mail send, gremlin delete |

`DefaultNeedsConfirmation(ac ActionClass) bool` is the single source of truth for the base policy. Individual actions can override via `action-policies.json` (`"auto_approve"` / `"always_ask"`).

### Registering with an ActionClass

```go
r.register(SkillEntry{
    Def:         myToolDef,
    PermLevel:   "execute",           // legacy; kept for policy-file keying
    ActionClass: ActionClassLocalWrite, // drives confirmation + dry-run
    FnResult:    myFnResult,
})
```

If `ActionClass` is omitted, it is derived from `PermLevel`: `read→Read`, `draft→LocalWrite`, `execute→ExternalSideEffect`. Always set it explicitly.

---

## Structured Tool Results

All skills should return `ToolResult` — either directly (via `FnResult`) or wrapped automatically (from `Fn`).

```go
type ToolResult struct {
    Success     bool
    Summary     string
    Artifacts   map[string]any  // concrete IDs, paths, timestamps
    Warnings    []string        // non-fatal issues (duplicate found, etc.)
    NextActions []string        // hints for the model on what to do next
    DryRun      bool            // true when result was simulated
}
```

### Constructors

| Constructor | When to use |
|---|---|
| `OKResult(summary, artifacts)` | Successful write with concrete output |
| `ErrResult(attempted, where, partial, err)` | Failure — includes retry hint in `NextActions` |
| `DryRunResult(summary, wouldHappen, target)` | Simulated action in dry-run mode |
| `wrapStringResult(action, s, err)` | Internal — wraps `Fn` string output automatically |

`ToolResult.FormatForModel()` serialises to JSON for the model message; falls back to `Summary` if marshalling fails.

---

## Dry-Run Mode

Dry-run is propagated via `context.Context`:

```go
ctx = skills.WithDryRun(ctx)   // inject
skills.IsDryRun(ctx)           // check
```

`LoopConfig.DryRun = true` causes `Loop.Run()` to inject dry-run into the context before the first iteration.

### Registry enforcement

- **Read-class actions** execute normally — they have no side effects.
- **All other classes with `Fn`** are synthetically skipped; the registry returns `DryRunResult` without calling the function.
- **`FnResult` skills** are called with the dry-run context. If they return a result with `DryRun == true`, that result is used. If they don't handle it, the registry falls back to a synthetic `DryRunResult`.

Skills that want richer simulation should check `IsDryRun(ctx)` at the top of their `FnResult`:

```go
FnResult: func(ctx context.Context, args json.RawMessage) (ToolResult, error) {
    if skills.IsDryRun(ctx) {
        return skills.DryRunResult(
            "would create reminder 'Buy milk'",
            "create reminder name=Buy milk list=Shopping",
            "Shopping",
        ), nil
    }
    // ... real execution
},
```

---

## Mutation Summaries and Diffs

For write operations that modify existing content, use `MutationSummary`:

```go
m := NewMutation("updated", "note 'Meeting notes'", oldBody, newBody)
// m.Diff is a unified diff if before != after
artifacts["mutation"] = m.ToArtifact()
```

`UnifiedDiff(fromPath, toPath, before, after string) string` produces a standard unified diff. Used by the approvals UI to render colored diffs for `fs.write_file` and `fs.patch_file` deferrals.

---

## Idempotency Pre-Checks

Before writing, check if the item already exists:

```go
dup := asCheckReminderExists(ctx, name, listName)
if dup.IsDuplicate {
    result.Warnings = append(result.Warnings, DuplicateWarning("reminder", dup))
    // Don't block — add the warning and let the model/user decide
}
```

`CheckDuplicateResult` fields:

| Field | Meaning |
|---|---|
| `IsDuplicate` | True if a matching item was found |
| `Candidates` | Slice of `{Description, Basis}` matches |
| `Confidence` | `"exact"` / `"high"` / `"fuzzy"` |

`NoDuplicate` is a ready-made zero value for the non-duplicate case.

---

## Failure Reporting

`ErrResult` builds an actionable failure envelope:

```go
return skills.ErrResult(
    "create reminder 'Buy milk'",   // what was attempted
    "AppleScript execution",         // where it failed
    false,                           // partial_change (true if state may be inconsistent)
    err,
), nil
```

When `partial_change` is true, `NextActions[0]` warns the model about potential partial state. Otherwise it suggests a plain retry.

---

## Logging and Redaction

The agent loop creates a `logstore.ActionLogEntry` after every tool call:

```go
logstore.WriteAction(logstore.ActionLogEntry{
    ToolName:     tc.Function.Name,
    ActionClass:  actionClass,
    ConvID:       shortConv,
    InputSummary: redactedArgs,  // secrets stripped
    Success:      result.Success,
    ElapsedMs:    elapsedMs,
    DryRun:       result.DryRun,
    Outcome:      result.Summary,
    Warnings:     result.Warnings,
})
```

`RedactArgs(args json.RawMessage) string` strips keys whose names contain `password`, `api_key`, `token`, `secret`, `credential`, `access_token`, or `private_key`. Non-object JSON returns `[non-object args]`; nil returns `{}`.

Redaction is applied:
- In the agent loop action log
- In approval deferral summaries stored in the DB

---

## Approval Deferral

The two-layer approval flow:

1. `DefaultNeedsConfirmation(ActionClass)` — base policy (see table above)
2. `action-policies.json` per-action override — `"auto_approve"` or `"always_ask"`

The approval UI receives `ActionClass` via `PendingApproval.ActionClass` so it can render the appropriate badge and risk language.

For `fs.write_file` and `fs.patch_file` deferrals, `computeWriteDiffPreview()` pre-computes and stores a unified diff so the approval card can render a colored before/after view.

---

## Adding a New Hardened Skill

1. Create `internal/skills/<name>.go` with a `register<Name>(r *Registry)` method.
2. Choose the correct `ActionClass` from the table above.
3. Use `FnResult` if you need dry-run support, idempotency checks, or rich artifacts.
4. Call `r.register<Name>()` in `NewRegistry()` in `registry.go`.
5. Add to `builtInSkills()` in `internal/features/skills.go`.
6. Add tests covering: success path, error path, dry-run (if FnResult), any duplicate/idempotency logic.
