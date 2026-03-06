package core

import (
	"errors"
)

// ErrUnauthorized is returned when Twitch API replies with 401.
var ErrUnauthorized = errors.New("twitch: unauthorized")

const (
	// ScopeChannelReadSubscriptions is the scope required to read channel subscribers.
	ScopeChannelReadSubscriptions = "channel:read:subscriptions"
	// EventTypeChannelSubscribe is the Twitch EventSub type for new subscriptions.
	EventTypeChannelSubscribe = "channel.subscribe"
	// EventTypeChannelSubEnd is the Twitch EventSub type for ended subscriptions.
	EventTypeChannelSubEnd = "channel.subscription.end"
	// EventTypeChannelSubGift is the Twitch EventSub type for gifted subscriptions.
	EventTypeChannelSubGift = "channel.subscription.gift"
)

// TokenResponse represents a Twitch OAuth token exchange or refresh response.
type TokenResponse struct {
	AccessToken  string   `json:"access_token"` //nolint:gosec // Not a secret literal, just a struct field
	TokenType    string   `json:"token_type"`
	RefreshToken string   `json:"refresh_token"` //nolint:gosec // Not a secret literal, just a struct field
	ExpiresIn    int      `json:"expires_in"`
	Scope        []string `json:"scope"`
}
