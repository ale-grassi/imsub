package twitch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestOAuthURL(t *testing.T) {
	t.Parallel()

	raw := OAuthURL("client", "https://example.com/auth/callback", "state-1", "channel:read:subscriptions")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(OAuthURL(...)) returned error: %v", err)
	}
	q := parsed.Query()
	if q.Get("client_id") != "client" || q.Get("state") != "state-1" {
		t.Errorf("OAuthURL(...) query = %q, want client_id=%q and state=%q", parsed.RawQuery, "client", "state-1")
	}
	if q.Get("force_verify") != "true" {
		t.Errorf("OAuthURL(...) force_verify = %q, want %q", q.Get("force_verify"), "true")
	}
}

func TestVerifyEventSubSignature(t *testing.T) {
	t.Parallel()

	secret := "secret"
	body := []byte(`{"event":"ok"}`)
	messageID := "msg-1"
	ts := time.Now().UTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(messageID + ts + string(body))); err != nil {
		t.Fatalf("mac.Write(messageID+ts+body) returned error: %v", err)
	}
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	headers := http.Header{}
	headers.Set("Twitch-Eventsub-Message-Id", messageID)
	headers.Set("Twitch-Eventsub-Message-Timestamp", ts)
	headers.Set("Twitch-Eventsub-Message-Signature", signature)

	if !VerifyEventSubSignature(secret, headers, body) {
		t.Errorf("VerifyEventSubSignature(secret, headers, body) = false, want true")
	}
	headers.Set("Twitch-Eventsub-Message-Signature", "sha256=deadbeef")
	if VerifyEventSubSignature(secret, headers, body) {
		t.Errorf("VerifyEventSubSignature(secret, headers, body) = true, want false after tampered signature")
	}
}
