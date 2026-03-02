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
	telegoutil "github.com/mymmrac/telego/telegoutil"
)

// onUnknownMessage replies with a generic help message and the main menu
// when the bot receives an unrecognized message or command.
func (c *Controller) onUnknownMessage(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	key := "cmd_help"
	if message.From != nil {
		var err error
		key, err = c.helpMessageKey(ctx, message.From.ID)
		if err != nil {
			c.log().Warn("resolve help message key failed", "telegram_user_id", message.From.ID, "error", err)
			key = "cmd_help"
		}
	}
	c.sendMsg(ctx, message.Chat.ID, i18n.Translate(lang, key), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
	return nil
}

// helpMessageKey selects the help text variant for the user's linked account state.
func (c *Controller) helpMessageKey(ctx context.Context, telegramUserID int64) (string, error) {
	_, hasViewer, err := c.viewerSvc.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		return "", err
	}
	_, hasCreator, err := c.creatorSvc.LoadOwnedCreator(ctx, telegramUserID)
	if err != nil {
		return "", err
	}

	switch {
	case hasViewer && hasCreator:
		return "cmd_help_both", nil
	case hasCreator:
		return "cmd_help_creator", nil
	case hasViewer:
		return "cmd_help_viewer", nil
	default:
		return "cmd_help", nil
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
			c.log().Warn("decline join request rate limit wait failed", "error", waitErr)
			return nil
		}
		if err := c.tg.DeclineChatJoinRequest(ctx, &telego.DeclineChatJoinRequestParams{
			ChatID: telegoutil.ID(req.Chat.ID),
			UserID: req.From.ID,
		}); err != nil {
			c.log().Warn("decline join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
		}
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, req.Chat.ID); waitErr != nil {
		c.log().Warn("approve join request rate limit wait failed", "error", waitErr)
		return nil
	}
	err = c.tg.ApproveChatJoinRequest(ctx, &telego.ApproveChatJoinRequestParams{
		ChatID: telegoutil.ID(req.Chat.ID),
		UserID: req.From.ID,
	})
	if err != nil {
		c.log().Warn("approve join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
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
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, "group_not_group"), replyOpts)
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, msg.Chat.ID); waitErr != nil {
		c.log().Warn("get chat member rate limit wait failed", "error", waitErr)
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, "group_not_admin"), replyOpts)
		return nil
	}
	member, err := c.tg.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: telegoutil.ID(msg.Chat.ID),
		UserID: msg.From.ID,
	})
	if err != nil || !IsAdmin(member) {
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, "group_not_admin"), replyOpts)
		return nil //nolint:nilerr // Ignore error to prevent telegram refetch
	}

	matched, ok, err := c.store.OwnedCreatorForUser(ctx, msg.From.ID)
	if err != nil {
		c.log().Warn("onRegisterGroup getOwnedCreator failed", "error", err)
		return nil
	}
	if !ok {
		c.sendMsg(ctx, msg.Chat.ID, i18n.Translate(lang, "group_not_creator"), replyOpts)
		return nil
	}

	groupName := msg.Chat.Title
	firstGroupRegistration := matched.GroupChatID == 0
	if err := c.store.UpdateCreatorGroup(ctx, matched.ID, msg.Chat.ID, groupName); err != nil {
		c.log().Warn("updateCreatorGroup failed", "error", err)
		return nil
	}
	if firstGroupRegistration {
		matched.GroupChatID = msg.Chat.ID
		matched.GroupName = groupName
		// Activation runs asynchronously to keep the command response fast.
		// The goroutine terminates when either:
		//  - all operations complete, or
		//  - the 3-minute timeout in activateCreatorOnFirstGroupRegistration fires.
		// context.WithoutCancel is used so the work survives the parent
		// request context being canceled.
		go c.activateCreatorOnFirstGroupRegistration(ctx, matched, msg.Chat.ID, lang)
	}
	successText := fmt.Sprintf(i18n.Translate(lang, "group_registered"), html.EscapeString(matched.Name))
	c.sendMsg(ctx, msg.Chat.ID, successText, &client.MessageOptions{
		ReplyToMessageID: msg.MessageID,
		ParseMode:        telego.ModeHTML,
	})
	return nil
}

func (c *Controller) activateCreatorOnFirstGroupRegistration(parent context.Context, creator core.Creator, groupChatID int64, lang string) {
	if parent == nil {
		c.log().Warn("activate creator called with nil context", "creator_id", creator.ID)
		return
	}
	baseCtx := context.WithoutCancel(parent)
	ctx, cancel := context.WithTimeout(baseCtx, 3*time.Minute)
	defer cancel()
	if err := c.eventSubSvc.EnsureEventSubForCreators(ctx, []core.Creator{creator}); err != nil {
		c.log().Warn("ensureEventSubForCreators failed after first group registration", "creator_id", creator.ID, "error", err)
		c.sendMsg(baseCtx, groupChatID, i18n.Translate(lang, "creator_eventsub_fail"), nil)
		return
	}
	count, err := c.eventSubSvc.DumpCurrentSubscribers(ctx, creator)
	if err != nil {
		c.log().Warn("dumpCurrentSubscribers failed after first group registration", "creator_id", creator.ID, "error", err)
		return
	}
	c.log().Info("creator activated on first group registration", "creator_id", creator.ID, "group_chat_id", groupChatID, "subscriber_count", count)
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
