# Provider Modularization — Weak-Points Remediation Plan

## Context

Across the planning conversation we designed a provider-modularization story: fold providers into two frameworks (Cloud LM, Local LM), keep Anthropic/Gemini as native drivers, add OpenRouter, unify the local-runtime UI, and generalize today's primary/fast model split into primary/router slots with a supportive-local mode for the skill-selection router and background work.

An objective critique of that plan flagged several execution-level weaknesses. This plan file addresses only those weak points — it does not re-litigate the architectural direction, which is settled. The goal is to convert the strategic plan into something shippable in reviewable increments, with tests, concrete interfaces, and no silent regressions.

Exploration of `Atlas/atlas-runtime` revealed that several primitives the original critique assumed were missing **already exist**:

- **Atlas Engine and Ollama are already first-class `ProviderType` values** (`internal/agent/provider.go:31-40`) — not new providers to add.
- **Per-provider primary/fast pairs already exist** for every provider (`internal/config/snapshot.go:32-49`), e.g. `SelectedAnthropicModel` + `SelectedAnthropicFastModel`. The "cross-provider fast" regression concern from the critique is moot because fast was never cross-provider in the first place — it's always been a second pick within the active provider.
- **`ResolveBackgroundProvider` and `ResolveHeavyBackgroundProvider` already implement the supportive-local router pattern** (`internal/chat/keychain.go:89-154`), complete with a health-check ping against `http://127.0.0.1:{AtlasEngineRouterPort}/health` (default 11986) and an `AtlasEngineRouterForAll` flag gating the "all background work → Engine" behavior.
- **`migrateBundleCustomKeys` in `internal/domain/control.go:59-102`** is the idempotent-at-startup migration pattern to follow for any config reshaping.
- **No provider interface** exists — dispatch is a switch on `ProviderType` in `streamWithToolDetection` (`internal/agent/provider.go:141-159`). Anthropic routes to its own function; OpenAI/Gemini share `streamOpenAICompatWithToolDetection`; local backends (LM Studio, Ollama, Atlas Engine) fall through to a non-streaming OpenAI-compat path.

So the weak-points work becomes: harden, formalize, test, and fill the remaining gaps around primitives that already exist. Much smaller than "build two frameworks from scratch."

---

## Weak points being addressed

From the critique, in priority order:

1. No testing strategy for provider-resolution and router-fallback state machines.
2. No shippable staging — everything bundled into one large change.
3. Router-fallback semantics under-specified (per-call vs sticky, detection mode, timeout budget, user signal).
4. "Framework" abstraction asserted but never designed — no concrete Go interface.
5. OpenRouter integration tax unacknowledged (`HTTP-Referer`/`X-Title` headers, model-list shape, pricing metadata).
6. Model-list caching under-thought (TTL semantics, stale cache handling, invalidation).
7. Per-feature backend override question left open instead of explicitly decided.
8. Cost/usage telemetry impact unaddressed.
9. Chat-header badge polish conflated with core correctness work.

---

## Decisions (previously open questions, now closed)

- **Per-feature backend overrides:** No. One local backend serves all supportive/background calls. `ResolveBackgroundProvider` and `ResolveHeavyBackgroundProvider` already share a single Engine router; we keep that.
- **Chat-header badges:** Deferred. Not in M1–M4 scope. Revisit as a polish milestone after core work lands.
- **Cross-provider fast model:** Not a feature today; existing code pairs primary and fast within the active provider. No regression to worry about. Document this in the plan so it doesn't resurface.
- **Cost telemetry:** Explicitly out of scope. Call out in M2 that OpenRouter's per-model pricing is not tracked yet, and leave a TODO in the provider config struct for a future telemetry pass.
- **Model-list cache:** Per-daemon (not per-user — Atlas is single-user), in-memory with 24h TTL, persisted to `go-runtime-config.json` on write for restart survival, force-refresh via explicit UI button, stale picks fall through to free-text "Other…" path.
- **Router fallback policy:** Passive detection, per-turn sticky, reset at the start of each new chat turn. See §"Router fallback contract" below.

---

## Staging — independently shippable milestones

