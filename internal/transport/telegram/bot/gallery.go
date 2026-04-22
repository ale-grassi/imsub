package bot

import (
	"fmt"
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
	Label             string
	Kind              string
	Target            string
	Style             string
	HasIcon           bool
	IconCustomEmojiID string
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
				return previewFromShared(buildViewerLinkedView(lang, core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"}, core.JoinTargets{}))
			},
		},
		{
			ID:    "viewer-linked-with-groups",
			Group: "Viewer",
			Title: "Linked account, active subscriptions with join buttons",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerLinkedView(lang, core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"}, core.JoinTargets{
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
				return previewFromShared(buildViewerLinkedView(lang, core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"}, core.JoinTargets{
					ActiveCreatorNames: []string{"streamer_one", "streamer_two"},
				}))
			},
		},
		{
			ID:    "viewer-error",
			Group: "Viewer",
			Title: "Generic viewer error",
			Notes: "Covers OAuth failures and status load failures while keeping Refresh and Reset available.",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildViewerErrorView(lang))
			},
		},
		{
			ID:    "subscription-start",
			Group: "Viewer",
			Title: "Subscription start notification",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionStartView(lang, core.UserIdentity{
					TwitchLogin:       "viewer_one",
					TwitchDisplayName: "Viewer One",
				}, "streamer_one", core.JoinTargets{
					ActiveCreatorNames: []string{"streamer_one"},
					JoinLinks: []core.JoinLink{
						{CreatorName: "streamer_one", GroupName: "VIP Lounge", InviteLink: "https://t.me/+vip"},
					},
				}))
			},
		},
		{
			ID:    "subscription-grace-start",
			Group: "Viewer",
			Title: "Grace period active",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionGraceStartView(lang, "streamer_one", sampleTime(72*time.Hour)))
			},
		},
		{
			ID:    "subscription-end",
			Group: "Viewer",
			Title: "Subscription expired",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildSubscriptionEndView(lang, "streamer_one"))
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
			ID:    "creator-status-no-groups",
			Group: "Creator",
			Title: "Creator dashboard with no linked groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorStatusView(lang, "", "imsub_bot", sampleCreator(), core.Status{
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
				return previewFromShared(buildCreatorStatusView(lang, "", "imsub_bot", creator, core.Status{
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
			ID:    "creator-reconnect-mismatch",
			Group: "Creator",
			Title: "Creator reconnect mismatch",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextView(lang, msgCreatorReconnectMismatch))
			},
		},
		{
			ID:    "creator-manage-groups",
			Group: "Creator",
			Title: "Manage groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorManagedGroupsView(lang, []core.ManagedGroup{
					sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn),
					sampleGroup("Patrons", 1002, core.GroupPolicyKick),
				}, ""))
			},
		},
		{
			ID:    "creator-group-settings",
			Group: "Creator",
			Title: "Group settings",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupSettingsView(lang, sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn), creatorMenuCallback(), ""))
			},
		},
		{
			ID:    "creator-group-language-picker",
			Group: "Creator",
			Title: "Group language picker",
			Render: func(lang string) PreviewView {
				group := sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserveWarn)
				group.Language = "it"
				return previewFromShared(buildCreatorGroupLanguagePickerView(lang, group))
			},
		},
		{
			ID:    "creator-group-policy-picker",
			Group: "Creator",
			Title: "Group policy picker",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupPolicyPickerView(lang, sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve)))
			},
		},
		{
			ID:    "creator-group-policy-confirm",
			Group: "Creator",
			Title: "Group policy change confirmation",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupPolicyConfirmView(lang, sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve), core.GroupPolicyGraceWeek))
			},
		},
		{
			ID:    "creator-group-unregister-confirm",
			Group: "Creator",
			Title: "Creator unlink-group confirmation",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildCreatorGroupUnregisterConfirmView(lang, sampleGroup("VIP Lounge", 1001, core.GroupPolicyObserve), creatorMenuCallback()))
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
			ID:    "creator-group-policy-updated",
			Group: "Creator",
			Title: "Group policy saved",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupPolicyUpdated, "VIP Lounge", formatCreatorGroupPolicyValue(lang, core.GroupPolicyKick)))
			},
		},
		{
			ID:    "creator-group-language-updated",
			Group: "Creator",
			Title: "Group language saved",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildTextViewWithArgs(lang, msgCreatorGroupLanguageUpdated, "VIP Lounge", formatGroupLanguageValue(lang, "it")))
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
			Title: "Group policy prompt with existing members",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupRegistrationPolicyPromptView(lang, 0, -1001, 0, 8))
			},
		},
		{
			ID:    "group-registration-registered",
			Group: "Group/Admin",
			Title: "Group linked result",
			Render: func(lang string) PreviewView {
				view, _ := buildGroupRegistrationView(lang, 0, "imsub_bot", usecase.RegisterGroupResult{
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
			ID:    "group-settings-warnings",
			Group: "Group/Admin",
			Title: "Settings warnings block",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupSettingWarningsView(lang, 0, sampleGroupIssues(lang)))
			},
		},
		{
			ID:    "group-setup-permissions",
			Group: "Group/Admin",
			Title: "Step 3 permissions prompt",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupSetupPermissionsView(lang))
			},
		},
		{
			ID:    "group-permissions-blocked",
			Group: "Group/Admin",
			Title: "Step 3 permissions blocked on /linkgroup",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupPermissionsBlockedView(lang, 0))
			},
		},
		{
			ID:    "group-functionality-compromised",
			Group: "Group/Admin",
			Title: "Managed group permissions downgraded",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupCompromisedView(lang, formatMissingRequiredPermissions(lang, membershipCapabilitySnapshot{isAdmin: true, canInviteUsers: false, canRestrictMembers: true})))
			},
		},
		{
			ID:    "group-functionality-compromised-owner-dm",
			Group: "Group/Admin",
			Title: "Owner DM for compromised group permissions",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupCompromisedOwnerView(lang, "VIP Lounge", formatMissingRequiredPermissions(lang, membershipCapabilitySnapshot{isAdmin: true, canInviteUsers: true, canRestrictMembers: false})))
			},
		},
		{
			ID:    "group-untracked-join-warning",
			Group: "Group/Admin",
			Title: "Unverified member join warning",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupUntrackedJoinWarningView(lang, 42, "Alex"))
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
				return previewFromShared(buildGroupUnregisteredView(lang, 0))
			},
		},
		{
			ID:    "group-removed-owner-dm",
			Group: "Group/Admin",
			Title: "Bot removed from managed group",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildGroupBotRemovedOwnerView(lang, "VIP Lounge"))
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
			Title: "Reset scope picker with viewer and creator data",
			Render: func(lang string) PreviewView {
				view := buildResetPromptView(lang, core.ScopeState{
					HasIdentity: true,
					Identity:    core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"},
					HasCreator:  true,
					Creator:     sampleCreator(),
				}, resetOriginViewer)
				return previewFromShared(view)
			},
		},
		{
			ID:    "reset-scope-picker-viewer-only",
			Group: "Reset",
			Title: "Reset scope picker with only viewer data",
			Render: func(lang string) PreviewView {
				view := buildResetPromptView(lang, core.ScopeState{
					HasIdentity: true,
					Identity:    core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"},
				}, resetOriginViewer)
				return previewFromShared(view)
			},
		},
		{
			ID:    "reset-scope-picker-creator-only",
			Group: "Reset",
			Title: "Reset scope picker with only creator data",
			Render: func(lang string) PreviewView {
				view := buildResetPromptView(lang, core.ScopeState{
					HasCreator: true,
					Creator:    sampleCreator(),
				}, resetOriginCreator)
				return previewFromShared(view)
			},
		},
		{
			ID:    "reset-creator-action-picker",
			Group: "Reset",
			Title: "Choose what to do with group members",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: i18n.Translate(lang, msgResetChooseCreatorActionBoth),
					opts: buildResetCreatorActionMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-viewer",
			Group: "Reset",
			Title: "Confirm viewer-data deletion",
			Render: func(lang string) PreviewView {
				groupNames := []string{"VIP Lounge", "Patrons", "Insiders"}
				return previewFromShared(sharedView{
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetConfirmViewerHTML),
						twitchProfileHTML("viewer_name", "Viewer Name"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_subscriber_groups_title"), groupNames),
						resetViewerConsequenceLine(lang, len(groupNames)),
					),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-viewer-no-groups",
			Group: "Reset",
			Title: "Confirm viewer-data deletion with no groups",
			Render: func(lang string) PreviewView {
				groupNames := []string(nil)
				return previewFromShared(sharedView{
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetConfirmViewerHTML),
						twitchProfileHTML("viewer_name", "Viewer Name"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_subscriber_groups_title"), groupNames),
						resetViewerConsequenceLine(lang, len(groupNames)),
					),
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
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetConfirmCreatorHTML),
						twitchProfileHTML("streamer_one", "Streamer One"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_managed_groups_title"), []string{"VIP Lounge", "Subscriber Chat"}),
						resetCreatorConsequenceLine(lang, 2),
						resetCreatorActionSummaryText(lang, core.CreatorResetKickTrackedMembers, 2),
					),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-creator-no-groups",
			Group: "Reset",
			Title: "Confirm creator-data deletion with no groups",
			Render: func(lang string) PreviewView {
				return previewFromShared(sharedView{
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetConfirmCreatorHTML),
						twitchProfileHTML("streamer_one", "Streamer One"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_managed_groups_title"), nil),
						resetCreatorConsequenceLine(lang, 0),
						resetCreatorActionSummaryText(lang, core.CreatorResetKickTrackedMembers, 0),
					),
					opts: clientResetConfirmMarkup(lang),
				})
			},
		},
		{
			ID:    "reset-confirm-both",
			Group: "Reset",
			Title: "Confirm deleting all linked data",
			Render: func(lang string) PreviewView {
				viewerGroups := []string{"VIP Lounge", "Patrons", "Insiders"}
				return previewFromShared(sharedView{
					text: fmt.Sprintf(
						i18n.Translate(lang, msgResetConfirmBothHTML),
						twitchProfileHTML("viewer_name", "Viewer Name"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_subscriber_groups_title"), viewerGroups),
						twitchProfileHTML("streamer_one", "Streamer One"),
						resetGroupSection(lang, i18n.Translate(lang, "reset_managed_groups_title"), []string{"VIP Lounge", "Subscriber Chat"}),
						resetViewerConsequenceLine(lang, len(viewerGroups)),
						resetCreatorConsequenceLine(lang, 2),
						resetCreatorActionSummaryText(lang, core.CreatorResetKickTrackedMembers, 2),
					),
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
					Scope:             usecase.ResetScopeViewer,
					ViewerLogin:       "viewer_name",
					ViewerDisplayName: "Viewer Name",
					GroupNames:        []string{"VIP Lounge", "Patrons", "Insiders"},
				}))
			},
		},
		{
			ID:    "reset-done-creator",
			Group: "Reset",
			Title: "Creator-data deleted",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildResetExecutionView(lang, usecase.ResetResult{
					Scope:              usecase.ResetScopeCreator,
					DeletedCount:       1,
					DeletedNames:       []string{"streamer_one"},
					CreatorLogin:       "streamer_one",
					CreatorDisplayName: "Streamer One",
					CreatorCleanup: core.CreatorGroupCleanupSummary{
						Action:                  core.CreatorResetKickTrackedMembers,
						ManagedGroupCount:       2,
						GroupNames:              []string{"VIP Lounge", "Subscriber Chat"},
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
					Scope:              usecase.ResetScopeBoth,
					ViewerLogin:        "viewer_name",
					ViewerDisplayName:  "Viewer Name",
					GroupNames:         []string{"VIP Lounge", "Patrons", "Insiders"},
					DeletedCount:       1,
					DeletedNames:       []string{"streamer_one"},
					CreatorLogin:       "streamer_one",
					CreatorDisplayName: "Streamer One",
					CreatorCleanup: core.CreatorGroupCleanupSummary{
						Action:                  core.CreatorResetKickTrackedMembers,
						ManagedGroupCount:       2,
						GroupNames:              []string{"VIP Lounge", "Subscriber Chat"},
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
			ID:    "info",
			Group: "Info",
			Title: "/info about screen",
			Render: func(lang string) PreviewView {
				return previewFromShared(buildInfoView(lang))
			},
		},
		{
			ID:    "cleanup-reset-warning",
			Group: "Cleanup",
			Title: "Creator reset cleanup warning",
			Render: func(lang string) PreviewView {
				view, _ := buildMemberCleanupResultView(lang, core.MemberCleanupResult{
					Kind:              core.MemberCleanupKindCreatorReset,
					CreatorLogin:      "streamer_one",
					ManagedGroupCount: 2,
					GroupNames:        []string{"VIP Lounge", "Subscriber Chat"},
					TargetedCount:     14,
					SucceededCount:    10,
					FailedCount:       4,
				})
				return previewFromShared(view)
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
				Label:             button.Text,
				Kind:              kind,
				Target:            target,
				Style:             button.Style,
				HasIcon:           strings.TrimSpace(button.IconCustomEmojiID) != "",
				IconCustomEmojiID: button.IconCustomEmojiID,
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
		ID:                "creator-1",
		TwitchLogin:       "streamer_one",
		TwitchDisplayName: "Streamer One",
		OwnerTelegramID:   77,
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
