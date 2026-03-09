package flows

import (
	"fmt"
	"html"
	"strings"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

func buildCreatorPromptView(lang, authURL string, reconnect bool) sharedView {
	openKey := btnRegisterCreatorOpen
	textKey := msgCreatorRegisterInfo
	if reconnect {
		openKey = btnReconnectCreator
		textKey = msgCreatorReconnectInfo
	}

	return sharedView{
		text: i18n.Translate(lang, textKey),
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.LinkButton(i18n.Translate(lang, openKey), authURL)),
				tu.InlineKeyboardRow(ui.CopyLinkButton(i18n.Translate(lang, btnCopyLink), authURL)),
			),
		},
	}
}

func buildCreatorStatusView(lang, reconnectURL string, creator core.Creator, status core.Status, groups []core.ManagedGroup) sharedView {
	profileDisplay := ui.TwitchProfileHTML(creator.Name)
	groupLines := CreatorGroupLines(lang, creator.Name, groups)
	authStatus := creatorAuthStatusText(status, lang)
	statusDetails := creatorStatusDetailsText(status, lang)
	statusMenuRows := creatorStatusMenuRows(lang, groups)

	if len(groups) == 0 {
		return sharedView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgCreatorRegisteredNoGroup),
				profileDisplay,
				authStatus,
				statusDetails,
				groupLines,
			),
			opts: client.MessageOptions{
				ParseMode:      telego.ModeHTML,
				DisablePreview: true,
				Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(false), statusMenuRows...),
			},
		}
	}

	eventSubStatus := creatorEventSubStatusText(status, lang)
	subscriberStatus := creatorSubscriberStatusText(status, lang)
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorRegistered),
			profileDisplay,
			eventSubStatus,
			authStatus,
			statusDetails,
			subscriberStatus,
			groupLines,
		),
		opts: client.MessageOptions{
			ParseMode:      telego.ModeHTML,
			DisablePreview: true,
			Markup:         ui.WithCreatorStatusMenu(lang, reconnectURL, creatorStatusMenuCallbacks(len(groups) > 1), statusMenuRows...),
		},
	}
}

func buildCreatorManagedGroupsView(lang string, creator core.Creator, groups []core.ManagedGroup, notice string) sharedView {
	text := i18n.Translate(lang, msgCreatorManageGroupsHTML)
	if len(groups) == 0 {
		text = i18n.Translate(lang, msgCreatorManageGroupsEmpty)
	} else {
		text = fmt.Sprintf(text, html.EscapeString(creator.Name))
	}
	if strings.TrimSpace(notice) != "" {
		text = notice + "\n\n" + text
	}

	rows := make([][]telego.InlineKeyboardButton, 0, len(groups)+1)
	nameCounts := creatorManagedGroupNameCounts(groups)
	for _, group := range groups {
		rows = append(rows, tu.InlineKeyboardRow(
			ui.GroupButton(creatorManagedGroupButtonLabel(group, nameCounts), creatorGroupPickCallback(group.ChatID)),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), creatorMenuCallback())))

	return sharedView{
		text: text,
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
			Markup:    tu.InlineKeyboard(rows...),
		},
	}
}

func buildCreatorGroupUnregisterConfirmView(lang string, creator core.Creator, group core.ManagedGroup, backCallback string) sharedView {
	groupLabel := creatorManagedGroupButtonLabel(group, map[string]int{group.GroupName: 1})
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgCreatorUnregisterConfirm),
			html.EscapeString(groupLabel),
			html.EscapeString(creator.Name),
		),
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.UnregisterButton(i18n.Translate(lang, btnUnregisterGroup), creatorGroupExecuteCallback(group.ChatID))),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)),
			),
		},
	}
}

func creatorManagedGroupNameCounts(groups []core.ManagedGroup) map[string]int {
	counts := make(map[string]int, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.GroupName)
		if name == "" {
			continue
		}
		counts[name]++
	}
	return counts
}

func creatorStatusMenuRows(lang string, groups []core.ManagedGroup) [][]telego.InlineKeyboardButton {
	if len(groups) != 1 {
		return nil
	}
	label := fmt.Sprintf(i18n.Translate(lang, btnManageGroup), creatorManagedGroupButtonLabel(groups[0], map[string]int{groups[0].GroupName: 1}))
	return [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(ui.GroupButton(label, creatorManageGroupsCallback())),
	}
}
