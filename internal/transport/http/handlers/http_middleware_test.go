package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"imsub/internal/platform/httputil"
)

func TestClientIPResolution(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	if got := httputil.ClientIP(req); got != "198.51.100.7" {
		t.Fatalf("expected remote host ip, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req2.Header.Set("X-Forwarded-For", " 203.0.113.10 , 198.51.100.3")
	if got := httputil.ClientIP(req2); got != "203.0.113.10" {
		t.Fatalf("expected XFF first ip, got %q", got)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req3.Header.Set("Fly-Client-IP", "192.0.2.44")
	req3.Header.Set("X-Forwarded-For", "203.0.113.10")
	if got := httputil.ClientIP(req3); got != "192.0.2.44" {
		t.Fatalf("expected Fly-Client-IP precedence, got %q", got)
	}
}

func TestFixedWindowRateLimiter(t *testing.T) {
	t.Parallel()

	l := NewFixedWindowRateLimiter(2, 1, 50*time.Millisecond)
	if !l.Allow("1.1.1.1") {
		t.Fatal("first request should pass")
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("second request for same IP should be blocked by per-IP limit")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatal("second global slot should pass for another IP")
	}
	if l.Allow("3.3.3.3") {
		t.Fatal("global limit should block further requests")
	}

	time.Sleep(60 * time.Millisecond)
	if !l.Allow("1.1.1.1") {
		t.Fatal("window reset should allow request again")
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing X-Frame-Options")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing Referrer-Policy")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy")
	}
}

func TestRateLimit(t *testing.T) {
	t.Parallel()

	limiter := NewFixedWindowRateLimiter(1, 1, time.Minute)
	calls := 0
	h := RateLimit(limiter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req1.RemoteAddr = "192.0.2.1:1111"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("unexpected first status: %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req2.RemoteAddr = "192.0.2.2:2222"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected second status: %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "60" {
		t.Fatal("missing Retry-After header")
	}
	if calls != 1 {
		t.Fatalf("expected downstream handler called once, got %d", calls)
	}
}

func TestRequestIDContextHelpers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if httputil.RequestIDFromContext(ctx) != "" {
		t.Fatal("empty context should not have request_id")
	}
	ctx2 := httputil.WithRequestID(ctx, "abc123")
	if httputil.RequestIDFromContext(ctx2) != "abc123" {
		t.Fatal("expected stored request_id")
	}
	if httputil.WithRequestID(nil, "x") != nil { //nolint:staticcheck // SA1012: testing explicit nil support
		t.Fatal("nil context should return nil")
	}
	if ctx3 := httputil.WithRequestID(ctx, ""); ctx3 != ctx {
		t.Fatal("empty rid should return original context")
	}
}

func TestNewRequestIDFormat(t *testing.T) {
	t.Parallel()

	rid := httputil.NewRequestID()
	if len(rid) != 32 {
		t.Fatalf("expected 32-char hex, got %d: %q", len(rid), rid)
	}
	for _, r := range rid {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("non-hex char %c in request id %q", r, rid)
		}
	}
}

func TestNewRequestIDUniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 50)
	for range 50 {
		rid := httputil.NewRequestID()
		if _, dup := seen[rid]; dup {
			t.Fatalf("duplicate request id: %q", rid)
		}
		seen[rid] = struct{}{}
	}
}

func TestRouteLabel(t *testing.T) {
	t.Parallel()

	if httputil.RouteLabel(nil) != "unknown" {
		t.Fatal("nil request should yield UNKNOWN")
	}
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	if got := httputil.RouteLabel(req); got != "/foo" {
		t.Fatalf("expected /foo, got %q", got)
	}
}

func TestLabelOrUnknown(t *testing.T) {
	t.Parallel()

	if httputil.LabelOrUnknown("") != "unknown" {
		t.Fatal("empty should be unknown")
	}
	if httputil.LabelOrUnknown("  ") != "unknown" {
		t.Fatal("whitespace should be unknown")
	}
	if httputil.LabelOrUnknown("ok") != "ok" {
		t.Fatal("expected ok")
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Parallel()

	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := httputil.RequestIDFromContext(r.Context())
		if rid == "" {
			t.Fatal("expected request id in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id response header")
	}
}
