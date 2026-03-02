// Package handlers provides HTTP middleware and helpers: request ID
// propagation, security headers, fixed-window rate limiting, and
// delegations to shared httputil utilities.
package handlers

import (
	"net/http"
	"sync"
	"time"

	"imsub/internal/platform/httputil"
)

// --- Request ID (delegated to httputil) ---

// RequestIDMiddleware injects or propagates an X-Request-Id header
// and stores the value in the request context.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := httputil.RequestID(r)
		if rid == "" {
			rid = httputil.NewRequestID()
			r.Header.Set("X-Request-Id", rid)
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := httputil.WithRequestID(r.Context(), rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- Security headers ---

// SecurityHeaders wraps a handler to set standard security response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

// --- Rate limiter ---

// FixedWindowRateLimiter enforces per-IP and global request rate limits
// within a fixed time window.
type FixedWindowRateLimiter struct {
	mu          sync.Mutex
	windowStart time.Time
	globalCount int
	perIP       map[string]int
	globalLimit int
	perIPLimit  int
	window      time.Duration
}

// NewFixedWindowRateLimiter creates a rate limiter with the given global and
// per-IP limits within the specified window.
func NewFixedWindowRateLimiter(globalLimit, perIPLimit int, window time.Duration) *FixedWindowRateLimiter {
	return &FixedWindowRateLimiter{
		windowStart: time.Now(),
		perIP:       make(map[string]int),
		globalLimit: globalLimit,
		perIPLimit:  perIPLimit,
		window:      window,
	}
}

// Allow reports whether a request from ip is permitted within the current window.
func (l *FixedWindowRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.windowStart) >= l.window {
		l.windowStart = now
		l.globalCount = 0
		l.perIP = make(map[string]int, len(l.perIP))
	}

	if l.globalCount >= l.globalLimit {
		return false
	}
	if l.perIP[ip] >= l.perIPLimit {
		return false
	}

	l.globalCount++
	l.perIP[ip]++
	return true
}

// RateLimit wraps a handler with the rate limiter, returning 429 when exceeded.
func RateLimit(l *FixedWindowRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httputil.ClientIP(r)
		if !l.Allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many requests. Wait about a minute, then try again.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
