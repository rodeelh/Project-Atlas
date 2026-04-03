// Package auth implements the Atlas web authentication model:
// HMAC-SHA256 signed launch tokens + 7-day session cookies.
// The security model mirrors WebAuthService.swift exactly.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"atlas-runtime-go/internal/storage"
)

// Sentinel errors returned by VerifyLaunchToken.
var (
	ErrInvalidToken = errors.New("invalid launch token")
	ErrExpiredToken = errors.New("launch token has expired")
	ErrAlreadyUsed  = errors.New("launch token has already been used")
)

const (
	SessionCookieName     = "atlas_session"
	tokenLifetime         = 60 * time.Second
	sessionLifetime       = 7 * 24 * time.Hour // local browser sessions
	remoteSessionLifetime = 24 * time.Hour      // remote sessions expire sooner
)

// Session is an active browser session.
type Session struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time
	IsRemote  bool
}

// IsValid reports whether the session has not yet expired.
func (s *Session) IsValid() bool { return time.Now().Before(s.ExpiresAt) }

// Service implements token issuance and session lifecycle.
// It is safe for concurrent use.
type Service struct {
	mu         sync.Mutex
	signingKey []byte
	sessions   map[string]*Session
	usedNonces map[string]struct{}
	db         *storage.DB
}

// NewService creates a Service with a fresh in-process signing key.
// Sessions validated at creation time survive daemon restarts because the
// SQLite store is consulted on cache miss (same model as WebAuthService.swift).
func NewService(db *storage.DB) *Service {
	key := make([]byte, 32)
	rand.Read(key)
	return &Service{
		signingKey: key,
		sessions:   make(map[string]*Session),
		usedNonces: make(map[string]struct{}),
		db:         db,
	}
}

// IssueLaunchToken issues a short-lived signed launch token.
// Format: base64url(payloadJSON).base64url(HMAC-SHA256)
// This matches the Swift WebAuthService.issueLaunchToken() format exactly.
func (s *Service) IssueLaunchToken() string {
	type payload struct {
		Exp    float64 `json:"exp"`
		Nonce  string  `json:"nonce"`
		Source string  `json:"source"`
	}
	p := payload{
		Exp:    float64(time.Now().Add(tokenLifetime).UnixNano()) / 1e9,
		Nonce:  newUUID(),
		Source: "menubar",
	}
	payloadJSON, _ := json.Marshal(p)
	payloadB64 := b64url(payloadJSON)

	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payloadB64))
	sig := mac.Sum(nil)

	return payloadB64 + "." + b64url(sig)
}

// VerifyLaunchToken verifies a raw launch token string.
// On success the nonce is consumed so it cannot be replayed.
func (s *Service) VerifyLaunchToken(raw string) error {
	parts := splitToken(raw)
	if len(parts) != 2 {
		return ErrInvalidToken
	}
	payloadB64, sigB64 := parts[0], parts[1]

	// Verify HMAC-SHA256 using constant-time comparison.
	sigBytes, err := b64urlDecode(sigB64)
	if err != nil {
		return ErrInvalidToken
	}
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payloadB64))
	expected := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expected) {
		return ErrInvalidToken
	}

	// Decode payload.
	type payload struct {
		Exp   float64 `json:"exp"`
		Nonce string  `json:"nonce"`
	}
	payloadBytes, err := b64urlDecode(payloadB64)
	if err != nil {
		return ErrInvalidToken
	}
	var p payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return ErrInvalidToken
	}

	// Check expiry.
	if float64(time.Now().UnixNano())/1e9 >= p.Exp {
		return ErrExpiredToken
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check one-time nonce.
	if _, used := s.usedNonces[p.Nonce]; used {
		return ErrAlreadyUsed
	}
	s.usedNonces[p.Nonce] = struct{}{}
	s.pruneNonces()

	return nil
}

