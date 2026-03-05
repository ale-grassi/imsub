package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"imsub/internal/platform/config"
	"imsub/internal/platform/observability"
)

type fakeHealthStore struct {
	pingErr error
}

func (f fakeHealthStore) Ping(_ context.Context) error {
	return f.pingErr
}

func testDeps(store healthStore) Dependencies {
	return Dependencies{
		Config: config.Config{
			TwitchWebhookPath:   "/webhooks/twitch",
			TelegramWebhookPath: "/webhooks/telegram",
			MetricsPath:         "/metrics",
			MetricsEnabled:      false,
		},
		Store: store,
		Handlers: Handlers{
			OAuthStart: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			TwitchCallback: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			EventSubWebhook: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			TelegramWebhook: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		},
	}
}

func TestNewHandlerHealthzOK(t *testing.T) {
	t.Parallel()

	deps := testDeps(fakeHealthStore{})
	handler := newHandler(deps, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("newHandler(...).ServeHTTP(GET /healthz).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("newHandler(...).ServeHTTP(GET /healthz).Body = %q, want %q", rec.Body.String(), "ok")
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("newHandler(...).ServeHTTP(GET /healthz).Header(\"X-Request-Id\") is empty, want non-empty")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("newHandler(...).ServeHTTP(GET /healthz).Header(%q) = %q, want %q", "X-Content-Type-Options", rec.Header().Get("X-Content-Type-Options"), "nosniff")
	}
}

func TestNewHandlerRootRedirectsToRepoHomepage(t *testing.T) {
	t.Parallel()

	deps := testDeps(fakeHealthStore{})
	handler := newHandler(deps, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("newHandler(...).ServeHTTP(GET /).StatusCode = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != repoHomepageURL {
		t.Errorf("newHandler(...).ServeHTTP(GET /).Header(%q) = %q, want %q", "Location", got, repoHomepageURL)
	}
}

func TestNewHandlerHealthzStoreError(t *testing.T) {
	t.Parallel()

	deps := testDeps(fakeHealthStore{pingErr: errors.New("redis down")})
	handler := newHandler(deps, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("newHandler(...).ServeHTTP(GET /healthz).StatusCode = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if rec.Body.String() != "redis unreachable" {
		t.Errorf("newHandler(...).ServeHTTP(GET /healthz).Body = %q, want %q", rec.Body.String(), "redis unreachable")
	}
}

func TestNewHandlerTelegramWebhookRouteGate(t *testing.T) {
	t.Parallel()

	t.Run("telegram_webhook_disabled_without_secret", func(t *testing.T) {
		t.Parallel()

		deps := testDeps(fakeHealthStore{})
		deps.Config.TelegramWebhookSecret = ""
		handler := newHandler(deps, nil)

		req := httptest.NewRequest(http.MethodPost, deps.Config.TelegramWebhookPath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("newHandler(...).ServeHTTP(POST %s).StatusCode = %d, want %d", deps.Config.TelegramWebhookPath, rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("telegram_webhook_enabled_with_secret", func(t *testing.T) {
		t.Parallel()

		deps := testDeps(fakeHealthStore{})
		deps.Config.TelegramWebhookSecret = "secret"
		handler := newHandler(deps, nil)

		req := httptest.NewRequest(http.MethodPost, deps.Config.TelegramWebhookPath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("newHandler(...).ServeHTTP(POST %s).StatusCode = %d, want %d", deps.Config.TelegramWebhookPath, rec.Code, http.StatusNoContent)
		}
	})
}

func TestNewHandlerMetricsRoute(t *testing.T) {
	t.Parallel()

	deps := testDeps(fakeHealthStore{})
	deps.Config.MetricsEnabled = true
	deps.Metrics = observability.New()
	handler := newHandler(deps, nil)

	req := httptest.NewRequest(http.MethodGet, deps.Config.MetricsPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("newHandler(...).ServeHTTP(GET %s).StatusCode = %d, want %d", deps.Config.MetricsPath, rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Errorf("newHandler(...).ServeHTTP(GET %s).BodyLen = %d, want > 0", deps.Config.MetricsPath, rec.Body.Len())
	}
}
