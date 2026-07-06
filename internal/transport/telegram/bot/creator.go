package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
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
	msgErrCreatorLink                      = "err_creator_link"
	msgCreatorRegisterInfo                 = "creator_register_info"
	msgCreatorRegisteredLinks              = "creator_registered_links"
	msgCreatorAuthHealthy                  = "creator_auth_healthy"
	msgCreatorAuthReconnect                = "creator_auth_reconnect_required"
	msgCreatorSubscribersPending           = "creator_subscribers_pending"
	msgCreatorSubscribersReady             = "creator_subscribers_ready"
	msgCreatorGroupsNone                   = "creator_groups_none"
	msgCreatorReconnectInfo                = "creator_reconnect_info"
	msgCreatorReconnectMismatch            = "creator_reconnect_mismatch"
	msgCreatorGroupUnregistered            = "creator_group_unregistered_html"
	msgCreatorGroupPolicyUpdated           = "creator_group_policy_updated_html"
	msgCreatorGroupLanguageUpdated         = "creator_group_language_updated_html"
	msgCreatorGroupMemberTagsState         = "creator_group_member_tags_state"
	msgCreatorGroupMemberTagsOffState      = "creator_group_member_tags_state_off"
	msgCreatorGroupMemberTagsEnableNotice  = "creator_group_member_tags_enable_notice_html"
	msgCreatorGroupMemberTagsDisableNotice = "creator_group_member_tags_disable_notice_html"
	msgCreatorGroupMemberTagsNeedTags      = "creator_group_member_tags_need_tags_html"
	msgCreatorGraceEnabled                 = "creator_grace_enabled"
	msgCreatorGraceDisabled                = "creator_grace_disabled"
	msgCreatorGraceUpdated                 = "creator_grace_updated"
	msgCreatorBlocklistEnabled             = "creator_blocklist_enabled"
	msgCreatorBlocklistDisabled            = "creator_blocklist_disabled"
	msgCreatorBlocklistOnNotice            = "creator_blocklist_on_notice"
	msgCreatorBlocklistOffNotice           = "creator_blocklist_off_notice"

	msgCreatorRegisteredTitle         = "creator_registered_title"
	msgCreatorRegisteredBody          = "creator_registered_body"
	msgCreatorRegisteredNoGroupTitle  = "creator_registered_no_group_title"
	msgCreatorRegisteredNoGroupBody   = "creator_registered_no_group_body"
	msgCreatorManageGroupsTitle       = "creator_manage_groups_title"
	msgCreatorManageGroupsBody        = "creator_manage_groups_body"
	msgCreatorGroupSettingsTitle      = "creator_group_settings_title"
	msgCreatorGroupPolicyTitle        = "creator_group_policy_title"
	msgCreatorGroupPolicyBody         = "creator_group_policy_body"
	msgCreatorGroupPolicyConfirmTitle = "creator_group_policy_confirm_title"
	msgCreatorGroupLanguageTitle      = "creator_group_language_title"
	msgCreatorGroupLanguageBody       = "creator_group_language_body"
	msgCreatorGroupMemberTagsTitle    = "creator_group_member_tags_title"
	msgCreatorGroupMemberTagsBody     = "creator_group_member_tags_body"
	msgCreatorUnregisterTitle         = "creator_unregister_title"
	msgCreatorUnregisterBody          = "creator_unregister_body"
	msgCreatorGraceTitle              = "creator_grace_title"
	msgCreatorGraceBody               = "creator_grace_body"
	msgCreatorSectionManagedGroups    = "creator_section_managed_groups"
	labelGroup                        = "label_group"
	labelCurrentPolicy                = "label_current_policy"
	labelMemberTags                   = "label_member_tags"
	labelCurrentSetting               = "label_current_setting"
	labelNewPolicy                    = "label_new_policy"
	labelAccount                      = "label_account"

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

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

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
		if action.target == creatorCallbackTargetMemberTags {
			return callbackNoAckAfterRender(c.replyCreatorGroupMemberTagsConfirm(ctx, userID, editMsgID, lang, action.chatID))
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
		if action.target == creatorCallbackTargetMemberTags {
			return callbackNoAckAfterRender(c.executeCreatorGroupMemberTagsUpdate(ctx, userID, editMsgID, lang, action.chatID, action.toggle))
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

//nolint:unparam // callback render helpers use a shared string-returning shape
func (c *Bot) replyCreatorGroupMemberTagsConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) string {
	group, ok := c.loadCreatorGroupForMemberTags(ctx, telegramUserID, editMsgID, lang, groupChatID)
	if !ok {
		return ""
	}
	view := buildCreatorGroupMemberTagsConfirmView(lang, group, "")
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

func (c *Bot) executeCreatorGroupMemberTagsUpdate(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64, toggle string) string {
	if c.groupMemberTagSync == nil {
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, "")
	}
	if toggle == "on" {
		if _, ok := c.loadCreatorGroupForMemberTags(ctx, telegramUserID, editMsgID, lang, groupChatID); !ok {
			return ""
		}
	}
	enable := toggle == "on"
	res, err := c.groupMemberTagSync.UpdateGroupMemberTagSync(ctx, telegramUserID, groupChatID, enable)
	if err != nil {
		c.log().Warn("UpdateGroupMemberTagSync from creator menu failed", "chat_id", groupChatID, "owner_telegram_id", telegramUserID, "toggle", toggle, "error", err)
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, "")
	}
	switch res.Outcome {
	case usecase.UpdateGroupMemberTagOutcomeNotManaged:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupMemberTagOutcomeNotOwner:
		return c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
	case usecase.UpdateGroupMemberTagOutcomeUnchanged, usecase.UpdateGroupMemberTagOutcomeUpdated:
		noticeKey := msgCreatorGroupMemberTagsDisableNotice
		if res.Group.MemberTagSyncEnabled {
			noticeKey = msgCreatorGroupMemberTagsEnableNotice
		}
		if c.memberTagSync != nil {
			c.runBackground(context.WithoutCancel(ctx), func(bgctx context.Context) {
				syncCtx, cancel := context.WithTimeout(bgctx, 30*time.Second)
				defer cancel()
				if res.Group.MemberTagSyncEnabled {
					if _, syncErr := c.memberTagSync.SyncGroup(syncCtx, res.Group.ChatID, true); syncErr != nil {
						c.log().Warn("member tag enable sync failed", "chat_id", res.Group.ChatID, "error", syncErr)
					}
					return
				}
				if _, syncErr := c.memberTagSync.CleanupGroup(syncCtx, res.Group.ChatID); syncErr != nil {
					c.log().Warn("member tag disable cleanup failed", "chat_id", res.Group.ChatID, "error", syncErr)
				}
			})
		}
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, i18n.Translate(lang, noticeKey))
	default:
		return c.replyCreatorGroupSettings(ctx, telegramUserID, editMsgID, lang, groupChatID, "")
	}
}

