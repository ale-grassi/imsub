package bot

import (
	"fmt"
	"html"
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

func buildViewerStatusErrorView(lang string) sharedView {
	return buildMainMenuTextView(lang, msgErrLoadStatus)
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

func buildCreatorReconnectRequiredView(lang, reconnectURL string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgCreatorReconnectNeeded),
		opts: client.MessageOptions{
			Markup: ui.CreatorStatusMenuMarkup(lang, reconnectURL, creatorStatusMenuCallbacks(false, false, false, false)),
		},
	}
}

func buildSubscriptionEndView(lang, viewerLogin, broadcasterLogin string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgSubEndPartial), html.EscapeString(viewerLogin)),
		opts: client.MessageOptions{
			Markup: ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
}

func buildSubscriptionGraceStartView(lang, viewerLogin, broadcasterLogin string, dueAt time.Time) sharedView {
	return sharedView{
		text: fmt.Sprintf(
			i18n.Translate(lang, msgSubGraceStart),
			html.EscapeString(viewerLogin),
			html.EscapeString(dueAt.UTC().Format("2006-01-02 15:04 UTC")),
		),
		opts: client.MessageOptions{
			Markup: ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
}

func buildSubscriptionGraceExpiredView(lang, viewerLogin, broadcasterLogin string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgSubGraceExpired), html.EscapeString(viewerLogin)),
		opts: client.MessageOptions{
			Markup: ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
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

func buildGroupBotRemovedOwnerView(lang, groupName string, cleanupLag bool) sharedView {
	key := msgGroupBotRemovedOwnerDM
	if cleanupLag {
		key = msgGroupBotRemovedLagDM
	}
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, key), html.EscapeString(groupName)),
		opts: client.MessageOptions{},
	}
}

func buildViewerOAuthFailureView(lang, key string) sharedView { return buildTextView(lang, key) }

func buildCreatorOAuthFailureView(lang, key string) sharedView {
	return buildTextView(lang, key)
}

func buildOAuthLoadStatusErrorView(lang string) sharedView { return buildViewerStatusErrorView(lang) }

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
