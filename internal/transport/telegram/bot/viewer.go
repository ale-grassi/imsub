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
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	msgLinkPromptHTML    = "link_prompt_html"
	msgOAuthExchangeFail = "oauth_exchange_fail"
	msgOAuthUserInfoFail = "oauth_userinfo_fail"
	msgOAuthSaveFail     = "oauth_save_fail"
	msgSubStartReady     = "sub_start_ready"
	msgSubEndPartial     = "sub_end_partial"
	msgSubGraceStart     = "sub_grace_start"
	msgSubGraceExpired   = "sub_grace_expired"

	btnLinkTwitch = "btn_link_twitch"
	btnJoin       = "btn_join"
)

// onStartCommand handles /start by initiating the viewer flow.
func (c *Bot) onStartCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	c.handleViewerStartForUser(ctx, msg.From.ID, 0, lang, msg.From.FirstName)
	return nil
}

func (c *Bot) handleViewerStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.handleViewerStartForUser(ctx, telegramUserID, editMsgID, lang, "")
}

func (c *Bot) handleViewerStartForUser(ctx context.Context, telegramUserID int64, editMsgID int, lang, userName string) string {
	access, err := c.viewerAccess.LoadAccess(ctx, telegramUserID)
	if err != nil {
		view := buildViewerStatusErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	if !access.HasIdentity {
		payload := core.OAuthStatePayload{
			Mode:            core.OAuthModeViewer,
			TelegramUserID:  telegramUserID,
			Language:        lang,
			PromptMessageID: editMsgID,
		}
		state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
		if err != nil {
			view := buildViewerStatusErrorView(lang)
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return view.text
		}
		authURL := c.oauthStartURL(state)
		view := buildViewerPromptView(lang, userName, authURL)
		if editMsgID != 0 {
			c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
			return ""
		}
		messageID := c.sendMsg(ctx, telegramUserID, view.text, &view.opts)
		if messageID == 0 {
			c.invalidateOAuthState(ctx, state)
			return ""
		}
		payload.PromptMessageID = messageID
		if err := c.store.SaveOAuthState(ctx, state, payload, 10*time.Minute); err != nil {
			c.log().Warn("saveOAuthState prompt message update failed", "error", err)
		}
		return ""
	}

	view := buildViewerLinkedView(lang, access.Identity.TwitchLogin, access.Targets)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

// HandleViewerOAuthCallback executes viewer OAuth callback side effects and notifications.
func (c *Bot) HandleViewerOAuthCallback(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, twitchDisplayName string, err error) {
	res, flowErr := c.viewerOAuth.Complete(ctx, code, payload, lang)
	if flowErr != nil {
		var fe *core.FlowError
		if errors.As(flowErr, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				view := buildViewerOAuthFailureView(lang, msgOAuthExchangeFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("viewer token exchange failed: %w", flowErr)
			case core.KindUserInfo:
				view := buildViewerOAuthFailureView(lang, msgOAuthUserInfoFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("viewer user info failed: %w", flowErr)
			case core.KindSave:
				view := buildViewerOAuthFailureView(lang, msgOAuthSaveFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("viewer save failed: %w", flowErr)
			case core.KindScopeMissing, core.KindStore:
				view := buildViewerOAuthFailureView(lang, msgOAuthSaveFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("viewer other fail: %w", flowErr)
			case core.KindCreatorMismatch:
				view := buildViewerOAuthFailureView(lang, msgOAuthSaveFail)
				c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
				return res.ResultLabel, "", fmt.Errorf("viewer creator mismatch fail: %w", flowErr)
			}
		}
		view := buildViewerOAuthFailureView(lang, msgOAuthSaveFail)
		c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
		return res.ResultLabel, "", fmt.Errorf("viewer unexpected fail: %w", flowErr)
	}
	if res.DisplacedUserID != 0 {
		c.kickDisplacedUser(ctx, res.DisplacedUserID)
	}
	if payload.PromptMessageID != 0 {
		c.deleteMessage(ctx, payload.TelegramUserID, payload.PromptMessageID)
	}

	access, buildErr := c.viewerAccess.LoadAccess(ctx, payload.TelegramUserID)
	if buildErr != nil {
		c.log().Warn("load viewer access failed after viewer oauth callback", "telegram_user_id", payload.TelegramUserID, "error", buildErr)
		view := buildOAuthLoadStatusErrorView(lang)
		c.sendMsg(ctx, payload.TelegramUserID, view.text, &view.opts)
		return resultLoadStatusFailed, res.TwitchDisplayName, fmt.Errorf("load viewer access: %w", buildErr)
	}
	view := buildViewerLinkedView(lang, res.TwitchLogin, access.Targets)
	c.reply(ctx, payload.TelegramUserID, 0, view.text, &view.opts)

	return res.ResultLabel, res.TwitchDisplayName, nil
}

func buildViewerPromptView(lang, userName, authURL string) sharedView {
	displayName := strings.TrimSpace(userName)
	if displayName == "" {
		displayName = i18n.Translate(lang, msgUserGenericName)
	}

	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgLinkPromptHTML), html.EscapeString(displayName)),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.LinkButton(i18n.Translate(lang, btnLinkTwitch), authURL)),
				tu.InlineKeyboardRow(ui.CopyLinkButton(i18n.Translate(lang, btnCopyLink), authURL)),
			),
		},
	}
}

func buildViewerLinkedView(lang, twitchLogin string, targets core.JoinTargets) sharedView {
	joinRows := renderJoinButtons(targets, lang)
	return sharedView{
		text: ui.LinkedStatusWithJoinStateHTML(lang, twitchLogin, targets.ActiveCreatorNames, len(joinRows) > 0),
		opts: client.MessageOptions{
			Markup:         ui.WithMainMenu(lang, viewerMainMenuCallbacks(), joinRows...),
			DisablePreview: true,
		},
	}
}
