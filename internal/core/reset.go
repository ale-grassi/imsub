package core

import (
	"context"
	"log/slog"
	"slices"
)

type kickFunc func(ctx context.Context, groupChatID int64, telegramUserID int64) error

type resetStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	UserCreatorIDs(ctx context.Context, telegramUserID int64) ([]string, error)
	LoadCreatorsByIDs(ctx context.Context, ids []string, filter func(Creator) bool) ([]Creator, error)
	DeleteAllUserData(ctx context.Context, telegramUserID int64) error
	DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error)
}

// Resetter coordinates viewer and creator reset workflows.
type Resetter struct {
	store resetStore
	kick  kickFunc
	log   *slog.Logger
}

// ScopeState describes which reset scopes currently exist for a user.
type ScopeState struct {
	Identity    UserIdentity
	HasIdentity bool
	Creator     Creator
	HasCreator  bool
}

// ViewerResetResult contains the outcome of a viewer reset.
type ViewerResetResult struct {
	HasIdentity bool
	Identity    UserIdentity
	GroupCount  int
}

// CreatorResetResult contains the outcome of a creator reset.
type CreatorResetResult struct {
	DeletedCount int
	DeletedNames []string
}

// BothResetResult contains the outcome of running both reset scopes.
type BothResetResult struct {
	HasIdentity  bool
	Identity     UserIdentity
	GroupCount   int
	DeletedCount int
	DeletedNames []string
}

// NewResetter creates a Resetter with optional logger fallback.
func NewResetter(store resetStore, kick kickFunc, logger *slog.Logger) *Resetter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Resetter{
		store: store,
		kick:  kick,
		log:   logger,
	}
}

// LoadScopes resolves whether viewer and/or creator state currently exists.
func (r *Resetter) LoadScopes(ctx context.Context, telegramUserID int64) (ScopeState, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return ScopeState{}, err
	}
	creator, hasCreator, err := r.store.OwnedCreatorForUser(ctx, telegramUserID)
	if err != nil {
		return ScopeState{}, err
	}
	return ScopeState{
		Identity:    identity,
		HasIdentity: hasIdentity,
		Creator:     creator,
		HasCreator:  hasCreator,
	}, nil
}

// CountViewerGroups returns how many creator groups the user may be removed from.
func (r *Resetter) CountViewerGroups(ctx context.Context, telegramUserID int64) (int, error) {
	return r.CountSubLinkedGroupsForUser(ctx, telegramUserID)
}

// ExecuteViewerReset removes viewer-linked data and group access.
func (r *Resetter) ExecuteViewerReset(ctx context.Context, telegramUserID int64) (ViewerResetResult, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return ViewerResetResult{}, err
	}
	if !hasIdentity {
		return ViewerResetResult{HasIdentity: false}, nil
	}
	groupCount, err := r.ResetViewerDataAndRevokeGroupAccess(ctx, telegramUserID)
	if err != nil {
		return ViewerResetResult{}, err
	}
	return ViewerResetResult{
		HasIdentity: true,
		Identity:    identity,
		GroupCount:  groupCount,
	}, nil
}

// ExecuteCreatorReset removes creator-owned data.
func (r *Resetter) ExecuteCreatorReset(ctx context.Context, telegramUserID int64) (CreatorResetResult, error) {
	deletedCount, deletedNames, err := r.DeleteCreatorData(ctx, telegramUserID)
	if err != nil {
		return CreatorResetResult{}, err
	}
	return CreatorResetResult{
		DeletedCount: deletedCount,
		DeletedNames: deletedNames,
	}, nil
}

// ExecuteBothReset performs viewer and creator reset scopes together.
func (r *Resetter) ExecuteBothReset(ctx context.Context, telegramUserID int64) (BothResetResult, error) {
	identity, hasIdentity, err := r.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return BothResetResult{}, err
	}

	groupCount := 0
	if hasIdentity {
		groupCount, err = r.ResetViewerDataAndRevokeGroupAccess(ctx, telegramUserID)
		if err != nil {
			return BothResetResult{}, err
		}
	}

	deletedCount, deletedNames, err := r.DeleteCreatorData(ctx, telegramUserID)
	if err != nil {
		return BothResetResult{}, err
	}

	return BothResetResult{
		HasIdentity:  hasIdentity,
		Identity:     identity,
		GroupCount:   groupCount,
		DeletedCount: deletedCount,
		DeletedNames: deletedNames,
	}, nil
}

// CountSubLinkedGroupsForUser returns the number of linked creator groups for the user.
func (r *Resetter) CountSubLinkedGroupsForUser(ctx context.Context, telegramUserID int64) (int, error) {
	groupIDs, err := r.SubLinkedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return 0, err
	}
	return len(groupIDs), nil
}

// SubLinkedGroupIDsForUser returns sorted linked creator group IDs for the user.
func (r *Resetter) SubLinkedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, error) {
	creatorIDs, err := r.store.UserCreatorIDs(ctx, telegramUserID)
	if err != nil {
		return nil, err
	}
	if len(creatorIDs) == 0 {
		return nil, nil
	}

	creators, err := r.store.LoadCreatorsByIDs(ctx, creatorIDs, func(c Creator) bool {
		return c.GroupChatID != 0
	})
	if err != nil {
		return nil, err
	}

	out := make([]int64, 0, len(creators))
	for _, c := range creators {
		out = append(out, c.GroupChatID)
	}
	slices.Sort(out)
	return out, nil
}

// ResetViewerDataAndRevokeGroupAccess kicks the user from linked groups and deletes viewer data.
func (r *Resetter) ResetViewerDataAndRevokeGroupAccess(ctx context.Context, telegramUserID int64) (int, error) {
	groupIDs, err := r.SubLinkedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return 0, err
	}
	for _, groupID := range groupIDs {
		if err := r.kick(ctx, groupID, telegramUserID); err != nil {
			r.log.Warn("kickFromGroup during reset failed", "telegram_user_id", telegramUserID, "group_id", groupID, "error", err)
		}
	}
	if err := r.store.DeleteAllUserData(ctx, telegramUserID); err != nil {
		return 0, err
	}
	return len(groupIDs), nil
}

// DeleteCreatorData removes creator data owned by ownerTelegramID.
func (r *Resetter) DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error) {
	return r.store.DeleteCreatorData(ctx, ownerTelegramID)
}
