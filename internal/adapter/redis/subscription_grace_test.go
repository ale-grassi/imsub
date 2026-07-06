package redis

import (
	"testing"
	"time"

	"imsub/internal/core"

	goredis "github.com/redis/go-redis/v9"
)

func TestListDueSubscriptionEndGracePrunesStaleDueEntries(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()

	job, err := s.UpsertSubscriptionEndGrace(ctx, core.PendingSubscriptionEndGrace{
		CreatorID:    "c1",
		TwitchUserID: "u1",
		DueAt:        now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("UpsertSubscriptionEndGrace failed: %v", err)
	}
	// Simulate a due entry whose job blob was lost (e.g. partial delete).
	if err := s.rdb.ZAdd(ctx, keySubscriptionEndGraceDue(), goredis.Z{
		Score:  float64(now.Add(-time.Hour).Unix()),
		Member: "c1:ghost",
	}).Err(); err != nil {
		t.Fatalf("seed stale due entry: %v", err)
	}

	due, err := s.ListDueSubscriptionEndGrace(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDueSubscriptionEndGrace failed: %v", err)
	}
	if len(due) != 1 || due[0].ID != job.ID {
		t.Fatalf("due = %+v, want only job %s", due, job.ID)
	}
	if ok, err := s.rdb.ZScore(ctx, keySubscriptionEndGraceDue(), "c1:ghost").Result(); err == nil {
		t.Fatalf("stale due entry still present with score %v, want pruned", ok)
	}
}

func TestListPendingMemberCleanupJobsSkipsMissingBlobs(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	created, err := s.CreateMemberCleanupJob(ctx, core.MemberCleanupJob{
		Kind:            core.MemberCleanupKindGroupUnregistration,
		OwnerTelegramID: 1,
		Targets:         []core.MemberCleanupTarget{{ChatID: 100, TelegramUserID: 10, MaxAttempts: 3}},
	})
	if err != nil {
		t.Fatalf("CreateMemberCleanupJob failed: %v", err)
	}
	// Simulate a pending-set entry whose job blob expired or was lost.
	if err := s.rdb.SAdd(ctx, keyPendingMemberCleanupJobs(), "ghost").Err(); err != nil {
		t.Fatalf("seed ghost pending id: %v", err)
	}

	jobs, err := s.ListPendingMemberCleanupJobs(ctx)
	if err != nil {
		t.Fatalf("ListPendingMemberCleanupJobs failed: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != created.ID {
		t.Fatalf("jobs = %+v, want only job %s", jobs, created.ID)
	}
}
