# Project Atlas — Claude Code Reference

Automatically loaded at the start of every session. Navigation map only — read actual source files for implementation details.

Current source of truth:
- System design: `Atlas/docs/architecture.md`
- API reference: `Atlas/docs/runtime-api-v1.md`
- Migration history: `archive/MIGRATION.md`
- This file: contributor navigation and workflow shortcuts

---

## Package Map

| Package | Owns |
| --- | --- |
| `atlas-runtime` | HTTP server, agent loop, all domain handlers, skills registry, forge pipeline, automations, workflows, auth, config, SQLite storage, comms bridges. Runs as launchd daemon `Atlas` on port 1984. |
| `atlas-tui` | Bubbletea terminal UI — connects to `localhost:1984`. Installed to `~/.local/bin/atlas`. |
| `atlas-web` | All product UI — Chat, Dashboards, Forge, Skills, Approvals, Memory, Automations, Workflows, Communications, Settings |

Swift packages are archived at `archive/swift/`. They are not built and not referenced by any active code.

---

## `atlas-runtime` Internal Package Map

| Package | Role |
| --- | --- |
| `cmd/atlas-runtime` | Binary entry point — flags, service wiring, `http.ListenAndServe` |
| `internal/agent` | OpenAI/Anthropic/Gemini/LM Studio provider calls, agent loop (tool dispatch, approval deferral, SSE emission) |
| `internal/auth` | Session auth — bootstrap, token issuance, HMAC session validation |
| `internal/chat` | `Service` (message handling, agent loop orchestration), `Broadcaster` (SSE fan-out) |
| `internal/comms` | Telegram + Discord bridge lifecycle, channel management |
| `internal/config` | `RuntimeConfigSnapshot`, `Store` (atomic JSON read/write), `GoRuntimeConfig` |
| `internal/creds` | Keychain credential bundle reader (`security` CLI) |
| `internal/domain` | HTTP domain handlers — one file per domain: `auth`, `chat`, `control`, `approvals`, `communications`, `features` |
| `internal/features` | Automations (GREMLINS.md), workflows (JSON), dashboards (JSON), API validation history, skills state, diary (DIARY.md) |
| `internal/forge` | Forge proposal lifecycle — AI research, JSON persistence, install/uninstall |
| `internal/logstore` | In-memory log ring buffer (500 entries) — written by agent loop and services, read by `GET /logs` |
| `internal/mind` | MIND.md two-tier reflection pipeline + SKILLS.md learned-routine detection; both run non-blocking after each turn |
| `internal/runtime` | Runtime introspection (port, start time) |
| `internal/server` | Chi router construction, middleware (session auth, CORS, remote-IP guard) |
| `internal/skills` | Built-in skill registry — weather, web, filesystem, system, terminal, applescript, finance, image, diary, browser, vault, gremlin, websearch, forge, info |
| `internal/storage` | SQLite — conversations, messages, memories, gremlin runs, deferred executions |
| `internal/validate` | API validation gate — pre-flight, live execution (max 2 attempts), audit |

---

## Dependency Rules

```
creds, config  ←  everything reads these
storage        ←  chat, domain, features
agent          ←  chat, forge
skills         ←  chat (registry injection), domain/features
features       ←  domain/features, skills/gremlin
forge          ←  domain/features, agent
validate       ←  domain/features (validate skill), forge
chat           ←  domain/chat, domain/features (runAutomation, runWorkflow)
comms          ←  domain/communications
domain/*       ←  server (router registration only)
server         ←  cmd/atlas-runtime
```

No package imports `domain`. `domain` imports everything above it.

---

## Key Files — Quick Reference

| File | Role |
| --- | --- |
| `cmd/atlas-runtime/main.go` | Entry point — all service construction and wiring |
| `internal/agent/loop.go` | Single-turn agent execution loop |
| `internal/agent/provider.go` | AI provider dispatch (OpenAI/Anthropic/Gemini/LM Studio) |
| `internal/chat/service.go` | `HandleMessage`, `RegenerateMind`, `ResolveProvider`, `Resume` |
| `internal/chat/keychain.go` | `resolveProvider` — builds `ProviderConfig` from config + Keychain |
| `internal/config/snapshot.go` | `RuntimeConfigSnapshot` — all runtime config fields |
| `internal/domain/features.go` | All feature domain handlers — skills, automations, workflows, dashboards, forge |
| `internal/features/skills.go` | `builtInSkills()` catalog, `ListSkills`, `SetSkillState`, `SetForgeSkillState` |
| `internal/skills/registry.go` | `NewRegistry` — registers all built-in skills |
| `internal/forge/service.go` | `Propose` — AI research pipeline, in-memory researching list |
| `internal/forge/store.go` | `forge-proposals.json` and `forge-installed.json` persistence |
| `internal/storage/db.go` | All SQLite queries |
| `internal/validate/gate.go` | `Gate.Run` — 3-phase API validation |
| `internal/mind/reflection.go` | `ReflectNonBlocking` — two-tier MIND.md update after each turn |
| `internal/mind/skills.go` | `LearnFromTurnNonBlocking` — SKILLS.md routine learning |
| `internal/features/diary.go` | `AppendDiaryEntry`, `ReadDiary`, `DiaryContext` — DIARY.md R/W |
| `internal/logstore/sink.go` | `logstore.Write` — global log sink, read by `GET /logs` |

