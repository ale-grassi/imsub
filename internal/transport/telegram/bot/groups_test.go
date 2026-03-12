package bot

import (
	"testing"

	"imsub/internal/core"
	"imsub/internal/usecase"
)

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
