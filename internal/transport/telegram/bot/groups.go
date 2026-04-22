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
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	telegramui "imsub/internal/transport/telegram/ui"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	"github.com/mymmrac/telego/telegoutil"
)

const groupBootstrapTimeout = 30 * time.Second

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

// onRegisterGroup handles /linkgroup by linking the current Telegram group
// to the caller's creator account. The caller must be a group admin and have
// a linked creator record.
func (c *Bot) onRegisterGroup(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	threadID := groupMessageThreadID(msg)

	if msg.Chat.Type == telego.ChatTypePrivate {
		_, ok, err := c.creatorStatus.LoadOwnedCreator(ctx, msg.From.ID)
		if err != nil {
			setTelegramCommandResponseResult(ctx, "error")
			c.observeTelegramCommandResponse(ctx, "error")
			c.log().Warn("OnRegisterGroup getOwnedCreator failed", "error", err)
			return nil
		}
		if !ok {
			setTelegramCommandResponseResult(ctx, "not_creator")
			c.observeTelegramCommandResponse(ctx, "not_creator")
			return nil
		}
		setTelegramCommandResponseResult(ctx, "private_chat")
		view := buildGroupReplyView(lang, msgGroupNotGroup, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, msg.Chat.ID); waitErr != nil {
		c.log().Warn("Get chat member rate limit wait failed", "error", waitErr)
		view := buildGroupReplyView(lang, msgGroupNotAdmin, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(msg.Chat.ID),
		UserID: msg.From.ID,
	})
	isAdmin := err == nil && IsAdmin(member)

	_, ok, err := c.creatorStatus.LoadOwnedCreator(ctx, msg.From.ID)
	if err != nil {
		setTelegramCommandResponseResult(ctx, "error")
		c.observeTelegramCommandResponse(ctx, "error")
		c.log().Warn("OnRegisterGroup getOwnedCreator failed", "error", err)
		return nil
	}

	if !isAdmin && !ok {
		setTelegramCommandResponseResult(ctx, "ignored")
		c.observeTelegramCommandResponse(ctx, "ignored")
		return nil
	}
	if !isAdmin {
		setTelegramCommandResponseResult(ctx, "not_admin")
		view := buildGroupReplyView(lang, msgGroupNotAdmin, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if !ok || c.groupRegistration == nil {
		_, managed, managedErr := c.store.ManagedGroupByChatID(ctx, msg.Chat.ID)
		if managedErr != nil {
			setTelegramCommandResponseResult(ctx, "error")
			c.observeTelegramCommandResponse(ctx, "error")
			c.log().Warn("ManagedGroupByChatID failed before not-creator reply", "chat_id", msg.Chat.ID, "error", managedErr)
			return nil
		}
		if managed {
			setTelegramCommandResponseResult(ctx, "managed_noop")
			c.observeTelegramCommandResponse(ctx, "managed_noop")
			return nil
		}
		setTelegramCommandResponseResult(ctx, "not_creator")
		view := buildGroupReplyView(lang, msgGroupNotCreator, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if eval := c.evaluateBotGroupCapabilities(ctx, msg.Chat.ID); len(eval.issues(lang)) > 0 {
		setTelegramCommandResponseResult(ctx, "capability_warning")
		view := buildGroupPermissionsBlockedView(lang, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	estimatedMembers := c.estimateExistingNonAdminMembers(ctx, msg.Chat.ID)
	setTelegramCommandResponseResult(ctx, "prompted_policy")
	view := buildGroupRegistrationPolicyPromptView(lang, msg.MessageID, msg.Chat.ID, threadID, estimatedMembers)
	view.opts.MessageThreadID = threadID
	if messageID := c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts); messageID != 0 {
		c.log().Info("group registration policy prompt rendered", "chat_id", msg.Chat.ID, "owner_telegram_id", msg.From.ID, "thread_id", threadID, "estimated_non_admin_members", estimatedMembers)
	}
	return nil
}

func (c *Bot) handleGroupCallback(ctx context.Context, userID, chatID int64, chatTitle string, editMsgID int, lang string, action callbackAction) callbackFeedback {
	lang = c.groupChatLanguage(ctx, chatID, lang)
	if chatID == 0 || action.chatID == 0 || action.chatID != chatID {
		setTelegramCallbackResponseResult(ctx, "chat_mismatch")
		c.log().Warn("group callback chat mismatch", "telegram_user_id", userID, "callback_chat_id", action.chatID, "message_chat_id", chatID)
		return noCallbackFeedback()
	}
	if !c.userCanRegisterGroup(ctx, userID, chatID) {
		setTelegramCallbackResponseResult(ctx, "not_admin")
		return callbackAlert(i18n.Translate(lang, msgGroupNotAdmin))
	}
	_, ok, err := c.creatorStatus.LoadOwnedCreator(ctx, userID)
	if err != nil {
		setTelegramCallbackResponseResult(ctx, "error")
		c.log().Warn("group callback getOwnedCreator failed", "chat_id", chatID, "owner_telegram_id", userID, "error", err)
		return noCallbackFeedback()
	}
	if !ok {
		_, managed, managedErr := c.store.ManagedGroupByChatID(ctx, chatID)
		if managedErr != nil {
			setTelegramCallbackResponseResult(ctx, "error")
			c.log().Warn("ManagedGroupByChatID failed before group not-creator callback", "chat_id", chatID, "owner_telegram_id", userID, "error", managedErr)
			return noCallbackFeedback()
		}
		if managed {
			setTelegramCallbackResponseResult(ctx, "managed_noop")
			return noCallbackFeedback()
		}
		setTelegramCallbackResponseResult(ctx, "not_creator")
		return callbackAlert(i18n.Translate(lang, msgGroupNotCreator))
	}
	switch action.verb {
	case callbackVerbPick:
		if action.policy == "" || c.groupRegistration == nil {
			setTelegramCallbackResponseResult(ctx, "unsupported")
			c.log().Warn("unsupported group registration callback action", "telegram_user_id", userID, "verb", action.verb, "policy", action.policy, "chat_id", action.chatID)
			return noCallbackFeedback()
		}
		c.log().Info("group registration callback received", "chat_id", chatID, "owner_telegram_id", userID, "policy", action.policy, "thread_id", action.threadID)
		if eval := c.evaluateBotGroupCapabilities(ctx, chatID); len(eval.issues(lang)) > 0 {
			setTelegramCallbackResponseResult(ctx, "capability_warning")
			view := buildGroupPermissionsBlockedView(lang, editMsgID)
			c.reply(ctx, chatID, editMsgID, view.text, &view.opts)
			return noCallbackFeedback()
		}
		regRes, err := c.groupRegistration.RegisterGroup(ctx, userID, chatID, chatTitle, lang, action.policy, action.threadID)
		if err != nil {
			setTelegramCallbackResponseResult(ctx, "error")
			c.log().Warn("RegisterGroup from callback failed", "chat_id", chatID, "owner_telegram_id", userID, "policy", action.policy, "error", err)
			return noCallbackFeedback()
		}
		c.log().Info("group registration callback completed", "chat_id", chatID, "owner_telegram_id", userID, "policy", action.policy, "outcome", regRes.Outcome)
		switch regRes.Outcome {
		case usecase.RegisterGroupOutcomeRegistered:
			setTelegramCallbackResponseResult(ctx, "registered")
		case usecase.RegisterGroupOutcomeAlreadyLinked:
			setTelegramCallbackResponseResult(ctx, "already_linked")
			return callbackAck(i18n.Translate(lang, msgCbGroupAlreadyLinked))
		case usecase.RegisterGroupOutcomeTakenByOther:
			setTelegramCallbackResponseResult(ctx, "taken_by_other")
			return callbackAlert(i18n.Translate(lang, msgCbGroupTakenByOther))
		case usecase.RegisterGroupOutcomeNotCreator:
			setTelegramCallbackResponseResult(ctx, "not_creator")
		default:
			setTelegramCallbackResponseResult(ctx, "unsupported")
		}
		view, ok := buildGroupRegistrationView(lang, editMsgID, c.botUsername(ctx), regRes)
		if !ok {
			switch regRes.Outcome {
			case usecase.RegisterGroupOutcomeAlreadyLinked, usecase.RegisterGroupOutcomeTakenByOther:
				return noCallbackFeedback()
			case usecase.RegisterGroupOutcomeNotCreator, usecase.RegisterGroupOutcomeRegistered:
				c.log().Warn("unexpected empty group registration view", "chat_id", chatID, "outcome", regRes.Outcome)
				return noCallbackFeedback()
			default:
				c.log().Warn("unsupported group registration outcome from callback", "chat_id", chatID, "outcome", regRes.Outcome)
				return noCallbackFeedback()
			}
		}
		c.reply(ctx, chatID, editMsgID, view.text, &view.opts)
		if view.dispatchFollowUp {
			c.dispatchGroupRegistrationFollowUp(ctx, lang, regRes)
		}
		return callbackAck(i18n.Translate(lang, msgCbGroupRegistered))
	case callbackVerbExecute:
		if action.resetAction == "" || c.groupUnregistration == nil {
			setTelegramCallbackResponseResult(ctx, "unsupported")
			c.log().Warn("unsupported group unregister callback action", "telegram_user_id", userID, "verb", action.verb, "action", action.resetAction, "chat_id", action.chatID)
			return noCallbackFeedback()
		}
		res, err := c.groupUnregistration.UnregisterGroup(ctx, userID, chatID, action.resetAction)
		if err != nil {
			setTelegramCallbackResponseResult(ctx, "error")
			c.log().Warn("UnregisterGroup from group callback failed", "chat_id", chatID, "owner_telegram_id", userID, "action", action.resetAction, "error", err)
			return noCallbackFeedback()
		}
		switch res.Outcome {
		case usecase.UnregisterGroupOutcomeNotManaged:
			setTelegramCallbackResponseResult(ctx, "managed_noop")
			return noCallbackFeedback()
		case usecase.UnregisterGroupOutcomeNotOwner:
			setTelegramCallbackResponseResult(ctx, "not_owner")
			return callbackAlert(i18n.Translate(lang, msgGroupUnregisterNotOwner))
		case usecase.UnregisterGroupOutcomeUnregistered, usecase.UnregisterGroupOutcomeUnregisteredCleanupLag:
			setTelegramCallbackResponseResult(ctx, "unregistered")
		default:
			setTelegramCallbackResponseResult(ctx, "unsupported")
			c.log().Warn("unsupported group unregistration outcome from callback", "chat_id", chatID, "outcome", res.Outcome)
			return noCallbackFeedback()
		}
		view := buildGroupUnregisteredView(lang, editMsgID)
		c.reply(ctx, chatID, editMsgID, view.text, &view.opts)
		return noCallbackFeedback()
	case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect, callbackVerbOpen, callbackVerbBack, callbackVerbMenu, callbackVerbCancel, callbackVerbExport:
		setTelegramCallbackResponseResult(ctx, "unsupported")
		c.log().Warn("known but unsupported group callback verb", "telegram_user_id", userID, "verb", action.verb, "chat_id", action.chatID)
		return noCallbackFeedback()
	default:
		setTelegramCallbackResponseResult(ctx, "unsupported")
		c.log().Warn("unknown group callback action", "telegram_user_id", userID, "verb", action.verb, "chat_id", action.chatID)
		return noCallbackFeedback()
	}
}

func (c *Bot) userCanRegisterGroup(ctx context.Context, userID, chatID int64) bool {
	if c.tgLimiter != nil {
		if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
			c.log().Warn("Get chat member rate limit wait failed", "chat_id", chatID, "error", waitErr)
			return false
		}
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(chatID),
		UserID: userID,
	})
	return err == nil && IsAdmin(member)
}

// onUnregisterCommand handles /unlinkgroup by unlinking the current Telegram group.
func (c *Bot) onUnregisterCommand(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := c.groupMessageLanguage(ctx, msg, i18n.NormalizeLanguage(msg.From.LanguageCode))
	threadID := groupMessageThreadID(msg)
	view := buildGroupReplyView(lang, msgGroupUnregisterNotOwner, msg.MessageID)
	view.opts.MessageThreadID = threadID

	if msg.Chat.Type == telego.ChatTypePrivate {
		creator, ok, err := c.store.OwnedCreatorForUser(ctx, msg.From.ID)
		if err != nil {
			c.log().Warn("OwnedCreatorForUser failed before unlink prompt", "owner_telegram_id", msg.From.ID, "error", err)
			return nil
		}
		if !ok || creator.ID == "" {
			return nil
		}
		notGroup := buildGroupReplyView(lang, msgGroupNotGroup, msg.MessageID)
		notGroup.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, notGroup.text, &notGroup.opts)
		return nil
	}

	if c.groupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable")
		return nil
	}

	group, managed, err := c.store.ManagedGroupByChatID(ctx, msg.Chat.ID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID failed before unregister prompt", "chat_id", msg.Chat.ID, "error", err)
		return nil
	}
	if !managed {
		return nil
	}
	creator, ok, err := c.store.OwnedCreatorForUser(ctx, msg.From.ID)
	if err != nil {
		c.log().Warn("OwnedCreatorForUser failed before unregister prompt", "chat_id", msg.Chat.ID, "owner_telegram_id", msg.From.ID, "error", err)
		return nil
	}
	if !ok || group.CreatorID != creator.ID {
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	prompt := buildGroupUnregisterPromptView(lang, msg.MessageID, msg.Chat.ID)
	prompt.opts.MessageThreadID = threadID
	c.sendMsg(ctx, msg.Chat.ID, prompt.text, &prompt.opts)
	return nil
}

func (c *Bot) activateCreatorOnFirstGroupRegistration(parent context.Context, creator core.Creator, lang string) {
	if parent == nil {
		c.log().Warn("Activate creator called with nil context", "creator_id", creator.ID)
		return
	}
	baseCtx := context.WithoutCancel(parent)
	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()
	res, err := c.creatorActivation.Activate(ctx, creator)
	if err != nil {
		c.log().Warn("creator activation failed after first group registration", "creator_id", creator.ID, "error", err)
		if notifyErr := c.sendCreatorReconnectPrompt(baseCtx, creator.OwnerTelegramID, lang); notifyErr != nil {
			c.log().Warn("creator reconnect prompt failed after first group registration", "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "error", notifyErr)
		}
		return
	}
	c.log().Info("creator activated on first group registration", "creator_id", creator.ID, "subscriber_count", res.SubscriberCount)
}

func (c *Bot) onMyChatMemberUpdated(ctx *tghandler.Context, update telego.ChatMemberUpdated) error {
	if update.Chat.Type == telego.ChatTypePrivate {
		return nil
	}

	oldCaps := capabilitiesFromChatMember(update.OldChatMember)
	newCaps := capabilitiesFromChatMember(update.NewChatMember)
	oldStatus := ""
	if update.OldChatMember != nil {
		oldStatus = update.OldChatMember.MemberStatus()
	}
	newStatus := update.NewChatMember.MemberStatus()

	switch newStatus {
	case telego.MemberStatusMember, telego.MemberStatusAdministrator, telego.MemberStatusCreator:
		lang := i18n.NormalizeLanguage(update.From.LanguageCode)
		group, managed, err := c.store.ManagedGroupByChatID(ctx, update.Chat.ID)
		if err != nil {
			c.log().Warn("ManagedGroupByChatID for chat-member update failed", "chat_id", update.Chat.ID, "error", err)
			return nil
		}
		if managed {
			if group.RegistrationThreadID > 0 {
				lang = c.groupChatLanguage(ctx, group.ChatID, lang)
			}
			if oldCaps.healthy() && !newCaps.healthy() {
				c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
					c.notifyManagedGroupPermissionsCompromised(bg, group, lang, newCaps)
				})
				return nil
			}
			if oldStatus == newStatus {
				return nil
			}
			c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
				warnings := c.evaluateGroupSettings(bg, update.Chat.ID).issues(lang)
				if len(warnings) == 0 {
					return
				}
				view := buildGroupSettingWarningsView(lang, 0, warnings)
				view.opts.MessageThreadID = group.RegistrationThreadID
				c.sendMsg(bg, update.Chat.ID, view.text, &view.opts)
			})
			return nil
		}
		if oldStatus == newStatus {
			return nil
		}
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			view := buildGroupSetupPermissionsView(lang)
			c.sendMsg(bg, update.Chat.ID, view.text, &view.opts)
		})
	case telego.MemberStatusLeft, telego.MemberStatusBanned:
		if oldStatus == newStatus {
			return nil
		}
		c.handleBotRemovedFromManagedGroup(ctx, update.Chat.ID, newStatus)
	default:
		if oldStatus == newStatus {
			return nil
		}
	}

	return nil
}

