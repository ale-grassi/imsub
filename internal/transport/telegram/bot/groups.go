package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	telegramui "imsub/internal/transport/telegram/ui"
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
func (c *Bot) onRegisterGroup(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	threadID := groupMessageThreadID(msg)

	if msg.Chat.Type == telego.ChatTypePrivate {
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
		c.log().Warn("OnRegisterGroup getOwnedCreator failed", "error", err)
		return nil
	}

	if !isAdmin && !ok {
		return nil
	}
	if !isAdmin {
		view := buildGroupReplyView(lang, msgGroupNotAdmin, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if !ok || c.groupRegistration == nil {
		view := buildGroupReplyView(lang, msgGroupNotCreator, msg.MessageID)
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}
	if eval := c.evaluateBotGroupCapabilities(ctx, msg.Chat.ID); len(eval.issues(lang)) > 0 {
		view := buildGroupSettingWarningsView(lang, msg.MessageID, eval.issues(lang))
		view.opts.MessageThreadID = threadID
		c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
		return nil
	}

	estimatedMembers := c.estimateExistingNonAdminMembers(ctx, msg.Chat.ID)
	view := buildGroupRegistrationPolicyPromptView(lang, msg.MessageID, msg.Chat.ID, threadID, estimatedMembers)
	view.opts.MessageThreadID = threadID
	c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
	return nil
}

func (c *Bot) handleGroupCallback(ctx context.Context, userID, chatID int64, chatTitle string, messageThreadID, editMsgID int, lang string, action callbackAction) string {
	if chatID == 0 || action.chatID == 0 || action.chatID != chatID {
		c.log().Warn("group callback chat mismatch", "telegram_user_id", userID, "callback_chat_id", action.chatID, "message_chat_id", chatID)
		return ""
	}
	if !c.userCanRegisterGroup(ctx, userID, chatID) {
		return i18n.Translate(lang, msgGroupNotAdmin)
	}
	_, ok, err := c.creatorStatus.LoadOwnedCreator(ctx, userID)
	if err != nil {
		c.log().Warn("group callback getOwnedCreator failed", "chat_id", chatID, "owner_telegram_id", userID, "error", err)
		return ""
	}
	if !ok {
		return i18n.Translate(lang, msgGroupNotCreator)
	}
	switch action.verb {
	case callbackVerbPick:
		if action.policy == "" || c.groupRegistration == nil {
			c.log().Warn("unsupported group registration callback action", "telegram_user_id", userID, "verb", action.verb, "policy", action.policy, "chat_id", action.chatID)
			return ""
		}
		if eval := c.evaluateBotGroupCapabilities(ctx, chatID); len(eval.issues(lang)) > 0 {
			return formatGroupSettingWarnings(lang, eval.issues(lang))
		}
		regRes, err := c.groupRegistration.RegisterGroup(ctx, userID, chatID, chatTitle, action.policy, action.threadID)
		if err != nil {
			c.log().Warn("RegisterGroup from callback failed", "chat_id", chatID, "owner_telegram_id", userID, "policy", action.policy, "error", err)
			return ""
		}
		view, ok := buildGroupRegistrationView(lang, editMsgID, regRes)
		if !ok {
			c.log().Warn("unsupported group registration outcome from callback", "chat_id", chatID, "outcome", regRes.Outcome)
			return ""
		}
		c.reply(ctx, chatID, editMsgID, view.text, &view.opts)
		if view.dispatchFollowUp {
			c.dispatchGroupRegistrationFollowUp(ctx, telego.Message{
				MessageID:       editMsgID,
				MessageThreadID: messageThreadID,
				IsTopicMessage:  messageThreadID > 0,
				Chat: telego.Chat{
					ID:    chatID,
					Type:  telego.ChatTypeSupergroup,
					Title: chatTitle,
				},
				From: &telego.User{ID: userID, LanguageCode: lang},
			}, lang, regRes, view, editMsgID, messageThreadID)
		}
		return ""
	case callbackVerbExecute:
		if action.resetAction == "" || c.groupUnregistration == nil {
			c.log().Warn("unsupported group unregister callback action", "telegram_user_id", userID, "verb", action.verb, "action", action.resetAction, "chat_id", action.chatID)
			return ""
		}
		res, err := c.groupUnregistration.UnregisterGroup(ctx, userID, chatID, action.resetAction)
		if err != nil {
			c.log().Warn("UnregisterGroup from group callback failed", "chat_id", chatID, "owner_telegram_id", userID, "action", action.resetAction, "error", err)
			return ""
		}
		switch res.Outcome {
		case usecase.UnregisterGroupOutcomeNotManaged:
			return ""
		case usecase.UnregisterGroupOutcomeNotOwner:
			return i18n.Translate(lang, msgGroupUnregisterNotOwner)
		case usecase.UnregisterGroupOutcomeUnregistered, usecase.UnregisterGroupOutcomeUnregisteredCleanupLag:
		default:
			c.log().Warn("unsupported group unregistration outcome from callback", "chat_id", chatID, "outcome", res.Outcome)
			return ""
		}
		view := buildGroupUnregisteredView(lang, editMsgID, res)
		c.reply(ctx, chatID, editMsgID, view.text, &view.opts)
		return ""
	case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect, callbackVerbOpen, callbackVerbBack, callbackVerbMenu, callbackVerbCancel:
		c.log().Warn("known but unsupported group callback verb", "telegram_user_id", userID, "verb", action.verb, "chat_id", action.chatID)
		return ""
	default:
		c.log().Warn("unknown group callback action", "telegram_user_id", userID, "verb", action.verb, "chat_id", action.chatID)
		return ""
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

// onUnregisterCommand handles /unregistergroup by unbinding the current Telegram group.
func (c *Bot) onUnregisterCommand(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	threadID := groupMessageThreadID(msg)
	view := buildGroupReplyView(lang, msgGroupUnregisterNotOwner, msg.MessageID)
	view.opts.MessageThreadID = threadID

	if msg.Chat.Type == telego.ChatTypePrivate {
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

func (c *Bot) activateCreatorOnFirstGroupRegistration(parent context.Context, creator core.Creator, groupChatID int64, messageThreadID int, lang string) {
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
		view := buildTextView(lang, msgCreatorEventSubFail)
		view.opts.MessageThreadID = messageThreadID
		c.sendMsg(baseCtx, groupChatID, view.text, &view.opts)
		return
	}
	c.log().Info("creator activated on first group registration", "creator_id", creator.ID, "group_chat_id", groupChatID, "subscriber_count", res.SubscriberCount)
}

func (c *Bot) onMyChatMemberUpdated(ctx *tghandler.Context, update telego.ChatMemberUpdated) error {
	if update.Chat.Type == telego.ChatTypePrivate {
		return nil
	}

	oldStatus := ""
	if update.OldChatMember != nil {
		oldStatus = update.OldChatMember.MemberStatus()
	}
	newStatus := update.NewChatMember.MemberStatus()
	if oldStatus == newStatus {
		return nil
	}

	switch newStatus {
	case telego.MemberStatusMember, telego.MemberStatusAdministrator, telego.MemberStatusCreator:
		lang := i18n.NormalizeLanguage(update.From.LanguageCode)
		view := buildGroupBotStatusChangedView(lang)
		groupMsgID := c.sendMsg(ctx, update.Chat.ID, view.text, &view.opts)
		if groupMsgID != 0 {
			c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
				c.sendPostRegistrationSettingsCheck(bg, update.Chat.ID, groupMsgID, 0, lang, view.text)
			})
		}
	case telego.MemberStatusLeft, telego.MemberStatusBanned:
		c.handleBotRemovedFromManagedGroup(ctx, update.Chat.ID, newStatus)
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
	c.notifyOwnerGroupAutoUnregistered(ctx, creator, res.Group, res.CleanupFailed)
}

func (c *Bot) notifyOwnerGroupAutoUnregistered(ctx context.Context, creator core.Creator, group core.ManagedGroup, cleanupLag bool) {
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, creator.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = identity.Language
	}
	view := buildGroupBotRemovedOwnerView(lang, group.GroupName, cleanupLag)
	if messageID := c.sendMsg(ctx, creator.OwnerTelegramID, view.text, &view.opts); messageID == 0 {
		c.log().Warn("send auto-unregister owner notification failed", "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "chat_id", group.ChatID)
	}
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
		c.observeGroupMember(ctx, group, memberUser.ID, "chat_member", status)
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
	c.observeGroupMember(ctx, group, msg.From.ID, "message", telego.MemberStatusMember)
	return nil
}

func (c *Bot) sendPostRegistrationSettingsCheck(ctx context.Context, groupChatID int64, groupMsgID int, messageThreadID int, lang, groupBaseText string) {
	warnings := c.evaluateGroupSettings(ctx, groupChatID).issues(lang)
	if groupMsgID != 0 {
		view := buildGroupSettingsCheckResultView(lang, groupBaseText, warnings)
		view.opts.MessageThreadID = messageThreadID
		c.reply(ctx, groupChatID, groupMsgID, view.text, &view.opts)
	}
}

func (c *Bot) sendPostRegistrationMessages(ctx context.Context, opts postRegistrationMessageOptions) {
	const draftID = 1

	rendered := renderPostRegistrationCopy(postRegistrationCopyInput{
		lang:          opts.lang,
		groupName:     opts.groupName,
		creatorName:   opts.creatorName,
		groupBaseText: opts.groupBaseText,
	}, nil)

	c.sendDraft(ctx, opts.ownerUserID, draftID, rendered.draftDM, &client.MessageOptions{})

	warnings := c.evaluateGroupSettings(ctx, opts.groupChatID).issues(opts.lang)
	rendered = renderPostRegistrationCopy(postRegistrationCopyInput{
		lang:          opts.lang,
		groupName:     opts.groupName,
		creatorName:   opts.creatorName,
		groupBaseText: opts.groupBaseText,
	}, warnings)
	c.sendDraft(ctx, opts.ownerUserID, draftID, rendered.finalDM, &client.MessageOptions{})
	c.sendMsg(ctx, opts.ownerUserID, rendered.finalDM, &client.MessageOptions{})

	if opts.groupMsgID != 0 {
		c.reply(ctx, opts.groupChatID, opts.groupMsgID, rendered.groupMessage, &client.MessageOptions{
			MessageThreadID: opts.messageThreadID,
		})
	}
}

type postRegistrationMessageOptions struct {
	groupChatID     int64
	groupMsgID      int
	messageThreadID int
	ownerUserID     int64
	groupName       string
	creatorName     string
	lang            string
	groupBaseText   string
}

type groupRegistrationView struct {
	text             string
	opts             client.MessageOptions
	groupBaseText    string
	dispatchFollowUp bool
}

func buildGroupRegistrationView(lang string, replyToMessageID int, regRes usecase.RegisterGroupResult) (groupRegistrationView, bool) {
	view := groupRegistrationView{opts: client.MessageOptions{ReplyToMessageID: replyToMessageID}}
	policyLine := formatGroupPolicyLine(lang, regRes.ExistingGroup.Policy)

	switch regRes.Outcome {
	case usecase.RegisterGroupOutcomeNotCreator:
		view.text = i18n.Translate(lang, msgGroupNotCreator)
	case usecase.RegisterGroupOutcomeTakenByOther:
		view.text = fmt.Sprintf(i18n.Translate(lang, msgGroupTakenByOther), html.EscapeString(regRes.OtherCreatorName))
	case usecase.RegisterGroupOutcomeAlreadyLinked:
		view.groupBaseText = fmt.Sprintf(i18n.Translate(lang, msgGroupAlreadyLinked), html.EscapeString(regRes.Creator.TwitchLogin))
		view.text = joinNonEmptySections(
			textSection{text: view.groupBaseText},
			textSection{text: policyLine},
			textSection{text: i18n.Translate(lang, msgGroupCheckingSettings)},
		)
		view.dispatchFollowUp = true
	case usecase.RegisterGroupOutcomeRegistered:
		view.groupBaseText = fmt.Sprintf(i18n.Translate(lang, msgGroupRegistered), html.EscapeString(regRes.Creator.TwitchLogin))
		view.text = joinNonEmptySections(
			textSection{text: view.groupBaseText},
			textSection{text: policyLine},
			textSection{text: i18n.Translate(lang, msgGroupCheckingSettings)},
		)
		view.dispatchFollowUp = true
	default:
		return groupRegistrationView{}, false
	}

	return view, true
}

func (c *Bot) dispatchGroupRegistrationFollowUp(ctx context.Context, msg telego.Message, lang string, regRes usecase.RegisterGroupResult, view groupRegistrationView, groupMsgID int, messageThreadID int) {
	if !regRes.FollowUp.NeedsActivation && !regRes.FollowUp.NeedsSettingsCheck {
		return
	}
	if regRes.FollowUp.NeedsActivation && c.creatorActivation != nil {
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			c.activateCreatorOnFirstGroupRegistration(bg, regRes.Creator, msg.Chat.ID, messageThreadID, lang)
		})
	}
	if !regRes.FollowUp.NeedsSettingsCheck {
		return
	}
	if regRes.FollowUp.NotifyOwner {
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			c.sendPostRegistrationMessages(bg, postRegistrationMessageOptions{
				groupChatID:     msg.Chat.ID,
				groupMsgID:      groupMsgID,
				messageThreadID: messageThreadID,
				ownerUserID:     msg.From.ID,
				groupName:       msg.Chat.Title,
				creatorName:     regRes.Creator.TwitchLogin,
				lang:            lang,
				groupBaseText:   view.groupBaseText,
			})
		})
		return
	}
	c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
		c.sendPostRegistrationSettingsCheck(bg, msg.Chat.ID, groupMsgID, messageThreadID, lang, view.groupBaseText)
	})
}

