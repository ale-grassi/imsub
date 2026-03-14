package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type creatorStatusServiceStub struct {
	loadOwnedCreatorFn  func(context.Context, int64) (core.Creator, bool, error)
	loadManagedGroupsFn func(context.Context, string) ([]core.ManagedGroup, error)
	loadStatusFn        func(context.Context, core.Creator) (core.Status, error)
}

func (s creatorStatusServiceStub) LoadOwnedCreator(ctx context.Context, telegramUserID int64) (core.Creator, bool, error) {
	return s.loadOwnedCreatorFn(ctx, telegramUserID)
}

func (s creatorStatusServiceStub) LoadManagedGroups(ctx context.Context, creatorID string) ([]core.ManagedGroup, error) {
	return s.loadManagedGroupsFn(ctx, creatorID)
}

func (s creatorStatusServiceStub) LoadStatus(ctx context.Context, creator core.Creator) (core.Status, error) {
	return s.loadStatusFn(ctx, creator)
}

type creatorStatusObserverStub struct {
	events []events.Event
}

func (o *creatorStatusObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestCreatorStatusLoadStatusUnlinked(t *testing.T) {
	t.Parallel()

	obs := &creatorStatusObserverStub{}
	uc := NewCreatorStatusUseCase(creatorStatusServiceStub{
		loadOwnedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{}, false, nil
		},
		loadManagedGroupsFn: func(context.Context, string) ([]core.ManagedGroup, error) {
			return nil, nil
		},
		loadStatusFn: func(context.Context, core.Creator) (core.Status, error) {
			return core.Status{}, nil
		},
	}, obs)

	got, err := uc.LoadStatus(t.Context(), 7)
	if err != nil {
		t.Fatalf("LoadStatus() error = %v", err)
	}
	if got.HasCreator {
		t.Fatalf("HasCreator = true, want false")
	}
	want := []events.Event{{Name: events.NameCreatorStatus, Outcome: "unlinked", Fields: map[string]string{"creator_id": ""}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorStatusLoadStatusLoaded(t *testing.T) {
	t.Parallel()

	obs := &creatorStatusObserverStub{}
	uc := NewCreatorStatusUseCase(creatorStatusServiceStub{
		loadOwnedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1", TwitchLogin: "alpha"}, true, nil
		},
		loadManagedGroupsFn: func(context.Context, string) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}}, nil
		},
		loadStatusFn: func(context.Context, core.Creator) (core.Status, error) {
			return core.Status{EventSub: core.EventSubActive, HasSubscriberCount: true, SubscriberCount: 12}, nil
		},
	}, obs)

	got, err := uc.LoadStatus(t.Context(), 7)
	if err != nil {
		t.Fatalf("LoadStatus() error = %v", err)
	}
	if !got.HasCreator || got.Creator.ID != "c1" || len(got.Groups) != 1 || !got.Status.HasSubscriberCount || got.IsDegraded {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameCreatorStatus, Outcome: "loaded", Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorStatusLoadStatusDegraded(t *testing.T) {
	t.Parallel()

	obs := &creatorStatusObserverStub{}
	uc := NewCreatorStatusUseCase(creatorStatusServiceStub{
		loadOwnedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1", TwitchLogin: "alpha"}, true, nil
		},
		loadManagedGroupsFn: func(context.Context, string) ([]core.ManagedGroup, error) {
			return nil, errors.New("groups boom")
		},
		loadStatusFn: func(context.Context, core.Creator) (core.Status, error) {
			return core.Status{EventSub: core.EventSubUnknown}, errors.New("status boom")
		},
	}, obs)

	got, err := uc.LoadStatus(t.Context(), 7)
	if err == nil {
		t.Fatal("LoadStatus() error = nil, want non-nil")
	}
	if !got.HasCreator || !got.IsDegraded || got.GroupsError == nil || got.StatusError == nil {
		t.Fatalf("got = %+v", got)
	}
	if !errors.Is(err, got.GroupsError) || !errors.Is(err, got.StatusError) {
		t.Fatalf("LoadStatus() error = %v, want join(%v,%v)", err, got.GroupsError, got.StatusError)
	}
	want := []events.Event{{Name: events.NameCreatorStatus, Outcome: "degraded", Fields: map[string]string{"creator_id": "c1"}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestCreatorStatusLoadStatusFailure(t *testing.T) {
	t.Parallel()

	obs := &creatorStatusObserverStub{}
	uc := NewCreatorStatusUseCase(creatorStatusServiceStub{
		loadOwnedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{}, false, errors.New("boom")
		},
		loadManagedGroupsFn: func(context.Context, string) ([]core.ManagedGroup, error) {
			return nil, nil
		},
		loadStatusFn: func(context.Context, core.Creator) (core.Status, error) {
			return core.Status{}, nil
		},
	}, obs)

	_, err := uc.LoadStatus(t.Context(), 7)
	if err == nil {
		t.Fatal("LoadStatus() error = nil, want non-nil")
	}
	want := []events.Event{{Name: events.NameCreatorStatus, Outcome: "failed", Fields: map[string]string{"creator_id": ""}}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
