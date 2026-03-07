package client

import (
	"context"
	"log/slog"

	"imsub/internal/transport/telegram/tgerr"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type limiter interface {
	Wait(ctx context.Context, chatID int64) error
}

// MessageOptions configures send/edit operations.
type MessageOptions struct {
	ParseMode        string
	Markup           *telego.InlineKeyboardMarkup
	DisablePreview   bool
	MessageThreadID  int
	ReplyToMessageID int
}

// Client wraps Telegram send/edit/delete/callback operations with limiter and
// error-tolerant behavior.
type Client struct {
	bot     *telego.Bot
	limiter limiter
	logger  *slog.Logger
}

// New creates a Telegram client wrapper with optional logger fallback.
func New(bot *telego.Bot, lim limiter, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		bot:     bot,
		limiter: lim,
		logger:  logger,
	}
}

// Send sends a Telegram message and returns its message ID, or 0 on failure.
func (c *Client) Send(ctx context.Context, chatID int64, text string, opts *MessageOptions) int {
	if c == nil || c.bot == nil {
		return 0
	}
	params := tu.Message(tu.ID(chatID), text)
	if opts != nil {
		if opts.Markup != nil {
			params.WithReplyMarkup(opts.Markup)
		}
		if opts.ParseMode != "" {
			params.WithParseMode(opts.ParseMode)
		}
		if opts.DisablePreview {
			params.WithLinkPreviewOptions(&telego.LinkPreviewOptions{IsDisabled: true})
		}
		if opts.MessageThreadID > 0 {
			params.WithMessageThreadID(opts.MessageThreadID)
		}
		if opts.ReplyToMessageID > 0 {
			params.WithReplyParameters((&telego.ReplyParameters{}).
				WithMessageID(opts.ReplyToMessageID).
				WithAllowSendingWithoutReply())
		}
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			c.logger.Warn("Send message rate limit wait failed", "chat_id", chatID, "error", err)
			return 0
		}
	}
	msg, err := c.bot.SendMessage(ctx, params)
	if err != nil && !tgerr.IsForbidden(err) {
		c.logger.Warn("Send message failed", "chat_id", chatID, "error", err)
		return 0
	}
	if msg == nil {
		return 0
	}
	return msg.MessageID
}

// Edit edits a Telegram message in place.
func (c *Client) Edit(ctx context.Context, chatID int64, messageID int, text string, opts *MessageOptions) {
	if c == nil || c.bot == nil {
		return
	}
	params := tu.EditMessageText(tu.ID(chatID), messageID, text)
	if opts != nil {
		if opts.Markup != nil {
			params.WithReplyMarkup(opts.Markup)
		}
		if opts.ParseMode != "" {
			params.WithParseMode(opts.ParseMode)
		}
		if opts.DisablePreview {
			params.WithLinkPreviewOptions(&telego.LinkPreviewOptions{IsDisabled: true})
		}
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			c.logger.Warn("Edit message rate limit wait failed", "chat_id", chatID, "error", err)
			return
		}
	}
	_, err := c.bot.EditMessageText(ctx, params)
	if err != nil && !tgerr.IsForbidden(err) {
		c.logger.Warn("Edit message failed", "message_id", messageID, "chat_id", chatID, "error", err)
	}
}

// Reply edits when messageID != 0, otherwise sends a new message.
func (c *Client) Reply(ctx context.Context, chatID int64, messageID int, text string, opts *MessageOptions) int {
	if messageID != 0 {
		c.Edit(ctx, chatID, messageID, text, opts)
		return messageID
	}
	return c.Send(ctx, chatID, text, opts)
}

// Delete deletes a Telegram message.
func (c *Client) Delete(ctx context.Context, chatID int64, messageID int) {
	if c == nil || c.bot == nil || chatID <= 0 || messageID <= 0 {
		return
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			return
		}
	}
	err := c.bot.DeleteMessage(ctx, &telego.DeleteMessageParams{
		ChatID:    tu.ID(chatID),
		MessageID: messageID,
	})
	if err != nil && !tgerr.IsBadRequest(err) && !tgerr.IsForbidden(err) {
		c.logger.Warn("Delete message failed", "chat_id", chatID, "message_id", messageID, "error", err)
	}
}

// SendDraft streams a partial message draft to a private chat.
// The draft is shown as a typing indicator with text that updates in place.
func (c *Client) SendDraft(ctx context.Context, chatID int64, draftID int, text string, opts *MessageOptions) {
	if c == nil || c.bot == nil {
		return
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			c.logger.Warn("Send draft rate limit wait failed", "chat_id", chatID, "error", err)
			return
		}
	}
	params := &telego.SendMessageDraftParams{
		ChatID:  chatID,
		DraftID: draftID,
		Text:    text,
	}
	if opts != nil {
		if opts.ParseMode != "" {
			params.ParseMode = opts.ParseMode
		}
		if opts.MessageThreadID > 0 {
			params.MessageThreadID = opts.MessageThreadID
		}
	}
	if err := c.bot.SendMessageDraft(ctx, params); err != nil && !tgerr.IsForbidden(err) {
		c.logger.Warn("Send draft failed", "chat_id", chatID, "draft_id", draftID, "error", err)
	}
}

// AnswerCallback sends callback query acknowledgement.
func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) {
	if c == nil || c.bot == nil {
		return
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 0); err != nil {
			c.logger.Warn("Answer callback rate limit wait failed", "error", err)
			return
		}
	}
	params := tu.CallbackQuery(callbackID)
	if text != "" {
		params.WithText(text)
	}
	if showAlert {
		params.WithShowAlert()
	}
	err := c.bot.AnswerCallbackQuery(ctx, params)
	if err != nil && !tgerr.IsForbidden(err) {
		c.logger.Warn("Answer callback failed", "error", err)
	}
}
