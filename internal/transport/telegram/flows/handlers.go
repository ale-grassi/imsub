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
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	"github.com/mymmrac/telego/telegoutil"
)

// onUnknownMessage replies with a generic help message and the main menu
// when the bot receives an unrecognized message or command.
func (c *Controller) onUnknownMessage(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	key := msgCmdHelp
	if message.From != nil {
		var err error
		key, err = c.helpMessageKey(ctx, message.From.ID)
		if err != nil {
			c.log().Warn("Resolve help message key failed", "telegram_user_id", message.From.ID, "error", err)
			key = msgCmdHelp
		}
	}
	c.sendMsg(ctx, message.Chat.ID, i18n.Translate(lang, key), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
	return nil
}

// helpMessageKey selects the help text variant for the user's linked account state.
func (c *Controller) helpMessageKey(ctx context.Context, telegramUserID int64) (string, error) {
	_, hasViewer, err := c.viewerSvc.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		return "", fmt.Errorf("load viewer identity for help message: %w", err)
	}
	_, hasCreator, err := c.creatorSvc.LoadOwnedCreator(ctx, telegramUserID)
	if err != nil {
		return "", fmt.Errorf("load owned creator for help message: %w", err)
	}

	switch {
	case hasViewer && hasCreator:
		return msgCmdHelpBoth, nil
	case hasCreator:
		return msgCmdHelpCreator, nil
	case hasViewer:
		return msgCmdHelpViewer, nil
	default:
		return msgCmdHelp, nil
	}
}

// onChatJoinRequest approves or declines a group join request based on the
// invite link name. The link must match the pattern "imsub-{userID}-{name}";
// requests from mismatched user IDs are declined.
func (c *Controller) onChatJoinRequest(ctx *tghandler.Context, req telego.ChatJoinRequest) error {
	if req.InviteLink == nil || !strings.HasPrefix(req.InviteLink.Name, "imsub-") {
		return nil
	}

	parts := strings.SplitN(req.InviteLink.Name, "-", 3)
	if len(parts) < 3 {
		return nil
	}
	linkUserID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || linkUserID != req.From.ID {
		c.log().Info("join request denied", "link_user", parts[1], "requester_id", req.From.ID, "chat_id", req.Chat.ID)
		if waitErr := c.tgLimiter.Wait(ctx, req.Chat.ID); waitErr != nil {
			c.log().Warn("Decline join request rate limit wait failed", "error", waitErr)
			return nil
		}
		if err := c.tg.DeclineChatJoinRequest(ctx, &telego.DeclineChatJoinRequestParams{
			ChatID: telegoutil.ID(req.Chat.ID),
			UserID: req.From.ID,
		}); err != nil {
			c.log().Warn("Decline join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
		}
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, req.Chat.ID); waitErr != nil {
		c.log().Warn("Approve join request rate limit wait failed", "error", waitErr)
		return nil
	}
	err = c.tg.ApproveChatJoinRequest(ctx, &telego.ApproveChatJoinRequestParams{
		ChatID: telegoutil.ID(req.Chat.ID),
		UserID: req.From.ID,
	})
	if err != nil {
		c.log().Warn("Approve join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
	}
	return nil
}

// onStartCommand handles /start by initiating the viewer flow.
func (c *Controller) onStartCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleViewerStartForUser(ctx, msg.From.ID, 0, lang, msg.From.FirstName)
	return nil
}

// onCreatorCommand handles /creator by initiating the creator registration
// or status flow.
func (c *Controller) onCreatorCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleCreatorRegistrationStart(ctx, msg.From.ID, 0, lang)
	return nil
}

