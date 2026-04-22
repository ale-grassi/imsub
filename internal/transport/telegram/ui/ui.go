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
	btnRefresh       = "btn_refresh"
	btnReconnect     = "btn_reconnect_creator"
	btnManageGroups  = "btn_manage_groups"
	btnGracePeriod   = "btn_grace_period"
	btnBlocklistSync = "btn_blocklist_sync"
	btnReset         = "btn_reset"
	btnSubscribe     = "btn_subscribe"
	btnBack          = "btn_back"
	btnResetConfirm  = "btn_reset_confirm"

	msgLinkedStatusHeadingHTML               = "linked_status_heading_html"
	msgLinkedStatusAccountHTML               = "linked_status_account_html"
	msgLinkedStatusNoSubsBodyHTMLNoAccount   = "linked_status_no_subs_body_html_no_account"
	msgLinkedStatusWithSubsBodyHTMLNoAccount = "linked_status_with_subs_body_html_no_account"
	msgLinkedStatusNoGroupsBodyHTMLNoAccount = "linked_status_with_subs_no_groups_body_html_no_account"

	refreshButtonEmojiID   = "5258420634785947640"
	linkButtonEmojiID      = "5257991477358763590"
	deleteButtonEmojiID    = "5258130763148172425"
	backButtonEmojiID      = "5258236805890710909"
	manageButtonEmojiID    = "5258096772776991776"
	blocklistButtonEmojiID = "5275969776668134187"
	graceButtonEmojiID     = "5258318620722733379"
	groupButtonEmojiID     = "5258513401784573443"
	unregisterEmojiID      = "5258084656674250503"
)

// MainMenuCallbacks defines callback data for the viewer main menu.
type MainMenuCallbacks struct {
	Refresh string
	Reset   string
}

// CreatorMenuCallbacks defines callback data for the creator status menu.
type CreatorMenuCallbacks struct {
	Refresh         string
	ManageGroups    string
	Grace           string
	Blocklist       string
	GraceActive     bool
	BlocklistActive bool
	Reset           string
}

func buildMainMenuMarkup(lang string, callbacks MainMenuCallbacks) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(RefreshButton(i18n.Translate(lang, btnRefresh), callbacks.Refresh)),
		tu.InlineKeyboardRow(ManageButton(i18n.Translate(lang, btnReset), callbacks.Reset)),
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
	if strings.TrimSpace(callbacks.Grace) != "" {
		rows = append(rows, tu.InlineKeyboardRow(GraceButton(i18n.Translate(lang, btnGracePeriod), callbacks.Grace, callbacks.GraceActive)))
	}
	if strings.TrimSpace(callbacks.Blocklist) != "" {
		rows = append(rows, tu.InlineKeyboardRow(BlocklistButton(i18n.Translate(lang, btnBlocklistSync), callbacks.Blocklist, callbacks.BlocklistActive)))
	}
	rows = append(rows, tu.InlineKeyboardRow(ManageButton(i18n.Translate(lang, btnReset), callbacks.Reset)))
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
func ResetScopePickerMarkup(lang, viewerText, viewerCallback, creatorText, creatorCallback, bothText, bothCallback, backCallback string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(DeleteButton(viewerText, viewerCallback)),
		tu.InlineKeyboardRow(DeleteButton(creatorText, creatorCallback)),
		tu.InlineKeyboardRow(DeleteButton(bothText, bothCallback)),
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
func LinkedStatusWithJoinStateHTML(lang, twitchLogin, twitchDisplayName string, activeNames []string, hasJoinButtons bool) string {
	return i18n.Translate(lang, msgLinkedStatusHeadingHTML) + "\n" +
		LinkedStatusAccountHTML(lang, twitchLogin, twitchDisplayName) + "\n\n" +
		LinkedStatusDetailsHTML(lang, activeNames, hasJoinButtons)
}

// LinkedStatusAccountHTML renders the reusable account line for viewer status screens.
func LinkedStatusAccountHTML(lang, twitchLogin, twitchDisplayName string) string {
	return fmt.Sprintf(i18n.Translate(lang, msgLinkedStatusAccountHTML), TwitchProfileHTML(twitchLogin, twitchDisplayName))
}

// LinkedStatusDetailsHTML renders the reusable subscription and join-link details without heading/account.
func LinkedStatusDetailsHTML(lang string, activeNames []string, hasJoinButtons bool) string {
	if len(activeNames) == 0 {
		return i18n.Translate(lang, msgLinkedStatusNoSubsBodyHTMLNoAccount)
	}
	items := make([]string, 0, len(activeNames))
	for _, name := range activeNames {
		items = append(items, "• "+html.EscapeString(name))
	}
	key := msgLinkedStatusWithSubsBodyHTMLNoAccount
	if !hasJoinButtons {
		key = msgLinkedStatusNoGroupsBodyHTMLNoAccount
	}
	return fmt.Sprintf(
		i18n.Translate(lang, key),
		strings.Join(items, "\n"),
	)
}

// TwitchProfileHTML renders an escaped Twitch profile hyperlink.
func TwitchProfileHTML(login, displayName string) string {
	profileURL := "https://twitch.tv/" + url.PathEscape(login)
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = login
	}
	linkLabel := "twitch.tv/" + login
	return fmt.Sprintf(
		"<code>%s</code> (<a href=\"%s\">%s</a>)",
		html.EscapeString(label),
		html.EscapeString(profileURL),
		html.EscapeString(linkLabel),
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

// BlocklistButton creates a creator ban-sync toggle button.
func BlocklistButton(text, data string, active bool) telego.InlineKeyboardButton {
	button := IconCallbackButton(text, data, blocklistButtonEmojiID)
	if active {
		return button.WithStyle("success")
	}
	return button
}

// GraceButton creates a creator subscription-end grace toggle button.
func GraceButton(text, data string, active bool) telego.InlineKeyboardButton {
	button := IconCallbackButton(text, data, graceButtonEmojiID)
	if active {
		return button.WithStyle("success")
	}
	return button
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
