package flows

import (
	"context"
	"errors"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	resultSaveFailed          = "save_failed"
	resultStoreFailed         = "store_failed"
	resultTokenExchangeFailed = "token_exchange_failed"
	resultUserInfoFailed      = "userinfo_failed"
	resultLoadStatusFailed    = "load_status_failed"
	resultScopeMissing        = "scope_missing"
	resultSuccess             = "success"
)

var errReconnectNotificationSend = errors.New("send reconnect-required notification")

// --- Viewer OAuth callback ---

// HandleViewerOAuthCallback executes viewer OAuth callback side effects and notifications.
func (c *Controller) HandleViewerOAuthCallback(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, twitchDisplayName string, err error) {
	res, flowErr := c.oauthSvc.LinkViewer(ctx, code, payload, lang)
	if flowErr != nil {
		var fe *core.FlowError
		if errors.As(flowErr, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthExchangeFail), nil)
				return resultTokenExchangeFailed, "", fmt.Errorf("viewer token exchange failed: %w", flowErr)
			case core.KindUserInfo:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthUserInfoFail), nil)
				return resultUserInfoFailed, "", fmt.Errorf("viewer user info failed: %w", flowErr)
			case core.KindSave:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthSaveFail), nil)
				return resultSaveFailed, "", fmt.Errorf("viewer save failed: %w", flowErr)
			case core.KindScopeMissing, core.KindStore:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthSaveFail), nil)
				return resultSaveFailed, "", fmt.Errorf("viewer other fail: %w", flowErr)
			case core.KindCreatorMismatch:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthSaveFail), nil)
				return resultSaveFailed, "", fmt.Errorf("viewer creator mismatch fail: %w", flowErr)
			}
		}
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgOAuthSaveFail), nil)
		return resultSaveFailed, "", fmt.Errorf("viewer unexpected fail: %w", flowErr)
	}
	if res.DisplacedUserID != 0 {
		c.kickDisplacedUser(ctx, res.DisplacedUserID)
	}
	if payload.PromptMessageID != 0 {
		c.deleteMessage(ctx, payload.TelegramUserID, payload.PromptMessageID)
	}

	joinRows, activeNames, buildErr := c.buildJoinButtons(ctx, payload.TelegramUserID, res.TwitchUserID, lang)
	if buildErr != nil {
		c.log().Warn("buildJoinButtons failed after viewer oauth callback", "telegram_user_id", payload.TelegramUserID, "error", buildErr)
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgErrLoadStatus), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return resultLoadStatusFailed, res.TwitchDisplayName, buildErr
	}
	c.replyLinkedStatus(ctx, payload.TelegramUserID, 0, lang, res.TwitchLogin, joinRows, activeNames)

	return resultSuccess, res.TwitchDisplayName, nil
}

// --- Creator OAuth callback ---

// HandleCreatorOAuthCallback executes creator OAuth callback side effects and notifications.
func (c *Controller) HandleCreatorOAuthCallback(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, creatorName string, err error) {
	res, flowErr := c.oauthSvc.LinkCreator(ctx, code, payload)
	if flowErr != nil {
		var fe *core.FlowError
		if errors.As(flowErr, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorExchangeFail), nil)
				return resultTokenExchangeFailed, "", fmt.Errorf("creator token exchange: %w", flowErr)
			case core.KindScopeMissing:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorScopeMissing), nil)
				return resultScopeMissing, "", fmt.Errorf("creator scope missing: %w", flowErr)
			case core.KindUserInfo:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorUserInfoFail), nil)
				return resultUserInfoFailed, "", fmt.Errorf("creator user info: %w", flowErr)
			case core.KindStore:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorStoreFail), nil)
				return resultStoreFailed, "", fmt.Errorf("creator store fail: %w", flowErr)
			case core.KindSave:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorStoreFail), nil)
				return resultStoreFailed, "", fmt.Errorf("creator save fail: %w", flowErr)
			case core.KindCreatorMismatch:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorReconnectMismatch), &client.MessageOptions{
					ParseMode: telego.ModeHTML,
				})
				return "creator_mismatch", "", fmt.Errorf("creator reconnect mismatch: %w", flowErr)
			}
		}
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, msgCreatorStoreFail), nil)
		return resultStoreFailed, "", fmt.Errorf("creator unexpected fail: %w", flowErr)
	}
	creator := res.Creator
	c.log().Debug("creator oauth exchange success", "creator_id", creator.ID, "creator_login", creator.Name, "owner_telegram_id", creator.OwnerTelegramID)
	if payload.PromptMessageID != 0 {
		c.deleteMessage(ctx, payload.TelegramUserID, payload.PromptMessageID)
	}
	c.replyCreatorStatus(ctx, payload.TelegramUserID, 0, lang, creator)
	return "success", res.BroadcasterDisplayName, nil
}

// NotifyCreatorReconnectRequired sends a one-shot stale-auth notification to a creator owner.
func (c *Controller) NotifyCreatorReconnectRequired(ctx context.Context, creator core.Creator) error {
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, creator.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = identity.Language
	}
	reconnectURL, err := c.creatorReconnectURL(ctx, creator.OwnerTelegramID, lang)
	if err != nil {
		return fmt.Errorf("creator reconnect url: %w", err)
	}
	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(ui.LinkButton(i18n.Translate(lang, btnReconnectCreator), reconnectURL)),
	)
	if messageID := c.sendMsg(ctx, creator.OwnerTelegramID, i18n.Translate(lang, msgCreatorReconnectNeeded), &client.MessageOptions{
		ParseMode: telego.ModeHTML,
		Markup:    markup,
	}); messageID == 0 {
		return errReconnectNotificationSend
	}
	return nil
}

// HandleSubscriptionEnd applies subscription-end side effects for a viewer.
func (c *Controller) HandleSubscriptionEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) error {
	res, err := c.subscriptionSvc.PrepareEnd(ctx, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin)
	if err != nil {
		c.log().Warn("process subscription end failed", "error", err)
		return fmt.Errorf("prepare subscription end: %w", err)
	}
	if !res.Found {
		return nil
	}

	if res.GroupChatID != 0 {
		if err := c.KickFromGroup(ctx, res.GroupChatID, res.TelegramUserID); err != nil {
			c.log().Warn("kickFromGroup failed", "telegram_user_id", res.TelegramUserID, "group_chat_id", res.GroupChatID, "error", err)
		}
	}

	c.sendMsg(ctx, res.TelegramUserID, fmt.Sprintf(i18n.Translate(res.Language, msgSubEndPartial), res.ViewerLogin), &client.MessageOptions{
		Markup: ui.SubEndSubscribeMarkup(res.Language, res.BroadcasterLogin),
	})
	return nil
}
