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

	refresh := RefreshButton("Refresh", "action:refresh_viewer")
	if refresh.IconCustomEmojiID != refreshButtonEmojiID {
		t.Errorf("RefreshButton(%q, %q) icon = %q, want %q", "Refresh", "action:refresh_viewer", refresh.IconCustomEmojiID, refreshButtonEmojiID)
	}

	link := LinkButton("Connect", "https://example.com")
	if link.IconCustomEmojiID != linkButtonEmojiID {
		t.Errorf("LinkButton(%q, %q) icon = %q, want %q", "Connect", "https://example.com", link.IconCustomEmojiID, linkButtonEmojiID)
	}
	if link.Style != "primary" {
		t.Errorf("LinkButton(%q, %q) style = %q, want %q", "Connect", "https://example.com", link.Style, "primary")
	}

	copyButton := CopyLinkButton("Copy link", "https://example.com")
	if copyButton.IconCustomEmojiID != "" {
		t.Errorf("CopyLinkButton(%q, %q) icon = %q, want empty", "Copy link", "https://example.com", copyButton.IconCustomEmojiID)
	}
	if copyButton.CopyText == nil || copyButton.CopyText.Text != "https://example.com" {
		t.Errorf("CopyLinkButton(%q, %q) copy_text = %+v, want text %q", "Copy link", "https://example.com", copyButton.CopyText, "https://example.com")
	}

	del := DeleteButton("Delete", "action:delete")
	if del.IconCustomEmojiID != deleteButtonEmojiID {
		t.Errorf("DeleteButton(%q, %q) icon = %q, want %q", "Delete", "action:delete", del.IconCustomEmojiID, deleteButtonEmojiID)
	}
	if del.Style != "danger" {
		t.Errorf("DeleteButton(%q, %q) style = %q, want %q", "Delete", "action:delete", del.Style, "danger")
	}

	reconnect := ReconnectButton("Reconnect", "creator:reconnect")
	if reconnect.IconCustomEmojiID != linkButtonEmojiID {
		t.Errorf("ReconnectButton(%q, %q) icon = %q, want %q", "Reconnect", "creator:reconnect", reconnect.IconCustomEmojiID, linkButtonEmojiID)
	}
	if reconnect.Style != "primary" {
		t.Errorf("ReconnectButton(%q, %q) style = %q, want %q", "Reconnect", "creator:reconnect", reconnect.Style, "primary")
	}

	back := BackButton("Back", "action:back")
	if back.IconCustomEmojiID != backButtonEmojiID {
		t.Errorf("BackButton(%q, %q) icon = %q, want %q", "Back", "action:back", back.IconCustomEmojiID, backButtonEmojiID)
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
	if got.InlineKeyboard[0][0].IconCustomEmojiID != linkButtonEmojiID {
		t.Errorf("SubEndSubscribeMarkup(%q, %q) icon = %q, want %q", "en", "name with spaces", got.InlineKeyboard[0][0].IconCustomEmojiID, linkButtonEmojiID)
	}
	if got.InlineKeyboard[0][0].Style != "primary" {
		t.Errorf("SubEndSubscribeMarkup(%q, %q) style = %q, want %q", "en", "name with spaces", got.InlineKeyboard[0][0].Style, "primary")
	}
}

