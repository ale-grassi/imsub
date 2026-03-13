package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"imsub/internal/events"
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

type viewerIdentityStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
}

type viewerStore interface {
	viewerIdentityStore
	viewerResolverStore
	viewerTrackedMembershipStore
}

// ViewerService orchestrates viewer subscription-to-group eligibility, cache sync,
// and invite creation through focused internal components.
type ViewerService struct {
	identity viewerIdentityStore
	resolver *viewerEligibilityResolver
	cache    *viewerMembershipCache
	invites  *viewerInviteBuilder
}

// NewViewerService creates a viewer service with optional logger fallback.
func NewViewerService(store viewerStore, group GroupOps, logger *slog.Logger, obs events.EventSink) *ViewerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ViewerService{
		identity: store,
		resolver: newViewerEligibilityResolver(store, group, logger, obs),
		cache:    newViewerMembershipCache(store, logger, obs),
		invites:  newViewerInviteBuilder(group, logger, obs),
	}
}

// LoadIdentity returns viewer identity for telegramUserID, if linked.
func (v *ViewerService) LoadIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	identity, found, err := v.identity.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return UserIdentity{}, false, fmt.Errorf("load user identity: %w", err)
	}
	return identity, found, nil
}

// BuildJoinTargets resolves active subscriptions and invite links for a viewer.
func (v *ViewerService) BuildJoinTargets(ctx context.Context, telegramUserID int64, twitchUserID string) (JoinTargets, error) {
	plan, err := v.resolver.resolve(ctx, telegramUserID, twitchUserID)
	if err != nil {
		return JoinTargets{}, err
	}

	v.cache.sync(ctx, telegramUserID, plan)
	joinLinks := v.invites.build(ctx, telegramUserID, plan.inviteGroups)

	return JoinTargets{
		ActiveCreatorNames: plan.activeCreatorNames,
		JoinLinks:          joinLinks,
	}, nil
}

// BuildJoinTargetsForCreator resolves invite links for a single creator only.
func (v *ViewerService) BuildJoinTargetsForCreator(ctx context.Context, creatorID string, telegramUserID int64, twitchUserID string) (JoinTargets, error) {
	plan, err := v.resolver.resolveForCreator(ctx, creatorID, telegramUserID, twitchUserID)
	if err != nil {
		return JoinTargets{}, err
	}

	v.cache.sync(ctx, telegramUserID, plan)
	joinLinks := v.invites.build(ctx, telegramUserID, plan.inviteGroups)

	return JoinTargets{
		ActiveCreatorNames: plan.activeCreatorNames,
		JoinLinks:          joinLinks,
	}, nil
}

// resolveJoinPlan exposes the resolver seam for focused package tests.
func (v *ViewerService) resolveJoinPlan(ctx context.Context, telegramUserID int64, twitchUserID string) (resolvedJoinPlan, error) {
	return v.resolver.resolve(ctx, telegramUserID, twitchUserID)
}

type resolvedJoinGroup struct {
	creatorName string
	group       ManagedGroup
}

type resolvedJoinPlan struct {
	activeCreatorNames []string
	inviteGroups       []resolvedJoinGroup
	untrackedGroups    []int64
}

type viewerResolverStore interface {
	ListActiveCreatorGroups(ctx context.Context) ([]ActiveCreatorGroups, error)
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error)
}

type viewerMembershipChecker interface {
	IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool
}

type viewerEligibilityResolver struct {
	store      viewerResolverStore
	membership viewerMembershipChecker
	log        *slog.Logger
	obs        events.EventSink
}

func newViewerEligibilityResolver(store viewerResolverStore, membership viewerMembershipChecker, logger *slog.Logger, obs events.EventSink) *viewerEligibilityResolver {
	return &viewerEligibilityResolver{
		store:      store,
		membership: membership,
		log:        logger,
		obs:        obs,
	}
}

func (r *viewerEligibilityResolver) resolve(ctx context.Context, telegramUserID int64, twitchUserID string) (resolvedJoinPlan, error) {
	active, err := r.store.ListActiveCreatorGroups(ctx)
	if err != nil {
		r.log.Warn("build join targets list active creator groups failed", "error", err)
		return resolvedJoinPlan{}, fmt.Errorf("list active creator groups: %w", err)
	}
	return r.resolveActiveCreators(ctx, active, telegramUserID, twitchUserID)
}

func (r *viewerEligibilityResolver) resolveForCreator(ctx context.Context, creatorID string, telegramUserID int64, twitchUserID string) (resolvedJoinPlan, error) {
	active, err := r.store.ListActiveCreatorGroups(ctx)
	if err != nil {
		r.log.Warn("build creator join targets list active creator groups failed", "creator_id", creatorID, "error", err)
		return resolvedJoinPlan{}, fmt.Errorf("list active creator groups: %w", err)
	}
	// TODO: Replace this with a creator-scoped store read if active creator count
	// grows enough for the full scan to matter.
	filtered := make([]ActiveCreatorGroups, 0, 1)
	for _, item := range active {
		if item.Creator.ID == creatorID {
			filtered = append(filtered, item)
			break
		}
	}
	return r.resolveActiveCreators(ctx, filtered, telegramUserID, twitchUserID)
}