func (c *Bot) loadCreatorGroupForMemberTags(ctx context.Context, telegramUserID int64, editMsgID int, lang string, groupChatID int64) (core.ManagedGroup, bool) {
	res, ok := c.loadCreatorStatusResult(ctx, telegramUserID, lang, editMsgID)
	if !ok {
		return core.ManagedGroup{}, false
	}
	group, found := findCreatorManagedGroup(res.Groups, groupChatID)
	if !found {
		c.replyCreatorManagedGroups(ctx, telegramUserID, editMsgID, lang, "")
		return core.ManagedGroup{}, false
	}
	caps, err := c.loadBotGroupCapabilities(ctx, group.ChatID)
	if err != nil {
		c.log().Warn("load bot group capabilities for member tags failed", "chat_id", group.ChatID, "error", err)
	}
	if !caps.canManageTags {
		view := buildCreatorGroupMemberTagsConfirmView(lang, group, i18n.Translate(lang, msgCreatorGroupMemberTagsNeedTags))
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return core.ManagedGroup{}, false
	}
	return group, true
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
		if errors.Is(err, core.ErrCreatorBlocklistScopeMissing) {
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

func splitScreenText(text string) (emoji, title, body string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", ""
	}
	headerLine, rest, hasBody := strings.Cut(text, "\n\n")
	headerLine = strings.TrimSpace(headerLine)
	if hasBody {
		body = strings.TrimSpace(rest)
	}
	title = headerLine
	if before, after, found := strings.Cut(headerLine, "<b>"); found {
		emoji = strings.TrimSpace(before)
		if boldTitle, headerBody, closed := strings.Cut(after, "</b>"); closed {
			title = boldTitle
			if headerBody = strings.TrimSpace(headerBody); headerBody != "" {
				body = joinNonEmptySections(textSection{text: headerBody}, textSection{text: body})
			}
		}
	}
	title = htmlTagPattern.ReplaceAllString(title, "")
	return strings.TrimSpace(emoji), strings.TrimSpace(title), body
}

func plainTranslatedText(lang, key string) string {
	return strings.TrimSpace(i18n.Translate(lang, key))
}

func creatorDetailItem(text string) (ui.DetailItem, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ui.DetailItem{}, false
	}
	raw = strings.TrimPrefix(raw, "• ")
	rawLabel, rawValue, found := strings.Cut(raw, ":")
	if !found {
		return ui.DetailItem{ValueHTML: ui.TrustedHTML(raw)}, true
	}
	label := strings.TrimSpace(htmlTagPattern.ReplaceAllString(rawLabel, ""))
	value := strings.TrimSpace(rawValue)
	if label == "" && value == "" {
		return ui.DetailItem{}, false
	}
	if label != "" {
		label += ":"
	}
	return ui.DetailItem{Label: label, ValueHTML: ui.TrustedHTML(value)}, true
}

