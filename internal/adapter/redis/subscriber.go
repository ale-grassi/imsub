package redis

import (
	"context"
	"fmt"
	"time"
)

// --- Subscriber cache ---

// IsCreatorSubscriber reports whether a Twitch user is in the creator's subscriber set.
func (s *Store) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	return s.rdb.SIsMember(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Result()
}

// AddCreatorSubscriber adds a Twitch user to the creator's subscriber set.
func (s *Store) AddCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	return s.rdb.SAdd(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Err()
}

// RemoveCreatorSubscriber removes a Twitch user from the creator's subscriber set.
func (s *Store) RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	return s.rdb.SRem(ctx, keyCreatorSubscribers(creatorID), twitchUserID).Err()
}

// CreatorSubscriberCount returns the number of cached subscribers for a creator.
func (s *Store) CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error) {
	return s.rdb.SCard(ctx, keyCreatorSubscribers(creatorID)).Result()
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
	return s.rdb.SAdd(ctx, tmpKey, args...).Err()
}

// FinalizeSubscriberDump atomically replaces the creator's subscriber set with the dump.
func (s *Store) FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error {
	destKey := keyCreatorSubscribers(creatorID)
	if !hasData {
		return s.rdb.Del(ctx, destKey).Err()
	}
	return s.rdb.Rename(ctx, tmpKey, destKey).Err()
}

// CleanupSubscriberDump removes a temporary subscriber dump key.
func (s *Store) CleanupSubscriberDump(ctx context.Context, tmpKey string) {
	if err := s.rdb.Del(ctx, tmpKey).Err(); err != nil {
		s.log().Warn("cleanup subscriber dump failed", "tmp_key", tmpKey, "error", err)
	}
}
