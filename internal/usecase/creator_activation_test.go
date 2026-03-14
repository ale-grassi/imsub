package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type creatorActivationServiceStub struct {
	ensureFn func(context.Context, []core.Creator) error
	dumpFn   func(context.Context, core.Creator) (int, error)
}

func (s creatorActivationServiceStub) EnsureEventSubForCreators(ctx context.Context, creators []core.Creator) error {
	return s.ensureFn(ctx, creators)
}

func (s creatorActivationServiceStub) DumpCurrentSubscribers(ctx context.Context, creator core.Creator) (int, error) {
	return s.dumpFn(ctx, creator)
}

type creatorActivationObserverStub struct {
	events []events.Event
}

func (o *creatorActivationObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestCreatorActivationSuccess(t *testing.T) {
	t.Parallel()

	obs := &creatorActivationObserverStub{}
	uc := NewCreatorActivationUseCase(creatorActivationServiceStub{
		ensureFn: func(context.Context, []core.Creator) error { return nil },
		dumpFn:   func(context.Context, core.Creator) (int, error) { return 12, nil },
	}, obs)

	got, err := uc.Activate(t.Context(), core.Creator{ID: "c1"})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if got.ResultLabel != creatorActivationResultSuccess || got.SubscriberCount != 12 {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameCreatorActivation, Outcome: creatorActivationResultSuccess, Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorActivationEnsureFailure(t *testing.T) {
	t.Parallel()

	obs := &creatorActivationObserverStub{}
	uc := NewCreatorActivationUseCase(creatorActivationServiceStub{
		ensureFn: func(context.Context, []core.Creator) error { return errors.New("boom") },
		dumpFn:   func(context.Context, core.Creator) (int, error) { return 0, nil },
	}, obs)

	got, err := uc.Activate(t.Context(), core.Creator{ID: "c1"})
	if err == nil {
		t.Fatal("Activate() error = nil, want non-nil")
	}
	if got.ResultLabel != creatorActivationResultEventSubFail {
		t.Fatalf("ResultLabel = %q, want %q", got.ResultLabel, creatorActivationResultEventSubFail)
	}
	want := []events.Event{{Name: events.NameCreatorActivation, Outcome: creatorActivationResultEventSubFail, Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorActivationDumpFailure(t *testing.T) {
	t.Parallel()

	obs := &creatorActivationObserverStub{}
	uc := NewCreatorActivationUseCase(creatorActivationServiceStub{
		ensureFn: func(context.Context, []core.Creator) error { return nil },
		dumpFn:   func(context.Context, core.Creator) (int, error) { return 0, errors.New("boom") },
	}, obs)

	got, err := uc.Activate(t.Context(), core.Creator{ID: "c1"})
	if err == nil {
		t.Fatal("Activate() error = nil, want non-nil")
	}
	if got.ResultLabel != creatorActivationResultDumpFail {
		t.Fatalf("ResultLabel = %q, want %q", got.ResultLabel, creatorActivationResultDumpFail)
	}
	want := []events.Event{{Name: events.NameCreatorActivation, Outcome: creatorActivationResultDumpFail, Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