// onRegisterGroup handles /registergroup by binding the current Telegram group
// to the caller's creator account. The caller must be a group admin and have
// a linked creator record.
func (c *Controller) onRegisterGroup(ctx *tghandler.Context, msg telego.Message) error {
	if msg.From == nil {
		return nil
	}
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	replyOpts := &client.MessageOptions{ReplyToMessageID: msg.MessageID}

	if msg.Chat.Type == telego.ChatTypePrivate {
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, msgGroupNotGroup), replyOpts)
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, msg.Chat.ID); waitErr != nil {
		c.log().Warn("Get chat member rate limit wait failed", "error", waitErr)
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, msgGroupNotAdmin), replyOpts)
		return nil
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(msg.Chat.ID),
		UserID: msg.From.ID,
	})
	isAdmin := err == nil && IsAdmin(member)

	matched, ok, err := c.store.OwnedCreatorForUser(ctx, msg.From.ID)
	if err != nil {
		c.log().Warn("OnRegisterGroup getOwnedCreator failed", "error", err)
		return nil
	}

	// Silently ignore users who are neither admin nor have a creator account.
	if !isAdmin && !ok {
		return nil
	}
	if !isAdmin {
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, msgGroupNotAdmin), replyOpts)
		return nil
	}
	if !ok {
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, msgGroupNotCreator), replyOpts)
		return nil
	}

	// Check if another creator already owns this group.
	otherName, taken := c.groupTakenByOtherCreator(ctx, msg.Chat.ID, matched.ID)
	if taken {
		takenText := fmt.Sprintf(i18n.Translate(lang, msgGroupTakenByOther), html.EscapeString(otherName))
		c.sendMsg(ctx, msg.Chat.ID, takenText, &client.MessageOptions{
			ReplyToMessageID: msg.MessageID,
			ParseMode:        telego.ModeHTML,
		})
		return nil
	}

	// Scenario 1: this group is already linked to this creator.
	if matched.GroupChatID == msg.Chat.ID {
		alreadyText := fmt.Sprintf(i18n.Translate(lang, msgGroupAlreadyLinked), html.EscapeString(matched.Name))
		checking := i18n.Translate(lang, msgGroupCheckingSettings)
		groupMsgID := c.sendMsg(ctx, msg.Chat.ID, alreadyText+"\n\n"+checking, &client.MessageOptions{
			ReplyToMessageID: msg.MessageID,
			ParseMode:        telego.ModeHTML,
		})
		go c.sendPostRegistrationSettingsCheck(context.WithoutCancel(ctx), msg.Chat.ID, groupMsgID, lang, alreadyText)
		return nil
	}

	// Scenario 2: creator already has a different group linked.
	if matched.GroupChatID != 0 {
		differentText := fmt.Sprintf(
			i18n.Translate(lang, msgGroupDifferentLinked),
			html.EscapeString(matched.Name),
			html.EscapeString(matched.GroupName),
		)
		c.sendMsg(ctx, msg.Chat.ID, differentText, &client.MessageOptions{
			ReplyToMessageID: msg.MessageID,
			ParseMode:        telego.ModeHTML,
		})
		return nil
	}

	// First-time registration.
	groupName := msg.Chat.Title
	if err := c.store.UpdateCreatorGroup(ctx, matched.ID, msg.Chat.ID, groupName); err != nil {
		c.log().Warn("UpdateCreatorGroup failed", "error", err)
		return nil
	}
	matched.GroupChatID = msg.Chat.ID
	matched.GroupName = groupName
	// Activation runs asynchronously to keep the command response fast.
	// The goroutine terminates when either:
	//  - all operations complete, or
	//  - the 3-minute timeout in activateCreatorOnFirstGroupRegistration fires.
	// context.WithoutCancel is used so the work survives the parent
	// request context being canceled.
	go c.activateCreatorOnFirstGroupRegistration(context.WithoutCancel(ctx), matched, msg.Chat.ID, lang)

	successText := fmt.Sprintf(i18n.Translate(lang, msgGroupRegistered), html.EscapeString(matched.Name))
	checking := i18n.Translate(lang, msgGroupCheckingSettings)
	groupMsgID := c.sendMsg(ctx, msg.Chat.ID, successText+"\n\n"+checking, &client.MessageOptions{
		ReplyToMessageID: msg.MessageID,
		ParseMode:        telego.ModeHTML,
	})

	// Check group settings asynchronously, then edit the group message
	// and send the creator DM with warnings appended.
	go c.sendPostRegistrationMessages(context.WithoutCancel(ctx), postRegistrationMessageOptions{
		groupChatID:   msg.Chat.ID,
		groupMsgID:    groupMsgID,
		ownerUserID:   msg.From.ID,
		groupName:     groupName,
		creatorName:   matched.Name,
		lang:          lang,
		groupBaseText: successText,
	})

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
	if err := c.eventSubSvc.EnsureEventSubForCreators(ctx, []core.Creator{creator}); err != nil {
		c.log().Warn("EnsureEventSubForCreators failed after first group registration", "creator_id", creator.ID, "error", err)
		c.sendMsg(baseCtx, groupChatID, i18n.Translate(lang, msgCreatorEventSubFail), nil)
		return
	}
	count, err := c.eventSubSvc.DumpCurrentSubscribers(ctx, creator)
	if err != nil {
		c.log().Warn("DumpCurrentSubscribers failed after first group registration", "creator_id", creator.ID, "error", err)
		return
	}
	c.log().Info("creator activated on first group registration", "creator_id", creator.ID, "group_chat_id", groupChatID, "subscriber_count", count)
}

// groupTakenByOtherCreator checks if any other creator already has this group linked.
// Returns the other creator's name and true if taken, or empty and false otherwise.
func (c *Controller) groupTakenByOtherCreator(ctx context.Context, groupChatID int64, currentCreatorID string) (string, bool) {
	creators, err := c.store.ListActiveCreators(ctx)
	if err != nil {
		c.log().Warn("ListActiveCreators for group taken check failed", "error", err)
		return "", false
	}
	for _, cr := range creators {
		if cr.GroupChatID == groupChatID && cr.ID != currentCreatorID {
			return cr.Name, true
		}
	}
	return "", false
}

