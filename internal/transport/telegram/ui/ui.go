package ui

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	// ActionRefreshViewer refreshes viewer status.
	ActionRefreshViewer = "action:refresh_viewer"
	// ActionRefreshCreator refreshes creator status.
	ActionRefreshCreator = "action:refresh_creator"
	// ActionRegisterCreator starts creator registration.
	ActionRegisterCreator = "action:register_creator"
	// ActionReconnectCreator starts creator reconnect for an existing creator.
	ActionReconnectCreator = "action:reconnect_creator"
	// ActionResetConfirm opens the reset scope picker.
	ActionResetConfirm = "action:reset_confirm"

	// ActionResetPickViewer selects viewer reset scope.
	ActionResetPickViewer = "action:reset_pick_viewer"
	// ActionResetPickCreator selects creator reset scope.
	ActionResetPickCreator = "action:reset_pick_creator"
	// ActionResetPickBoth selects combined reset scope.
	ActionResetPickBoth = "action:reset_pick_both"
	// ActionResetPickerBack returns from the scope picker to the originating menu.
	ActionResetPickerBack = "action:reset_back_menu"
	// ActionResetPickerCancel exits the scope picker when entered from /reset.
	ActionResetPickerCancel = "action:reset_cancel"

	// ActionResetConfirmBack returns from the confirmation screen to the scope picker or menu.
	ActionResetConfirmBack = "action:reset_back"
	// ActionResetDoViewer confirms viewer data deletion.
	ActionResetDoViewer = "action:reset_do_viewer"
	// ActionResetDoCreator confirms creator data deletion.
	ActionResetDoCreator = "action:reset_do_creator"
	// ActionResetDoBoth confirms combined data deletion.
	ActionResetDoBoth = "action:reset_do_both"

	btnRefresh   = "btn_refresh"
	btnReconnect = "btn_reconnect_creator"
	btnReset     = "btn_reset"
	btnSubscribe = "btn_subscribe"

	msgLinkedStatusNoSubsHTML           = "linked_status_no_subs_html"
	msgLinkedStatusWithSubsHTML         = "linked_status_with_subs_html"
	msgLinkedStatusWithSubsNoGroupsHTML = "linked_status_with_subs_no_groups_html"

	refreshButtonEmojiID = "5258420634785947640"
	linkButtonEmojiID    = "5257991477358763590"
	deleteButtonEmojiID  = "5258130763148172425"
	backButtonEmojiID    = "5258236805890710909"
)

func buildMainMenuMarkup(lang, refreshAction string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(RefreshButton(i18n.Translate(lang, btnRefresh), refreshAction)),
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnReset), ActionResetConfirm)),
	)
}

// MainMenuMarkup builds the viewer main-menu inline keyboard.
func MainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return buildMainMenuMarkup(lang, ActionRefreshViewer)
}

// CreatorStatusMenuMarkup builds the creator status inline keyboard.
func CreatorStatusMenuMarkup(lang, reconnectURL string) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, 3)
	if strings.TrimSpace(reconnectURL) != "" {
		rows = append(rows, tu.InlineKeyboardRow(LinkButton(i18n.Translate(lang, btnReconnect), reconnectURL)))
	}
	rows = append(rows, tu.InlineKeyboardRow(RefreshButton(i18n.Translate(lang, btnRefresh), ActionRefreshCreator)))
	rows = append(rows, tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnReset), ActionResetConfirm)))
	return tu.InlineKeyboard(rows...)
}

// CreatorMainMenuMarkup builds the default creator main-menu inline keyboard.
func CreatorMainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return CreatorStatusMenuMarkup(lang, "")
}

func appendMainMenuRows(menu *telego.InlineKeyboardMarkup, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	markup := tu.InlineKeyboard(rows...)
	markup.InlineKeyboard = append(markup.InlineKeyboard, menu.InlineKeyboard...)
	return markup
}

// WithMainMenu appends the viewer main menu rows to existing keyboard rows.
func WithMainMenu(lang string, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return appendMainMenuRows(MainMenuMarkup(lang), rows...)
}

// WithCreatorStatusMenu appends the creator status menu rows to existing keyboard rows.
func WithCreatorStatusMenu(lang, reconnectURL string, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return appendMainMenuRows(CreatorStatusMenuMarkup(lang, reconnectURL), rows...)
}

