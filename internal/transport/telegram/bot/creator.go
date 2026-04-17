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
	msgErrCreatorLink              = "err_creator_link"
	msgCreatorRegisterInfo         = "creator_register_info"
	msgCreatorRegisteredNoGroup    = "creator_registered_no_group_html"
	msgCreatorRegistered           = "creator_registered_html"
	msgCreatorRegisteredLinks      = "creator_registered_links"
	msgCreatorAuthHealthy          = "creator_auth_healthy"
	msgCreatorAuthReconnect        = "creator_auth_reconnect_required"
	msgCreatorSubscribersPending   = "creator_subscribers_pending"
	msgCreatorSubscribersReady     = "creator_subscribers_ready"
	msgCreatorGroupsNone           = "creator_groups_none"
	msgCreatorReconnectInfo        = "creator_reconnect_info"
	msgCreatorReconnectMismatch    = "creator_reconnect_mismatch"
	msgCreatorManageGroupsHTML     = "creator_manage_groups_html"
	msgCreatorGroupSettingsHTML    = "creator_group_settings_html"
	msgCreatorGroupPolicyHTML      = "creator_group_policy_picker_html"
	msgCreatorGroupPolicyConfirm   = "creator_group_policy_confirm_html"
	msgCreatorGroupLanguageHTML    = "creator_group_language_picker_html"
	msgCreatorUnregisterConfirm    = "creator_unregister_confirm_html"
	msgCreatorGroupUnregistered    = "creator_group_unregistered_html"
	msgCreatorGroupPolicyUpdated   = "creator_group_policy_updated_html"
	msgCreatorGroupLanguageUpdated = "creator_group_language_updated_html"
	msgCreatorGracePickerHTML      = "creator_grace_picker_html"
	msgCreatorGraceEnabled         = "creator_grace_enabled"
	msgCreatorGraceDisabled        = "creator_grace_disabled"
	msgCreatorGraceUpdated         = "creator_grace_updated"
	msgCreatorBlocklistEnabled     = "creator_blocklist_enabled"
	msgCreatorBlocklistDisabled    = "creator_blocklist_disabled"
	msgCreatorBlocklistOnNotice    = "creator_blocklist_on_notice"
	msgCreatorBlocklistOffNotice   = "creator_blocklist_off_notice"

	btnRegisterCreatorOpen = "btn_register_creator_open"
	btnReconnectCreator    = "btn_reconnect_creator"
	btnManageGroup         = "btn_manage_group"
	btnGracePeriodOff      = "btn_grace_period_off"
	btnGracePeriod24h      = "btn_grace_period_24h"
	btnGracePeriod48h      = "btn_grace_period_48h"
	btnGracePeriod72h      = "btn_grace_period_72h"
	btnChangeGroupPolicy   = "btn_change_group_policy"
	btnChangeGroupLanguage = "btn_change_group_language"
	btnConfirmGroupPolicy  = "btn_confirm_group_policy"
	btnLanguageEnglish     = "btn_language_english"
	btnLanguageItalian     = "btn_language_italian"
	btnUnregisterGroup     = "btn_unregister_group"
	labelCurrentLanguage   = "label_current_language"
)

// onCreatorCommand handles /creator by showing the creator home/status flow.
func (c *Bot) onCreatorCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleCreatorStart(ctx, msg.From.ID, 0, lang)
	return nil
}

