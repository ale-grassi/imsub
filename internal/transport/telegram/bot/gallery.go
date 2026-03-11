package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// PreviewButton is a gallery-friendly representation of one inline button.
type PreviewButton struct {
	Label   string
	Kind    string
	Target  string
	Style   string
	HasIcon bool
}

// PreviewView is a gallery-friendly render of one Telegram message state.
type PreviewView struct {
	Text           string
	DisablePreview bool
	Buttons        [][]PreviewButton
}

// PreviewScenario describes one meaningful message state for gallery rendering.
type PreviewScenario struct {
	ID     string
	Group  string
	Title  string
	Notes  string
	Render func(lang string) PreviewView
}

// PreviewScenarios returns a curated catalog of Telegram message states.
func PreviewScenarios() []PreviewScenario {
	return []PreviewScenario{
		{
			ID:    "viewer-onboarding",
			Group: "Viewer",
			Title: "/start onboarding prompt",
			Notes: "Viewer has not linked Twitch yet.",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerPromptView(lang, "Alex", "https://example.com/oauth/viewer"))
			},
		},
		{
			ID:    "viewer-linked-no-subs",
			Group: "Viewer",
			Title: "Linked account, no active subscriptions",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerLinkedView(lang, "viewer_name", core.JoinTargets{}))
			},
		},
		{
			ID:    "viewer-linked-with-groups",
			Group: "Viewer",
			Title: "Linked account, active subscriptions with join buttons",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerLinkedView(lang, "viewer_name", core.JoinTargets{
					ActiveCreatorNames: []string{"streamer_one", "streamer_two"},
					JoinLinks: []core.JoinLink{
						{CreatorName: "streamer_one", GroupName: "VIP Lounge", InviteLink: "https://t.me/+vip"},
						{CreatorName: "streamer_two", GroupName: "Patrons", InviteLink: "https://t.me/+patrons"},
					},
				}))
			},
		},
		{
			ID:    "viewer-linked-no-groups-yet",
			Group: "Viewer",
			Title: "Linked account, subscriptions found but no linked groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerLinkedView(lang, "viewer_name", core.JoinTargets{
					ActiveCreatorNames: []string{"streamer_one", "streamer_two"},
				}))
			},
		},
		{
			ID:    "viewer-oauth-exchange-fail",
			Group: "Viewer",
			Title: "Viewer OAuth exchange failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerOAuthFailureView(lang, msgOAuthExchangeFail))
			},
		},
		{
			ID:    "viewer-oauth-userinfo-fail",
			Group: "Viewer",
			Title: "Viewer OAuth user info failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerOAuthFailureView(lang, msgOAuthUserInfoFail))
			},
		},
		{
			ID:    "viewer-oauth-save-fail",
			Group: "Viewer",
			Title: "Viewer OAuth save failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerOAuthFailureView(lang, msgOAuthSaveFail))
			},
		},
		{
			ID:    "viewer-status-error",
			Group: "Viewer",
			Title: "Viewer status load error",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerStatusErrorView(lang))
			},
		},
		{
			ID:    "subscription-start",
			Group: "Viewer",
			Title: "Subscription start notification",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionStartView(lang, "streamer_one", core.JoinTargets{
					JoinLinks: []core.JoinLink{
						{CreatorName: "streamer_one", GroupName: "VIP Lounge", InviteLink: "https://t.me/+vip"},
					},
				}))
			},
		},
		{
			ID:    "subscription-end",
			Group: "Viewer",
			Title: "Subscription ended notification",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionEndView(lang, "viewer_name", "streamer_one"))
			},
		},
		{
			ID:    "subscription-grace-start",
			Group: "Viewer",
			Title: "Grace period started",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionGraceStartView(lang, "viewer_name", "streamer_one", sampleTime(72*time.Hour)))
			},
		},
		{
			ID:    "subscription-grace-expired",
			Group: "Viewer",
			Title: "Grace period expired",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionGraceExpiredView(lang, "viewer_name", "streamer_one"))
			},
		},
		{
			ID:    "creator-onboarding",
			Group: "Creator",
			Title: "/creator onboarding prompt",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorPromptView(lang, "https://example.com/oauth/creator", false))
			},
		},
		{
			ID:    "creator-reconnect-prompt",
			Group: "Creator",
			Title: "Creator reconnect prompt",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorPromptView(lang, "https://example.com/oauth/creator/reconnect", true))
			},
		},
		{
			ID:    "creator-reconnect-required",
			Group: "Creator",
			Title: "Creator reconnect-required notification",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorReconnectRequiredView(lang, "https://example.com/oauth/creator/reconnect"))
			},
		},
		{
			ID:    "creator-status-no-groups",
			Group: "Creator",
			Title: "Creator dashboard with no linked groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorStatusView(lang, "", sampleCreator(), core.Status{
					Auth:         core.CreatorAuthHealthy,
					LastSyncAt:   sampleTime(0),
					AuthStatusAt: sampleTime(-24 * time.Hour),
				}, nil))
			},
		},
		{
			ID:    "creator-status-active-single-group",
			Group: "Creator",
			Title: "Creator dashboard with one linked group",
			Render: func(lang string) PreviewView {
				creator := sampleCreator()
				creator.BlocklistSyncEnabled = true
				creator.SubscriptionEndGrace = core.SubscriptionEndGrace48h
				return previewFromShared(buildCreatorStatusView(lang, "", creator, core.Status{
					EventSub:           core.EventSubActive,
					Auth:               core.CreatorAuthHealthy,
					LastSyncAt:         sampleTime(-2 * time.Hour),
					SubscriberCount:    128,
					HasSubscriberCount: true,
					BannedUserCount:    4,
					HasBannedUserCount: true,
				}, []core.ManagedGroup{sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn)}))
			},
		},
		{
			ID:    "creator-status-eventsub-inactive",
			Group: "Creator",
			Title: "Creator dashboard with EventSub inactive",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorStatusView(lang, "", sampleCreator(), core.Status{
					EventSub:           core.EventSubInactive,
					Auth:               core.CreatorAuthHealthy,
					SubscriberCount:    23,
					HasSubscriberCount: true,
				}, []core.ManagedGroup{sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve)}))
			},
		},
		{
			ID:    "creator-status-eventsub-unknown",
			Group: "Creator",
			Title: "Creator dashboard with EventSub unknown",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorStatusView(lang, "", sampleCreator(), core.Status{
					EventSub:           core.EventSubUnknown,
					Auth:               core.CreatorAuthHealthy,
					BannedUserCount:    2,
					HasBannedUserCount: true,
				}, []core.ManagedGroup{sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve)}))
			},
		},
		{
			ID:    "creator-status-auth-reconnect",
			Group: "Creator",
			Title: "Creator dashboard with reconnect required",
			Render: func(lang string) PreviewView {
				status := core.Status{
					EventSub:           core.EventSubInactive,
					Auth:               core.CreatorAuthReconnectRequired,
					AuthStatusAt:       sampleTime(-6 * time.Hour),
					SubscriberCount:    12,
					HasSubscriberCount: true,
				}
				return previewFromShared(buildCreatorStatusView(lang, "https://example.com/oauth/creator/reconnect", sampleCreator(), status, []core.ManagedGroup{sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve)}))
			},
		},
		{
			ID:    "creator-eventsub-fail",
			Group: "Creator",
			Title: "Creator EventSub failure note",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorEventSubFail))
			},
		},
		{
			ID:    "creator-exchange-fail",
			Group: "Creator",
			Title: "Creator OAuth exchange failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorOAuthFailureView(lang, msgCreatorExchangeFail))
			},
		},
		{
			ID:    "creator-scope-missing",
			Group: "Creator",
			Title: "Creator OAuth scope missing",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorOAuthFailureView(lang, msgCreatorScopeMissing))
			},
		},
		{
			ID:    "creator-userinfo-fail",
			Group: "Creator",
			Title: "Creator OAuth user info failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorOAuthFailureView(lang, msgCreatorUserInfoFail))
			},
		},
		{
			ID:    "creator-store-fail",
			Group: "Creator",
			Title: "Creator OAuth/store failure",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorOAuthFailureView(lang, msgCreatorStoreFail))
			},
		},
		{
			ID:    "creator-reconnect-mismatch",
			Group: "Creator",
			Title: "Creator reconnect mismatch",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorOAuthFailureView(lang, msgCreatorReconnectMismatch))
			},
		},
		{
			ID:    "creator-manage-groups",
			Group: "Creator",
			Title: "Manage linked groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorManagedGroupsView(lang, sampleCreator(), []core.ManagedGroup{
					sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn),
					sampleGroup("Patrons", 1002, core.GroupPolicyKick),
				}, ""))
			},
		},
		{
			ID:    "creator-manage-groups-empty",
			Group: "Creator",
			Title: "Manage linked groups empty state",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorManagedGroupsView(lang, sampleCreator(), nil, ""))
			},
		},
		{
			ID:    "creator-group-settings",
			Group: "Creator",
			Title: "Group settings view",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupSettingsView(lang, sampleCreator(), sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn), creatorMenuCallback(), "Setup note"))
			},
		},
		{
			ID:    "creator-group-policy-picker",
			Group: "Creator",
			Title: "Group policy picker",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupPolicyPickerView(lang, sampleCreator(), sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve)))
			},
		},
		{
			ID:    "creator-group-policy-confirm",
			Group: "Creator",
			Title: "Group policy change confirmation",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupPolicyConfirmView(lang, sampleCreator(), sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve), core.GroupPolicyGraceWeek))
			},
		},
		{
			ID:    "creator-group-unregister-confirm",
			Group: "Creator",
			Title: "Creator unlink-group confirmation",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupUnregisterConfirmView(lang, sampleCreator(), sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve), creatorMenuCallback()))
			},
		},
		{
			ID:    "creator-group-unregistered",
			Group: "Creator",
			Title: "Creator group unlinked result",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupUnregistered, "VIP Lounge"))
			},
		},
		{
			ID:    "creator-group-unregistered-cleanup",
			Group: "Creator",
			Title: "Creator group unlinked with background cleanup",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupUnregisteredKicked, "VIP Lounge", 14))
			},
		},
		{
			ID:    "creator-group-unregistered-cleanup-failed",
			Group: "Creator",
			Title: "Creator group unlinked, cleanup queue failed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupUnregisteredKickAllFailed, "VIP Lounge", 14))
			},
		},
		{
			ID:    "creator-group-unavailable",
			Group: "Creator",
			Title: "Creator group unavailable notice",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorGroupUnavailable))
			},
		},
		{
			ID:    "creator-group-policy-updated",
			Group: "Creator",
			Title: "Group policy updated",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupPolicyUpdated, "VIP Lounge", formatGroupPolicyLine(lang, core.GroupPolicyKick)))
			},
		},
		{
			ID:    "creator-group-policy-same",
			Group: "Creator",
			Title: "Group policy unchanged",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupPolicySame, "VIP Lounge"))
			},
		},
		{
			ID:    "creator-group-policy-denied",
			Group: "Creator",
			Title: "Group policy denied",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorGroupPolicyDenied))
			},
		},
		{
			ID:    "creator-grace-picker",
			Group: "Creator",
			Title: "Grace-period picker",
			Render: func(lang string) PreviewView {
				creator := sampleCreator()
				creator.SubscriptionEndGrace = core.SubscriptionEndGrace24h
				return previewFromShared(buildCreatorGracePickerView(lang, creator))
			},
		},
		{
			ID:    "creator-grace-updated",
			Group: "Creator",
			Title: "Grace-period updated",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGraceUpdated, formatCreatorGraceValue(lang, core.SubscriptionEndGrace48h)))
			},
		},
		{
			ID:    "creator-blocklist-on",
			Group: "Creator",
			Title: "Twitch ban sync enabled notice",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorBlocklistOnNotice))
			},
		},
		{
			ID:    "creator-blocklist-off",
			Group: "Creator",
			Title: "Twitch ban sync disabled notice",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorBlocklistOffNotice))
			},
		},
		{
			ID:    "group-not-group",
			Group: "Group/Admin",
			Title: "Command used outside a group",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgGroupNotGroup))
			},
		},
		{
			ID:    "group-not-admin",
			Group: "Group/Admin",
			Title: "Non-admin tried group command",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgGroupNotAdmin))
			},
		},
		{
			ID:    "group-not-creator",
			Group: "Group/Admin",
			Title: "Group registration without creator setup",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgGroupNotCreator))
			},
		},
		{
			ID:    "group-policy-prompt",
			Group: "Group/Admin",
			Title: "Group policy prompt",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupRegistrationPolicyPromptView(lang, 0, -1001, 0, 0))
			},
		},
		{
			ID:    "group-policy-prompt-existing-members",
			Group: "Group/Admin",
			Title: "Group policy prompt with existing members warning",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupRegistrationPolicyPromptView(lang, 0, -1001, 0, 8))
			},
		},
		{
			ID:    "group-registration-registered",
			Group: "Group/Admin",
			Title: "Group linked result",
			Render: func(lang string) PreviewView {
				view, _ := buildGroupRegistrationView(lang, 0, usecase.RegisterGroupResult{
					Outcome: usecase.RegisterGroupOutcomeRegistered,
					Creator: sampleCreator(),
					ExistingGroup: core.ManagedGroup{
						GroupName: "VIP Lounge",
						Policy:    core.GroupPolicyObserveWarn,
					},
				})
				return previewFromGroupRegistration(view)
			},
		},
		{
			ID:    "group-registration-already-linked",
			Group: "Group/Admin",
			Title: "Group already linked result",
			Render: func(lang string) PreviewView {
				view, _ := buildGroupRegistrationView(lang, 0, usecase.RegisterGroupResult{
					Outcome: usecase.RegisterGroupOutcomeAlreadyLinked,
					Creator: sampleCreator(),
					ExistingGroup: core.ManagedGroup{
						GroupName: "VIP Lounge",
						Policy:    core.GroupPolicyObserveWarn,
					},
				})
				return previewFromGroupRegistration(view)
			},
		},
		{
			ID:    "group-registration-taken",
			Group: "Group/Admin",
			Title: "Group linked to another creator",
			Render: func(lang string) PreviewView {
				view, _ := buildGroupRegistrationView(lang, 0, usecase.RegisterGroupResult{
					Outcome:          usecase.RegisterGroupOutcomeTakenByOther,
					OtherCreatorName: "other_streamer",
				})
				return previewFromGroupRegistration(view)
			},
		},
		{
			ID:    "group-registration-not-creator",
			Group: "Group/Admin",
			Title: "Group registration by non-creator",
			Render: func(lang string) PreviewView {
				view, _ := buildGroupRegistrationView(lang, 0, usecase.RegisterGroupResult{
					Outcome: usecase.RegisterGroupOutcomeNotCreator,
				})
				return previewFromGroupRegistration(view)
			},
		},
		{
			ID:    "post-registration-group-ok",
			Group: "Group/Admin",
			Title: "Post-registration group message with passing settings",
			Render: func(lang string) PreviewView {
				out := renderPostRegistrationCopy(postRegistrationCopyInput{
					lang:          lang,
					groupName:     "VIP Lounge",
					creatorName:   "streamer_one",
					groupBaseText: fmt.Sprintf(i18n.Translate(lang, msgGroupRegistered), html.EscapeString("streamer_one")),
				}, nil)
				return PreviewView{Text: out.groupMessage}
			},
		},
		{
			ID:    "post-registration-dm-warnings",
			Group: "Group/Admin",
			Title: "Post-registration owner DM with settings warnings",
			Render: func(lang string) PreviewView {
				out := renderPostRegistrationCopy(postRegistrationCopyInput{
					lang:          lang,
					groupName:     "VIP Lounge",
					creatorName:   "streamer_one",
					groupBaseText: fmt.Sprintf(i18n.Translate(lang, msgGroupRegistered), html.EscapeString("streamer_one")),
				}, sampleGroupIssues(lang))
				return PreviewView{Text: out.finalDM}
			},
		},
		{
			ID:    "group-settings-warnings",
			Group: "Group/Admin",
			Title: "Settings warnings block",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupSettingWarningsView(lang, 0, sampleGroupIssues(lang)))
			},
		},
		{
			ID:    "group-bot-status-changed",
			Group: "Group/Admin",
			Title: "Bot status changed notice",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupBotStatusChangedView(lang))
			},
		},
		{
			ID:    "group-untracked-join-warning",
			Group: "Group/Admin",
			Title: "Unverified member join warning",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUntrackedJoinWarningView(lang))
			},
		},
		{
			ID:    "group-unregister-prompt",
			Group: "Group/Admin",
			Title: "Group unlink confirmation",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUnregisterPromptView(lang, 0, -1001))
			},
		},
		{
			ID:    "group-unregistered",
			Group: "Group/Admin",
			Title: "Group unlinked result",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUnregisteredView(lang, 0, usecase.UnregisterGroupResult{}))
			},
		},
		{
			ID:    "group-unregistered-cleanup",
			Group: "Group/Admin",
			Title: "Group unlinked with background cleanup",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUnregisteredView(lang, 0, usecase.UnregisterGroupResult{
					MemberAction:            core.CreatorResetKickTrackedMembers,
					TargetedMembershipCount: 11,
				}))
			},
		},
		{
			ID:    "group-unregistered-cleanup-failed",
			Group: "Group/Admin",
			Title: "Group unlinked, cleanup queue failed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUnregisteredView(lang, 0, usecase.UnregisterGroupResult{
					MemberAction:            core.CreatorResetKickTrackedMembers,
					TargetedMembershipCount: 11,
					CleanupQueueFailed:      true,
				}))
			},
		},
		{
			ID:    "group-removed-owner-dm",
			Group: "Group/Admin",
			Title: "Bot removed from managed group",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupBotRemovedOwnerView(lang, "VIP Lounge", false))
			},
		},
		{
			ID:    "group-removed-owner-dm-cleanup-lag",
			Group: "Group/Admin",
			Title: "Bot removed from managed group with cleanup lag",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupBotRemovedOwnerView(lang, "VIP Lounge", true))
			},
		},
		{
			ID:    "reset-empty",
			Group: "Reset",
			Title: "Reset empty state",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetEmptyView(lang))
			},
		},
		{
			ID:    "reset-scope-picker",
			Group: "Reset",
			Title: "Reset scope picker",
			Render: func(lang string) PreviewView {
				view, _ := buildResetPromptView(lang, core.ScopeState{
					HasIdentity: true,
					Identity:    core.UserIdentity{TwitchLogin: "viewer_name"},
					HasCreator:  true,
					Creator:     sampleCreator(),
				}, resetOriginViewer)
				return previewFromShared(view)
			},
		},
		{
			ID:    "reset-creator-action-picker",
			Group: "Reset",
			Title: "Choose what to do with tracked members",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetChooseCreatorActionBoth),
						html.EscapeString("viewer_name"),
						html.EscapeString("streamer_one"),
						2,
					),
					opts: buildResetCreatorActionMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-viewer",
			Group: "Reset",
			Title: "Confirm viewer-data deletion",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: fmt.Sprintf(i18n.Translate(lang, msgResetConfirmViewerHTML), html.EscapeString("viewer_name"), 3),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-creator",
			Group: "Reset",
			Title: "Confirm creator-data deletion",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: fmt.Sprintf(i18n.Translate(lang, msgResetConfirmCreatorHTML), html.EscapeString("streamer_one"), 1, 2, i18n.Translate(lang, msgResetActionKickLine)),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-both",
			Group: "Reset",
			Title: "Confirm deleting all linked data",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: fmt.Sprintf(i18n.Translate(lang, msgResetConfirmBothHTML), html.EscapeString("viewer_name"), html.EscapeString("streamer_one"), 1, 3, 2, i18n.Translate(lang, msgResetActionKickLine)),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-done-viewer",
			Group: "Reset",
			Title: "Viewer-data deleted",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetExecutionView(lang, usecase.ResetResult{
					Scope:       usecase.ResetScopeViewer,
					ViewerLogin: "viewer_name",
					GroupCount:  3,
				}))
			},
		},
		{
			ID:    "reset-done-creator",
			Group: "Reset",
			Title: "Creator-data deleted",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetExecutionView(lang, usecase.ResetResult{
					Scope:        usecase.ResetScopeCreator,
					DeletedCount: 1,
					DeletedNames: []string{"streamer_one"},
					CreatorCleanup: core.CreatorGroupCleanupSummary{
						Action:                  core.CreatorResetKickTrackedMembers,
						ManagedGroupCount:       2,
						TargetedMembershipCount: 14,
						Queued:                  true,
					},
				}))
			},
		},
		{
			ID:    "reset-done-both",
			Group: "Reset",
			Title: "All linked data deleted",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetExecutionView(lang, usecase.ResetResult{
					Scope:        usecase.ResetScopeBoth,
					ViewerLogin:  "viewer_name",
					GroupCount:   3,
					DeletedCount: 1,
					DeletedNames: []string{"streamer_one"},
					CreatorCleanup: core.CreatorGroupCleanupSummary{
						Action:                  core.CreatorResetKickTrackedMembers,
						ManagedGroupCount:       2,
						TargetedMembershipCount: 14,
						Queued:                  true,
					},
				}))
			},
		},
		{
			ID:    "reset-exit",
			Group: "Reset",
			Title: "Reset cancelled",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgResetExitHTML))
			},
		},
		{
			ID:    "reset-error",
			Group: "Reset",
			Title: "Reset error",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetErrorView(lang))
			},
		},
		{
			ID:    "cleanup-group-done",
			Group: "Cleanup",
			Title: "Group cleanup completed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:           core.MemberCleanupKindGroupUnregistration,
					GroupName:      "VIP Lounge",
					TargetedCount:  12,
					SucceededCount: 12,
				}))
			},
		},
		{
			ID:    "cleanup-group-partial",
			Group: "Cleanup",
			Title: "Group cleanup completed with leftovers",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:           core.MemberCleanupKindGroupUnregistration,
					GroupName:      "VIP Lounge",
					TargetedCount:  12,
					SucceededCount: 9,
					FailedCount:    3,
				}))
			},
		},
		{
			ID:    "cleanup-group-failed",
			Group: "Cleanup",
			Title: "Group cleanup failed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:          core.MemberCleanupKindGroupUnregistration,
					GroupName:     "VIP Lounge",
					TargetedCount: 12,
					FailedCount:   12,
				}))
			},
		},
		{
			ID:    "cleanup-reset-done",
			Group: "Cleanup",
			Title: "Creator reset cleanup completed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:              core.MemberCleanupKindCreatorReset,
					CreatorLogin:      "streamer_one",
					ManagedGroupCount: 2,
					TargetedCount:     14,
					SucceededCount:    14,
				}))
			},
		},
		{
			ID:    "cleanup-reset-partial",
			Group: "Cleanup",
			Title: "Creator reset cleanup completed with leftovers",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:              core.MemberCleanupKindCreatorReset,
					CreatorLogin:      "streamer_one",
					ManagedGroupCount: 2,
					TargetedCount:     14,
					SucceededCount:    10,
					FailedCount:       4,
				}))
			},
		},
		{
			ID:    "cleanup-reset-failed",
			Group: "Cleanup",
			Title: "Creator reset cleanup failed",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:              core.MemberCleanupKindCreatorReset,
					CreatorLogin:      "streamer_one",
					ManagedGroupCount: 2,
					TargetedCount:     14,
					FailedCount:       14,
				}))
			},
		},
	}
}

