# Atlas Runtime API v1 Draft

This is the first versioned runtime API contract draft for Project Atlas.

Status: draft compatibility contract.

It describes the current Atlas runtime contract that the web UI and companion clients rely on.

Primary sources:

- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/domain`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/domain)
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-web/src/api/client.ts`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-web/src/api/client.ts)

## Contract goals

- Preserve current web UI behavior as the runtime evolves.
- Give Atlas clients a stable compatibility target.
- Separate stable API behavior from incidental implementation details.

## Versioning rule

- This document defines `v1`.
- The current runtime does not yet expose an explicit `v1` path prefix.
- Compatibility should be judged against `v1` behavior even if the live routes remain unprefixed.
- If a breaking change is needed later, it should become an explicit `v2` contract rather than an untracked route drift.

## Transport model

- Transport: HTTP/1.1 JSON API plus SSE for message streaming.
- Encoding: JSON using the runtime encoder in `AtlasJSON`.
- Authentication:
  - local native bootstrap via launch token and session cookie
  - remote browser access via remote access key and session cookie
- Static web shell is served from `/web`.

## Contract rules

### Rule 1: the runtime is the source of truth

The web UI and native shell are clients. Runtime config, onboarding state, sessions, approvals, conversations, and communications state must be runtime-owned.

### Rule 2: secrets never leave the secret layer unnecessarily

`/api-keys` exposes presence and key names, not raw provider secrets.

### Rule 3: onboarding is a runtime concern

Onboarding completion is part of the shared runtime config state and must remain readable and writable by the web UI.

### Rule 4: route groups migrate together

Go migration units should follow contract groups such as auth, config, approvals, communications, chat, and workflows rather than arbitrary package boundaries.

## Stable route groups in v1

### Auth and session bootstrap

Required behavior:

- trusted local clients can mint a launch token
- a browser can exchange a launch token for a session cookie
- remote access uses a separate login gate and remote key flow
- remote session revocation is explicit

Core routes:

- `GET /auth/token`
- `GET /auth/bootstrap`
- `GET /auth/remote-gate`
- `POST /auth/remote`
- `GET /auth/remote-status`
- `GET /auth/remote-key`
- `DELETE /auth/remote-sessions`

### Runtime status and config

Required behavior:

- the web app can poll runtime state and logs
- config reads and writes use a shared snapshot model
- config writes return both updated config and restart impact

Core routes:

- `GET /status`
- `GET /logs`
- `GET /config`
- `PUT /config`
- `GET /onboarding`
- `PUT /onboarding`

Required payload expectations:

- `/status` must include runtime state, port, pending approvals, and communications snapshot
- `/config` must round-trip the fields in `RuntimeConfigSnapshot`
- `/onboarding` must expose `{ "completed": boolean }`

### Chat and conversation history

Required behavior:

- send-message requests work without the web app having to understand internal agent orchestration
- streaming remains SSE-based for incremental assistant progress
- conversation history remains queryable

Core routes:

- `POST /message`
- `GET /message/stream`
- `GET /conversations`
- `GET /conversations/search`
- `GET /conversations/{id}`

### Approvals and policies

Required behavior:

- pending approvals are listable
- approvals are resolved by `toolCallID`
- policy changes return the full updated policy map

Core routes:

- `GET /approvals`
- `POST /approvals/{toolCallID}/approve`
- `POST /approvals/{toolCallID}/deny`
- `GET /action-policies`
- `PUT /action-policies/{actionID}`

### Credentials and communications

Required behavior:

- provider credential presence can be queried without leaking values
- communication platforms can be validated before enablement
- communication state is returned as a normalized snapshot

Core routes:

- `GET /api-keys`
- `POST /api-keys`
- `DELETE /api-keys`
- `POST /api-keys/invalidate-cache`
- `GET /communications`
- `GET /communications/channels`
- `GET /communications/platforms/{platform}/setup`
- `PUT /communications/platforms/{platform}`
- `POST /communications/platforms/{platform}/validate`

### Operator domains

These are already part of the current runtime surface and should remain grouped for migration:

- memories
- skills
- mind and skills memory
- automations
- workflows
- forge
- dashboards

## Compatibility baseline in code

The current codebase now has a compatibility baseline in the Go runtime and web contract layer:

- route ownership and handler shapes in [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/domain`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/domain)
- auth/session behavior in [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/auth`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/auth)
- current web-facing request and payload contracts in [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-web/src/api/contracts.ts`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-web/src/api/contracts.ts)
- current web client transport usage in [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-web/src/api/client.ts`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-web/src/api/client.ts)
  - onboarding state round-trips
  - config update restart semantics
  - remote access status and auth bootstrap
  - remote API-key authentication
  - API-key presence/status responses
  - communication setup value reads
  - communication validation and enable/disable mutation behavior
  - conversation summaries, search, and detail reads
  - approval listing and deny lifecycle for deferred execution
  - action-policy mutation responses returning the updated policy map
  - approval-linked chat/SSE lifecycle for denied and resumed conversations
  - message failure behavior, including conversation ID preservation and reusable conversation creation
  - `MIND.md`, `SKILLS.md`, and memory create/list routes preserving runtime-owned document semantics
  - workflow definition create/update/delete, workflow run execution, and workflow run history reads
  - dashboard proposal creation, install lifecycle, pinning, access tracking, and removal behavior
  - forge researching state, proposal create/list/reject flows, install-enable behavior, installed skill listing, and uninstall behavior

This is now a broad compatibility baseline we can preserve while the runtime changes underneath.

The current contract artifacts now also include:

- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/docs/runtime-api-inventory.md`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/docs/runtime-api-inventory.md) for human-readable grouping
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/docs/runtime-api-manifest.json`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/docs/runtime-api-manifest.json) for a machine-readable route baseline
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-web/src/api/contracts.ts`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-web/src/api/contracts.ts) for the current web-facing contract layer

## What remains before v1 is strong

- reduce route-local response structs inside `AgentRuntime`
- derive web client types from shared contracts or generated schema where practical
- make versioning explicit at the API boundary if the unprefixed live routes need a future breaking change
