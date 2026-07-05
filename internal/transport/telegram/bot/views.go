package bot

import (
	"fmt"
	"html"
	"math"
	"net/url"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"
)

type sharedView struct {
	text string
	opts client.MessageOptions
}

type textSection struct {
	text string
}

func sharedViewFromRendered(rendered ui.RenderedScreen) sharedView {
	return sharedView{text: rendered.Text, opts: rendered.Opts}
}

func sharedScreenViewFromHTML(textHTML ui.HTML, actions []ui.ActionGroup, navigation []ui.NavigationItem, disablePreview bool) sharedView {
	emoji, title, bodyHTML := splitScreenText(string(textHTML))
	screen := ui.Screen{
		Actions:        actions,
		Navigation:     navigation,
		DisablePreview: disablePreview,
	}
	if emoji != "" && title != "" {
		screen.Header = ui.HeaderSection{Emoji: emoji, Title: title, BodyHTML: ui.TrustedHTML(bodyHTML)}
	} else {
		screen.Body = []ui.BodySection{{TextHTML: textHTML}}
	}
	return sharedViewFromRendered(ui.RenderScreen(screen))
}

func buildTextView(lang, key string) sharedView {
	return sharedScreenViewFromHTML(ui.TrustedHTML(i18n.Translate(lang, key)), nil, nil, false)
}

func buildMainMenuTextView(lang, key string) sharedView {
	return sharedScreenViewFromHTML(ui.TrustedHTML(i18n.Translate(lang, key)), viewerMainMenuActionGroups(lang), nil, false)
}

func buildViewerErrorView(lang string) sharedView {
	return buildTextView(lang, msgViewerError)
}

func buildViewerGodView(lang string, targets core.JoinTargets) sharedView {
	return sharedScreenViewFromHTML(
		ui.TrustedHTML(fmt.Sprintf(i18n.Translate(lang, "viewer_god_html"), renderCreatorNames(targets.ActiveCreatorNames))),
		viewerMainMenuActionGroups(lang),
		nil,
		true,
	)
}

func buildInfoView(lang string) sharedView {
	return sharedScreenViewFromHTML(ui.TrustedHTML(i18n.Translate(lang, msgCmdInfoHTML)), nil, nil, true)
}

func buildCreatorStatusErrorView(lang string) sharedView {
	return buildTextView(lang, msgErrLoadStatus)
}

func buildCreatorLinkErrorView(lang string) sharedView {
	return buildTextView(lang, msgErrCreatorLink)
}

func buildSubscriptionEndView(lang, broadcasterLogin string) sharedView {
	return buildSubscriptionNoticeView(
		fmt.Sprintf(i18n.Translate(lang, msgSubEndPartial), html.EscapeString(broadcasterLogin)),
		lang,
		broadcasterLogin,
	)
}

func buildSubscriptionGraceStartView(lang, broadcasterLogin string, dueAt time.Time) sharedView {
	remainingHours := graceRemainingHours(dueAt, time.Now().UTC())
	return buildSubscriptionNoticeView(
		fmt.Sprintf(
			i18n.Translate(lang, msgSubGraceStart),
			html.EscapeString(broadcasterLogin),
			remainingHours,
		),
		lang,
		broadcasterLogin,
	)
}

func buildSubscriptionGraceExpiredView(lang, broadcasterLogin string) sharedView {
	return buildSubscriptionEndView(lang, broadcasterLogin)
}

func buildSubscriptionStartView(lang string, identity core.UserIdentity, broadcasterLogin string, targets core.JoinTargets) sharedView {
	joinActions := viewerJoinActionGroups(targets, lang)
	emoji, title, _ := splitScreenText(i18n.Translate(lang, msgSubStartHeading))
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji: emoji,
			Title: title,
		},
		Body: []ui.BodySection{
			{TextHTML: ui.TrustedHTML(ui.LinkedStatusAccountHTML(lang, identity.TwitchLogin, identity.TwitchDisplayName))},
			{TextHTML: ui.TrustedHTML(fmt.Sprintf(i18n.Translate(lang, msgSubStartBodyHTML), html.EscapeString(broadcasterLogin)))},
			{TextHTML: ui.TrustedHTML(ui.LinkedStatusDetailsHTML(lang, targets.ActiveCreatorNames, len(joinActions) > 0))},
		},
		Actions:        append(joinActions, viewerMainMenuActionGroups(lang)...),
		DisablePreview: true,
	}))
}

func buildSubscriptionNoticeView(textHTML, lang, broadcasterLogin string) sharedView {
	emoji, title, bodyHTML := splitScreenText(textHTML)
	return sharedViewFromRendered(ui.RenderScreen(ui.Screen{
		Header: ui.HeaderSection{
			Emoji:    emoji,
			Title:    title,
			BodyHTML: ui.TrustedHTML(bodyHTML),
		},
		Actions: []ui.ActionGroup{subscribeActionGroup(lang, broadcasterLogin)},
	}))
}

func subscribeActionGroup(lang, broadcasterLogin string) ui.ActionGroup {
	login := strings.TrimSpace(broadcasterLogin)
	if login == "" {
		return ui.ActionGroup{}
	}
	return ui.ActionGroup{Items: []ui.ActionItem{{
		Kind:        ui.ActionKindURL,
		Label:       i18n.Translate(lang, "btn_subscribe"),
		Target:      "https://www.twitch.tv/subs/" + url.PathEscape(login),
		IconEmojiID: "5257991477358763590",
		Available:   true,
	}}}
}

func buildGroupBotRemovedOwnerView(lang, groupName string) sharedView {
	return sharedScreenViewFromHTML(ui.TrustedHTML(fmt.Sprintf(i18n.Translate(lang, msgGroupBotRemovedOwnerDM), html.EscapeString(groupName))), nil, nil, false)
}

func joinNonEmptyLines(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func joinNonEmptySections(sections ...textSection) string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		text := strings.TrimSpace(section.text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n\n")
}

func renderWarningBlock(title string, warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	lines := make([]string, 0, len(warnings)+1)
	lines = append(lines, title)
	lines = append(lines, warnings...)
	return joinNonEmptyLines(lines...)
}

func graceRemainingHours(dueAt, now time.Time) int {
	if dueAt.IsZero() {
		return 0
	}
	remaining := dueAt.UTC().Sub(now.UTC())
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours()))
}

func renderCreatorNames(names []string) string {
	lines := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lines = append(lines, "• "+html.EscapeString(name))
	}
	if len(lines) == 0 {
		return "• -"
	}
	return strings.Join(lines, "\n")
}

func botEntryLinks(botUsername string) (handle string, link string) {
	botUsername = strings.TrimSpace(strings.TrimPrefix(botUsername, "@"))
	if botUsername == "" {
		return "", ""
	}
	return "@" + botUsername, "t.me/" + botUsername
}
