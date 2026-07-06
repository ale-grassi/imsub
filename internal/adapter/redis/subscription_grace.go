package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

// SubscriptionEndGraceJobID returns the deterministic pending grace job ID for
// one creator/viewer pair.
func (s *Store) SubscriptionEndGraceJobID(creatorID, twitchUserID string) string {
	return creatorID + ":" + twitchUserID
}

// UpsertSubscriptionEndGrace stores or refreshes a pending delayed sub-end job.
func (s *Store) UpsertSubscriptionEndGrace(ctx context.Context, job core.PendingSubscriptionEndGrace) (core.PendingSubscriptionEndGrace, error) {
	now := time.Now().UTC()
	if job.ID == "" {
		job.ID = s.SubscriptionEndGraceJobID(job.CreatorID, job.TwitchUserID)
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	blob, err := json.Marshal(job)
	if err != nil {
		return core.PendingSubscriptionEndGrace{}, fmt.Errorf("marshal pending subscription-end grace: %w", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, keySubscriptionEndGraceJob(job.ID), blob, 0)
	pipe.ZAdd(ctx, keySubscriptionEndGraceDue(), redis.Z{
		Score:  float64(job.DueAt.UTC().Unix()),
		Member: job.ID,
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return core.PendingSubscriptionEndGrace{}, fmt.Errorf("redis exec upsert subscription-end grace: %w", err)
	}
	return job, nil
}

// DeleteSubscriptionEndGrace deletes a pending delayed sub-end job.
func (s *Store) DeleteSubscriptionEndGrace(ctx context.Context, creatorID, twitchUserID string) error {
	jobID := s.SubscriptionEndGraceJobID(creatorID, twitchUserID)
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, keySubscriptionEndGraceJob(jobID))
	pipe.ZRem(ctx, keySubscriptionEndGraceDue(), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec delete subscription-end grace: %w", err)
	}
	return nil
}

// ListDueSubscriptionEndGrace returns due delayed sub-end jobs up to limit.
func (s *Store) ListDueSubscriptionEndGrace(ctx context.Context, now time.Time, limit int64) ([]core.PendingSubscriptionEndGrace, error) {
	args := redis.ZRangeArgs{
		Key:     keySubscriptionEndGraceDue(),
		ByScore: true,
		Start:   "-inf",
		Stop:    strconv.FormatInt(now.UTC().Unix(), 10),
	}
	if limit > 0 {
		args.Offset = 0
		args.Count = limit
	}
	ids, err := s.rdb.ZRangeArgs(ctx, args).Result()
	if err != nil {
		return nil, fmt.Errorf("redis zrangebyscore due subscription-end grace: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = keySubscriptionEndGraceJob(id)
	}
	blobs, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget subscription-end grace jobs: %w", err)
	}

	out := make([]core.PendingSubscriptionEndGrace, 0, len(ids))
	var stale []any
	for i, id := range ids {
		raw, ok := blobs[i].(string)
		if !ok {
			// Due entry without a job blob: drop it so the sweep does not
			// re-read it forever.
			stale = append(stale, id)
			continue
		}
		var job core.PendingSubscriptionEndGrace
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return nil, fmt.Errorf("unmarshal subscription-end grace job %s: %w", id, err)
		}
		out = append(out, job)
	}
	if len(stale) > 0 {
		if err := s.rdb.ZRem(ctx, keySubscriptionEndGraceDue(), stale...).Err(); err != nil {
			s.log().Warn("prune stale subscription-end grace due entries failed", "count", len(stale), "error", err)
		}
	}
	return out, nil
}

// ClaimSubscriptionEndGrace acquires a short-lived processing lock for a due
// delayed sub-end job.
func (s *Store) ClaimSubscriptionEndGrace(ctx context.Context, jobID string, ttl time.Duration) (bool, error) {
	err := s.rdb.SetArgs(ctx, keySubscriptionEndGraceLock(jobID), "1", redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Err()
	// SET NX replies null when the lock is already held; go-redis surfaces
	// that as redis.Nil, which is contention rather than an error.
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis set nx subscription-end grace lock: %w", err)
	}
	return true, nil
}
