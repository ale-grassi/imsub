package bot

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	tu "github.com/mymmrac/telego/telegoutil"
)

type sharedView struct {
	text string
	opts client.MessageOptions
}

type textSection struct {
	text string
}

func buildTextView(lang, key string) sharedView {
	return sharedView{text: i18n.Translate(lang, key)}
}

func buildMainMenuTextView(lang, key string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, key),
		opts: client.MessageOptions{Markup: viewerMainMenuMarkup(lang)},
	}
}

func buildViewerErrorView(lang string) sharedView {
	return buildMainMenuTextView(lang, msgViewerError)
}

func buildInfoView(lang string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgCmdInfoHTML),
		opts: client.MessageOptions{DisablePreview: true},
	}
}

func buildCreatorStatusErrorView(lang string) sharedView {
	return buildTextView(lang, msgErrLoadStatus)
}

func buildCreatorLinkErrorView(lang string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgErrCreatorLink),
		opts: client.MessageOptions{Markup: creatorMainMenuMarkup(lang)},
	}
}

func buildSubscriptionEndView(lang, broadcasterLogin string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgSubEndPartial), html.EscapeString(broadcasterLogin)),
		opts: client.MessageOptions{
			Markup: ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
}

func buildSubscriptionGraceStartView(lang, broadcasterLogin string, dueAt time.Time) sharedView {
	remainingHours := graceRemainingHours(dueAt, time.Now().UTC())
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgSubGraceStart),
			html.EscapeString(broadcasterLogin),
			remainingHours,
		),
		opts: client.MessageOptions{
			Markup: ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
}

func buildSubscriptionGraceExpiredView(lang, broadcasterLogin string) sharedView {
	return buildSubscriptionEndView(lang, broadcasterLogin)
}

func buildSubscriptionStartView(lang, broadcasterLogin string, targets core.JoinTargets) sharedView {
	joinRows := renderJoinButtons(targets, lang)
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgSubStartReady), html.EscapeString(broadcasterLogin)),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(joinRows...),
		},
	}
}

func buildGroupBotRemovedOwnerView(lang, groupName string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgGroupBotRemovedOwnerDM), html.EscapeString(groupName)),
		opts: client.MessageOptions{},
	}
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
