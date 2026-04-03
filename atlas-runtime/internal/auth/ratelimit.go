package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// RemoteAuthLimiter is a simple sliding-window rate limiter keyed by IP address.
// Default policy: 5 attempts per IP per minute.
// Intended for use on the /auth/remote login endpoint only.
type RemoteAuthLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	window   time.Duration
	limit    int
}

// NewRemoteAuthLimiter creates a limiter with the default policy (5/min).
func NewRemoteAuthLimiter() *RemoteAuthLimiter {
	return &RemoteAuthLimiter{
		attempts: make(map[string][]time.Time),
		window:   time.Minute,
		limit:    5,
	}
}

// Allow returns true if the IP is within the rate limit, false if throttled.
// A denied attempt still counts toward the window.
func (l *RemoteAuthLimiter) Allow(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	// Prune timestamps outside the window.
	times := l.attempts[ip]
	fresh := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}

	allowed := len(fresh) < l.limit
	l.attempts[ip] = append(fresh, now)
	return allowed
}

// Middleware wraps h with IP-based rate limiting, writing 429 on breach.
func (l *RemoteAuthLimiter) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteIP(r)
		if !l.Allow(ip) {
			writeError(w, http.StatusTooManyRequests,
				"Too many login attempts. Please wait a minute and try again.")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// remoteIP extracts the real client IP from the request, preferring
// X-Forwarded-For (set by reverse proxies) over RemoteAddr.
func remoteIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		return host[:idx]
	}
	return host
}