func buildCreatorStatusView(lang, reconnectURL, botUsername string, creator core.Creator, status core.Status, groups []core.ManagedGroup) sharedView {
	profileDisplay := ui.TwitchProfileHTML(creator.TwitchLogin, creator.TwitchDisplayName)
	requirements := creatorStatusRequirements(lang, status, groups)
	isActive := len(groups) > 0
	managedGroupsBody := CreatorGroupLines(lang, groups)
	detailItems := []ui.DetailItem{{
		Label:     i18n.Translate(lang, labelAccount) + ":",
		ValueHTML: ui.TrustedHTML(profileDisplay),
	}}
	for _, line := range []string{
		creatorGraceStatusText(lang, creator, isActive),
		creatorBlocklistStatusText(lang, creator, isActive),
	} {
		if item, ok := creatorDetailItem(line); ok {
			detailItems = append(detailItems, item)
		}
	}
	for line := range strings.SplitSeq(creatorCacheSummaryText(status, lang), "\n") {
		if item, ok := creatorDetailItem(line); ok {
			detailItems = append(detailItems, item)
		}
	}
	var extraRows []ui.ActionItem
	if len(groups) == 1 {
		label := fmt.Sprintf(i18n.Translate(lang, btnManageGroup), singleManagedGroupLabel(groups[0]))
		extraRows = append(extraRows, creatorGroupedActionItem(label, creatorManageGroupsCallback()))
	}
	menuItems := make([]ui.ActionItem, 0, 5)
	if strings.TrimSpace(reconnectURL) != "" {
		menuItems = append(menuItems, ui.ActionItem{
			Kind:        ui.ActionKindURL,
			Label:       i18n.Translate(lang, "btn_reconnect_creator"),
			Target:      reconnectURL,
			IconEmojiID: "5257991477358763590",
			Style:       "primary",
			Available:   true,
		})
	} else {
		menuItems = append(menuItems, creatorIconActionItem(i18n.Translate(lang, "btn_refresh"), creatorRefreshCallback(), "5258420634785947640"))
	}
	if len(groups) > 1 {
		menuItems = append(menuItems, creatorIconActionItem(i18n.Translate(lang, "btn_manage_groups"), creatorManageGroupsCallback(), "5258096772776991776"))
	}
	if len(groups) > 0 {
		grace := creatorIconActionItem(i18n.Translate(lang, "btn_grace_period"), creatorGraceOpenCallback(), "5258318620722733379")
		if creator.SubscriptionEndGrace.Enabled() {
			grace.Style = ui.StyleSuccess
		}
		menuItems = append(menuItems, grace)
		blocklist := creatorIconActionItem(i18n.Translate(lang, "btn_blocklist_sync"), creatorBlocklistToggleCallback(), "5275969776668134187")
		if creator.BlocklistSyncEnabled {
			blocklist.Style = ui.StyleSuccess
		}
		menuItems = append(menuItems, blocklist)
	}
	menuItems = append(menuItems, creatorIconActionItem(i18n.Translate(lang, "btn_reset"), resetOpenCallback(resetOriginCreator), "5258096772776991776"))

	bodySections := []ui.BodySection{{
		Title:    plainTranslatedText(lang, msgCreatorSectionManagedGroups),
		TextHTML: ui.TrustedHTML(managedGroupsBody),
	}}
	if len(groups) == 0 {
		return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
			Header: ui.HeaderSection{
				Emoji:    "⚠️",
				Title:    i18n.Translate(lang, msgCreatorRegisteredNoGroupTitle),
				BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorRegisteredNoGroupBody)),
			},
			Requirements: ui.RequirementsSection{Title: plainTranslatedText(lang, "creator_dashboard_setup_status"), Items: requirements},
			Details:      ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: detailItems},
			Body:         bodySections,
			Actions: []ui.ActionGroup{
				{Items: extraRows},
				{Items: menuItems},
			},
			DisablePreview: true,
		}))
	}

	if finalStepBlock := creatorFinalStepText(lang, botUsername); finalStepBlock != "" {
		managedGroupsBody = joinNonEmptyLines(managedGroupsBody, "", finalStepBlock)
	}
	bodySections[0].TextHTML = ui.TrustedHTML(managedGroupsBody)

	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "✅",
			Title:    i18n.Translate(lang, msgCreatorRegisteredTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorRegisteredBody)),
		},
		Requirements: ui.RequirementsSection{Title: plainTranslatedText(lang, "creator_dashboard_setup_status"), Items: requirements},
		Details:      ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: detailItems},
		Body:         bodySections,
		Actions: []ui.ActionGroup{
			{Items: extraRows},
			{Items: menuItems},
		},
		DisablePreview: true,
	}))
}

