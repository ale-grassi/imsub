package flows

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/core"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/groupops"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
)

// --- Simplified message transport ---

// sendMsg sends a Telegram message and returns its message ID, or 0 on
// failure. Pass nil opts for plain text.
func (c *Controller) sendMsg(ctx context.Context, chatID int64, text string, opts *client.MessageOptions) int {
	return c.tgClient().Send(ctx, chatID, text, opts)
}

// reply edits the message if messageID != 0, otherwise sends a new one.
func (c *Controller) reply(ctx context.Context, chatID int64, messageID int, text string, opts *client.MessageOptions) {
	c.tgClient().Reply(ctx, chatID, messageID, text, opts)
}

func (c *Controller) sendDraft(ctx context.Context, chatID int64, draftID int, text, parseMode string) {
	c.tgClient().SendDraft(ctx, chatID, draftID, text, parseMode)
}

func (c *Controller) deleteMessage(ctx context.Context, chatID int64, messageID int) {
	c.tgClient().Delete(ctx, chatID, messageID)
}

// --- Group operations ---

// createInviteLink creates a single-use, join-request invite link for
// groupChatID that expires in 10 minutes.
func (c *Controller) createInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	link, err := c.tgGroupOps().CreateInviteLink(ctx, groupChatID, telegramUserID, name)
	if err != nil {
		return "", fmt.Errorf("create invite link from group ops: %w", err)
	}
	return link, nil
}

// kickDisplacedUser removes telegramUserID from every active creator's group.
// A "displaced user" is the old Telegram user previously linked to that Twitch
// account. Used when a Twitch account is re-linked to a different Telegram user.
func (c *Controller) kickDisplacedUser(ctx context.Context, telegramUserID int64) {
	c.tgGroupOps().KickDisplacedUser(ctx, telegramUserID)
}

// isGroupMember reports whether telegramUserID is a member, admin, creator,
// or restricted user in groupChatID.
func (c *Controller) isGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	return c.tgGroupOps().IsGroupMember(ctx, groupChatID, telegramUserID)
}

// KickFromGroup bans and immediately unbans telegramUserID from groupChatID.
// The short ban duration (60s) ensures the user is removed without a permanent ban.
func (c *Controller) KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64) error {
	if err := c.tgGroupOps().KickFromGroup(ctx, groupChatID, telegramUserID); err != nil {
		return fmt.Errorf("kick from group via group ops: %w", err)
	}
	return nil
}

func (c *Controller) replyLinkedStatus(
	ctx context.Context,
	telegramUserID int64,
	editMsgID int,
	lang, twitchLogin string,
	joinRows [][]telego.InlineKeyboardButton,
	activeNames []string,
) {
	text := ui.LinkedStatusWithJoinStateHTML(lang, twitchLogin, activeNames, len(joinRows) > 0)
	c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{
		ParseMode:      telego.ModeHTML,
		Markup:         ui.WithMainMenu(lang, joinRows...),
		DisablePreview: true,
	})
}

func (c *Controller) answerCallback(ctx context.Context, callbackID, text string) {
	c.answerCallbackOpts(ctx, callbackID, text, false)
}

func (c *Controller) answerCallbackAlert(ctx context.Context, callbackID, text string) {
	c.answerCallbackOpts(ctx, callbackID, text, true)
}

func (c *Controller) answerCallbackOpts(ctx context.Context, callbackID, text string, showAlert bool) {
	c.tgClient().AnswerCallback(ctx, callbackID, text, showAlert)
}

// --- OAuth state ---

func (c *Controller) createOAuthState(ctx context.Context, payload core.OAuthStatePayload, ttl time.Duration) (string, error) {
	state, err := NewSecureToken(24)
	if err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	if err := c.store.SaveOAuthState(ctx, state, payload, ttl); err != nil {
		return "", fmt.Errorf("save oauth state: %w", err)
	}
	return state, nil
}

func (c *Controller) tgClient() *client.Client {
	if c == nil {
		return client.New(nil, nil, nil)
	}
	if c.telegramClient == nil {
		c.telegramClient = client.New(c.tg, c.tgLimiter, c.log())
	}
	return c.telegramClient
}

func (c *Controller) tgGroupOps() *groupops.Client {
	if c == nil {
		return groupops.New(nil, nil, nil, nil)
	}
	if c.telegramGroupOps == nil {
		c.telegramGroupOps = groupops.New(c.tg, c.tgLimiter, c.log(), c.store)
	}
	return c.telegramGroupOps
}
