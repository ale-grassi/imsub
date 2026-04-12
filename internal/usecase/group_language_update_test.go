package usecase

import (
	"context"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type groupLanguageUpdateStoreStub struct {
	ownedCreatorFn func(context.Context, int64) (core.Creator, bool, error)
	groupByChatFn  func(context.Context, int64) (core.ManagedGroup, bool, error)
	upsertFn       func(context.Context, core.ManagedGroup) error
}

func (s groupLanguageUpdateStoreStub) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	return s.ownedCreatorFn(ctx, ownerTelegramID)
}

func (s groupLanguageUpdateStoreStub) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	return s.groupByChatFn(ctx, chatID)
}

func (s groupLanguageUpdateStoreStub) UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error {
	return s.upsertFn(ctx, group)
}

type groupLanguageUpdateObserverStub struct {
	events []events.Event
}

func (o *groupLanguageUpdateObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestUpdateGroupLanguageNotManaged(t *testing.T) {
	t.Parallel()

	obs := &groupLanguageUpdateObserverStub{}
	uc := NewGroupLanguageUpdateUseCase(groupLanguageUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) { return core.Creator{}, false, nil },
		groupByChatFn:  func(context.Context, int64) (core.ManagedGroup, bool, error) { return core.ManagedGroup{}, false, nil },
		upsertFn:       func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupLanguage(t.Context(), 7, 100, "it")
	if err != nil {
		t.Fatalf("UpdateGroupLanguage() error = %v", err)
	}
	if got.Outcome != UpdateGroupLanguageOutcomeNotManaged {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupLanguageOutcomeNotManaged)
	}
	want := []events.Event{{Name: events.NameGroupLanguageUpdate, Outcome: "not_managed"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupLanguageNotOwner(t *testing.T) {
	t.Parallel()

	obs := &groupLanguageUpdateObserverStub{}
	uc := NewGroupLanguageUpdateUseCase(groupLanguageUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c2", Language: "en"}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.UpdateGroupLanguage(t.Context(), 7, 100, "it")
	if err != nil {
		t.Fatalf("UpdateGroupLanguage() error = %v", err)
	}
	if got.Outcome != UpdateGroupLanguageOutcomeNotOwner {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupLanguageOutcomeNotOwner)
	}
	want := []events.Event{{Name: events.NameGroupLanguageUpdate, Outcome: "not_owner"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupLanguageUnchanged(t *testing.T) {
	t.Parallel()

	obs := &groupLanguageUpdateObserverStub{}
	upserted := false
	uc := NewGroupLanguageUpdateUseCase(groupLanguageUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1", Language: "it"}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error {
			upserted = true
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupLanguage(t.Context(), 7, 100, "it-IT")
	if err != nil {
		t.Fatalf("UpdateGroupLanguage() error = %v", err)
	}
	if got.Outcome != UpdateGroupLanguageOutcomeUnchanged || upserted {
		t.Fatalf("got = %+v upserted=%v", got, upserted)
	}
	want := []events.Event{{Name: events.NameGroupLanguageUpdate, Outcome: "unchanged"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestUpdateGroupLanguageUpdated(t *testing.T) {
	t.Parallel()

	obs := &groupLanguageUpdateObserverStub{}
	var saved core.ManagedGroup
	uc := NewGroupLanguageUpdateUseCase(groupLanguageUpdateStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{
				ChatID:               100,
				CreatorID:            "c1",
				GroupName:            "VIP",
				Language:             "en",
				Policy:               core.GroupPolicyObserve,
				RegistrationThreadID: 321,
			}, true, nil
		},
		upsertFn: func(_ context.Context, group core.ManagedGroup) error {
			saved = group
			return nil
		},
	}, obs)

	got, err := uc.UpdateGroupLanguage(t.Context(), 7, 100, "it")
	if err != nil {
		t.Fatalf("UpdateGroupLanguage() error = %v", err)
	}
	if got.Outcome != UpdateGroupLanguageOutcomeUpdated {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, UpdateGroupLanguageOutcomeUpdated)
	}
	if saved.Language != "it" || saved.RegistrationThreadID != 321 || saved.CreatorID != "c1" {
		t.Fatalf("saved group = %+v", saved)
	}
	want := []events.Event{{Name: events.NameGroupLanguageUpdate, Outcome: "updated"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