func TestMainMenuAndWithMainMenuMarkup(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	viewerCallbacks := MainMenuCallbacks{
		Refresh: "viewer:refresh",
		Reset:   "reset:open:viewer",
	}
	menu := MainMenuMarkup("en", viewerCallbacks)
	if menu == nil || len(menu.InlineKeyboard) != 2 {
		t.Fatalf("MainMenuMarkup(%q) = %+v, want 2 rows", "en", menu)
	}
	if menu.InlineKeyboard[0][0].CallbackData != viewerCallbacks.Refresh {
		t.Errorf("MainMenuMarkup(%q) first callback = %+v, want CallbackData=%q", "en", menu.InlineKeyboard[0][0], viewerCallbacks.Refresh)
	}
	if menu.InlineKeyboard[0][0].IconCustomEmojiID != refreshButtonEmojiID {
		t.Errorf("MainMenuMarkup(%q) first icon = %q, want %q", "en", menu.InlineKeyboard[0][0].IconCustomEmojiID, refreshButtonEmojiID)
	}
	if menu.InlineKeyboard[1][0].CallbackData != viewerCallbacks.Reset {
		t.Errorf("MainMenuMarkup(%q) second callback = %+v, want CallbackData=%q", "en", menu.InlineKeyboard[1][0], viewerCallbacks.Reset)
	}
	if menu.InlineKeyboard[1][0].IconCustomEmojiID != manageButtonEmojiID {
		t.Errorf("MainMenuMarkup(%q) second icon = %q, want %q", "en", menu.InlineKeyboard[1][0].IconCustomEmojiID, manageButtonEmojiID)
	}
	if menu.InlineKeyboard[1][0].Style != "" {
		t.Errorf("MainMenuMarkup(%q) second style = %q, want empty", "en", menu.InlineKeyboard[1][0].Style)
	}

	creatorCallbacks := CreatorMenuCallbacks{
		Refresh:         "creator:refresh",
		ManageGroups:    "creator:open:groups",
		Grace:           "creator:open:grace",
		GraceActive:     true,
		Blocklist:       "creator:exec:blocklist",
		BlocklistActive: true,
		Reset:           "reset:open:creator",
	}
	creatorMenu := CreatorMainMenuMarkup("en", CreatorMenuCallbacks{
		Refresh: "creator:refresh",
		Reset:   "reset:open:creator",
	})
	if creatorMenu == nil || len(creatorMenu.InlineKeyboard) != 2 {
		t.Fatalf("CreatorMainMenuMarkup(%q) = %+v, want 2 rows", "en", creatorMenu)
	}
	if creatorMenu.InlineKeyboard[0][0].CallbackData != "creator:refresh" {
		t.Errorf("CreatorMainMenuMarkup(%q) first callback = %+v, want CallbackData=%q", "en", creatorMenu.InlineKeyboard[0][0], "creator:refresh")
	}
	if creatorMenu.InlineKeyboard[0][0].IconCustomEmojiID != refreshButtonEmojiID {
		t.Errorf("CreatorMainMenuMarkup(%q) first icon = %q, want %q", "en", creatorMenu.InlineKeyboard[0][0].IconCustomEmojiID, refreshButtonEmojiID)
	}
	if creatorMenu.InlineKeyboard[1][0].CallbackData != "reset:open:creator" {
		t.Errorf("CreatorMainMenuMarkup(%q) second callback = %+v, want CallbackData=%q", "en", creatorMenu.InlineKeyboard[1][0], "reset:open:creator")
	}
	if creatorMenu.InlineKeyboard[1][0].IconCustomEmojiID != manageButtonEmojiID {
		t.Errorf("CreatorMainMenuMarkup(%q) second icon = %q, want %q", "en", creatorMenu.InlineKeyboard[1][0].IconCustomEmojiID, manageButtonEmojiID)
	}
	if creatorMenu.InlineKeyboard[1][0].Style != "" {
		t.Errorf("CreatorMainMenuMarkup(%q) second style = %q, want empty", "en", creatorMenu.InlineKeyboard[1][0].Style)
	}

	reconnectMenu := CreatorStatusMenuMarkup("en", "https://example.com/reconnect", creatorCallbacks)
	if reconnectMenu == nil || len(reconnectMenu.InlineKeyboard) != 5 {
		t.Fatalf("CreatorStatusMenuMarkup(%q, reconnectURL) = %+v, want 5 rows", "en", reconnectMenu)
	}
	if reconnectMenu.InlineKeyboard[0][0].URL != "https://example.com/reconnect" {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) first url = %q, want %q", "en", reconnectMenu.InlineKeyboard[0][0].URL, "https://example.com/reconnect")
	}
	if reconnectMenu.InlineKeyboard[0][0].IconCustomEmojiID != linkButtonEmojiID {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) first icon = %q, want %q", "en", reconnectMenu.InlineKeyboard[0][0].IconCustomEmojiID, linkButtonEmojiID)
	}
	if reconnectMenu.InlineKeyboard[0][0].Style != "primary" {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) first style = %q, want %q", "en", reconnectMenu.InlineKeyboard[0][0].Style, "primary")
	}
	if reconnectMenu.InlineKeyboard[1][0].CallbackData != creatorCallbacks.ManageGroups {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) manage callback = %+v, want CallbackData=%q", "en", reconnectMenu.InlineKeyboard[1][0], creatorCallbacks.ManageGroups)
	}
	if reconnectMenu.InlineKeyboard[1][0].IconCustomEmojiID != manageButtonEmojiID {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) manage icon = %q, want %q", "en", reconnectMenu.InlineKeyboard[1][0].IconCustomEmojiID, manageButtonEmojiID)
	}
	if reconnectMenu.InlineKeyboard[2][0].CallbackData != creatorCallbacks.Grace {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) grace callback = %+v, want CallbackData=%q", "en", reconnectMenu.InlineKeyboard[2][0], creatorCallbacks.Grace)
	}
	if reconnectMenu.InlineKeyboard[2][0].IconCustomEmojiID != graceButtonEmojiID {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) grace icon = %q, want %q", "en", reconnectMenu.InlineKeyboard[2][0].IconCustomEmojiID, graceButtonEmojiID)
	}
	if reconnectMenu.InlineKeyboard[2][0].Style != "success" {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) grace style = %q, want %q", "en", reconnectMenu.InlineKeyboard[2][0].Style, "success")
	}
	if reconnectMenu.InlineKeyboard[3][0].CallbackData != creatorCallbacks.Blocklist {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) blocklist callback = %+v, want CallbackData=%q", "en", reconnectMenu.InlineKeyboard[3][0], creatorCallbacks.Blocklist)
	}
	if reconnectMenu.InlineKeyboard[3][0].IconCustomEmojiID != blocklistButtonEmojiID {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) blocklist icon = %q, want %q", "en", reconnectMenu.InlineKeyboard[3][0].IconCustomEmojiID, blocklistButtonEmojiID)
	}
	if reconnectMenu.InlineKeyboard[3][0].Style != "success" {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) blocklist style = %q, want %q", "en", reconnectMenu.InlineKeyboard[3][0].Style, "success")
	}
	if reconnectMenu.InlineKeyboard[4][0].CallbackData != creatorCallbacks.Reset {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) reset callback = %+v, want CallbackData=%q", "en", reconnectMenu.InlineKeyboard[4][0], creatorCallbacks.Reset)
	}
	if reconnectMenu.InlineKeyboard[4][0].IconCustomEmojiID != manageButtonEmojiID {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) reset icon = %q, want %q", "en", reconnectMenu.InlineKeyboard[4][0].IconCustomEmojiID, manageButtonEmojiID)
	}
	if reconnectMenu.InlineKeyboard[4][0].Style != "" {
		t.Errorf("CreatorStatusMenuMarkup(%q, reconnectURL) reset style = %q, want empty", "en", reconnectMenu.InlineKeyboard[4][0].Style)
	}

	extra := WithMainMenu("en", viewerCallbacks, []telego.InlineKeyboardButton{CallbackButton("X", "x")})
	if extra == nil || len(extra.InlineKeyboard) != 3 {
		t.Errorf("WithMainMenu(%q, rows=1) = %+v, want 3 rows", "en", extra)
	}
	if extra.InlineKeyboard[1][0].CallbackData != viewerCallbacks.Refresh {
		t.Errorf("WithMainMenu(%q, rows=1) refresh callback = %+v, want CallbackData=%q", "en", extra.InlineKeyboard[1][0], viewerCallbacks.Refresh)
	}

	creatorExtra := WithCreatorMainMenu("en", creatorCallbacks, []telego.InlineKeyboardButton{CallbackButton("X", "x")})
	if creatorExtra == nil || len(creatorExtra.InlineKeyboard) != 6 {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) = %+v, want 6 rows", "en", creatorExtra)
	}
	if creatorExtra.InlineKeyboard[1][0].CallbackData != creatorCallbacks.Refresh {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) refresh callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[1][0], creatorCallbacks.Refresh)
	}
	if creatorExtra.InlineKeyboard[2][0].CallbackData != creatorCallbacks.ManageGroups {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) manage callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[2][0], creatorCallbacks.ManageGroups)
	}
	if creatorExtra.InlineKeyboard[3][0].CallbackData != creatorCallbacks.Grace {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) grace callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[3][0], creatorCallbacks.Grace)
	}
	if creatorExtra.InlineKeyboard[4][0].CallbackData != creatorCallbacks.Blocklist {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) blocklist callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[4][0], creatorCallbacks.Blocklist)
	}
	if creatorExtra.InlineKeyboard[5][0].CallbackData != creatorCallbacks.Reset {
		t.Errorf("WithCreatorMainMenu(%q, rows=1) reset callback = %+v, want CallbackData=%q", "en", creatorExtra.InlineKeyboard[5][0], creatorCallbacks.Reset)
	}

	creatorReconnectExtra := WithCreatorStatusMenu("en", "https://example.com/reconnect", creatorCallbacks, []telego.InlineKeyboardButton{CallbackButton("X", "x")})
	if creatorReconnectExtra == nil || len(creatorReconnectExtra.InlineKeyboard) != 6 {
		t.Errorf("WithCreatorStatusMenu(%q, reconnectURL, rows=1) = %+v, want 6 rows", "en", creatorReconnectExtra)
	}
	if creatorReconnectExtra.InlineKeyboard[1][0].URL != "https://example.com/reconnect" {
		t.Errorf("WithCreatorStatusMenu(%q, reconnectURL, rows=1) reconnect url = %q, want %q", "en", creatorReconnectExtra.InlineKeyboard[1][0].URL, "https://example.com/reconnect")
	}
	if creatorReconnectExtra.InlineKeyboard[2][0].CallbackData != creatorCallbacks.ManageGroups {
		t.Errorf("WithCreatorStatusMenu(%q, reconnectURL, rows=1) manage callback = %+v, want CallbackData=%q", "en", creatorReconnectExtra.InlineKeyboard[2][0], creatorCallbacks.ManageGroups)
	}
	if creatorReconnectExtra.InlineKeyboard[3][0].CallbackData != creatorCallbacks.Grace {
		t.Errorf("WithCreatorStatusMenu(%q, reconnectURL, rows=1) grace callback = %+v, want CallbackData=%q", "en", creatorReconnectExtra.InlineKeyboard[3][0], creatorCallbacks.Grace)
	}
	if creatorReconnectExtra.InlineKeyboard[4][0].CallbackData != creatorCallbacks.Blocklist {
		t.Errorf("WithCreatorStatusMenu(%q, reconnectURL, rows=1) blocklist callback = %+v, want CallbackData=%q", "en", creatorReconnectExtra.InlineKeyboard[4][0], creatorCallbacks.Blocklist)
	}

	inactiveCallbacks := CreatorMenuCallbacks{
		Refresh:   "creator:refresh",
		Blocklist: "creator:exec:blocklist",
		Reset:     "reset:open:creator",
	}
	inactiveMenu := CreatorStatusMenuMarkup("en", "", inactiveCallbacks)
	if inactiveMenu.InlineKeyboard[1][0].Style != "" {
		t.Errorf("CreatorStatusMenuMarkup(%q) inactive blocklist style = %q, want empty", "en", inactiveMenu.InlineKeyboard[1][0].Style)
	}

	resetPicker := ResetScopePickerMarkup("en", "reset:pick:viewer:viewer", "reset:pick:viewer:creator", "reset:pick:viewer:both", "reset:back:viewer")
	if resetPicker == nil || len(resetPicker.InlineKeyboard) != 4 {
		t.Fatalf("ResetScopePickerMarkup(%q, ...) = %+v, want 4 rows", "en", resetPicker)
	}
	if resetPicker.InlineKeyboard[0][0].CallbackData != "reset:pick:viewer:viewer" {
		t.Errorf("ResetScopePickerMarkup first callback = %+v, want %q", resetPicker.InlineKeyboard[0][0], "reset:pick:viewer:viewer")
	}
	if resetPicker.InlineKeyboard[3][0].CallbackData != "reset:back:viewer" {
		t.Errorf("ResetScopePickerMarkup back callback = %+v, want %q", resetPicker.InlineKeyboard[3][0], "reset:back:viewer")
	}

	resetConfirm := ResetConfirmMarkup("en", "reset:exec:viewer:both", "reset:back:viewer")
	if resetConfirm == nil || len(resetConfirm.InlineKeyboard) != 2 {
		t.Fatalf("ResetConfirmMarkup(%q, ...) = %+v, want 2 rows", "en", resetConfirm)
	}
	if resetConfirm.InlineKeyboard[0][0].CallbackData != "reset:exec:viewer:both" {
		t.Errorf("ResetConfirmMarkup confirm callback = %+v, want %q", resetConfirm.InlineKeyboard[0][0], "reset:exec:viewer:both")
	}
	if resetConfirm.InlineKeyboard[1][0].CallbackData != "reset:back:viewer" {
		t.Errorf("ResetConfirmMarkup back callback = %+v, want %q", resetConfirm.InlineKeyboard[1][0], "reset:back:viewer")
	}
}

func TestLinkedStatusWithNoGroupsMessage(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	got := LinkedStatusWithJoinStateHTML("en", "alice", []string{"Creator One"}, false)
	if !strings.Contains(got, "No new Telegram groups available") {
		t.Errorf("LinkedStatusWithJoinStateHTML(%q, %q, %v, %t) = %q, want message containing %q", "en", "alice", []string{"Creator One"}, false, got, "No new Telegram groups available")
	}
}
