# Atlas Storage Design

This document defines the Atlas storage architecture for the current Go runtime and its cross-platform direction.

It is intentionally practical. The goal is to keep storage portable, secure, and easy to evolve as Atlas expands beyond macOS.

## Goals

- Make runtime configuration portable across macOS, Windows, and Linux.
- Keep secrets in OS-backed secure storage rather than app-managed files.
- Preserve a clean separation between human-authored guidance and machine-owned state.
- Let the runtime own the storage contracts.
- Support incremental migration from the current Swift storage layer.

## Storage Rules

- Use Markdown for human-authored guidance and intent only.
- Use structured storage for real runtime configuration.
- Keep secrets out of Markdown entirely.
- Do not use platform preference systems like `UserDefaults` as the long-term source of truth for portable runtime config.

## Canonical Split

Atlas should use four storage classes:

1. Human guidance
   - Example: `MIND.md`
   - Purpose: user-authored instructions, intent, goals, notes
   - Format: Markdown

2. Runtime config
   - Example: `config.json`
   - Purpose: machine-validated configuration that controls runtime behavior
   - Format: TOML

3. Operational state
   - Example: SQLite databases
   - Purpose: logs, sessions, approvals, memory, workflow state, caches, and other runtime-owned records
   - Format: SQLite or other structured machine storage

4. Secrets
   - Example: provider API keys, remote access secrets, channel tokens
   - Purpose: credential material that must not be stored in normal config or docs
   - Format: OS-native secret backend behind a `SecretStore` interface

## ConfigStore

### Decision

The canonical Atlas runtime config store should be a file-backed JSON document.

Recommended file name:

- `config.json`

Recommended ownership:

- The runtime owns the schema.
- The web UI and native companion shells read and write config through runtime APIs, not by writing the file directly.

### Why JSON

- Already matches the live runtime implementation
- Easy for the web UI and runtime to round-trip without translation layers
- Stable for typed machine-owned configuration
- Supported everywhere without extra parsing ambiguity

### What belongs in `config.json`

- runtime port
- onboarding completion state
- enabled providers and channels
- model selections
- feature flags
- local mode / remote mode toggles
- non-secret user preferences
- paths and other runtime options

### What does not belong in `config.json`

- API keys
- tokens
- session secrets
- remote access credentials
- encrypted blobs managed by the app itself

### ConfigStore requirements

- One canonical config file per Atlas runtime instance
- Schema version field in the file
- Strong typed validation on load
- Atomic writes using temp-file-then-rename semantics
- Restricted file permissions where the OS supports them
- Default values applied in code, not by tolerating malformed files
- Clear migration support for renamed or deprecated fields

### Path strategy

Use the standard app-data/config location for the host platform rather than app-only preference APIs.

Suggested targets:

- macOS: `~/Library/Application Support/ProjectAtlas/config.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/project-atlas/config.json`
- Windows: `%AppData%\\ProjectAtlas\\config.json`

The exact path should be provided by a `PathProvider` or equivalent platform abstraction.

## SecretStore

### Decision

Atlas secrets should live behind a portable `SecretStore` interface, with each platform using the native OS secret system underneath.

### Primary backend by platform

- macOS: Keychain
- Windows: Credential Manager or a DPAPI-backed implementation
- Linux desktop: Secret Service / libsecret
- Linux headless/server: explicit alternative backend, environment injection, or managed secret provider depending on deployment mode

### SecretStore design requirements

- The runtime depends on an interface, not a specific OS API
- Secrets are stored as individual named entries rather than one large application blob where possible
- Read, write, delete, and existence checks should be explicit
- Backends must avoid logging secret values
- Secret identifiers should be stable across runtimes and shells
- Secret access failures should produce actionable errors without exposing secret content

### Recommended interface shape

At a minimum:

- `Get(name)`
- `Set(name, value)`
- `Delete(name)`
- `Has(name)`

Optional if needed later:

- `ListNames()`
- `GetMany(names)`

### Why not store secrets in files

- weaker OS-level protection
- harder rotation and auditing
- greater accidental exposure risk through backups, logs, and support artifacts
- encourages config/secrets sprawl

If Atlas ever needs a non-native fallback for unsupported environments, it should be treated as an explicit secondary backend with strong warnings and a separate threat-model review.

## Recommended Runtime Ownership

The runtime should own the storage contracts:

- `ConfigStore`
- `SecretStore`
- `PathProvider`

The native companion app should not own the canonical storage model. It may call runtime APIs or use a shared backend adapter during migration, but it should not define storage behavior.

## Current Runtime State

### Current state

- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/config/snapshot.go`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/config/snapshot.go) defines the live `RuntimeConfigSnapshot`
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/config/store.go`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/config/store.go) implements atomic JSON-backed config persistence
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/config/paths.go`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/config/paths.go) defines the current support-dir layout
- [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/creds`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/creds) and [`/Users/ralhassan/Desktop/CODING/Project Atlas/Atlas/atlas-runtime/internal/comms/keychain.go`](/Users/ralhassan/Desktop/CODING/Project%20Atlas/Atlas/atlas-runtime/internal/comms/keychain.go) use macOS Keychain-backed storage for secrets today

### Direction

1. Keep `RuntimeConfigSnapshot` as the conceptual config schema baseline.
2. Keep JSON as the canonical runtime config format unless a later compatibility reason justifies a change.
3. Introduce a real `SecretStore` abstraction above the current keychain implementation.
4. Keep Keychain as the first concrete backend while defining the interface for Windows and Linux.
5. Gradually move secret storage away from a single bundle entry toward stable per-secret records if the migration cost is acceptable.

## Near-Term Implementation Plan

1. Define storage interfaces in a runtime-owned package boundary.
2. Keep the current JSON-backed config implementation and isolate it behind runtime-owned interfaces.
3. Add a `SecretStore` protocol and wrap the current macOS keychain behavior behind it.
4. Move all callers to the interfaces rather than direct `UserDefaults` or Keychain assumptions.
5. Add migration logic only when introducing a second secret backend or a cross-platform path strategy.
6. Keep macOS as the first validated platform while ensuring path names, secret identifiers, and interfaces are cross-platform from the start.

## Non-Goals

- Storing secrets in Markdown
- Making the native menu bar app the owner of storage contracts
- Replacing secure OS secret systems with custom encryption by default
- Collapsing config, state, and secrets into one storage backend
