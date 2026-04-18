package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrCreatorMissing is returned by PromoteExistingMemberIfEligible when the
// creator row referenced by a managed group cannot be found. Callers can use
// errors.Is to surface this as a partial failure without aborting the sweep.
var ErrCreatorMissing = errors.New("creator missing")

// MemberEligibilityStore provides the minimum store surface needed to decide
// whether an observed Telegram member should be treated as tracked.
type MemberEligibilityStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error)
}

// PromoteExistingMemberStore is the store surface required to promote an
// already-present Telegram member from untracked to tracked.
type PromoteExistingMemberStore interface {
	MemberEligibilityStore
	Creator(ctx context.Context, creatorID string) (Creator, bool, error)
	AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

// IsEligibleTrackedMember reports whether telegramUserID currently qualifies
// for tracked membership under creator. Pass twitchUserID when the caller has
// already resolved it; otherwise pass "" and the store will be consulted.
func IsEligibleTrackedMember(ctx context.Context, store MemberEligibilityStore, god *GodAccessChecker, creator Creator, telegramUserID int64, twitchUserID string) (bool, error) {
	if god != nil && god.IsGodTelegramUser(telegramUserID) {
		return true, nil
	}
	if store == nil {
		return false, nil
	}

	if twitchUserID == "" {
		identity, found, err := store.UserIdentity(ctx, telegramUserID)
		if err != nil {
			return false, fmt.Errorf("load user identity: %w", err)
		}
		if !found || identity.TwitchUserID == "" {
			return false, nil
		}
		twitchUserID = identity.TwitchUserID
	}

	subscribed, err := store.IsCreatorSubscriber(ctx, creator.ID, twitchUserID)
	if err != nil {
		return false, fmt.Errorf("check creator subscriber: %w", err)
	}
	if !subscribed {
		return false, nil
	}
	if !creator.BlocklistSyncEnabled {
		return true, nil
	}

	blocked, err := store.IsCreatorBlocked(ctx, creator.ID, twitchUserID)
	if err != nil {
		return false, fmt.Errorf("check creator blocked: %w", err)
	}
	return !blocked, nil
}

// PromoteExistingMemberIfEligible loads the creator, checks eligibility, and on
// success moves telegramUserID from untracked to tracked with the given source.
// Returns (promoted, err). A missing creator row is surfaced as ErrCreatorMissing
// with promoted=false so callers can decide whether to alert.
func PromoteExistingMemberIfEligible(
	ctx context.Context,
	store PromoteExistingMemberStore,
	god *GodAccessChecker,
	group ManagedGroup,
	telegramUserID int64,
	source string,
	now time.Time,
) (bool, error) {
	if store == nil {
		return false, nil
	}
	creator, found, err := store.Creator(ctx, group.CreatorID)
	if err != nil {
		return false, fmt.Errorf("load creator: %w", err)
	}
	if !found {
		return false, fmt.Errorf("%w: creator_id=%s chat_id=%d", ErrCreatorMissing, group.CreatorID, group.ChatID)
	}
	eligible, err := IsEligibleTrackedMember(ctx, store, god, creator, telegramUserID, "")
	if err != nil {
		return false, fmt.Errorf("check member eligibility: %w", err)
	}
	if !eligible {
		return false, nil
	}
	if err := store.AddTrackedGroupMember(ctx, group.ChatID, telegramUserID, source, now); err != nil {
		return false, fmt.Errorf("track existing member: %w", err)
	}
	if err := store.RemoveUntrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
		return true, fmt.Errorf("remove untracked existing member: %w", err)
	}
	return true, nil
}
