package core

import "time"

// MemberCleanupKind identifies which workflow enqueued a background cleanup.
type MemberCleanupKind string

const (
	// MemberCleanupKindGroupUnregistration is emitted when a single managed group
	// is unregistered with background tracked-member cleanup.
	MemberCleanupKindGroupUnregistration MemberCleanupKind = "group_unregistration"
	// MemberCleanupKindCreatorReset is emitted when creator reset schedules
	// background tracked-member cleanup across managed groups.
	MemberCleanupKindCreatorReset MemberCleanupKind = "creator_reset"
)

// MemberCleanupStatus tracks background cleanup progress.
type MemberCleanupStatus string

const (
	// MemberCleanupStatusPending means cleanup still has targets left to process.
	MemberCleanupStatusPending MemberCleanupStatus = "pending"
	// MemberCleanupStatusDone means all queued targets were removed successfully.
	MemberCleanupStatusDone MemberCleanupStatus = "done"
	// MemberCleanupStatusExhausted means retries were exhausted for one or more
	// remaining targets.
	MemberCleanupStatusExhausted MemberCleanupStatus = "exhausted"
)

// MemberCleanupTarget is one Telegram membership removal attempt.
type MemberCleanupTarget struct {
	ChatID         int64 `json:"chat_id"`
	TelegramUserID int64 `json:"telegram_user_id"`
	Attempts       int   `json:"attempts"`
	MaxAttempts    int   `json:"max_attempts"`
}

// MemberCleanupJob is a persisted background cleanup job snapshot.
type MemberCleanupJob struct {
	ID                string                `json:"id"`
	Kind              MemberCleanupKind     `json:"kind"`
	Status            MemberCleanupStatus   `json:"status"`
	OwnerTelegramID   int64                 `json:"owner_telegram_id"`
	CreatorID         string                `json:"creator_id"`
	CreatorLogin      string                `json:"creator_login"`
	GroupChatID       int64                 `json:"group_chat_id"`
	GroupName         string                `json:"group_name"`
	ManagedGroupCount int                   `json:"managed_group_count"`
	TotalTargets      int                   `json:"total_targets"`
	SucceededCount    int                   `json:"succeeded_count"`
	Targets           []MemberCleanupTarget `json:"targets"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// MemberCleanupResult is the final outcome sent to the creator.
type MemberCleanupResult struct {
	Kind              MemberCleanupKind
	Status            MemberCleanupStatus
	OwnerTelegramID   int64
	CreatorLogin      string
	GroupName         string
	ManagedGroupCount int
	TargetedCount     int
	SucceededCount    int
	FailedCount       int
}
