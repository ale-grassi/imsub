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
	btnRefresh          = "btn_refresh"
	btnReconnect        = "btn_reconnect_creator"
	btnManageGroups     = "btn_manage_groups"
	btnReset            = "btn_reset"
	btnSubscribe        = "btn_subscribe"
	btnResetViewerData  = "btn_reset_viewer_data"
	btnResetCreatorData = "btn_reset_creator_data"
	btnResetAllData     = "btn_reset_all_data"
	btnBack             = "btn_back"
	btnResetConfirm     = "btn_reset_confirm"

	msgLinkedStatusNoSubsHTML           = "linked_status_no_subs_html"
	msgLinkedStatusWithSubsHTML         = "linked_status_with_subs_html"
	msgLinkedStatusWithSubsNoGroupsHTML = "linked_status_with_subs_no_groups_html"

	refreshButtonEmojiID = "5258420634785947640"
	linkButtonEmojiID    = "5257991477358763590"
	deleteButtonEmojiID  = "5258130763148172425"
	backButtonEmojiID    = "5258236805890710909"
	manageButtonEmojiID  = "5258096772776991776"
	groupButtonEmojiID   = "5258513401784573443"
	unregisterEmojiID    = "5258084656674250503"
)

// MainMenuCallbacks defines callback data for the viewer main menu.
type MainMenuCallbacks struct {
	Refresh string
	Reset   string
}

// CreatorMenuCallbacks defines callback data for the creator status menu.
type CreatorMenuCallbacks struct {
	Refresh      string
	ManageGroups string
	Reset        string
}

func buildMainMenuMarkup(lang string, callbacks MainMenuCallbacks) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(RefreshButton(i18n.Translate(lang, btnRefresh), callbacks.Refresh)),
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnReset), callbacks.Reset)),
	)
}

// MainMenuMarkup builds the viewer main-menu inline keyboard.
func MainMenuMarkup(lang string, callbacks MainMenuCallbacks) *telego.InlineKeyboardMarkup {
	return buildMainMenuMarkup(lang, callbacks)
}

// CreatorStatusMenuMarkup builds the creator status inline keyboard.
func CreatorStatusMenuMarkup(lang, reconnectURL string, callbacks CreatorMenuCallbacks) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, 3)
	if strings.TrimSpace(reconnectURL) != "" {
		rows = append(rows, tu.InlineKeyboardRow(LinkButton(i18n.Translate(lang, btnReconnect), reconnectURL)))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(RefreshButton(i18n.Translate(lang, btnRefresh), callbacks.Refresh)))
	}
	if strings.TrimSpace(callbacks.ManageGroups) != "" {
		rows = append(rows, tu.InlineKeyboardRow(ManageButton(i18n.Translate(lang, btnManageGroups), callbacks.ManageGroups)))
	}
	rows = append(rows, tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnReset), callbacks.Reset)))
	return tu.InlineKeyboard(rows...)
}

// CreatorMainMenuMarkup builds the default creator main-menu inline keyboard.
func CreatorMainMenuMarkup(lang string, callbacks CreatorMenuCallbacks) *telego.InlineKeyboardMarkup {
	return CreatorStatusMenuMarkup(lang, "", callbacks)
}

func appendMainMenuRows(menu *telego.InlineKeyboardMarkup, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	markup := tu.InlineKeyboard(rows...)
	markup.InlineKeyboard = append(markup.InlineKeyboard, menu.InlineKeyboard...)
	return markup
}

// WithMainMenu appends the viewer main menu rows to existing keyboard rows.
func WithMainMenu(lang string, callbacks MainMenuCallbacks, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return appendMainMenuRows(MainMenuMarkup(lang, callbacks), rows...)
}

// WithCreatorStatusMenu appends the creator status menu rows to existing keyboard rows.
func WithCreatorStatusMenu(lang, reconnectURL string, callbacks CreatorMenuCallbacks, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return appendMainMenuRows(CreatorStatusMenuMarkup(lang, reconnectURL, callbacks), rows...)
}

// WithCreatorMainMenu appends the default creator main menu rows to existing keyboard rows.
func WithCreatorMainMenu(lang string, callbacks CreatorMenuCallbacks, rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return WithCreatorStatusMenu(lang, "", callbacks, rows...)
}

// ResetScopePickerMarkup builds the reset scope picker keyboard.
func ResetScopePickerMarkup(lang, viewerCallback, creatorCallback, bothCallback, backCallback string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnResetViewerData), viewerCallback)),
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnResetCreatorData), creatorCallback)),
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnResetAllData), bothCallback)),
		tu.InlineKeyboardRow(BackButton(i18n.Translate(lang, btnBack), backCallback)),
	)
}

// ResetConfirmMarkup builds the reset confirmation keyboard.
func ResetConfirmMarkup(lang, confirmCallback, backCallback string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(DeleteButton(i18n.Translate(lang, btnResetConfirm), confirmCallback)),
		tu.InlineKeyboardRow(BackButton(i18n.Translate(lang, btnBack), backCallback)),
	)
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

// ManageButton creates a creator group-management action button.
func ManageButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, manageButtonEmojiID)
}

// GroupButton creates a managed-group selection button.
func GroupButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, groupButtonEmojiID)
}

// UnregisterButton creates a destructive unregister-group button.
func UnregisterButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, unregisterEmojiID).WithStyle("danger")
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
