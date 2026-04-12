package usecase

import (
	"context"
	"slices"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
)

type groupRegistrationStoreStub struct {
	ownedCreatorFn func(context.Context, int64) (core.Creator, bool, error)
	groupByChatFn  func(context.Context, int64) (core.ManagedGroup, bool, error)
	creatorFn      func(context.Context, string) (core.Creator, bool, error)
	upsertFn       func(context.Context, core.ManagedGroup) error
}

type groupRegistrationObserverStub struct {
	events []events.Event
}

func (o *groupRegistrationObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func (s groupRegistrationStoreStub) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	return s.ownedCreatorFn(ctx, ownerTelegramID)
}
func (s groupRegistrationStoreStub) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	return s.groupByChatFn(ctx, chatID)
}
func (s groupRegistrationStoreStub) Creator(ctx context.Context, creatorID string) (core.Creator, bool, error) {
	return s.creatorFn(ctx, creatorID)
}
func (s groupRegistrationStoreStub) UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error {
	return s.upsertFn(ctx, group)
}

func TestRegisterGroupNotCreator(t *testing.T) {
	t.Parallel()

	obs := &groupRegistrationObserverStub{}
	uc := NewGroupRegistrationUseCase(groupRegistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{}, false, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{}, false, nil
		},
		creatorFn: func(context.Context, string) (core.Creator, bool, error) {
			return core.Creator{}, false, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.RegisterGroup(t.Context(), 7, 100, "VIP", "en", core.GroupPolicyObserve, 0)
	if err != nil {
		t.Fatalf("RegisterGroup error = %v", err)
	}
	if got.Outcome != RegisterGroupOutcomeNotCreator {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, RegisterGroupOutcomeNotCreator)
	}
	want := []events.Event{{Name: events.NameGroupRegistration, Outcome: "not_creator"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestRegisterGroupTakenByOther(t *testing.T) {
	t.Parallel()

	obs := &groupRegistrationObserverStub{}
	uc := NewGroupRegistrationUseCase(groupRegistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1", TwitchLogin: "owner"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c2"}, true, nil
		},
		creatorFn: func(context.Context, string) (core.Creator, bool, error) {
			return core.Creator{ID: "c2", TwitchLogin: "other"}, true, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.RegisterGroup(t.Context(), 7, 100, "VIP", "en", core.GroupPolicyObserve, 0)
	if err != nil {
		t.Fatalf("RegisterGroup error = %v", err)
	}
	if got.Outcome != RegisterGroupOutcomeTakenByOther || got.OtherCreatorName != "other" {
		t.Fatalf("got = %+v", got)
	}
	if got.FollowUp != (RegisterGroupFollowUp{}) {
		t.Fatalf("FollowUp = %+v, want zero value", got.FollowUp)
	}
	want := []events.Event{{Name: events.NameGroupRegistration, Outcome: "taken_by_other"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestRegisterGroupAlreadyLinkedNeedsSettingsCheckOnly(t *testing.T) {
	t.Parallel()

	obs := &groupRegistrationObserverStub{}
	uc := NewGroupRegistrationUseCase(groupRegistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1", TwitchLogin: "owner"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{ChatID: 100, CreatorID: "c1", GroupName: "VIP"}, true, nil
		},
		creatorFn: func(context.Context, string) (core.Creator, bool, error) {
			return core.Creator{}, false, nil
		},
		upsertFn: func(context.Context, core.ManagedGroup) error { return nil },
	}, obs)

	got, err := uc.RegisterGroup(t.Context(), 7, 100, "VIP", "en", core.GroupPolicyObserve, 0)
	if err != nil {
		t.Fatalf("RegisterGroup error = %v", err)
	}
	if got.Outcome != RegisterGroupOutcomeAlreadyLinked {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, RegisterGroupOutcomeAlreadyLinked)
	}
	wantFollowUp := RegisterGroupFollowUp{NeedsSettingsCheck: true}
	if got.FollowUp != wantFollowUp {
		t.Fatalf("FollowUp = %+v, want %+v", got.FollowUp, wantFollowUp)
	}
	want := []events.Event{{Name: events.NameGroupRegistration, Outcome: "already_linked"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}

func TestRegisterGroupRegistered(t *testing.T) {
	t.Parallel()

	var saved core.ManagedGroup
	obs := &groupRegistrationObserverStub{}
	uc := NewGroupRegistrationUseCase(groupRegistrationStoreStub{
		ownedCreatorFn: func(context.Context, int64) (core.Creator, bool, error) {
			return core.Creator{ID: "c1", TwitchLogin: "owner"}, true, nil
		},
		groupByChatFn: func(context.Context, int64) (core.ManagedGroup, bool, error) {
			return core.ManagedGroup{}, false, nil
		},
		creatorFn: func(context.Context, string) (core.Creator, bool, error) {
			return core.Creator{}, false, nil
		},
		upsertFn: func(_ context.Context, group core.ManagedGroup) error {
			saved = group
			return nil
		},
	}, obs)

	got, err := uc.RegisterGroup(t.Context(), 7, 100, "VIP", "it-IT", core.GroupPolicyObserveWarn, 321)
	if err != nil {
		t.Fatalf("RegisterGroup error = %v", err)
	}
	if got.Outcome != RegisterGroupOutcomeRegistered || !got.IsNewRegistration {
		t.Fatalf("got = %+v", got)
	}
	wantFollowUp := RegisterGroupFollowUp{
		NeedsActivation:    true,
		NeedsSettingsCheck: true,
		NotifyOwner:        true,
	}
	if got.FollowUp != wantFollowUp {
		t.Fatalf("FollowUp = %+v, want %+v", got.FollowUp, wantFollowUp)
	}
	if saved.ChatID != 100 || saved.CreatorID != "c1" || saved.GroupName != "VIP" || saved.Language != "it" || saved.Policy != core.GroupPolicyObserveWarn || saved.RegistrationThreadID != 321 {
		t.Fatalf("saved = %+v", saved)
	}
	want := []events.Event{{Name: events.NameGroupRegistration, Outcome: "registered"}}
	if !slices.EqualFunc(obs.events, want, equalEvents) {
		t.Fatalf("events = %+v, want %+v", obs.events, want)
	}
}