type postRegistrationCopyInput struct {
	lang          string
	groupName     string
	creatorName   string
	groupBaseText string
}

type postRegistrationRendered struct {
	draftDM      string
	finalDM      string
	groupMessage string
}

func formatGroupSettingWarnings(lang string, issues []string) string {
	return renderWarningBlock(i18n.Translate(lang, msgGroupWarnSettingsIntro), issues)
}

func formatGroupSettingsResult(lang string, issues []string) string {
	if len(issues) > 0 {
		return formatGroupSettingWarnings(lang, issues)
	}
	return i18n.Translate(lang, msgGroupSettingsOK)
}

func renderPostRegistrationCopy(in postRegistrationCopyInput, issues []string) postRegistrationRendered {
	settingsResult := formatGroupSettingsResult(in.lang, issues)
	dmBase := fmt.Sprintf(
		i18n.Translate(in.lang, msgGroupRegisteredDM),
		html.EscapeString(in.groupName),
		html.EscapeString(in.creatorName),
	)

	return postRegistrationRendered{
		draftDM: joinNonEmptySections(
			textSection{text: dmBase},
			textSection{text: i18n.Translate(in.lang, msgGroupCheckingSettings)},
		),
		finalDM: joinNonEmptySections(
			textSection{text: dmBase},
			textSection{text: settingsResult},
		),
		groupMessage: joinNonEmptySections(
			textSection{text: in.groupBaseText},
			textSection{text: settingsResult},
		),
	}
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

func (c *Bot) observeGroupMember(ctx context.Context, group core.ManagedGroup, telegramUserID int64, source, status string) {
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
	if err := c.store.UpsertUntrackedGroupMember(ctx, group.ChatID, telegramUserID, source, status, now); err != nil {
		c.log().Warn("UpsertUntrackedGroupMember failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "source", source, "error", err)
		return
	}
	if group.Policy == core.GroupPolicyObserveWarn && source == "chat_member" {
		c.sendGroupUntrackedJoinWarning(ctx, group)
		return
	}
	if group.Policy != core.GroupPolicyKick {
		return
	}
	if err := c.KickFromGroup(ctx, group.ChatID, telegramUserID); err != nil {
		c.log().Warn("kick unverified group member failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "source", source, "error", err)
		return
	}
	if err := c.store.RemoveUntrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
		c.log().Warn("RemoveUntrackedGroupMember after kick failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
	}
}

func (c *Bot) sendGroupUntrackedJoinWarning(ctx context.Context, group core.ManagedGroup) {
	lang := "en"
	creator, ok, err := c.store.Creator(ctx, group.CreatorID)
	if err != nil {
		c.log().Warn("load creator for untracked join warning failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "error", err)
	} else if ok {
		if identity, found, identityErr := c.store.UserIdentity(ctx, creator.OwnerTelegramID); identityErr != nil {
			c.log().Warn("load owner identity for untracked join warning failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "owner_telegram_id", creator.OwnerTelegramID, "error", identityErr)
		} else if found && identity.Language != "" {
			lang = i18n.NormalizeLanguage(identity.Language)
		}
	}

	view := buildGroupUntrackedJoinWarningView(lang)
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
				telegoutil.InlineKeyboardRow(telegramui.CallbackButton(i18n.Translate(lang, btnGroupPolicyObserve), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyObserve))),
				telegoutil.InlineKeyboardRow(telegramui.CallbackButton(i18n.Translate(lang, btnGroupPolicyObserveWarn), groupRegisterPolicyCallback(chatID, threadID, core.GroupPolicyObserveWarn))),
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

func buildGroupSettingsCheckResultView(lang, groupBaseText string, issues []string) sharedView {
	return sharedView{
		text: groupBaseText + "\n\n" + formatGroupSettingsResult(lang, issues),
		opts: client.MessageOptions{},
	}
}

func buildGroupBotStatusChangedView(lang string) sharedView {
	return buildTextView(lang, msgGroupBotStatusChanged)
}

func buildGroupUntrackedJoinWarningView(lang string) sharedView {
	return buildTextView(lang, msgGroupUntrackedJoinWarning)
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

func buildGroupUnregisteredView(lang string, replyToMessageID int, res usecase.UnregisterGroupResult) sharedView {
	key := msgGroupUnregistered
	text := i18n.Translate(lang, key)
	if res.MemberAction == core.CreatorResetKickTrackedMembers {
		key = msgGroupUnregisteredKicked
		if res.TargetedMembershipCount > 0 && res.KickFailureCount == res.TargetedMembershipCount {
			key = msgGroupUnregisteredKickAllFailed
		}
		text = fmt.Sprintf(i18n.Translate(lang, key), res.TargetedMembershipCount, res.KickFailureCount)
	}
	view := sharedView{text: text}
	if key == msgGroupUnregistered {
		view = buildTextView(lang, key)
	}
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
