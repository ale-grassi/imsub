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
	"imsub/internal/events"
	"imsub/internal/platform/config"
)

type callbacksFakeStore struct {
	getDeleteOAuthStateFn func(ctx context.Context, state string) (core.OAuthStatePayload, error)
	eventProcessedFn      func(ctx context.Context, messageID string) (bool, error)
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

func (f *callbacksFakeStore) EventProcessed(ctx context.Context, messageID string) (bool, error) {
	if f.eventProcessedFn != nil {
		return f.eventProcessedFn(ctx, messageID)
	}
	return false, nil
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
	events []events.Event
}

func (f *callbacksFakeObserver) Emit(_ context.Context, evt events.Event) {
	f.events = append(f.events, evt)
}

func assertEventSubEvent(t *testing.T, obs *callbacksFakeObserver, outcome, messageType, subscriptionType, creatorID string) {
	t.Helper()
	if len(obs.events) != 1 {
		t.Fatalf("eventsub event count = %d, want 1: %+v", len(obs.events), obs.events)
	}
	evt := obs.events[0]
	if evt.Name != events.NameEventSubMessage || evt.Outcome != outcome {
		t.Fatalf("eventsub event = %+v, want outcome %q", evt, outcome)
	}
	if evt.Fields["message_type"] != messageType {
		t.Fatalf("eventsub message_type = %q, want %q", evt.Fields["message_type"], messageType)
	}
	if evt.Fields["subscription_type"] != subscriptionType {
		t.Fatalf("eventsub subscription_type = %q, want %q", evt.Fields["subscription_type"], subscriptionType)
	}
	if evt.Fields["creator_id"] != creatorID {
		t.Fatalf("eventsub creator_id = %q, want %q", evt.Fields["creator_id"], creatorID)
	}
}

func newController(store controllerStore, sink events.EventSink, viewer viewerOAuthHandler, creator creatorOAuthHandler, subEnd subEndHandler) *Controller {
	return New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store:             store,
		Events:            sink,
		ViewerOAuth:       viewer,
		CreatorOAuth:      creator,
		SubscriptionStart: nil,
		SubscriptionEnd:   subEnd,
	})
}

func TestTwitchCallbackMissingParams(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(&callbacksFakeStore{}, obs, nil, nil, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/callback", nil)
	rec := httptest.NewRecorder()

	c.TwitchCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("TwitchCallback(missing state/code).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if len(obs.events) != 1 || obs.events[0].Name != events.NameOAuthCallback || obs.events[0].Outcome != "missing_params" {
		t.Errorf("oauth events = %+v, want one oauth_callback missing_params", obs.events)
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
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/auth/callback?state=s1&code=abc", nil)
	rec := httptest.NewRecorder()

	c.TwitchCallback(rec, req)

	if !called {
		t.Error("TwitchCallback(viewer mode) did not call viewer handler")
	}
	if len(obs.events) != 1 || obs.events[0].Fields["mode"] != "viewer" || obs.events[0].Outcome != "success" {
		t.Errorf("oauth events = %+v, want viewer success", obs.events)
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
	assertEventSubEvent(t, obs, "challenge_ok", "webhook_callback_verification", "channel.subscribe", "c1")
}

func TestEventSubWebhookDuplicate(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(
		&callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, messageID string) (bool, error) {
				if messageID != "msg-dup" {
					t.Fatalf("EventProcessed(messageID=%q) got unexpected id, want %q", messageID, "msg-dup")
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
	assertEventSubEvent(t, obs, "duplicate", "notification", "channel.subscribe", "c1")
}

func TestEventSubWebhookStoreFailure(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	c := newController(
		&callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) {
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
	assertEventSubEvent(t, obs, "redis_error", "notification", "channel.subscribe", "c1")
}

func TestEventSubWebhookSubscribeFailureDoesNotMarkProcessed(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	markCalls := 0
	c := newController(
		&callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, messageID string) (bool, error) {
				if messageID != "msg-sub-fail" {
					t.Fatalf("EventProcessed(messageID=%q) got unexpected id, want %q", messageID, "msg-sub-fail")
				}
				return false, nil
			},
			addSubscriberFn: func(_ context.Context, creatorID, twitchUserID string) error {
				if creatorID != "c1" || twitchUserID != "u1" {
					t.Fatalf("AddCreatorSubscriber(%q, %q) got unexpected args", creatorID, twitchUserID)
				}
				return errors.New("redis hiccup")
			},
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				markCalls++
				return false, nil
			},
		},
		obs,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{"user_id":"u1","user_login":"viewer1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-sub-fail", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("EventSubWebhook(subscribe failure).StatusCode = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if markCalls != 0 {
		t.Fatalf("MarkEventProcessed call count = %d, want 0", markCalls)
	}
	assertEventSubEvent(t, obs, "notification_subscribe_store_failed", "notification", "channel.subscribe", "c1")
}

func TestEventSubWebhookSubscribeInvokesStartHandler(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	startCalls := 0
	c := New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store: &callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				return false, nil
			},
		},
		Events: obs,
		SubscriptionStart: func(_ context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) error {
			startCalls++
			if broadcasterID != "c1" || broadcasterLogin != "alpha" || twitchUserID != "u1" || twitchLogin != "viewer1" {
				t.Fatalf("SubscriptionStart(%q, %q, %q, %q) got unexpected args", broadcasterID, broadcasterLogin, twitchUserID, twitchLogin)
			}
			return nil
		},
	})

	body := []byte(`{"subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{"broadcaster_user_login":"alpha","user_id":"u1","user_login":"viewer1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-sub-start", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("EventSubWebhook(subscribe start).StatusCode = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if startCalls != 1 {
		t.Fatalf("SubscriptionStart call count = %d, want 1", startCalls)
	}
	assertEventSubEvent(t, obs, "notification_subscribe", "notification", "channel.subscribe", "c1")
}

func TestEventSubWebhookSubscribeStartFailureStillAcknowledged(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	markCalls := 0
	c := New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store: &callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				markCalls++
				return false, nil
			},
		},
		Events: obs,
		SubscriptionStart: func(context.Context, string, string, string, string) error {
			return errors.New("dm failed")
		},
	})

	body := []byte(`{"subscription":{"type":"channel.subscribe","condition":{"broadcaster_user_id":"c1"}},"event":{"broadcaster_user_login":"alpha","user_id":"u1","user_login":"viewer1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-sub-start-fail", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("EventSubWebhook(subscribe start failure).StatusCode = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if markCalls != 1 {
		t.Fatalf("MarkEventProcessed call count = %d, want 1", markCalls)
	}
	assertEventSubEvent(t, obs, "notification_subscribe", "notification", "channel.subscribe", "c1")
}

func TestEventSubWebhookBanPassesPermanentFlag(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	markCalls := 0
	banCalls := 0
	c := New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store: &callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) {
				return false, nil
			},
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				markCalls++
				return false, nil
			},
		},
		Events: obs,
		BlocklistBan: func(_ context.Context, creatorID, twitchUserID string, isPermanent bool) error {
			banCalls++
			if creatorID != "c1" || twitchUserID != "u1" {
				t.Fatalf("BlocklistBan(%q, %q) got unexpected args", creatorID, twitchUserID)
			}
			if isPermanent {
				t.Fatal("BlocklistBan isPermanent = true, want false for timeout payload")
			}
			return nil
		},
	})

	body := []byte(`{"subscription":{"type":"channel.ban","condition":{"broadcaster_user_id":"c1"}},"event":{"user_id":"u1","user_login":"viewer1","is_permanent":false}}`)
	req := signedEventSubRequest(t, "secret", "msg-ban-timeout", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("EventSubWebhook(ban).StatusCode = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if banCalls != 1 {
		t.Fatalf("BlocklistBan call count = %d, want 1", banCalls)
	}
	if markCalls != 1 {
		t.Fatalf("MarkEventProcessed call count = %d, want 1", markCalls)
	}
	assertEventSubEvent(t, obs, "notification_ban", "notification", "channel.ban", "c1")
}

