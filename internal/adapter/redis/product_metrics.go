package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// TrackTelegramActiveUser records the most recent user-driven Telegram activity.
func (s *Store) TrackTelegramActiveUser(ctx context.Context, telegramUserID int64, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if err := s.rdb.ZAdd(ctx, keyTelegramActiveUsers(), goredis.Z{
		Score:  float64(at.UTC().Unix()),
		Member: strconv.FormatInt(telegramUserID, 10),
	}).Err(); err != nil {
		return fmt.Errorf("redis zadd telegram active user: %w", err)
	}
	return nil
}

// PruneTelegramActiveUsersBefore removes Telegram active-user entries older than the cutoff.
func (s *Store) PruneTelegramActiveUsersBefore(ctx context.Context, before time.Time) error {
	cutoff := before.UTC().Unix()
	if err := s.rdb.ZRemRangeByScore(ctx, keyTelegramActiveUsers(), "-inf", strconv.FormatInt(cutoff-1, 10)).Err(); err != nil {
		return fmt.Errorf("redis zremrangebyscore telegram active users: %w", err)
	}
	return nil
}

// CountTelegramActiveUsersSince returns the exact unique Telegram users seen since the cutoff.
// This method does not prune old entries; callers that want bounded storage should prune explicitly.
func (s *Store) CountTelegramActiveUsersSince(ctx context.Context, since time.Time) (int, error) {
	cutoff := since.UTC().Unix()
	count, err := s.rdb.ZCount(ctx, keyTelegramActiveUsers(), strconv.FormatInt(cutoff, 10), "+inf").Result()
	if err != nil {
		return 0, fmt.Errorf("redis zcount telegram active users: %w", err)
	}
	return int(count), nil
}

// CountLinkedViewerAccounts returns the current number of linked viewer identities.
func (s *Store) CountLinkedViewerAccounts(ctx context.Context) (int, error) {
	count, err := s.rdb.SCard(ctx, keyUsersSet()).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard users set: %w", err)
	}
	return int(count), nil
}

// CountLinkedCreatorAccounts returns the current number of linked creator accounts.
func (s *Store) CountLinkedCreatorAccounts(ctx context.Context) (int, error) {
	count, err := s.rdb.SCard(ctx, keyCreatorsSet()).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard creators set: %w", err)
	}
	return int(count), nil
}

// CountManagedGroups returns the current number of managed Telegram groups.
func (s *Store) CountManagedGroups(ctx context.Context) (int, error) {
	count, err := s.rdb.SCard(ctx, keyManagedGroupsSet()).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard managed groups set: %w", err)
	}
	return int(count), nil
}