// WithCreatorMainMenu appends the default creator main menu rows to existing keyboard rows.
func WithCreatorMainMenu(lang string, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return WithCreatorStatusMenu(lang, "", rows...)
}

// LinkedStatusWithJoinStateHTML renders the viewer linked status block for the
// current join availability.
func LinkedStatusWithJoinStateHTML(lang, twitchLogin string, activeNames []string, hasJoinButtons bool) string {
	profileDisplay := TwitchProfileHTML(twitchLogin)
	if len(activeNames) == 0 {
		return fmt.Sprintf(i18n.Translate(lang, msgLinkedStatusNoSubsHTML), profileDisplay)
	}
	items := make([]string, 0, len(activeNames))
	for _, name := range activeNames {
		items = append(items, "• "+html.EscapeString(name))
	}
	key := msgLinkedStatusWithSubsHTML
	if !hasJoinButtons {
		key = msgLinkedStatusWithSubsNoGroupsHTML
	}
	return fmt.Sprintf(
		i18n.Translate(lang, key),
		profileDisplay,
		strings.Join(items, "\n"),
	)
}

// TwitchProfileHTML renders an escaped Twitch profile hyperlink.
func TwitchProfileHTML(login string) string {
	profileURL := "https://twitch.tv/" + url.PathEscape(login)
	return fmt.Sprintf(
		"<code>%s</code> (<a href=\"%s\">%s</a>)",
		html.EscapeString(login),
		html.EscapeString(profileURL),
		html.EscapeString(profileURL),
	)
}

// CallbackButton creates an inline callback button.
func CallbackButton(text, data string) telego.InlineKeyboardButton {
	return tu.InlineKeyboardButton(text).WithCallbackData(data)
}

// IconCallbackButton creates an inline callback button with a custom emoji icon.
func IconCallbackButton(text, data, iconCustomEmojiID string) telego.InlineKeyboardButton {
	button := CallbackButton(text, data)
	if strings.TrimSpace(iconCustomEmojiID) == "" {
		return button
	}
	return button.WithIconCustomEmojiID(iconCustomEmojiID)
}

// URLButton creates an inline URL button.
func URLButton(text, targetURL string) telego.InlineKeyboardButton {
	return tu.InlineKeyboardButton(text).WithURL(targetURL)
}

// IconURLButton creates an inline URL button with a custom emoji icon.
func IconURLButton(text, targetURL, iconCustomEmojiID string) telego.InlineKeyboardButton {
	button := URLButton(text, targetURL)
	if strings.TrimSpace(iconCustomEmojiID) == "" {
		return button
	}
	return button.WithIconCustomEmojiID(iconCustomEmojiID)
}

// CopyTextButton creates an inline copy-text button.
func CopyTextButton(text, copyText string) telego.InlineKeyboardButton {
	return tu.InlineKeyboardButton(text).WithCopyText(&telego.CopyTextButton{
		Text: copyText,
	})
}

// RefreshButton creates a refresh action button.
func RefreshButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, refreshButtonEmojiID)
}

// LinkButton creates a link/open/connect action button.
func LinkButton(text, targetURL string) telego.InlineKeyboardButton {
	return IconURLButton(text, targetURL, linkButtonEmojiID).WithStyle("primary")
}

// CopyLinkButton creates a copy-link action button.
func CopyLinkButton(text, copyText string) telego.InlineKeyboardButton {
	return CopyTextButton(text, copyText)
}

// DeleteButton creates a destructive action button.
func DeleteButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, deleteButtonEmojiID).WithStyle("danger")
}

// ReconnectButton creates a primary reconnect action button.
func ReconnectButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, linkButtonEmojiID).WithStyle("primary")
}

// BackButton creates a back-navigation action button.
func BackButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, backButtonEmojiID)
}

// SubEndSubscribeMarkup builds a Twitch subscribe CTA keyboard for sub-end messages.
func SubEndSubscribeMarkup(lang, creatorLogin string) *telego.InlineKeyboardMarkup {
	login := strings.TrimSpace(creatorLogin)
	if login == "" {
		return nil
	}
	subscribeURL := "https://www.twitch.tv/subs/" + url.PathEscape(login)
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(LinkButton(i18n.Translate(lang, btnSubscribe), subscribeURL)),
	)
}
