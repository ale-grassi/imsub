package core

import "time"

const (
	// OAuthStateTTL is how long a Telegram-issued Twitch authorization link remains valid.
	OAuthStateTTL = 30 * time.Minute
	// ViewerInviteLinkTTL is how long a viewer join link remains valid before Telegram expires it.
	ViewerInviteLinkTTL = 60 * time.Minute
	// BootstrapInviteLinkTTL is how long the temporary MTProto bootstrap invite remains valid.
	BootstrapInviteLinkTTL = 10 * time.Minute
)
