package usecase

import (
	"context"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type groupMemberTagUpdateStoreStub struct {
	ownedCreatorFn func(context.Context, int64) (core.Creator, bool, error)
	groupByChatFn  func(context.Context, int64) (core.ManagedGroup, bool, error)
	upsertFn       func(context.Context, core.ManagedGroup) error
}

func (s groupMemberTagUpdateStoreStub) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	return s.ownedCreatorFn(ctx, ownerTelegramID)
}

func (s groupMemberTagUpdateStoreStub) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	return s.groupByChatFn(ctx, chatID)
}

func (s groupMemberTagUpdateStoreStub) UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error {
	return s.upsertFn(ctx, group)
}

type groupMemberTagUpdateObserverStub struct {
	events []events.Event
}

func (o *groupMemberTagUpdateObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestUpdateGroupMemberTagNotManaged(t *testing.T) {
	t.Parallel()

	obs := &groupMemberTagUpdateObserverStub{}
	uc := NewGroupMemberTagUpdateUseCase(groupMemberTagUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) { return core.Creator{}, false, nil },
		groupByChatFn:  func(context.Context, int64) (core.ManagedGroup, bool, error) { return core.ManagedGroup{}, false, nil },
		upsertFn:       func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupMemberTagSync(t.Context(), 7, 100, true)
	if err != nil {
		t.Fatalf("UpdateGroupMemberTagSync() error = %v", err)
	}
	if got.Outcome != UpdateGroupMemberTagOutcomeNotManaged {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupMemberTagOutcomeNotManaged)
	}
	want := []events.Event{{Name: events.NameGroupMemberTagUpdate, Outcome: "not_managed"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupMemberTagNotOwner(t *testing.T) {
	t.Parallel()

	obs := &groupMemberTagUpdateObserverStub{}
	uc := NewGroupMemberTagUpdateUseCase(groupMemberTagUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c2"}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupMemberTagSync(t.Context(), 7, 100, true)
	if err != nil {
		t.Fatalf("UpdateGroupMemberTagSync() error = %v", err)
	}
	if got.Outcome != UpdateGroupMemberTagOutcomeNotOwner {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupMemberTagOutcomeNotOwner)
	}
	want := []events.Event{{Name: events.NameGroupMemberTagUpdate, Outcome: "not_owner"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupMemberTagUnchanged(t *testing.T) {
	t.Parallel()

	obs := &groupMemberTagUpdateObserverStub{}
	upserted := false
	uc := NewGroupMemberTagUpdateUseCase(groupMemberTagUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error {
			upserted = true
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupMemberTagSync(t.Context(), 7, 100, true)
	if err != nil {
		t.Fatalf("UpdateGroupMemberTagSync() error = %v", err)
	}
	if got.Outcome != UpdateGroupMemberTagOutcomeUnchanged || upserted {
		t.Fatalf("got = %+v upserted=%v", got, upserted)
	}
	want := []events.Event{{Name: events.NameGroupMemberTagUpdate, Outcome: "unchanged"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupMemberTagUpdated(t *testing.T) {
	t.Parallel()

	obs := &groupMemberTagUpdateObserverStub{}
	var saved core.ManagedGroup
	uc := NewGroupMemberTagUpdateUseCase(groupMemberTagUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{
				ChatID:               100,
				CreatorID:            "c1",
				GroupName:            "VIP",
				Policy:               core.GroupPolicyObserve,
				Language:             "en",
				RegistrationThreadID: 321,
			}, true, nil
		},
		upsertFn: func(_ context.Context, group core.ManagedGroup) error {
			saved = group
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupMemberTagSync(t.Context(), 7, 100, true)
	if err != nil {
		t.Fatalf("UpdateGroupMemberTagSync() error = %v", err)
	}
	if got.Outcome != UpdateGroupMemberTagOutcomeUpdated {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupMemberTagOutcomeUpdated)
	}
	if !saved.MemberTagSyncEnabled || saved.RegistrationThreadID != 321 || saved.CreatorID != "c1" {
		t.Fatalf("saved group = %+v", saved)
	}
	want := []events.Event{{Name: events.NameGroupMemberTagUpdate, Outcome: "updated"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
