package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"imsub/internal/events"
)

type blocklistStore interface {
	Creator(ctx context.Context, creatorID string) (Creator, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	UpdateCreatorBlocklistSyncEnabled(ctx context.Context, creatorID string, enabled bool) error
	UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string, grantedScopes []string) error
	MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (bool, error)
	MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastBanSync(ctx context.Context, creatorID string, at time.Time) error
	NewCreatorBlocklistDumpKey(creatorID string) string
	AddToCreatorBlocklistDump(ctx context.Context, tmpKey string, userIDs []string) error
	FinalizeCreatorBlocklistDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	CleanupCreatorBlocklistDump(ctx context.Context, tmpKey string)
	AddCreatorBlockedUser(ctx context.Context, creatorID, twitchUserID string) error
	RemoveCreatorBlockedUser(ctx context.Context, creatorID, twitchUserID string) error
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]ManagedGroup, error)
	ResolveTelegramUserIDByTwitch(ctx context.Context, twitchUserID string) (int64, bool, error)
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

type blocklistGroupRevoker interface {
	KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64, reason KickReason) error
}

const creatorAuthErrorBlocklistTokenRefreshFailed = "blocklist_token_refresh_failed"

// ErrCreatorBlocklistScopeMissing reports that the creator token lacks the
// scopes needed for blocklist sync and its EventSub subscriptions.
var ErrCreatorBlocklistScopeMissing = errors.New("creator missing required blocklist scopes")

// CreatorBlocklistService manages synced creator ban-list state and enforcement.
type CreatorBlocklistService struct {
	store    blocklistStore
	twitch   TwitchAPI
	revoker  blocklistGroupRevoker
	log      *slog.Logger
	observer events.EventSink
}

// NewCreatorBlocklistService creates a creator blocklist service.
func NewCreatorBlocklistService(store blocklistStore, twitch TwitchAPI, revoker blocklistGroupRevoker, logger *slog.Logger) *CreatorBlocklistService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreatorBlocklistService{
		store:   store,
		twitch:  twitch,
		revoker: revoker,
		log:     logger,
	}
}

// SetObserver wires metrics/observability hooks into blocklist flows.
func (s *CreatorBlocklistService) SetObserver(observer events.EventSink) {
	s.observer = observer
}

// ToggleBlocklistSync enables or disables blocklist sync for a creator.
func (s *CreatorBlocklistService) ToggleBlocklistSync(ctx context.Context, ownerTelegramID int64, enabled bool) (Creator, int, error) {
	creator, ok, err := s.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		return Creator{}, 0, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok {
		return Creator{}, 0, nil
	}
	if enabled && !hasBlocklistScopes(creator.GrantedScopes) {
		return Creator{}, 0, ErrCreatorBlocklistScopeMissing
	}
	if err := s.store.UpdateCreatorBlocklistSyncEnabled(ctx, creator.ID, enabled); err != nil {
		return Creator{}, 0, fmt.Errorf("update creator blocklist sync enabled: %w", err)
	}
	creator.BlocklistSyncEnabled = enabled
	if !enabled {
		return creator, 0, nil
	}
	count, err := s.SyncCreatorBlocklist(ctx, creator)
	return creator, count, err
}

func hasBlocklistScopes(scopes []string) bool {
	return slices.Contains(scopes, ScopeModerationRead) &&
		slices.Contains(scopes, ScopeChannelModerate)
}

