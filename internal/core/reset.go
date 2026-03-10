package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
)

type kickFunc = func(ctx context.Context, groupChatID int64, telegramUserID int64, reason KickReason) error

// CreatorResetGroupAction describes what a creator reset should do with tracked
// members of the creator's managed groups.
type CreatorResetGroupAction string

const (
	// CreatorResetKeepMembers deletes creator data without kicking tracked
	// members from the creator's managed groups.
	CreatorResetKeepMembers CreatorResetGroupAction = "keep_members"
	// CreatorResetKickTrackedMembers kicks tracked members from the creator's
	// managed groups before deleting creator data.
	CreatorResetKickTrackedMembers CreatorResetGroupAction = "kick_tracked_members"
)

type eventSubCleaner interface {
	DeleteEventSubsForCreator(ctx context.Context, creatorID string) error
}

type resetStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	ListTrackedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, error)
	ListTrackedGroupMemberIDs(ctx context.Context, chatID int64) ([]int64, error)
	ListActiveCreators(ctx context.Context) ([]Creator, error)
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]ManagedGroup, error)
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	CreateMemberCleanupJob(ctx context.Context, job MemberCleanupJob) (MemberCleanupJob, error)
	DeleteAllUserData(ctx context.Context, telegramUserID int64) error
	DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error)
}

// ResetService coordinates viewer and creator reset workflows.
type ResetService struct {
	store         resetStore
	kick          kickFunc
	log           *slog.Logger
	eventSubClean eventSubCleaner
}

// GroupResolutionStats describes how reset target groups were resolved.
type GroupResolutionStats struct {
	TrackedCount        int
	CanonicalCount      int
	CanonicalAddedCount int
	TotalCount          int
}

// ScopeState describes which reset scopes currently exist for a user.
type ScopeState struct {
	Identity    UserIdentity
	HasIdentity bool
	Creator     Creator
	HasCreator  bool
}

// CreatorGroupCleanupSummary describes creator-group cleanup performed during
// creator-involving reset scopes.
type CreatorGroupCleanupSummary struct {
	Action                  CreatorResetGroupAction
	ManagedGroupCount       int
	TargetedMembershipCount int
	KickFailureCount        int
	Queued                  bool
	QueueFailed             bool
}

// ViewerResetResult contains the outcome of a viewer reset.
type ViewerResetResult struct {
	HasIdentity     bool
	Identity        UserIdentity
	GroupCount      int
	GroupResolution GroupResolutionStats
}

// CreatorResetResult contains the outcome of a creator reset.
type CreatorResetResult struct {
	DeletedCount   int
	DeletedNames   []string
	CreatorCleanup CreatorGroupCleanupSummary
}

// BothResetResult contains the outcome of running both reset scopes.
type BothResetResult struct {
	HasIdentity     bool
	Identity        UserIdentity
	GroupCount      int
	GroupResolution GroupResolutionStats
	DeletedCount    int
	DeletedNames    []string
	CreatorCleanup  CreatorGroupCleanupSummary
}

// NewResetService creates a reset service with optional logger fallback.
func NewResetService(store resetStore, kick func(context.Context, int64, int64, KickReason) error, logger *slog.Logger) *ResetService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResetService{
		store: store,
		kick:  kick,
		log:   logger,
	}
}

// SetEventSubCleaner wires an EventSub cleanup hook into creator reset flows.
func (r *ResetService) SetEventSubCleaner(cleaner eventSubCleaner) {
	r.eventSubClean = cleaner
}

// LoadScopes resolves whether viewer and/or creator state currently exists.
func (r *ResetService) LoadScopes(ctx context.Context, telegramUserID int64) (ScopeState, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return ScopeState{}, fmt.Errorf("load user identity: %w", err)
	}
	creator, hasCreator, err := r.store.OwnedCreatorForUser(ctx, telegramUserID)
	if err != nil {
		return ScopeState{}, fmt.Errorf("load owned creator: %w", err)
	}
	return ScopeState{
		Identity:    identity,
		HasIdentity: hasIdentity,
		Creator:     creator,
		HasCreator:  hasCreator,
	}, nil
}

// CountViewerGroups returns how many creator groups the user may be removed from.
func (r *ResetService) CountViewerGroups(ctx context.Context, telegramUserID int64) (int, error) {
	return r.CountSubLinkedGroupsForUser(ctx, telegramUserID)
}

// CountCreatorGroups returns how many managed groups are owned by the creator.
func (r *ResetService) CountCreatorGroups(ctx context.Context, telegramUserID int64) (int, error) {
	creator, hasCreator, err := r.store.OwnedCreatorForUser(ctx, telegramUserID)
	if err != nil {
		return 0, fmt.Errorf("load owned creator: %w", err)
	}
	if !hasCreator {
		return 0, nil
	}
	groups, err := r.store.ListManagedGroupsByCreator(ctx, creator.ID)
	if err != nil {
		return 0, fmt.Errorf("list managed groups by creator %s: %w", creator.ID, err)
	}
	return len(groups), nil
}

