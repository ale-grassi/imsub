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
	msgLinkedStatusAccountHTML               = "linked_status_account_html"
	msgLinkedStatusNoSubsBodyHTMLNoAccount   = "linked_status_no_subs_body_html_no_account"
	msgLinkedStatusWithSubsBodyHTMLNoAccount = "linked_status_with_subs_body_html_no_account"
	msgLinkedStatusNoGroupsBodyHTMLNoAccount = "linked_status_with_subs_no_groups_body_html_no_account"

	linkButtonEmojiID  = "5257991477358763590"
	backButtonEmojiID  = "5258236805890710909"
	groupButtonEmojiID = "5258513401784573443"
)

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

// LinkButton creates a link/open/connect action button.
func LinkButton(text, targetURL string) telego.InlineKeyboardButton {
	return IconURLButton(text, targetURL, linkButtonEmojiID).WithStyle("primary")
}

// CopyLinkButton creates a copy-link action button.
func CopyLinkButton(text, copyText string) telego.InlineKeyboardButton {
	return CopyTextButton(text, copyText)
}

// GroupButton creates a managed-group selection button.
func GroupButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, groupButtonEmojiID)
}

// BackButton creates a back-navigation action button.
func BackButton(text, data string) telego.InlineKeyboardButton {
	return IconCallbackButton(text, data, backButtonEmojiID)
}
