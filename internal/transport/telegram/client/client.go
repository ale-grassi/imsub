package client

import (
	"context"
	"log/slog"
	"strings"

	"imsub/internal/events"
	"imsub/internal/transport/telegram"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type limiter interface {
	Wait(ctx context.Context, chatID int64) error
}

// MessageOptions configures send/edit operations.
type MessageOptions struct {
	DisableHTML        bool
	DisableCustomEmoji bool
	Markup             *telego.InlineKeyboardMarkup
	DisablePreview     bool
	MessageThreadID    int
	ReplyToMessageID   int
}

// Client wraps Telegram send/edit/delete/callback operations with limiter and
// error-tolerant behavior.
type Client struct {
	bot     *telego.Bot
	limiter limiter
	logger  *slog.Logger
	events  events.EventSink
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

// SetObserver configures the optional event sink used for telemetry projection.
func (c *Client) SetObserver(sink events.EventSink) {
	if c == nil {
		return
	}
	c.events = events.EnsureSink(sink)
}

func (c *Client) emitTelegramAPIError(ctx context.Context, method string, err error) {
	if c == nil || c.events == nil || err == nil {
		return
	}
	c.events.Emit(ctx, events.Event{
		Name: events.NameTelegramAPIError,
		Fields: map[string]string{
			"method": method,
			"reason": normalizeTelegramAPIErrorReason(err),
		},
	})
}

func normalizeTelegramAPIErrorReason(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "message_too_long"):
		return "message_too_long"
	case strings.Contains(msg, "too many requests"), strings.Contains(msg, "retry after"):
		return "rate_limited"
	case strings.Contains(msg, "forbidden"):
		return "forbidden"
	case strings.Contains(msg, "bad request"):
		return "bad_request"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	default:
		return "unknown"
	}
}

// Send sends a Telegram message and returns its message ID, or 0 on failure.
func (c *Client) Send(ctx context.Context, chatID int64, text string, opts *MessageOptions) int {
	if c == nil || c.bot == nil {
		return 0
	}
	text = transformOutgoingText(text, opts)
	params := tu.Message(tu.ID(chatID), text)
	params.WithParseMode(telego.ModeHTML)
	if opts != nil {
		if opts.Markup != nil {
			params.WithReplyMarkup(opts.Markup)
		}
		if opts.DisableHTML {
			params.ParseMode = ""
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
	if err != nil {
		if !telegram.IsForbidden(err) {
			c.logger.Warn("Send message failed", "chat_id", chatID, "error", err)
		}
		c.emitTelegramAPIError(ctx, "send_message", err)
		return 0
	}
	if msg == nil {
		return 0
	}
	return msg.MessageID
}

// Edit edits a Telegram message in place and returns the edited message ID, or 0 on failure.
func (c *Client) Edit(ctx context.Context, chatID int64, messageID int, text string, opts *MessageOptions) int {
	if c == nil || c.bot == nil {
		return 0
	}
	text = transformOutgoingText(text, opts)
	params := tu.EditMessageText(tu.ID(chatID), messageID, text)
	params.WithParseMode(telego.ModeHTML)
	if opts != nil {
		if opts.Markup != nil {
			params.WithReplyMarkup(opts.Markup)
		}
		if opts.DisableHTML {
			params.ParseMode = ""
		}
		if opts.DisablePreview {
			params.WithLinkPreviewOptions(&telego.LinkPreviewOptions{IsDisabled: true})
		}
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			c.logger.Warn("Edit message rate limit wait failed", "chat_id", chatID, "error", err)
			return 0
		}
	}
	_, err := c.bot.EditMessageText(ctx, params)
	if err != nil {
		if !telegram.IsForbidden(err) {
			c.logger.Warn("Edit message failed", "message_id", messageID, "chat_id", chatID, "error", err)
		}
		c.emitTelegramAPIError(ctx, "edit_message_text", err)
		return 0
	}
	return messageID
}

// Reply edits when messageID != 0, otherwise sends a new message.
func (c *Client) Reply(ctx context.Context, chatID int64, messageID int, text string, opts *MessageOptions) int {
	if messageID != 0 {
		if c == nil || c.bot == nil {
			return messageID
		}
		return c.Edit(ctx, chatID, messageID, text, opts)
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
	if err != nil {
		if !telegram.IsBadRequest(err) && !telegram.IsForbidden(err) {
			c.logger.Warn("Delete message failed", "chat_id", chatID, "message_id", messageID, "error", err)
		}
		c.emitTelegramAPIError(ctx, "delete_message", err)
	}
}

// SendDraft streams a partial message draft to a private chat.
// The draft is shown as a typing indicator with text that updates in place.
func (c *Client) SendDraft(ctx context.Context, chatID int64, draftID int, text string, opts *MessageOptions) {
	if c == nil || c.bot == nil {
		return
	}
	text = transformOutgoingText(text, opts)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatID); err != nil {
			c.logger.Warn("Send draft rate limit wait failed", "chat_id", chatID, "error", err)
			return
		}
	}
	params := &telego.SendMessageDraftParams{
		ChatID:    chatID,
		DraftID:   draftID,
		Text:      text,
		ParseMode: telego.ModeHTML,
	}
	if opts != nil {
		if opts.DisableHTML {
			params.ParseMode = ""
		}
		if opts.MessageThreadID > 0 {
			params.MessageThreadID = opts.MessageThreadID
		}
	}
	if err := c.bot.SendMessageDraft(ctx, params); err != nil {
		if !telegram.IsForbidden(err) {
			c.logger.Warn("Send draft failed", "chat_id", chatID, "draft_id", draftID, "error", err)
		}
		c.emitTelegramAPIError(ctx, "send_message_draft", err)
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
	if err != nil {
		if !telegram.IsForbidden(err) {
			c.logger.Warn("Answer callback failed", "error", err)
		}
		c.emitTelegramAPIError(ctx, "answer_callback_query", err)
	}
}
