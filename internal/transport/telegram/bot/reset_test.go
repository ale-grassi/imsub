package bot

import (
	"strings"
	"testing"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/usecase"
)

func ensureResetTestI18n(t *testing.T) {
	t.Helper()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}
}

func TestBuildResetPromptView(t *testing.T) {
	t.Parallel()
	ensureResetTestI18n(t)

	view, ok := buildResetPromptView("en", core.ScopeState{
		HasIdentity: true,
		HasCreator:  true,
	}, resetOriginViewer)
	if !ok || view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildResetPromptView() = (%+v, %t), want populated view", view, ok)
	}
}

func TestBuildResetPromptViewEmpty(t *testing.T) {
	t.Parallel()
	ensureResetTestI18n(t)

	view, ok := buildResetPromptView("en", core.ScopeState{}, resetOriginViewer)
	if !ok || view.text == "" {
		t.Fatalf("buildResetPromptView() = (%+v, %t), want empty-state view", view, ok)
	}
}

func TestBuildResetExecutionView(t *testing.T) {
	t.Parallel()
	ensureResetTestI18n(t)

	view := buildResetExecutionView("en", usecase.ResetResult{Scope: usecase.ResetScopeViewer, ViewerLogin: "viewer", GroupCount: 2})
	if view.text == "" {
		t.Fatalf("buildResetExecutionView() = %+v, want text", view)
	}
}

func TestBuildResetErrorView(t *testing.T) {
	t.Parallel()
	ensureResetTestI18n(t)

	view := buildResetErrorView("en")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildResetErrorView() = %+v, want text and markup", view)
	}
}

func TestBuildResetExecutionViewCreatorIncludesCleanup(t *testing.T) {
	t.Parallel()
	ensureResetTestI18n(t)

	view := buildResetExecutionView("en", usecase.ResetResult{
		Scope:        usecase.ResetScopeCreator,
		DeletedCount: 1,
		DeletedNames: []string{"creator1"},
		CreatorCleanup: core.CreatorGroupCleanupSummary{
			Action:                  core.CreatorResetKickTrackedMembers,
			ManagedGroupCount:       2,
			TargetedMembershipCount: 4,
			KickFailureCount:        1,
		},
	})
	if view.text == "" {
		t.Fatalf("buildResetExecutionView() = %+v, want text", view)
	}
	if !containsAll(view.text, "Managed groups unlinked", "Tracked memberships targeted for kick") {
		t.Fatalf("buildResetExecutionView() text = %q, want creator cleanup details", view.text)
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
