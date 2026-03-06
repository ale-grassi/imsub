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
	btnReset     = "btn_reset"
	btnSubscribe = "btn_subscribe"

	msgLinkedStatusNoSubsHTML           = "linked_status_no_subs_html"
	msgLinkedStatusWithSubsHTML         = "linked_status_with_subs_html"
	msgLinkedStatusWithSubsNoGroupsHTML = "linked_status_with_subs_no_groups_html"
)

func buildMainMenuMarkup(lang, refreshAction string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(CallbackButton(i18n.Translate(lang, btnRefresh), refreshAction)),
		tu.InlineKeyboardRow(CallbackButton(i18n.Translate(lang, btnReset), ActionResetConfirm)),
	)
}

// MainMenuMarkup builds the viewer main-menu inline keyboard.
func MainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return buildMainMenuMarkup(lang, ActionRefreshViewer)
}

// CreatorMainMenuMarkup builds the creator main-menu inline keyboard.
func CreatorMainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return buildMainMenuMarkup(lang, ActionRefreshCreator)
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

// WithCreatorMainMenu appends the creator main menu rows to existing keyboard rows.
func WithCreatorMainMenu(lang string, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return appendMainMenuRows(CreatorMainMenuMarkup(lang), rows...)
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

// URLButton creates an inline URL button.
func URLButton(text, targetURL string) telego.InlineKeyboardButton {
	return tu.InlineKeyboardButton(text).WithURL(targetURL)
}

// SubEndSubscribeMarkup builds a Twitch subscribe CTA keyboard for sub-end messages.
func SubEndSubscribeMarkup(lang, creatorLogin string) *telego.InlineKeyboardMarkup {
	login := strings.TrimSpace(creatorLogin)
	if login == "" {
		return nil
	}
	subscribeURL := "https://www.twitch.tv/subs/" + url.PathEscape(login)
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(URLButton(i18n.Translate(lang, btnSubscribe), subscribeURL)),
	)
}