func previewFromShared(view sharedView) PreviewView {
	return PreviewView{
		Text:           view.text,
		DisablePreview: view.opts.DisablePreview,
		Buttons:        previewButtons(view.opts.Markup),
	}
}

func previewFromGroupRegistration(view groupRegistrationView) PreviewView {
	return PreviewView{
		Text:           view.text,
		DisablePreview: view.opts.DisablePreview,
		Buttons:        previewButtons(view.opts.Markup),
	}
}

func previewButtons(markup *telego.InlineKeyboardMarkup) [][]PreviewButton {
	if markup == nil {
		return nil
	}
	rows := make([][]PreviewButton, 0, len(markup.InlineKeyboard))
	for _, row := range markup.InlineKeyboard {
		buttons := make([]PreviewButton, 0, len(row))
		for _, button := range row {
			kind := "callback"
			target := button.CallbackData
			switch {
			case strings.TrimSpace(button.URL) != "":
				kind = "url"
				target = button.URL
			case button.CopyText != nil:
				kind = "copy"
				target = button.CopyText.Text
			}
			buttons = append(buttons, PreviewButton{
				Label:   button.Text,
				Kind:    kind,
				Target:  target,
				Style:   button.Style,
				HasIcon: strings.TrimSpace(button.IconCustomEmojiID) != "",
			})
		}
		rows = append(rows, buttons)
	}
	return rows
}

