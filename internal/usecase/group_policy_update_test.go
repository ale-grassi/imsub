package usecase

import (
	"context"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type groupPolicyUpdateStoreStub struct {
	ownedCreatorFn func(context.Context, int64) (core.Creator, bool, error)
	groupByChatFn  func(context.Context, int64) (core.ManagedGroup, bool, error)
	upsertFn       func(context.Context, core.ManagedGroup) error
}

func (s groupPolicyUpdateStoreStub) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	return s.ownedCreatorFn(ctx, ownerTelegramID)
}

func (s groupPolicyUpdateStoreStub) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	return s.groupByChatFn(ctx, chatID)
}

func (s groupPolicyUpdateStoreStub) UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error {
	return s.upsertFn(ctx, group)
}

type groupPolicyUpdateObserverStub struct {
	events []events.Event
}

func (o *groupPolicyUpdateObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestUpdateGroupPolicyNotManaged(t *testing.T) {
	t.Parallel()

	obs := &groupPolicyUpdateObserverStub{}
	uc := NewGroupPolicyUpdateUseCase(groupPolicyUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) { return core.Creator{}, false, nil },
		groupByChatFn:  func(context.Context, int64) (core.ManagedGroup, bool, error) { return core.ManagedGroup{}, false, nil },
		upsertFn:       func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupPolicy(t.Context(), 7, 100, core.GroupPolicyKick)
	if err != nil {
		t.Fatalf("UpdateGroupPolicy() error = %v", err)
	}
	if got.Outcome != UpdateGroupPolicyOutcomeNotManaged {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupPolicyOutcomeNotManaged)
	}
	want := []events.Event{{Name: events.NameGroupPolicyUpdate, Outcome: "not_managed"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupPolicyNotOwner(t *testing.T) {
	t.Parallel()

	obs := &groupPolicyUpdateObserverStub{}
	uc := NewGroupPolicyUpdateUseCase(groupPolicyUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c2", Policy: core.GroupPolicyObserve}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupPolicy(t.Context(), 7, 100, core.GroupPolicyKick)
	if err != nil {
		t.Fatalf("UpdateGroupPolicy() error = %v", err)
	}
	if got.Outcome != UpdateGroupPolicyOutcomeNotOwner {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupPolicyOutcomeNotOwner)
	}
	want := []events.Event{{Name: events.NameGroupPolicyUpdate, Outcome: "not_owner"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupPolicyUnchanged(t *testing.T) {
	t.Parallel()

	obs := &groupPolicyUpdateObserverStub{}
	upserted := false
	uc := NewGroupPolicyUpdateUseCase(groupPolicyUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1", Policy: core.GroupPolicyObserveWarn}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error {
			upserted = true
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupPolicy(t.Context(), 7, 100, core.GroupPolicyObserveWarn)
	if err != nil {
		t.Fatalf("UpdateGroupPolicy() error = %v", err)
	}
	if got.Outcome != UpdateGroupPolicyOutcomeUnchanged || upserted {
		t.Fatalf("got = %+v upserted=%v", got, upserted)
	}
	want := []events.Event{{Name: events.NameGroupPolicyUpdate, Outcome: "unchanged"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupPolicyUpdated(t *testing.T) {
	t.Parallel()

	obs := &groupPolicyUpdateObserverStub{}
	var saved core.ManagedGroup
	uc := NewGroupPolicyUpdateUseCase(groupPolicyUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{
				ChatID:               100,
				CreatorID:            "c1",
				GroupName:            "VIP",
				Policy:               core.GroupPolicyObserve,
				RegistrationThreadID: 321,
			}, true, nil
		},
		upsertFn: func(_ context.Context, group core.ManagedGroup) error {
			saved = group
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupPolicy(t.Context(), 7, 100, core.GroupPolicyGraceWeek)
	if err != nil {
		t.Fatalf("UpdateGroupPolicy() error = %v", err)
	}
	if got.Outcome != UpdateGroupPolicyOutcomeUpdated {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupPolicyOutcomeUpdated)
	}
	if saved.Policy != core.GroupPolicyGraceWeek || saved.RegistrationThreadID != 321 || saved.CreatorID != "c1" {
		t.Fatalf("saved group = %+v", saved)
	}
	want := []events.Event{{Name: events.NameGroupPolicyUpdate, Outcome: "updated"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
