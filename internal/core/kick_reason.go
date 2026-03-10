package core

// KickReason labels why a Telegram group removal was attempted.
type KickReason string

// Kick reasons are low-cardinality workflow labels for Telegram member removals.
const (
	KickReasonBlocklist           KickReason = "blocklist"
	KickReasonCreatorReset        KickReason = "creator_reset"
	KickReasonDisplacedUser       KickReason = "displaced_user"
	KickReasonGroupGracePolicy    KickReason = "group_grace_policy"
	KickReasonGroupPolicy         KickReason = "group_policy"
	KickReasonGroupUnregistration KickReason = "group_unregistration"
	KickReasonSubscriptionEnd     KickReason = "subscription_end"
	KickReasonSubscriptionGrace   KickReason = "subscription_grace"
	KickReasonViewerReset         KickReason = "viewer_reset"
)
