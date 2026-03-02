package flows

import (
	"context"
	"fmt"
	"html"
	"strings"

	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
)

// handleResetViewerCommand executes viewer data deletion: revokes group access
// and removes all viewer-related Redis keys. Runtime scales with the number
// of linked groups (one kick/unban round-trip per group).
func (c *Controller) handleResetViewerCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	res, err := c.resetSvc.ExecuteViewerReset(ctx, telegramUserID)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "err_reset"), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return i18n.Translate(lang, "err_reset")
	}
	// Nothing to delete if viewer scope is absent.
	if !res.HasIdentity {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "reset_nothing_html"), &client.MessageOptions{ParseMode: telego.ModeHTML})
		return ""
	}
	// Emit deterministic completion message with affected counts.
	text := fmt.Sprintf(i18n.Translate(lang, "reset_done_viewer_html"), html.EscapeString(res.Identity.TwitchLogin), res.GroupCount)
	c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{ParseMode: telego.ModeHTML})
	return ""
}

// handleResetCreatorCommand deletes creator data and reports a summarized
// result. Does not kick members from the linked Telegram group.
func (c *Controller) handleResetCreatorCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	// Delete creator records owned by this Telegram user.
	res, err := c.resetSvc.ExecuteCreatorReset(ctx, telegramUserID)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "err_reset"), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return i18n.Translate(lang, "err_reset")
	}
	// If no creator record existed, exit with empty-state message.
	if res.DeletedCount == 0 {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "reset_nothing_html"), &client.MessageOptions{ParseMode: telego.ModeHTML})
		return ""
	}
	// Render the successful deletion summary.
	text := fmt.Sprintf(
		i18n.Translate(lang, "reset_done_creator_html"),
		html.EscapeString(strings.Join(res.DeletedNames, ", ")),
		res.DeletedCount,
	)
	c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{ParseMode: telego.ModeHTML})
	return ""
}

// handleResetBothCommand executes a full reset across both viewer and creator
// scopes. Viewer cleanup (including group kicks) runs first, followed by
// creator data deletion. This is the heaviest reset path.
func (c *Controller) handleResetBothCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	res, err := c.resetSvc.ExecuteBothReset(ctx, telegramUserID)
	if err != nil {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "err_reset"), &client.MessageOptions{Markup: ui.MainMenuMarkup(lang)})
		return i18n.Translate(lang, "err_reset")
	}
	// Both scopes absent: reply with empty-state instead of success summary.
	if !res.HasIdentity && res.DeletedCount == 0 {
		c.reply(ctx, telegramUserID, editMsgID, i18n.Translate(lang, "reset_nothing_html"), &client.MessageOptions{ParseMode: telego.ModeHTML})
		return ""
	}
	// Render final full-reset summary.
	viewerName := "-"
	if res.HasIdentity {
		viewerName = html.EscapeString(res.Identity.TwitchLogin)
	}
	text := fmt.Sprintf(
		i18n.Translate(lang, "reset_done_both_html"),
		viewerName,
		res.GroupCount,
		html.EscapeString(strings.Join(res.DeletedNames, ", ")),
		res.DeletedCount,
	)
	c.reply(ctx, telegramUserID, editMsgID, text, &client.MessageOptions{ParseMode: telego.ModeHTML})
	return ""
}
