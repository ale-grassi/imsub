package core

// Tracked-member source labels. These are persisted via AddTrackedGroupMember
// and surfaced in metrics/logs, so values must stay stable.
const (
	SourceGodList                = "god_list"
	SourceGodListGracePolicy     = "god_list_grace_policy"
	SourceGodListKickPolicy      = "god_list_kick_policy"
	SourceMTProtoBootstrap       = "mtproto_bootstrap"
	SourceObservedExistingMember = "observed_existing_member"
	SourceViewerExistingMember   = "viewer_existing_member"
	SourceViewerJoinTarget       = "viewer_join_target"
	SourceGracePolicyRescue      = "grace_policy_rescue"
	SourceKickPolicyRescue       = "kick_policy_rescue"
)
