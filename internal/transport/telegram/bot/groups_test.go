package bot

import (
	"context"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
)

type bootstrapEventSinkStub struct {
	events []events.Event
}

func (s *bootstrapEventSinkStub) Emit(_ context.Context, evt events.Event) {
	s.events = append(s.events, evt)
}

func TestCheckGroupSettingsIncludesBotCapabilityWarnings(t *testing.T) {
	t.Parallel()

	got := groupSettingsEvaluation{
		botCapabilities: groupCapabilityEvaluation{botMissing: true},
		isPublic:        true,
		joinByRequest:   false,
		untrackedCount:  2,
	}.issues("en")
	if len(got) == 0 {
		t.Fatal("groupSettingsEvaluation.issues() = empty, want warnings")
	}
}

func TestFormatGroupSettingsResultWithWarnings(t *testing.T) {
	t.Parallel()
	if got := formatGroupSettingsResult("en", []string{"warning"}); got == "" {
		t.Fatal("formatGroupSettingsResult() = empty, want text")
	}
}

func TestBuildGroupRegistrationViewTakenByOther(t *testing.T) {
	t.Parallel()
	if _, ok := buildGroupRegistrationView("en", 1, usecase.RegisterGroupResult{
		Outcome:          usecase.RegisterGroupOutcomeTakenByOther,
		OtherCreatorName: "other",
	}); ok {
		t.Fatal("buildGroupRegistrationView() ok = true, want false")
	}
}

func TestBuildGroupRegistrationViewAlreadyLinked(t *testing.T) {
	t.Parallel()
	if _, ok := buildGroupRegistrationView("en", 1, usecase.RegisterGroupResult{
		Outcome: usecase.RegisterGroupOutcomeAlreadyLinked,
		Creator: core.Creator{TwitchLogin: "creator"},
	}); ok {
		t.Fatal("buildGroupRegistrationView() ok = true, want false")
	}
}

func TestBuildGroupRegistrationViewRegistered(t *testing.T) {
	t.Parallel()
	if _, ok := buildGroupRegistrationView("en", 1, usecase.RegisterGroupResult{
		Outcome: usecase.RegisterGroupOutcomeRegistered,
		Creator: core.Creator{TwitchLogin: "creator"},
	}); !ok {
		t.Fatal("buildGroupRegistrationView() ok = false, want true")
	}
}

func TestBuildGroupRegistrationViewUnsupportedOutcome(t *testing.T) {
	t.Parallel()
	if _, ok := buildGroupRegistrationView("en", 1, usecase.RegisterGroupResult{}); ok {
		t.Fatal("buildGroupRegistrationView() ok = true, want false")
	}
}

func TestGroupCapabilityEvaluationIssuesBotMissing(t *testing.T) {
	t.Parallel()
	if got := (groupCapabilityEvaluation{botMissing: true}).issues("en"); len(got) == 0 {
		t.Fatal("groupCapabilityEvaluation.issues() = empty, want warnings")
	}
}

func TestGroupSettingsEvaluationIssues(t *testing.T) {
	t.Parallel()
	got := groupSettingsEvaluation{
		botCapabilities: groupCapabilityEvaluation{canInviteUsers: false, canRestrictUsers: false},
		isPublic:        true,
		joinByRequest:   false,
		untrackedCount:  3,
	}.issues("en")
	if len(got) < 3 {
		t.Fatalf("groupSettingsEvaluation.issues() len = %d, want multiple warnings", len(got))
	}
}

func TestBuildGroupReplyView(t *testing.T) {
	t.Parallel()
	view := buildGroupReplyView("en", msgGroupNotGroup, 10)
	if view.text == "" || view.opts.ReplyToMessageID != 10 {
		t.Fatalf("buildGroupReplyView() = %+v, want text and reply target", view)
	}
}

func TestBuildGroupSettingWarningsView(t *testing.T) {
	t.Parallel()
	view := buildGroupSettingWarningsView("en", 10, []string{"warn"})
	if view.text == "" || view.opts.ReplyToMessageID != 10 {
		t.Fatalf("buildGroupSettingWarningsView() = %+v, want text and reply target", view)
	}
}

func TestBuildGroupRegistrationPolicyPromptView(t *testing.T) {
	t.Parallel()

	view := buildGroupRegistrationPolicyPromptView("en", 10, -100, 321, 4)
	if view.text == "" || view.opts.ReplyToMessageID != 10 || view.opts.Markup == nil {
		t.Fatalf("buildGroupRegistrationPolicyPromptView() = %+v, want text reply target and markup", view)
	}
}

func TestFormatGroupPolicyLine(t *testing.T) {
	t.Parallel()

	if got := formatGroupPolicyLine("en", core.GroupPolicyObserveWarn); got == "" {
		t.Fatal("formatGroupPolicyLine() = empty, want text")
	}
}

func TestBootstrapSupportForCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		caps   groupCapabilityEvaluation
		policy core.GroupPolicy
		wantOK bool
		want   string
	}{
		{name: "bot missing", caps: groupCapabilityEvaluation{botMissing: true}, policy: core.GroupPolicyObserve, want: "bot_missing"},
		{name: "no invite", caps: groupCapabilityEvaluation{canRestrictUsers: true}, policy: core.GroupPolicyObserve, want: "bot_no_invite_rights"},
		{name: "kick needs restrict", caps: groupCapabilityEvaluation{canInviteUsers: true}, policy: core.GroupPolicyKick, want: "bot_no_restrict_rights"},
		{name: "observe without restrict allowed", caps: groupCapabilityEvaluation{canInviteUsers: true}, policy: core.GroupPolicyObserve, wantOK: true},
		{name: "kick supported", caps: groupCapabilityEvaluation{canInviteUsers: true, canRestrictUsers: true}, policy: core.GroupPolicyKick, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotOK, gotReason := bootstrapSupportForCapabilities(tc.caps, tc.policy)
			if gotOK != tc.wantOK || gotReason != tc.want {
				t.Fatalf("bootstrapSupportForCapabilities(%+v, %q) = (%v, %q), want (%v, %q)", tc.caps, tc.policy, gotOK, gotReason, tc.wantOK, tc.want)
			}
		})
	}
}

func TestDispatchGroupRegistrationFollowUpEmitsDisabledBootstrapOutcome(t *testing.T) {
	t.Parallel()

	sink := &bootstrapEventSinkStub{}
	b := &Bot{events: sink}
	regRes := usecase.RegisterGroupResult{
		Outcome: usecase.RegisterGroupOutcomeRegistered,
		ExistingGroup: core.ManagedGroup{
			ChatID:    42,
			CreatorID: "creator-1",
			Policy:    core.GroupPolicyObserve,
		},
	}

	b.dispatchGroupRegistrationFollowUp(t.Context(), telego.Message{Chat: telego.Chat{ID: 42}}, "en", regRes, groupRegistrationView{}, 0, 0)

	if len(sink.events) != 1 {
		t.Fatalf("emitted events = %d, want 1", len(sink.events))
	}
	if got := sink.events[0].Outcome; got != "disabled" {
		t.Fatalf("event outcome = %q, want %q", got, "disabled")
	}
	if got := sink.events[0].Fields["reason"]; got != "mtproto_not_configured" {
		t.Fatalf("event reason = %q, want %q", got, "mtproto_not_configured")
	}
}