// ExecuteViewerReset removes viewer-linked data and group access.
func (r *ResetService) ExecuteViewerReset(ctx context.Context, telegramUserID int64) (ViewerResetResult, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return ViewerResetResult{}, fmt.Errorf("load user identity: %w", err)
	}
	if !hasIdentity {
		return ViewerResetResult{HasIdentity: false}, nil
	}
	groupCount, resolution, err := r.ResetViewerDataAndRevokeGroupAccess(ctx, telegramUserID)
	if err != nil {
		return ViewerResetResult{}, fmt.Errorf("reset viewer data and revoke: %w", err)
	}
	return ViewerResetResult{
		HasIdentity:     true,
		Identity:        identity,
		GroupCount:      groupCount,
		GroupResolution: resolution,
	}, nil
}

// ExecuteCreatorReset removes creator-owned data.
func (r *ResetService) ExecuteCreatorReset(ctx context.Context, telegramUserID int64, action CreatorResetGroupAction) (CreatorResetResult, error) {
	cleanup, err := r.cleanupCreatorGroupsForReset(ctx, telegramUserID, action)
	if err != nil {
		return CreatorResetResult{}, fmt.Errorf("cleanup creator groups for reset: %w", err)
	}
	deletedCount, deletedNames, err := r.DeleteCreatorData(ctx, telegramUserID)
	if err != nil {
		return CreatorResetResult{}, fmt.Errorf("delete creator data: %w", err)
	}
	return CreatorResetResult{
		DeletedCount:   deletedCount,
		DeletedNames:   deletedNames,
		CreatorCleanup: cleanup,
	}, nil
}

// ExecuteBothReset performs viewer and creator reset scopes together.
func (r *ResetService) ExecuteBothReset(ctx context.Context, telegramUserID int64, action CreatorResetGroupAction) (BothResetResult, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return BothResetResult{}, fmt.Errorf("load user identity: %w", err)
	}

	groupCount := 0
	var resolution GroupResolutionStats
	if hasIdentity {
		groupCount, resolution, err = r.ResetViewerDataAndRevokeGroupAccess(ctx, telegramUserID)
		if err != nil {
			return BothResetResult{}, fmt.Errorf("reset viewer data and revoke: %w", err)
		}
	}

	cleanup, err := r.cleanupCreatorGroupsForReset(ctx, telegramUserID, action)
	if err != nil {
		return BothResetResult{}, fmt.Errorf("cleanup creator groups for reset: %w", err)
	}

	deletedCount, deletedNames, err := r.DeleteCreatorData(ctx, telegramUserID)
	if err != nil {
		return BothResetResult{}, fmt.Errorf("delete creator data: %w", err)
	}

	return BothResetResult{
		HasIdentity:     hasIdentity,
		Identity:        identity,
		GroupCount:      groupCount,
		GroupResolution: resolution,
		DeletedCount:    deletedCount,
		DeletedNames:    deletedNames,
		CreatorCleanup:  cleanup,
	}, nil
}

// CountSubLinkedGroupsForUser returns the number of linked creator groups for the user.
func (r *ResetService) CountSubLinkedGroupsForUser(ctx context.Context, telegramUserID int64) (int, error) {
	groupIDs, _, err := r.resolveSubLinkedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return 0, fmt.Errorf("sub linked group ids: %w", err)
	}
	return len(groupIDs), nil
}

// SubLinkedGroupIDsForUser returns sorted linked creator group IDs for the user.
// Tracked membership is used as a fast path, then canonical subscription state
// is consulted to fill gaps when the cache is stale or incomplete.
func (r *ResetService) SubLinkedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, error) {
	groupIDs, _, err := r.resolveSubLinkedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return nil, err
	}
	return groupIDs, nil
}

func (r *ResetService) resolveSubLinkedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, GroupResolutionStats, error) {
	groupIDs, err := r.store.ListTrackedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return nil, GroupResolutionStats{}, fmt.Errorf("list tracked group ids for user: %w", err)
	}
	stats := GroupResolutionStats{TrackedCount: len(groupIDs)}
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return nil, GroupResolutionStats{}, fmt.Errorf("load user identity for canonical group lookup: %w", err)
	}
	if hasIdentity && identity.TwitchUserID != "" {
		derivedGroupIDs, err := r.canonicalGroupIDsForUser(ctx, identity.TwitchUserID)
		if err != nil {
			return nil, GroupResolutionStats{}, fmt.Errorf("canonical group ids for user: %w", err)
		}
		stats.CanonicalCount = len(derivedGroupIDs)
		existing := make(map[int64]struct{}, len(groupIDs))
		for _, groupID := range groupIDs {
			existing[groupID] = struct{}{}
		}
		for _, groupID := range derivedGroupIDs {
			if _, ok := existing[groupID]; !ok {
				stats.CanonicalAddedCount++
			}
		}
		groupIDs = append(groupIDs, derivedGroupIDs...)
	}
	slices.Sort(groupIDs)
	groupIDs = slices.Compact(groupIDs)
	stats.TotalCount = len(groupIDs)
	return groupIDs, stats, nil
}

