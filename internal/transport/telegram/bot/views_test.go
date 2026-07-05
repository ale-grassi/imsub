package bot

import (
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/ui"
)

func TestJoinNonEmptyLines(t *testing.T) {
	t.Parallel()
	if got := joinNonEmptyLines("a", "", "b"); got != "a\nb" {
		t.Fatalf("joinNonEmptyLines() = %q, want %q", got, "a\nb")
	}
}

func TestJoinNonEmptySections(t *testing.T) {
	t.Parallel()
	if got := joinNonEmptySections(textSection{text: "a"}, textSection{text: ""}, textSection{text: "b"}); got != "a\n\nb" {
		t.Fatalf("joinNonEmptySections() = %q, want %q", got, "a\n\nb")
	}
}

func TestRenderWarningBlock(t *testing.T) {
	t.Parallel()
	if got := renderWarningBlock("title", []string{"x", "y"}); got == "" {
		t.Fatal("renderWarningBlock() = empty, want text")
	}
}

func TestBuildMainMenuTextView(t *testing.T) {
	t.Parallel()
	view := buildMainMenuTextView("en", msgCmdHelp)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildMainMenuTextView() = %+v, want text and markup", view)
	}
}

func TestBuildViewerErrorViewHasNoButtons(t *testing.T) {
	t.Parallel()
	view := buildViewerErrorView("en")
	if view.text != i18n.Translate("en", msgViewerError) {
		t.Fatalf("buildViewerErrorView() text = %q, want %q", view.text, i18n.Translate("en", msgViewerError))
	}
	if !strings.Contains(view.text, "/start") {
		t.Fatalf("buildViewerErrorView() text = %q, want /start guidance", view.text)
	}
	if !strings.Contains(view.text, "/reset") {
		t.Fatalf("buildViewerErrorView() text = %q, want /reset guidance", view.text)
	}
	if view.opts.Markup != nil {
		t.Fatalf("buildViewerErrorView() markup = %+v, want nil", view.opts.Markup)
	}
}

func TestBuildCreatorLinkErrorView(t *testing.T) {
	t.Parallel()
	view := buildCreatorLinkErrorView("en")
	if view.text == "" {
		t.Fatalf("buildCreatorLinkErrorView() = %+v, want text", view)
	}
	if !strings.Contains(view.text, "/creator") {
		t.Fatalf("buildCreatorLinkErrorView() text = %q, want /creator guidance", view.text)
	}
	if view.opts.Markup != nil {
		t.Fatalf("buildCreatorLinkErrorView() markup = %+v, want nil", view.opts.Markup)
	}
}

func TestBuildTextViewCreatorReconnectMismatchReturnsText(t *testing.T) {
	t.Parallel()
	view := buildTextView("en", msgCreatorReconnectMismatch)
	if view.text == "" {
		t.Fatalf("buildTextView() = %+v, want text", view)
	}
}

func TestBuildSubscriptionEndViewIncludesSubscribeMarkup(t *testing.T) {
	t.Parallel()
	view := buildSubscriptionEndView("en", "streamer1")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildSubscriptionEndView() = %+v, want text and markup", view)
	}
}

func TestBuildSubscriptionEndViewEscapesBroadcasterLogin(t *testing.T) {
	t.Parallel()

	view := buildSubscriptionEndView("en", "<streamer>")
	if !strings.Contains(view.text, "&lt;streamer&gt;") {
		t.Fatalf("buildSubscriptionEndView() text = %q, want escaped broadcaster login", view.text)
	}
	if strings.Contains(view.text, "<streamer>") {
		t.Fatalf("buildSubscriptionEndView() text = %q, did not expect raw broadcaster login", view.text)
	}
}

func TestGraceRemainingHoursRoundsUp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 11, 10, 0, 0, 0, time.UTC)
	if got := graceRemainingHours(now.Add(72*time.Hour-time.Minute), now); got != 72 {
		t.Fatalf("graceRemainingHours() = %d, want 72", got)
	}
	if got := graceRemainingHours(now.Add(30*time.Minute), now); got != 1 {
		t.Fatalf("graceRemainingHours() = %d, want 1", got)
	}
	if got := graceRemainingHours(now, now); got != 0 {
		t.Fatalf("graceRemainingHours() = %d, want 0", got)
	}
}

