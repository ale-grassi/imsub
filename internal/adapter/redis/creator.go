package redis

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

// --- Creator ---

func parseCreatorTimeField(logger *slog.Logger, creatorID string, vals map[string]string, key string) time.Time {
	raw := vals[key]
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		logger.Warn("parseCreatorHash invalid timestamp field, using zero time",
			"creator_id", creatorID,
			"field", key,
			"raw", raw,
			"error", err,
		)
		return time.Time{}
	}
	return ts
}

func (s *Store) parseCreatorHash(vals map[string]string, fallbackID string) core.Creator {
	ownerID, _ := strconv.ParseInt(vals["owner_telegram_id"], 10, 64)
	groupChatID, _ := strconv.ParseInt(vals["group_chat_id"], 10, 64)
	updatedAt, err := time.Parse(time.RFC3339, vals["updated_at"])
	if err != nil {
		s.log().Warn("parseCreatorHash invalid updated_at, using current time",
			"creator_id", fallbackID,
			"updated_at_raw", vals["updated_at"],
			"error", err,
		)
		updatedAt = time.Now().UTC()
	}
	c := core.Creator{
		ID:              vals["id"],
		Name:            vals["name"],
		OwnerTelegramID: ownerID,
		GroupChatID:     groupChatID,
		GroupName:       vals["group_name"],
		AccessToken:     vals["access_token"],
		RefreshToken:    vals["refresh_token"],
		UpdatedAt:       updatedAt,
		AuthStatus:      core.CreatorAuthStatus(vals["auth_status"]),
		AuthErrorCode:   vals["auth_error_code"],
		AuthStatusAt:    parseCreatorTimeField(s.log(), fallbackID, vals, "auth_status_changed_at"),
		LastSyncAt:      parseCreatorTimeField(s.log(), fallbackID, vals, "last_subscriber_sync_at"),
		LastNoticeAt:    parseCreatorTimeField(s.log(), fallbackID, vals, "last_reconnect_notice_at"),
	}
	if c.ID == "" {
		c.ID = fallbackID
	}
	if c.Name == "" {
		c.Name = c.ID
	}
	if c.AuthStatus == "" {
		c.AuthStatus = core.CreatorAuthHealthy
	}
	return c
}

// Creator returns the creator with the given ID, or false if not found.
func (s *Store) Creator(ctx context.Context, creatorID string) (core.Creator, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, keyCreator(creatorID)).Result()
	if err != nil {
		return core.Creator{}, false, fmt.Errorf("redis hgetall creator: %w", err)
	}
	if len(vals) == 0 {
		return core.Creator{}, false, nil
	}
	return s.parseCreatorHash(vals, creatorID), true, nil
}

