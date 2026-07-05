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

func TestBuildResetConfirmReplyViewWithCreatorAction(t *testing.T) {
	t.Parallel()

	scopes := core.ScopeState{
		HasCreator: true,
		Creator:    core.Creator{TwitchLogin: "streamer_one", TwitchDisplayName: "Streamer One"},
	}
	view := resetConfirmViewText("en", scopes, resetScopeCreator, core.CreatorResetKickTrackedMembers, nil, []string{"VIP Lounge"})
	if view.text == "" {
		t.Fatal("resetConfirmViewText() = empty, want confirm text")
	}
	reply := buildResetConfirmReplyView("en", view, resetOriginCreator, resetScopeCreator, core.CreatorResetKickTrackedMembers, true)
	if reply.opts.Markup == nil || len(reply.opts.Markup.InlineKeyboard) != 2 {
		t.Fatalf("buildResetConfirmReplyView() markup = %+v, want confirm row and back row", reply.opts.Markup)
	}
	if got, want := reply.opts.Markup.InlineKeyboard[0][0].CallbackData, resetExecuteWithActionCallback(resetOriginCreator, resetScopeCreator, core.CreatorResetKickTrackedMembers); got != want {
		t.Errorf("buildResetConfirmReplyView() confirm callback = %q, want %q", got, want)
	}
	if got, want := reply.opts.Markup.InlineKeyboard[1][0].CallbackData, resetPickCallback(resetOriginCreator, resetScopeCreator); got != want {
		t.Errorf("buildResetConfirmReplyView() back callback = %q, want %q", got, want)
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

func TestResetConfirmViewTextGuards(t *testing.T) {
	t.Parallel()

	populated := core.ScopeState{
		HasIdentity: true,
		Identity:    core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"},
		HasCreator:  true,
		Creator:     core.Creator{TwitchLogin: "streamer_one", TwitchDisplayName: "Streamer One"},
	}
	cases := []struct {
		name   string
		scopes core.ScopeState
		scope  resetScope
		empty  bool
	}{
		{name: "viewer without identity", scopes: core.ScopeState{HasCreator: true}, scope: resetScopeViewer, empty: true},
		{name: "creator without creator", scopes: core.ScopeState{HasIdentity: true}, scope: resetScopeCreator, empty: true},
		{name: "both without any scope", scopes: core.ScopeState{}, scope: resetScopeBoth, empty: true},
		{name: "unknown scope", scopes: populated, scope: resetScope("bogus"), empty: true},
		{name: "viewer populated", scopes: populated, scope: resetScopeViewer},
		{name: "creator populated", scopes: populated, scope: resetScopeCreator},
		{name: "both populated", scopes: populated, scope: resetScopeBoth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			view := resetConfirmViewText("en", tc.scopes, tc.scope, core.CreatorResetKeepMembers, []string{"Viewer Group"}, []string{"Creator Group"})
			if tc.empty != (view.text == "") {
				t.Fatalf("resetConfirmViewText(%s) text = %q, want empty=%t", tc.name, view.text, tc.empty)
			}
		})
	}
}

func TestResetConfirmViewTextListsGroupsAndSummary(t *testing.T) {
	t.Parallel()

	scopes := core.ScopeState{
		HasIdentity: true,
		Identity:    core.UserIdentity{TwitchLogin: "viewer_name", TwitchDisplayName: "Viewer Name"},
		HasCreator:  true,
		Creator:     core.Creator{TwitchLogin: "streamer_one", TwitchDisplayName: "Streamer One"},
	}
	view := resetConfirmViewText("en", scopes, resetScopeBoth, core.CreatorResetKickTrackedMembers, []string{"Viewer Group"}, []string{"Creator Group"})
	for _, want := range []string{
		"twitch.tv/viewer_name",
		"twitch.tv/streamer_one",
		"Viewer Group",
		"Creator Group",
		resetCreatorActionSummaryText("en", core.CreatorResetKickTrackedMembers, 1),
	} {
		if !strings.Contains(view.text, want) {
			t.Errorf("resetConfirmViewText(both) text = %q, want substring %q", view.text, want)
		}
	}
	if strings.Contains(view.text, "%!") {
		t.Errorf("resetConfirmViewText(both) text = %q, contains fmt error marker", view.text)
	}
}

func TestBuildResetConfirmReplyViewWithoutCreatorAction(t *testing.T) {
	t.Parallel()

	view := resetConfirmView{text: "confirm?"}
	reply := buildResetConfirmReplyView("en", view, resetOriginViewer, resetScopeViewer, core.CreatorResetKeepMembers, false)
	if reply.opts.Markup == nil || len(reply.opts.Markup.InlineKeyboard) != 2 {
		t.Fatalf("buildResetConfirmReplyView() markup = %+v, want confirm row and back row", reply.opts.Markup)
	}
	confirm := reply.opts.Markup.InlineKeyboard[0][0]
	if got, want := confirm.CallbackData, resetExecuteCallback(resetOriginViewer, resetScopeViewer); got != want {
		t.Errorf("buildResetConfirmReplyView() confirm callback = %q, want %q", got, want)
	}
	if confirm.Style != "danger" {
		t.Errorf("buildResetConfirmReplyView() confirm style = %q, want danger", confirm.Style)
	}
	if got, want := reply.opts.Markup.InlineKeyboard[1][0].CallbackData, resetBackCallback(resetOriginViewer); got != want {
		t.Errorf("buildResetConfirmReplyView() back callback = %q, want %q", got, want)
	}
}

func TestBuildResetEmptyViewNavigation(t *testing.T) {
	t.Parallel()

	view := buildResetEmptyView("en", resetPromptNavigation("en", resetOriginViewer))
	if view.opts.Markup == nil || len(view.opts.Markup.InlineKeyboard) != 1 {
		t.Fatalf("buildResetEmptyView() markup = %+v, want single navigation row", view.opts.Markup)
	}
	if got, want := view.opts.Markup.InlineKeyboard[0][0].CallbackData, resetPromptBackCallback(resetOriginViewer); got != want {
		t.Errorf("buildResetEmptyView() back callback = %q, want %q", got, want)
	}

	bare := buildResetEmptyView("en", nil)
	if bare.opts.Markup != nil {
		t.Fatalf("buildResetEmptyView(nil navigation) markup = %+v, want nil", bare.opts.Markup)
	}
}
