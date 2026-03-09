package flows

import (
	"context"

	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
)

// onCreatorCommand handles /creator by showing the creator home/status flow.
func (c *Controller) onCreatorCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleCreatorStart(ctx, msg.From.ID, 0, lang)
	return nil
}

func (c *Controller) handleCreatorCallback(ctx context.Context, userID int64, editMsgID int, lang string, action callbackAction) string {
	switch action.verb {
	case callbackVerbRefresh, callbackVerbRegister:
		return c.handleCreatorStart(ctx, userID, editMsgID, lang)
	case callbackVerbReconnect:
		return c.handleCreatorReconnectStart(ctx, userID, editMsgID, lang)
	case callbackVerbOpen:
		if action.target == creatorCallbackTargetGroups {
			return c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, "")
		}
	case callbackVerbPick:
		if action.target == creatorCallbackTargetGroup {
			return c.replyCreatorGroupUnregisterConfirm(ctx, userID, editMsgID, lang, action.chatID)
		}
	case callbackVerbBack:
		if action.target == creatorCallbackTargetGroups {
			return c.replyCreatorManagedGroups(ctx, userID, editMsgID, lang, "")
		}
	case callbackVerbMenu:
		return c.handleCreatorStart(ctx, userID, editMsgID, lang)
	case callbackVerbExecute:
		if action.target == creatorCallbackTargetGroup {
			return c.executeCreatorGroupUnregister(ctx, userID, editMsgID, lang, action.chatID)
		}
	case callbackVerbCancel:
		// parseCallbackAction rejects this verb for creator callbacks.
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return ""
	default:
		c.log().Warn("unsupported creator callback verb", "telegram_user_id", userID, "verb", action.verb)
		return ""
	}
	c.log().Warn("unsupported creator callback action", "telegram_user_id", userID, "verb", action.verb, "target", action.target, "chat_id", action.chatID)
	return ""
}
