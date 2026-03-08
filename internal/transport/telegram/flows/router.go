package flows

import (
	"context"

	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
)

const msgCbRefreshed = "cb_refreshed"

// RegisterTelegramHandlers binds Telegram commands, callbacks, and join-request handlers.
func (c *Controller) RegisterTelegramHandlers() {
	if c.tgHandler == nil {
		return
	}

	privateOnly := func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type == telego.ChatTypePrivate && update.Message.From != nil
	}
	groupOnly := func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type != telego.ChatTypePrivate && update.Message.From != nil
	}

	c.tgHandler.HandleMessage(c.onRegisterGroup, tghandler.CommandEqual("registergroup"))
	c.tgHandler.HandleMessage(c.onUnregisterCommand, tghandler.And(tghandler.CommandEqual("unregister"), groupOnly))
	c.tgHandler.HandleMessage(c.onStartCommand, tghandler.And(tghandler.CommandEqual("start"), privateOnly))
	c.tgHandler.HandleMessage(c.onCreatorCommand, tghandler.And(tghandler.CommandEqual("creator"), privateOnly))
	c.tgHandler.HandleMessage(c.onResetCommand, tghandler.And(tghandler.CommandEqual("reset"), privateOnly))
	c.tgHandler.HandleCallbackQuery(func(ctx *tghandler.Context, query telego.CallbackQuery) error {
		c.onCallbackQuery(ctx, query)
		return nil
	}, tghandler.AnyCallbackQuery())
	c.tgHandler.HandleChatJoinRequest(c.onChatJoinRequest)
	c.tgHandler.HandleChatMemberUpdated(c.onChatMemberUpdated)
	c.tgHandler.HandleMyChatMemberUpdated(c.onMyChatMemberUpdated)
	c.tgHandler.HandleMessage(c.onGroupMessage, tghandler.And(tghandler.AnyMessage(), groupOnly))
	c.tgHandler.HandleMessage(c.onUnknownMessage, tghandler.And(tghandler.AnyMessage(), privateOnly))
}

func (c *Controller) onCallbackQuery(ctx context.Context, q telego.CallbackQuery) {
	lang := i18n.NormalizeLanguage(q.From.LanguageCode)
	var msgID int
	if q.Message != nil {
		msgID = q.Message.GetMessageID()
	}

	action, ok := parseCallbackAction(q.Data)
	if !ok {
		c.log().Warn("ignore unknown callback data", "telegram_user_id", q.From.ID, "data", q.Data)
		c.answerCallback(ctx, q.ID, "")
		return
	}

	alertErr := c.dispatchCallbackAction(ctx, q.From.ID, msgID, lang, action)
	if alertErr != "" {
		c.answerCallbackAlert(ctx, q.ID, alertErr)
		return
	}

	callbackText := ""
	if action.verb == callbackVerbRefresh {
		callbackText = i18n.Translate(lang, msgCbRefreshed)
	}
	c.answerCallback(ctx, q.ID, callbackText)
}

func (c *Controller) dispatchCallbackAction(ctx context.Context, userID int64, editMsgID int, lang string, action callbackAction) string {
	switch action.domain {
	case callbackDomainViewer:
		return c.handleViewerStart(ctx, userID, editMsgID, lang)
	case callbackDomainCreator:
		switch action.verb {
		case callbackVerbRefresh, callbackVerbRegister:
			return c.handleCreatorRegistrationStart(ctx, userID, editMsgID, lang)
		case callbackVerbReconnect:
			return c.handleCreatorReconnectStart(ctx, userID, editMsgID, lang)
		case callbackVerbOpen, callbackVerbPick, callbackVerbBack, callbackVerbMenu, callbackVerbCancel, callbackVerbExecute:
			// parseCallbackAction rejects these verbs for creator callbacks.
			c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
			return ""
		default:
			c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
			return ""
		}
	case callbackDomainReset:
		return c.handleResetAction(ctx, userID, editMsgID, lang, action)
	}
	c.log().Warn("unsupported callback action", "telegram_user_id", userID, "data", action.String())
	return ""
}
