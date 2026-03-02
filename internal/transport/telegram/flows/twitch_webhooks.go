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
)

// --- Viewer OAuth callback ---

// HandleViewerOAuthCallback executes viewer OAuth callback side effects and notifications.
func (c *Controller) HandleViewerOAuthCallback(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, twitchDisplayName string, err error) {
	res, flowErr := c.oauthSvc.LinkViewer(ctx, code, payload, lang)
	if flowErr != nil {
		var fe *core.FlowError
		if errors.As(flowErr, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "oauth_exchange_fail"), nil)
				return "token_exchange_failed", "", flowErr
			case core.KindUserInfo:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "oauth_userinfo_fail"), nil)
				return "userinfo_failed", "", flowErr
			case core.KindSave:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "oauth_save_fail"), nil)
				return "save_failed", "", flowErr
			}
		}
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "oauth_save_fail"), nil)
		return "save_failed", "", flowErr
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
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "err_load_status"), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return "load_status_failed", res.TwitchDisplayName, buildErr
	}
	c.replyLinkedStatus(ctx, payload.TelegramUserID, 0, lang, res.TwitchLogin, joinRows, activeNames)

	return "success", res.TwitchDisplayName, nil
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
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "creator_exchange_fail"), nil)
				return "token_exchange_failed", "", flowErr
			case core.KindScopeMissing:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "creator_scope_missing"), nil)
				return "scope_missing", "", flowErr
			case core.KindUserInfo:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "creator_userinfo_fail"), nil)
				return "userinfo_failed", "", flowErr
			case core.KindStore:
				c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "creator_store_fail"), nil)
				return "store_failed", "", flowErr
			}
		}
		c.sendMsg(ctx, payload.TelegramUserID, i18n.Translate(lang, "creator_store_fail"), nil)
		return "store_failed", "", flowErr
	}
	creator := res.Creator
	c.log().Debug("creator oauth exchange success", "creator_id", creator.ID, "creator_login", creator.Name, "owner_telegram_id", creator.OwnerTelegramID)
	if payload.PromptMessageID != 0 {
		c.deleteMessage(ctx, payload.TelegramUserID, payload.PromptMessageID)
	}
	profileDisplay := ui.TwitchProfileHTML(creator.Name)
	groupLines := CreatorGroupLine(lang, creator)
	text := fmt.Sprintf(
		i18n.Translate(lang, "creator_registered_no_group_html"),
		profileDisplay,
		groupLines,
	)
	c.sendMsg(ctx, payload.TelegramUserID, text, &client.MessageOptions{ParseMode: telego.ModeHTML, DisablePreview: true})
	return "success", res.BroadcasterDisplayName, nil
}

// HandleSubscriptionEnd applies subscription-end side effects for a viewer.
func (c *Controller) HandleSubscriptionEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) error {
	res, err := c.subscriptionSvc.PrepareEnd(ctx, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin)
	if err != nil {
		c.log().Warn("process subscription end failed", "error", err)
		return err
	}
	if !res.Found {
		return nil
	}

	if res.GroupChatID != 0 {
		if err := c.KickFromGroup(ctx, res.GroupChatID, res.TelegramUserID); err != nil {
			c.log().Warn("kickFromGroup failed", "telegram_user_id", res.TelegramUserID, "group_chat_id", res.GroupChatID, "error", err)
		}
	}

	c.sendMsg(ctx, res.TelegramUserID, fmt.Sprintf(i18n.Translate(res.Language, "sub_end_partial"), res.ViewerLogin), &client.MessageOptions{
		Markup: ui.SubEndSubscribeMarkup(res.Language, res.BroadcasterLogin),
	})
	return nil
}
