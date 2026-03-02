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
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "err_load_status"), nil)
		return i18n.Translate(lang, "err_load_status")
	}
	if ok {
		c.replyCreatorStatus(ctx, telegramUserID, editMsgID, lang, owned)
		return ""
	}

	payload := core.OAuthStatePayload{
		Mode:            core.OAuthModeCreator,
		TelegramUserID:  telegramUserID,
		Language:        lang,
		PromptMessageID: editMsgID,
	}
	state, err := c.createOAuthState(ctx, payload, 10*time.Minute)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "err_creator_link"), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return i18n.Translate(lang, "err_creator_link")
	}
	url := c.oauthStartURL(state)
	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(ui.URLButton(i18n.Translate(lang, "btn_register_creator_open"), url)),
	)
	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "creator_register_info"), &client.MessageOptions{ParseMode: telego.ModeHTML, Markup: markup})
		return ""
	}
	messageID := c.sendMsg(ctx, telegramUserID, i18n.Translate(lang, "creator_register_info"), &client.MessageOptions{ParseMode: telego.ModeHTML, Markup: markup})
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
	if creator.GroupChatID == 0 {
		text := fmt.Sprintf(
			i18n.Translate(lang, "creator_registered_no_group_html"),
			profileDisplay,
			groupLines,
		)
		c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{ParseMode: telego.ModeHTML, DisablePreview: true})
		return
	}
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := c.creatorSvc.LoadStatus(statusCtx, creator.ID)
	if err != nil {
		c.log().Warn("LoadStatus failed", "creator_id", creator.ID, "error", err)
	}
	eventSubStatus := creatorEventSubStatusText(status, lang)
	subscriberStatus := creatorSubscriberStatusText(status, lang)
	text := fmt.Sprintf(
		i18n.Translate(lang, "creator_registered_html"),
		profileDisplay,
		eventSubStatus,
		subscriberStatus,
		groupLines,
	)

	if editMsgID != 0 {
		c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{ParseMode: telego.ModeHTML, DisablePreview: true})
		return
	}

	c.sendMsg(ctx, telegramUserID, text, &client.MessageOptions{ParseMode: telego.ModeHTML, DisablePreview: true})
}

func creatorEventSubStatusText(status core.Status, lang string) string {
	switch status.EventSub {
	case core.EventSubActive:
		return i18n.Translate(lang, "creator_eventsub_active")
	case core.EventSubInactive:
		return i18n.Translate(lang, "creator_eventsub_inactive")
	default:
		return i18n.Translate(lang, "creator_eventsub_unknown")
	}
}

func creatorSubscriberStatusText(status core.Status, lang string) string {
	if !status.HasSubscriberCount {
		return i18n.Translate(lang, "creator_subscribers_pending")
	}
	return fmt.Sprintf(i18n.Translate(lang, "creator_subscribers_ready"), status.SubscriberCount)
}

// CreatorGroupLine returns one HTML bullet line describing creator-to-group binding.
func CreatorGroupLine(lang string, creator core.Creator) string {
	if creator.GroupChatID == 0 {
		return i18n.Translate(lang, "creator_groups_none")
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