func buildTextViewWithArgs(lang, key string, args ...any) sharedView {
	return sharedView{text: fmt.Sprintf(i18n.Translate(lang, key), args...)}
}

func sampleCreator() core.Creator {
	return core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer_one",
		OwnerTelegramID: 77,
	}
}

func sampleGroup(name string, chatID int64, policy core.GroupPolicy) core.ManagedGroup {
	return core.ManagedGroup{
		ChatID:    chatID,
		CreatorID: "creator-1",
		GroupName: name,
		Policy:    policy,
	}
}

func sampleGroupIssues(lang string) []string {
	return []string{
		i18n.Translate(lang, msgGroupWarnPublic),
		fmt.Sprintf(i18n.Translate(lang, msgGroupWarnUntrackedUsers), 4),
		i18n.Translate(lang, msgGroupWarnBotNoInvite),
	}
}

func sampleTime(offset time.Duration) time.Time {
	base := time.Date(2026, time.March, 11, 10, 0, 0, 0, time.UTC)
	return base.Add(offset)
}

func buildResetCreatorActionMarkup(lang string) client.MessageOptions {
	return client.MessageOptions{
		Markup: tu.InlineKeyboard(
			tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnResetKeepMembers), resetActionPickCallback(resetOriginViewer, resetScopeBoth, core.CreatorResetKeepMembers))),
			tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnResetKickTrackedMembers), resetActionPickCallback(resetOriginViewer, resetScopeBoth, core.CreatorResetKickTrackedMembers), "5258318620722733379").WithStyle("danger")),
			tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), resetBackCallback(resetOriginViewer))),
		),
	}
}

func clientResetConfirmMarkup(lang string) client.MessageOptions {
	return client.MessageOptions{
		Markup: ui.ResetConfirmMarkup(lang, "preview:confirm", "preview:back"),
	}
}
