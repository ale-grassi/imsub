package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// IsCreatorBlocked reports whether a Twitch user is in the creator's synced blocklist.
func (s *Store) IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	res, err := s.rdb.SIsMember(ctx, keyCreatorBlockedUsers(creatorID), twitchUserID).Result()
	if err != nil {
		return false, fmt.Errorf("redis sismember creator blocked user: %w", err)
	}
	return res, nil
}

// AddCreatorBlockedUser adds a Twitch user to the creator's synced blocklist cache.
func (s *Store) AddCreatorBlockedUser(ctx context.Context, creatorID, twitchUserID string) error {
	if err := s.rdb.SAdd(ctx, keyCreatorBlockedUsers(creatorID), twitchUserID).Err(); err != nil {
		return fmt.Errorf("redis sadd creator blocked user: %w", err)
	}
	s.dumps.record(keyCreatorBlockedUsers(creatorID), twitchUserID, true)
	return nil
}

// RemoveCreatorBlockedUser removes a Twitch user from the creator's synced blocklist cache.
func (s *Store) RemoveCreatorBlockedUser(ctx context.Context, creatorID, twitchUserID string) error {
	if err := s.rdb.SRem(ctx, keyCreatorBlockedUsers(creatorID), twitchUserID).Err(); err != nil {
		return fmt.Errorf("redis srem creator blocked user: %w", err)
	}
	s.dumps.record(keyCreatorBlockedUsers(creatorID), twitchUserID, false)
	return nil
}

// CreatorBlockedUserCount returns the number of cached blocked Twitch users for a creator.
func (s *Store) CreatorBlockedUserCount(ctx context.Context, creatorID string) (int64, error) {
	count, err := s.rdb.SCard(ctx, keyCreatorBlockedUsers(creatorID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard creator blocked user cache: %w", err)
	}
	return count, nil
}

// NewCreatorBlocklistDumpKey returns a unique temporary Redis key for a
// blocklist dump and starts journaling live ban events so ones racing the
// dump survive the finalize RENAME.
func (s *Store) NewCreatorBlocklistDumpKey(creatorID string) string {
	tmpKey := fmt.Sprintf("%s:tmp:%d", keyCreatorBlockedUsers(creatorID), time.Now().UnixNano())
	s.dumps.begin(keyCreatorBlockedUsers(creatorID), tmpKey)
	return tmpKey
}

// AddToCreatorBlocklistDump appends Twitch user IDs to a temporary blocklist dump set.
func (s *Store) AddToCreatorBlocklistDump(ctx context.Context, tmpKey string, userIDs []string) error {
	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}
	if err := s.rdb.SAdd(skipBackupTracking(ctx), tmpKey, args...).Err(); err != nil {
		return fmt.Errorf("redis sadd creator blocklist dump: %w", err)
	}
	return nil
}

// FinalizeCreatorBlocklistDump atomically replaces the creator's blocklist
// cache with the dump, then replays ban events that arrived while it was built.
func (s *Store) FinalizeCreatorBlocklistDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error {
	destKey := keyCreatorBlockedUsers(creatorID)
	if !hasData {
		if err := s.rdb.Del(ctx, destKey).Err(); err != nil {
			return fmt.Errorf("redis del creator blocklist dest: %w", err)
		}
		s.replayDumpJournal(ctx, tmpKey)
		return nil
	}
	if err := s.rdb.Rename(ctx, tmpKey, destKey).Err(); err != nil {
		return fmt.Errorf("redis rename creator blocklist tmp to dest: %w", err)
	}
	s.replayDumpJournal(ctx, tmpKey)
	return nil
}

// CleanupCreatorBlocklistDump removes a temporary creator blocklist dump key.
func (s *Store) CleanupCreatorBlocklistDump(ctx context.Context, tmpKey string) {
	s.dumps.discard(tmpKey)
	if err := s.rdb.Del(skipBackupTracking(ctx), tmpKey).Err(); err != nil {
		s.log().Warn("cleanup creator blocklist dump failed", "tmp_key", tmpKey, "error", err)
	}
}

// ForEachCreatorBlockedUser scans the creator's synced blocklist without loading
// the complete set into memory.
func (s *Store) ForEachCreatorBlockedUser(ctx context.Context, creatorID string, fn func(twitchUserID string) error) error {
	key := keyCreatorBlockedUsers(creatorID)
	var cursor uint64
	for {
		userIDs, nextCursor, err := s.rdb.SScan(ctx, key, cursor, "*", 500).Result()
		if err != nil {
			return fmt.Errorf("redis sscan creator blocked users: %w", err)
		}
		for _, userID := range userIDs {
			if err := fn(userID); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			return nil
		}
	}
}

// ResolveTelegramUserIDByTwitch maps a Twitch user ID to a Telegram user ID if linked.
func (s *Store) ResolveTelegramUserIDByTwitch(ctx context.Context, twitchUserID string) (int64, bool, error) {
	raw, err := s.rdb.Get(ctx, keyTwitchToTelegram(twitchUserID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("redis get twitch mapping: %w", err)
	}
	id, parseErr := strconv.ParseInt(raw, 10, 64)
	if parseErr != nil {
		return 0, false, fmt.Errorf("parse telegram user id %q: %w", raw, parseErr)
	}
	return id, true, nil
}
