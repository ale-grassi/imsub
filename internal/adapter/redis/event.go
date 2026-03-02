package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- Event dedup ---

// MarkEventProcessed uses SET NX to deduplicate events, returning true if already processed.
func (s *Store) MarkEventProcessed(ctx context.Context, messageID string, ttl time.Duration) (alreadyProcessed bool, err error) {
	err = s.rdb.SetArgs(ctx, keyEventMessage(messageID), "1", redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Err()
	if errors.Is(err, redis.Nil) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}
