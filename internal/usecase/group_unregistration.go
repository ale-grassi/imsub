package usecase

import (
	"context"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

// UnregisterGroupOutcome identifies the group-unregistration result.
type UnregisterGroupOutcome string

const (
	// UnregisterGroupOutcomeNotManaged means the group is not currently managed.
	UnregisterGroupOutcomeNotManaged UnregisterGroupOutcome = "not_managed"
	// UnregisterGroupOutcomeNotOwner means the caller does not own the managed group.
	UnregisterGroupOutcomeNotOwner UnregisterGroupOutcome = "not_owner"
	// UnregisterGroupOutcomeUnregistered means the group and its eager cleanup were removed successfully.
	UnregisterGroupOutcomeUnregistered UnregisterGroupOutcome = "unregistered"
	// UnregisterGroupOutcomeUnregisteredCleanupLag means the group was removed but cleanup must be completed later.
	UnregisterGroupOutcomeUnregisteredCleanupLag UnregisterGroupOutcome = "unregistered_cleanup_lag"
)

type groupUnregistrationStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	ListTrackedGroupMemberIDs(ctx context.Context, chatID int64) ([]int64, error)
	DeleteManagedGroup(ctx context.Context, chatID int64) error
}

type groupUnregistrationCleaner interface {
	DeleteEventSubsForCreator(ctx context.Context, creatorID string) error
}

type groupUnregistrationKicker func(ctx context.Context, groupChatID int64, telegramUserID int64) error

// UnregisterGroupResult is the application-layer result for group unregistration.
type UnregisterGroupResult struct {
	Outcome                 UnregisterGroupOutcome
	Creator                 core.Creator
	Group                   core.ManagedGroup
	CleanupFailed           bool
	MemberAction            core.CreatorResetGroupAction
	TargetedMembershipCount int
	KickFailureCount        int
}

// GroupUnregistrationUseCase coordinates managed-group removal and best-effort cleanup.
type GroupUnregistrationUseCase struct {
	store   groupUnregistrationStore
	cleaner groupUnregistrationCleaner
	kick    groupUnregistrationKicker
	events  events.EventSink
}

// NewGroupUnregistrationUseCase builds a group-unregistration use case.
func NewGroupUnregistrationUseCase(store groupUnregistrationStore, cleaner groupUnregistrationCleaner, kick groupUnregistrationKicker, sink events.EventSink) *GroupUnregistrationUseCase {
	return &GroupUnregistrationUseCase{
		store:   store,
		cleaner: cleaner,
		kick:    kick,
		events:  events.EnsureSink(sink),
	}
}

// UnregisterGroup removes a managed group if it belongs to the caller's creator.
func (u *GroupUnregistrationUseCase) UnregisterGroup(ctx context.Context, ownerTelegramID, groupChatID int64, action core.CreatorResetGroupAction) (UnregisterGroupResult, error) {
	if action == "" {
		action = core.CreatorResetKeepMembers
	}
	group, managed, err := u.store.ManagedGroupByChatID(ctx, groupChatID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UnregisterGroupResult{}, fmt.Errorf("load managed group by chat id: %w", err)
	}
	if !managed {
		u.recordOutcome(ctx, string(UnregisterGroupOutcomeNotManaged))
		return UnregisterGroupResult{Outcome: UnregisterGroupOutcomeNotManaged}, nil
	}

	creator, ok, err := u.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UnregisterGroupResult{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok || group.CreatorID != creator.ID {
		u.recordOutcome(ctx, string(UnregisterGroupOutcomeNotOwner))
		return UnregisterGroupResult{
			Outcome: UnregisterGroupOutcomeNotOwner,
			Group:   group,
		}, nil
	}

	targetedMembershipCount := 0
	kickFailureCount := 0
	if action == core.CreatorResetKickTrackedMembers {
		memberIDs, err := u.store.ListTrackedGroupMemberIDs(ctx, groupChatID)
		if err != nil {
			u.recordOutcome(ctx, "failed")
			return UnregisterGroupResult{}, fmt.Errorf("list tracked group member ids: %w", err)
		}
		targetedMembershipCount = len(memberIDs)
		if u.kick != nil {
			for _, telegramUserID := range memberIDs {
				if err := u.kick(ctx, groupChatID, telegramUserID); err != nil {
					kickFailureCount++
				}
			}
		}
	}

	if err := u.store.DeleteManagedGroup(ctx, groupChatID); err != nil {
		u.recordOutcome(ctx, "failed")
		return UnregisterGroupResult{}, fmt.Errorf("delete managed group: %w", err)
	}

	outcome := UnregisterGroupOutcomeUnregistered
	cleanupFailed := false
	if u.cleaner != nil {
		if err := u.cleaner.DeleteEventSubsForCreator(ctx, creator.ID); err != nil {
			outcome = UnregisterGroupOutcomeUnregisteredCleanupLag
			cleanupFailed = true
		}
	}

	u.recordOutcome(ctx, string(outcome))
	return UnregisterGroupResult{
		Outcome:                 outcome,
		Creator:                 creator,
		Group:                   group,
		CleanupFailed:           cleanupFailed,
		MemberAction:            action,
		TargetedMembershipCount: targetedMembershipCount,
		KickFailureCount:        kickFailureCount,
	}, nil
}

func (u *GroupUnregistrationUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupUnregistration,
		Outcome: outcome,
	})
}
