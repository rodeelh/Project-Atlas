package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// tailscaleNet is the pre-parsed Tailscale CGNAT range (100.64.0.0/10).
// Computed once at package init so IsTailscaleIP never allocates per-call.
var tailscaleNet *net.IPNet

func init() {
	_, tailscaleNet, _ = net.ParseCIDR("100.64.0.0/10")
}

// RequireSession is a chi middleware that enforces the Atlas session model:
//   - Requests from localhost with no Origin header (native URLSession / macOS app) — bypass auth.
//   - Requests from a Tailscale IP when tailscaleEnabled() — bypass auth entirely.
//     Tailscale's cryptographic device identity is the trust mechanism; no Atlas token needed.
//   - All other remote requests (LAN) require an Atlas remote session via /auth/remote-gate.
//
// NOTE: browsers omit the Origin header on same-origin GET requests, so we must
// also inspect r.Host to distinguish a local browser from a LAN browser.
func RequireSession(svc *Service, tailscaleEnabled func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			isLocalHost := isLocalhostHost(r.Host)

			// Native client or same-origin local browser — bypass auth.
			if origin == "" && isLocalHost {
				next.ServeHTTP(w, r)
				return
			}

			// Tailscale devices bypass Atlas session auth when Tailscale is enabled.
			if tailscaleEnabled != nil && tailscaleEnabled() && isTailscaleRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Tailscale is disabled (or the bypass above didn't fire) — explicitly
			// reject Tailscale IPs regardless of any session they may hold.
			// This closes the window where a Tailscale user authenticates via the
			// LAN key after the Tailscale toggle is turned off mid-session.
			if isTailscaleRequest(r) {
				writeError(w, http.StatusForbidden, "Tailscale access is not enabled.")
				return
			}

			// All other remote requests (LAN) require a valid Atlas session.
			sessionID := SessionIDFromCookie(r.Header.Get("Cookie"))
			if !svc.ValidateSession(sessionID) {
				writeError(w, http.StatusUnauthorized,
					"Not authenticated. Open Atlas on the host Mac or visit /auth/remote-gate.")
				return
			}

			// Non-localhost requests require a remote session specifically.
			isRemoteReq := !isLocalHost || (origin != "" && !isLocalhostOrigin(origin))
			if isRemoteReq {
				sess := svc.SessionDetail(sessionID)
				if sess == nil || !sess.IsRemote {
					writeError(w, http.StatusUnauthorized,
						"Remote access requires authentication via /auth/remote-gate.")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// LanGate is a chi middleware that rejects non-localhost requests when remote
// access is disabled. Tailscale connections are allowed through regardless of
// the LAN toggle when tailscaleEnabled() returns true.
// Returns a browser-friendly HTML page for navigation requests so the user
// sees a clear message rather than raw JSON.
func LanGate(remoteEnabled func() bool, tailscaleEnabled func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isLocalhostHost(r.Host) && !remoteEnabled() {
				// Tailscale connections bypass the LAN gate — they have their own trust model.
				if tailscaleEnabled != nil && tailscaleEnabled() && isTailscaleRequest(r) {
					next.ServeHTTP(w, r)
					return
				}
				if strings.Contains(r.Header.Get("Accept"), "text/html") {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, lanDisabledHTML())
					return
				}
				writeError(w, http.StatusForbidden,
					"Remote access is not enabled. Enable it in Atlas Settings.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func lanDisabledHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Atlas — Remote Access Disabled</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<style>
:root{
  --bg:#0a0a0a;--surface:#111111;--surface-2:#181818;
  --border:rgba(255,255,255,0.07);--border-2:rgba(255,255,255,0.13);
  --text:#f0f0f0;--text-2:#888888;--shadow:0 12px 30px rgba(0,0,0,0.32);
}
@media(prefers-color-scheme:light){
  :root{
    --bg:#eceae6;--surface:#f7f5f1;--surface-2:#ffffff;
    --border:rgba(32,24,16,0.12);--border-2:rgba(32,24,16,0.2);
    --text:#171411;--text-2:#5f5850;--shadow:0 12px 30px rgba(0,0,0,0.10);
  }
}
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
body{
  font-family:'Inter',-apple-system,'Helvetica Neue',system-ui,sans-serif;
  background:var(--bg);color:var(--text);
  display:flex;align-items:center;justify-content:center;
  min-height:100vh;padding:20px;
}
.card{
  background:var(--surface);border:1px solid var(--border);
  border-radius:16px;padding:40px 36px;max-width:380px;width:100%;
  box-shadow:var(--shadow);text-align:center;
}
h1{font-size:1.125rem;font-weight:600;letter-spacing:-0.01em;margin-bottom:6px;color:var(--text)}
.subtitle{font-size:.875rem;color:var(--text-2);margin-bottom:24px;line-height:1.6}
.notice{
  background:var(--surface-2);border:1px solid var(--border);
  border-radius:10px;padding:16px;text-align:left;
}
.notice p{font-size:.825rem;color:var(--text-2);line-height:1.6}
.notice strong{color:var(--text);font-weight:500}
</style>
</head>
<body>
<div class="card">
  <h1>Remote Access Disabled</h1>
  <p class="subtitle">This Atlas runtime is not currently<br>accepting remote connections.</p>
  <div class="notice">
    <p>To enable access, open <strong>Atlas</strong> on the host Mac and go to
    <strong>Settings &rarr; Remote Access</strong>, then turn on
    <strong>LAN Access</strong>.</p>
  </div>
</div>
</body>
</html>`
}

// isLocalhostOrigin returns true if origin is http://localhost:* or http://127.0.0.1:*.
func isLocalhostOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1")
}

// CanonicalHost strips any port and IPv6 brackets so host comparisons are stable
// across localhost forms such as localhost:1984, 127.0.0.1:1984, and [::1]:1984.
func CanonicalHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// IsLocalhostHost returns true if host refers to the loopback address.
func IsLocalhostHost(host string) bool {
	switch CanonicalHost(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// isLocalhostHost is the package-local alias retained for internal call sites.
func isLocalhostHost(host string) bool {
	return IsLocalhostHost(host)
}

// isTailscaleRequest returns true if the request originates from a Tailscale
// node IP (100.64.0.0/10 CGNAT range assigned by Tailscale).
func isTailscaleRequest(r *http.Request) bool {
	ip := requestIP(r)
	if ip == "" {
		return false
	}
	return IsTailscaleIP(ip)
}

// IsTailscaleAddr reports whether addr (host:port or bare IP) is a Tailscale IP.
// Exported so other packages (e.g. domain) can reuse without reimplementing.
func IsTailscaleAddr(addr string) bool {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return IsTailscaleIP(host)
	}
	return IsTailscaleIP(addr)
}

// IsTailscaleIP reports whether ipStr is in the Tailscale CGNAT range 100.64.0.0/10.
// Uses the package-level pre-parsed net to avoid per-call allocation.
func IsTailscaleIP(ipStr string) bool {
	if tailscaleNet == nil {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return tailscaleNet.Contains(ip)
}

// requestIP extracts the bare IP address from r.RemoteAddr.
func requestIP(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
