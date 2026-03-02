package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"

	"github.com/mymmrac/telego"
)

type oauthFakeStore struct {
	getOAuthStateFn func(ctx context.Context, state string) (core.OAuthStatePayload, error)
}

func (f *oauthFakeStore) OAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	if f.getOAuthStateFn != nil {
		return f.getOAuthStateFn(ctx, state)
	}
	return core.OAuthStatePayload{}, nil
}

func (f *oauthFakeStore) DeleteOAuthState(context.Context, string) (core.OAuthStatePayload, error) {
	return core.OAuthStatePayload{}, nil
}

func (f *oauthFakeStore) MarkEventProcessed(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (f *oauthFakeStore) AddCreatorSubscriber(context.Context, string, string) error {
	return nil
}

type oauthFakeObserver struct {
	telegramResult string
}

func (f *oauthFakeObserver) TelegramWebhookResult(result string) {
	f.telegramResult = result
}
func (f *oauthFakeObserver) OAuthCallback(string, string)           {}
func (f *oauthFakeObserver) EventSubMessage(string, string, string) {}

func testController(store controllerStore, obs metricsObserver, updates chan<- telego.Update) *Controller {
	return New(Dependencies{
		Config: config.Config{
			TwitchClientID:        "client-id",
			PublicBaseURL:         "https://example.com",
			TelegramWebhookSecret: "secret",
		},
		Store:           store,
		Observer:        obs,
		TelegramUpdates: updates,
	})
}

func TestOAuthStartMissingState(t *testing.T) {
	t.Parallel()

	c := testController(&oauthFakeStore{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/auth/start/", nil)
	req.SetPathValue("state", "")
	rec := httptest.NewRecorder()

	c.OAuthStart(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("OAuthStart(state=%q).StatusCode = %d, want %d", "", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "Missing Twitch link") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing %q", "", rec.Body.String(), "Missing Twitch link")
	}
}

func TestOAuthStartCreatorScope(t *testing.T) {
	t.Parallel()

	c := testController(&oauthFakeStore{
		getOAuthStateFn: func(_ context.Context, state string) (core.OAuthStatePayload, error) {
			if state != "state-1" {
				t.Fatalf("OAuthState(state=%q) got unexpected state, want %q", state, "state-1")
			}
			return core.OAuthStatePayload{Mode: core.OAuthModeCreator}, nil
		},
	}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/auth/start/state-1", nil)
	req.SetPathValue("state", "state-1")
	rec := httptest.NewRecorder()

	c.OAuthStart(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("OAuthStart(state=%q).StatusCode = %d, want %d", "state-1", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "channel%3Aread%3Asubscriptions") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing creator scope", "state-1", body)
	}
}

func TestTelegramWebhookQueueUnavailable(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{}, obs, nil)

	updateBody, _ := json.Marshal(telego.Update{UpdateID: 123})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(string(updateBody)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	rec := httptest.NewRecorder()

	c.TelegramWebhook(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("TelegramWebhook(queue=nil).StatusCode = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if obs.telegramResult != "updates_channel_unavailable" {
		t.Errorf("TelegramWebhook(queue=nil) observer result = %q, want %q", obs.telegramResult, "updates_channel_unavailable")
	}
}

func TestTelegramWebhookEnqueueSuccess(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	updates := make(chan telego.Update, 1)
	c := testController(&oauthFakeStore{}, obs, updates)

	payload := telego.Update{UpdateID: 321}
	updateBody, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(string(updateBody)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	rec := httptest.NewRecorder()

	c.TelegramWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("TelegramWebhook(queue=buffered).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	select {
	case u := <-updates:
		if u.UpdateID != payload.UpdateID {
			t.Errorf("TelegramWebhook(queue=buffered) enqueued UpdateID = %d, want %d", u.UpdateID, payload.UpdateID)
		}
	default:
		t.Error("TelegramWebhook(queue=buffered) did not enqueue update")
	}
	if obs.telegramResult != "ok" {
		t.Errorf("TelegramWebhook(queue=buffered) observer result = %q, want %q", obs.telegramResult, "ok")
	}
}

func TestOAuthStartViewerNoScope(t *testing.T) {
	t.Parallel()

	c := testController(&oauthFakeStore{
		getOAuthStateFn: func(_ context.Context, _ string) (core.OAuthStatePayload, error) {
			return core.OAuthStatePayload{Mode: core.OAuthModeViewer}, nil
		},
	}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/auth/start/state-2", nil)
	req.SetPathValue("state", "state-2")
	rec := httptest.NewRecorder()

	c.OAuthStart(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("OAuthStart(state=%q).StatusCode = %d, want %d", "state-2", rec.Code, http.StatusOK)
	}
	raw := rec.Body.String()
	if !strings.Contains(raw, "client_id=client-id") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing %q", "state-2", raw, "client_id=client-id")
	}
	if strings.Contains(raw, url.QueryEscape(core.ScopeChannelReadSubscriptions)) {
		t.Errorf("OAuthStart(state=%q).Body = %q, want no creator scope", "state-2", raw)
	}
}
