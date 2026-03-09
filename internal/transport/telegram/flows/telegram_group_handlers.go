package flows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	"github.com/mymmrac/telego/telegoutil"
)

type botGroupCapabilities struct {
	isAdmin            bool
	canInviteUsers     bool
	canRestrictMembers bool
}

var errTelegramBotNotConfigured = errors.New("telegram bot not configured")

func (c botGroupCapabilities) evaluation() groupCapabilityEvaluation {
	if !c.isAdmin {
		return groupCapabilityEvaluation{botMissing: true}
	}
	return groupCapabilityEvaluation{
		canInviteUsers:   c.canInviteUsers,
		canRestrictUsers: c.canRestrictMembers,
	}
}

// onRegisterGroup handles /registergroup by binding the current Telegram group
// to the caller's creator account. The caller must be a group admin and have
// a linked creator record.
func (c *Controller) onRegisterGroup(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)

	if msg.Chat.Type == telego.ChatTypePrivate {
		view := buildGroupReplyView(lang, msgGroupNotGroup, msg.MessageID)
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, msg.Chat.ID); waitErr != nil {
		c.log().Warn("Get chat member rate limit wait failed", "error", waitErr)
		view := buildGroupReplyView(lang, msgGroupNotAdmin, msg.MessageID)
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(msg.Chat.ID),
		UserID: msg.From.ID,
	})
	isAdmin := err == nil && IsAdmin(member)

	_, ok, err := c.app.CreatorStatus.LoadOwnedCreator(ctx, msg.From.ID)
	if err != nil {
		c.log().Warn("OnRegisterGroup getOwnedCreator failed", "error", err)
		return nil
	}

	// Silently ignore users who are neither admin nor have a creator account.
	if !isAdmin && !ok {
		return nil
	}
	if !isAdmin {
		view := buildGroupReplyView(lang, msgGroupNotAdmin, msg.MessageID)
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if !ok || c.app.GroupRegistration == nil {
		view := buildGroupReplyView(lang, msgGroupNotCreator, msg.MessageID)
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if eval := c.evaluateBotGroupCapabilities(ctx, msg.Chat.ID); len(eval.issues(lang)) > 0 {
		view := buildGroupSettingWarningsView(lang, msg.MessageID, eval.issues(lang))
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	regRes, err := c.app.GroupRegistration.RegisterGroup(ctx, msg.From.ID, msg.Chat.ID, msg.Chat.Title)
	if err != nil {
		c.log().Warn("RegisterGroup failed", "chat_id", msg.Chat.ID, "owner_telegram_id", msg.From.ID, "error", err)
		return nil
	}
	view, ok := buildGroupRegistrationView(lang, msg.MessageID, regRes)
	if !ok {
		c.log().Warn("unsupported group registration outcome", "chat_id", msg.Chat.ID, "outcome", regRes.Outcome)
		return nil
	}

	groupMsgID := c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
	if view.dispatchFollowUp {
		c.dispatchGroupRegistrationFollowUp(ctx, msg, lang, regRes, view, groupMsgID)
	}
	return nil
}

// onUnregisterCommand handles /unregistergroup by unbinding the current Telegram group
// from the caller's creator account. The caller must be the creator managing it.
func (c *Controller) onUnregisterCommand(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	view := buildGroupReplyView(lang, msgGroupUnregisterNotOwner, msg.MessageID)

	if msg.Chat.Type == telego.ChatTypePrivate {
		notGroup := buildGroupReplyView(lang, msgGroupNotGroup, msg.MessageID)
		c.sendMsg(ctx, msg.Chat.ID, notGroup.text, &notGroup.opts)
		return nil
	}

	if c.app.GroupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable")
		return nil
	}

	res, err := c.app.GroupUnregistration.UnregisterGroup(ctx, msg.From.ID, msg.Chat.ID)
	if err != nil {
		c.log().Warn("UnregisterGroup failed", "chat_id", msg.Chat.ID, "owner_telegram_id", msg.From.ID, "error", err)
		return nil
	}
	switch res.Outcome {
	case usecase.UnregisterGroupOutcomeNotManaged:
		return nil
	case usecase.UnregisterGroupOutcomeNotOwner:
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	case usecase.UnregisterGroupOutcomeUnregistered, usecase.UnregisterGroupOutcomeUnregisteredCleanupLag:
	}
	if res.CleanupFailed {
		c.log().Warn("group unregistered but eventsub cleanup deferred to reconciliation", "creator_id", res.Creator.ID, "chat_id", msg.Chat.ID)
	}

	success := buildTextView(lang, msgGroupUnregistered)
	success.opts.ReplyToMessageID = msg.MessageID
	c.sendMsg(ctx, msg.Chat.ID, success.text, &success.opts)
	return nil
}

func (c *Controller) activateCreatorOnFirstGroupRegistration(parent context.Context, creator core.Creator, groupChatID int64, lang string) {
	if parent == nil {
		c.log().Warn("Activate creator called with nil context", "creator_id", creator.ID)
		return
	}
	baseCtx := context.WithoutCancel(parent)
	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()
	res, err := c.app.CreatorActivation.Activate(ctx, creator)
	if err != nil {
		c.log().Warn("creator activation failed after first group registration", "creator_id", creator.ID, "error", err)
		view := buildTextView(lang, msgCreatorEventSubFail)
		c.sendMsg(baseCtx, groupChatID, view.text, &view.opts)
		return
	}
	c.log().Info("creator activated on first group registration", "creator_id", creator.ID, "group_chat_id", groupChatID, "subscriber_count", res.SubscriberCount)
}

func (c *Controller) onMyChatMemberUpdated(ctx *tghandler.Context, update telego.ChatMemberUpdated) error {
	if update.Chat.Type == telego.ChatTypePrivate {
		return nil
	}

	switch update.NewChatMember.MemberStatus() {
	case telego.MemberStatusMember, telego.MemberStatusAdministrator, telego.MemberStatusCreator:
		lang := i18n.NormalizeLanguage(update.From.LanguageCode)
		view := buildGroupBotStatusChangedView(lang)
		groupMsgID := c.sendMsg(ctx, update.Chat.ID, view.text, &view.opts)
		if groupMsgID != 0 {
			c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
				c.sendPostRegistrationSettingsCheck(bg, update.Chat.ID, groupMsgID, lang, view.text)
			})
		}
	case telego.MemberStatusLeft, telego.MemberStatusBanned:
		if c.groupMatchesActiveCreator(ctx, update.Chat.ID) {
			c.log().Info("bot removed from registered group; auto-unregister should be the next step", "chat_id", update.Chat.ID, "new_status", update.NewChatMember.MemberStatus())
		}
	}

	return nil
}

