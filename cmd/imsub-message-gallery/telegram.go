package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"

	"imsub/internal/transport/telegram/bot"
	telegramclient "imsub/internal/transport/telegram/client"

	"github.com/joho/godotenv"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const galleryPreviewCallbackData = "gallery:noop"

var (
	errMissingTelegramToken = errors.New("missing IMSUB_TELEGRAM_BOT_TOKEN")
	errSendScenario         = errors.New("send telegram gallery scenario")

	// Dotenv files consulted by lookupTelegramBotToken, in precedence order
	// (first match wins). ".env.dev" lets contributors keep a development-only
	// token that shadows the production ".env".
	dotenvSearchOrder = []string{".env.dev", ".env"}
)

type telegramSendResult struct {
	ChatID int64
	Sent   int
}

func sendTelegramGallery(page galleryPage, chatID int64, noHeader bool) (telegramSendResult, error) {
	token, err := lookupTelegramBotToken()
	if err != nil {
		return telegramSendResult{}, err
	}
	if token == "" {
		return telegramSendResult{}, errMissingTelegramToken
	}
	tgBot, err := telego.NewBot(token, telego.WithDiscardLogger())
	if err != nil {
		return telegramSendResult{}, fmt.Errorf("init telegram bot: %w", err)
	}
	client := telegramclient.New(tgBot, nil, nil)

	sent := 0
	for _, section := range page.Sections {
		for _, scenario := range section.Scenarios {
			for _, card := range scenario.Cards {
				text := formatTelegramCardText(section.Name, scenario, card, noHeader)
				opts := &telegramclient.MessageOptions{
					DisablePreview: card.DisablePreview,
					Markup:         buildTelegramPreviewMarkup(scenario.ID, card),
				}
				if messageID := client.Send(context.Background(), chatID, text, opts); messageID == 0 {
					return telegramSendResult{}, fmt.Errorf("%w %q (%s)", errSendScenario, scenario.ID, card.Language)
				}
				sent++
			}
		}
	}

	return telegramSendResult{ChatID: chatID, Sent: sent}, nil
}

func lookupTelegramBotToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("IMSUB_TELEGRAM_BOT_TOKEN")); token != "" {
		return token, nil
	}

	for _, path := range dotenvSearchOrder {
		envMap, err := godotenv.Read(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		if token := strings.TrimSpace(envMap["IMSUB_TELEGRAM_BOT_TOKEN"]); token != "" {
			return token, nil
		}
	}

	return "", nil
}

func formatTelegramCardText(sectionName string, scenario galleryScenario, card galleryCard, noHeader bool) string {
	if noHeader {
		return card.RawText
	}
	header := fmt.Sprintf(
		"<b>%s</b>\n<i>%s · %s · %s</i>",
		html.EscapeString(scenario.Title),
		html.EscapeString(sectionName),
		html.EscapeString(scenario.ID),
		html.EscapeString(card.Language),
	)
	return header + "\n\n" + card.RawText
}

func buildTelegramPreviewMarkup(scenarioID string, card galleryCard) *telego.InlineKeyboardMarkup {
	if !card.HasButtons || len(card.Buttons) == 0 {
		return nil
	}
	rows := make([][]telego.InlineKeyboardButton, 0, len(card.Buttons))
	for rowIdx, row := range card.Buttons {
		buttons := make([]telego.InlineKeyboardButton, 0, len(row))
		for colIdx, item := range row {
			buttons = append(buttons, buildTelegramPreviewButton(scenarioID, card.Language, rowIdx, colIdx, item))
		}
		rows = append(rows, tu.InlineKeyboardRow(buttons...))
	}
	return tu.InlineKeyboard(rows...)
}

func buildTelegramPreviewButton(scenarioID, language string, rowIdx, colIdx int, item bot.PreviewButton) telego.InlineKeyboardButton {
	var button telego.InlineKeyboardButton
	switch item.Kind {
	case "url":
		button = tu.InlineKeyboardButton(item.Label).WithURL(item.Target)
	case "copy":
		button = tu.InlineKeyboardButton(item.Label).WithCopyText(&telego.CopyTextButton{Text: item.Target})
	default:
		button = tu.InlineKeyboardButton(item.Label).WithCallbackData(
			fmt.Sprintf("%s:%s:%s:%d:%d", galleryPreviewCallbackData, scenarioID, strings.ToLower(language), rowIdx, colIdx),
		)
	}
	if strings.TrimSpace(item.IconCustomEmojiID) != "" {
		button = button.WithIconCustomEmojiID(item.IconCustomEmojiID)
	}
	if strings.TrimSpace(item.Style) != "" {
		button = button.WithStyle(item.Style)
	}
	return button
}
