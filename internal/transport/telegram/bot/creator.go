package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	msgErrCreatorLink            = "err_creator_link"
	msgCreatorRegisterInfo       = "creator_register_info"
	msgCreatorRegisteredNoGroup  = "creator_registered_no_group_html"
	msgCreatorRegistered         = "creator_registered_html"
	msgCreatorEventSubActive     = "creator_eventsub_active"
	msgCreatorEventSubInactive   = "creator_eventsub_inactive"
	msgCreatorEventSubUnknown    = "creator_eventsub_unknown"
	msgCreatorEventSubFail       = "creator_eventsub_fail"
	msgCreatorAuthHealthy        = "creator_auth_healthy"
	msgCreatorAuthReconnect      = "creator_auth_reconnect_required"
	msgCreatorSubscribersPending = "creator_subscribers_pending"
	msgCreatorSubscribersReady   = "creator_subscribers_ready"
	msgCreatorGroupsNone         = "creator_groups_none"
	msgCreatorExchangeFail       = "creator_exchange_fail"
	msgCreatorReconnectInfo      = "creator_reconnect_info"
	msgCreatorReconnectMismatch  = "creator_reconnect_mismatch"
	msgCreatorReconnectNeeded    = "creator_reconnect_needed"
	msgCreatorScopeMissing       = "creator_scope_missing"
	msgCreatorUserInfoFail       = "creator_userinfo_fail"
	msgCreatorStoreFail          = "creator_store_fail"
	msgCreatorManageGroupsHTML   = "creator_manage_groups_html"
	msgCreatorManageGroupsEmpty  = "creator_manage_groups_empty_html"
	msgCreatorGroupSettingsHTML  = "creator_group_settings_html"
	msgCreatorGroupPolicyHTML    = "creator_group_policy_picker_html"
	msgCreatorGroupPolicyConfirm = "creator_group_policy_confirm_html"
	msgCreatorUnregisterConfirm  = "creator_unregister_confirm_html"
	msgCreatorGroupUnregistered  = "creator_group_unregistered_html"
	msgCreatorGroupUnavailable   = "creator_group_unavailable_html"
	msgCreatorGroupPolicyUpdated = "creator_group_policy_updated_html"
	msgCreatorGroupPolicySame    = "creator_group_policy_same_html"
	msgCreatorGroupPolicyDenied  = "creator_group_policy_not_owner"
	msgCreatorBlocklistEnabled   = "creator_blocklist_enabled"
	msgCreatorBlocklistDisabled  = "creator_blocklist_disabled"
	msgCreatorBlocklistOnNotice  = "creator_blocklist_on_notice"
	msgCreatorBlocklistOffNotice = "creator_blocklist_off_notice"

	btnRegisterCreatorOpen = "btn_register_creator_open"
	btnReconnectCreator    = "btn_reconnect_creator"
	btnManageGroup         = "btn_manage_group"
	btnChangeGroupPolicy   = "btn_change_group_policy"
	btnConfirmGroupPolicy  = "btn_confirm_group_policy"
	btnUnregisterGroup     = "btn_unregister_group"
)

// onCreatorCommand handles /creator by showing the creator home/status flow.
func (c *Bot) onCreatorCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleCreatorStart(ctx, msg.From.ID, 0, lang)
	return nil
}

