package flows

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/usecase"
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
	msgCreatorUnregisterConfirm  = "creator_unregister_confirm_html"
	msgCreatorGroupUnregistered  = "creator_group_unregistered_html"
	msgCreatorGroupUnavailable   = "creator_group_unavailable_html"

	btnRegisterCreatorOpen = "btn_register_creator_open"
	btnReconnectCreator    = "btn_reconnect_creator"
	btnManageGroup         = "btn_manage_group"
	btnUnregisterGroup     = "btn_unregister_group"
)

// --- Creator flow ---

func (c *Controller) handleCreatorStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	_, ok, err := c.app.CreatorStatus.LoadOwnedCreator(ctx, telegramUserID)
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

func (c *Controller) handleCreatorReconnectStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, true)
}

func (c *Controller) creatorReconnectURL(ctx context.Context, telegramUserID int64, lang string) (string, error) {
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

func (c *Controller) replyCreatorOAuthPrompt(ctx context.Context, telegramUserID int64, editMsgID int, lang string, reconnect bool) string {
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

func (c *Controller) replyCreatorStatus(ctx context.Context, telegramUserID int64, editMsgID int, lang string) {
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.app.CreatorStatus.LoadStatus(statusCtx, telegramUserID)
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

func (c *Controller) replyCreatorManagedGroups(ctx context.Context, telegramUserID int64, editMsgID int, lang, notice string) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	if len(res.Groups) == 1 {
		return c.replyCreatorGroupUnregisterConfirmForResult(ctx, telegramUserID, editMsgID, lang, res, res.Groups[0].ChatID)
	}
	view := buildCreatorManagedGroupsView(lang, res.Creator, res.Groups, notice)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Controller) replyCreatorGroupUnregisterConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	return c.replyCreatorGroupUnregisterConfirmForResult(ctx, telegramUserID, editMsgID, lang, res, groupChatID)
}

func (c *Controller) replyCreatorGroupUnregisterConfirmForResult(
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

	backCallback := creatorGroupBackCallback()
	if len(res.Groups) <= 1 {
		backCallback = creatorMenuCallback()
	}
	view := buildCreatorGroupUnregisterConfirmView(lang, res.Creator, group, backCallback)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Controller) executeCreatorGroupUnregister(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	if c.app.GroupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable")
		return ""
	}

	res, err := c.app.GroupUnregistration.UnregisterGroup(ctx, telegramUserID, groupChatID)
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
		groupName := creatorManagedGroupButtonLabel(res.Group, map[string]int{res.Group.GroupName: 1})
		notice := fmt.Sprintf(i18n.Translate(lang, msgCreatorGroupUnregistered), html.EscapeString(groupName))
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, notice)
	default:
		c.log().Warn("unsupported group unregistration outcome", "chat_id", groupChatID, "outcome", res.Outcome)
		return ""
	}
}

func (c *Controller) loadCreatorStatusResult(ctx context.Context, telegramUserID int64, lang string, editMsgID int) (usecase.CreatorStatusResult, bool) {
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.app.CreatorStatus.LoadStatus(statusCtx, telegramUserID)
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