func (c *Bot) handleBotRemovedFromManagedGroup(ctx context.Context, chatID int64, newStatus string) {
	group, ok, err := c.store.ManagedGroupByChatID(ctx, chatID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for bot removal failed", "chat_id", chatID, "error", err)
		return
	}
	if !ok {
		return
	}
	creator, ok, err := c.store.Creator(ctx, group.CreatorID)
	if err != nil {
		c.log().Warn("Creator lookup for bot removal failed", "chat_id", chatID, "creator_id", group.CreatorID, "error", err)
		return
	}
	if !ok {
		c.log().Warn("Creator missing for managed group on bot removal", "chat_id", chatID, "creator_id", group.CreatorID)
		return
	}
	if c.groupUnregistration == nil {
		c.log().Warn("group unregistration use case unavailable for bot removal", "chat_id", chatID, "creator_id", creator.ID)
		return
	}

	res, err := c.groupUnregistration.UnregisterGroup(ctx, creator.OwnerTelegramID, chatID, core.CreatorResetKeepMembers)
	if err != nil {
		c.log().Warn("auto-unregister after bot removal failed", "chat_id", chatID, "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "new_status", newStatus, "error", err)
		return
	}
	switch res.Outcome {
	case usecase.UnregisterGroupOutcomeNotManaged:
		return
	case usecase.UnregisterGroupOutcomeNotOwner:
		c.log().Warn("auto-unregister after bot removal rejected owner", "chat_id", chatID, "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID)
		return
	case usecase.UnregisterGroupOutcomeUnregistered, usecase.UnregisterGroupOutcomeUnregisteredCleanupLag:
	default:
		c.log().Warn("auto-unregister after bot removal returned unexpected outcome", "chat_id", chatID, "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "outcome", res.Outcome)
		return
	}

	c.log().Info("auto-unregistered managed group after bot removal", "chat_id", chatID, "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "new_status", newStatus, "cleanup_failed", res.CleanupFailed)
	c.notifyOwnerGroupAutoUnregistered(ctx, creator, res.Group)
}

func (c *Bot) notifyOwnerGroupAutoUnregistered(ctx context.Context, creator core.Creator, group core.ManagedGroup) {
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, creator.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = identity.Language
	}
	view := buildGroupBotRemovedOwnerView(lang, group.GroupName)
	if messageID := c.sendMsg(ctx, creator.OwnerTelegramID, view.text, &view.opts); messageID == 0 {
		c.log().Warn("send auto-unregister owner notification failed", "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "chat_id", group.ChatID)
	}
}

func (c *Bot) notifyManagedGroupPermissionsCompromised(ctx context.Context, group core.ManagedGroup, lang string, caps membershipCapabilitySnapshot) {
	missing := formatMissingRequiredPermissions(lang, caps)
	view := buildGroupCompromisedView(lang, missing)
	view.opts.MessageThreadID = group.RegistrationThreadID
	c.sendMsg(ctx, group.ChatID, view.text, &view.opts)

	creator, ok, err := c.store.Creator(ctx, group.CreatorID)
	if err != nil || !ok {
		if err != nil {
			c.log().Warn("load creator for compromised group notification failed", "creator_id", group.CreatorID, "chat_id", group.ChatID, "error", err)
		}
		return
	}
	ownerLang := lang
	if identity, found, identityErr := c.store.UserIdentity(ctx, creator.OwnerTelegramID); identityErr == nil && found && identity.Language != "" {
		ownerLang = identity.Language
	}
	dmView := buildGroupCompromisedOwnerView(ownerLang, group.GroupName, formatMissingRequiredPermissions(ownerLang, caps))
	c.sendMsg(ctx, creator.OwnerTelegramID, dmView.text, &dmView.opts)
}

func (c *Bot) onChatMemberUpdated(ctx *tghandler.Context, update telego.ChatMemberUpdated) error {
	group, ok, err := c.store.ManagedGroupByChatID(ctx, update.Chat.ID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for chat_member failed", "chat_id", update.Chat.ID, "error", err)
		return nil
	}
	if !ok {
		return nil
	}

	memberUser := update.NewChatMember.MemberUser()
	if memberUser.IsBot || IsAdmin(update.NewChatMember) {
		return nil
	}

	status := update.NewChatMember.MemberStatus()
	switch status {
	case telego.MemberStatusMember, telego.MemberStatusRestricted:
		c.observeGroupMember(ctx, group, memberUser.ID, "chat_member", status, telegramUserDisplayName(memberUser))
	case telego.MemberStatusLeft, telego.MemberStatusBanned:
		c.removeObservedGroupMember(ctx, group.ChatID, memberUser.ID)
	}
	return nil
}

func (c *Bot) onGroupMessage(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil || msg.From.IsBot || strings.HasPrefix(msg.Text, "/") {
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
	c.observeGroupMember(ctx, group, msg.From.ID, "message", telego.MemberStatusMember, telegramUserDisplayName(*msg.From))
	return nil
}

func (c *Bot) groupMessageLanguage(ctx context.Context, msg telego.Message, fallback string) string {
	if msg.Chat.Type == telego.ChatTypePrivate {
		return i18n.NormalizeLanguage(fallback)
	}
	return c.groupChatLanguage(ctx, msg.Chat.ID, fallback)
}

func (c *Bot) groupChatLanguage(ctx context.Context, chatID int64, fallback string) string {
	if group, ok, err := c.store.ManagedGroupByChatID(ctx, chatID); err == nil && ok {
		if group.Language != "" {
			return i18n.NormalizeLanguage(group.Language)
		}
		if creator, creatorOK, creatorErr := c.store.Creator(ctx, group.CreatorID); creatorErr == nil && creatorOK {
			if identity, found, identityErr := c.store.UserIdentity(ctx, creator.OwnerTelegramID); identityErr == nil && identity.Language != "" && found {
				return i18n.NormalizeLanguage(identity.Language)
			}
		}
		return i18n.DefaultLanguage
	}
	return i18n.NormalizeLanguage(fallback)
}

func telegramUserDisplayName(user telego.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if fullName != "" {
		return fullName
	}
	return ""
}

type groupRegistrationView struct {
	text             string
	opts             client.MessageOptions
	groupBaseText    string
	dispatchFollowUp bool
}

func buildGroupRegistrationView(lang string, replyToMessageID int, botUsername string, regRes usecase.RegisterGroupResult) (groupRegistrationView, bool) {
	view := groupRegistrationView{opts: client.MessageOptions{ReplyToMessageID: replyToMessageID}}
	policyLine := formatGroupPolicyLine(lang, regRes.ExistingGroup.Policy)

	switch regRes.Outcome {
	case usecase.RegisterGroupOutcomeNotCreator:
		view.text = i18n.Translate(lang, msgGroupNotCreator)
	case usecase.RegisterGroupOutcomeTakenByOther:
		return groupRegistrationView{}, false
	case usecase.RegisterGroupOutcomeAlreadyLinked:
		return groupRegistrationView{}, false
	case usecase.RegisterGroupOutcomeRegistered:
		handle, link := botEntryLinks(botUsername)
		linksLine := ""
		if handle != "" && link != "" {
			linksLine = fmt.Sprintf(i18n.Translate(lang, msgGroupRegisteredLinks), html.EscapeString(handle), html.EscapeString(link))
		}
		view.groupBaseText = joinNonEmptySections(
			textSection{text: i18n.Translate(lang, msgGroupRegistered)},
			textSection{text: linksLine},
		)
		view.text = joinNonEmptySections(
			textSection{text: view.groupBaseText},
			textSection{text: policyLine},
		)
		if link != "" {
			view.opts.Markup = telegoutil.InlineKeyboard(
				telegoutil.InlineKeyboardRow(telegramui.CopyLinkButton(i18n.Translate(lang, btnCopyLink), "https://"+link)),
			)
		}
		view.dispatchFollowUp = true
	default:
		return groupRegistrationView{}, false
	}

	return view, true
}

func (c *Bot) dispatchGroupRegistrationFollowUp(ctx context.Context, lang string, regRes usecase.RegisterGroupResult) {
	shouldBootstrap := regRes.Outcome == usecase.RegisterGroupOutcomeRegistered
	if !regRes.FollowUp.NeedsActivation && !regRes.FollowUp.NeedsSettingsCheck && !shouldBootstrap {
		return
	}
	if regRes.FollowUp.NeedsActivation && c.creatorActivation != nil {
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			c.activateCreatorOnFirstGroupRegistration(bg, regRes.Creator, lang)
		})
	}
	if !shouldBootstrap {
		return
	}
	if c.groupBootstrap == nil {
		c.emitGroupBootstrapOutcome(ctx, regRes.ExistingGroup, "disabled", "mtproto_not_configured")
		c.log().Info("group mtproto bootstrap skipped", "chat_id", regRes.ExistingGroup.ChatID, "creator_id", regRes.ExistingGroup.CreatorID, "reason", "mtproto_not_configured")
		return
	}
	if supported, reason := c.groupBootstrapSupport(ctx, regRes.ExistingGroup); !supported {
		c.emitGroupBootstrapOutcome(ctx, regRes.ExistingGroup, "unsupported", reason)
		c.log().Info("group mtproto bootstrap skipped", "chat_id", regRes.ExistingGroup.ChatID, "creator_id", regRes.ExistingGroup.CreatorID, "reason", reason)
		return
	}
	c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
		bootstrapCtx, cancel := context.WithTimeout(bg, groupBootstrapTimeout)
		defer cancel()
		if err := c.groupBootstrap.BootstrapGroup(bootstrapCtx, regRes.ExistingGroup); err != nil {
			c.log().Warn("group mtproto bootstrap failed", "chat_id", regRes.ExistingGroup.ChatID, "creator_id", regRes.ExistingGroup.CreatorID, "error", err)
		}
	})
}

func (c *Bot) groupBootstrapSupport(ctx context.Context, group core.ManagedGroup) (bool, string) {
	caps, err := c.loadBotGroupCapabilities(ctx, group.ChatID)
	if err != nil {
		c.log().Warn("group mtproto bootstrap support check failed; proceeding", "chat_id", group.ChatID, "creator_id", group.CreatorID, "error", err)
		return true, ""
	}
	return bootstrapSupportForCapabilities(caps.evaluation(), group.Policy)
}

func bootstrapSupportForCapabilities(caps groupCapabilityEvaluation, policy core.GroupPolicy) (bool, string) {
	if caps.botMissing {
		return false, "bot_missing"
	}
	if !caps.canInviteUsers {
		return false, "bot_no_invite_rights"
	}
	if policy == core.GroupPolicyKick && !caps.canRestrictUsers {
		return false, "bot_no_restrict_rights"
	}
	return true, ""
}

func (c *Bot) emitGroupBootstrapOutcome(ctx context.Context, group core.ManagedGroup, outcome, reason string) {
	if c == nil || c.events == nil {
		return
	}
	fields := map[string]string{
		"chat_id":    strconv.FormatInt(group.ChatID, 10),
		"creator_id": group.CreatorID,
		"policy":     string(group.Policy),
	}
	if reason != "" {
		fields["reason"] = reason
	}
	c.events.Emit(ctx, events.Event{
		Name:    events.NameTelegramMTProtoBootstrap,
		Outcome: outcome,
		Fields:  fields,
	})
}

func formatGroupSettingWarnings(lang string, issues []string) string {
	return renderWarningBlock(i18n.Translate(lang, msgGroupWarnSettingsIntro), issues)
}

func formatGroupSettingsResult(lang string, issues []string) string {
	if len(issues) > 0 {
		return formatGroupSettingWarnings(lang, issues)
	}
	return ""
}

type groupSettingsEvaluation struct {
	botCapabilities groupCapabilityEvaluation
	isPublic        bool
	joinByRequest   bool
	untrackedCount  int
}

type groupCapabilityEvaluation struct {
	botMissing       bool
	canInviteUsers   bool
	canRestrictUsers bool
}

type membershipCapabilitySnapshot struct {
	isAdmin            bool
	canInviteUsers     bool
	canRestrictMembers bool
}

func (s membershipCapabilitySnapshot) healthy() bool {
	return s.isAdmin && s.canInviteUsers && s.canRestrictMembers
}

func capabilitiesFromChatMember(member telego.ChatMember) membershipCapabilitySnapshot {
	switch m := member.(type) {
	case *telego.ChatMemberOwner:
		return membershipCapabilitySnapshot{isAdmin: true, canInviteUsers: true, canRestrictMembers: true}
	case *telego.ChatMemberAdministrator:
		return membershipCapabilitySnapshot{
			isAdmin:            true,
			canInviteUsers:     m.CanInviteUsers,
			canRestrictMembers: m.CanRestrictMembers,
		}
	default:
		return membershipCapabilitySnapshot{}
	}
}

func (s membershipCapabilitySnapshot) missingRequiredPermissions() []string {
	if !s.isAdmin {
		return []string{"admin", "invite", "restrict"}
	}
	var out []string
	if !s.canInviteUsers {
		out = append(out, "invite")
	}
	if !s.canRestrictMembers {
		out = append(out, "restrict")
	}
	return out
}

func (e groupCapabilityEvaluation) issues(lang string) []string {
	if e.botMissing {
		return []string{i18n.Translate(lang, msgGroupWarnBotNotAdmin)}
	}

	var issues []string
	if !e.canInviteUsers {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnBotNoInvite))
	}
	if !e.canRestrictUsers {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnBotNoRestrict))
	}
	return issues
}

