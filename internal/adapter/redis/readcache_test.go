package redis

import (
	"context"
	"sync"
	"testing"

	"imsub/internal/core"
)

type countingObserver struct {
	mu     sync.Mutex
	counts map[string]int
}

func (o *countingObserver) ObserveRedisCommand(_ context.Context, _, command, _ string, count int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.counts == nil {
		o.counts = make(map[string]int)
	}
	o.counts[command] += count
}

func (o *countingObserver) total() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	sum := 0
	for _, n := range o.counts {
		sum += n
	}
	return sum
}

func TestManagedGroupReadsServedFromCacheUntilWrite(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	for _, group := range []core.ManagedGroup{
		{ChatID: 100, CreatorID: "c1", GroupName: "One"},
		{ChatID: 200, CreatorID: "c2", GroupName: "Two"},
	} {
		if err := s.UpsertManagedGroup(ctx, group); err != nil {
			t.Fatalf("UpsertManagedGroup(%d) failed: %v", group.ChatID, err)
		}
	}

	warm, err := s.ListManagedGroups(ctx)
	if err != nil {
		t.Fatalf("ListManagedGroups (warm-up) failed: %v", err)
	}
	if len(warm) != 2 {
		t.Fatalf("warm-up groups = %d, want 2", len(warm))
	}

	observer := &countingObserver{}
	s.SetCommandObserver(observer)

	cached, err := s.ListManagedGroups(ctx)
	if err != nil {
		t.Fatalf("ListManagedGroups (cached) failed: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("cached groups = %d, want 2", len(cached))
	}
	byCreator, err := s.ListManagedGroupsByCreator(ctx, "c1")
	if err != nil {
		t.Fatalf("ListManagedGroupsByCreator (cached) failed: %v", err)
	}
	if len(byCreator) != 1 || byCreator[0].ChatID != 100 {
		t.Fatalf("cached groups by creator = %+v, want chat 100", byCreator)
	}
	if group, found, err := s.ManagedGroupByChatID(ctx, 200); err != nil || !found || group.GroupName != "Two" {
		t.Fatalf("cached ManagedGroupByChatID = (%+v, %t, %v), want group Two", group, found, err)
	}
	if _, found, err := s.ManagedGroupByChatID(ctx, 999); err != nil || found {
		t.Fatalf("cached negative lookup = (found=%t, %v), want miss without error", found, err)
	}
	if got := observer.total(); got != 0 {
		t.Fatalf("redis commands during cached reads = %d (%v), want 0", got, observer.counts)
	}

	// A write must invalidate so the next read observes the new group.
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 300, CreatorID: "c1", GroupName: "Three"}); err != nil {
		t.Fatalf("UpsertManagedGroup(300) failed: %v", err)
	}
	after, err := s.ListManagedGroups(ctx)
	if err != nil {
		t.Fatalf("ListManagedGroups (after write) failed: %v", err)
	}
	if len(after) != 3 {
		t.Fatalf("groups after write = %d, want 3", len(after))
	}
}

func TestCreatorReadsServedFromCacheUntilWrite(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertCreator(ctx, core.Creator{ID: "c1", TwitchLogin: "one", OwnerTelegramID: 1}); err != nil {
		t.Fatalf("UpsertCreator(c1) failed: %v", err)
	}
	if _, err := s.ListCreators(ctx); err != nil {
		t.Fatalf("ListCreators (warm-up) failed: %v", err)
	}

	observer := &countingObserver{}
	s.SetCommandObserver(observer)

	if creator, found, err := s.Creator(ctx, "c1"); err != nil || !found || creator.TwitchLogin != "one" {
		t.Fatalf("cached Creator = (%+v, %t, %v), want creator one", creator, found, err)
	}
	if _, found, err := s.Creator(ctx, "ghost"); err != nil || found {
		t.Fatalf("cached negative creator lookup = (found=%t, %v), want miss without error", found, err)
	}
	if got := observer.total(); got != 0 {
		t.Fatalf("redis commands during cached reads = %d (%v), want 0", got, observer.counts)
	}

	// Token updates must invalidate so reconcile flows never see stale tokens.
	if err := s.UpdateCreatorTokens(ctx, "c1", "fresh-access", "fresh-refresh", nil); err != nil {
		t.Fatalf("UpdateCreatorTokens failed: %v", err)
	}
	creator, found, err := s.Creator(ctx, "c1")
	if err != nil || !found {
		t.Fatalf("Creator after token update = (found=%t, %v), want hit", found, err)
	}
	if creator.AccessToken != "fresh-access" {
		t.Fatalf("creator access token = %q, want fresh-access", creator.AccessToken)
	}
}
