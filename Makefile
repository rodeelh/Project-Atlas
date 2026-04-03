.PHONY: build build-tui build-web \
        install uninstall \
        run dev \
        daemon-start daemon-stop daemon-restart daemon-status daemon-logs \
        download-engine engine-update \
        tidy clean check

RUNTIME_DIR  := atlas-runtime
TUI_DIR      := atlas-tui
WEB_DIR      := atlas-web

BINARY       := Atlas
TUI_BINARY   := atlas
DAEMON_LABEL := Atlas
PLIST_TMPL   := $(RUNTIME_DIR)/com.atlas.runtime.plist.tmpl

# ── Atlas Engine LM ───────────────────────────────────────────────────────────
# Pinned llama.cpp release. Override with: make install LLAMA_VERSION=bXXXX
LLAMA_VERSION ?= b8641

# ── Build ─────────────────────────────────────────────────────────────────────

build:
	cd $(RUNTIME_DIR) && go build -o $(BINARY) ./cmd/atlas-runtime

build-tui:
	cd $(TUI_DIR) && go build -o $(TUI_BINARY) .

build-web:
	cd $(WEB_DIR) && npm run build

tidy:
	cd $(RUNTIME_DIR) && go mod tidy
	cd $(TUI_DIR) && go mod tidy

clean:
	rm -f $(RUNTIME_DIR)/$(BINARY)
	rm -f $(TUI_DIR)/$(TUI_BINARY)

# ── Run (dev) ─────────────────────────────────────────────────────────────────

run: build
	$(RUNTIME_DIR)/$(BINARY) -web-dir $(WEB_DIR)/dist

dev: build
	$(RUNTIME_DIR)/$(BINARY) -port 1985 -web-dir $(WEB_DIR)/dist

# ── Daemon ────────────────────────────────────────────────────────────────────
#
# install  — build all components, deploy to ~/Library/Application Support/Atlas/,
#            write plist to ~/Library/LaunchAgents/, load daemon (idempotent).
# uninstall — unload daemon, remove plist and installed files (data preserved).

download-engine:
	@mkdir -p "$$HOME/Library/Application Support/Atlas/engine"
	@if [ ! -f "$$HOME/Library/Application Support/Atlas/engine/llama-server" ]; then \
		echo "→ Downloading llama-server $(LLAMA_VERSION) for $$(uname -m)..."; \
		ARCH=$$(uname -m); \
		ZIP="llama-$(LLAMA_VERSION)-bin-macos-$$ARCH.zip"; \
		URL="https://github.com/ggerganov/llama.cpp/releases/download/$(LLAMA_VERSION)/$$ZIP"; \
		curl -L --progress-bar -o /tmp/llama-engine.tar.gz "$$URL" || { echo "✗ llama-server download failed — Engine LM will not be available"; rm -f /tmp/llama-engine.tar.gz; exit 0; }; \
		mkdir -p /tmp/llama-extract && \
		tar -xzf /tmp/llama-engine.tar.gz -C /tmp/llama-extract 2>/dev/null; \
		EXTRACTED=$$(ls /tmp/llama-extract/); \
		cp /tmp/llama-extract/$$EXTRACTED/llama-server "$$HOME/Library/Application Support/Atlas/engine/llama-server" || \
			{ echo "✗ Could not extract llama-server from archive"; rm -rf /tmp/llama-extract /tmp/llama-engine.tar.gz; exit 0; }; \
		cp /tmp/llama-extract/$$EXTRACTED/*.dylib "$$HOME/Library/Application Support/Atlas/engine/" 2>/dev/null; \
		chmod +x "$$HOME/Library/Application Support/Atlas/engine/llama-server"; \
		rm -rf /tmp/llama-extract /tmp/llama-engine.tar.gz; \
		echo "✓ llama-server $(LLAMA_VERSION) + shared libraries ready"; \
	else \
		echo "→ llama-server already installed ($(LLAMA_VERSION)) — use 'make engine-update' to upgrade"; \
	fi

engine-update:
	@echo "→ Removing existing llama-server and shared libraries..."
	@rm -f "$$HOME/Library/Application Support/Atlas/engine/llama-server"
	@rm -f "$$HOME/Library/Application Support/Atlas/engine/"*.dylib
	@$(MAKE) download-engine LLAMA_VERSION=$(LLAMA_VERSION)

install: build build-tui build-web download-engine
	@echo "→ Installing TUI..."
	@mkdir -p "$$HOME/.local/bin"
	cp $(TUI_DIR)/$(TUI_BINARY) "$$HOME/.local/bin/$(TUI_BINARY)"
	@echo "→ Installing runtime binary and web assets..."
	@mkdir -p "$$HOME/Library/Application Support/Atlas"
	cp $(RUNTIME_DIR)/$(BINARY) "$$HOME/Library/Application Support/Atlas/$(BINARY)"
	rsync -a --delete $(WEB_DIR)/dist/ "$$HOME/Library/Application Support/Atlas/web/"
	@echo "→ Creating log directory..."
	@mkdir -p "$$HOME/Library/Logs/Atlas"
	@echo "→ Installing plist..."
	@mkdir -p "$$HOME/Library/LaunchAgents"
	sed "s|__HOME__|$$HOME|g" $(PLIST_TMPL) \
		> "$$HOME/Library/LaunchAgents/$(DAEMON_LABEL).plist"
	@echo "→ Stopping any running Atlas process on port 1984..."
	@-lsof -ti tcp:1984 | xargs kill 2>/dev/null; true
	@echo "→ Loading daemon (unloading first if already loaded)..."
	@-launchctl unload "$$HOME/Library/LaunchAgents/$(DAEMON_LABEL).plist" 2>/dev/null; true
	launchctl load -w "$$HOME/Library/LaunchAgents/$(DAEMON_LABEL).plist"
	@echo "✓ Atlas daemon installed and running on port 1984"

uninstall:
	@echo "→ Unloading daemon..."
	@-launchctl unload -w "$$HOME/Library/LaunchAgents/$(DAEMON_LABEL).plist" 2>/dev/null; true
	@-rm -f "$$HOME/Library/LaunchAgents/$(DAEMON_LABEL).plist"
	@echo "→ Removing installed files..."
	@-rm -f "$$HOME/Library/Application Support/Atlas/$(BINARY)"
	@-rm -rf "$$HOME/Library/Application Support/Atlas/web"
	@-rm -f "$$HOME/.local/bin/$(TUI_BINARY)"
	@echo "✓ Uninstalled (data in ~/Library/Application Support/ProjectAtlas preserved)"

daemon-start:
	launchctl start $(DAEMON_LABEL)

daemon-stop:
	launchctl stop $(DAEMON_LABEL)

daemon-restart: daemon-stop daemon-start

daemon-status:
	launchctl print gui/$$(id -u)/$(DAEMON_LABEL)

daemon-logs:
	tail -f "$$HOME/Library/Logs/Atlas/runtime.log"

# ── Lint ──────────────────────────────────────────────────────────────────────

check:
	cd $(RUNTIME_DIR) && go fmt ./... && go vet ./...
	cd $(TUI_DIR) && go fmt ./... && go vet ./...