func (e groupSettingsEvaluation) issues(lang string) []string {
	issues := e.botCapabilities.issues(lang)
	if e.isPublic {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnPublic))
	}
	if !e.joinByRequest {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnJoinByReq))
	}
	if e.untrackedCount > 0 {
		issues = append(issues, fmt.Sprintf(i18n.Translate(lang, msgGroupWarnUntrackedUsers), e.untrackedCount))
	}
	return issues
}

func (c *Bot) evaluateGroupSettings(ctx context.Context, chatID int64) groupSettingsEvaluation {
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChat rate limit wait failed", "error", waitErr)
		return groupSettingsEvaluation{}
	}
	chat, err := c.tg.GetChat(ctx, &telego.GetChatParams{ChatID: telegoutil.ID(chatID)})
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

func (c *Bot) evaluateBotGroupCapabilities(ctx context.Context, chatID int64) groupCapabilityEvaluation {
	caps, err := c.loadBotGroupCapabilities(ctx, chatID)
	if err != nil {
		c.log().Warn("load bot group capabilities failed", "chat_id", chatID, "error", err)
		return groupCapabilityEvaluation{botMissing: true}
	}
	return caps.evaluation()
}

func (c *Bot) loadBotGroupCapabilities(ctx context.Context, chatID int64) (botGroupCapabilities, error) {
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

func (c *Bot) countUntrackedMembers(ctx context.Context, chatID int64) int {
	count, err := c.store.CountUntrackedGroupMembers(ctx, chatID)
	if err == nil {
		return count
	}
	c.log().Warn("CountUntrackedGroupMembers failed, falling back to estimate", "chat_id", chatID, "error", err)
	return c.estimateExistingNonAdminMembers(ctx, chatID)
}

func (c *Bot) estimateExistingNonAdminMembers(ctx context.Context, chatID int64) int {
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChatMemberCount rate limit wait failed", "error", waitErr)
		return 0
	}
	total, err := c.tg.GetChatMemberCount(ctx, &telego.GetChatMemberCountParams{ChatID: telegoutil.ID(chatID)})
	if err != nil || total == nil {
		c.log().Warn("GetChatMemberCount failed", "chat_id", chatID, "error", err)
		return 0
	}
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChatAdministrators rate limit wait failed", "error", waitErr)
		return 0
	}
	admins, err := c.tg.GetChatAdministrators(ctx, &telego.GetChatAdministratorsParams{ChatID: telegoutil.ID(chatID)})
	if err != nil {
		c.log().Warn("GetChatAdministrators failed", "chat_id", chatID, "error", err)
		return 0
	}
	untracked := *total - len(admins)
	if untracked < 0 {
		return 0
	}
	return untracked
}

