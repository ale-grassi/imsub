package core

import (
	"errors"
)

// ErrUnauthorized is returned when Twitch API replies with 401.
var ErrUnauthorized = errors.New("twitch: unauthorized")

const (
	// ScopeChannelReadSubscriptions is the scope required to read channel subscribers.
	ScopeChannelReadSubscriptions = "channel:read:subscriptions"
	// ScopeChannelModerate is the scope required for moderation EventSub subscriptions.
	ScopeChannelModerate = "channel:moderate"
	// ScopeModerationRead is the scope required to read the creator's moderation ban list.
	ScopeModerationRead = "moderation:read"
	// EventTypeChannelSubscribe is the Twitch EventSub type for new subscriptions.
	EventTypeChannelSubscribe = "channel.subscribe"
	// EventTypeChannelSubEnd is the Twitch EventSub type for ended subscriptions.
	EventTypeChannelSubEnd = "channel.subscription.end"
	// EventTypeChannelBan is the Twitch EventSub type for channel bans.
	EventTypeChannelBan = "channel.ban"
	// EventTypeChannelUnban is the Twitch EventSub type for channel unbans.
	EventTypeChannelUnban = "channel.unban"
)

// ListEventSubsOpts configures the ListEventSubs query.
type ListEventSubsOpts struct {
	UserID string // filter by condition user_id; empty = no filter
}

// EventSubSubscription represents a single Twitch EventSub subscription.
type EventSubSubscription struct {
	ID            string
	Status        string
	Type          string
	BroadcasterID string
}

// TokenResponse represents a Twitch OAuth token exchange or refresh response.
type TokenResponse struct {
	AccessToken  string   `json:"access_token"` // #nosec G117 -- token response field name must match Twitch's JSON schema
	TokenType    string   `json:"token_type"`
	RefreshToken string   `json:"refresh_token"` // #nosec G117 -- token response field name must match Twitch's JSON schema
	ExpiresIn    int      `json:"expires_in"`
	Scope        []string `json:"scope"`
}