// sendPostRegistrationSettingsCheck runs group settings checks and edits the
// group message to append warnings or an "all good" status. No DM is sent.
func (c *Controller) sendPostRegistrationSettingsCheck(ctx context.Context, groupChatID int64, groupMsgID int, lang, groupBaseText string) {
	warnings := c.checkGroupSettings(ctx, groupChatID, lang)
	var settingsResult string
	if len(warnings) > 0 {
		settingsResult = formatGroupSettingWarnings(lang, warnings)
	} else {
		settingsResult = i18n.Translate(lang, msgGroupSettingsOK)
	}
	if groupMsgID != 0 {
		c.reply(ctx, groupChatID, groupMsgID, groupBaseText+"\n\n"+settingsResult, &client.MessageOptions{ParseMode: telego.ModeHTML})
	}
}

// sendPostRegistrationMessages streams a draft DM to the creator while
// checking group settings, then finalises the DM and edits the group message.
func (c *Controller) sendPostRegistrationMessages(ctx context.Context, opts postRegistrationMessageOptions) {
	const draftID = 1

	dmBase := fmt.Sprintf(
		i18n.Translate(opts.lang, msgGroupRegisteredDM),
		html.EscapeString(opts.groupName),
		html.EscapeString(opts.creatorName),
	)

	// Stream partial DM with "checking..." status.
	checking := i18n.Translate(opts.lang, msgGroupCheckingSettings)
	c.sendDraft(ctx, opts.ownerUserID, draftID, dmBase+"\n\n"+checking, &client.MessageOptions{ParseMode: telego.ModeHTML})

	warnings := c.checkGroupSettings(ctx, opts.groupChatID, opts.lang)

	var settingsResult string
	if len(warnings) > 0 {
		settingsResult = formatGroupSettingWarnings(opts.lang, warnings)
	} else {
		settingsResult = i18n.Translate(opts.lang, msgGroupSettingsOK)
	}

	dmText := dmBase + "\n\n" + settingsResult
	// Update the draft with the result before sending the final message.
	c.sendDraft(ctx, opts.ownerUserID, draftID, dmText, &client.MessageOptions{ParseMode: telego.ModeHTML})

	// Send the final message which replaces the draft.
	c.sendMsg(ctx, opts.ownerUserID, dmText, &client.MessageOptions{ParseMode: telego.ModeHTML})

	if opts.groupMsgID != 0 {
		c.reply(ctx, opts.groupChatID, opts.groupMsgID, opts.groupBaseText+"\n\n"+settingsResult, &client.MessageOptions{ParseMode: telego.ModeHTML})
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

// checkGroupSettings fetches full chat info and returns warnings for any
// settings that would undermine subscription-gated access.
func (c *Controller) checkGroupSettings(ctx context.Context, chatID int64, lang string) []string {
	if waitErr := c.tgLimiter.Wait(ctx, chatID); waitErr != nil {
		c.log().Warn("GetChat rate limit wait failed", "error", waitErr)
		return nil
	}
	chat, err := c.tg.GetChat(ctx, &telego.GetChatParams{
		ChatID: telegoutil.ID(chatID),
	})
	if err != nil {
		c.log().Warn("GetChat for group settings check failed", "chat_id", chatID, "error", err)
		return nil
	}

	var issues []string
	if chat.Username != "" || len(chat.ActiveUsernames) > 0 {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnPublic))
	}
	if !chat.JoinByRequest {
		issues = append(issues, i18n.Translate(lang, msgGroupWarnJoinByReq))
	}
	if untrackedCount := c.countUntrackedMembers(ctx, chatID); untrackedCount > 0 {
		issues = append(issues, fmt.Sprintf(i18n.Translate(lang, msgGroupWarnUntrackedUsers), untrackedCount))
	}
	return issues
}

func formatGroupSettingWarnings(lang string, issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	return i18n.Translate(lang, msgGroupWarnSettingsIntro) + "\n" + strings.Join(issues, "\n")
}

// countUntrackedMembers returns the number of group members that are neither
// admins nor bots. These users joined before the bot started managing access
// and are not tracked.
func (c *Controller) countUntrackedMembers(ctx context.Context, chatID int64) int {
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
	// The admin list already includes creator/admin accounts and admin bots.
	privileged := len(admins)
	untracked := *total - privileged
	if untracked < 0 {
		return 0
	}
	return untracked
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

// onResetCommand handles /reset by showing the reset confirmation prompt.
func (c *Controller) onResetCommand(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	c.handleResetPrompt(ctx, message.From.ID, 0, lang)
	return nil
}
