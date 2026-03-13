package bot

import (
	"testing"

	"imsub/internal/core"
)

func TestBuildViewerPromptView(t *testing.T) {
	t.Parallel()

	view := buildViewerPromptView("en", "Alice", "https://example.com")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildViewerPromptView() = %+v, want non-empty text and markup", view)
	}
}

func TestBuildViewerLinkedView(t *testing.T) {
	t.Parallel()

	view := buildViewerLinkedView("en", core.UserIdentity{TwitchLogin: "viewer1", TwitchDisplayName: "Viewer One"}, core.JoinTargets{
		ActiveCreatorNames: []string{"creator1"},
		JoinLinks: []core.JoinLink{{
			CreatorName: "creator1",
			GroupName:   "VIP",
			InviteLink:  "https://t.me/+invite",
		}},
	})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildViewerLinkedView() = %+v, want non-empty text and markup", view)
	}
}