func (c *Bot) handleCreatorCallback(ctx context.Context, userID int64, editMsgID int, lang string, action callbackAction) callbackFeedback {
	switch action.verb {
	case callbackVerbRefresh, callbackVerbRegister:
		return callbackNoAckAfterRender(c.handleCreatorStart(ctx, userID, editMsgID, lang))
	case callbackVerbReconnect:
		return callbackNoAckAfterRender(c.handleCreatorReconnectStart(ctx, userID, editMsgID, lang))
	case callbackVerbOpen:
		if action.target == creatorCallbackTargetGroups {
			return callbackNoAckAfterRender(c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, ""))
		}
		if action.target == creatorCallbackTargetGrace {
			return callbackNoAckAfterRender(c.replyCreatorGracePicker(ctx, userID, editMsgID, lang))
		}
		if action.target == creatorCallbackTargetGroupConfirm {
			return callbackNoAckAfterRender(c.replyCreatorGroupUnregisterConfirm(ctx, userID, editMsgID, lang, action.chatID))
		}
		if action.target == creatorCallbackTargetPolicy {
			return callbackNoAckAfterRender(c.replyCreatorGroupPolicyPicker(ctx, userID, editMsgID, lang, action.chatID))
		}
		if action.target == creatorCallbackTargetLanguage {
			return callbackNoAckAfterRender(c.replyCreatorGroupLanguagePicker(ctx, userID, editMsgID, lang, action.chatID))
		}
	case callbackVerbPick:
		if action.target == creatorCallbackTargetGroup {
			return callbackNoAckAfterRender(c.replyCreatorGroupSettings(ctx, userID, editMsgID, lang, action.chatID, ""))
		}
		if action.target == creatorCallbackTargetPolicy {
			return callbackNoAckAfterRender(c.replyCreatorGroupPolicyConfirm(ctx, userID, editMsgID, lang, action.chatID, action.policy))
		}
	case callbackVerbBack:
		if action.target == creatorCallbackTargetGroups {
			return callbackNoAckAfterRender(c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, ""))
		}
		if action.target == creatorCallbackTargetPolicy {
			return callbackNoAckAfterRender(c.replyCreatorGroupSettings(ctx, userID, editMsgID, lang, action.chatID, ""))
		}
		if action.target == creatorCallbackTargetLanguage {
			return callbackNoAckAfterRender(c.replyCreatorGroupSettings(ctx, userID, editMsgID, lang, action.chatID, ""))
		}
	case callbackVerbMenu:
		return callbackNoAckAfterRender(c.handleCreatorStart(ctx, userID, editMsgID, lang))
	case callbackVerbExecute:
		if action.target == creatorCallbackTargetGroup {
			memberAction := action.resetAction
			if memberAction == "" {
				memberAction = core.CreatorResetKeepMembers
			}
			return callbackNoAckAfterRender(c.executeCreatorGroupUnregister(ctx, userID, editMsgID, lang, action.chatID, memberAction))
		}
		if action.target == creatorCallbackTargetPolicy {
			return callbackNoAckAfterRender(c.executeCreatorGroupPolicyUpdate(ctx, userID, editMsgID, lang, action.chatID, action.policy))
		}
		if action.target == creatorCallbackTargetLanguage {
			return callbackNoAckAfterRender(c.executeCreatorGroupLanguageUpdate(ctx, userID, editMsgID, lang, action.chatID, action.language))
		}
		if action.target == creatorCallbackTargetBlocklist {
			return callbackNoAckAfterRender(c.toggleCreatorBlocklist(ctx, userID, editMsgID, lang))
		}
		if action.target == creatorCallbackTargetGrace {
			return callbackNoAckAfterRender(c.updateCreatorGrace(ctx, userID, editMsgID, lang, action.grace))
		}
	case callbackVerbCancel:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return noCallbackFeedback()
	case callbackVerbExport:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return noCallbackFeedback()
	default:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return noCallbackFeedback()
	}
	c.log().Warn("unsupported creator callback action", "telegram_user_id", userID, "verb", action.verb, "target", action.target, "chat_id", action.chatID)
	return noCallbackFeedback()
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
	state, err := c.createOAuthState(ctx, payload, core.OAuthStateTTL)
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
	state, err := c.createOAuthState(ctx, payload, core.OAuthStateTTL)
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
	if err := c.store.SaveOAuthState(ctx, state, payload, core.OAuthStateTTL); err != nil {
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
		if err == nil {
			view := buildCreatorPromptView(lang, reconnectURL, true)
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return
		}
	}
	view := buildCreatorStatusView(lang, reconnectURL, c.botUsername(ctx), res.Creator, res.Status, res.Groups)

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
				c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
				return res.ResultLabel, "", fmt.Errorf("creator token exchange: %w", flowErr)
			case core.KindScopeMissing:
				c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
				return res.ResultLabel, "", fmt.Errorf("creator scope missing: %w", flowErr)
			case core.KindUserInfo:
				c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
				return res.ResultLabel, "", fmt.Errorf("creator user info: %w", flowErr)
			case core.KindStore:
				c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
				return res.ResultLabel, "", fmt.Errorf("creator store fail: %w", flowErr)
			case core.KindSave:
				c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
				return res.ResultLabel, "", fmt.Errorf("creator save fail: %w", flowErr)
			case core.KindCreatorMismatch:
				view := buildTextView(lang, msgCreatorReconnectMismatch)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("creator reconnect mismatch: %w", flowErr)
			}
		}
		c.sendCreatorReconnectPromptFallback(ctx, payload.TelegramUserID, lang)
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
	return c.sendCreatorReconnectPrompt(ctx, creator.OwnerTelegramID, lang)
}

