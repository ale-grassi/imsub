package usecase

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
)

// UpdateGroupPolicyOutcome identifies the group-policy update result.
type UpdateGroupPolicyOutcome string

const (
	// UpdateGroupPolicyOutcomeNotManaged means the group is not currently managed.
	UpdateGroupPolicyOutcomeNotManaged UpdateGroupPolicyOutcome = "not_managed"
	// UpdateGroupPolicyOutcomeNotOwner means the caller does not own the managed group.
	UpdateGroupPolicyOutcomeNotOwner UpdateGroupPolicyOutcome = "not_owner"
	// UpdateGroupPolicyOutcomeUnchanged means the selected policy already matches the stored one.
	UpdateGroupPolicyOutcomeUnchanged UpdateGroupPolicyOutcome = "unchanged"
	// UpdateGroupPolicyOutcomeUpdated means the group policy was updated.
	UpdateGroupPolicyOutcomeUpdated UpdateGroupPolicyOutcome = "updated"
)

type groupPolicyUpdateStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error
}

// UpdateGroupPolicyResult is the application-layer result for group policy updates.
type UpdateGroupPolicyResult struct {
	Outcome UpdateGroupPolicyOutcome
	Creator core.Creator
	Group   core.ManagedGroup
}

// GroupPolicyUpdateUseCase coordinates managed-group policy updates.
type GroupPolicyUpdateUseCase struct {
	store  groupPolicyUpdateStore
	events events.EventSink
	now    func() time.Time
}

// NewGroupPolicyUpdateUseCase builds a group-policy update use case.
func NewGroupPolicyUpdateUseCase(store groupPolicyUpdateStore, sink events.EventSink) *GroupPolicyUpdateUseCase {
	return &GroupPolicyUpdateUseCase{
		store:  store,
		events: events.EnsureSink(sink),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// UpdateGroupPolicy updates the unverified-member policy for a managed group owned by the caller.
func (u *GroupPolicyUpdateUseCase) UpdateGroupPolicy(ctx context.Context, ownerTelegramID, groupChatID int64, policy core.GroupPolicy) (UpdateGroupPolicyResult, error) {
	group, managed, err := u.store.ManagedGroupByChatID(ctx, groupChatID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupPolicyResult{}, fmt.Errorf("load managed group by chat id: %w", err)
	}
	if !managed {
		u.recordOutcome(ctx, string(UpdateGroupPolicyOutcomeNotManaged))
		return UpdateGroupPolicyResult{Outcome: UpdateGroupPolicyOutcomeNotManaged}, nil
	}

	creator, ok, err := u.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupPolicyResult{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok || group.CreatorID != creator.ID {
		u.recordOutcome(ctx, string(UpdateGroupPolicyOutcomeNotOwner))
		return UpdateGroupPolicyResult{
			Outcome: UpdateGroupPolicyOutcomeNotOwner,
			Group:   group,
		}, nil
	}

	if policy == "" {
		policy = core.GroupPolicyObserve
	}
	if group.Policy == "" {
		group.Policy = core.GroupPolicyObserve
	}
	if group.Policy == policy {
		u.recordOutcome(ctx, string(UpdateGroupPolicyOutcomeUnchanged))
		return UpdateGroupPolicyResult{
			Outcome: UpdateGroupPolicyOutcomeUnchanged,
			Creator: creator,
			Group:   group,
		}, nil
	}

	group.Policy = policy
	group.UpdatedAt = u.now()
	if err := u.store.UpsertManagedGroup(ctx, group); err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupPolicyResult{}, fmt.Errorf("upsert managed group: %w", err)
	}

	u.recordOutcome(ctx, string(UpdateGroupPolicyOutcomeUpdated))
	return UpdateGroupPolicyResult{
		Outcome: UpdateGroupPolicyOutcomeUpdated,
		Creator: creator,
		Group:   group,
	}, nil
}

func (u *GroupPolicyUpdateUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupPolicyUpdate,
		Outcome: outcome,
	})
}