func (c *Bot) observeGroupMember(ctx context.Context, group core.ManagedGroup, telegramUserID int64, source, status, memberLabel string) {
	if c.godAccess != nil && c.godAccess.IsGodTelegramUser(telegramUserID) {
		now := time.Now().UTC()
		if err := c.store.AddTrackedGroupMember(ctx, group.ChatID, telegramUserID, core.SourceGodList, now); err != nil {
			c.log().Warn("AddTrackedGroupMember god access failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
			return
		}
		if err := c.store.RemoveUntrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
			c.log().Warn("RemoveUntrackedGroupMember god access failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
		}
		return
	}
	tracked, err := c.store.IsTrackedGroupMember(ctx, group.ChatID, telegramUserID)
	if err != nil {
		c.log().Warn("IsTrackedGroupMember failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
		return
	}
	now := time.Now().UTC()
	if tracked {
		if err := c.store.AddTrackedGroupMember(ctx, group.ChatID, telegramUserID, source, now); err != nil {
			c.log().Warn("AddTrackedGroupMember refresh failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
		}
		if err := c.store.RemoveUntrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
			c.log().Warn("RemoveUntrackedGroupMember refresh failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
		}
		return
	}
	promoted, err := core.PromoteExistingMemberIfEligible(ctx, c.store, c.godAccess, group, telegramUserID, core.SourceObservedExistingMember, now)
	if promoted {
		if err != nil {
			c.log().Warn("promote observed group member cleanup failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", telegramUserID, "error", err)
		}
		return
	}
	if err != nil {
		if !errors.Is(err, core.ErrCreatorMissing) {
			c.log().Warn("promote observed group member failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", telegramUserID, "error", err)
			return
		}
		c.log().Warn("creator missing for observed group member", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", telegramUserID)
	}
	if err := c.store.UpsertUntrackedGroupMember(ctx, group.ChatID, telegramUserID, source, status, now); err != nil {
		c.log().Warn("UpsertUntrackedGroupMember failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "source", source, "error", err)
		return
	}
	if group.Policy == core.GroupPolicyObserveWarn && source == "chat_member" {
		c.sendGroupUntrackedJoinWarning(ctx, group, telegramUserID, memberLabel)
		return
	}
	if group.Policy != core.GroupPolicyKick {
		return
	}
	if err := c.KickFromGroup(ctx, group.ChatID, telegramUserID, core.KickReasonGroupPolicy); err != nil {
		c.log().Warn("kick unverified group member failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "source", source, "error", err)
		return
	}
	if err := c.store.RemoveUntrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
		c.log().Warn("RemoveUntrackedGroupMember after kick failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
	}
}

func (c *Bot) sendGroupUntrackedJoinWarning(ctx context.Context, group core.ManagedGroup, telegramUserID int64, memberLabel string) {
	lang := c.groupChatLanguage(ctx, group.ChatID, group.Language)
	view := buildGroupUntrackedJoinWarningView(lang, telegramUserID, memberLabel)
	view.opts.MessageThreadID = group.RegistrationThreadID
	if c.sendMsg(ctx, group.ChatID, view.text, &view.opts) == 0 {
		c.log().Warn("send untracked join warning failed", "chat_id", group.ChatID, "registration_thread_id", group.RegistrationThreadID)
	}
}

func (c *Bot) removeObservedGroupMember(ctx context.Context, chatID, telegramUserID int64) {
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

func buildGroupReplyView(lang, key string, replyToMessageID int) sharedView {
	view := buildTextView(lang, key)
	view.opts = client.MessageOptions{ReplyToMessageID: replyToMessageID}
	return view
}

func buildGroupRegistrationPolicyPromptView(lang string, replyToMessageID int, chatID int64, threadID int, estimatedMembers int) sharedView {
	text := i18n.Translate(lang, msgGroupPolicyPrompt)
	if estimatedMembers > 0 {
		text = joinNonEmptySections(
			textSection{text: text},
			textSection{text: fmt.Sprintf(i18n.Translate(lang, msgGroupPolicyExistingMembers), estimatedMembers)},
		)
	}

	return sharedView{
		text: text,
		opts: client.MessageOptions{
			ReplyToMessageID: replyToMessageID,
			Markup: telegoutil.InlineKeyboard(
				telegoutil.InlineKeyboardRow(telegramui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyObserve), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyObserve), "5253959125838090076")),
				telegoutil.InlineKeyboardRow(telegramui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyObserveWarn), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyObserveWarn), "5253959125838090076")),
				telegoutil.InlineKeyboardRow(telegramui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyKick), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyKick), "5258318620722733379").WithStyle("danger")),
				telegoutil.InlineKeyboardRow(telegramui.IconCallbackButton(i18n.Translate(lang, btnGroupPolicyGrace), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyGraceWeek), "5258123337149717894").WithStyle("danger")),
			),
		},
	}
}