func (c *Bot) sendCreatorReconnectPrompt(ctx context.Context, telegramUserID int64, lang string) error {
	reconnectURL, err := c.creatorReconnectURL(ctx, telegramUserID, lang)
	if err != nil {
		return fmt.Errorf("creator reconnect url: %w", err)
	}
	view := buildCreatorPromptView(lang, reconnectURL, true)
	if messageID := c.sendMsg(ctx, telegramUserID, view.text, &view.opts); messageID == 0 {
		return errReconnectNotificationSend
	}
	return nil
}

func (c *Bot) sendCreatorReconnectPromptFallback(ctx context.Context, telegramUserID int64, lang string) {
	if err := c.sendCreatorReconnectPrompt(ctx, telegramUserID, lang); err != nil {
		c.log().Warn("sendCreatorReconnectPrompt failed", "telegram_user_id", telegramUserID, "error", err)
		view := buildCreatorLinkErrorView(lang)
		c.sendMsg(ctx, telegramUserID, view.text, &view.opts)
	}
}

func creatorEventSubStatusText(status core.Status, lang string) string {
	if status.LastSyncAt.IsZero() {
		return ""
	}
	return fmt.Sprintf(i18n.Translate(lang, "creator_last_sync_at"), telegramDateTimeHTML(status.LastSyncAt, "Dt"))
}