func creatorStatusRequirements(lang string, status core.Status, groups []core.ManagedGroup) []ui.RequirementItem {
	items := make([]ui.RequirementItem, 0, 3)
	authLine := creatorAuthStatusText(status, lang)
	authItem, ok := creatorDetailItem(authLine)
	switch {
	case status.Auth == core.CreatorAuthReconnectRequired && ok:
		items = append(items, ui.RequirementItem{
			Label:      strings.TrimSuffix(authItem.Label, ":"),
			State:      ui.RequirementStateBlocked,
			DetailHTML: ui.TrustedHTML(joinNonEmptyLines(string(authItem.ValueHTML), creatorStatusDetailsText(status, lang))),
		})
	case ok:
		items = append(items, ui.RequirementItem{
			Label:      strings.TrimSuffix(authItem.Label, ":"),
			State:      ui.RequirementStateReady,
			DetailHTML: authItem.ValueHTML,
		})
	}
	if len(groups) == 0 {
		items = append(items, ui.RequirementItem{
			Label:      plainTranslatedText(lang, msgCreatorSectionManagedGroups),
			State:      ui.RequirementStateAttention,
			DetailHTML: ui.TrustedHTML(joinNonEmptyLines(i18n.Translate(lang, msgCreatorGroupsNone), creatorSyncDisabledText(lang))),
		})
	}
	return items
}

func buildCreatorGracePickerView(lang string, creator core.Creator) sharedView {
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "⏳",
			Title:    i18n.Translate(lang, msgCreatorGraceTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorGraceBody)),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{{
			Label:     i18n.Translate(lang, labelCurrentSetting) + ":",
			ValueHTML: ui.EscapeHTML(formatCreatorGraceValue(lang, creator.SubscriptionEndGrace)),
		}}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnGracePeriodOff), creatorGraceExecuteCallback(core.SubscriptionEndGraceOff))}},
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnGracePeriod24h), creatorGraceExecuteCallback(core.SubscriptionEndGrace24h))}},
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnGracePeriod48h), creatorGraceExecuteCallback(core.SubscriptionEndGrace48h))}},
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnGracePeriod72h), creatorGraceExecuteCallback(core.SubscriptionEndGrace72h))}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorMenuCallback()}},
	}))
}

func buildCreatorManagedGroupsView(lang string, groups []core.ManagedGroup, notice string) sharedView {
	rows := make([]ui.ActionItem, 0, len(groups))
	nameCounts := creatorManagedGroupNameCounts(groups)
	for _, group := range groups {
		rows = append(rows, creatorGroupedActionItem(creatorManagedGroupButtonLabel(group, nameCounts), creatorGroupPickCallback(group.ChatID)))
	}
	actionGroups := make([]ui.ActionGroup, 0, len(rows))
	for _, row := range rows {
		actionGroups = append(actionGroups, ui.ActionGroup{Items: []ui.ActionItem{row}})
	}
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			NoticeHTML: ui.TrustedHTML(notice),
			Emoji:      "🗂️",
			Title:      i18n.Translate(lang, msgCreatorManageGroupsTitle),
			BodyHTML:   ui.TrustedHTML(i18n.Translate(lang, msgCreatorManageGroupsBody)),
		},
		Actions:    actionGroups,
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorMenuCallback()}},
	}))
}

