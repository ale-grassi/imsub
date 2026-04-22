package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imsub/internal/transport/telegram/bot"
)

func TestBuildTelegramPreviewMarkupMapsButtons(t *testing.T) {
	t.Parallel()

	card := galleryCard{
		Language:   "EN",
		HasButtons: true,
		Buttons: [][]bot.PreviewButton{
			{
				{
					Label:             "Open",
					Kind:              "url",
					Target:            "https://example.com",
					Style:             "primary",
					IconCustomEmojiID: "123",
				},
				{
					Label:  "Copy link",
					Kind:   "copy",
					Target: "https://example.com/copy",
				},
				{
					Label: "Refresh",
					Kind:  "callback",
				},
			},
		},
	}

	markup := buildTelegramPreviewMarkup("viewer-onboarding", card)
	if markup == nil {
		t.Fatal("buildTelegramPreviewMarkup() = nil")
	}
	if len(markup.InlineKeyboard) != 1 {
		t.Fatalf("len(markup.InlineKeyboard) = %d, want 1", len(markup.InlineKeyboard))
	}
	if len(markup.InlineKeyboard[0]) != 3 {
		t.Fatalf("len(markup.InlineKeyboard[0]) = %d, want 3", len(markup.InlineKeyboard[0]))
	}

	urlButton := markup.InlineKeyboard[0][0]
	if urlButton.URL != "https://example.com" {
		t.Fatalf("urlButton.URL = %q, want %q", urlButton.URL, "https://example.com")
	}
	if urlButton.Style != "primary" {
		t.Fatalf("urlButton.Style = %q, want %q", urlButton.Style, "primary")
	}
	if urlButton.IconCustomEmojiID != "123" {
		t.Fatalf("urlButton.IconCustomEmojiID = %q, want %q", urlButton.IconCustomEmojiID, "123")
	}

	copyButton := markup.InlineKeyboard[0][1]
	if copyButton.CopyText == nil || copyButton.CopyText.Text != "https://example.com/copy" {
		t.Fatalf("copyButton.CopyText = %#v, want copied text", copyButton.CopyText)
	}

	callbackButton := markup.InlineKeyboard[0][2]
	if !strings.HasPrefix(callbackButton.CallbackData, galleryPreviewCallbackData+":viewer-onboarding:en:0:2") {
		t.Fatalf("callbackButton.CallbackData = %q, want inert gallery callback", callbackButton.CallbackData)
	}
}

func TestFormatTelegramCardTextAddsHeader(t *testing.T) {
	t.Parallel()

	scenario := galleryScenario{
		ID:    "viewer-onboarding",
		Title: "/start onboarding prompt",
	}
	card := galleryCard{
		Language: "EN",
		RawText:  "Hello world",
	}

	text := formatTelegramCardText("Viewer", scenario, card, false)
	if !strings.Contains(text, "<b>/start onboarding prompt</b>") {
		t.Fatalf("formatTelegramCardText() missing title header: %q", text)
	}
	if !strings.Contains(text, "<i>Viewer · viewer-onboarding · EN</i>") {
		t.Fatalf("formatTelegramCardText() missing metadata header: %q", text)
	}
	if !strings.HasSuffix(text, "Hello world") {
		t.Fatalf("formatTelegramCardText() missing body: %q", text)
	}
}

func TestFormatTelegramCardTextWithoutHeader(t *testing.T) {
	t.Parallel()

	scenario := galleryScenario{
		ID:    "viewer-onboarding",
		Title: "/start onboarding prompt",
	}
	card := galleryCard{
		Language: "EN",
		RawText:  "Hello world",
	}

	text := formatTelegramCardText("Viewer", scenario, card, true)
	if text != "Hello world" {
		t.Fatalf("formatTelegramCardText() = %q, want %q", text, "Hello world")
	}
}

func TestLookupTelegramBotTokenPrefersDotEnvDev(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "")
	envDevPath := filepath.Join(tempDir, ".env.dev")
	if err := os.WriteFile(envDevPath, []byte("IMSUB_TELEGRAM_BOT_TOKEN=dev-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", envDevPath, err)
	}
	envPath := filepath.Join(tempDir, ".env")
	if err := os.WriteFile(envPath, []byte("IMSUB_TELEGRAM_BOT_TOKEN=env-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", envPath, err)
	}

	token, err := lookupTelegramBotToken()
	if err != nil {
		t.Fatalf("lookupTelegramBotToken() error = %v", err)
	}
	if token != "dev-token" {
		t.Fatalf("lookupTelegramBotToken() = %q, want %q", token, "dev-token")
	}
}
