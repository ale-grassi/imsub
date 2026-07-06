package redis

import (
	"context"
	"strings"
	"sync"

	"imsub/internal/events"
)

// dumpJournal captures individual set mutations (webhook-driven subscriber or
// blocklist updates) that land while a full dump of the same set is being
// rebuilt from the Twitch API. Finalizing a dump replaces the destination set
// via RENAME, which would silently discard any event that arrived after the
// dump snapshot started; replaying the journal after the rename preserves
// them. In-process only, so it shares the single-instance assumption of the
// rest of this package; a crash loses at most what today's behavior already
// loses (the event is re-derived on the next reconcile cycle).
type dumpJournal struct {
	mu     sync.Mutex
	active map[string]*dumpLog // destination key -> in-flight dump log
	byTmp  map[string]string   // tmp dump key -> destination key
}

type dumpLog struct {
	entries []dumpEntry
}

type dumpEntry struct {
	member string
	add    bool
}

// begin starts journaling events against destKey until the dump identified by
// tmpKey is finalized or discarded. A newer dump for the same destination
// replaces the previous log.
func (j *dumpJournal) begin(destKey, tmpKey string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.active == nil {
		j.active = make(map[string]*dumpLog)
		j.byTmp = make(map[string]string)
	}
	for tmp, dest := range j.byTmp {
		if dest == destKey {
			delete(j.byTmp, tmp)
		}
	}
	j.active[destKey] = &dumpLog{}
	j.byTmp[tmpKey] = destKey
}

// record notes a live mutation of destKey; a no-op unless a dump is in flight.
func (j *dumpJournal) record(destKey, member string, add bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	log := j.active[destKey]
	if log == nil {
		return
	}
	log.entries = append(log.entries, dumpEntry{member: member, add: add})
}

// take stops journaling for tmpKey's dump and returns the destination key with
// the net effect per member (last operation wins), in first-seen order.
func (j *dumpJournal) take(tmpKey string) (destKey string, adds, removes []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	destKey, ok := j.byTmp[tmpKey]
	if !ok {
		return "", nil, nil
	}
	delete(j.byTmp, tmpKey)
	log := j.active[destKey]
	delete(j.active, destKey)
	if log == nil {
		return destKey, nil, nil
	}

	final := make(map[string]bool, len(log.entries))
	order := make([]string, 0, len(log.entries))
	for _, entry := range log.entries {
		if _, seen := final[entry.member]; !seen {
			order = append(order, entry.member)
		}
		final[entry.member] = entry.add
	}
	for _, member := range order {
		if final[member] {
			adds = append(adds, member)
		} else {
			removes = append(removes, member)
		}
	}
	return destKey, adds, removes
}

// discard drops the journal for tmpKey's dump without replaying it.
func (j *dumpJournal) discard(tmpKey string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	destKey, ok := j.byTmp[tmpKey]
	if !ok {
		return
	}
	delete(j.byTmp, tmpKey)
	delete(j.active, destKey)
}

// replayDumpJournal applies journaled mutations that raced a finished dump.
// Best-effort: on failure the next reconcile cycle restores the same state,
// which is no worse than the pre-journal behavior.
func (s *Store) replayDumpJournal(ctx context.Context, tmpKey string) {
	destKey, adds, removes := s.dumps.take(tmpKey)
	if destKey == "" || (len(adds) == 0 && len(removes) == 0) {
		return
	}
	pipe := s.rdb.Pipeline()
	if len(adds) > 0 {
		pipe.SAdd(ctx, destKey, stringSliceToAny(adds)...)
	}
	if len(removes) > 0 {
		pipe.SRem(ctx, destKey, stringSliceToAny(removes)...)
	}
	outcome := "applied"
	if _, err := pipe.Exec(ctx); err != nil {
		outcome = "failed"
		s.log().Warn("replay dump journal failed", "dest_key", destKey, "adds", len(adds), "removes", len(removes), "error", err)
	}
	if s.eventSink != nil {
		set := "blocklist"
		if strings.HasPrefix(destKey, "imsub:creator:subscribers:") {
			set = "subscribers"
		}
		s.eventSink.Emit(ctx, events.Event{
			Name:    events.NameDumpJournalReplay,
			Outcome: outcome,
			Fields:  map[string]string{"set": set},
			Count:   len(adds) + len(removes),
		})
	}
}
