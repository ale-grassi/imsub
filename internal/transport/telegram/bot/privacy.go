package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"imsub/internal/platform/i18n"
)

const (
	msgExportSentHTML   = "export_sent_html"
	msgExportFailedHTML = "export_failed_html"
	btnExportData       = "btn_export_data"
	exportDataEmojiID   = "5877307202888273539"
)

func (c *Bot) exportMyData(ctx context.Context, telegramUserID int64, lang string) callbackFeedback {
	if c.privacy == nil {
		return callbackAlert(i18n.Translate(lang, msgExportFailedHTML))
	}
	exportData, err := c.privacy.Export(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("privacy export failed", "telegram_user_id", telegramUserID, "error", err)
		return callbackAlert(i18n.Translate(lang, msgExportFailedHTML))
	}
	body, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		c.log().Warn("privacy export marshal failed", "telegram_user_id", telegramUserID, "error", err)
		return callbackAlert(i18n.Translate(lang, msgExportFailedHTML))
	}
	filename := fmt.Sprintf("imsub-export-%d-%s.json", telegramUserID, time.Now().UTC().Format("20060102T150405Z"))
	if c.telegramClient.SendDocument(ctx, telegramUserID, filename, body, "") == 0 {
		return callbackAlert(i18n.Translate(lang, msgExportFailedHTML))
	}
	return callbackAlert(i18n.Translate(lang, msgExportSentHTML))
}
