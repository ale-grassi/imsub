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
	// ActionRefresh refreshes viewer status.
	ActionRefresh = "action:refresh"
	// ActionRegisterCreator starts creator registration.
	ActionRegisterCreator = "action:register_creator"
	// ActionResetConfirm opens reset scope selection.
	ActionResetConfirm = "action:reset_confirm"
	// ActionResetPickViewer selects viewer reset scope.
	ActionResetPickViewer = "action:reset_pick_viewer"
	// ActionResetPickCreator selects creator reset scope.
	ActionResetPickCreator = "action:reset_pick_creator"
	// ActionResetPickBoth selects combined reset scope.
	ActionResetPickBoth = "action:reset_pick_both"
	// ActionResetBack returns to the reset scope selection.
	ActionResetBack = "action:reset_back"
	// ActionResetDoViewer confirms viewer reset.
	ActionResetDoViewer = "action:reset_do_viewer"
	// ActionResetDoCreator confirms creator reset.
	ActionResetDoCreator = "action:reset_do_creator"
	// ActionResetDoBoth confirms combined reset.
	ActionResetDoBoth = "action:reset_do_both"
)

// MainMenuMarkup builds the shared main-menu inline keyboard.
func MainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(CallbackButton(i18n.Translate(lang, "btn_refresh"), ActionRefresh)),
		tu.InlineKeyboardRow(CallbackButton(i18n.Translate(lang, "btn_reset"), ActionResetConfirm)),
	)
}

// WithMainMenu appends the main menu rows to existing keyboard rows.
func WithMainMenu(lang string, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	markup := tu.InlineKeyboard(rows...)
	markup.InlineKeyboard = append(markup.InlineKeyboard, MainMenuMarkup(lang).InlineKeyboard...)
	return markup
}

// LinkedStatusWithJoinStateHTML renders the viewer linked status block for the
// current join availability.
func LinkedStatusWithJoinStateHTML(lang, twitchLogin string, activeNames []string, hasJoinButtons bool) string {
	profileDisplay := TwitchProfileHTML(twitchLogin)
	if len(activeNames) == 0 {
		return fmt.Sprintf(i18n.Translate(lang, "linked_status_no_subs_html"), profileDisplay)
	}
	items := make([]string, 0, len(activeNames))
	for _, name := range activeNames {
		items = append(items, "• "+html.EscapeString(name))
	}
	key := "linked_status_with_subs_html"
	if !hasJoinButtons {
		key = "linked_status_with_subs_no_groups_html"
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
		tu.InlineKeyboardRow(URLButton(i18n.Translate(lang, "btn_subscribe"), subscribeURL)),
	)
}
