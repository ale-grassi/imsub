package bot

import (
	"strings"
	"testing"

	"imsub/internal/core"
	"imsub/internal/usecase"
)

func TestBuildResetPromptView(t *testing.T) {
	t.Parallel()

	view := buildResetPromptView("en", core.ScopeState{
		HasIdentity: true,
		HasCreator:  true,
	}, resetOriginViewer)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildResetPromptView() = %+v, want populated view", view)
	}
	found := false
	for _, row := range view.opts.Markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == resetExportCallback(resetOriginViewer) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("buildResetPromptView() missing export callback")
	}
}

func TestBuildResetPromptViewEmpty(t *testing.T) {
	t.Parallel()

	view := buildResetPromptView("en", core.ScopeState{}, resetOriginViewer)
	if view.text == "" {
		t.Fatalf("buildResetPromptView() = %+v, want empty-state view", view)
	}
	for _, row := range view.opts.Markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == resetExportCallback(resetOriginViewer) {
				t.Fatal("buildResetPromptView() should not include export callback for empty state")
			}
		}
	}
}

func TestBuildResetExecutionView(t *testing.T) {
	t.Parallel()

	view := buildResetExecutionView("en", usecase.ResetResult{Scope: usecase.ResetScopeViewer, ViewerLogin: "viewer", GroupCount: 2})
	if view.text == "" {
		t.Fatalf("buildResetExecutionView() = %+v, want text", view)
	}
}

func TestBuildResetErrorView(t *testing.T) {
	t.Parallel()

	view := buildResetErrorView("en")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildResetErrorView() = %+v, want text and markup", view)
	}
}

func TestBuildResetExecutionViewCreatorIncludesCleanup(t *testing.T) {
	t.Parallel()

	view := buildResetExecutionView("en", usecase.ResetResult{
		Scope:        usecase.ResetScopeCreator,
		DeletedCount: 1,
		DeletedNames: []string{"creator1"},
		CreatorCleanup: core.CreatorGroupCleanupSummary{
			Action:                  core.CreatorResetKickTrackedMembers,
			ManagedGroupCount:       2,
			GroupNames:              []string{"VIP Lounge", "Subscriber Chat"},
			TargetedMembershipCount: 4,
			KickFailureCount:        1,
		},
	})
	if view.text == "" {
		t.Fatalf("buildResetExecutionView() = %+v, want text", view)
	}
	if !containsAll(view.text, "Managed groups:", "VIP Lounge", "Current group members are being removed in the background") {
		t.Fatalf("buildResetExecutionView() text = %q, want creator cleanup details", view.text)
	}
}

func TestRenderResetViewerGroupsEmpty(t *testing.T) {
	t.Parallel()

	got := renderResetViewerGroups("en", nil)
	if got != "No groups" {
		t.Fatalf("renderResetViewerGroups() = %q, want %q", got, "No groups")
	}
}

func TestResetViewerConsequenceLine(t *testing.T) {
	t.Parallel()

	if got := resetViewerConsequenceLine("en", 0); got != "\n• No subscribers-only groups found, so you will not be removed from any groups" {
		t.Fatalf("resetViewerConsequenceLine(0) = %q, want zero-group message", got)
	}
	if got := resetViewerConsequenceLine("en", 1); got != "\n• You will be removed from these subscribers-only groups" {
		t.Fatalf("resetViewerConsequenceLine(1) = %q, want removal warning", got)
	}
}

func TestResetGroupSection(t *testing.T) {
	t.Parallel()

	if got := resetGroupSection("en", "Subscribers-only groups", nil); got != "" {
		t.Fatalf("resetGroupSection(nil) = %q, want empty string", got)
	}
	if got := resetGroupSection("en", "Subscribers-only groups", []string{"VIP Lounge"}); got != "\n<b>Subscribers-only groups:</b>\n• VIP Lounge" {
		t.Fatalf("resetGroupSection(non-empty) = %q, want rendered section", got)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