func buildCreatorGroupSettingsView(lang string, group core.ManagedGroup, backCallback, notice string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			NoticeHTML: ui.TrustedHTML(notice),
			Emoji:      "⚙️",
			Title:      i18n.Translate(lang, msgCreatorGroupSettingsTitle),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
			{Label: i18n.Translate(lang, labelCurrentPolicy) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupPolicyValue(lang, group.Policy))},
			{Label: i18n.Translate(lang, labelCurrentLanguage) + ":", ValueHTML: ui.EscapeHTML(formatGroupLanguageValue(lang, group.Language))},
			{Label: i18n.Translate(lang, labelMemberTags) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupMemberTagsState(lang, group.MemberTagSyncEnabled))},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorIconActionItem(i18n.Translate(lang, btnChangeGroupPolicy), creatorGroupPolicyOpenCallback(group.ChatID), "5258318620722733379")}},
			{Items: []ui.ActionItem{creatorIconActionItem(i18n.Translate(lang, btnChangeGroupLanguage), creatorGroupLanguageOpenCallback(group.ChatID), "5879585266426973039")}},
			{Items: []ui.ActionItem{creatorGroupMemberTagActionItem(lang, group)}},
			{Items: []ui.ActionItem{creatorDangerActionItem(i18n.Translate(lang, btnUnregisterGroup), creatorGroupConfirmCallback(group.ChatID), "5258084656674250503")}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: backCallback}},
	}))
}

func buildCreatorGroupMemberTagsConfirmView(lang string, group core.ManagedGroup, notice string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	toggle := "on"
	buttonKey := btnMemberTagSyncEnable
	if group.MemberTagSyncEnabled {
		toggle = "off"
		buttonKey = btnMemberTagSyncDisable
	}
	requirements := []ui.RequirementItem(nil)
	if strings.TrimSpace(notice) != "" {
		_, requirementTitle, requirementBody := splitScreenText(notice)
		requirements = append(requirements, ui.RequirementItem{
			Label:      requirementTitle,
			State:      ui.RequirementStateBlocked,
			DetailHTML: ui.TrustedHTML(requirementBody),
		})
	}
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "🏷️",
			Title:    i18n.Translate(lang, msgCreatorGroupMemberTagsTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorGroupMemberTagsBody)),
		},
		Requirements: ui.RequirementsSection{Title: plainTranslatedText(lang, "creator_dashboard_setup_status"), Items: requirements},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
			{Label: i18n.Translate(lang, labelCurrentSetting) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupMemberTagsState(lang, group.MemberTagSyncEnabled))},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{func() ui.ActionItem {
				item := creatorIconActionItem(i18n.Translate(lang, buttonKey), creatorGroupMemberTagsExecuteCallback(group.ChatID, toggle), "5296348778012361146")
				item.Available = strings.TrimSpace(notice) == ""
				return item
			}()}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorGroupPickCallback(group.ChatID)}},
	}))
}

func buildCreatorGroupLanguagePickerView(lang string, group core.ManagedGroup) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "🌐",
			Title:    i18n.Translate(lang, msgCreatorGroupLanguageTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorGroupLanguageBody)),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
			{Label: i18n.Translate(lang, labelCurrentLanguage) + ":", ValueHTML: ui.EscapeHTML(formatGroupLanguageValue(lang, group.Language))},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnLanguageEnglish), creatorGroupLanguageExecuteCallback(group.ChatID, "en"))}},
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnLanguageItalian), creatorGroupLanguageExecuteCallback(group.ChatID, "it"))}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorGroupPickCallback(group.ChatID)}},
	}))
}

func buildCreatorGroupPolicyPickerView(lang string, group core.ManagedGroup) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "🛡️",
			Title:    i18n.Translate(lang, msgCreatorGroupPolicyTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorGroupPolicyBody)),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
			{Label: i18n.Translate(lang, labelCurrentPolicy) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupPolicyValue(lang, group.Policy))},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorIconActionItem(i18n.Translate(lang, btnGroupPolicyObserve), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyObserve), "5253959125838090076")}},
			{Items: []ui.ActionItem{creatorIconActionItem(i18n.Translate(lang, btnGroupPolicyObserveWarn), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyObserveWarn), "5253959125838090076")}},
			{Items: []ui.ActionItem{creatorDangerActionItem(i18n.Translate(lang, btnGroupPolicyKick), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyKick), "5258318620722733379")}},
			{Items: []ui.ActionItem{creatorDangerActionItem(i18n.Translate(lang, btnGroupPolicyGrace), creatorGroupPolicyPickCallback(group.ChatID, core.GroupPolicyGraceWeek), "5258123337149717894")}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorGroupPickCallback(group.ChatID)}},
	}))
}

