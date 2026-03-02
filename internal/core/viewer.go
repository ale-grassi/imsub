package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
)

// GroupOps abstracts Telegram group membership and invite operations.
type GroupOps interface {
	IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool
	CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error)
}

// JoinLink is a transport-agnostic join action for one creator group.
type JoinLink struct {
	CreatorName string
	GroupName   string
	InviteLink  string
}

// JoinTargets contains the viewer's active creators and join links.
type JoinTargets struct {
	ActiveCreatorNames []string
	JoinLinks          []JoinLink
}

type viewerStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	ListActiveCreators(ctx context.Context) ([]Creator, error)
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	RemoveUserCreatorByTelegram(ctx context.Context, telegramUserID int64, creatorID string) error
	AddUserCreatorMembership(ctx context.Context, telegramUserID int64, creatorID string) error
}

// Viewer owns viewer subscription-to-group eligibility logic.
type Viewer struct {
	store viewerStore
	group GroupOps
	log   *slog.Logger
}

// NewViewer creates a Viewer service with optional logger fallback.
func NewViewer(store viewerStore, group GroupOps, logger *slog.Logger) *Viewer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Viewer{
		store: store,
		group: group,
		log:   logger,
	}
}

// LoadIdentity returns viewer identity for telegramUserID, if linked.
func (v *Viewer) LoadIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	return v.store.UserIdentity(ctx, telegramUserID)
}

// BuildJoinTargets resolves active subscriptions and invite links for a viewer.
func (v *Viewer) BuildJoinTargets(ctx context.Context, telegramUserID int64, twitchUserID string) (JoinTargets, error) {
	creators, err := v.store.ListActiveCreators(ctx)
	if err != nil {
		v.log.Warn("build join targets list active creators failed", "error", err)
		return JoinTargets{}, fmt.Errorf("list active creators: %w", err)
	}

	out := JoinTargets{
		ActiveCreatorNames: make([]string, 0, len(creators)),
		JoinLinks:          make([]JoinLink, 0, len(creators)),
	}
	for _, creator := range creators {
		isSubscriber, err := v.store.IsCreatorSubscriber(ctx, creator.ID, twitchUserID)
		if err != nil {
			v.log.Warn("build join targets is creator subscriber failed", "creator_id", creator.ID, "error", err)
			continue
		}
		if !isSubscriber {
			if err := v.store.RemoveUserCreatorByTelegram(ctx, telegramUserID, creator.ID); err != nil {
				v.log.Warn("remove user creator membership failed", "telegram_user_id", telegramUserID, "creator_id", creator.ID, "error", err)
			}
			continue
		}

		out.ActiveCreatorNames = append(out.ActiveCreatorNames, creator.Name)

		if creator.GroupChatID != 0 && v.group.IsGroupMember(ctx, creator.GroupChatID, telegramUserID) {
			continue
		}

		if err := v.store.AddUserCreatorMembership(ctx, telegramUserID, creator.ID); err != nil {
			v.log.Warn("add user creator membership failed", "telegram_user_id", telegramUserID, "creator_id", creator.ID, "error", err)
		}

		inviteLink, err := v.group.CreateInviteLink(ctx, creator.GroupChatID, telegramUserID, creator.Name)
		if err != nil {
			v.log.Warn("create invite link failed", "creator_id", creator.ID, "error", err)
			continue
		}

		out.JoinLinks = append(out.JoinLinks, JoinLink{
			CreatorName: creator.Name,
			GroupName:   creator.GroupName,
			InviteLink:  inviteLink,
		})
	}

	slices.Sort(out.ActiveCreatorNames)
	return out, nil
}
