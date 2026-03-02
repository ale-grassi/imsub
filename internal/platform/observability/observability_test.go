package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNilSafety(t *testing.T) {
	t.Parallel()

	var m *Metrics
	m.OAuthCallback("viewer", "ok")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TelegramWebhookResult("ok")
	m.BackgroundJob("audit", "ok", time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("m.Handler().ServeHTTP status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMetricsExposure(t *testing.T) {
	t.Parallel()

	m := New()
	m.OAuthCallback("viewer", "success")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TelegramWebhookResult("ok")
	m.BackgroundJob("integrity_audit", "ok", 120*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("m.Handler().ServeHTTP status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	needles := []string{
		"imsub_oauth_callbacks_total",
		"imsub_eventsub_messages_total",
		"imsub_telegram_webhook_updates_total",
		"imsub_background_jobs_total",
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Errorf("m.Handler() output missing %q", needle)
		}
	}
}

func TestMiddlewareNilDependencies(t *testing.T) {
	t.Parallel()

	m := New()
	handler := m.Middleware(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Middleware(nil,nil,nil) status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