func buildGroupSettingWarningsView(lang string, replyToMessageID int, issues []string) sharedView {
	return sharedView{
		text: formatGroupSettingWarnings(lang, issues),
		opts: client.MessageOptions{
			ReplyToMessageID: replyToMessageID,
		},
	}
}

func buildGroupSetupPermissionsView(lang string) sharedView {
	return sharedView{text: i18n.Translate(lang, msgGroupSetupPermissions)}
}

func buildGroupPermissionsBlockedView(lang string, replyToMessageID int) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgGroupPermissionsBlocked),
		opts: client.MessageOptions{ReplyToMessageID: replyToMessageID},
	}
}

func buildGroupCompromisedView(lang, missing string) sharedView {
	return sharedView{text: fmt.Sprintf(i18n.Translate(lang, msgGroupCompromised), missing)}
}

func buildGroupCompromisedOwnerView(lang, groupName, missing string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgGroupCompromisedOwnerDM), html.EscapeString(groupName), missing),
	}
}

func formatMissingRequiredPermissions(lang string, caps membershipCapabilitySnapshot) string {
	missing := caps.missingRequiredPermissions()
	lines := make([]string, 0, len(missing))
	for _, item := range missing {
		switch item {
		case "admin":
			lines = append(lines, "• "+permissionShortLabel(lang, msgGroupWarnBotNotAdmin))
		case "invite":
			lines = append(lines, "• "+permissionShortLabel(lang, msgGroupWarnBotNoInvite))
		case "restrict":
			lines = append(lines, "• "+permissionShortLabel(lang, msgGroupWarnBotNoRestrict))
		}
	}
	return strings.Join(lines, "\n")
}

