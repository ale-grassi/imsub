package core

import (
	"errors"
	"time"
)

// ErrDifferentTwitch is returned when a user tries to link a Twitch account
// but is already linked to another.
var ErrDifferentTwitch = errors.New("telegram user already linked to a different Twitch account; run /reset first")

// OAuthMode identifies which flow an OAuth state belongs to.
type OAuthMode string

const (
	// OAuthModeViewer identifies viewer OAuth linking.
	OAuthModeViewer OAuthMode = "viewer"
	// OAuthModeCreator identifies creator OAuth linking.
	OAuthModeCreator OAuthMode = "creator"
)

// UserIdentity represents a Telegram user who has linked their Twitch account.
type UserIdentity struct {
	TelegramUserID int64
	TwitchUserID   string
	TwitchLogin    string
	Language       string
	VerifiedAt     time.Time
}

// CreatorAuthStatus describes whether a creator's stored OAuth state is usable.
type CreatorAuthStatus string

const (
	// CreatorAuthHealthy means the creator token state is healthy.
	CreatorAuthHealthy CreatorAuthStatus = "healthy"
	// CreatorAuthReconnectRequired means the creator must reconnect via OAuth.
	CreatorAuthReconnectRequired CreatorAuthStatus = "reconnect_required"
)

// Creator represents a Twitch broadcaster/creator and their associated Telegram group.
type Creator struct {
	ID              string
	Name            string
	OwnerTelegramID int64
	GroupChatID     int64
	GroupName       string
	AccessToken     string `json:"access_token"`  //nolint:gosec
	RefreshToken    string `json:"refresh_token"` //nolint:gosec
	UpdatedAt       time.Time
	AuthStatus      CreatorAuthStatus
	AuthErrorCode   string
	AuthStatusAt    time.Time
	LastSyncAt      time.Time
	LastNoticeAt    time.Time
}

// OAuthStatePayload represents the data encoded in the OAuth state parameter.
type OAuthStatePayload struct {
	Mode            OAuthMode `json:"mode"`
	TelegramUserID  int64     `json:"telegram_user_id"`
	Language        string    `json:"language,omitempty"`
	PromptMessageID int       `json:"prompt_message_id,omitempty"`
	Reconnect       bool      `json:"reconnect,omitempty"`
}