func (c *Bot) handleCreatorCallback(ctx context.Context, userID int64, editMsgID int, lang string, action callbackAction) string {
	switch action.verb {
	case callbackVerbRefresh, callbackVerbRegister:
		return c.handleCreatorStart(ctx, userID, editMsgID, lang)
	case callbackVerbReconnect:
		return c.handleCreatorReconnectStart(ctx, userID, editMsgID, lang)
	case callbackVerbOpen:
		if action.target == creatorCallbackTargetGroups {
			return c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, "")
		}
		if action.target == creatorCallbackTargetGroupConfirm {
			return c.replyCreatorGroupUnregisterConfirm(ctx, userID, editMsgID, lang, action.chatID)
		}
		if action.target == creatorCallbackTargetPolicy {
			return c.replyCreatorGroupPolicyPicker(ctx, userID, editMsgID, lang, action.chatID)
		}
	case callbackVerbPick:
		if action.target == creatorCallbackTargetGroup {
			return c.replyCreatorGroupSettings(ctx, userID, editMsgID, lang, action.chatID, "")
		}
		if action.target == creatorCallbackTargetPolicy {
			return c.replyCreatorGroupPolicyConfirm(ctx, userID, editMsgID, lang, action.chatID, action.policy)
		}
	case callbackVerbBack:
		if action.target == creatorCallbackTargetGroups {
			return c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, "")
		}
		if action.target == creatorCallbackTargetPolicy {
			return c.replyCreatorGroupSettings(ctx, userID, editMsgID, lang, action.chatID, "")
		}
	case callbackVerbMenu:
		return c.handleCreatorStart(ctx, userID, editMsgID, lang)
	case callbackVerbExecute:
		if action.target == creatorCallbackTargetGroup {
			return c.executeCreatorGroupUnregister(ctx, userID, editMsgID, lang, action.chatID)
		}
		if action.target == creatorCallbackTargetPolicy {
			return c.executeCreatorGroupPolicyUpdate(ctx, userID, editMsgID, lang, action.chatID, action.policy)
		}
		if action.target == creatorCallbackTargetBlocklist {
			return c.toggleCreatorBlocklist(ctx, userID, editMsgID, lang)
		}
	case callbackVerbCancel:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return ""
	default:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return ""
	}
	c.log().Warn("unsupported creator callback action", "telegram_user_id", userID, "verb", action.verb, "target", action.target, "chat_id", action.chatID)
	return ""
}

func (c *Bot) handleCreatorStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	_, ok, err := c.creatorStatus.LoadOwnedCreator(ctx, telegramUserID)
	if err != nil {
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	if ok {
		c.replyCreatorStatus(ctx, telegramUserID, editMsgID, lang)
		return ""
	}

	return c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, false)
}

func (c *Bot) handleCreatorReconnectStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, true)
}

func (c *Bot) creatorReconnectURL(ctx context.Context, telegramUserID int64, lang string) (string, error) {
	payload := core.OAuthStatePayload{
		Mode:           core.OAuthModeCreator,
		TelegramUserID: telegramUserID,
		Language:       lang,
		Reconnect:      true,
	}
	state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
	if err != nil {
		return "", err
	}
	return c.oauthStartURL(state), nil
}

func (c *Bot) replyCreatorOAuthPrompt(ctx context.Context, telegramUserID int64, editMsgID int, lang string, reconnect bool) string {
	payload := core.OAuthStatePayload{
		Mode:            core.OAuthModeCreator,
		TelegramUserID:  telegramUserID,
		Language:        lang,
		PromptMessageID: editMsgID,
		Reconnect:       reconnect,
	}
	state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
	if err != nil {
		view := buildCreatorLinkErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	url := c.oauthStartURL(state)
	view := buildCreatorPromptView(lang, url, reconnect)
	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}
	messageID := c.sendMsg(ctx, telegramUserID, view.text, &view.opts)
	if messageID == 0 {
		c.invalidateOAuthState(ctx, state)
		return ""
	}
	payload.PromptMessageID = messageID
	if err := c.store.SaveOAuthState(ctx, state, payload, 10*time.Minute); err != nil {
		c.log().Warn("saveOAuthState creator prompt message update failed", "error", err)
	}
	return ""
}

func (c *Bot) replyCreatorStatus(ctx context.Context, telegramUserID int64, editMsgID int, lang string) {
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.creatorStatus.LoadStatus(statusCtx, telegramUserID)
	if err != nil {
		if !res.HasCreator {
			c.log().Warn("LoadStatus failed", "telegram_user_id", telegramUserID, "error", err)
			view := buildCreatorStatusErrorView(lang)
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return
		}
		c.log().Warn("LoadStatus degraded", "telegram_user_id", telegramUserID, "error", err)
	}
	if !res.HasCreator {
		c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, false)
		return
	}
	if res.GroupsError != nil {
		c.log().Warn("LoadManagedGroups failed", "creator_id", res.Creator.ID, "error", res.GroupsError)
	}
	if res.StatusError != nil {
		c.log().Warn("LoadStatus degraded", "creator_id", res.Creator.ID, "error", res.StatusError)
	}
	reconnectURL := ""
	if res.Status.Auth == core.CreatorAuthReconnectRequired {
		reconnectURL, err = c.creatorReconnectURL(ctx, telegramUserID, lang)
		if err != nil {
			c.log().Warn("creatorReconnectURL failed", "telegram_user_id", telegramUserID, "creator_id", res.Creator.ID, "error", err)
		}
	}
	view := buildCreatorStatusView(lang, reconnectURL, res.Creator, res.Status, res.Groups)

	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return
	}

	c.sendMsg(ctx, telegramUserID, view.text, &view.opts)
}

