# Stubs Reference

Remaining stub implementations in the Go runtime as of the Phase 8/9 migration. Updated 2026-03-31.

---

## Fixed (2026-03-31)

| Route / Function | File | Resolution |
|---|---|---|
| `GET /memories/{id}/tags` | `domain/chat.go` | Implemented — fetches memory from SQLite and returns parsed `tags_json` array |
| `GET /conversations/search` | `domain/chat.go` + `storage/db.go` | Implemented — `SearchConversationSummaries` searches message content via LIKE |
| `GET /logs` | `domain/control.go` + `internal/logstore/sink.go` | Implemented — 500-entry ring buffer populated by agent loop and chat service; returns entries in chronological order |

---

## Deferred — Dashboard Mutating Routes

All 7 routes return HTTP 501. Read routes (`GET /dashboards/proposals`, `GET /dashboards/installed`) are fully implemented.

| Route | File | Deferred To |
|---|---|---|
| `POST /dashboards/proposals` | `domain/features.go` | V1.0 rewrite |
| `POST /dashboards/install` | `domain/features.go` | V1.0 rewrite |
| `POST /dashboards/reject` | `domain/features.go` | V1.0 rewrite |
| `DELETE /dashboards/installed` | `domain/features.go` | V1.0 rewrite |
| `POST /dashboards/access` | `domain/features.go` | V1.0 rewrite |
| `POST /dashboards/pin` | `domain/features.go` | V1.0 rewrite |
| `POST /dashboards/widgets/execute` | `domain/features.go` | V1.0 rewrite |

Dashboard AI planning and widget execution require a full redesign. The handler is `dashboardsDeferred()`.

---

## Deferred — Forge Skill Tool Calls

The skill-callable forge actions in `skills/forge_skill.go` return informational redirect messages pointing users to the Forge web UI. They do not execute any AI logic themselves — the agent-loop-based forge pipeline lives in `domain/features.go` (`forgePropose`) and `forge/service.go`.

| Skill Name | Handler | Deferred To |
|---|---|---|
| `forge.plan` | `forgePlan()` | Forge web UI |
| `forge.review` | `forgeReview()` | Forge web UI |
| `forge.validate` | `forgeValidate()` | Forge web UI |

`forge.propose` is intentional — it redirects to the web UI by design.

---


---

## Not Stubs (Guard Conditions)

These were flagged during the initial stub audit but are correctly implemented. The `chatSvc == nil` check is a startup safety guard, not missing functionality.

| Function | File | Note |
|---|---|---|
| `runAutomation()` | `domain/features.go` | Full implementation follows the nil guard |
| `runWorkflow()` | `domain/features.go` | Full implementation follows the nil guard |
| `forgePropose()` | `domain/features.go` | Full implementation follows the nil guard |
