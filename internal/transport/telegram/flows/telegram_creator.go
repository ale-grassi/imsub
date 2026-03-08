package flows

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// --- Creator flow ---

func (c *Controller) handleCreatorRegistrationStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	owned, ok, err := c.creatorSvc.LoadOwnedCreator(ctx, telegramUserID)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, msgErrLoadStatus), nil)
		return i18n.Translate(lang, msgErrLoadStatus)
	}
	if ok {
		c.replyCreatorStatus(ctx, telegramUserID, editMsgID, lang, owned)
		return ""
	}

	return c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, false)
}

func (c *Controller) handleCreatorReconnectStart(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.replyCreatorOAuthPrompt(ctx, telegramUserID, editMsgID, lang, true)
}

func (c *Controller) creatorReconnectURL(ctx context.Context, telegramUserID int64, lang string) (string, error) {
	payload := core.OAuthStatePayload{
		Mode:           core.OAuthModeCreator,
		TelegramUserID: telegramUserID,
		Language:       lang,
		Reconnect:      true,
	}
	state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
	if err != nil {
		return "", err
	}
	return c.oauthStartURL(state), nil
}

func (c *Controller) replyCreatorOAuthPrompt(ctx context.Context, telegramUserID int64, editMsgID int, lang string, reconnect bool) string {
	payload := core.OAuthStatePayload{
		Mode:            core.OAuthModeCreator,
		TelegramUserID:  telegramUserID,
		Language:        lang,
		PromptMessageID: editMsgID,
		Reconnect:       reconnect,
	}
	state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, msgErrCreatorLink), &client.MessageOptions{Markup: ui.CreatorMainMenuMarkup(lang)})
		return i18n.Translate(lang, msgErrCreatorLink)
	}
	url := c.oauthStartURL(state)
	openKey := btnRegisterCreatorOpen
	textKey := msgCreatorRegisterInfo
	if reconnect {
		openKey = btnReconnectCreator
		textKey = msgCreatorReconnectInfo
	}
	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(ui.LinkButton(i18n.Translate(lang, openKey), url)),
		tu.InlineKeyboardRow(ui.CopyLinkButton(i18n.Translate(lang, btnCopyLink), url)),
	)
	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, textKey), &client.MessageOptions{ParseMode: telego.ModeHTML, Markup: markup})
		return ""
	}
	messageID := c.sendMsg(ctx, telegramUserID, i18n.Translate(lang, textKey), &client.MessageOptions{ParseMode: telego.ModeHTML, Markup: markup})
	if messageID != 0 {
		payload.PromptMessageID = messageID
		if err := c.store.SaveOAuthState(ctx, state, payload, 10*time.Minute); err != nil {
			c.log().Warn("saveOAuthState creator prompt message update failed", "error", err)
		}
	}
	return ""
}

func (c *Controller) replyCreatorStatus(ctx context.Context, telegramUserID int64, editMsgID int, lang string, creator core.Creator) {
	profileDisplay := ui.TwitchProfileHTML(creator.Name)
	groupLines := CreatorGroupLine(lang, creator)
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := c.creatorSvc.LoadStatus(statusCtx, creator)
	if err != nil {
		c.log().Warn("LoadStatus failed", "creator_id", creator.ID, "error", err)
	}
	reconnectURL := ""
	if status.Auth == core.CreatorAuthReconnectRequired {
		reconnectURL, err = c.creatorReconnectURL(ctx, telegramUserID, lang)
		if err != nil {
			c.log().Warn("creatorReconnectURL failed", "telegram_user_id", telegramUserID, "creator_id", creator.ID, "error", err)
		}
	}
	authStatus := creatorAuthStatusText(status, lang)
	statusDetails := creatorStatusDetailsText(status, lang)
	if creator.GroupChatID == 0 {
		text := fmt.Sprintf(
			i18n.Translate(lang, msgCreatorRegisteredNoGroup),
			profileDisplay,
			authStatus,
			statusDetails,
			groupLines,
		)
		c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{
			ParseMode:         telego.ModeHTML,
			EnableCustomEmoji: true,
			DisablePreview:    true,
			Markup:            ui.WithCreatorStatusMenu(lang, reconnectURL),
		})
		return
	}
	eventSubStatus := creatorEventSubStatusText(status, lang)
	subscriberStatus := creatorSubscriberStatusText(status, lang)
	text := fmt.Sprintf(
		i18n.Translate(lang, msgCreatorRegistered),
		profileDisplay,
		eventSubStatus,
		authStatus,
		statusDetails,
		subscriberStatus,
		groupLines,
	)

	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{
			ParseMode:         telego.ModeHTML,
			EnableCustomEmoji: true,
			DisablePreview:    true,
			Markup:            ui.WithCreatorStatusMenu(lang, reconnectURL),
		})
		return
	}

	c.sendMsg(ctx, telegramUserID, text, &client.MessageOptions{
		ParseMode:         telego.ModeHTML,
		EnableCustomEmoji: true,
		DisablePreview:    true,
		Markup:            ui.WithCreatorStatusMenu(lang, reconnectURL),
	})
}

func creatorEventSubStatusText(status core.Status, lang string) string {
	switch status.EventSub {
	case core.EventSubActive:
		return i18n.Translate(lang, msgCreatorEventSubActive)
	case core.EventSubInactive:
		return i18n.Translate(lang, msgCreatorEventSubInactive)
	case core.EventSubUnknown:
		return i18n.Translate(lang, msgCreatorEventSubUnknown)
	default:
		return i18n.Translate(lang, msgCreatorEventSubUnknown)
	}
}

func creatorAuthStatusText(status core.Status, lang string) string {
	switch status.Auth {
	case core.CreatorAuthReconnectRequired:
		return i18n.Translate(lang, msgCreatorAuthReconnect)
	case core.CreatorAuthHealthy:
		return i18n.Translate(lang, msgCreatorAuthHealthy)
	default:
		return i18n.Translate(lang, msgCreatorAuthHealthy)
	}
}

func creatorSubscriberStatusText(status core.Status, lang string) string {
	if !status.HasSubscriberCount {
		return i18n.Translate(lang, msgCreatorSubscribersPending)
	}
	return fmt.Sprintf(i18n.Translate(lang, msgCreatorSubscribersReady), status.SubscriberCount)
}

func creatorStatusDetailsText(status core.Status, lang string) string {
	lines := make([]string, 0, 3)
	if !status.LastSyncAt.IsZero() {
		lines = append(lines, fmt.Sprintf(i18n.Translate(lang, "creator_last_sync_at"), formatStatusTime(status.LastSyncAt)))
	}
	if status.Auth == core.CreatorAuthReconnectRequired && !status.AuthStatusAt.IsZero() {
		lines = append(lines, fmt.Sprintf(i18n.Translate(lang, "creator_reconnect_since"), formatStatusTime(status.AuthStatusAt)))
	}
	lines = append(lines, i18n.Translate(lang, "creator_refresh_note"))
	return strings.Join(lines, "\n")
}

func formatStatusTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format("2006-01-02 15:04 UTC")
}

// CreatorGroupLine returns one HTML bullet line describing creator-to-group binding.
func CreatorGroupLine(lang string, creator core.Creator) string {
	if creator.GroupChatID == 0 {
		return i18n.Translate(lang, msgCreatorGroupsNone)
	}
	groupName := strings.TrimSpace(creator.GroupName)
	if groupName == "" {
		groupName = "-"
	}
	return fmt.Sprintf(
		"• <b>%s</b> -> <b>%s</b>",
		html.EscapeString(creator.Name),
		html.EscapeString(groupName),
	)
}