// HandleCreatorOAuthCallback executes creator OAuth callback side effects and notifications.
func (c *Bot) HandleCreatorOAuthCallback(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, creatorName string, err error) {
	res, flowErr := c.creatorOAuth.Complete(ctx, code, payload)
	if flowErr != nil {
		var fe *core.FlowError
		if errors.As(flowErr, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				view := buildCreatorOAuthFailureView(lang, msgCreatorExchangeFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator token exchange: %w", flowErr)
			case core.KindScopeMissing:
				view := buildCreatorOAuthFailureView(lang, msgCreatorScopeMissing)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator scope missing: %w", flowErr)
			case core.KindUserInfo:
				view := buildCreatorOAuthFailureView(lang, msgCreatorUserInfoFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator user info: %w", flowErr)
			case core.KindStore:
				view := buildCreatorOAuthFailureView(lang, msgCreatorStoreFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator store fail: %w", flowErr)
			case core.KindSave:
				view := buildCreatorOAuthFailureView(lang, msgCreatorStoreFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator save fail: %w", flowErr)
			case core.KindCreatorMismatch:
				view := buildCreatorOAuthFailureView(lang, msgCreatorReconnectMismatch)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator reconnect mismatch: %w", flowErr)
			}
		}
		view := buildCreatorOAuthFailureView(lang, msgCreatorStoreFail)
		c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
		return res.ResultLabel, "", fmt.Errorf("creator unexpected fail: %w", flowErr)
	}
	creator := res.Creator
	c.log().Debug("creator oauth exchange success", "creator_id", creator.ID, "creator_login", creator.TwitchLogin, "owner_telegram_id", creator.OwnerTelegramID)
	if payload.PromptMessageID != 0 {
		c.deleteMessage(ctx, payload.TelegramUserID, payload.PromptMessageID)
	}
	c.replyCreatorStatus(ctx, payload.TelegramUserID, 0, lang)
	return res.ResultLabel, res.BroadcasterDisplayName, nil
}

// NotifyCreatorReconnectRequired sends a one-shot stale-auth notification to a creator owner.
func (c *Bot) NotifyCreatorReconnectRequired(ctx context.Context, creator core.Creator) error {
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, creator.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = identity.Language
	}
	reconnectURL, err := c.creatorReconnectURL(ctx, creator.OwnerTelegramID, lang)
	if err != nil {
		return fmt.Errorf("creator reconnect url: %w", err)
	}
	view := buildCreatorReconnectRequiredView(lang, reconnectURL)
	if messageID := c.sendMsg(ctx, creator.OwnerTelegramID, view.text, &view.opts); messageID == 0 {
		return errReconnectNotificationSend
	}
	return nil
}

func creatorEventSubStatusText(status core.Status, lang string) string {
	switch status.EventSub {
	case core.EventSubActive:
		return i18n.Translate(lang, msgCreatorEventSubActive)
	case core.EventSubInactive:
		return i18n.Translate(lang, msgCreatorEventSubInactive)
	case core.EventSubUnknown:
		return i18n.Translate(lang, msgCreatorEventSubUnknown)
	default:
		return i18n.Translate(lang, msgCreatorEventSubUnknown)
	}
}

func creatorAuthStatusText(status core.Status, lang string) string {
	switch status.Auth {
	case core.CreatorAuthReconnectRequired:
		return i18n.Translate(lang, msgCreatorAuthReconnect)
	case core.CreatorAuthHealthy:
		return i18n.Translate(lang, msgCreatorAuthHealthy)
	default:
		return i18n.Translate(lang, msgCreatorAuthHealthy)
	}
}

func creatorSubscriberStatusText(status core.Status, lang string) string {
	if !status.HasSubscriberCount {
		return i18n.Translate(lang, msgCreatorSubscribersPending)
	}
	return fmt.Sprintf(i18n.Translate(lang, msgCreatorSubscribersReady), status.SubscriberCount)
}

func creatorStatusDetailsText(status core.Status, lang string) string {
	lastSyncLine := ""
	if !status.LastSyncAt.IsZero() {
		lastSyncLine = fmt.Sprintf(i18n.Translate(lang, "creator_last_sync_at"), formatStatusTime(status.LastSyncAt))
	}
	reconnectLine := ""
	if status.Auth == core.CreatorAuthReconnectRequired && !status.AuthStatusAt.IsZero() {
		reconnectLine = fmt.Sprintf(i18n.Translate(lang, "creator_reconnect_since"), formatStatusTime(status.AuthStatusAt))
	}
	return joinNonEmptyLines(lastSyncLine, reconnectLine)
}

func creatorBannedUserCountText(status core.Status, lang string) string {
	if !status.HasBannedUserCount {
		return ""
	}
	return fmt.Sprintf(i18n.Translate(lang, "creator_banned_users_cached"), status.BannedUserCount)
}

func creatorCacheSummaryText(status core.Status, lang string) string {
	subscriberLine := ""
	if status.HasSubscriberCount {
		subscriberLine = fmt.Sprintf(i18n.Translate(lang, "creator_subscribers_cached"), creatorSubscriberStatusText(status, lang))
	}
	return joinNonEmptyLines(subscriberLine, creatorBannedUserCountText(status, lang))
}

func creatorBlocklistStatusText(lang string, creator core.Creator, active bool) string {
	if !active {
		return ""
	}
	if creator.BlocklistSyncEnabled {
		return i18n.Translate(lang, msgCreatorBlocklistEnabled)
	}
	return i18n.Translate(lang, msgCreatorBlocklistDisabled)
}

func formatStatusTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format("2006-01-02 15:04 UTC")
}

// CreatorGroupLines returns HTML bullet lines describing creator-to-group bindings.
func CreatorGroupLines(lang, creatorName string, groups []core.ManagedGroup) string {
	if len(groups) == 0 {
		return i18n.Translate(lang, msgCreatorGroupsNone)
	}
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		groupName := strings.TrimSpace(group.GroupName)
		if groupName == "" {
			groupName = "-"
		}
		lines = append(lines, fmt.Sprintf(
			"• <b>%s</b> -> <b>%s</b>",
			html.EscapeString(creatorName),
			html.EscapeString(groupName),
		))
	}
	return strings.Join(lines, "\n")
}