func (r *viewerEligibilityResolver) resolveActiveCreators(ctx context.Context, active []ActiveCreatorGroups, telegramUserID int64, twitchUserID string) (resolvedJoinPlan, error) {
	out := resolvedJoinPlan{
		activeCreatorNames: make([]string, 0, len(active)),
		inviteGroups:       make([]resolvedJoinGroup, 0, len(active)),
		untrackedGroups:    make([]int64, 0, len(active)),
	}
	for _, item := range active {
		isSubscriber, err := r.store.IsCreatorSubscriber(ctx, item.Creator.ID, twitchUserID)
		if err != nil {
			r.log.Warn("build join targets is creator subscriber failed", "creator_id", item.Creator.ID, "error", err)
			continue
		}
		if !isSubscriber {
			for _, group := range item.Groups {
				out.untrackedGroups = append(out.untrackedGroups, group.ChatID)
			}
			continue
		}
		if item.Creator.BlocklistSyncEnabled {
			isBlocked, err := r.store.IsCreatorBlocked(ctx, item.Creator.ID, twitchUserID)
			if err != nil {
				r.log.Warn("build join targets is creator blocked failed", "creator_id", item.Creator.ID, "error", err)
				continue
			}
			if isBlocked {
				for _, group := range item.Groups {
					out.untrackedGroups = append(out.untrackedGroups, group.ChatID)
				}
				continue
			}
		}

		creatorName := item.Creator.TwitchDisplayName
		if creatorName == "" {
			creatorName = item.Creator.TwitchLogin
		}
		out.activeCreatorNames = append(out.activeCreatorNames, creatorName)
		for _, group := range item.Groups {
			if r.membership.IsGroupMember(ctx, group.ChatID, telegramUserID) {
				continue
			}
			out.inviteGroups = append(out.inviteGroups, resolvedJoinGroup{
				creatorName: creatorName,
				group:       group,
			})
		}
	}

	slices.Sort(out.activeCreatorNames)
	recordViewerJoinTargets(ctx, r.obs, "active_creators", len(out.activeCreatorNames))
	recordViewerJoinTargets(ctx, r.obs, "invite_groups", len(out.inviteGroups))
	return out, nil
}

type viewerTrackedMembershipStore interface {
	AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

type viewerMembershipCache struct {
	store viewerTrackedMembershipStore
	log   *slog.Logger
	obs   events.EventSink
}

func newViewerMembershipCache(store viewerTrackedMembershipStore, logger *slog.Logger, obs events.EventSink) *viewerMembershipCache {
	return &viewerMembershipCache{
		store: store,
		log:   logger,
		obs:   obs,
	}
}

func (c *viewerMembershipCache) sync(ctx context.Context, telegramUserID int64, plan resolvedJoinPlan) {
	now := time.Now().UTC()

	for _, groupChatID := range plan.untrackedGroups {
		if err := c.store.RemoveTrackedGroupMember(ctx, groupChatID, telegramUserID); err != nil {
			c.log.Warn("remove tracked group member failed", "telegram_user_id", telegramUserID, "chat_id", groupChatID, "error", err)
		}
	}
	for _, joinGroup := range plan.inviteGroups {
		if err := c.store.AddTrackedGroupMember(ctx, joinGroup.group.ChatID, telegramUserID, "viewer_join_target", now); err != nil {
			c.log.Warn("add tracked group member failed", "telegram_user_id", telegramUserID, "chat_id", joinGroup.group.ChatID, "error", err)
		}
	}

	recordViewerJoinTargets(ctx, c.obs, "cache_removes", len(plan.untrackedGroups))
	recordViewerJoinTargets(ctx, c.obs, "cache_adds", len(plan.inviteGroups))
}

type viewerInviteOps interface {
	CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error)
}

type viewerInviteBuilder struct {
	group viewerInviteOps
	log   *slog.Logger
	obs   events.EventSink
}

func newViewerInviteBuilder(group viewerInviteOps, logger *slog.Logger, obs events.EventSink) *viewerInviteBuilder {
	return &viewerInviteBuilder{
		group: group,
		log:   logger,
		obs:   obs,
	}
}

func (b *viewerInviteBuilder) build(ctx context.Context, telegramUserID int64, groups []resolvedJoinGroup) []JoinLink {
	links := make([]JoinLink, 0, len(groups))
	for _, joinGroup := range groups {
		inviteLink, err := b.group.CreateInviteLink(ctx, joinGroup.group.ChatID, telegramUserID, joinGroup.creatorName)
		if err != nil {
			b.log.Warn("create invite link failed", "creator_name", joinGroup.creatorName, "chat_id", joinGroup.group.ChatID, "error", err)
			recordViewerInviteLink(ctx, b.obs, "failed")
			continue
		}
		links = append(links, JoinLink{
			CreatorName: joinGroup.creatorName,
			GroupName:   joinGroup.group.GroupName,
			InviteLink:  inviteLink,
		})
		recordViewerInviteLink(ctx, b.obs, "ok")
	}
	recordViewerJoinTargets(ctx, b.obs, "join_links", len(links))

	return links
}

func recordViewerJoinTargets(ctx context.Context, obs events.EventSink, kind string, count int) {
	if obs == nil || count <= 0 {
		return
	}
	obs.Emit(ctx, events.Event{
		Name:   events.NameViewerJoinTarget,
		Fields: map[string]string{"kind": kind},
		Count:  count,
	})
}

func recordViewerInviteLink(ctx context.Context, obs events.EventSink, result string) {
	if obs == nil {
		return
	}
	obs.Emit(ctx, events.Event{
		Name:    events.NameViewerInviteLink,
		Outcome: result,
	})
}