func creatorSyncDisabledText(lang string) string {
	return i18n.Translate(lang, "creator_last_sync_disabled")
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

func telegramDateTimeHTML(ts time.Time, format string) string {
	unix := ts.UTC().Unix()
	fallback := html.EscapeString(formatStatusTime(ts))
	return fmt.Sprintf(`<tg-time unix="%d" format="%s">%s</tg-time>`, unix, html.EscapeString(format), fallback)
}

func creatorStatusDetailsText(status core.Status, lang string) string {
	if status.Auth == core.CreatorAuthReconnectRequired && !status.AuthStatusAt.IsZero() {
		return fmt.Sprintf(i18n.Translate(lang, "creator_reconnect_since"), telegramDateTimeHTML(status.AuthStatusAt, "Dt"))
	}
	return ""
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

func creatorFinalStepText(lang, botUsername string) string {
	handle, link := botEntryLinks(botUsername)
	if handle == "" && link == "" {
		return ""
	}
	return fmt.Sprintf(i18n.Translate(lang, msgCreatorRegisteredLinks), html.EscapeString(handle), html.EscapeString(link))
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

func creatorGraceStatusText(lang string, creator core.Creator, active bool) string {
	if !active {
		return ""
	}
	if creator.SubscriptionEndGrace.Enabled() {
		return fmt.Sprintf(i18n.Translate(lang, msgCreatorGraceEnabled), formatCreatorGraceValue(lang, creator.SubscriptionEndGrace))
	}
	return i18n.Translate(lang, msgCreatorGraceDisabled)
}

func formatCreatorGraceValue(lang string, grace core.SubscriptionEndGrace) string {
	switch grace {
	case core.SubscriptionEndGraceOff:
		return i18n.Translate(lang, btnGracePeriodOff)
	case core.SubscriptionEndGrace24h:
		return i18n.Translate(lang, btnGracePeriod24h)
	case core.SubscriptionEndGrace48h:
		return i18n.Translate(lang, btnGracePeriod48h)
	case core.SubscriptionEndGrace72h:
		return i18n.Translate(lang, btnGracePeriod72h)
	default:
		return i18n.Translate(lang, btnGracePeriodOff)
	}
}

func formatStatusTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format("2006-01-02 15:04 UTC")
}

// CreatorGroupLines returns HTML bullet lines describing managed group names.
func CreatorGroupLines(lang string, groups []core.ManagedGroup) string {
	if len(groups) == 0 {
		return i18n.Translate(lang, msgCreatorGroupsNone)
	}
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		groupName := strings.TrimSpace(group.GroupName)
		if groupName == "" {
			groupName = "-"
		}
		lines = append(lines, "• "+html.EscapeString(groupName))
	}
	return strings.Join(lines, "\n")
}

func (c *Bot) replyCreatorManagedGroups(ctx context.Context, telegramUserID int64, editMsgID int, lang, notice string) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	if len(res.Groups) == 0 {
		return c.replyCreatorStatusWithNotice(ctx, telegramUserID, editMsgID, lang, notice)
	}
	if len(res.Groups) == 1 {
		return c.replyCreatorGroupSettingsForResult(ctx, telegramUserID, editMsgID, lang, res, res.Groups[0].ChatID, notice)
	}
	view := buildCreatorManagedGroupsView(lang, res.Groups, notice)
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
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	}

	backCallback := creatorGroupBackCallback()
	if len(res.Groups) <= 1 {
		backCallback = creatorMenuCallback()
	}
	view := buildCreatorGroupSettingsView(lang, group, backCallback, notice)
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
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	}
	view := buildCreatorGroupPolicyPickerView(lang, group)
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
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	}
	view := buildCreatorGroupPolicyConfirmView(lang, group, policy)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) replyCreatorGroupLanguagePicker(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	}
	view := buildCreatorGroupLanguagePickerView(lang, group)
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
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	}

	backCallback := creatorGroupPickCallback(groupChatID)
	view := buildCreatorGroupUnregisterConfirmView(lang, group, backCallback)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) executeCreatorGroupUnregister(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, action core.CreatorResetGroupAction) string {
	if c.groupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable")
		return ""
	}

	res, err := c.groupUnregistration.UnregisterGroup(ctx, telegramUserID, groupChatID, action)
	if err != nil {
		c.log().Warn("UnregisterGroup from creator menu failed", "chat_id", groupChatID, "owner_telegram_id", telegramUserID, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	switch res.Outcome {
	case usecase.UnregisterGroupOutcomeNotManaged:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
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
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupPolicyOutcomeNotOwner:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupPolicyOutcomeUnchanged, usecase.UpdateGroupPolicyOutcomeUpdated:
		groupName := singleManagedGroupLabel(res.Group)
		notice := fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyUpdated),
			html.EscapeString(groupName),
			html.EscapeString(formatCreatorGroupPolicyValue(lang, res.Group.Policy)),
		)
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, notice)
	default:
		c.log().Warn("unsupported group policy update outcome", "chat_id", groupChatID, "outcome", res.Outcome)
		return ""
	}
}

