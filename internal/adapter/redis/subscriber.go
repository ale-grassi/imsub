package redis

import (
	"context"
	"fmt"
	"time"
)

// --- Subscriber cache ---

// IsCreatorSubscriber reports whether a Twitch user is in the creator's subscriber set.
func (s *Store) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	res, err := s.rdb.SIsMember(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Result()
	if err != nil {
		return false, fmt.Errorf("redis sismember subscriber cache: %w", err)
	}
	return res, nil
}

// AddCreatorSubscriber adds a Twitch user to the creator's subscriber set.
func (s *Store) AddCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	if err := s.rdb.SAdd(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Err(); err != nil {
		return fmt.Errorf("redis sadd creator subscriber: %w", err)
	}
	return nil
}

// RemoveCreatorSubscriber removes a Twitch user from the creator's subscriber set.
func (s *Store) RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	if err := s.rdb.SRem(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Err(); err != nil {
		return fmt.Errorf("redis srem creator subscriber: %w", err)
	}
	return nil
}

// CreatorSubscriberCount returns the number of cached subscribers for a creator.
func (s *Store) CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error) {
	count, err := s.rdb.SCard(ctx, keyCreatorSubscribers(creatorID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard subscriber cache: %w", err)
	}
	return count, nil
}

// --- Subscriber dump ---

// NewSubscriberDumpKey returns a unique temporary Redis key for a subscriber dump.
func (s *Store) NewSubscriberDumpKey(creatorID string) string {
	return fmt.Sprintf("%s:tmp:%d", keyCreatorSubscribers(creatorID), time.Now().UnixNano())
}

// AddToSubscriberDump appends user IDs to a temporary subscriber dump set.
func (s *Store) AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error {
	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}
	if err := s.rdb.SAdd(skipBackupTracking(ctx), tmpKey, args...).Err(); err != nil {
		return fmt.Errorf("redis sadd subscriber dump: %w", err)
	}
	return nil
}

// FinalizeSubscriberDump atomically replaces the creator's subscriber set with the dump.
func (s *Store) FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error {
	destKey := keyCreatorSubscribers(creatorID)
	if !hasData {
		if err := s.rdb.Del(ctx, destKey).Err(); err != nil {
			return fmt.Errorf("redis del subscriber dest: %w", err)
		}
		return nil
	}
	if err := s.rdb.Rename(ctx, tmpKey, destKey).Err(); err != nil {
		return fmt.Errorf("redis rename subscriber tmp to dest: %w", err)
	}
	return nil
}

// CleanupSubscriberDump removes a temporary subscriber dump key.
func (s *Store) CleanupSubscriberDump(ctx context.Context, tmpKey string) {
	if err := s.rdb.Del(skipBackupTracking(ctx), tmpKey).Err(); err != nil {
		s.log().Warn("cleanup subscriber dump failed", "tmp_key", tmpKey, "error", err)
	}
}