func (s *Store) loadCreatorsBySet(ctx context.Context, setKey string, filter func(core.Creator) bool) ([]core.Creator, error) {
	ids, err := s.rdb.SMembers(ctx, setKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers %s: %w", setKey, err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	slices.Sort(ids)
	return s.LoadCreatorsByIDs(ctx, ids, filter)
}

// LoadCreatorsByIDs fetches creators by ID in a single pipeline, applying an optional filter.
func (s *Store) LoadCreatorsByIDs(ctx context.Context, ids []string, filter func(core.Creator) bool) ([]core.Creator, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, keyCreator(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis exec load creators by ids: %w", err)
	}

	out := make([]core.Creator, 0, len(ids))
	for i, id := range ids {
		vals, err := cmds[i].Result()
		if err != nil || len(vals) == 0 {
			continue
		}
		c := s.parseCreatorHash(vals, id)
		if filter != nil && !filter(c) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// ListCreators returns all registered creators.
func (s *Store) ListCreators(ctx context.Context) ([]core.Creator, error) {
	return s.loadCreatorsBySet(ctx, keyCreatorsSet(), nil)
}

// ListActiveCreators returns creators that have a bound group chat.
func (s *Store) ListActiveCreators(ctx context.Context) ([]core.Creator, error) {
	return s.loadCreatorsBySet(ctx, keyActiveCreatorsSet(), func(c core.Creator) bool {
		return c.GroupChatID != 0
	})
}

// OwnedCreatorForUser returns the creator owned by the given Telegram user.
func (s *Store) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	ids, err := s.rdb.SMembers(ctx, keyCreatorByOwner(ownerTelegramID)).Result()
	if err != nil {
		return core.Creator{}, false, fmt.Errorf("redis smembers creator by owner: %w", err)
	}
	if len(ids) == 0 {
		return core.Creator{}, false, nil
	}
	slices.Sort(ids)
	if len(ids) > 1 {
		s.log().Warn("multiple creators found for owner, selecting first valid deterministically", "owner_telegram_id", ownerTelegramID, "count", len(ids))
	}
	for _, creatorID := range ids {
		c, ok, getErr := s.Creator(ctx, creatorID)
		if getErr != nil {
			return core.Creator{}, false, getErr
		}
		if !ok || c.OwnerTelegramID != ownerTelegramID {
			continue
		}
		return c, true, nil
	}
	return core.Creator{}, false, nil
}

// UpsertCreator creates or updates a creator record and its index entries.
func (s *Store) UpsertCreator(ctx context.Context, c core.Creator) error {
	existing, exists, err := s.Creator(ctx, c.ID)
	if err != nil {
		return err
	}
	if c.AuthStatus == "" {
		c.AuthStatus = core.CreatorAuthHealthy
	}

	activeGroupChatID := c.GroupChatID
	if activeGroupChatID == 0 && exists {
		activeGroupChatID = existing.GroupChatID
	}

	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyCreatorsSet(), c.ID)
	pipe.SAdd(ctx, keyCreatorByOwner(c.OwnerTelegramID), c.ID)
	if exists && existing.OwnerTelegramID != 0 && existing.OwnerTelegramID != c.OwnerTelegramID {
		pipe.SRem(ctx, keyCreatorByOwner(existing.OwnerTelegramID), c.ID)
	}

	pipe.HSet(ctx, keyCreator(c.ID), map[string]any{
		"id":                c.ID,
		"name":              c.Name,
		"owner_telegram_id": strconv.FormatInt(c.OwnerTelegramID, 10),
		"access_token":      c.AccessToken,
		"refresh_token":     c.RefreshToken,
		"updated_at":        time.Now().UTC().Format(time.RFC3339),
		"auth_status":       string(c.AuthStatus),
		"auth_error_code":   c.AuthErrorCode,
	})
	if !c.AuthStatusAt.IsZero() {
		pipe.HSet(ctx, keyCreator(c.ID), "auth_status_changed_at", c.AuthStatusAt.UTC().Format(time.RFC3339))
	} else {
		pipe.HDel(ctx, keyCreator(c.ID), "auth_status_changed_at")
	}
	if !c.LastSyncAt.IsZero() {
		pipe.HSet(ctx, keyCreator(c.ID), "last_subscriber_sync_at", c.LastSyncAt.UTC().Format(time.RFC3339))
	} else {
		pipe.HDel(ctx, keyCreator(c.ID), "last_subscriber_sync_at")
	}
	if !c.LastNoticeAt.IsZero() {
		pipe.HSet(ctx, keyCreator(c.ID), "last_reconnect_notice_at", c.LastNoticeAt.UTC().Format(time.RFC3339))
	} else {
		pipe.HDel(ctx, keyCreator(c.ID), "last_reconnect_notice_at")
	}

	if activeGroupChatID != 0 {
		pipe.SAdd(ctx, keyActiveCreatorsSet(), c.ID)
	} else {
		pipe.SRem(ctx, keyActiveCreatorsSet(), c.ID)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis exec upsert creator: %w", err)
	}
	return nil
}

// DeleteCreatorData removes a creator and cleans up member reverse-index entries.
func (s *Store) DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error) {
	c, ok, err := s.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return 0, nil, nil
	}
	memberIDs, err := s.rdb.SMembers(ctx, keyCreatorMembers(c.ID)).Result()
	if err != nil {
		return 0, nil, fmt.Errorf("redis smembers creator members: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	for _, tgStr := range memberIDs {
		tgID, parseErr := strconv.ParseInt(tgStr, 10, 64)
		if parseErr != nil {
			s.log().Warn("DeleteCreatorData invalid member telegram id, skipping reverse-index cleanup", "creator_id", c.ID, "member_raw", tgStr, "error", parseErr)
			continue
		}
		pipe.SRem(ctx, keyUserCreators(tgID), c.ID)
	}
	pipe.Del(ctx, keyCreatorMembers(c.ID))
	pipe.Del(ctx, keyCreatorSubscribers(c.ID))
	pipe.Del(ctx, keyCreator(c.ID))
	pipe.SRem(ctx, keyCreatorsSet(), c.ID)
	pipe.SRem(ctx, keyActiveCreatorsSet(), c.ID)
	pipe.SRem(ctx, keyCreatorByOwner(ownerTelegramID), c.ID)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, nil, fmt.Errorf("redis exec delete creator data: %w", err)
	}

	return 1, []string{c.Name}, nil
}