func (c *Controller) onChatMemberUpdated(ctx *tghandler.Context, update telego.ChatMemberUpdated) error {
	group, ok, err := c.store.ManagedGroupByChatID(ctx, update.Chat.ID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for chat_member failed", "chat_id", update.Chat.ID, "error", err)
		return nil
	}
	if !ok {
		return nil
	}

	memberUser := update.NewChatMember.MemberUser()
	if memberUser.IsBot {
		return nil
	}
	if IsAdmin(update.NewChatMember) {
		return nil
	}

	status := update.NewChatMember.MemberStatus()
	switch status {
	case telego.MemberStatusMember, telego.MemberStatusRestricted:
		c.observeGroupMember(ctx, group.ChatID, memberUser.ID, "chat_member", status)
	case telego.MemberStatusLeft, telego.MemberStatusBanned:
		c.removeObservedGroupMember(ctx, group.ChatID, memberUser.ID)
	}
	return nil
}

func (c *Controller) onGroupMessage(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil || msg.From.IsBot {
		return nil
	}
	if strings.HasPrefix(msg.Text, "/") {
		return nil
	}
	group, ok, err := c.store.ManagedGroupByChatID(ctx, msg.Chat.ID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for group message failed", "chat_id", msg.Chat.ID, "error", err)
		return nil
	}
	if !ok {
		return nil
	}
	c.observeGroupMember(ctx, group.ChatID, msg.From.ID, "message", telego.MemberStatusMember)
	return nil
}