// CreateSession creates a new browser session and persists it to SQLite.
// Local sessions last 7 days; remote sessions last 24 hours.
func (s *Service) CreateSession(isRemote bool) *Session {
	now := time.Now()
	lifetime := sessionLifetime
	if isRemote {
		lifetime = remoteSessionLifetime
	}
	sess := &Session{
		ID:        randomHex(32),
		CreatedAt: now,
		ExpiresAt: now.Add(lifetime),
		IsRemote:  isRemote,
	}

	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.pruneExpiredSessions()
	s.mu.Unlock()

	go s.db.SaveWebSession(sess.ID, sess.CreatedAt, sess.ExpiresAt, isRemote)
	return sess
}

// ValidateSession returns true if the session ID is known and not expired.
// Consults the SQLite store on cache miss (after daemon restart).
func (s *Service) ValidateSession(id string) bool {
	if id == "" {
		return false
	}

	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		valid := sess.IsValid()
		if !valid {
			delete(s.sessions, id)
			go s.db.DeleteWebSession(id)
		}
		s.mu.Unlock()
		if valid {
			go s.db.RefreshWebSession(id)
		}
		return valid
	}
	s.mu.Unlock()

	// Cache miss — consult SQLite (happens once per restart per session).
	rec, err := s.db.FetchWebSession(id)
	if err != nil || rec == nil {
		return false
	}
	sess := &Session{
		ID:        rec.ID,
		CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt,
		IsRemote:  rec.IsRemote,
	}
	if !sess.IsValid() {
		return false
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	go s.db.RefreshWebSession(id)
	return true
}

// SessionDetail returns the full Session for id, or nil if invalid.
func (s *Service) SessionDetail(id string) *Session {
	if !s.ValidateSession(id) {
		return nil
	}
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	return sess
}

// InvalidateAllRemoteSessions removes all remote sessions (e.g. API key rotation).
func (s *Service) InvalidateAllRemoteSessions() {
	s.mu.Lock()
	for id, sess := range s.sessions {
		if sess.IsRemote {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
	go s.db.DeleteAllRemoteWebSessions()
}

// ValidateAPIKey performs a constant-time comparison of the presented key
// against the stored remote access API key.
func ValidateAPIKey(presented, stored string) bool {
	if presented == "" || stored == "" {
		return false
	}
	return hmac.Equal([]byte(presented), []byte(stored))
}

// SessionSetCookieValue returns the Set-Cookie header value for a session.
// Matches WebAuthService.sessionSetCookieValue(for:).
func SessionSetCookieValue(sess *Session) string {
	sameSite := "Strict"
	if sess.IsRemote {
		sameSite = "Lax"
	}
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	return fmt.Sprintf(
		"%s=%s; HttpOnly; SameSite=%s; Path=/; Max-Age=%d",
		SessionCookieName, sess.ID, sameSite, maxAge,
	)
}

// SessionIDFromCookie extracts the atlas_session value from a Cookie header.
func SessionIDFromCookie(cookieHeader string) string {
	for len(cookieHeader) > 0 {
		var pair string
		pair, cookieHeader, _ = strings.Cut(cookieHeader, ";")
		pair = strings.TrimSpace(pair)
		k, v, _ := strings.Cut(pair, "=")
		if strings.TrimSpace(k) == SessionCookieName {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ── Private helpers ───────────────────────────────────────────────────────────

func (s *Service) pruneExpiredSessions() {
	for id, sess := range s.sessions {
		if !sess.IsValid() {
			delete(s.sessions, id)
		}
	}
}

func (s *Service) pruneNonces() {
	if len(s.usedNonces) > 500 {
		count := 0
		for k := range s.usedNonces {
			delete(s.usedNonces, k)
			count++
			if count >= 250 {
				break
			}
		}
	}
}

func b64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func splitToken(raw string) []string {
	// Split on last dot only (payload may contain dots via base64url padding)
	idx := -1
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] == '.' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	return []string{raw[:idx], raw[idx+1:]}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
