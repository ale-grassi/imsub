package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type groupUnregistrationStoreStub struct {
	ownedCreatorFn func(context.Context, int64) (core.Creator, bool, error)
	groupByChatFn  func(context.Context, int64) (core.ManagedGroup, bool, error)
	listTrackedFn  func(context.Context, int64) ([]int64, error)
	deleteFn       func(context.Context, int64) error
}

func (s groupUnregistrationStoreStub) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	return s.ownedCreatorFn(ctx, ownerTelegramID)
}
func (s groupUnregistrationStoreStub) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	return s.groupByChatFn(ctx, chatID)
}
func (s groupUnregistrationStoreStub) ListTrackedGroupMemberIDs(ctx context.Context, chatID int64) ([]int64, error) {
	return s.listTrackedFn(ctx, chatID)
}
func (s groupUnregistrationStoreStub) DeleteManagedGroup(ctx context.Context, chatID int64) error {
	return s.deleteFn(ctx, chatID)
}

type groupUnregistrationCleanerStub struct {
	deleteFn func(context.Context, string) error
}

func (s groupUnregistrationCleanerStub) DeleteEventSubsForCreator(ctx context.Context, creatorID string) error {
	return s.deleteFn(ctx, creatorID)
}

type groupUnregistrationKickerStub struct {
	kickFn func(context.Context, int64, int64) error
}

func (s groupUnregistrationKickerStub) kick(ctx context.Context, groupChatID int64, telegramUserID int64) error {
	return s.kickFn(ctx, groupChatID, telegramUserID)
}

type groupUnregistrationObserverStub struct {
	events []events.Event
}

func (o *groupUnregistrationObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestUnregisterGroupNotManaged(t *testing.T) {
	t.Parallel()

	obs := &groupUnregistrationObserverStub{}
	uc := NewGroupUnregistrationUseCase(groupUnregistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) { return core.Creator{}, false, nil },
		groupByChatFn:  func(context.Context, int64) (core.ManagedGroup, bool, error) { return core.ManagedGroup{}, false, nil },
		listTrackedFn:  func(context.Context, int64) ([]int64, error) { return nil, nil },
		deleteFn:       func(context.Context, int64) error { return nil },
	}, nil, nil, obs)

	got, err := uc.UnregisterGroup(t.Context(), 7, 100, core.CreatorResetKeepMembers)
	if err != nil {
		t.Fatalf("UnregisterGroup() error = %v", err)
	}
	if got.Outcome != UnregisterGroupOutcomeNotManaged {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UnregisterGroupOutcomeNotManaged)
	}
	want := []events.Event{{Name: events.NameGroupUnregistration, Outcome: "not_managed"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUnregisterGroupNotOwner(t *testing.T) {
	t.Parallel()

	obs := &groupUnregistrationObserverStub{}
	uc := NewGroupUnregistrationUseCase(groupUnregistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c2"}, true, nil
		},
		listTrackedFn: func(context.Context, int64) ([]int64, error) { return nil, nil },
		deleteFn:      func(context.Context, int64) error { return nil },
	}, nil, nil, obs)

	got, err := uc.UnregisterGroup(t.Context(), 7, 100, core.CreatorResetKeepMembers)
	if err != nil {
		t.Fatalf("UnregisterGroup() error = %v", err)
	}
	if got.Outcome != UnregisterGroupOutcomeNotOwner {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UnregisterGroupOutcomeNotOwner)
	}
	want := []events.Event{{Name: events.NameGroupUnregistration, Outcome: "not_owner"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUnregisterGroupSuccess(t *testing.T) {
	t.Parallel()

	obs := &groupUnregistrationObserverStub{}
	deleted := false
	cleaned := false
	uc := NewGroupUnregistrationUseCase(groupUnregistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1"}, true, nil
		},
		listTrackedFn: func(context.Context, int64) ([]int64, error) { return nil, nil },
		deleteFn: func(context.Context, int64) error {
			deleted = true
			return nil
		},
	}, groupUnregistrationCleanerStub{
		deleteFn: func(context.Context, string) error {
			cleaned = true
			return nil
		},
	}, nil, obs)

	got, err := uc.UnregisterGroup(t.Context(), 7, 100, core.CreatorResetKeepMembers)
	if err != nil {
		t.Fatalf("UnregisterGroup() error = %v", err)
	}
	if got.Outcome != UnregisterGroupOutcomeUnregistered || !deleted || !cleaned || got.CleanupFailed {
		t.Fatalf("got = %+v deleted=%v cleaned=%v", got, deleted, cleaned)
	}
	want := []events.Event{{Name: events.NameGroupUnregistration, Outcome: "unregistered"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUnregisterGroupCleanupLag(t *testing.T) {
	t.Parallel()

	obs := &groupUnregistrationObserverStub{}
	uc := NewGroupUnregistrationUseCase(groupUnregistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1"}, true, nil
		},
		listTrackedFn: func(context.Context, int64) ([]int64, error) { return nil, nil },
		deleteFn:      func(context.Context, int64) error { return nil },
	}, groupUnregistrationCleanerStub{
		deleteFn: func(context.Context, string) error { return errors.New("boom") },
	}, nil, obs)

	got, err := uc.UnregisterGroup(t.Context(), 7, 100, core.CreatorResetKeepMembers)
	if err != nil {
		t.Fatalf("UnregisterGroup() error = %v", err)
	}
	if got.Outcome != UnregisterGroupOutcomeUnregisteredCleanupLag || !got.CleanupFailed {
		t.Fatalf("got = %+v", got)
	}
	want := []events.Event{{Name: events.NameGroupUnregistration, Outcome: "unregistered_cleanup_lag"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUnregisterGroupKickTrackedMembers(t *testing.T) {
	t.Parallel()

	obs := &groupUnregistrationObserverStub{}
	var kicked [][2]int64
	uc := NewGroupUnregistrationUseCase(groupUnregistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1"}, true, nil
		},
		listTrackedFn: func(context.Context, int64) ([]int64, error) { return []int64{9, 8}, nil },
		deleteFn:      func(context.Context, int64) error { return nil },
	}, nil, groupUnregistrationKickerStub{
		kickFn: func(_ context.Context, groupChatID int64, telegramUserID int64) error {
			kicked = append(kicked, [2]int64{groupChatID, telegramUserID})
			if telegramUserID == 8 {
				return errors.New("boom")
			}
			return nil
		},
	}.kick, obs)

	got, err := uc.UnregisterGroup(t.Context(), 7, 100, core.CreatorResetKickTrackedMembers)
	if err != nil {
		t.Fatalf("UnregisterGroup() error = %v", err)
	}
	if got.TargetedMembershipCount != 2 || got.KickFailureCount != 1 || got.MemberAction != core.CreatorResetKickTrackedMembers {
		t.Fatalf("got = %+v", got)
	}
	if !slices.Equal(kicked, [][2]int64{{100, 9}, {100, 8}}) {
		t.Fatalf("kicked = %v, want %v", kicked, [][2]int64{{100, 9}, {100, 8}})
	}
}
