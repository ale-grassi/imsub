package twitch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"time"
)

// OAuthURL builds Twitch OAuth2 authorize URL for the provided state/scope.
func OAuthURL(clientID, redirectURL, state, scope string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("state", state)
	if scope != "" {
		q.Set("scope", scope)
		q.Set("force_verify", "true")
	}
	return "https://id.twitch.tv/oauth2/authorize?" + q.Encode()
}

// VerifyEventSubSignature validates Twitch EventSub delivery signature and age.
func VerifyEventSubSignature(secret string, headers http.Header, body []byte) bool {
	id := headers.Get("Twitch-Eventsub-Message-Id")
	timestamp := headers.Get("Twitch-Eventsub-Message-Timestamp")
	signature := headers.Get("Twitch-Eventsub-Message-Signature")
	if id == "" || timestamp == "" || signature == "" {
		return false
	}

	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return false
	}
	if age := time.Since(ts); age > 10*time.Minute || age < -10*time.Minute {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(id + timestamp + string(body))); err != nil {
		// hash.Hash writes never fail for in-memory hash implementations.
		return false
	}
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// EventSubEnvelope is top-level Twitch EventSub payload.
type EventSubEnvelope struct {
	Challenge    string `json:"challenge"`
	Subscription struct {
		Type      string `json:"type"`
		Condition struct {
			BroadcasterUserID string `json:"broadcaster_user_id"`
		} `json:"condition"`
	} `json:"subscription"`
	Event struct {
		UserID               string `json:"user_id"`
		UserLogin            string `json:"user_login"`
		BroadcasterUserLogin string `json:"broadcaster_user_login"`
	} `json:"event"`
}