func TestBuildSubscriptionStartViewIncludesJoinButtons(t *testing.T) {
	t.Parallel()

	view := buildSubscriptionStartView("en", core.UserIdentity{
		TwitchLogin:       "viewer_one",
		TwitchDisplayName: "Viewer One",
	}, "streamer1", core.JoinTargets{
		ActiveCreatorNames: []string{"streamer1"},
		JoinLinks: []core.JoinLink{{
			CreatorName: "streamer1",
			GroupName:   "VIP",
			InviteLink:  "https://t.me/+invite",
		}},
	})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildSubscriptionStartView() = %+v, want text and markup", view)
	}
	if !strings.Contains(view.text, "New subscription active") {
		t.Fatalf("buildSubscriptionStartView() text = %q, want notification intro", view.text)
	}
	if !strings.Contains(view.text, "<code>Viewer One</code>") {
		t.Fatalf("buildSubscriptionStartView() text = %q, want viewer account", view.text)
	}
	if !strings.Contains(view.text, "Your subscription to <b>streamer1</b> is active.") {
		t.Fatalf("buildSubscriptionStartView() text = %q, want broadcaster-specific notice", view.text)
	}
	if strings.Contains(view.text, "Twitch connected") {
		t.Fatalf("buildSubscriptionStartView() text = %q, did not expect generic connected heading", view.text)
	}
	if !strings.Contains(view.text, "<b>Account:</b>") {
		t.Fatalf("buildSubscriptionStartView() text = %q, want account line under intro", view.text)
	}
	if !strings.Contains(view.text, "If a link expires, tap <b>Refresh</b> to load updated links") {
		t.Fatalf("buildSubscriptionStartView() text = %q, want linked status guidance", view.text)
	}
	if got, want := len(view.opts.Markup.InlineKeyboard), 3; got != want {
		t.Fatalf("buildSubscriptionStartView() rows = %d, want %d", got, want)
	}
	if got, want := view.opts.Markup.InlineKeyboard[1][0].CallbackData, viewerRefreshCallback(); got != want {
		t.Fatalf("buildSubscriptionStartView() refresh callback = %q, want %q", got, want)
	}
	if got, want := view.opts.Markup.InlineKeyboard[2][0].CallbackData, resetOpenCallback(resetOriginViewer); got != want {
		t.Fatalf("buildSubscriptionStartView() reset callback = %q, want %q", got, want)
	}
}

func TestBuildGroupBotRemovedOwnerViewEscapesGroupName(t *testing.T) {
	t.Parallel()

	view := buildGroupBotRemovedOwnerView("en", "<VIP>")
	if !strings.Contains(view.text, "&lt;VIP&gt;") {
		t.Fatalf("buildGroupBotRemovedOwnerView() text = %q, want escaped group name", view.text)
	}
	if strings.Contains(view.text, "<VIP>") {
		t.Fatalf("buildGroupBotRemovedOwnerView() text = %q, did not expect raw group name", view.text)
	}
}

func TestSharedScreenViewFromHTMLNormalizesHeader(t *testing.T) {
	t.Parallel()

	view := sharedScreenViewFromHTML(ui.TrustedHTML("✅ <b>Done</b>\nBody line"), nil, nil, false)
	if got, want := view.text, "✅ <b>Done</b>\n\nBody line"; got != want {
		t.Fatalf("sharedScreenViewFromHTML() text = %q, want header normalized to %q", got, want)
	}
}

func TestSharedScreenViewFromHTMLWithoutHeaderKeepsText(t *testing.T) {
	t.Parallel()

	const plain = "no header here, just <code>text</code>"
	view := sharedScreenViewFromHTML(ui.TrustedHTML(plain), nil, nil, true)
	if view.text != plain {
		t.Fatalf("sharedScreenViewFromHTML() text = %q, want unchanged %q", view.text, plain)
	}
	if !view.opts.DisablePreview {
		t.Fatal("sharedScreenViewFromHTML() DisablePreview = false, want true")
	}
}