// SyncCreatorBlocklist refreshes the cached permanent-ban set for a creator and enforces it.
func (s *CreatorBlocklistService) SyncCreatorBlocklist(ctx context.Context, creator Creator) (int, error) {
	if ctx == nil {
		return 0, errNilContext
	}
	if !creator.BlocklistSyncEnabled {
		return 0, nil
	}

	total := 0
	var cursor string
	tmpKey := s.store.NewCreatorBlocklistDumpKey(creator.ID)
	cleanupCtx := context.WithoutCancel(ctx)
	defer s.store.CleanupCreatorBlocklistDump(cleanupCtx, tmpKey)
	refreshed := false
	wroteAny := false
	var bannedUserIDs []string

	for {
		userIDs, nextCursor, err := s.twitch.ListBannedUserPage(ctx, creator.AccessToken, creator.ID, cursor)
		if err != nil && !refreshed && isUnauthorized(err) {
			updated, refreshErr := s.refreshCreatorAccessToken(ctx, creator)
			if refreshErr != nil {
				s.emitBlocklistSync(ctx, creator.ID, "failed", total)
				s.markCreatorReconnectRequired(ctx, creator, creatorAuthErrorBlocklistTokenRefreshFailed)
				return total, fmt.Errorf("refresh access token on blocklist sync: %w", refreshErr)
			}
			creator = updated
			refreshed = true
			continue
		}
		if err != nil {
			s.emitBlocklistSync(ctx, creator.ID, "failed", total)
			return total, fmt.Errorf("list banned user page: %w", err)
		}
		total += len(userIDs)
		if len(userIDs) > 0 {
			if err := s.store.AddToCreatorBlocklistDump(ctx, tmpKey, userIDs); err != nil {
				s.emitBlocklistSync(ctx, creator.ID, "failed", total)
				return total, fmt.Errorf("add to creator blocklist dump: %w", err)
			}
			bannedUserIDs = append(bannedUserIDs, userIDs...)
			wroteAny = true
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	if err := s.store.FinalizeCreatorBlocklistDump(ctx, creator.ID, tmpKey, wroteAny); err != nil {
		s.emitBlocklistSync(ctx, creator.ID, "failed", total)
		return total, fmt.Errorf("finalize creator blocklist dump: %w", err)
	}
	if err := s.enforceCreatorBlocklist(ctx, creator, bannedUserIDs); err != nil {
		s.emitBlocklistSync(ctx, creator.ID, "failed", total)
		return total, err
	}
	if err := s.store.UpdateCreatorLastBanSync(ctx, creator.ID, time.Now().UTC()); err != nil {
		s.emitBlocklistSync(ctx, creator.ID, "failed", total)
		return total, fmt.Errorf("update creator last ban sync: %w", err)
	}
	s.emitBlocklistSync(ctx, creator.ID, "ok", total)
	return total, nil
}

// HandleBanEvent applies a Twitch channel.ban EventSub notification.
func (s *CreatorBlocklistService) HandleBanEvent(ctx context.Context, creatorID, twitchUserID string, isPermanent bool) error {
	creator, ok, err := s.store.Creator(ctx, creatorID)
	if err != nil {
		return fmt.Errorf("load creator for ban event: %w", err)
	}
	if !ok || !creator.BlocklistSyncEnabled {
		return nil
	}
	if !isPermanent {
		return nil
	}
	if err := s.store.AddCreatorBlockedUser(ctx, creatorID, twitchUserID); err != nil {
		return fmt.Errorf("add creator blocked user: %w", err)
	}
	groups, err := s.store.ListManagedGroupsByCreator(ctx, creator.ID)
	if err != nil {
		return fmt.Errorf("list managed groups by creator: %w", err)
	}
	return s.enforceBlockedUser(ctx, creator.ID, groups, twitchUserID)
}

// HandleUnbanEvent applies a Twitch channel.unban EventSub notification.
func (s *CreatorBlocklistService) HandleUnbanEvent(ctx context.Context, creatorID, twitchUserID string, _ bool) error {
	creator, ok, err := s.store.Creator(ctx, creatorID)
	if err != nil {
		return fmt.Errorf("load creator for unban event: %w", err)
	}
	if !ok || !creator.BlocklistSyncEnabled {
		return nil
	}
	if err := s.store.RemoveCreatorBlockedUser(ctx, creatorID, twitchUserID); err != nil {
		return fmt.Errorf("remove creator blocked user: %w", err)
	}
	return nil
}

func (s *CreatorBlocklistService) enforceCreatorBlocklist(ctx context.Context, creator Creator, twitchUserIDs []string) error {
	groups, err := s.store.ListManagedGroupsByCreator(ctx, creator.ID)
	if err != nil {
		return fmt.Errorf("list managed groups by creator: %w", err)
	}
	for _, twitchUserID := range twitchUserIDs {
		if err := s.enforceBlockedUser(ctx, creator.ID, groups, twitchUserID); err != nil {
			return err
		}
	}
	return nil
}

func (s *CreatorBlocklistService) enforceBlockedUser(ctx context.Context, creatorID string, groups []ManagedGroup, twitchUserID string) error {
	telegramUserID, found, err := s.store.ResolveTelegramUserIDByTwitch(ctx, twitchUserID)
	if err != nil {
		return fmt.Errorf("resolve telegram user by twitch: %w", err)
	}
	if !found {
		return nil
	}
	enforcedGroups := 0
	for _, group := range groups {
		if err := s.store.RemoveTrackedGroupMember(ctx, group.ChatID, telegramUserID); err != nil {
			return fmt.Errorf("remove tracked group member: %w", err)
		}
		if s.revoker != nil {
			if err := s.revoker.KickFromGroup(ctx, group.ChatID, telegramUserID, KickReasonBlocklist); err != nil {
				return fmt.Errorf("kick from group: %w", err)
			}
		}
		enforcedGroups++
	}
	s.emitBlocklistEnforcement(ctx, creatorID, "ok", enforcedGroups)
	return nil
}

func (s *CreatorBlocklistService) refreshCreatorAccessToken(ctx context.Context, creator Creator) (Creator, error) {
	return refreshCreatorAccessToken(ctx, creator, s.twitch, s.store, func(result string) {
		s.emitTokenRefresh(ctx, creator.ID, result)
	})
}

func (s *CreatorBlocklistService) markCreatorReconnectRequired(ctx context.Context, creator Creator, errorCode string) {
	if _, err := s.store.MarkCreatorAuthReconnectRequired(ctx, creator.ID, errorCode, time.Now().UTC()); err != nil {
		s.log.Warn("mark creator auth reconnect required failed", "creator_id", creator.ID, "error", err)
	}
}

func (s *CreatorBlocklistService) emitTokenRefresh(ctx context.Context, creatorID, result string) {
	if s == nil || s.observer == nil {
		return
	}
	s.observer.Emit(ctx, events.Event{
		Name:    events.NameCreatorTokenRefresh,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}

func (s *CreatorBlocklistService) emitBlocklistSync(ctx context.Context, creatorID, result string, count int) {
	if s == nil || s.observer == nil {
		return
	}
	s.observer.Emit(ctx, events.Event{
		Name:    events.NameCreatorBlocklistSync,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
		Count:   count,
	})
}

func (s *CreatorBlocklistService) emitBlocklistEnforcement(ctx context.Context, creatorID, result string, count int) {
	if s == nil || s.observer == nil || count <= 0 {
		return
	}
	s.observer.Emit(ctx, events.Event{
		Name:    events.NameCreatorBlocklistEnforcement,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
		Count:   count,
	})
}
