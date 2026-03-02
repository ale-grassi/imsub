//go:build integration
// +build integration

package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imsub/internal/adapter/redis"
	"imsub/internal/domain"
	"imsub/internal/platform/config"
	httphandlers "imsub/internal/transport/http/handlers"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/mymmrac/telego"
	redislib "github.com/redis/go-redis/v9"
)

type integrationObserver struct{}

func (integrationObserver) TelegramWebhookResult(_ string) {}
func (integrationObserver) OAuthCallback(_, _ string)      {}
func (integrationObserver) EventSubMessage(_, _, _ string) {}

func newIntegrationStore(t *testing.T) *redis.Store {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store, err := redis.NewStore("redis://"+mr.Addr(), logger)
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestHTTPAPITwitchCallbackViewerConsumesState(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	ctx := t.Context()
	state := "state-viewer-1"
	payload := domain.OAuthStatePayload{
		Mode:           domain.OAuthModeViewer,
		TelegramUserID: 42,
		Language:       "it",
	}
	if err := store.SaveOAuthState(ctx, state, payload, 5*time.Minute); err != nil {
		t.Fatalf("save oauth state: %v", err)
	}

	var called bool
	api := httphandlers.New(httphandlers.Dependencies{
		Config:   config.Config{},
		Store:    store,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Observer: integrationObserver{},
		ViewerOAuth: func(_ context.Context, w http.ResponseWriter, code string, gotPayload domain.OAuthStatePayload, lang string) string {
			called = true
			if code != "code-123" {
				t.Fatalf("ViewerOAuth() code = %q, want \"code-123\"", code)
			}
			if gotPayload.TelegramUserID != payload.TelegramUserID || gotPayload.Mode != domain.OAuthModeViewer {
				t.Fatalf("ViewerOAuth() payload = %+v, want TelegramUserID=%d, Mode=%d", gotPayload, payload.TelegramUserID, domain.OAuthModeViewer)
			}
			if lang != "it" {
				t.Fatalf("ViewerOAuth() lang = %q, want \"it\"", lang)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return "success"
		},
		CreatorOAuth: func(_ context.Context, _ http.ResponseWriter, _ string, _ domain.OAuthStatePayload, _ string) string {
			t.Fatal("creator handler should not be called")
			return ""
		},
		SubscriptionEnd: func(_ context.Context, _, _, _, _ string) error {
			t.Fatal("subscription end handler should not be called")
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+"&code=code-123", nil)
	rec := httptest.NewRecorder()
	api.TwitchCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TwitchCallback() status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatalf("ViewerOAuth() called = %t, want true", called)
	}
	_, err := store.OAuthState(ctx, state)
	if !errorsIsRedisNil(err) {
		t.Fatalf("OAuthState() returned error %v, want nil", err)
	}
}

func TestHTTPAPIEventSubSubscribeDedupesAndWritesStore(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	secret := "eventsub-secret"
	api := httphandlers.New(httphandlers.Dependencies{
		Config: config.Config{
			TwitchEventSubSecret: secret,
		},
		Store:    store,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Observer: integrationObserver{},
		ViewerOAuth: func(_ context.Context, _ http.ResponseWriter, _ string, _ domain.OAuthStatePayload, _ string) string {
			t.Fatal("viewer handler should not be called")
			return ""
		},
		CreatorOAuth: func(_ context.Context, _ http.ResponseWriter, _ string, _ domain.OAuthStatePayload, _ string) string {
			t.Fatal("creator handler should not be called")
			return ""
		},
		SubscriptionEnd: func(_ context.Context, _, _, _, _ string) error {
			t.Fatal("subscription end should not be called for subscribe event")
			return nil
		},
	})

	body := map[string]any{
		"subscription": map[string]any{
			"type": "channel.subscribe",
			"condition": map[string]any{
				"broadcaster_user_id": "creator-1",
			},
		},
		"event": map[string]any{
			"user_id":    "viewer-1",
			"user_login": "viewer_login",
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	reqWith := func(messageID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/twitch", strings.NewReader(string(raw)))
		req.Header.Set("Twitch-Eventsub-Message-Type", "notification")
		req.Header.Set("Twitch-Eventsub-Message-Id", messageID)
		ts := time.Now().UTC().Format(time.RFC3339)
		req.Header.Set("Twitch-Eventsub-Message-Timestamp", ts)
		req.Header.Set("Twitch-Eventsub-Message-Signature", eventSubSignature(secret, messageID, ts, raw))
		return req
	}

	rec1 := httptest.NewRecorder()
	api.EventSubWebhook(rec1, reqWith("msg-1"))
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("EventSubWebhook() status = %d body=%q, want 204 on first notification", rec1.Code, rec1.Body.String())
	}

	ok, err := store.IsCreatorSubscriber(t.Context(), "creator-1", "viewer-1")
	if err != nil {
		t.Fatalf("is creator subscriber: %v", err)
	}
	if !ok {
		t.Fatalf("IsCreatorSubscriber() = %t, want true", ok)
	}

	rec2 := httptest.NewRecorder()
	api.EventSubWebhook(rec2, reqWith("msg-1"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("EventSubWebhook() status = %d body=%q, want 200 for duplicate notification", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "duplicate ignored") {
		t.Fatalf("EventSubWebhook() body = %q, want to contain \"duplicate ignored\"", rec2.Body.String())
	}
}

func TestHTTPAPITelegramWebhookEnqueuesUpdate(t *testing.T) {
	t.Parallel()

	store := newIntegrationStore(t)
	updates := make(chan telego.Update, 1)
	api := httphandlers.New(httphandlers.Dependencies{
		Config: config.Config{
			TelegramWebhookSecret: "tg-secret",
		},
		Store:           store,
		Observer:        integrationObserver{},
		TelegramUpdates: updates,
	})

	payload, _ := json.Marshal(telego.Update{UpdateID: 777})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(string(payload)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "tg-secret")
	rec := httptest.NewRecorder()

	api.TelegramWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TelegramWebhook() status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	select {
	case u := <-updates:
		if u.UpdateID != 777 {
			t.Fatalf("TelegramWebhook() update id = %d, want 777", u.UpdateID)
		}
	default:
		t.Fatalf("TelegramWebhook() enqueued update = false, want true")
	}
}

func eventSubSignature(secret, messageID, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(messageID + timestamp + string(body)))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func errorsIsRedisNil(err error) bool {
	return errors.Is(err, redislib.Nil)
}
