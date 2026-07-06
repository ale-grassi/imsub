package redis

import (
	"testing"
	"time"
)

func TestClaimMemberCleanupJobContended(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	claimed, err := s.ClaimMemberCleanupJob(ctx, "job-1", time.Minute)
	if err != nil {
		t.Fatalf("first ClaimMemberCleanupJob failed: %v", err)
	}
	if !claimed {
		t.Fatal("first ClaimMemberCleanupJob = false, want true")
	}

	claimed, err = s.ClaimMemberCleanupJob(ctx, "job-1", time.Minute)
	if err != nil {
		t.Fatalf("second ClaimMemberCleanupJob failed: %v", err)
	}
	if claimed {
		t.Fatal("second ClaimMemberCleanupJob = true, want false")
	}
}

func TestClaimSubscriptionEndGraceContended(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	claimed, err := s.ClaimSubscriptionEndGrace(ctx, "creator:viewer", time.Minute)
	if err != nil {
		t.Fatalf("first ClaimSubscriptionEndGrace failed: %v", err)
	}
	if !claimed {
		t.Fatal("first ClaimSubscriptionEndGrace = false, want true")
	}

	claimed, err = s.ClaimSubscriptionEndGrace(ctx, "creator:viewer", time.Minute)
	if err != nil {
		t.Fatalf("second ClaimSubscriptionEndGrace failed: %v", err)
	}
	if claimed {
		t.Fatal("second ClaimSubscriptionEndGrace = true, want false")
	}
}
