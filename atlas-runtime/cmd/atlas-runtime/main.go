// Command Atlas is the Atlas Go runtime — the full replacement for AtlasRuntimeService.
// It serves the complete Atlas HTTP API natively in Go. No Swift backend is required.
//
//	Atlas [-port 1984] [-web-dir ./web]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"atlas-runtime-go/internal/agent"
	"atlas-runtime-go/internal/auth"
	"atlas-runtime-go/internal/browser"
	"atlas-runtime-go/internal/chat"
	"atlas-runtime-go/internal/comms"
	"atlas-runtime-go/internal/config"
	"atlas-runtime-go/internal/domain"
	"atlas-runtime-go/internal/engine"
	"atlas-runtime-go/internal/forge"
	"atlas-runtime-go/internal/logstore"
	"atlas-runtime-go/internal/mind"
	"atlas-runtime-go/internal/runtime"
	"atlas-runtime-go/internal/server"
	"atlas-runtime-go/internal/skills"
	"atlas-runtime-go/internal/storage"
)

func main() {
	portFlag := flag.Int("port", 0, "Override the HTTP port (default: value from config.json)")
	webDirFlag := flag.String("web-dir", "", "Path to the built atlas-web/dist directory")
	flag.Parse()

	// ── Config ────────────────────────────────────────────────────────────────
	cfgStore := config.NewStore()
	cfg := cfgStore.Load()

	port := cfg.RuntimePort
	if *portFlag > 0 {
		port = *portFlag
	}

	// ── First-run seeding ─────────────────────────────────────────────────────
	// Seed MIND.md and SKILLS.md if they don't exist yet. No-ops on subsequent runs.
	if err := mind.InitMindIfNeeded(config.SupportDir()); err != nil {
		log.Printf("Atlas: warn: seed MIND.md: %v", err)
	}
	if err := mind.InitSkillsIfNeeded(config.SupportDir()); err != nil {
		log.Printf("Atlas: warn: seed SKILLS.md: %v", err)
	}

	// ── Database ──────────────────────────────────────────────────────────────
	dbPath := config.DBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		log.Fatalf("Atlas: create support dir: %v", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("Atlas: open database: %v", err)
	}
	defer db.Close()

	// ── Services ──────────────────────────────────────────────────────────────
	authSvc := auth.NewService(db)
	runtimeSvc := runtime.NewService(port)
	bc := chat.NewBroadcaster()

	// Browser manager — launched lazily on first browser.* skill call.
	// Runs headless by default; set browserShowWindow: true in go-runtime-config.json
	// to open a visible Chrome window (useful for debugging or demos).
	goCfg := config.LoadGoConfig()
	browserMgr := browser.New(db, !goCfg.BrowserShowWindow)
	defer browserMgr.Close()

	skillsRegistry := skills.NewRegistry(config.SupportDir(), db, browserMgr)

	// Load user-installed custom skills from ~/Library/Application Support/ProjectAtlas/skills/.
	// Non-fatal: a broken custom skill never prevents Atlas from starting.
	skillsRegistry.LoadCustomSkills(config.SupportDir())

	// Wire vision inference into the skills registry so browser.solve_captcha
	// can call the active AI provider directly from within a skill function.
	// The closure re-resolves the provider on every call so runtime config
	// changes (e.g. switching from OpenAI to Anthropic) are picked up immediately.
	skillsRegistry.SetVisionFn(func(ctx context.Context, imageB64, prompt string) (string, error) {
		prov, err := chat.ResolveProvider(cfgStore.Load())
		if err != nil {
			return "", fmt.Errorf("vision: no AI provider configured: %w", err)
		}
		return agent.CallVision(ctx, prov, imageB64, prompt)
	})

	// Engine LM — bundled llama-server subprocess manager.
	engineMgr := engine.NewManager(config.AtlasInstallDir(), config.ModelsDir())

	// Phase 3 Tool Router — second llama-server instance on AtlasEngineRouterPort (default 11986).
	// Shares the same binary and models dir as the primary engine; just a different port + model.
	routerMgr := engine.NewManager(config.AtlasInstallDir(), config.ModelsDir())

	engineMgr.SetIdleTimeout(60 * time.Minute)  // eject primary model after 60 min idle
	routerMgr.SetIdleTimeout(12 * time.Hour)    // eject router model after 12 hr idle

	chatSvc := chat.NewService(db, cfgStore, bc, skillsRegistry)
	chatSvc.SetEngineManager(engineMgr)
	chatSvc.SetRouterEngineManager(routerMgr)
	commsSvc := comms.New(cfgStore, db)
	forgeSvc := forge.NewService(config.SupportDir())

	// Wire forge.orchestration.propose → forge service.
	skillsRegistry.SetForgePersistFn(func(specJSON, plansJSON, summary, rationale, contractJSON string) (
		id, name, skillID, riskLevel string,
		actionNames, domains []string,
		err error,
	) {
		return forgeSvc.PersistProposalFromJSON(specJSON, plansJSON, summary, rationale, contractJSON)
	})

	// Wire gremlin.run_now → chat service.
	skillsRegistry.SetRunAutomationFn(func(ctx context.Context, gremlinID, prompt string) (string, error) {
		resp, err := chatSvc.HandleMessage(ctx, chat.MessageRequest{
			Message: prompt,
		})
		if err != nil {
			return "", err
		}
		if resp.Response.ErrorMessage != "" {
			return "", fmt.Errorf("%s", resp.Response.ErrorMessage)
		}
		return resp.Response.AssistantMessage, nil
	})

	// Wire approval resolver to Telegram bridge (allows inline approve/deny buttons).
	commsSvc.SetApprovalResolver(func(toolCallID string, approved bool) error {
		go chatSvc.Resume(toolCallID, approved)
		return nil
	})

	// Wire chat handler to comms bridges.
	// This is the single mapping point between comms.BridgeRequest and chat.MessageRequest.
	// When chat.MessageRequest gains a new field, add it to comms.BridgeRequest and map it here.
	commsSvc.SetChatHandler(func(ctx context.Context, req comms.BridgeRequest) (string, string, error) {
		chatAttachments := make([]chat.MessageAttachment, len(req.Attachments))
		for i, a := range req.Attachments {
			chatAttachments[i] = chat.MessageAttachment{Filename: a.Filename, MimeType: a.MimeType, Data: a.Data}
		}
		resp, err := chatSvc.HandleMessage(ctx, chat.MessageRequest{
			Message:        req.Text,
			ConversationID: req.ConvID,
			Platform:       req.Platform,
			Attachments:    chatAttachments,
		})
		if err != nil {
			return "", "", err
		}
		if resp.Response.ErrorMessage != "" {
			return "", "", fmt.Errorf("%s", resp.Response.ErrorMessage)
		}
		return resp.Response.AssistantMessage, resp.Conversation.ID, nil
	})
	commsSvc.Start()
	defer commsSvc.Stop()

	// Dream cycle — nightly memory consolidation (prune, merge, diary synthesis, MIND refresh).
	// Uses the heavy background provider (cloud fast model by default; local router when
	// AtlasEngineRouterForAll is enabled) for quality-sensitive consolidation work.
	dreamStop := mind.StartDreamCycle(config.SupportDir(), db, cfgStore,
		func() (agent.ProviderConfig, error) {
			return chat.ResolveHeavyBackgroundProvider(cfgStore.Load())
		})
	defer dreamStop()

	// ── Web UI directory ──────────────────────────────────────────────────────
	webDir := *webDirFlag
	if webDir == "" {
		webDir = resolveWebDir()
	}
	if webDir != "" {
		log.Printf("Atlas: web UI at %s", webDir)
	} else {
		log.Printf("Atlas: web UI not found (use -web-dir to specify path)")
	}

	// ── Domain handlers ───────────────────────────────────────────────────────
	authDomain := domain.NewAuthDomain(authSvc, cfgStore, webDir, port)
	authDomain.EnsureRemoteKey() // Generate initial key if Keychain has none.
	controlDomain := domain.NewControlDomain(cfgStore, runtimeSvc, db, engineMgr)
	chatDomain := domain.NewChatDomain(chatSvc, bc, db)
	approvalsDomain := domain.NewApprovalsDomain(db, config.SupportDir())
	commsDomain := domain.NewCommunicationsDomain(commsSvc)
	featuresDomain := domain.NewFeaturesDomain(config.SupportDir(), db, chatSvc, bc, forgeSvc)
	engineDomain := domain.NewEngineDomain(engineMgr, routerMgr, cfgStore)

	// Wire approval resolution → agent loop resumption.
	approvalsDomain.OnResolve = func(toolCallID, status string) {
		approved := status == "approved"
		go chatSvc.Resume(toolCallID, approved)
	}

	// ── HTTP server ───────────────────────────────────────────────────────────
	remoteEnabled := func() bool { return cfgStore.Load().RemoteAccessEnabled }
	tailscaleEnabled := func() bool { return cfgStore.Load().TailscaleEnabled }

	handler := server.BuildRouter(
		authDomain,
		controlDomain,
		chatDomain,
		approvalsDomain,
		commsDomain,
		featuresDomain,
		engineDomain,
		authSvc,
		remoteEnabled,
		tailscaleEnabled,
	)

	addr := fmt.Sprintf("0.0.0.0:%d", port)

	runtimeSvc.MarkStarted()
	logstore.Write("info", fmt.Sprintf("Runtime started on port %d", port),
		map[string]string{"provider": cfg.ActiveAIProvider})
	log.Printf("Atlas: listening on http://%s", addr)
	log.Printf("Atlas: config at %s", config.ConfigPath())
	log.Printf("Atlas: database at %s", dbPath)
	log.Printf("Atlas: all domains native — no Swift backend required")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Atlas: server error: %v", err)
	}
}

// resolveWebDir attempts to find the atlas-web/dist directory relative to
// the binary or the current working directory.
func resolveWebDir() string {
	candidates := []string{
		filepath.Join(filepath.Dir(os.Args[0]), "web"),
		filepath.Join("Atlas", "atlas-web", "dist"),
		filepath.Join("..", "atlas-web", "dist"),
		filepath.Join("..", "..", "atlas-web", "dist"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "index.html")); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}
