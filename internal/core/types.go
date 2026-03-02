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

// Creator represents a Twitch broadcaster/creator and their associated Telegram group.
type Creator struct {
	ID              string
	Name            string
	OwnerTelegramID int64
	GroupChatID     int64
	GroupName       string
	AccessToken     string
	RefreshToken    string
	UpdatedAt       time.Time
}

// OAuthStatePayload represents the data encoded in the OAuth state parameter.
type OAuthStatePayload struct {
	Mode            OAuthMode `json:"mode"`
	TelegramUserID  int64     `json:"telegram_user_id"`
	Language        string    `json:"language,omitempty"`
	PromptMessageID int       `json:"prompt_message_id,omitempty"`
}