func (c *Bot) replyCreatorManagedGroups(ctx context.Context, telegramUserID int64, editMsgID int, lang, notice string) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	if len(res.Groups) == 1 {
		return c.replyCreatorGroupSettingsForResult(ctx, telegramUserID, editMsgID, lang, res, res.Groups[0].ChatID, notice)
	}
	view := buildCreatorManagedGroupsView(lang, res.Creator, res.Groups, notice)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) replyCreatorGroupSettings(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, notice string) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	return c.replyCreatorGroupSettingsForResult(ctx, telegramUserID, editMsgID, lang, res, groupChatID, notice)
}

func (c *Bot) replyCreatorGroupSettingsForResult(
	ctx context.Context,
	telegramUserID int64,
	editMsgID int,
	lang string,
	res usecase.CreatorStatusResult,
	groupChatID int64,
	notice string,
) string {
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		view := buildCreatorManagedGroupsView(
			lang,
			res.Creator,
			res.Groups,
			i18n.Translate(lang, msgCreatorGroupUnavailable),
		)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}

	backCallback := creatorGroupBackCallback()
	if len(res.Groups) <= 1 {
		backCallback = creatorMenuCallback()
	}
	view := buildCreatorGroupSettingsView(lang, res.Creator, group, backCallback, notice)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) replyCreatorGroupPolicyPicker(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgCreatorGroupUnavailable))
	}
	view := buildCreatorGroupPolicyPickerView(lang, res.Creator, group)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) replyCreatorGroupPolicyConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, policy core.GroupPolicy) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgCreatorGroupUnavailable))
	}
	view := buildCreatorGroupPolicyConfirmView(lang, res.Creator, group, policy)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) replyCreatorGroupUnregisterConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	return c.replyCreatorGroupUnregisterConfirmForResult(ctx, telegramUserID, editMsgID, lang, res, groupChatID)
}

