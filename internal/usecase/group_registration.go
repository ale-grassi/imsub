package usecase

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
)

// RegisterGroupOutcome identifies the store-backed registration decision.
type RegisterGroupOutcome string

const (
	// RegisterGroupOutcomeNotCreator means the caller has no owned creator.
	RegisterGroupOutcomeNotCreator RegisterGroupOutcome = "not_creator"
	// RegisterGroupOutcomeTakenByOther means the group is already linked to another creator.
	RegisterGroupOutcomeTakenByOther RegisterGroupOutcome = "taken_by_other"
	// RegisterGroupOutcomeAlreadyLinked means the group is already linked to the caller's creator.
	RegisterGroupOutcomeAlreadyLinked RegisterGroupOutcome = "already_linked"
	// RegisterGroupOutcomeRegistered means the group was newly linked to the caller's creator.
	RegisterGroupOutcomeRegistered RegisterGroupOutcome = "registered"
)

type groupRegistrationStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	Creator(ctx context.Context, creatorID string) (core.Creator, bool, error)
	UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error
}

// RegisterGroupFollowUp describes non-store actions implied by the registration result.
type RegisterGroupFollowUp struct {
	NeedsActivation    bool
	NeedsSettingsCheck bool
	NotifyOwner        bool
}

// RegisterGroupResult is the application-layer result for a group registration attempt.
type RegisterGroupResult struct {
	Outcome           RegisterGroupOutcome
	Creator           core.Creator
	ExistingGroup     core.ManagedGroup
	OtherCreatorName  string
	IsNewRegistration bool
	FollowUp          RegisterGroupFollowUp
}

// GroupRegistrationUseCase coordinates store-backed group registration decisions.
type GroupRegistrationUseCase struct {
	store  groupRegistrationStore
	events events.EventSink
	now    func() time.Time
}

// NewGroupRegistrationUseCase builds a group registration use case.
func NewGroupRegistrationUseCase(store groupRegistrationStore, sink events.EventSink) *GroupRegistrationUseCase {
	return &GroupRegistrationUseCase{
		store:  store,
		events: events.EnsureSink(sink),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// RegisterGroup decides and applies the store-backed part of /registergroup.
func (u *GroupRegistrationUseCase) RegisterGroup(ctx context.Context, ownerTelegramID, groupChatID int64, groupName, language string, policy core.GroupPolicy, registrationThreadID int) (RegisterGroupResult, error) {
	creator, ok, err := u.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return RegisterGroupResult{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok {
		u.recordOutcome(ctx, string(RegisterGroupOutcomeNotCreator))
		return RegisterGroupResult{Outcome: RegisterGroupOutcomeNotCreator}, nil
	}

	group, alreadyManaged, err := u.store.ManagedGroupByChatID(ctx, groupChatID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return RegisterGroupResult{}, fmt.Errorf("load managed group by chat id: %w", err)
	}
	if alreadyManaged && group.CreatorID != creator.ID {
		otherName, err := u.creatorNameByID(ctx, group.CreatorID)
		if err != nil {
			u.recordOutcome(ctx, "failed")
			return RegisterGroupResult{}, fmt.Errorf("load competing creator: %w", err)
		}
		u.recordOutcome(ctx, string(RegisterGroupOutcomeTakenByOther))
		return RegisterGroupResult{
			Outcome:           RegisterGroupOutcomeTakenByOther,
			Creator:           creator,
			ExistingGroup:     group,
			OtherCreatorName:  otherName,
			IsNewRegistration: false,
		}, nil
	}
	if alreadyManaged {
		u.recordOutcome(ctx, string(RegisterGroupOutcomeAlreadyLinked))
		return RegisterGroupResult{
			Outcome:           RegisterGroupOutcomeAlreadyLinked,
			Creator:           creator,
			ExistingGroup:     group,
			IsNewRegistration: false,
			FollowUp: RegisterGroupFollowUp{
				NeedsSettingsCheck: true,
			},
		}, nil
	}

	if policy == "" {
		policy = core.GroupPolicyObserve
	}

	managedGroup := core.ManagedGroup{
		ChatID:               groupChatID,
		CreatorID:            creator.ID,
		GroupName:            groupName,
		Language:             i18n.NormalizeLanguage(language),
		Policy:               policy,
		RegistrationThreadID: registrationThreadID,
		RegisteredAt:         u.now(),
	}
	if err := u.store.UpsertManagedGroup(ctx, managedGroup); err != nil {
		u.recordOutcome(ctx, "failed")
		return RegisterGroupResult{}, fmt.Errorf("upsert managed group: %w", err)
	}
	u.recordOutcome(ctx, string(RegisterGroupOutcomeRegistered))
	return RegisterGroupResult{
		Outcome:           RegisterGroupOutcomeRegistered,
		Creator:           creator,
		ExistingGroup:     managedGroup,
		IsNewRegistration: true,
		FollowUp: RegisterGroupFollowUp{
			NeedsActivation:    true,
			NeedsSettingsCheck: true,
			NotifyOwner:        true,
		},
	}, nil
}

func (u *GroupRegistrationUseCase) creatorNameByID(ctx context.Context, creatorID string) (string, error) {
	creator, ok, err := u.store.Creator(ctx, creatorID)
	if err != nil {
		return "", fmt.Errorf("load creator %s: %w", creatorID, err)
	}
	if !ok || creator.TwitchLogin == "" {
		return creatorID, nil
	}
	return creator.TwitchLogin, nil
}

func (u *GroupRegistrationUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupRegistration,
		Outcome: outcome,
	})
}