func permissionShortLabel(lang, key string) string {
	text := i18n.Translate(lang, key)
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "• ")
	text = strings.TrimPrefix(text, "•")
	return text
}

func buildGroupUntrackedJoinWarningView(lang string, memberUserID int64, memberLabel string) sharedView {
	memberLabel = strings.TrimSpace(memberLabel)
	if memberLabel == "" {
		memberLabel = i18n.Translate(lang, msgUserGenericName)
	}
	return sharedView{text: fmt.Sprintf(i18n.Translate(lang, msgGroupUntrackedJoinWarning), telegramUserMentionHTML(memberUserID, memberLabel))}
}

func telegramUserMentionHTML(userID int64, label string) string {
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, userID, html.EscapeString(label))
}

func buildGroupUnregisterPromptView(lang string, replyToMessageID int, chatID int64) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgGroupUnregisterPrompt),
		opts: client.MessageOptions{
			ReplyToMessageID: replyToMessageID,
			Markup: telegoutil.InlineKeyboard(
				telegoutil.InlineKeyboardRow(telegramui.CallbackButton(i18n.Translate(lang, btnResetKeepMembers), groupUnregisterExecuteCallback(chatID, core.CreatorResetKeepMembers))),
				telegoutil.InlineKeyboardRow(telegramui.IconCallbackButton(i18n.Translate(lang, btnResetKickTrackedMembers), groupUnregisterExecuteCallback(chatID, core.CreatorResetKickTrackedMembers), "5258318620722733379").WithStyle("danger")),
			),
		},
	}
}

func buildGroupUnregisteredView(lang string, replyToMessageID int) sharedView {
	view := buildTextView(lang, msgGroupUnregistered)
	view.opts.ReplyToMessageID = replyToMessageID
	return view
}

func formatGroupPolicyLine(lang string, policy core.GroupPolicy) string {
	switch policy {
	case core.GroupPolicyObserve:
		return i18n.Translate(lang, msgGroupPolicyObserveLine)
	case core.GroupPolicyObserveWarn:
		return i18n.Translate(lang, msgGroupPolicyObserveWarnLine)
	case core.GroupPolicyKick:
		return i18n.Translate(lang, msgGroupPolicyKickLine)
	case core.GroupPolicyGraceWeek:
		return i18n.Translate(lang, msgGroupPolicyGraceLine)
	default:
		return i18n.Translate(lang, msgGroupPolicyObserveLine)
	}
}

func groupMessageThreadID(msg telego.Message) int {
	if msg.IsTopicMessage && msg.MessageThreadID > 0 {
		return msg.MessageThreadID
	}
	return 0
}
