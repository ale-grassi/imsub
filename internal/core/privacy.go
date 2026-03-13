package core

import "time"

// ConsentRecord captures the user's explicit consent to personal-data storage.
type ConsentRecord struct {
	TelegramUserID int64
	Language       string
	PolicyVersion  string
	GrantedAt      time.Time
}

// PrivacyReceipt summarizes one privacy-related action performed for a user.
type PrivacyReceipt struct {
	ID               string
	TelegramUserID   int64
	Kind             string
	Scope            string
	Result           string
	RequestedAt      time.Time
	CompletedAt      time.Time
	DeletedViewer    bool
	DeletedCreator   bool
	DeletedGroups    int
	DeletedAncillary int
}

// PrivacyExport is the machine-readable user-data export returned by the bot.
type PrivacyExport struct {
	SchemaVersion        string                 `json:"schema_version"`
	ExportedAt           time.Time              `json:"exported_at"`
	User                 PrivacyExportUser      `json:"user"`
	Consent              *ConsentRecord         `json:"consent,omitempty"`
	Viewer               *UserIdentity          `json:"viewer,omitempty"`
	Creator              *PrivacyExportCreator  `json:"creator,omitempty"`
	ManagedGroups        []ManagedGroup         `json:"managed_groups,omitempty"`
	TrackedGroupIDs      []int64                `json:"tracked_group_ids,omitempty"`
	UntrackedMemberships []UntrackedGroupMember `json:"untracked_memberships,omitempty"`
	Receipts             []PrivacyReceipt       `json:"receipts,omitempty"`
}

// PrivacyExportUser holds user-identifying export metadata.
type PrivacyExportUser struct {
	TelegramUserID int64 `json:"telegram_user_id"`
}

// PrivacyExportCreator is the export-safe creator representation.
type PrivacyExportCreator struct {
	ID                   string               `json:"id"`
	TwitchLogin          string               `json:"twitch_login"`
	TwitchDisplayName    string               `json:"twitch_display_name,omitempty"`
	OwnerTelegramID      int64                `json:"owner_telegram_id"`
	GrantedScopes        []string             `json:"granted_scopes,omitempty"`
	UpdatedAt            time.Time            `json:"updated_at"`
	AuthStatus           CreatorAuthStatus    `json:"auth_status"`
	AuthErrorCode        string               `json:"auth_error_code,omitempty"`
	AuthStatusAt         time.Time            `json:"auth_status_at"`
	LastSyncAt           time.Time            `json:"last_sync_at"`
	LastBanSyncAt        time.Time            `json:"last_ban_sync_at"`
	LastNoticeAt         time.Time            `json:"last_notice_at"`
	BlocklistSyncEnabled bool                 `json:"blocklist_sync_enabled"`
	SubscriptionEndGrace SubscriptionEndGrace `json:"subscription_end_grace"`
}
