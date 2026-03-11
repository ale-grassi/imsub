package bot

import (
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
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

func TestBuildViewerErrorViewIncludesMainMenu(t *testing.T) {
	t.Parallel()
	view := buildViewerErrorView("en")
	if view.text != i18n.Translate("en", msgViewerError) {
		t.Fatalf("buildViewerErrorView() text = %q, want %q", view.text, i18n.Translate("en", msgViewerError))
	}
	if view.opts.Markup == nil {
		t.Fatalf("buildViewerErrorView() = %+v, want markup", view)
	}
	if got := len(view.opts.Markup.InlineKeyboard); got != 2 {
		t.Fatalf("buildViewerErrorView() rows = %d, want 2", got)
	}
	if got, want := view.opts.Markup.InlineKeyboard[0][0].CallbackData, viewerMainMenuCallbacks().Refresh; got != want {
		t.Fatalf("buildViewerErrorView() refresh callback = %q, want %q", got, want)
	}
	if got, want := view.opts.Markup.InlineKeyboard[1][0].CallbackData, viewerMainMenuCallbacks().Reset; got != want {
		t.Fatalf("buildViewerErrorView() reset callback = %q, want %q", got, want)
	}
}

func TestBuildCreatorLinkErrorView(t *testing.T) {
	t.Parallel()
	view := buildCreatorLinkErrorView("en")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorLinkErrorView() = %+v, want text and markup", view)
	}
}

func TestBuildCreatorOAuthFailureViewReconnectMismatchReturnsText(t *testing.T) {
	t.Parallel()
	view := buildCreatorOAuthFailureView("en", msgCreatorReconnectMismatch)
	if view.text == "" {
		t.Fatalf("buildCreatorOAuthFailureView() = %+v, want text", view)
	}
}

func TestBuildCreatorReconnectRequiredViewIncludesMarkup(t *testing.T) {
	t.Parallel()
	view := buildCreatorReconnectRequiredView("en", "https://example.com")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorReconnectRequiredView() = %+v, want text and markup", view)
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

	view := buildSubscriptionStartView("en", "streamer1", core.JoinTargets{
		JoinLinks: []core.JoinLink{{
			CreatorName: "streamer1",
			GroupName:   "VIP",
			InviteLink:  "https://t.me/+invite",
		}},
	})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildSubscriptionStartView() = %+v, want text and markup", view)
	}
}

func TestBuildGroupBotRemovedOwnerViewEscapesGroupName(t *testing.T) {
	t.Parallel()

	view := buildGroupBotRemovedOwnerView("en", "<VIP>", false)
	if !strings.Contains(view.text, "&lt;VIP&gt;") {
		t.Fatalf("buildGroupBotRemovedOwnerView() text = %q, want escaped group name", view.text)
	}
	if strings.Contains(view.text, "<VIP>") {
		t.Fatalf("buildGroupBotRemovedOwnerView() text = %q, did not expect raw group name", view.text)
	}
}
