package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/auth"
	"atlas-runtime-go/internal/config"
)

// AuthDomain handles auth bootstrap, web static serving, and CORS preflight.
//
// Routes owned (all auth-exempt — registered outside the RequireSession group):
//
//	OPTIONS  *                     — CORS preflight
//	GET      /                     — redirect → /web
//	GET      /web                  — web UI static
//	GET      /web/*                — web UI static assets
//	GET      /auth/token           — issue launch token (native app)
//	GET      /auth/bootstrap       — exchange token → session cookie → /web
//	GET      /auth/ping            — diagnostic HTML ping
//	GET      /auth/remote-gate     — remote login page HTML
//	POST     /auth/remote          — API key auth → session cookie → /web
//
// Auth-required (registered inside RequireSession group):
//
//	GET      /auth/remote-status   — LAN access info (lanIP, accessURL, port)
//	GET      /auth/remote-key      — remote access key (authenticated)
//	POST     /auth/remote-key      — rotate remote access key (authenticated)
//	DELETE   /auth/remote-sessions — revoke all remote sessions + rotate key
type AuthDomain struct {
	svc      *auth.Service
	cfgStore *config.Store
	webDir   string // path to atlas-web/dist for static serving
	limiter  *auth.RemoteAuthLimiter
	port     int // resolved listen port (from -port flag or config)
}

// NewAuthDomain creates an AuthDomain.
// webDir is the path to the built web UI directory (e.g. atlas-web/dist).
// port is the resolved HTTP listen port (may differ from cfg.RuntimePort when -port flag is used).
func NewAuthDomain(svc *auth.Service, cfgStore *config.Store, webDir string, port int) *AuthDomain {
	return &AuthDomain{
		svc:      svc,
		cfgStore: cfgStore,
		webDir:   webDir,
		port:     port,
		limiter:  auth.NewRemoteAuthLimiter(),
	}
}

// EnsureRemoteKey generates and stores a remote access key if none currently
// exists in the Keychain. Call once at startup.
func (d *AuthDomain) EnsureRemoteKey() {
	cfg := d.cfgStore.Load()
	if key := readRemoteAccessKey(cfg); key != "" {
		return // already present
	}
	newKey, err := generateAndStoreRemoteKey()
	if err != nil {
		log.Printf("Atlas: warning — could not generate remote access key: %v", err)
		return
	}
	log.Printf("Atlas: generated initial remote access key (len=%d)", len(newKey))
}

// RegisterPublic mounts auth-exempt routes on the root router.
// Call this BEFORE applying RequireSession middleware.
func (d *AuthDomain) RegisterPublic(r chi.Router) {
	r.Options("/*", d.preflight)
	r.Get("/", d.rootRedirect)
	r.Get("/web", d.serveWeb)
	r.Get("/web/*", d.serveWeb)
	r.Get("/auth/token", d.getToken)
	r.Get("/auth/bootstrap", d.bootstrap)
	r.Get("/auth/ping", d.ping)
	r.Get("/auth/remote-gate", d.remoteGate)
	// /auth/remote is rate-limited
	r.Post("/auth/remote", d.limiter.Middleware(http.HandlerFunc(d.remoteAuth)).ServeHTTP)
}