Each milestone is a PR-sized chunk that lands cleanly and leaves the system in a correct state.

### M1 — Resolver split + tests (pure refactor, no user-visible change)

**Goal:** Lock down the provider-resolution contract with tests before touching anything else.

- Rename nothing user-facing. Internally formalize the four resolution call sites:
  - `ResolveProvider` → primary-slot resolver (unchanged behavior).
  - `ResolveFastProvider` → stays as the second-pick-within-active-provider resolver (unchanged behavior).
  - `ResolveBackgroundProvider` → router-slot resolver: prefers Engine router via health check, falls back to `ResolveFastProvider`.
  - `ResolveHeavyBackgroundProvider` → reflection-slot resolver: Engine router only if `AtlasEngineRouterForAll`, else `ResolveFastProvider`.
- Add a doc comment block above each function in `internal/chat/keychain.go` stating the contract, fallback order, and the call sites that use it. This is the canonical reference.
- **Tests** (`internal/chat/keychain_test.go` — new file):
  - Primary resolution for each of the 6 provider types with and without keys set.
  - Fast resolution falls through to primary-model when `Selected*FastModel` is empty.
  - Background resolution calls the Engine health check, returns Engine config on 200, falls back on non-200/timeout.
  - Background resolution with `AtlasEngineRouterPort` unset uses the default 11986.
  - Heavy background resolution gated on `AtlasEngineRouterForAll`.
  - Health-check timeout is bounded (see router-fallback contract below).
- **Critical files:**
  - `internal/chat/keychain.go` (doc comments only)
  - `internal/chat/keychain_test.go` (new)
  - `internal/config/snapshot.go` (read-only reference)

**Ships independently. No behavior change. Establishes the safety net everything else depends on.**

### M2 — OpenRouter as a Cloud LM provider

**Goal:** Add OpenRouter with honest acknowledgment of its integration tax. No framework abstraction yet — add it as a sixth provider the same way the existing five are wired.

- Add `ProviderOpenRouter = "openrouter"` to `internal/agent/provider.go:31-40`.
- Add `SelectedOpenRouterModel` and `SelectedOpenRouterFastModel` fields to `RuntimeConfigSnapshot` in `internal/config/snapshot.go`, mirroring the existing per-provider pattern.
- Add `OpenRouterAPIKey string` to `Bundle` in `internal/creds/bundle.go:13-25`.
- Add `"openrouter"` → `openRouterAPIKey` mapping in `storeAPIKey` (`internal/domain/control.go:1292-1343`).
- Dispatch: OpenRouter is OpenAI-wire-compatible. In `streamWithToolDetection`, route it through `streamOpenAICompatWithToolDetection` with `BaseURL = "https://openrouter.ai/api/v1"`.
- **Integration tax, made explicit:**
  - Inject `HTTP-Referer: https://github.com/rodeelh/project-atlas` and `X-Title: Atlas` headers on every OpenRouter request. Add an `ExtraHeaders map[string]string` field to `ProviderConfig` (`internal/agent/provider.go:43-48`) so the OpenAI-compat streamer can carry them without hardcoding.
  - Model list endpoint: `GET https://openrouter.ai/api/v1/models`. Response includes `id`, `name`, `context_length`, `pricing`, and a popularity-ish sort field. Filter to top ~25 by whatever sort field is stable, plus an "Other…" free-text fallback.
  - Cost metadata is parsed but **not stored or acted on**. Leave a `// TODO: cost telemetry` comment at the parse site.