func (c *Bot) executeCreatorGroupLanguageUpdate(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, language string) string {
	if c.groupLanguageUpdate == nil {
		c.log().Warn("group language update use case unavailable")
		return ""
	}

	res, err := c.groupLanguageUpdate.UpdateGroupLanguage(ctx, telegramUserID, groupChatID, language)
	if err != nil {
		c.log().Warn("UpdateGroupLanguage from creator menu failed", "chat_id", groupChatID, "owner_telegram_id", telegramUserID, "language", language, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	switch res.Outcome {
	case usecase.UpdateGroupLanguageOutcomeNotManaged:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupLanguageOutcomeNotOwner:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupLanguageOutcomeUnchanged, usecase.UpdateGroupLanguageOutcomeUpdated:
		groupName := singleManagedGroupLabel(res.Group)
		notice := fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupLanguageUpdated),
			html.EscapeString(groupName),
			html.EscapeString(formatGroupLanguageValue(lang, res.Group.Language)),
		)
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, notice)
	default:
		c.log().Warn("unsupported group language update outcome", "chat_id", groupChatID, "outcome", res.Outcome)
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

func (c *Bot) replyCreatorGracePicker(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	view := buildCreatorGracePickerView(lang, res.Creator)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return view.text
}

func (c *Bot) updateCreatorGrace(ctx context.Context, telegramUserID int64, editMsgID int, lang string, grace core.SubscriptionEndGrace) string {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return ""
	}
	if !validSubscriptionEndGrace(grace) {
		return ""
	}
	if err := c.store.UpdateCreatorSubscriptionEndGrace(ctx, res.Creator.ID, grace); err != nil {
		c.log().Warn("update creator subscription-end grace failed", "telegram_user_id", telegramUserID, "creator_id", res.Creator.ID, "grace", grace, "error", err)
		view := buildCreatorStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	notice := fmt.Sprintf(i18n.Translate(lang, msgCreatorGraceUpdated), formatCreatorGraceValue(lang, grace))
	return c.replyCreatorStatusWithNotice(ctx, telegramUserID, editMsgID, lang, notice)
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
		if reconnectURL != "" {
			view := buildCreatorPromptView(lang, reconnectURL, true)
			if strings.TrimSpace(notice) != "" {
				view.text = notice + "\n\n" + view.text
			}
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return view.text
		}
	}
	view := buildCreatorStatusView(lang, reconnectURL, c.botUsername(ctx), res.Creator, res.Status, res.Groups)
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

func buildCreatorStatusView(lang, reconnectURL, botUsername string, creator core.Creator, status core.Status, groups []core.ManagedGroup) sharedView {
	profileDisplay := ui.TwitchProfileHTML(creator.TwitchLogin, creator.TwitchDisplayName)
	groupLines := CreatorGroupLines(lang, groups)
	eventSubStatus := creatorEventSubStatusText(status, lang)
	authStatus := creatorAuthStatusText(status, lang)
	statusDetails := creatorStatusDetailsText(status, lang)
	isActive := len(groups) > 0
	blocklistStatus := creatorBlocklistStatusText(lang, creator, isActive)
	graceStatus := creatorGraceStatusText(lang, creator, isActive)
	summarySections := []textSection{
		creatorDashboardSection(lang, "creator_dashboard_setup_status", authStatus, eventSubStatus, statusDetails),
		creatorDashboardSection(lang, "creator_dashboard_settings", graceStatus, blocklistStatus),
		creatorDashboardSection(lang, "creator_dashboard_current_data", creatorCacheSummaryText(status, lang)),
	}
	summaryBlock := joinNonEmptySections(summarySections...)
	statusMenuRows := creatorStatusMenuRows(lang, groups)

	if len(groups) == 0 {
		noGroupSummaryBlock := joinNonEmptySections(
			creatorDashboardSection(lang, "creator_dashboard_setup_status", authStatus, creatorSyncDisabledText(lang), statusDetails),
		)
		return sharedView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgCreatorRegisteredNoGroup),
				profileDisplay,
				noGroupSummaryBlock,
			),
			opts: client.MessageOptions{
				DisablePreview: true,
				Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(false, false, false, false), statusMenuRows...),
			},
		}
	}

	if finalStepBlock := creatorFinalStepText(lang, botUsername); finalStepBlock != "" {
		summaryBlock = joinNonEmptySections(textSection{text: summaryBlock}, textSection{text: finalStepBlock})
	}

	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorRegistered),
			profileDisplay,
			summaryBlock,
			groupLines,
		),
		opts: client.MessageOptions{
			DisablePreview: true,
			Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(len(groups) > 1, true, creator.SubscriptionEndGrace.Enabled(), creator.BlocklistSyncEnabled), statusMenuRows...),
		},
	}
}

func creatorDashboardSection(lang, titleKey string, lines ...string) textSection {
	body := joinNonEmptyLines(lines...)
	if strings.TrimSpace(body) == "" {
		return textSection{}
	}
	return textSection{text: joinNonEmptyLines(i18n.Translate(lang, titleKey), body)}
}

func buildCreatorGracePickerView(lang string, creator core.Creator) sharedView {
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGracePickerHTML),
			html.EscapeString(formatCreatorGraceValue(lang, creator.SubscriptionEndGrace)),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnGracePeriodOff), creatorGraceExecuteCallback(core.SubscriptionEndGraceOff))),
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnGracePeriod24h), creatorGraceExecuteCallback(core.SubscriptionEndGrace24h))),
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnGracePeriod48h), creatorGraceExecuteCallback(core.SubscriptionEndGrace48h))),
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnGracePeriod72h), creatorGraceExecuteCallback(core.SubscriptionEndGrace72h))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorMenuCallback())),
			),
		},
	}
}