// Register mounts auth-required routes. Call inside a RequireSession group.
func (d *AuthDomain) Register(r chi.Router) {
	r.Get("/auth/remote-status", d.remoteStatus)
	r.Get("/auth/remote-key", d.remoteKey)
	r.Post("/auth/remote-key", d.rotateRemoteKey)
	r.Delete("/auth/remote-sessions", d.revokeRemoteSessions)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (d *AuthDomain) preflight(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (d *AuthDomain) rootRedirect(w http.ResponseWriter, r *http.Request) {
	// Tailscale devices go straight to /web — no token required.
	// LAN devices without a valid remote session are sent to the auth gate.
	// Localhost always goes straight to /web.
	if isRemoteHost(r.Host) {
		cfg := d.cfgStore.Load()
		if cfg.TailscaleEnabled && auth.IsTailscaleAddr(r.RemoteAddr) {
			http.Redirect(w, r, "/web", http.StatusFound)
			return
		}
		sessionID := auth.SessionIDFromCookie(r.Header.Get("Cookie"))
		sess := d.svc.SessionDetail(sessionID)
		if sess == nil || !sess.IsRemote {
			http.Redirect(w, r, "/auth/remote-gate", http.StatusFound)
			return
		}
	}
	http.Redirect(w, r, "/web", http.StatusFound)
}

func (d *AuthDomain) serveWeb(w http.ResponseWriter, r *http.Request) {
	if d.webDir == "" {
		http.Error(w, "Web UI not configured. Run: cd atlas-web && npm run build", http.StatusNotFound)
		return
	}

	urlPath := r.URL.Path

	// For remote (non-localhost) requests serving the SPA root:
	//   - Tailscale devices load the SPA directly — no token needed.
	//   - LAN devices without a valid remote session are redirected to the gate.
	isSPARoot := urlPath == "/web" || urlPath == "/web/" || urlPath == "/web/index.html"
	if isSPARoot && isRemoteHost(r.Host) {
		cfg := d.cfgStore.Load()
		if cfg.TailscaleEnabled && auth.IsTailscaleAddr(r.RemoteAddr) {
			// Tailscale — serve SPA directly.
		} else {
			sessionID := auth.SessionIDFromCookie(r.Header.Get("Cookie"))
			sess := d.svc.SessionDetail(sessionID)
			if sess == nil || !sess.IsRemote {
				http.Redirect(w, r, "/auth/remote-gate", http.StatusFound)
				return
			}
		}
	}

	if urlPath == "/web" || urlPath == "/web/" {
		urlPath = "/web/index.html"
	}

	// Keep lookups rooted under d.webDir. We normalize to a relative path after
	// cleaning so requests like /web/../../etc/passwd cannot escape the bundle.
	fsPath := strings.TrimPrefix(urlPath, "/web")
	relPath := strings.TrimPrefix(filepath.Clean("/"+fsPath), string(os.PathSeparator))
	if relPath == "" {
		relPath = "index.html"
	}

	filePath := filepath.Join(d.webDir, relPath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// SPA fallback — serve index.html for unrecognised paths.
		filePath = filepath.Join(d.webDir, "index.html")
	}

	http.ServeFile(w, r, filePath)
}

// isRemoteHost returns true if host is NOT a loopback address.
func isRemoteHost(host string) bool {
	return !auth.IsLocalhostHost(host)
}

func (d *AuthDomain) getToken(w http.ResponseWriter, r *http.Request) {
	token := d.svc.IssueLaunchToken()
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (d *AuthDomain) bootstrap(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "Missing 'token' query parameter.")
		return
	}
	if err := d.svc.VerifyLaunchToken(token); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	sess := d.svc.CreateSession(false)
	w.Header().Set("Set-Cookie", auth.SessionSetCookieValue(sess))
	http.Redirect(w, r, "/web", http.StatusFound)
}

func (d *AuthDomain) ping(w http.ResponseWriter, r *http.Request) {
	html := fmt.Sprintf(`<html><body style='font-family:monospace;padding:32px'>
<h2>✓ Atlas Go runtime is reachable</h2>
<p>Runtime: atlas-runtime</p>
<p>Time: %s</p>
<script>document.write('<p>JS works ✓</p><p>Origin: '+location.origin+'</p><p>Host: '+location.host+'</p>')</script>
</body></html>`, nowRFC3339())
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, html)
}

func (d *AuthDomain) remoteGate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, remoteGateHTML())
}

