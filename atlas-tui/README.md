# atlas-tui

A terminal UI for [Project Atlas](https://github.com/ralhassan/atlas) — a secondary client for the local Atlas runtime.

## Requirements

- Go 1.22+
- Atlas runtime running on port 1984 (`atlas-runtime`)

## Install

```bash
cd atlas-tui
go build -o atlas-tui .
./atlas-tui
```

## Usage

On first launch, atlas-tui walks you through onboarding — setting up your AI provider keys, chat integrations, and permissions. After that it drops into the main interface.

The TUI is optional. The web UI remains the primary product surface.

### Key bindings

| Key | Action |
|-----|--------|
| `1` / `2` / `3` | Switch between Chat / Status / Settings tabs |
| `tab` | Cycle tabs |
| `enter` | Send message / confirm |
| `ctrl+c` | Quit |
| `r` | Refresh (Status tab) |
| `c` | Clear logs (Status tab) |
| `s` | Save settings (Settings tab) |
| `↑↓` | Scroll / navigate |
| `space` | Toggle boolean setting |

## Configuration

Config is stored at `~/.config/atlas-tui/config.json`.

Environment overrides:
- `ATLAS_BASE_URL` — override the daemon URL (default: `http://localhost:1984`)
- `ATLAS_TOKEN` — auth token (not needed for localhost)

## Architecture

```
atlas-tui/
├── main.go            — entry point
├── client/            — HTTP + SSE client for the Atlas daemon API
├── config/            — TUI config (base URL, onboarding state)
├── onboarding/        — onboarding flow data and helpers
└── ui/
    ├── app.go         — root BubbleTea model, tab routing
    ├── chat.go        — chat screen + full onboarding flow
    ├── status.go      — daemon status + log viewer (polls every 3s)
    ├── settings.go    — editable runtime config
    └── styles.go      — lipgloss color palette and styles
```