func TestEventSubWebhookSubscriptionEndFailure(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	markCalls := 0
	c := New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store: &callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				markCalls++
				return false, nil
			},
		},
		Events: obs,
		SubscriptionEnd: func(context.Context, string, string, string, string) error {
			return errors.New("remove failed")
		},
	})

	body := []byte(`{"subscription":{"type":"channel.subscription.end","condition":{"broadcaster_user_id":"c1"}},"event":{"broadcaster_user_login":"alpha","user_id":"u1","user_login":"viewer1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-sub-end-fail", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("EventSubWebhook(subscription end failure).StatusCode = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if markCalls != 0 {
		t.Fatalf("MarkEventProcessed call count = %d, want 0", markCalls)
	}
	assertEventSubEvent(t, obs, "notification_subscription_end_failed", "notification", "channel.subscription.end", "c1")
}

func TestEventSubWebhookUnbanFailure(t *testing.T) {
	t.Parallel()

	obs := &callbacksFakeObserver{}
	markCalls := 0
	c := New(Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: "secret",
		},
		Store: &callbacksFakeStore{
			eventProcessedFn: func(_ context.Context, _ string) (bool, error) { return false, nil },
			markEventFn: func(context.Context, string, time.Duration) (bool, error) {
				markCalls++
				return false, nil
			},
		},
		Events: obs,
		BlocklistUnban: func(_ context.Context, creatorID, twitchUserID string, _ bool) error {
			if creatorID != "c1" || twitchUserID != "u1" {
				t.Fatalf("BlocklistUnban(%q, %q) got unexpected args", creatorID, twitchUserID)
			}
			return errors.New("unban failed")
		},
	})

	body := []byte(`{"subscription":{"type":"channel.unban","condition":{"broadcaster_user_id":"c1"}},"event":{"user_id":"u1","user_login":"viewer1"}}`)
	req := signedEventSubRequest(t, "secret", "msg-unban-fail", time.Now().UTC().Format(time.RFC3339), "notification", body)
	rec := httptest.NewRecorder()

	c.EventSubWebhook(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("EventSubWebhook(unban failure).StatusCode = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if markCalls != 0 {
		t.Fatalf("MarkEventProcessed call count = %d, want 0", markCalls)
	}
	assertEventSubEvent(t, obs, "notification_unban_failed", "notification", "channel.unban", "c1")
}

func signedEventSubRequest(t *testing.T, secret, messageID, ts, messageType string, body []byte) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	// hash.Hash.Write never returns an error.
	_, _ = mac.Write([]byte(messageID + ts + string(body)))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/twitch", strings.NewReader(string(body)))
	req.Header.Set("Twitch-Eventsub-Message-Id", messageID)
	req.Header.Set("Twitch-Eventsub-Message-Timestamp", ts)
	req.Header.Set("Twitch-Eventsub-Message-Signature", sig)
	req.Header.Set("Twitch-Eventsub-Message-Type", messageType)
	return req
}