func (c *Bot) replyCreatorGroupUnregisterConfirmForResult(
	ctx context.Context,
	telegramUserID int64,
	editMsgID int,
	lang string,
	res usecase.CreatorStatusResult,
	groupChatID int64,
) string {
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		view := buildCreatorManagedGroupsView(
			lang,
			res.Creator,
			res.Groups,
			i18n.Translate(lang, msgCreatorGroupUnavailable),
		)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}

	backCallback := creatorGroupPickCallback(groupChatID)
	view := buildCreatorGroupUnregisterConfirmView(lang, res.Creator, group, backCallback)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) executeCreatorGroupUnregister(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	if c.groupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable")
		return ""
	}

	res, err := c.groupUnregistration.UnregisterGroup(ctx, telegramUserID, groupChatID)
	if err != nil {
		c.log().Warn("UnregisterGroup from creator menu failed", "chat_id", groupChatID, "owner_telegram_id", telegramUserID, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	switch res.Outcome {
	case usecase.UnregisterGroupOutcomeNotManaged:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgCreatorGroupUnavailable))
	case usecase.UnregisterGroupOutcomeNotOwner:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgGroupUnregisterNotOwner))
	case usecase.UnregisterGroupOutcomeUnregistered, usecase.UnregisterGroupOutcomeUnregisteredCleanupLag:
		if res.CleanupFailed {
			c.log().Warn("group unregistered from creator menu but eventsub cleanup deferred to reconciliation", "creator_id", res.Creator.ID, "chat_id", groupChatID)
		}
		groupName := singleManagedGroupLabel(res.Group)
		notice := fmt.Sprintf(i18n.Translate(lang, msgCreatorGroupUnregistered), html.EscapeString(groupName))
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, notice)
	default:
		c.log().Warn("unsupported group unregistration outcome", "chat_id", groupChatID, "outcome", res.Outcome)
		return ""
	}
}

func (c *Bot) executeCreatorGroupPolicyUpdate(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, policy core.GroupPolicy) string {
	if c.groupPolicyUpdate == nil {
		c.log().Warn("group policy update use case unavailable")
		return ""
	}

	res, err := c.groupPolicyUpdate.UpdateGroupPolicy(ctx, telegramUserID, groupChatID, policy)
	if err != nil {
		c.log().Warn("UpdateGroupPolicy from creator menu failed", "chat_id", groupChatID, "owner_telegram_id", telegramUserID, "policy", policy, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	switch res.Outcome {
	case usecase.UpdateGroupPolicyOutcomeNotManaged:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgCreatorGroupUnavailable))
	case usecase.UpdateGroupPolicyOutcomeNotOwner:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, msgCreatorGroupPolicyDenied))
	case usecase.UpdateGroupPolicyOutcomeUnchanged:
		groupName := singleManagedGroupLabel(res.Group)
		notice := fmt.Sprintf(i18n.Translate(lang, msgCreatorGroupPolicySame), html.EscapeString(groupName))
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, notice)
	case usecase.UpdateGroupPolicyOutcomeUpdated:
		groupName := singleManagedGroupLabel(res.Group)
		notice := fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyUpdated),
			html.EscapeString(groupName),
			formatGroupPolicyLine(lang, res.Group.Policy),
		)
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, notice)
	default:
		c.log().Warn("unsupported group policy update outcome", "chat_id", groupChatID, "outcome", res.Outcome)
		return ""
	}
}

func (c *Bot) toggleCreatorBlocklist(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	if c.creatorBlocklist == nil {
		c.log().Warn("creator blocklist service unavailable")
		return ""
	}
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	enable := !res.Creator.BlocklistSyncEnabled
	creator, _, err := c.creatorBlocklist.ToggleBlocklistSync(ctx, telegramUserID, enable)
	if err != nil {
		if errors.Is(err, core.ErrCreatorModerationScopeMissing) {
			c.log().Warn("creator blocklist toggle requires reconnect", "telegram_user_id", telegramUserID, "creator_id", res.Creator.ID)
			c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, true)
			return ""
		}
		c.log().Warn("toggle creator blocklist sync failed", "telegram_user_id", telegramUserID, "enable", enable, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	noticeKey := msgCreatorBlocklistOffNotice
	if creator.BlocklistSyncEnabled {
		noticeKey = msgCreatorBlocklistOnNotice
	}
	return c.replyCreatorStatusWithNotice(ctx, telegramUserID, editMsgID, lang, i18n.Translate(lang, noticeKey))
}

func (c *Bot) loadCreatorStatusResult(ctx context.Context, telegramUserID int64, lang string, editMsgID int) (usecase.CreatorStatusResult, bool) {
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.creatorStatus.LoadStatus(statusCtx, telegramUserID)
	if err != nil {
		if !res.HasCreator {
			c.log().Warn("LoadStatus failed", "telegram_user_id", telegramUserID, "error", err)
			view := buildCreatorStatusErrorView(lang)
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return usecase.CreatorStatusResult{}, false
		}
		c.log().Warn("LoadStatus degraded", "telegram_user_id", telegramUserID, "error", err)
	}
	if !res.HasCreator {
		c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, false)
		return usecase.CreatorStatusResult{}, false
	}
	if res.GroupsError != nil {
		c.log().Warn("LoadManagedGroups failed", "creator_id", res.Creator.ID, "error", res.GroupsError)
	}
	if res.StatusError != nil {
		c.log().Warn("LoadStatus degraded", "creator_id", res.Creator.ID, "error", res.StatusError)
	}
	return res, true
}