// UpdateCreatorGroup binds or unbinds a Telegram group to a creator.
func (s *Store) UpdateCreatorGroup(ctx context.Context, creatorID string, groupChatID int64, groupName string) error {
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, keyCreator(creatorID), map[string]any{
		"group_chat_id": strconv.FormatInt(groupChatID, 10),
		"group_name":    groupName,
	})
	if groupChatID != 0 {
		pipe.SAdd(ctx, keyActiveCreatorsSet(), creatorID)
	} else {
		pipe.SRem(ctx, keyActiveCreatorsSet(), creatorID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec update creator group: %w", err)
	}
	return nil
}

// UpdateCreatorTokens replaces the creator's OAuth access and refresh tokens.
func (s *Store) UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string) error {
	fields := map[string]any{
		"access_token": accessToken,
		"updated_at":   time.Now().UTC().Format(time.RFC3339),
	}
	if refreshToken != "" {
		fields["refresh_token"] = refreshToken
	}
	if err := s.rdb.HSet(ctx, keyCreator(creatorID), fields).Err(); err != nil {
		return fmt.Errorf("redis hset update creator tokens: %w", err)
	}
	return nil
}

// MarkCreatorAuthReconnectRequired records that a creator must reconnect their Twitch account.
func (s *Store) MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (bool, error) {
	creator, ok, err := s.Creator(ctx, creatorID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if creator.AuthStatus == core.CreatorAuthReconnectRequired && creator.AuthErrorCode == errorCode {
		return false, nil
	}
	fields := map[string]any{
		"auth_status":            string(core.CreatorAuthReconnectRequired),
		"auth_error_code":        errorCode,
		"auth_status_changed_at": at.UTC().Format(time.RFC3339),
	}
	if err := s.rdb.HSet(ctx, keyCreator(creatorID), fields).Err(); err != nil {
		return false, fmt.Errorf("redis hset creator auth reconnect required: %w", err)
	}
	return true, nil
}

// MarkCreatorAuthHealthy clears reconnect-required auth state for a creator.
func (s *Store) MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error {
	fields := map[string]any{
		"auth_status":            string(core.CreatorAuthHealthy),
		"auth_error_code":        "",
		"auth_status_changed_at": at.UTC().Format(time.RFC3339),
	}
	if err := s.rdb.HSet(ctx, keyCreator(creatorID), fields).Err(); err != nil {
		return fmt.Errorf("redis hset creator auth healthy: %w", err)
	}
	return nil
}

// UpdateCreatorLastSync stores the timestamp of the last successful subscriber sync.
func (s *Store) UpdateCreatorLastSync(ctx context.Context, creatorID string, at time.Time) error {
	if err := s.rdb.HSet(ctx, keyCreator(creatorID), "last_subscriber_sync_at", at.UTC().Format(time.RFC3339)).Err(); err != nil {
		return fmt.Errorf("redis hset creator last sync: %w", err)
	}
	return nil
}

// UpdateCreatorLastReconnectNotice stores the timestamp of the last reconnect-required notice.
func (s *Store) UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error {
	if err := s.rdb.HSet(ctx, keyCreator(creatorID), "last_reconnect_notice_at", at.UTC().Format(time.RFC3339)).Err(); err != nil {
		return fmt.Errorf("redis hset creator last reconnect notice: %w", err)
	}
	return nil
}

// CreatorAuthReconnectRequiredCount counts creators currently marked as reconnect_required.
func (s *Store) CreatorAuthReconnectRequiredCount(ctx context.Context) (int, error) {
	creators, err := s.ListCreators(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, creator := range creators {
		if creator.AuthStatus == core.CreatorAuthReconnectRequired {
			total++
		}
	}
	return total, nil
}
