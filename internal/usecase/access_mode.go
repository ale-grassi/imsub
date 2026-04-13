package usecase

// ViewerAccessMode identifies how viewer access was resolved.
type ViewerAccessMode string

const (
	// ViewerAccessModeUnlinked means the Telegram user has no viewer access.
	ViewerAccessModeUnlinked ViewerAccessMode = "unlinked"
	// ViewerAccessModeLinked means the Telegram user is using linked Twitch identity access.
	ViewerAccessModeLinked ViewerAccessMode = "linked"
	// ViewerAccessModeGod means the Telegram user is globally bypassed.
	ViewerAccessModeGod ViewerAccessMode = "god"
)
