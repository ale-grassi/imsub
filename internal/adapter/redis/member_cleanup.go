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

const memberCleanupCompletedTTL = 7 * 24 * time.Hour

// CreateMemberCleanupJob stores a new pending background cleanup job.
func (s *Store) CreateMemberCleanupJob(ctx context.Context, job core.MemberCleanupJob) (core.MemberCleanupJob, error) {
	seq, err := s.rdb.Incr(ctx, keyMemberCleanupJobSeq()).Result()
	if err != nil {
		return core.MemberCleanupJob{}, fmt.Errorf("redis incr member cleanup seq: %w", err)
	}
	now := time.Now().UTC()
	job.ID = strconv.FormatInt(seq, 10)
	job.Status = core.MemberCleanupStatusPending
	job.TotalTargets = len(job.Targets)
	job.CreatedAt = now
	job.UpdatedAt = now

	raw, err := json.Marshal(job)
	if err != nil {
		return core.MemberCleanupJob{}, fmt.Errorf("marshal member cleanup job: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, keyMemberCleanupJob(job.ID), raw, 0)
	pipe.SAdd(ctx, keyPendingMemberCleanupJobs(), job.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return core.MemberCleanupJob{}, fmt.Errorf("redis exec create member cleanup job: %w", err)
	}
	return job, nil
}

// ListPendingMemberCleanupJobs returns persisted pending cleanup jobs.
func (s *Store) ListPendingMemberCleanupJobs(ctx context.Context) ([]core.MemberCleanupJob, error) {
	ids, err := s.rdb.SMembers(ctx, keyPendingMemberCleanupJobs()).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers pending member cleanup jobs: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]core.MemberCleanupJob, 0, len(ids))
	for _, id := range ids {
		job, ok, err := s.MemberCleanupJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok && job.Status == core.MemberCleanupStatusPending {
			out = append(out, job)
		}
	}
	return out, nil
}

// MemberCleanupJob loads one persisted cleanup job by ID.
func (s *Store) MemberCleanupJob(ctx context.Context, jobID string) (core.MemberCleanupJob, bool, error) {
	raw, err := s.rdb.Get(ctx, keyMemberCleanupJob(jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return core.MemberCleanupJob{}, false, nil
		}
		return core.MemberCleanupJob{}, false, fmt.Errorf("redis get member cleanup job: %w", err)
	}
	var job core.MemberCleanupJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return core.MemberCleanupJob{}, false, fmt.Errorf("unmarshal member cleanup job: %w", err)
	}
	return job, true, nil
}

// ClaimMemberCleanupJob acquires a short-lived processing lock for a job.
func (s *Store) ClaimMemberCleanupJob(ctx context.Context, jobID string, ttl time.Duration) (bool, error) {
	err := s.rdb.SetArgs(ctx, keyMemberCleanupJobLock(jobID), "1", redis.SetArgs{
		TTL:  ttl,
		Mode: "NX",
	}).Err()
	// SET NX replies null when the lock is already held; go-redis surfaces
	// that as redis.Nil, which is contention rather than an error.
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis setnx member cleanup job lock: %w", err)
	}
	return true, nil
}

// ReleaseMemberCleanupJob drops the processing lock so the next scheduler tick
// can continue draining a job's remaining targets without waiting for the
// lock TTL to expire.
func (s *Store) ReleaseMemberCleanupJob(ctx context.Context, jobID string) error {
	if err := s.rdb.Del(ctx, keyMemberCleanupJobLock(jobID)).Err(); err != nil {
		return fmt.Errorf("redis del member cleanup job lock: %w", err)
	}
	return nil
}

// SaveMemberCleanupJob persists cleanup-job progress.
func (s *Store) SaveMemberCleanupJob(ctx context.Context, job core.MemberCleanupJob) error {
	job.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal member cleanup job: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, keyMemberCleanupJob(job.ID), raw, 0)
	if job.Status == core.MemberCleanupStatusPending {
		pipe.SAdd(ctx, keyPendingMemberCleanupJobs(), job.ID)
	} else {
		pipe.SRem(ctx, keyPendingMemberCleanupJobs(), job.ID)
		pipe.Expire(ctx, keyMemberCleanupJob(job.ID), memberCleanupCompletedTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec save member cleanup job: %w", err)
	}
	return nil
}