---

## Skill Classification

Skills fall into three categories. The category determines where new capabilities belong.

| Category | Description | Location | Deploy |
| --- | --- | --- | --- |
| **Core built-in** | Requires Atlas internals: shared process state, SQLite, SSE broadcaster, Keychain, or go-rod Chrome. Cannot be decoupled without significant rework. | `internal/skills/` compiled into binary | `make install` + daemon restart |
| **Standard built-in** | Self-contained API calls or system operations compiled in for convenience. Could theoretically be custom skills but migration is low-ROI. Leave them as-is unless there is a specific reason to change them. | `internal/skills/` compiled into binary | `make install` + daemon restart |
| **Custom skill** | User-installed or third-party capability. An executable (any language) in its own directory. Atlas calls it via subprocess (stdin/stdout JSON). No recompile needed. | `~/Library/Application Support/ProjectAtlas/skills/<id>/` | Drop folder + daemon restart |

**Core built-in skills (must stay compiled-in):**
`browser.*` — shared go-rod Chrome process · `vault.*` — Keychain + internal creds · `gremlin.*` — SQLite + GREMLINS.md · `forge.*` — internal forge service · `atlas.*` / `info.*` — self-introspection · `diary.*` — internal diary.go integration

**Standard built-in skills (compiled-in, leave as-is):**
`weather.*` · `web.*` · `websearch.*` · `fs.*` · `system.*` · `terminal.*` · `applescript.*` · `finance.*` · `image.*`

**Decision rule for new skills:** Does it need direct access to a Go struct, the SQLite DB, the SSE broadcaster, or a shared process? → Core built-in. Is it a third-party API, personal automation, or domain-specific tool? → Custom skill.

---

## Custom Skills

Custom skills are user-installed executables that Atlas calls via subprocess. They appear in `GET /skills` alongside built-ins with `"source": "custom"`. From the model's perspective there is no difference.

**Directory layout:**
```
~/Library/Application Support/ProjectAtlas/skills/
  jira/
    skill.json     ← manifest
    run            ← executable (chmod +x, any language)
  github/
    skill.json
    run
```

**`skill.json` manifest:**
```json
{
  "id": "jira",
  "name": "Jira",
  "version": "1.0.0",
  "description": "Search and create Jira issues",
  "author": "Your Name",
  "actions": [
    {
      "name": "search",
      "description": "Search Jira issues by JQL query",
      "permission_level": "read",
      "action_class": "read",
      "parameters": {
        "type": "object",
        "properties": {
          "query": { "type": "string", "description": "JQL query string" }
        },
        "required": ["query"]
      }
    }
  ]
}
```

Actions register as `<id>.<name>` — e.g. `jira.search`. This matches the built-in naming convention (`weather.current`, `browser.navigate`).

**Subprocess protocol — one JSON line in, one JSON line out:**
```
stdin:  {"action": "search", "args": {"query": "bug priority=high"}}
stdout: {"success": true, "output": "Found 12 issues: ..."}
stdout: {"success": false, "error": "connection refused"}   ← on error
```

- Process is spawned fresh per call, killed after 30s timeout (same as built-ins)
- Working directory is the plugin's own folder — relative paths work
- Environment variables pass through — credentials can live in `.env` or the OS keychain
- `action_class` in the manifest is respected by the approval system — declaring `external_side_effect` triggers normal approval flow
- Output is size-limited to 1 MB before passing to the model

**HTTP routes (planned — not yet implemented):**
```
GET    /skills/custom          — list installed custom skills
POST   /skills/install         — install from local path or URL
DELETE /skills/:id             — remove custom skill directory
```

---

## Where to Add Things

| Task | File(s) to edit |
| --- | --- |
| New **core** built-in skill | Create `internal/skills/<name>.go` + call `r.register<Name>()` in `NewRegistry` + add to `builtInSkills()` in `internal/features/skills.go` |
| New **custom** skill | Create `~/...ProjectAtlas/skills/<id>/skill.json` + `run` executable. Appears automatically after daemon restart. |
| New HTTP route | Add handler to appropriate `internal/domain/<file>.go` + register in `Register(r chi.Router)` |
| New config field | `internal/config/snapshot.go` + `Defaults()` function |
| New credential field | `internal/creds/bundle.go` `Bundle` struct + update `domain/control.go` `storeAPIKey` mapping |
| New web UI screen | `atlas-web/src/screens/<Name>.tsx` + route in `atlas-web/src/App.tsx` + types/methods in `atlas-web/src/api/contracts.ts` + `atlas-web/src/api/client.ts` |
| New Forge skill type | `internal/forge/types.go` |
| New storage table | `internal/storage/db.go` `createSchema()` + add query methods |
| Add a log entry | Call `logstore.Write(level, message, meta)` — visible at `GET /logs` |
| Extend diary context | `internal/features/diary.go` — `DiaryContext` is injected into system prompt by `chat/service.go` |

