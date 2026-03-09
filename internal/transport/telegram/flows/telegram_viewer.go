package flows

import (
	"context"
	"time"

	"imsub/internal/core"
)

const (
	msgLinkPromptHTML    = "link_prompt_html"
	msgOAuthExchangeFail = "oauth_exchange_fail"
	msgOAuthUserInfoFail = "oauth_userinfo_fail"
	msgOAuthSaveFail     = "oauth_save_fail"
	msgSubEndPartial     = "sub_end_partial"

	btnLinkTwitch = "btn_link_twitch"
	btnJoin       = "btn_join"
)

// --- Viewer flow ---

func (c *Controller) handleViewerStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.handleViewerStartForUser(ctx, telegramUserID, editMsgID, lang, "")
}

func (c *Controller) handleViewerStartForUser(ctx context.Context, telegramUserID int64, editMsgID int, lang, userName string) string {
	access, err := c.app.ViewerAccess.LoadAccess(ctx, telegramUserID)
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
