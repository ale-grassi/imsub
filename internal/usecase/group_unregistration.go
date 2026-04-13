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
	CreateMemberCleanupJob(ctx context.Context, job core.MemberCleanupJob) (core.MemberCleanupJob, error)
	DeleteManagedGroup(ctx context.Context, chatID int64) error
}

type groupUnregistrationCleaner interface {
	DeleteEventSubsForCreator(ctx context.Context, creatorID string) error
}

// UnregisterGroupResult is the application-layer result for group unregistration.
type UnregisterGroupResult struct {
	Outcome                 UnregisterGroupOutcome
	Creator                 core.Creator
	Group                   core.ManagedGroup
	CleanupFailed           bool
	MemberAction            core.CreatorResetGroupAction
	TargetedMembershipCount int
	CleanupQueued           bool
	CleanupQueueFailed      bool
}

// GroupUnregistrationUseCase coordinates managed-group removal and best-effort cleanup.
type GroupUnregistrationUseCase struct {
	store   groupUnregistrationStore
	cleaner groupUnregistrationCleaner
	god     *core.GodAccessChecker
	events  events.EventSink
}

// NewGroupUnregistrationUseCase builds a group-unregistration use case.
func NewGroupUnregistrationUseCase(store groupUnregistrationStore, cleaner groupUnregistrationCleaner, god *core.GodAccessChecker, sink events.EventSink) *GroupUnregistrationUseCase {
	return &GroupUnregistrationUseCase{
		store:   store,
		cleaner: cleaner,
		god:     god,
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
	var queuedJob core.MemberCleanupJob
	shouldQueueCleanup := false
	cleanupQueued := false
	cleanupQueueFailed := false
	if action == core.CreatorResetKickTrackedMembers {
		memberIDs, err := u.store.ListTrackedGroupMemberIDs(ctx, groupChatID)
		if err != nil {
			u.recordOutcome(ctx, "failed")
			return UnregisterGroupResult{}, fmt.Errorf("list tracked group member ids: %w", err)
		}
		targetedMembershipCount = len(memberIDs)
		if targetedMembershipCount > 0 {
			targets := make([]core.MemberCleanupTarget, 0, targetedMembershipCount)
			for _, telegramUserID := range memberIDs {
				if u.god != nil && u.god.IsGodTelegramUser(telegramUserID) {
					targetedMembershipCount--
					continue
				}
				targets = append(targets, core.MemberCleanupTarget{
					ChatID:         groupChatID,
					TelegramUserID: telegramUserID,
					MaxAttempts:    3,
				})
			}
			queuedJob = core.MemberCleanupJob{
				Kind:            core.MemberCleanupKindGroupUnregistration,
				OwnerTelegramID: ownerTelegramID,
				CreatorID:       creator.ID,
				CreatorLogin:    creator.TwitchLogin,
				GroupChatID:     group.ChatID,
				GroupName:       group.GroupName,
				Targets:         targets,
			}
			shouldQueueCleanup = len(targets) > 0
		}
	}

	if err := u.store.DeleteManagedGroup(ctx, groupChatID); err != nil {
		u.recordOutcome(ctx, "failed")
		return UnregisterGroupResult{}, fmt.Errorf("delete managed group: %w", err)
	}
	if shouldQueueCleanup {
		if _, err := u.store.CreateMemberCleanupJob(ctx, queuedJob); err != nil {
			cleanupQueueFailed = true
		} else {
			cleanupQueued = true
		}
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
		CleanupQueued:           cleanupQueued,
		CleanupQueueFailed:      cleanupQueueFailed,
	}, nil
}

func (u *GroupUnregistrationUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupUnregistration,
		Outcome: outcome,
	})
}