func (d *AuthDomain) remoteAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		log.Printf("Atlas: remote login rejected — missing key (ip=%s)", remoteClientIP(r))
		writeError(w, http.StatusBadRequest, "Missing remote access key.")
		return
	}
	cfg := d.cfgStore.Load()
	if !cfg.RemoteAccessEnabled {
		log.Printf("Atlas: remote login rejected — remote access disabled (ip=%s)", remoteClientIP(r))
		writeError(w, http.StatusForbidden, "Remote access is not enabled.")
		return
	}
	// Prevent Tailscale IPs from obtaining a LAN session when Tailscale is
	// disabled. Without this check a Tailscale user could use the LAN key to
	// bypass the Tailscale toggle by creating a session that RequireSession
	// would otherwise accept.
	if auth.IsTailscaleAddr(r.RemoteAddr) && !cfg.TailscaleEnabled {
		log.Printf("Atlas: remote login rejected — Tailscale access disabled (ip=%s)", remoteClientIP(r))
		writeError(w, http.StatusForbidden, "Tailscale access is not enabled.")
		return
	}
	storedKey := readRemoteAccessKey(cfg)
	if !auth.ValidateAPIKey(key, storedKey) {
		log.Printf("Atlas: remote login rejected — invalid key (ip=%s)", remoteClientIP(r))
		writeError(w, http.StatusUnauthorized, "Invalid remote access key.")
		return
	}
	sess := d.svc.CreateSession(true)
	w.Header().Set("Set-Cookie", auth.SessionSetCookieValue(sess))
	log.Printf("Atlas: remote session created (ip=%s, expires=%s)", remoteClientIP(r), sess.ExpiresAt.Format("2006-01-02 15:04:05 UTC"))
	// Return 200 JSON so the gate page JS can navigate to /web after the cookie
	// is reliably applied. Using 302 with fetch redirect:'manual' is unreliable
	// across browsers (opaque redirect cookie-setting behaviour varies).
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (d *AuthDomain) remoteStatus(w http.ResponseWriter, r *http.Request) {
	cfg := d.cfgStore.Load()
	// Resolved port: prefer the port passed at construction (from -port flag),
	// fall back to config value. cfg.RuntimePort is 0 when the binary is run
	// with -port flag without updating the config file.
	port := d.port
	if port == 0 {
		port = cfg.RuntimePort
	}
	lanIP := detectLANIP()
	var accessURL string
	if lanIP != "" && port > 0 {
		accessURL = fmt.Sprintf("http://%s:%d/auth/remote-gate", lanIP, port)
	}

	// Always detect the Tailscale IP so the UI can show "Tailscale detected —
	// enable it to use it" even when TailscaleEnabled is false. The endpoint is
	// auth-protected so this is intentional, not an accidental disclosure.
	tailscaleIP := detectTailscaleIP()
	var tailscaleURL string
	if tailscaleIP != "" && port > 0 && cfg.TailscaleEnabled {
		// Tailscale users land directly on the app — no gate needed.
		tailscaleURL = fmt.Sprintf("http://%s:%d", tailscaleIP, port)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"remoteAccessEnabled": cfg.RemoteAccessEnabled,
		"port":                port,
		"lanIP":               lanIP,
		"accessURL":           accessURL,
		"tailscaleEnabled":    cfg.TailscaleEnabled,
		"tailscaleIP":         tailscaleIP,
		"tailscaleURL":        tailscaleURL,
		"tailscaleConnected":  tailscaleIP != "",
	})
}

func (d *AuthDomain) remoteKey(w http.ResponseWriter, r *http.Request) {
	cfg := d.cfgStore.Load()
	key := readRemoteAccessKey(cfg)
	if key == "" {
		// Auto-generate if missing (e.g. first run after enabling remote access).
		var err error
		key, err = generateAndStoreRemoteKey()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not generate remote access key.")
			return
		}
		log.Printf("Atlas: auto-generated missing remote access key")
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

func (d *AuthDomain) rotateRemoteKey(w http.ResponseWriter, r *http.Request) {
	newKey, err := generateAndStoreRemoteKey()
	if err != nil {
		log.Printf("Atlas: key rotation failed: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not rotate remote access key.")
		return
	}
	log.Printf("Atlas: remote access key rotated")
	writeJSON(w, http.StatusOK, map[string]string{"key": newKey})
}

func (d *AuthDomain) revokeRemoteSessions(w http.ResponseWriter, r *http.Request) {
	d.svc.InvalidateAllRemoteSessions()
	log.Printf("Atlas: all remote sessions revoked")

	// Also rotate the key so revoked sessions cannot re-authenticate with the old key.
	newKey, err := generateAndStoreRemoteKey()
	if err != nil {
		log.Printf("Atlas: warning — key rotation after revoke failed: %v", err)
		// Still return success — sessions are revoked even if key rotation failed.
	} else {
		log.Printf("Atlas: remote access key rotated after session revoke")
		_ = newKey
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// readRemoteAccessKey reads the remote access API key from the Keychain.
func readRemoteAccessKey(_ config.RuntimeConfigSnapshot) string {
	out, err := execSecurityInDomain("find-generic-password",
		"-s", "com.projectatlas.remotekey",
		"-a", "remoteAccessKey",
		"-w",
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// generateAndStoreRemoteKey creates a cryptographically random 32-byte (64-char hex)
// key and stores it in the Keychain. Overwrites any existing key.
func generateAndStoreRemoteKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	key := hex.EncodeToString(b)

	// -U updates the item if it already exists.
	_, err := execSecurityInDomain("add-generic-password",
		"-s", "com.projectatlas.remotekey",
		"-a", "remoteAccessKey",
		"-w", key,
		"-U",
	)
	if err != nil {
		return "", fmt.Errorf("keychain write: %w", err)
	}
	return key, nil
}

// detectLANIP walks the host's network interfaces and returns the first
// private-range IPv4 address found on an active non-loopback interface.
func detectLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		// Skip loopback and down interfaces.
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if isPrivateIPv4(ip4) {
				return ip4.String()
			}
		}
	}
	return ""
}

// detectTailscaleIP returns the Tailscale IP (100.64.0.0/10) of this machine,
// or empty string if Tailscale is not running or not connected.
// Uses auth.IsTailscaleIP to avoid duplicating the range definition.
func detectTailscaleIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		// Skip loopback and down interfaces — mirrors detectLANIP behaviour.
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if auth.IsTailscaleIP(ip4.String()) {
				return ip4.String()
			}
		}
	}
	return ""
}