- **New HTTP endpoint:** `GET /providers/openrouter/models` returns the cached list. Lives in `internal/domain/control.go` or a new `internal/domain/providers.go`.
- **Model-list cache:**
  - In-memory struct in the domain layer: `{fetchedAt time.Time, models []OpenRouterModel}`.
  - 24h TTL. On miss or expiry, fetch synchronously (the Settings screen can await it; it's a rare path).
  - Persist last successful fetch into `go-runtime-config.json` as `OpenRouterModelCache` so daemon restarts don't force a refetch.
  - Force refresh: `GET /providers/openrouter/models?refresh=1`.
  - Stale picks: if the user's configured model is no longer in the list, the Settings UI shows it with a "(unverified)" badge but still lets them use it. No hard failure.
- **Settings UI** (`atlas-web/src/screens/Settings.tsx`): add an OpenRouter row in the provider picker and a `ModelPickerRows` entry for OpenRouter with a dropdown fed by the new endpoint + "Other…" free-text toggle.
- **Tests:**
  - Unit test for OpenRouter dispatch path (config → request headers include `HTTP-Referer` and `X-Title`).
  - Unit test for model-list cache (fresh fetch, TTL hit, TTL miss, refresh param bypasses cache).
  - Manual QA: set OpenRouter key, pick a model, send a chat turn, verify response.
- **Critical files:**
  - `internal/agent/provider.go`
  - `internal/config/snapshot.go`
  - `internal/creds/bundle.go`
  - `internal/domain/control.go`
  - `internal/domain/providers.go` (new, optional — can live in control.go)
  - `atlas-web/src/screens/Settings.tsx`
  - `atlas-web/src/api/contracts.ts`, `client.ts`

**Ships independently. Gives users OpenRouter. Validates the "new provider without a framework" cost as a baseline.**

### M3 — Router fallback contract hardening

**Goal:** Nail down the router-fallback semantics so the supportive-local mode is predictable under failure.

**Router fallback contract:**

- **Detection mode:** Passive on the request path + cheap active health check.
  - The existing health-check ping at `internal/chat/keychain.go:120` stays. Bound its timeout to **500 ms** (document and enforce in code). Any non-200 or timeout → fallback.
  - Add passive detection: if a background/router call to the Engine fails after resolution, record the failure in a per-turn sticky flag and fall back for the remainder of that turn.
- **Sticky scope:** Per chat turn. A turn is the agent loop invocation in `internal/chat/service.go` `HandleMessage`. Pass a `turnContext` struct (new, small) into the resolver call sites so the sticky flag lives on the turn, not on a package-level global.
- **Reset:** On each new `HandleMessage` call. No cross-turn state.
- **Retry budget:** Zero for the router slot — the whole point is latency. A single failed call → fallback for the turn.
- **User signal:** One `logstore.Write(warn, "router fallback: engine offline, using cloud fast model", meta)` **per fallback event, not per call**. Dedupe via the sticky flag. No user-visible chat message; the fallback is transparent.
- **Tests** (extend `internal/chat/keychain_test.go` and add `internal/chat/router_fallback_test.go`):
  - Health check returns 200 within 500 ms → Engine selected.
  - Health check returns 500 → fallback.
  - Health check hangs past 500 ms → fallback (timeout enforced).
  - Passive fallback: first call succeeds, second call fails → third call uses fallback within the same turn.
  - Sticky flag resets on new turn.
  - Exactly one warn log per fallback event per turn.
- **Critical files:**
  - `internal/chat/keychain.go`
  - `internal/chat/service.go` (turn context plumbing)
  - `internal/chat/router_fallback_test.go` (new)

**Ships independently. Hardens existing behavior; does not add new providers or UI.**

### M4 — UI unification for Local and Cloud

**Goal:** Settle the user-facing terminology without changing runtime semantics.

- Collapse the per-local-provider rows (LM Studio, Ollama, Atlas Engine) in `Settings.tsx` into a single "Local Model" card with a **Backend** selector. The backend selector sets `ActiveAIProvider` to the corresponding `ProviderType` — this is pure UI reshuffling over existing config fields.
- Atlas Engine is the default backend.
- Each backend still writes its own `Selected{Backend}Model` and `Selected{Backend}ModelFast` fields. No config migration needed because the fields already exist.
- The Cloud card keeps per-provider rows (OpenAI, Anthropic, Gemini, OpenRouter, Custom-OpenAI-compat) with their existing primary + fast dropdowns. Add a **"Same as primary"** one-click button next to the fast dropdown that copies the primary selection into the fast field. Helper text: "Pick a cheaper model to save cost, or use the same model for everything."
- Terminology locked: user-facing stays **"Primary model"** and **"Fast model."** Internal code/docs use **"primary slot"** and **"router slot."** Document this mapping in a short comment block at the top of `internal/chat/keychain.go`.
- Role selector on the Local card: **Solo** vs **Supportive**. This maps to the existing `AtlasEngineRouterForAll` flag plus (new) `ActiveAIProvider` being set to a cloud provider vs a local one. Specifically:
  - Solo = `ActiveAIProvider` is a local backend; `AtlasEngineRouterForAll` becomes meaningless (local is already everything).
  - Supportive = `ActiveAIProvider` is a cloud provider; the Local card is configured; `AtlasEngineRouterForAll` toggles whether heavy background work also goes to the router.
- No new backend config fields. The role selector is a UI derivation of existing state.
- **Tests:**
  - Playwright or equivalent not in scope; manual QA checklist in the PR description covering the 4 valid states from the earlier design.
- **Critical files:**
  - `atlas-web/src/screens/Settings.tsx`
  - `atlas-web/src/api/contracts.ts`
  - `internal/chat/keychain.go` (doc comment only)

**Ships independently. UI-only reshuffling. Runtime untouched.**

### M5 (deferred, not in this plan's scope) — Provider framework abstraction

The "Cloud LM framework / Local LM framework" Go interface. Only worth doing once M1–M4 are in and we have real-world feedback on whether the switch-statement dispatch is actually painful. Candidate interface sketch for the future:

```go
type ProviderDriver interface {
    Stream(ctx, ProviderConfig, []OAIMessage, []Tool, Emitter) (streamResult, error)
    ListModels(ctx, ProviderConfig) ([]Model, error)
}
```

Deliberately omitted from this plan — premature abstraction until at least one more provider after OpenRouter justifies it.

---

## Verification

**After M1:**
```bash
cd Atlas/atlas-runtime
go test ./internal/chat/...
go vet ./...
```
All new resolver tests pass. Zero behavior change in manual chat testing.

**After M2:**
```bash
go test ./internal/chat/... ./internal/domain/...
go build ./...
make install && make daemon-restart
```
In the web UI: add OpenRouter key, pick a model from the live-fetched list, send a chat turn, verify streaming response. Confirm `HTTP-Referer` and `X-Title` headers are sent via a `curl` intercept or temporary log line. Confirm cache persists across `make daemon-restart`.

**After M3:**
```bash
go test ./internal/chat/...
```
All router-fallback tests pass. Manual: start daemon with Engine unreachable, confirm chat turns still work using cloud fast model, confirm exactly one warn log per turn in `~/Library/Logs/Atlas/runtime.log`.

**After M4:**
Manual QA checklist in PR description walks through the 4 valid states (cloud-only, cloud+supportive, local-solo, invalid/both-unset) and confirms the Settings UI correctly represents each. Confirm "Same as primary" button copies the field. Confirm backend selector switches `ActiveAIProvider` correctly.

---

## Files touched (consolidated)

| File | Milestone |
|---|---|
| `internal/chat/keychain.go` | M1 (doc), M3 (fallback), M4 (doc) |
| `internal/chat/keychain_test.go` (new) | M1 |
| `internal/chat/service.go` | M3 (turn context) |
| `internal/chat/router_fallback_test.go` (new) | M3 |
| `internal/agent/provider.go` | M2 |
| `internal/config/snapshot.go` | M2 |
| `internal/creds/bundle.go` | M2 |
| `internal/domain/control.go` | M2 |
| `internal/domain/providers.go` (new, optional) | M2 |
| `atlas-web/src/screens/Settings.tsx` | M2, M4 |
| `atlas-web/src/api/contracts.ts` | M2, M4 |
| `atlas-web/src/api/client.ts` | M2 |

## Explicitly out of scope

- Provider framework Go interface (deferred to M5, not planned here).
- Cost/usage telemetry for OpenRouter (TODO comment only).
- Chat header model badges (polish, defer).
- Cross-provider fast model (never existed, no regression).
- Config schema versioning (existing JSON-defaults pattern is sufficient; `migrateBundleCustomKeys` is the precedent if ever needed).
- Custom drop-in provider folders (aspirational, not this work).