func (r *ResetService) canonicalGroupIDsForUser(ctx context.Context, twitchUserID string) ([]int64, error) {
	creators, err := r.store.ListActiveCreators(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active creators: %w", err)
	}

	groupIDs := make([]int64, 0)
	for _, creator := range creators {
		isSubscriber, err := r.store.IsCreatorSubscriber(ctx, creator.ID, twitchUserID)
		if err != nil {
			return nil, fmt.Errorf("is creator subscriber %s: %w", creator.ID, err)
		}
		if !isSubscriber {
			continue
		}
		groups, err := r.store.ListManagedGroupsByCreator(ctx, creator.ID)
		if err != nil {
			return nil, fmt.Errorf("list managed groups by creator %s: %w", creator.ID, err)
		}
		for _, group := range groups {
			groupIDs = append(groupIDs, group.ChatID)
		}
	}
	return groupIDs, nil
}

// ResetViewerDataAndRevokeGroupAccess kicks the user only from tracked groups
// plus any canonically-derived eligible groups, then deletes viewer data.
// Untracked/observed-only group presence is ignored until a
// creator-configurable policy exists.
func (r *ResetService) ResetViewerDataAndRevokeGroupAccess(ctx context.Context, telegramUserID int64) (int, GroupResolutionStats, error) {
	groupIDs, stats, err := r.resolveSubLinkedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return 0, GroupResolutionStats{}, fmt.Errorf("sub linked group ids: %w", err)
	}
	for _, groupID := range groupIDs {
		if err := r.kick(ctx, groupID, telegramUserID, KickReasonViewerReset); err != nil {
			r.log.Warn("kickFromGroup during reset failed", "telegram_user_id", telegramUserID, "group_id", groupID, "error", err)
		}
	}
	if err := r.store.DeleteAllUserData(ctx, telegramUserID); err != nil {
		return 0, GroupResolutionStats{}, fmt.Errorf("delete all user data: %w", err)
	}
	return len(groupIDs), stats, nil
}

// DeleteCreatorData removes creator data owned by ownerTelegramID.
func (r *ResetService) DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error) {
	// Best-effort EventSub cleanup: load creator ID before deletion.
	if r.eventSubClean != nil {
		creator, hasCreator, loadErr := r.store.OwnedCreatorForUser(ctx, ownerTelegramID)
		if loadErr != nil {
			r.log.Warn("load creator for eventsub cleanup failed", "owner_telegram_id", ownerTelegramID, "error", loadErr)
		} else if hasCreator {
			if cleanErr := r.eventSubClean.DeleteEventSubsForCreator(ctx, creator.ID); cleanErr != nil {
				r.log.Warn("eventsub cleanup during reset failed", "creator_id", creator.ID, "error", cleanErr)
			}
		}
	}

	deletedCount, deletedNames, err = r.store.DeleteCreatorData(ctx, ownerTelegramID)
	if err != nil {
		return 0, nil, fmt.Errorf("delete creator data: %w", err)
	}
	return deletedCount, deletedNames, nil
}

func (r *ResetService) cleanupCreatorGroupsForReset(ctx context.Context, ownerTelegramID int64, action CreatorResetGroupAction) (CreatorGroupCleanupSummary, error) {
	if action == "" {
		action = CreatorResetKeepMembers
	}

	creator, hasCreator, err := r.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		return CreatorGroupCleanupSummary{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !hasCreator {
		return CreatorGroupCleanupSummary{Action: action}, nil
	}

	groups, err := r.store.ListManagedGroupsByCreator(ctx, creator.ID)
	if err != nil {
		return CreatorGroupCleanupSummary{}, fmt.Errorf("list managed groups by creator %s: %w", creator.ID, err)
	}
	summary := CreatorGroupCleanupSummary{
		Action:            action,
		ManagedGroupCount: len(groups),
	}
	if action != CreatorResetKickTrackedMembers {
		return summary, nil
	}

	targets := make([]MemberCleanupTarget, 0)
	for _, group := range groups {
		memberIDs, err := r.store.ListTrackedGroupMemberIDs(ctx, group.ChatID)
		if err != nil {
			return CreatorGroupCleanupSummary{}, fmt.Errorf("list tracked group member ids for %d: %w", group.ChatID, err)
		}
		summary.TargetedMembershipCount += len(memberIDs)
		for _, telegramUserID := range memberIDs {
			targets = append(targets, MemberCleanupTarget{
				ChatID:         group.ChatID,
				TelegramUserID: telegramUserID,
				MaxAttempts:    3,
			})
		}
	}
	if len(targets) == 0 {
		return summary, nil
	}
	_, err = r.store.CreateMemberCleanupJob(ctx, MemberCleanupJob{
		Kind:              MemberCleanupKindCreatorReset,
		OwnerTelegramID:   ownerTelegramID,
		CreatorID:         creator.ID,
		CreatorLogin:      creator.TwitchLogin,
		ManagedGroupCount: len(groups),
		Targets:           targets,
	})
	if err != nil {
		summary.QueueFailed = true
		r.log.Warn("enqueue creator reset member cleanup failed", "creator_id", creator.ID, "owner_telegram_id", ownerTelegramID, "error", err)
		return summary, nil
	}
	summary.Queued = true

	return summary, nil
}
