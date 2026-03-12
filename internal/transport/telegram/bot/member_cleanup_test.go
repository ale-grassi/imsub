package bot

import (
	"testing"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
)

func TestBuildMemberCleanupResultViewSkipsSuccessfulCleanup(t *testing.T) {
	t.Parallel()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view, ok := buildMemberCleanupResultView("en", core.MemberCleanupResult{
		Kind:           core.MemberCleanupKindGroupUnregistration,
		GroupName:      "VIP Lounge",
		TargetedCount:  3,
		SucceededCount: 3,
		FailedCount:    0,
	})
	if ok {
		t.Fatalf("buildMemberCleanupResultView() ok = true, want false")
	}
	if view.text != "" {
		t.Fatalf("buildMemberCleanupResultView() text = %q, want empty", view.text)
	}
}

func TestBuildMemberCleanupResultViewKeepsPartialAndFailedNotifications(t *testing.T) {
	t.Parallel()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	partialView, ok := buildMemberCleanupResultView("en", core.MemberCleanupResult{
		Kind:              core.MemberCleanupKindCreatorReset,
		CreatorLogin:      "streamer_one",
		ManagedGroupCount: 2,
		GroupNames:        []string{"VIP Lounge", "Subscriber Chat"},
		TargetedCount:     4,
		SucceededCount:    3,
		FailedCount:       1,
	})
	if !ok || partialView.text == "" {
		t.Fatalf("buildMemberCleanupResultView(partial) = (%+v, %t), want populated view", partialView, ok)
	}
}
