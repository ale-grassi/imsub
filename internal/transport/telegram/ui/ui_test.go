package ui

import (
	"strings"
	"testing"

	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
)

func TestProfileAndButtons(t *testing.T) {
	t.Parallel()

	htmlOut := TwitchProfileHTML(`a/b & "x"`)
	if !strings.Contains(htmlOut, "https://twitch.tv/a%2Fb") {
		t.Errorf("TwitchProfileHTML(%q) = %q, want path-escaped URL", `a/b & "x"`, htmlOut)
	}
	if !strings.Contains(htmlOut, "&amp;") {
		t.Errorf("TwitchProfileHTML(%q) = %q, want escaped HTML entities", `a/b & "x"`, htmlOut)
	}

	cb := CallbackButton("Refresh", "action:refresh_viewer")
	if cb.CallbackData != "action:refresh_viewer" || cb.Text != "Refresh" {
		t.Errorf("CallbackButton(%q, %q) = %+v, want Text=%q CallbackData=%q", "Refresh", "action:refresh_viewer", cb, "Refresh", "action:refresh_viewer")
	}
	ub := URLButton("Open", "https://example.com")
	if ub.URL != "https://example.com" || ub.Text != "Open" {
		t.Errorf("URLButton(%q, %q) = %+v, want Text=%q URL=%q", "Open", "https://example.com", ub, "Open", "https://example.com")
	}
}

func TestSubEndSubscribeMarkup(t *testing.T) {
	t.Parallel()

	if got := SubEndSubscribeMarkup("en", "  "); got != nil {
		t.Errorf("SubEndSubscribeMarkup(%q, %q) = non-nil, want nil", "en", "  ")
	}
	got := SubEndSubscribeMarkup("en", "name with spaces")
	if got == nil {
		t.Fatalf("SubEndSubscribeMarkup(%q, %q) = nil, want non-nil", "en", "name with spaces")
		return // prevent staticcheck SA5011 warning
	}
	if len(got.InlineKeyboard) != 1 || len(got.InlineKeyboard[0]) != 1 {
		t.Errorf("SubEndSubscribeMarkup(%q, %q) keyboard = %+v, want 1 row with 1 button", "en", "name with spaces", got.InlineKeyboard)
	}
	url := got.InlineKeyboard[0][0].URL
	if !strings.Contains(url, "https://www.twitch.tv/subs/name%20with%20spaces") {
		t.Errorf("SubEndSubscribeMarkup(%q, %q) URL = %q, want escaped subscribe URL", "en", "name with spaces", url)
	}
}

func TestMainMenuAndWithMainMenuMarkup(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	menu := MainMenuMarkup("en")
	if menu == nil || len(menu.InlineKeyboard) != 2 {
		t.Fatalf("MainMenuMarkup(%q) = %+v, want 2 rows", "en", menu)
	}
	if menu.InlineKeyboard[0][0].CallbackData != ActionRefreshViewer {
		t.Errorf("MainMenuMarkup(%q) first callback = %+v, want CallbackData=%q", "en", menu.InlineKeyboard[0][0], ActionRefreshViewer)
	}
	if menu.InlineKeyboard[1][0].CallbackData != ActionResetConfirm {
		t.Errorf("MainMenuMarkup(%q) second callback = %+v, want CallbackData=%q", "en", menu.InlineKeyboard[1][0], ActionResetConfirm)
	}

	creatorMenu := CreatorMainMenuMarkup("en")
	if creatorMenu == nil || len(creatorMenu.InlineKeyboard) != 2 {
		t.Fatalf("CreatorMainMenuMarkup(%q) = %+v, want 2 rows", "en", creatorMenu)
	}
	if creatorMenu.InlineKeyboard[0][0].CallbackData != ActionRefreshCreator {
		t.Errorf("CreatorMainMenuMarkup(%q) first callback = %+v, want CallbackData=%q", "en", creatorMenu.InlineKeyboard[0][0], ActionRefreshCreator)
	}
	if creatorMenu.InlineKeyboard[1][0].CallbackData != ActionResetConfirm {
		t.Errorf("CreatorMainMenuMarkup(%q) second callback = %+v, want CallbackData=%q", "en", creatorMenu.InlineKeyboard[1][0], ActionResetConfirm)
	}

	extra := WithMainMenu("en", []telego.InlineKeyboardButton{CallbackButton("X", "x")})
	if extra == nil || len(extra.InlineKeyboard) != 3 {
		t.Errorf("WithMainMenu(%q, rows=1) = %+v, want 3 rows", "en", extra)
	}
	if extra.InlineKeyboard[1][0].CallbackData != ActionRefreshViewer {
		t.Errorf("WithMainMenu(%q, rows=1) refresh callback = %+v, want CallbackData=%q", "en", extra.InlineKeyboard[1][0], ActionRefreshViewer)
	}

	creatorExtra := WithCreatorMainMenu("en", []telego.InlineKeyboardButton{CallbackButton("X", "x")})
	if creatorExtra == nil || len(creatorExtra.InlineKeyboard) != 3 {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) = %+v, want 3 rows", "en", creatorExtra)
	}
	if creatorExtra.InlineKeyboard[1][0].CallbackData != ActionRefreshCreator {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) refresh callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[1][0], ActionRefreshCreator)
	}
}

func TestLinkedStatusWithNoGroupsMessage(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	got := LinkedStatusWithJoinStateHTML("en", "alice", []string{"Creator One"}, false)
	if !strings.Contains(got, "No Telegram groups are available yet") {
		t.Errorf("LinkedStatusWithJoinStateHTML(%q, %q, %v, %t) = %q, want message containing %q", "en", "alice", []string{"Creator One"}, false, got, "No Telegram groups are available yet")
	}
}
