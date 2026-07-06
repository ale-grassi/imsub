package redis

import (
	"slices"
	"testing"
)

func TestFinalizeSubscriberDumpReplaysEventsRacingTheDump(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	// u-before unsubscribed before the dump started; the snapshot omits them.
	if err := s.AddCreatorSubscriber(ctx, "c1", "u-before"); err != nil {
		t.Fatalf("seed pre-dump subscriber: %v", err)
	}

	tmpKey := s.NewSubscriberDumpKey("c1")
	if err := s.AddToSubscriberDump(ctx, tmpKey, []string{"u-snap", "u-lapsed"}); err != nil {
		t.Fatalf("AddToSubscriberDump failed: %v", err)
	}

	// Events arriving mid-dump: a new sub and an unsub of a snapshotted user.
	if err := s.AddCreatorSubscriber(ctx, "c1", "u-new"); err != nil {
		t.Fatalf("mid-dump AddCreatorSubscriber failed: %v", err)
	}
	if err := s.RemoveCreatorSubscriber(ctx, "c1", "u-lapsed"); err != nil {
		t.Fatalf("mid-dump RemoveCreatorSubscriber failed: %v", err)
	}

	if err := s.FinalizeSubscriberDump(ctx, "c1", tmpKey, true); err != nil {
		t.Fatalf("FinalizeSubscriberDump failed: %v", err)
	}

	members, err := s.rdb.SMembers(ctx, keyCreatorSubscribers("c1")).Result()
	if err != nil {
		t.Fatalf("read subscriber set: %v", err)
	}
	slices.Sort(members)
	want := []string{"u-new", "u-snap"}
	if !slices.Equal(members, want) {
		t.Fatalf("subscribers after finalize = %v, want %v", members, want)
	}
}

func TestFinalizeEmptySubscriberDumpReplaysMidDumpSubscribe(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	tmpKey := s.NewSubscriberDumpKey("c1")
	if err := s.AddCreatorSubscriber(ctx, "c1", "u-new"); err != nil {
		t.Fatalf("mid-dump AddCreatorSubscriber failed: %v", err)
	}
	if err := s.FinalizeSubscriberDump(ctx, "c1", tmpKey, false); err != nil {
		t.Fatalf("FinalizeSubscriberDump(empty) failed: %v", err)
	}

	members, err := s.rdb.SMembers(ctx, keyCreatorSubscribers("c1")).Result()
	if err != nil {
		t.Fatalf("read subscriber set: %v", err)
	}
	if !slices.Equal(members, []string{"u-new"}) {
		t.Fatalf("subscribers after empty finalize = %v, want [u-new]", members)
	}
}

func TestCleanupSubscriberDumpDiscardsJournal(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	tmpKey := s.NewSubscriberDumpKey("c1")
	if err := s.AddCreatorSubscriber(ctx, "c1", "u-live"); err != nil {
		t.Fatalf("mid-dump AddCreatorSubscriber failed: %v", err)
	}
	s.CleanupSubscriberDump(ctx, tmpKey)

	if destKey, adds, removes := s.dumps.take(tmpKey); destKey != "" || adds != nil || removes != nil {
		t.Fatalf("journal after discard = (%q, %v, %v), want empty", destKey, adds, removes)
	}
	// The live write itself must survive; only the journal is dropped.
	if ok, err := s.rdb.SIsMember(ctx, keyCreatorSubscribers("c1"), "u-live").Result(); err != nil || !ok {
		t.Fatalf("live subscriber after discard = (%t, %v), want present", ok, err)
	}
}