func (c *Bot) replyCreatorStatusWithNotice(ctx context.Context, telegramUserID int64, editMsgID int, lang, notice string) string {
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.creatorStatus.LoadStatus(statusCtx, telegramUserID)
	if err != nil || !res.HasCreator {
		c.replyCreatorStatus(ctx, telegramUserID, editMsgID, lang)
		return ""
	}
	reconnectURL := ""
	if res.Status.Auth == core.CreatorAuthReconnectRequired {
		reconnectURL, _ = c.creatorReconnectURL(ctx, telegramUserID, lang)
	}
	view := buildCreatorStatusView(lang, reconnectURL, res.Creator, res.Status, res.Groups)
	if strings.TrimSpace(notice) != "" {
		view.text = notice + "\n\n" + view.text
	}
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return view.text
}

func findCreatorManagedGroup(groups []core.ManagedGroup, groupChatID int64) (core.ManagedGroup, bool) {
	for _, group := range groups {
		if group.ChatID == groupChatID {
			return group, true
		}
	}
	return core.ManagedGroup{}, false
}

func creatorManagedGroupButtonLabel(group core.ManagedGroup, counts map[string]int) string {
	name := strings.TrimSpace(group.GroupName)
	if name == "" {
		name = strconv.FormatInt(group.ChatID, 10)
	}
	if counts[name] > 1 {
		return fmt.Sprintf("%s (%d)", name, group.ChatID)
	}
	return name
}

func singleManagedGroupLabel(group core.ManagedGroup) string {
	return creatorManagedGroupButtonLabel(group, map[string]int{group.GroupName: 1})
}

func buildCreatorPromptView(lang, authURL string, reconnect bool) sharedView {
	openKey := btnRegisterCreatorOpen
	textKey := msgCreatorRegisterInfo
	if reconnect {
		openKey = btnReconnectCreator
		textKey = msgCreatorReconnectInfo
	}

	return sharedView{
		text: i18n.Translate(lang, textKey),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.LinkButton(i18n.Translate(lang, openKey), authURL)),
				tu.InlineKeyboardRow(ui.CopyLinkButton(i18n.Translate(lang, btnCopyLink), authURL)),
			),
		},
	}
}

func buildCreatorStatusView(lang, reconnectURL string, creator core.Creator, status core.Status, groups []core.ManagedGroup) sharedView {
	profileDisplay := ui.TwitchProfileHTML(creator.TwitchLogin)
	groupLines := CreatorGroupLines(lang, creator.TwitchLogin, groups)
	authStatus := creatorAuthStatusText(status, lang)
	statusDetails := creatorStatusDetailsText(status, lang)
	isActive := len(groups) > 0
	blocklistStatus := creatorBlocklistStatusText(lang, creator, isActive)
	accountStatusDetails := joinNonEmptyLines(statusDetails, blocklistStatus)
	statusMenuRows := creatorStatusMenuRows(lang, groups)

	if len(groups) == 0 {
		return sharedView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgCreatorRegisteredNoGroup),
				profileDisplay,
				authStatus,
				accountStatusDetails,
				groupLines,
			),
			opts: client.MessageOptions{
				DisablePreview: true,
				Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(false, false, false), statusMenuRows...),
			},
		}
	}

	eventSubStatus := creatorEventSubStatusText(status, lang)
	cacheSummary := creatorCacheSummaryText(status, lang)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorRegistered),
			profileDisplay,
			eventSubStatus,
			authStatus,
			accountStatusDetails,
			cacheSummary,
			groupLines,
		),
		opts: client.MessageOptions{
			DisablePreview: true,
			Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(len(groups) > 1, true, creator.BlocklistSyncEnabled), statusMenuRows...),
		},
	}
}