// isPrivateIPv4 returns true for addresses in RFC-1918 ranges:
// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16.
func isPrivateIPv4(ip net.IP) bool {
	private := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	for _, cidr := range private {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteClientIP extracts the best-effort client IP from a request.
func remoteClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		return host[:idx]
	}
	return host
}

func remoteGateHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Atlas — Remote Access</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<style>
:root{
  --bg:#0a0a0a;--surface:#111111;--surface-2:#181818;
  --border:rgba(255,255,255,0.07);--border-2:rgba(255,255,255,0.13);
  --text:#f0f0f0;--text-2:#888888;
  --accent:#4D86C8;--accent-hover:#3a73b5;
  --input-bg:#0a0a0a;--shadow:0 12px 30px rgba(0,0,0,0.32);
}
@media(prefers-color-scheme:light){
  :root{
    --bg:#eceae6;--surface:#f7f5f1;--surface-2:#ffffff;
    --border:rgba(32,24,16,0.12);--border-2:rgba(32,24,16,0.2);
    --text:#171411;--text-2:#5f5850;
    --input-bg:#ffffff;--shadow:0 12px 30px rgba(0,0,0,0.10);
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
  border-radius:16px;padding:40px 36px;max-width:400px;width:100%;
  box-shadow:var(--shadow);text-align:center;
}
h1{font-size:1.25rem;font-weight:600;letter-spacing:-0.01em;margin-bottom:6px;color:var(--text)}
.subtitle{font-size:.875rem;color:var(--text-2);margin-bottom:28px;line-height:1.5}
.field{margin-bottom:12px;text-align:left}
label{display:block;font-size:.8rem;font-weight:500;color:var(--text-2);margin-bottom:6px;letter-spacing:0.02em}
input[type=password]{
  width:100%;padding:11px 14px;
  background:var(--input-bg);color:var(--text);
  border:1px solid var(--border-2);border-radius:8px;
  font-family:inherit;font-size:.925rem;outline:none;
  transition:border-color .15s;
}
input[type=password]::placeholder{color:var(--text-2);opacity:.6}
input[type=password]:focus{border-color:var(--accent)}
button{
  width:100%;padding:11px 14px;margin-top:4px;
  background:var(--accent);color:#fff;
  border:none;border-radius:8px;
  font-family:inherit;font-size:.925rem;font-weight:500;
  cursor:pointer;transition:background .15s,opacity .15s;
}
button:hover:not(:disabled){background:var(--accent-hover)}
button:disabled{opacity:.55;cursor:not-allowed}
.err{
  display:none;margin-top:14px;padding:10px 14px;
  background:rgba(255,59,48,.08);border:1px solid rgba(255,59,48,.2);
  border-radius:8px;color:#ff3b30;font-size:.825rem;line-height:1.45;text-align:left;
}
@media(prefers-color-scheme:light){
  .err{background:rgba(255,59,48,.06);border-color:rgba(255,59,48,.15);color:#c0392b}
}
</style>
</head>
<body>
<div class="card">
  <h1>Atlas</h1>
  <p class="subtitle">Enter your remote access key to connect<br>to this runtime.</p>
  <form id="f" onsubmit="event.preventDefault();login()">
    <div class="field">
      <label for="k">Access Key</label>
      <input id="k" type="password" placeholder="Paste your remote access key" autocomplete="current-password" autofocus>
    </div>
    <button type="submit" id="btn">Connect</button>
    <div class="err" id="err"></div>
  </form>
</div>
<script>
async function login(){
  var k=document.getElementById('k').value.trim();
  if(!k)return;
  var btn=document.getElementById('btn');
  var err=document.getElementById('err');
  btn.disabled=true;btn.textContent='Connecting\u2026';
  err.style.display='none';
  try{
    var res=await fetch('/auth/remote',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({key:k})});
    if(res.ok){window.location='/web';return;}
    var j=await res.json().catch(function(){return{};});
    showErr(j.error||'Login failed ('+res.status+')');
  }catch(e){showErr('Network error. Check that Atlas is running on the host Mac.');}
  btn.disabled=false;btn.textContent='Connect';
}
function showErr(msg){var e=document.getElementById('err');e.textContent=msg;e.style.display='block';}
</script>
</body>
</html>`
}