func buildCreatorManagedGroupsView(lang string, groups []core.ManagedGroup, notice string) sharedView {
	text := i18n.Translate(lang, msgCreatorManageGroupsHTML)
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

func buildCreatorGroupSettingsView(lang string, group core.ManagedGroup, backCallback, notice string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	text := fmt.Sprintf(
		i18n.Translate(lang, msgCreatorGroupSettingsHTML),
		html.EscapeString(groupLabel),
		html.EscapeString(formatCreatorGroupPolicyValue(lang, group.Policy)),
		html.EscapeString(formatGroupLanguageValue(lang, group.Language)),
	)
	if strings.TrimSpace(notice) != "" {
		text = notice + "\n\n" + text
	}
	return sharedView{
		text: text,
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnChangeGroupPolicy), creatorGroupPolicyOpenCallback(group.ChatID), "5258318620722733379")),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnChangeGroupLanguage), creatorGroupLanguageOpenCallback(group.ChatID), "5879585266426973039")),
				tu.InlineKeyboardRow(ui.UnregisterButton(i18n.Translate(lang, btnUnregisterGroup), creatorGroupConfirmCallback(group.ChatID))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)),
			),
		},
	}
}

func buildCreatorGroupLanguagePickerView(lang string, group core.ManagedGroup) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupLanguageHTML),
			html.EscapeString(groupLabel),
			html.EscapeString(formatGroupLanguageValue(lang, group.Language)),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnLanguageEnglish), creatorGroupLanguageExecuteCallback(group.ChatID, "en"))),
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnLanguageItalian), creatorGroupLanguageExecuteCallback(group.ChatID, "it"))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorGroupPickCallback(group.ChatID))),
			),
		},
	}
}

func buildCreatorGroupPolicyPickerView(lang string, group core.ManagedGroup) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyHTML),
			html.EscapeString(groupLabel),
			html.EscapeString(formatCreatorGroupPolicyValue(lang, group.Policy)),
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

func buildCreatorGroupPolicyConfirmView(lang string, group core.ManagedGroup, selectedPolicy core.GroupPolicy) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorGroupPolicyConfirm),
			html.EscapeString(groupLabel),
			html.EscapeString(formatCreatorGroupPolicyValue(lang, group.Policy)),
			html.EscapeString(formatCreatorGroupPolicyValue(lang, selectedPolicy)),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnConfirmGroupPolicy), creatorGroupPolicyExecuteCallback(group.ChatID, selectedPolicy))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorGroupPolicyOpenCallback(group.ChatID))),
			),
		},
	}
}

func buildCreatorGroupUnregisterConfirmView(lang string, group core.ManagedGroup, backCallback string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorUnregisterConfirm),
			html.EscapeString(groupLabel),
		),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnResetKeepMembers), creatorGroupExecuteWithActionCallback(group.ChatID, core.CreatorResetKeepMembers))),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnResetKickTrackedMembers), creatorGroupExecuteWithActionCallback(group.ChatID, core.CreatorResetKickTrackedMembers), "5258318620722733379").WithStyle("danger")),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)),
			),
		},
	}
}

func formatCreatorGroupPolicyValue(lang string, policy core.GroupPolicy) string {
	switch policy {
	case core.GroupPolicyObserve:
		return i18n.Translate(lang, btnGroupPolicyObserve)
	case core.GroupPolicyObserveWarn:
		return i18n.Translate(lang, btnGroupPolicyObserveWarn)
	case core.GroupPolicyKick:
		return i18n.Translate(lang, btnGroupPolicyKick)
	case core.GroupPolicyGraceWeek:
		return i18n.Translate(lang, btnGroupPolicyGrace)
	default:
		return i18n.Translate(lang, btnGroupPolicyObserve)
	}
}

func formatGroupLanguageValue(lang, groupLang string) string {
	switch groupLang {
	case "it":
		return i18n.Translate(lang, btnLanguageItalian)
	default:
		return i18n.Translate(lang, btnLanguageEnglish)
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
