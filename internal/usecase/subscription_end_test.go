package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type subscriptionEndServiceStub struct {
	prepareFn func(context.Context, string, string, string, string) (core.PreparedEnd, error)
	cancelFn  func(context.Context, string, string) error
}

func (s subscriptionEndServiceStub) PrepareEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) (core.PreparedEnd, error) {
	return s.prepareFn(ctx, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin)
}

func (s subscriptionEndServiceStub) CancelGrace(ctx context.Context, broadcasterID, twitchUserID string) error {
	if s.cancelFn != nil {
		return s.cancelFn(ctx, broadcasterID, twitchUserID)
	}
	return nil
}

type subscriptionEndObserverStub struct {
	events []events.Event
}

func (o *subscriptionEndObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestSubscriptionEndPrepareApplied(t *testing.T) {
	t.Parallel()

	obs := &subscriptionEndObserverStub{}
	uc := NewSubscriptionEndUseCase(subscriptionEndServiceStub{
		prepareFn: func(context.Context, string, string, string, string) (core.PreparedEnd, error) {
			return core.PreparedEnd{Found: true, TelegramUserID: 7}, nil
		},
	}, obs)

	got, err := uc.Prepare(t.Context(), "c1", "alpha", "u1", "viewer")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if got.ResultLabel != subscriptionEndResultApplied || !got.Prepared.Found {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameSubscriptionEnd, Outcome: subscriptionEndResultApplied}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestSubscriptionEndPrepareMissing(t *testing.T) {
	t.Parallel()

	obs := &subscriptionEndObserverStub{}
	uc := NewSubscriptionEndUseCase(subscriptionEndServiceStub{
		prepareFn: func(context.Context, string, string, string, string) (core.PreparedEnd, error) {
			return core.PreparedEnd{Found: false}, nil
		},
	}, obs)

	got, err := uc.Prepare(t.Context(), "c1", "alpha", "u1", "viewer")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if got.ResultLabel != subscriptionEndResultMissing || got.Prepared.Found {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameSubscriptionEnd, Outcome: subscriptionEndResultMissing}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestSubscriptionEndPrepareFailure(t *testing.T) {
	t.Parallel()

	obs := &subscriptionEndObserverStub{}
	uc := NewSubscriptionEndUseCase(subscriptionEndServiceStub{
		prepareFn: func(context.Context, string, string, string, string) (core.PreparedEnd, error) {
			return core.PreparedEnd{}, errors.New("boom")
		},
	}, obs)

	got, err := uc.Prepare(t.Context(), "c1", "alpha", "u1", "viewer")
	if err == nil {
		t.Fatal("Prepare() error = nil, want non-nil")
	}
	if got.ResultLabel != subscriptionEndResultFailed {
		t.Fatalf("ResultLabel = %q, want %q", got.ResultLabel, subscriptionEndResultFailed)
	}
	want := []events.Event{{Name: events.NameSubscriptionEnd, Outcome: subscriptionEndResultFailed}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
