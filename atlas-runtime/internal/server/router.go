// Package server builds and configures the HTTP router.
package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"atlas-runtime-go/internal/auth"
	"atlas-runtime-go/internal/domain"
)

// BuildRouter constructs the chi router with CORS, auth middleware, and all
// registered domain handlers. The routing structure mirrors the Swift runtime's
// RuntimeHTTPHandler.route() auth-gate + handler-dispatch pattern.
func BuildRouter(
	authDomain *domain.AuthDomain,
	controlDomain *domain.ControlDomain,
	chatDomain *domain.ChatDomain,
	approvalsDomain *domain.ApprovalsDomain,
	commsDomain *domain.CommunicationsDomain,
	featuresDomain *domain.FeaturesDomain,
	engineDomain *domain.EngineDomain,
	authSvc *auth.Service,
	remoteEnabled func() bool,
	tailscaleEnabled func() bool,
) http.Handler {
	r := chi.NewRouter()

	// Request logger (dev-friendly output).
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// CORS — reflect the Origin back for trusted sources.
	// Using AllowedOrigins: ["*"] with AllowCredentials: true is spec-invalid
	// (browsers will reject it). Instead we use AllowOriginFunc to:
	//   • always allow localhost origins (local browser sessions)
	//   • allow any non-localhost origin when remote access is enabled
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			if strings.HasPrefix(origin, "http://localhost") ||
				strings.HasPrefix(origin, "http://127.0.0.1") {
				return true
			}
			if remoteEnabled() {
				return true
			}
			// Tailscale: only allow cross-origin requests that physically
			// originate from a Tailscale IP. Checking the Origin header alone
			// is insufficient — an attacker could forge Origin: http://100.x.x.x.
			// r.RemoteAddr is the actual TCP peer address and cannot be spoofed.
			return tailscaleEnabled() && auth.IsTailscaleAddr(r.RemoteAddr)
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Cookie", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// LAN gate — rejects non-localhost requests when remote access is disabled.
	// Tailscale connections bypass this gate when Tailscale is enabled.
	r.Use(auth.LanGate(remoteEnabled, tailscaleEnabled))

	// ── Auth-exempt routes ────────────────────────────────────────────────────
	// These must be registered BEFORE the RequireSession middleware group so
	// that the browser can reach /auth/bootstrap (token exchange) without a
	// session, and the menu bar app can reach /auth/token without one.
	authDomain.RegisterPublic(r)

	// ── Session-protected routes ──────────────────────────────────────────────
	r.Group(func(protected chi.Router) {
		protected.Use(auth.RequireSession(authSvc, tailscaleEnabled))

		// Auth-required auth routes (/auth/remote-status, /auth/remote-key, DELETE /auth/remote-sessions).
		authDomain.Register(protected)

		// All other domain routes.
		controlDomain.Register(protected)
		chatDomain.Register(protected)
		approvalsDomain.Register(protected)
		commsDomain.Register(protected)
		featuresDomain.Register(protected)
		engineDomain.Register(protected)
	})

	return r
}