func buildCreatorGroupPolicyConfirmView(lang string, group core.ManagedGroup, selectedPolicy core.GroupPolicy) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji: "⚠️",
			Title: i18n.Translate(lang, msgCreatorGroupPolicyConfirmTitle),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
			{Label: i18n.Translate(lang, labelCurrentPolicy) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupPolicyValue(lang, group.Policy))},
			{Label: i18n.Translate(lang, labelNewPolicy) + ":", ValueHTML: ui.EscapeHTML(formatCreatorGroupPolicyValue(lang, selectedPolicy))},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnConfirmGroupPolicy), creatorGroupPolicyExecuteCallback(group.ChatID, selectedPolicy))}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: creatorGroupPolicyOpenCallback(group.ChatID)}},
	}))
}

func buildCreatorGroupUnregisterConfirmView(lang string, group core.ManagedGroup, backCallback string) sharedView {
	groupLabel := singleManagedGroupLabel(group)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    "⚠️",
			Title:    i18n.Translate(lang, msgCreatorUnregisterTitle),
			BodyHTML: ui.TrustedHTML(i18n.Translate(lang, msgCreatorUnregisterBody)),
		},
		Details: ui.DetailsSection{Title: plainTranslatedText(lang, "creator_dashboard_current_data"), Items: []ui.DetailItem{
			{Label: i18n.Translate(lang, labelGroup) + ":", ValueHTML: ui.EscapeHTML(groupLabel)},
		}},
		Actions: []ui.ActionGroup{
			{Items: []ui.ActionItem{creatorCallbackActionItem(i18n.Translate(lang, btnResetKeepMembers), creatorGroupExecuteWithActionCallback(group.ChatID, core.CreatorResetKeepMembers))}},
			{Items: []ui.ActionItem{creatorDangerActionItem(i18n.Translate(lang, btnResetKickTrackedMembers), creatorGroupExecuteWithActionCallback(group.ChatID, core.CreatorResetKickTrackedMembers), "5258318620722733379")}},
		},
		Navigation: []ui.NavigationItem{{Label: i18n.Translate(lang, btnBack), Target: backCallback}},
	}))
}

func creatorCallbackActionItem(label, callback string) ui.ActionItem {
	return ui.ActionItem{
		Kind:      ui.ActionKindCallback,
		Label:     label,
		Target:    callback,
		Available: true,
	}
}

func creatorIconActionItem(label, callback, icon string) ui.ActionItem {
	return ui.ActionItem{
		Kind:        ui.ActionKindCallback,
		Label:       label,
		Target:      callback,
		IconEmojiID: icon,
		Available:   true,
	}
}

func creatorDangerActionItem(label, callback, icon string) ui.ActionItem {
	item := creatorIconActionItem(label, callback, icon)
	item.Style = ui.StyleDanger
	return item
}

func creatorGroupedActionItem(label, callback string) ui.ActionItem {
	return creatorIconActionItem(label, callback, "5258513401784573443")
}

func creatorGroupMemberTagActionItem(lang string, group core.ManagedGroup) ui.ActionItem {
	item := creatorIconActionItem(groupMemberTagButtonText(lang, group.MemberTagSyncEnabled), creatorGroupMemberTagsOpenCallback(group.ChatID), "5296348778012361146")
	if group.MemberTagSyncEnabled {
		item.Style = ui.StyleSuccess
	}
	return item
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

func formatCreatorGroupMemberTagsState(lang string, enabled bool) string {
	if enabled {
		return i18n.Translate(lang, msgCreatorGroupMemberTagsState)
	}
	return i18n.Translate(lang, msgCreatorGroupMemberTagsOffState)
}

func groupMemberTagButtonText(lang string, enabled bool) string {
	if enabled {
		return i18n.Translate(lang, btnMemberTagSyncDisable)
	}
	return i18n.Translate(lang, btnMemberTagSyncEnable)
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
