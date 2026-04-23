package usecase

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
)

// UpdateGroupMemberTagOutcome identifies the member-tag-sync update result.
type UpdateGroupMemberTagOutcome string

const (
	// UpdateGroupMemberTagOutcomeNotManaged indicates the target chat is not a managed group.
	UpdateGroupMemberTagOutcomeNotManaged UpdateGroupMemberTagOutcome = "not_managed"
	// UpdateGroupMemberTagOutcomeNotOwner indicates the caller does not own the managed group.
	UpdateGroupMemberTagOutcomeNotOwner UpdateGroupMemberTagOutcome = "not_owner"
	// UpdateGroupMemberTagOutcomeUnchanged indicates the requested flag already matched the current state.
	UpdateGroupMemberTagOutcomeUnchanged UpdateGroupMemberTagOutcome = "unchanged"
	// UpdateGroupMemberTagOutcomeUpdated indicates the managed group flag was updated successfully.
	UpdateGroupMemberTagOutcomeUpdated UpdateGroupMemberTagOutcome = "updated"
)

type groupMemberTagUpdateStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error
}

// UpdateGroupMemberTagResult is the application-layer result for per-group member-tag-sync updates.
type UpdateGroupMemberTagResult struct {
	Outcome UpdateGroupMemberTagOutcome
	Creator core.Creator
	Group   core.ManagedGroup
}

// GroupMemberTagUpdateUseCase coordinates per-group member-tag-sync updates.
type GroupMemberTagUpdateUseCase struct {
	store  groupMemberTagUpdateStore
	events events.EventSink
	now    func() time.Time
}

// NewGroupMemberTagUpdateUseCase builds a member-tag-sync update use case.
func NewGroupMemberTagUpdateUseCase(store groupMemberTagUpdateStore, sink events.EventSink) *GroupMemberTagUpdateUseCase {
	return &GroupMemberTagUpdateUseCase{
		store:  store,
		events: events.EnsureSink(sink),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// UpdateGroupMemberTagSync updates the member-tag-sync flag for a managed group owned by the caller.
func (u *GroupMemberTagUpdateUseCase) UpdateGroupMemberTagSync(ctx context.Context, ownerTelegramID, groupChatID int64, enabled bool) (UpdateGroupMemberTagResult, error) {
	group, managed, err := u.store.ManagedGroupByChatID(ctx, groupChatID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupMemberTagResult{}, fmt.Errorf("load managed group by chat id: %w", err)
	}
	if !managed {
		u.recordOutcome(ctx, string(UpdateGroupMemberTagOutcomeNotManaged))
		return UpdateGroupMemberTagResult{Outcome: UpdateGroupMemberTagOutcomeNotManaged}, nil
	}

	creator, ok, err := u.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupMemberTagResult{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok || group.CreatorID != creator.ID {
		u.recordOutcome(ctx, string(UpdateGroupMemberTagOutcomeNotOwner))
		return UpdateGroupMemberTagResult{
			Outcome: UpdateGroupMemberTagOutcomeNotOwner,
			Group:   group,
		}, nil
	}

	if group.MemberTagSyncEnabled == enabled {
		u.recordOutcome(ctx, string(UpdateGroupMemberTagOutcomeUnchanged))
		return UpdateGroupMemberTagResult{
			Outcome: UpdateGroupMemberTagOutcomeUnchanged,
			Creator: creator,
			Group:   group,
		}, nil
	}

	group.MemberTagSyncEnabled = enabled
	group.UpdatedAt = u.now()
	if err := u.store.UpsertManagedGroup(ctx, group); err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupMemberTagResult{}, fmt.Errorf("upsert managed group: %w", err)
	}

	u.recordOutcome(ctx, string(UpdateGroupMemberTagOutcomeUpdated))
	return UpdateGroupMemberTagResult{
		Outcome: UpdateGroupMemberTagOutcomeUpdated,
		Creator: creator,
		Group:   group,
	}, nil
}

func (u *GroupMemberTagUpdateUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupMemberTagUpdate,
		Outcome: outcome,
	})
}
