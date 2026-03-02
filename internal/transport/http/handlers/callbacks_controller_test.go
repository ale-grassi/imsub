package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"
)

type callbacksFakeStore struct {
	getDeleteOAuthStateFn func(ctx context.Context, state string) (core.OAuthStatePayload, error)
	markEventFn           func(ctx context.Context, messageID string, ttl time.Duration) (bool, error)
	addSubscriberFn       func(ctx context.Context, creatorID, twitchUserID string) error
}

func (f *callbacksFakeStore) OAuthState(context.Context, string) (core.OAuthStatePayload, error) {
	return core.OAuthStatePayload{}, nil
}

func (f *callbacksFakeStore) DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	if f.getDeleteOAuthStateFn != nil {
		return f.getDeleteOAuthStateFn(ctx, state)
	}
	return core.OAuthStatePayload{}, nil
}

func (f *callbacksFakeStore) MarkEventProcessed(ctx context.Context, messageID string, ttl time.Duration) (bool, error) {
	if f.markEventFn != nil {
		return f.markEventFn(ctx, messageID, ttl)
	}
	return false, nil
}

func (f *callbacksFakeStore) AddCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	if f.addSubscriberFn != nil {
		return f.addSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return nil
}

type callbacksFakeObserver struct {
	oauthMode   string
	oauthResult string
	eventType   string
	eventSub    string
	eventResult string
}

func (f *callbacksFakeObserver) TelegramWebhookResult(string) {}

func (f *callbacksFakeObserver) OAuthCallback(mode, result string) {
	f.oauthMode = mode
	f.oauthResult = result
}

func (f *callbacksFakeObserver) EventSubMessage(messageType, subscriptionType, result string) {
	f.eventType = messageType
	f.eventSub = subscriptionType
	f.eventResult = result
}

//nolint:unparam // test helper args can be constant
func newController(store controllerStore, obs metricsObserver, viewer viewerOAuthHandler, creator creatorOAuthHandler, subEnd subEndHandler) *Controller {
	return New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store:           store,
		Observer:        obs,
		ViewerOAuth:     viewer,
		CreatorOAuth:    creator,
		SubscriptionEnd: subEnd,
	})
}

func TestTwitchCallbackMissingParams(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(&callbacksFakeStore{}, obs, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	rec := httptest.NewRecorder()

	c.TwitchCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("TwitchCallback(missing state/code).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if obs.oauthResult != "missing_params" {
		t.Errorf("TwitchCallback(missing state/code) observer result = %q, want %q", obs.oauthResult, "missing_params")
	}
}

func TestTwitchCallbackRoutesViewer(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	called := false
	c := newController(
		&callbacksFakeStore{
			getDeleteOAuthStateFn: func(_ context.Context, state string) (core.OAuthStatePayload, error) {
				if state != "s1" {
					t.Fatalf("DeleteOAuthState(state=%q) got unexpected state, want %q", state, "s1")
				}
				return core.OAuthStatePayload{
					Mode:           core.OAuthModeViewer,
					TelegramUserID: 7,
					Language:       "it-IT",
				}, nil
			},
		},
		obs,
		func(_ context.Context, code string, _ core.OAuthStatePayload, lang string) (string, string, error) {
			called = true
			if code != "abc" {
				t.Fatalf("viewerOAuth(code=%q) got unexpected code, want %q", code, "abc")
			}
			if lang != "it" {
				t.Fatalf("expected normalized lang it, got %q", lang)
			}
			return "success", "TestUser", nil
		},
		nil,
		nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s1&code=abc", nil)
	rec := httptest.NewRecorder()

	c.TwitchCallback(rec, req)

	if !called {
		t.Error("TwitchCallback(viewer mode) did not call viewer handler")
	}
	if obs.oauthMode != "viewer" || obs.oauthResult != "success" {
		t.Errorf("TwitchCallback(viewer mode) observer labels = (mode=%q, result=%q), want (mode=%q, result=%q)", obs.oauthMode, obs.oauthResult, "viewer", "success")
	}
}

func TestEventSubWebhookChallenge(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(&callbacksFakeStore{}, obs, nil, nil, nil)

	body := []byte(`{"challenge":"abc123","subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{}}`)
	req := signedEventSubRequest(t, "secret", "msg-1", time.Now().UTC().Format(time.RFC3339), "webhook_callback_verification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("EventSubWebhook(challenge).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "abc123" {
		t.Errorf("EventSubWebhook(challenge).Body = %q, want %q", rec.Body.String(), "abc123")
	}
	if obs.eventResult != "challenge_ok" {
		t.Errorf("EventSubWebhook(challenge) observer result = %q, want %q", obs.eventResult, "challenge_ok")
	}
}

func TestEventSubWebhookDuplicate(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(
		&callbacksFakeStore{
			markEventFn: func(_ context.Context, messageID string, _ time.Duration) (bool, error) {
				if messageID != "msg-dup" {
					t.Fatalf("MarkEventProcessed(messageID=%q) got unexpected id, want %q", messageID, "msg-dup")
				}
				return true, nil
			},
		},
		obs,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{"user_id":"u1","user_login":"v1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-dup", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("EventSubWebhook(duplicate).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "duplicate ignored") {
		t.Errorf("EventSubWebhook(duplicate).Body = %q, want body containing %q", rec.Body.String(), "duplicate ignored")
	}
	if obs.eventResult != "duplicate" {
		t.Errorf("EventSubWebhook(duplicate) observer result = %q, want %q", obs.eventResult, "duplicate")
	}
}

func TestEventSubWebhookStoreFailure(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(
		&callbacksFakeStore{
			markEventFn: func(_ context.Context, _ string, _ time.Duration) (bool, error) {
				return false, errors.New("redis down")
			},
		},
		obs,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{"user_id":"u1","user_login":"v1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-2", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("EventSubWebhook(store failure).StatusCode = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if obs.eventResult != "redis_error" {
		t.Errorf("EventSubWebhook(store failure) observer result = %q, want %q", obs.eventResult, "redis_error")
	}
}

func signedEventSubRequest(t *testing.T, secret, messageID, ts, messageType string, body []byte) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	// hash.Hash.Write never returns an error.
	_, _ = mac.Write([]byte(messageID + ts + string(body)))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/twitch", strings.NewReader(string(body)))
	req.Header.Set("Twitch-Eventsub-Message-Id", messageID)
	req.Header.Set("Twitch-Eventsub-Message-Timestamp", ts)
	req.Header.Set("Twitch-Eventsub-Message-Signature", sig)
	req.Header.Set("Twitch-Eventsub-Message-Type", messageType)
	return req
}