func buildCreatorManagedGroupsView(lang string, creator core.Creator, groups []core.ManagedGroup, notice string) sharedView {
	text := i18n.Translate(lang, msgCreatorManageGroupsHTML)
	if len(groups) == 0 {
		text = i18n.Translate(lang, msgCreatorManageGroupsEmpty)
	} else {
		text = fmt.Sprintf(text, html.EscapeString(creator.TwitchLogin))
	}
	if strings.TrimSpace(notice) != "" {
		text = notice + "\n\n" + text
	}

	rows := make([][]telego.InlineKeyboardButton, 0, len(groups)+1)
	nameCounts := creatorManagedGroupNameCounts(groups)
	for _, group := range groups {
		rows = append(rows, tu.InlineKeyboardRow(
			ui.GroupButton(creatorManagedGroupButtonLabel(group, nameCounts), creatorGroupPickCallback(group.ChatID)),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorMenuCallback())))

	return sharedView{
		text: text,
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(rows...),
		},
	}
}

func buildCreatorGroupSettingsView(lang string, creator core.Creator, group core.ManagedGroup, backCallback, notice string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	text := fmt.Sprintf(
		i18n.Translate(lang, msgCreatorGroupSettingsHTML),
		html.EscapeString(groupLabel),
		html.EscapeString(creator.TwitchLogin),
		formatGroupPolicyLine(lang, group.Policy),
	)
	if strings.TrimSpace(notice) != "" {
		text = notice + "\n\n" + text
	}
	return sharedView{
		text: text,
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnChangeGroupPolicy), creatorGroupPolicyOpenCallback(group.ChatID), "5258318620722733379")),
				tu.InlineKeyboardRow(ui.UnregisterButton(i18n.Translate(lang, btnUnregisterGroup), creatorGroupConfirmCallback(group.ChatID))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)),
			),
		},
	}
}

func buildCreatorGroupPolicyPickerView(lang string, creator core.Creator, group core.ManagedGroup) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyHTML),
			html.EscapeString(groupLabel),
			html.EscapeString(creator.TwitchLogin),
			formatGroupPolicyLine(lang, group.Policy),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyObserve), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyObserve), "5253959125838090076")),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyObserveWarn), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyObserveWarn), "5253959125838090076")),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyKick), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyKick), "5258318620722733379").WithStyle("danger")),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyGrace), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyGraceWeek), "5258123337149717894").WithStyle("danger")),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorGroupPickCallback(group.ChatID))),
			),
		},
	}
}

func buildCreatorGroupPolicyConfirmView(lang string, creator core.Creator, group core.ManagedGroup, selectedPolicy core.GroupPolicy) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyConfirm),
			html.EscapeString(groupLabel),
			html.EscapeString(creator.TwitchLogin),
			formatGroupPolicyLine(lang, group.Policy),
			formatGroupPolicyLine(lang, selectedPolicy),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnConfirmGroupPolicy), creatorGroupPolicyExecuteCallback(group.ChatID, selectedPolicy))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorGroupPolicyOpenCallback(group.ChatID))),
			),
		},
	}
}

func buildCreatorGroupUnregisterConfirmView(lang string, creator core.Creator, group core.ManagedGroup, backCallback string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorUnregisterConfirm),
			html.EscapeString(groupLabel),
			html.EscapeString(creator.TwitchLogin),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.UnregisterButton(i18n.Translate(lang, btnUnregisterGroup), creatorGroupExecuteCallback(group.ChatID))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)),
			),
		},
	}
}

func creatorManagedGroupNameCounts(groups []core.ManagedGroup) map[string]int {
	counts := make(map[string]int, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.GroupName)
		if name == "" {
			continue
		}
		counts[name]++
	}
	return counts
}

func creatorStatusMenuRows(lang string, groups []core.ManagedGroup) [][]telego.InlineKeyboardButton {
	if len(groups) != 1 {
		return nil
	}
	label := fmt.Sprintf(i18n.Translate(lang, btnManageGroup), singleManagedGroupLabel(groups[0]))
	return [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(ui.GroupButton(label, creatorManageGroupsCallback())),
	}
}