// sendPostRegistrationSettingsCheck runs group settings checks and edits the
// group message to append warnings or an "all good" status. No DM is sent.
func (c *Controller) sendPostRegistrationSettingsCheck(ctx context.Context, groupChatID int64, groupMsgID int, lang, groupBaseText string) {
	warnings := c.evaluateGroupSettings(ctx, groupChatID).issues(lang)
	if groupMsgID != 0 {
		view := buildGroupSettingsCheckResultView(lang, groupBaseText, warnings)
		c.reply(ctx, groupChatID, groupMsgID, view.text, &view.opts)
	}
}

// sendPostRegistrationMessages streams a draft DM to the creator while
// checking group settings, then finalises the DM and edits the group message.
func (c *Controller) sendPostRegistrationMessages(ctx context.Context, opts postRegistrationMessageOptions) {
	const draftID = 1

	rendered := renderPostRegistrationCopy(postRegistrationCopyInput{
		lang:          opts.lang,
		groupName:     opts.groupName,
		creatorName:   opts.creatorName,
		groupBaseText: opts.groupBaseText,
	}, nil)

	c.sendDraft(ctx, opts.ownerUserID, draftID, rendered.draftDM, &client.MessageOptions{
		ParseMode: telego.ModeHTML,
	})

	warnings := c.evaluateGroupSettings(ctx, opts.groupChatID).issues(opts.lang)
	rendered = renderPostRegistrationCopy(postRegistrationCopyInput{
		lang:          opts.lang,
		groupName:     opts.groupName,
		creatorName:   opts.creatorName,
		groupBaseText: opts.groupBaseText,
	}, warnings)
	c.sendDraft(ctx, opts.ownerUserID, draftID, rendered.finalDM, &client.MessageOptions{
		ParseMode: telego.ModeHTML,
	})

	c.sendMsg(ctx, opts.ownerUserID, rendered.finalDM, &client.MessageOptions{
		ParseMode: telego.ModeHTML,
	})

	if opts.groupMsgID != 0 {
		c.reply(ctx, opts.groupChatID, opts.groupMsgID, rendered.groupMessage, &client.MessageOptions{
			ParseMode: telego.ModeHTML,
		})
	}
}

type postRegistrationMessageOptions struct {
	groupChatID   int64
	groupMsgID    int
	ownerUserID   int64
	groupName     string
	creatorName   string
	lang          string
	groupBaseText string
}

func (c *Controller) evaluateGroupSettings(ctx context.Context, chatID int64) groupSettingsEvaluation {
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChat rate limit wait failed", "error", waitErr)
		return groupSettingsEvaluation{}
	}
	chat, err := c.tg.GetChat(ctx, &telego.GetChatParams{
		ChatID: telegoutil.ID(chatID),
	})
	if err != nil {
		c.log().Warn("GetChat for group settings check failed", "chat_id", chatID, "error", err)
		return groupSettingsEvaluation{}
	}

	return groupSettingsEvaluation{
		botCapabilities: c.evaluateBotGroupCapabilities(ctx, chatID),
		isPublic:        chat.Username != "" || len(chat.ActiveUsernames) > 0,
		joinByRequest:   chat.JoinByRequest,
		untrackedCount:  c.countUntrackedMembers(ctx, chatID),
	}
}

func (c *Controller) evaluateBotGroupCapabilities(ctx context.Context, chatID int64) groupCapabilityEvaluation {
	caps, err := c.loadBotGroupCapabilities(ctx, chatID)
	if err != nil {
		c.log().Warn("load bot group capabilities failed", "chat_id", chatID, "error", err)
		return groupCapabilityEvaluation{botMissing: true}
	}
	return caps.evaluation()
}

