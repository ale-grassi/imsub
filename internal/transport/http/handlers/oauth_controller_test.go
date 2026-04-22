package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/config"

	"github.com/mymmrac/telego"
)

type oauthFakeStore struct {
	getOAuthStateFn    func(ctx context.Context, state string) (core.OAuthStatePayload, error)
	deleteOAuthStateFn func(ctx context.Context, state string) (core.OAuthStatePayload, error)
}

func (f *oauthFakeStore) OAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	if f.getOAuthStateFn != nil {
		return f.getOAuthStateFn(ctx, state)
	}
	return core.OAuthStatePayload{}, nil
}

func (f *oauthFakeStore) DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	if f.deleteOAuthStateFn != nil {
		return f.deleteOAuthStateFn(ctx, state)
	}
	return core.OAuthStatePayload{}, nil
}

func (f *oauthFakeStore) EventProcessed(context.Context, string) (bool, error) {
	return false, nil
}

func (f *oauthFakeStore) MarkEventProcessed(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (f *oauthFakeStore) AddCreatorSubscriber(context.Context, string, string) error {
	return nil
}

type oauthFakeObserver struct {
	events []events.Event
}

func (f *oauthFakeObserver) Emit(_ context.Context, evt events.Event) {
	f.events = append(f.events, evt)
}

func testController(store controllerStore, sink events.EventSink, updates chan<- telego.Update) *Controller {
	return New(Dependencies{
		Config: config.Config{
			TwitchClientID:        "client-id",
			PublicBaseURL:         "https://example.com",
			TelegramWebhookSecret: "secret",
		},
		Store:           store,
		Events:          sink,
		TelegramUpdates: updates,
	})
}

func TestOAuthStartMissingState(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{}, obs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/start/", nil)
	req.SetPathValue("state", "")
	rec := httptest.NewRecorder()

	c.OAuthStart(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("OAuthStart(state=%q).StatusCode = %d, want %d", "", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "Missing Twitch link") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing %q", "", rec.Body.String(), "Missing Twitch link")
	}
	if !strings.Contains(rec.Body.String(), "Restart the same connection flow to get a new link.") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing recovery steps", "", rec.Body.String())
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameOAuthStart || obs.events[0].Outcome != "missing_state" || obs.events[0].Fields["mode"] != "unknown" {
		t.Errorf("oauth_start events = %+v, want one unknown missing_state", obs.events)
	}
}

func TestTwitchCallbackExpiredLinkRendersNeutralRecoverySteps(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{
		deleteOAuthStateFn: func(_ context.Context, state string) (core.OAuthStatePayload, error) {
			if state != "missing" {
				t.Fatalf("DeleteOAuthState(state=%q) got unexpected state, want %q", state, "missing")
			}
			return core.OAuthStatePayload{}, errors.New("missing state")
		},
	}, obs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/callback?state=missing&code=code-1", nil)
	rec := httptest.NewRecorder()

	c.TwitchCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("TwitchCallback(expired state).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Twitch link expired") {
		t.Fatalf("TwitchCallback(expired state).Body = %q, want expired title", body)
	}
	if !strings.Contains(body, "Restart the same connection flow to get a new link.") {
		t.Fatalf("TwitchCallback(expired state).Body = %q, want neutral recovery steps", body)
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameOAuthCallback || obs.events[0].Outcome != "state_missing" || obs.events[0].Fields["mode"] != "unknown" {
		t.Errorf("oauth_callback events = %+v, want one unknown state_missing", obs.events)
	}
}

func TestTwitchCallbackViewerSaveFailedRendersStartGuidance(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{
		deleteOAuthStateFn: func(_ context.Context, state string) (core.OAuthStatePayload, error) {
			if state != "viewer-state" {
				t.Fatalf("DeleteOAuthState(state=%q) got unexpected state, want %q", state, "viewer-state")
			}
			return core.OAuthStatePayload{Mode: core.OAuthModeViewer, Language: "en"}, nil
		},
	}, obs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/callback?state=viewer-state&code=code-1", nil)
	rec := httptest.NewRecorder()

	c.viewer = func(context.Context, string, core.OAuthStatePayload, string) (string, string, error) {
		return "viewer_save_failed", "", &core.FlowError{Kind: core.KindSave}
	}
	c.creator = func(context.Context, string, core.OAuthStatePayload, string) (string, string, error) {
		t.Fatal("creator callback should not run for viewer payload")
		return "", "", nil
	}
	c.TwitchCallback(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("TwitchCallback(viewer save failed).StatusCode = %d, want %d", rec.Code, http.StatusConflict)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Use /start to get a new Twitch link.") {
		t.Fatalf("TwitchCallback(viewer save failed).Body = %q, want /start guidance", body)
	}
}

func TestOAuthStartCreatorScope(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{
		getOAuthStateFn: func(_ context.Context, state string) (core.OAuthStatePayload, error) {
			if state != "state-1" {
				t.Fatalf("OAuthState(state=%q) got unexpected state, want %q", state, "state-1")
			}
			return core.OAuthStatePayload{Mode: core.OAuthModeCreator}, nil
		},
	}, obs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/start/state-1", nil)
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
	if !strings.Contains(body, "moderation%3Aread") {
		t.Errorf("OAuthStart(state=%q).Body = %q, want body containing moderation scope", "state-1", body)
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameOAuthStart || obs.events[0].Outcome != "ok" || obs.events[0].Fields["mode"] != "creator" {
		t.Errorf("oauth_start events = %+v, want one creator ok", obs.events)
	}
}

func TestTelegramWebhookQueueUnavailable(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{}, obs, nil)

	updateBody, _ := json.Marshal(telego.Update{UpdateID: 123})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/telegram", strings.NewReader(string(updateBody)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	rec := httptest.NewRecorder()

	c.TelegramWebhook(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("TelegramWebhook(queue=nil).StatusCode = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameTelegramWebhook || obs.events[0].Outcome != "updates_channel_unavailable" {
		t.Errorf("telegram events = %+v, want updates_channel_unavailable", obs.events)
	}
}

func TestTelegramWebhookEnqueueSuccess(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	updates := make(chan telego.Update, 1)
	c := testController(&oauthFakeStore{}, obs, updates)

	payload := telego.Update{UpdateID: 321}
	updateBody, _ := json.Marshal(payload)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/telegram", strings.NewReader(string(updateBody)))
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
	if len(obs.events) != 1 || obs.events[0].Name != events.NameTelegramWebhook || obs.events[0].Outcome != "ok" {
		t.Errorf("telegram events = %+v, want ok", obs.events)
	}
}

func TestTelegramWebhookUnauthorized(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	updates := make(chan telego.Update, 1)
	c := testController(&oauthFakeStore{}, obs, updates)

	updateBody, _ := json.Marshal(telego.Update{UpdateID: 456})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/telegram", strings.NewReader(string(updateBody)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong")
	rec := httptest.NewRecorder()

	c.TelegramWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("TelegramWebhook(wrong secret).StatusCode = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	select {
	case <-updates:
		t.Fatal("TelegramWebhook(wrong secret) should not enqueue update")
	default:
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameTelegramWebhook || obs.events[0].Outcome != "unauthorized" {
		t.Errorf("telegram events = %+v, want unauthorized", obs.events)
	}
}

func TestOAuthStartViewerNoScope(t *testing.T) {
	t.Parallel()

	obs := &oauthFakeObserver{}
	c := testController(&oauthFakeStore{
		getOAuthStateFn: func(_ context.Context, _ string) (core.OAuthStatePayload, error) {
			return core.OAuthStatePayload{Mode: core.OAuthModeViewer}, nil
		},
	}, obs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/start/state-2", nil)
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
	if len(obs.events) != 1 || obs.events[0].Name != events.NameOAuthStart || obs.events[0].Outcome != "ok" || obs.events[0].Fields["mode"] != "viewer" {
		t.Errorf("oauth_start events = %+v, want one viewer ok", obs.events)
	}
}
