package core

import "time"

// Duration converts a configured subscription-end grace value to a duration.
func (g SubscriptionEndGrace) Duration() time.Duration {
	switch g {
	case SubscriptionEndGraceOff:
		return 0
	case SubscriptionEndGrace24h:
		return 24 * time.Hour
	case SubscriptionEndGrace48h:
		return 48 * time.Hour
	case SubscriptionEndGrace72h:
		return 72 * time.Hour
	default:
		return 0
	}
}

// Enabled reports whether delayed subscription-end removal is active.
func (g SubscriptionEndGrace) Enabled() bool {
	return g.Duration() > 0
}

// PendingSubscriptionEndGrace captures a delayed removal scheduled after a
// subscription-end event.
type PendingSubscriptionEndGrace struct {
	ID             string    `json:"id"`
	CreatorID      string    `json:"creator_id"`
	CreatorLogin   string    `json:"creator_login"`
	TwitchUserID   string    `json:"twitch_user_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	ViewerLogin    string    `json:"viewer_login"`
	Language       string    `json:"language"`
	DueAt          time.Time `json:"due_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SubscriptionEndMode identifies how a subscription-end event should be
// enforced.
type SubscriptionEndMode string

const (
	// SubscriptionEndModeImmediate removes access right away.
	SubscriptionEndModeImmediate SubscriptionEndMode = "immediate"
	// SubscriptionEndModeGrace defers removal until a configured grace window expires.
	SubscriptionEndModeGrace SubscriptionEndMode = "grace"
)

// ExpiredSubscriptionGraceResult is the transport-ready result of enforcing a
// delayed subscription-end removal.
type ExpiredSubscriptionGraceResult struct {
	TelegramUserID   int64
	Language         string
	ViewerLogin      string
	BroadcasterLogin string
}
