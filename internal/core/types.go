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

// Creator represents a Twitch broadcaster/creator and their OAuth state.
type Creator struct {
	ID                   string
	TwitchLogin          string
	OwnerTelegramID      int64
	AccessToken          string `json:"access_token"`  // #nosec G117 -- stored OAuth field name must match serialized schema
	RefreshToken         string `json:"refresh_token"` // #nosec G117 -- stored OAuth field name must match serialized schema
	GrantedScopes        []string
	UpdatedAt            time.Time
	AuthStatus           CreatorAuthStatus
	AuthErrorCode        string
	AuthStatusAt         time.Time
	LastSyncAt           time.Time
	LastBanSyncAt        time.Time
	LastNoticeAt         time.Time
	BlocklistSyncEnabled bool
	SubscriptionEndGrace SubscriptionEndGrace
}

// SubscriptionEndGrace describes how long access should be retained after a
// Twitch subscription-end notification before group removal is enforced.
type SubscriptionEndGrace string

const (
	// SubscriptionEndGraceOff disables delayed removal.
	SubscriptionEndGraceOff SubscriptionEndGrace = "off"
	// SubscriptionEndGrace24h delays removal by 24 hours.
	SubscriptionEndGrace24h SubscriptionEndGrace = "24h"
	// SubscriptionEndGrace48h delays removal by 48 hours.
	SubscriptionEndGrace48h SubscriptionEndGrace = "48h"
	// SubscriptionEndGrace72h delays removal by 72 hours.
	SubscriptionEndGrace72h SubscriptionEndGrace = "72h"
)

// GroupPolicy describes what the bot should do with users discovered in a group
// but not yet verified as tracked members.
type GroupPolicy string

const (
	// GroupPolicyObserve records untracked members without enforcement.
	GroupPolicyObserve GroupPolicy = "observe"
	// GroupPolicyObserveWarn records untracked members and warns when one joins.
	GroupPolicyObserveWarn GroupPolicy = "observe_warn"
	// GroupPolicyKick removes observed unverified members immediately.
	GroupPolicyKick GroupPolicy = "kick"
	// GroupPolicyGraceWeek removes observed unverified members after 7 days.
	GroupPolicyGraceWeek GroupPolicy = "grace_7d"
)

// ManagedGroup represents a Telegram group linked to a creator.
type ManagedGroup struct {
	ChatID               int64
	CreatorID            string
	GroupName            string
	Policy               GroupPolicy
	RegistrationThreadID int
	RegisteredAt         time.Time
	UpdatedAt            time.Time
}

// UntrackedGroupMember represents a user observed in a managed group but not
// currently tracked as verified.
type UntrackedGroupMember struct {
	ChatID         int64
	TelegramUserID int64
	Source         string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	LastStatus     string
}

// ActiveCreatorGroups packages the active creator record with its managed groups.
type ActiveCreatorGroups struct {
	Creator Creator
	Groups  []ManagedGroup
}

// OAuthStatePayload represents the data encoded in the OAuth state parameter.
type OAuthStatePayload struct {
	Mode            OAuthMode `json:"mode"`
	TelegramUserID  int64     `json:"telegram_user_id"`
	Language        string    `json:"language,omitempty"`
	PromptMessageID int       `json:"prompt_message_id,omitempty"`
	Reconnect       bool      `json:"reconnect,omitempty"`
}