---

## Build Commands

```bash
# Go runtime
cd Atlas/atlas-runtime
go mod tidy && go build ./...              # verify — clean = no output
go vet ./...                               # linter — clean = no output
go build -o Atlas ./cmd/atlas-runtime      # build binary
./Atlas -port 1984 -web-dir ../atlas-web/dist   # run locally (dev)

# TUI
cd Atlas/atlas-tui
go build -o atlas .                        # build binary

# Web UI
cd Atlas/atlas-web
npm install                                # first time only
npm run dev                                # dev server (hot reload)
npm run build                              # production build → dist/

# Install everything as daemon + deploy (from Atlas/atlas-runtime)
make install                               # build all, deploy, load launchd daemon
make daemon-start / daemon-stop / daemon-restart
make daemon-status                         # launchctl print — PID, state, exit code
make daemon-logs                           # tail -f ~/Library/Logs/Atlas/runtime.log
make uninstall                             # unload daemon, remove installed files
```

**Installed locations:**
- Runtime daemon: `~/Library/Application Support/Atlas/Atlas` (label: `Atlas`, plist: `~/Library/LaunchAgents/Atlas.plist`)
- Web assets: `~/Library/Application Support/Atlas/web/`
- TUI: `~/.local/bin/atlas`
- Logs: `~/Library/Logs/Atlas/runtime.log` + `runtime-error.log`

---

## Critical Conventions

**Skills**
- Register in `internal/skills/registry.go` `NewRegistry()` + list in `internal/features/skills.go` `builtInSkills()`.
- Permission levels: `"read"` (auto-approve), `"draft"` (requires approval), `"execute"` (requires approval unless policy overrides).
- Read-only credential fetches in skill `Fn` bodies — call `creds.Read()` inline; don't cache.

**HTTP routes**
- All routes live inside the `RequireSession` middleware group in `server/router.go` except `/auth/*` and `/web/*`.
- Localhost requests (no `Origin` header) bypass session auth — same as Swift runtime.
- SSE streams set `Content-Type: text/event-stream` and flush on every write.
- 204 No Content is valid — web client handles empty bodies.

**Keychain**
- Credential bundle: `security find-generic-password -s com.projectatlas.credentials -a bundle -w`
- All secrets are in one JSON bundle read by `internal/creds/bundle.go`.
- Custom/third-party keys live under `customSecrets` in the bundle — no code change needed.

**Config**
- Shared file: `~/Library/Application Support/ProjectAtlas/config.json` (`RuntimeConfigSnapshot`).
- Go-only sidecar: `~/Library/Application Support/ProjectAtlas/go-runtime-config.json` (`GoRuntimeConfig`).
- Atomic writes: temp file → rename. Never write directly to the config path.

**Forge**
- `POST /forge/proposals` runs AI research in a background goroutine; returns 202 immediately with a `ResearchingItem`.
- Proposals persist in `forge-proposals.json`; installed skills in `forge-installed.json`.
- `forge.BuildInstalledRecord(proposal)` converts a proposal into the `SkillRecord` shape for `GET /forge/installed`.

**Approvals**
- `POST /approvals/{toolCallID}/approve` takes the tool call ID, not the approval record ID.
- Approval resolution calls `chatSvc.Resume(toolCallID, approved)` in a goroutine.

**Dashboards**
- Read routes (`GET /dashboards/proposals`, `GET /dashboards/installed`) are native.
- Mutating routes (POST create/install/reject/pin/access/widgets/execute) return 501 — **deferred to V1.0 rewrite**.

**Communications**
- `GET /communications`, `GET /communications/channels`, `PUT /communications/platforms/:platform`, `POST /communications/platforms/:platform/validate`.
- Telegram and Discord platform lifecycle managed by `internal/comms/`.

---

## Data Files (Application Support)

| File | Written by | Purpose |
| --- | --- | --- |
| `config.json` | Go runtime + web UI | `RuntimeConfigSnapshot` |
| `go-runtime-config.json` | Go runtime | Go-only sidecar config |
| `atlas.sqlite3` | Go runtime | Conversations, messages, memories, gremlin runs |
| `MIND.md` | User / AI / mind.reflection | System prompt for the agent — updated each turn by the reflection pipeline |
| `SKILLS.md` | User / AI / mind.skills | Skills-layer memory — learned routines written after repeated tool sequences |
| `DIARY.md` | Go runtime / diary.record | Per-day diary entries (max 3/day) |
| `GREMLINS.md` | User / web UI | Automation definitions |
| `workflow-definitions.json` | Web UI | Workflow definitions |
| `workflow-runs.json` | Go runtime | Workflow run records |
| `forge-proposals.json` | Go runtime | Forge proposal records |
| `forge-installed.json` | Go runtime | Installed forge skill records |
| `go-skill-states.json` | Go runtime | Skill enable/disable overrides |
| `action-policies.json` | Web UI / approvals | Per-action approval policies |
| `fs-roots.json` | Web UI | Approved filesystem roots |
| `api-validation-history.json` | Validate gate | API validation audit log |

All files live in `~/Library/Application Support/ProjectAtlas/`.