func (c *Controller) loadBotGroupCapabilities(ctx context.Context, chatID int64) (botGroupCapabilities, error) {
	if c.tg == nil {
		return botGroupCapabilities{}, errTelegramBotNotConfigured
	}

	me, err := c.tg.GetMe(ctx)
	if err != nil {
		return botGroupCapabilities{}, fmt.Errorf("get bot profile: %w", err)
	}

	if c.tgLimiter != nil {
		if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
			return botGroupCapabilities{}, fmt.Errorf("wait get chat member: %w", waitErr)
		}
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(chatID),
		UserID: me.ID,
	})
	if err != nil {
		return botGroupCapabilities{}, fmt.Errorf("get bot chat member: %w", err)
	}

	switch m := member.(type) {
	case *telego.ChatMemberOwner:
		return botGroupCapabilities{isAdmin: true, canInviteUsers: true, canRestrictMembers: true}, nil
	case *telego.ChatMemberAdministrator:
		return botGroupCapabilities{
			isAdmin:            true,
			canInviteUsers:     m.CanInviteUsers,
			canRestrictMembers: m.CanRestrictMembers,
		}, nil
	default:
		return botGroupCapabilities{}, nil
	}
}

func (c *Controller) groupMatchesActiveCreator(ctx context.Context, chatID int64) bool {
	_, ok, err := c.store.ManagedGroupByChatID(ctx, chatID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for my_chat_member check failed", "chat_id", chatID, "error", err)
		return false
	}
	return ok
}

func (c *Controller) countUntrackedMembers(ctx context.Context, chatID int64) int {
	count, err := c.store.CountUntrackedGroupMembers(ctx, chatID)
	if err == nil {
		return count
	}
	c.log().Warn("CountUntrackedGroupMembers failed, falling back to estimate", "chat_id", chatID, "error", err)

	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChatMemberCount rate limit wait failed", "error", waitErr)
		return 0
	}
	total, err := c.tg.GetChatMemberCount(ctx, &telego.GetChatMemberCountParams{
		ChatID: telegoutil.ID(chatID),
	})
	if err != nil || total == nil {
		c.log().Warn("GetChatMemberCount failed", "chat_id", chatID, "error", err)
		return 0
	}
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChatAdministrators rate limit wait failed", "error", waitErr)
		return 0
	}
	admins, err := c.tg.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{
		ChatID: telegoutil.ID(chatID),
	})
	if err != nil {
		c.log().Warn("GetChatAdministrators failed", "chat_id", chatID, "error", err)
		return 0
	}
	privileged := len(admins)
	untracked := *total - privileged
	if untracked < 0 {
		return 0
	}
	return untracked
}

func (c *Controller) observeGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string) {
	tracked, err := c.store.IsTrackedGroupMember(ctx, chatID, telegramUserID)
	if err != nil {
		c.log().Warn("IsTrackedGroupMember failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "error", err)
		return
	}
	now := time.Now().UTC()
	if tracked {
		if err := c.store.AddTrackedGroupMember(ctx, chatID, telegramUserID, source, now); err != nil {
			c.log().Warn("AddTrackedGroupMember refresh failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "error", err)
		}
		if err := c.store.RemoveUntrackedGroupMember(ctx, chatID, telegramUserID); err != nil {
			c.log().Warn("RemoveUntrackedGroupMember refresh failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "error", err)
		}
		return
	}
	if err := c.store.UpsertUntrackedGroupMember(ctx, chatID, telegramUserID, source, status, now); err != nil {
		c.log().Warn("UpsertUntrackedGroupMember failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "source", source, "error", err)
	}
}

func (c *Controller) removeObservedGroupMember(ctx context.Context, chatID, telegramUserID int64) {
	if err := c.store.RemoveTrackedGroupMember(ctx, chatID, telegramUserID); err != nil {
		c.log().Warn("RemoveTrackedGroupMember failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "error", err)
	}
	if err := c.store.RemoveUntrackedGroupMember(ctx, chatID, telegramUserID); err != nil {
		c.log().Warn("RemoveUntrackedGroupMember failed", "chat_id", chatID, "telegram_user_id", telegramUserID, "error", err)
	}
}

// IsAdmin reports whether member has Administrator or Creator status.
func IsAdmin(member telego.ChatMember) bool {
	if member == nil {
		return false
	}
	switch member.MemberStatus() {
	case telego.MemberStatusAdministrator, telego.MemberStatusCreator:
		return true
	}
	return false
}
