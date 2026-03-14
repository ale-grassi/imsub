package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type creatorOAuthServiceStub struct {
	linkCreatorFn func(context.Context, string, core.OAuthStatePayload) (core.CreatorResult, error)
}

func (s creatorOAuthServiceStub) LinkCreator(ctx context.Context, code string, payload core.OAuthStatePayload) (core.CreatorResult, error) {
	return s.linkCreatorFn(ctx, code, payload)
}

type creatorOAuthObserverStub struct {
	events []events.Event
}

func (o *creatorOAuthObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestCreatorOAuthCompleteSuccess(t *testing.T) {
	t.Parallel()

	obs := &creatorOAuthObserverStub{}
	uc := NewCreatorOAuthUseCase(creatorOAuthServiceStub{
		linkCreatorFn: func(context.Context, string, core.OAuthStatePayload) (core.CreatorResult, error) {
			return core.CreatorResult{
				Creator:                core.Creator{ID: "c1", TwitchLogin: "alpha", OwnerTelegramID: 7},
				BroadcasterDisplayName: "Alpha",
			}, nil
		},
	}, obs)

	got, err := uc.Complete(t.Context(), "code", core.OAuthStatePayload{TelegramUserID: 7})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got.ResultLabel != creatorOAuthResultSuccess || got.Creator.ID != "c1" || got.BroadcasterDisplayName != "Alpha" {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameCreatorOAuth, Outcome: creatorOAuthResultSuccess, Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorOAuthCompleteMapsFlowErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "token_exchange", err: &core.FlowError{Kind: core.KindTokenExchange, Cause: errors.New("boom")}, want: creatorOAuthResultTokenExchangeFailed},
		{name: "scope_missing", err: &core.FlowError{Kind: core.KindScopeMissing, Cause: errors.New("boom")}, want: creatorOAuthResultScopeMissing},
		{name: "user_info", err: &core.FlowError{Kind: core.KindUserInfo, Cause: errors.New("boom")}, want: creatorOAuthResultUserInfoFailed},
		{name: "store", err: &core.FlowError{Kind: core.KindStore, Cause: errors.New("boom")}, want: creatorOAuthResultStoreFailed},
		{name: "save", err: &core.FlowError{Kind: core.KindSave, Cause: errors.New("boom")}, want: creatorOAuthResultStoreFailed},
		{name: "mismatch", err: &core.FlowError{Kind: core.KindCreatorMismatch, Cause: errors.New("boom")}, want: creatorOAuthResultMismatch},
		{name: "unknown", err: errors.New("boom"), want: creatorOAuthResultStoreFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			obs := &creatorOAuthObserverStub{}
			uc := NewCreatorOAuthUseCase(creatorOAuthServiceStub{
				linkCreatorFn: func(context.Context, string, core.OAuthStatePayload) (core.CreatorResult, error) {
					return core.CreatorResult{}, tc.err
				},
			}, obs)

			got, err := uc.Complete(t.Context(), "code", core.OAuthStatePayload{TelegramUserID: 7})
			if err == nil {
				t.Fatal("Complete() error = nil, want non-nil")
			}
			if got.ResultLabel != tc.want {
				t.Fatalf("ResultLabel = %q, want %q", got.ResultLabel, tc.want)
			}
			want := []events.Event{{Name: events.NameCreatorOAuth, Outcome: tc.want, Fields: map[string]string{"creator_id": ""}}}
			if !slices.EqualFunc(obs.events, want, equalEvents) {
				t.Fatalf("events = %+v, want %+v", obs.events, want)
			}
		})
	}
}
