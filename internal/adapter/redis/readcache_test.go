package redis

import (
	"context"
	"sync"
	"testing"

	"imsub/internal/core"
	"imsub/internal/events"
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

type recordingSink struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *recordingSink) Emit(_ context.Context, evt events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
}

func (r *recordingSink) named(name string) []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]events.Event, 0, len(r.events))
	for _, evt := range r.events {
		if evt.Name == name {
			out = append(out, evt)
		}
	}
	return out
}

func TestReadCacheEmitsHitAndMissEvents(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 100, CreatorID: "c1", GroupName: "One"}); err != nil {
		t.Fatalf("UpsertManagedGroup failed: %v", err)
	}
	sink := &recordingSink{}
	s.SetEventSink(sink)
	if _, err := s.ListManagedGroups(ctx); err != nil {
		t.Fatalf("ListManagedGroups (miss) failed: %v", err)
	}
	if _, err := s.ListManagedGroups(ctx); err != nil {
		t.Fatalf("ListManagedGroups (hit) failed: %v", err)
	}

	var hits, misses int
	for _, evt := range sink.named(events.NameRedisReadCache) {
		if evt.Fields["cache"] != "groups" {
			continue
		}
		switch evt.Outcome {
		case "hit":
			hits++
		case "miss":
			misses++
		}
	}
	if hits != 1 || misses != 1 {
		t.Fatalf("groups cache events = %d hits / %d misses, want 1/1", hits, misses)
	}
}

func TestDumpJournalReplayEmitsEvent(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	sink := &recordingSink{}
	s.SetEventSink(sink)

	tmpKey := s.NewSubscriberDumpKey("c1")
	if err := s.AddToSubscriberDump(ctx, tmpKey, []string{"u-snap"}); err != nil {
		t.Fatalf("AddToSubscriberDump failed: %v", err)
	}
	if err := s.AddCreatorSubscriber(ctx, "c1", "u-new"); err != nil {
		t.Fatalf("mid-dump AddCreatorSubscriber failed: %v", err)
	}
	if err := s.FinalizeSubscriberDump(ctx, "c1", tmpKey, true); err != nil {
		t.Fatalf("FinalizeSubscriberDump failed: %v", err)
	}

	replays := sink.named(events.NameDumpJournalReplay)
	if len(replays) != 1 {
		t.Fatalf("dump journal replay events = %d, want 1", len(replays))
	}
	evt := replays[0]
	if evt.Outcome != "applied" || evt.Fields["set"] != "subscribers" || evt.Count != 1 {
		t.Fatalf("replay event = outcome=%q set=%q count=%d, want applied/subscribers/1", evt.Outcome, evt.Fields["set"], evt.Count)
	}
}
