package redis

import (
	"strings"
	"sync"
	"time"

	"imsub/internal/core"
)

// readCacheTTL bounds staleness from writers outside this process (e.g.
// imsub-admin restoring a backup into a live instance). In-process writes
// invalidate immediately via the command hook, so the TTL is a backstop, not
// the consistency mechanism.
const readCacheTTL = 5 * time.Minute

// readCache holds in-process copies of the managed-group and creator lists.
// Periodic jobs re-read both every few minutes; serving them from memory
// avoids one SMEMBERS plus one HGETALL per entity on every read. Correct only
// while a single instance owns all writes (same assumption as backup
// dirty-tracking dedup).
type readCache struct {
	mu sync.Mutex

	groupsGen  uint64
	groups     []core.ManagedGroup
	groupsAt   time.Time
	groupsSet  bool
	creatorGen uint64
	creators   []core.Creator
	creatorsAt time.Time
	creatorSet bool

	now func() time.Time
}

func (c *readCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// cachedGroups returns the cached group list and the generation to use when
// storing a fresh load; ok is false on miss or expiry.
func (c *readCache) cachedGroups() (groups []core.ManagedGroup, gen uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groupsSet && c.clock().Sub(c.groupsAt) < readCacheTTL {
		return c.groups, c.groupsGen, true
	}
	return nil, c.groupsGen, false
}

// storeGroups records a freshly loaded group list unless an invalidation
// happened since the load started (generation mismatch).
func (c *readCache) storeGroups(gen uint64, groups []core.ManagedGroup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groupsGen != gen {
		return
	}
	c.groups = groups
	c.groupsAt = c.clock()
	c.groupsSet = true
}

func (c *readCache) invalidateGroups() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groupsGen++
	c.groups = nil
	c.groupsSet = false
}

func (c *readCache) cachedCreators() (creators []core.Creator, gen uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creatorSet && c.clock().Sub(c.creatorsAt) < readCacheTTL {
		return c.creators, c.creatorGen, true
	}
	return nil, c.creatorGen, false
}

func (c *readCache) storeCreators(gen uint64, creators []core.Creator) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creatorGen != gen {
		return
	}
	c.creators = creators
	c.creatorsAt = c.clock()
	c.creatorSet = true
}

func (c *readCache) invalidateCreators() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creatorGen++
	c.creators = nil
	c.creatorSet = false
}

// invalidateReadCacheKeys drops cached lists whose source keys were mutated.
// Called from the command hook for every successful write, including ones
// with backup tracking skipped (e.g. RESTORE during a backup load).
func (s *Store) invalidateReadCacheKeys(keys []string) {
	for _, key := range keys {
		if key == keyManagedGroupsSet() || isManagedGroupHashKey(key) {
			s.reads.invalidateGroups()
		}
		if key == keyCreatorsSet() || isCreatorHashKey(key) {
			s.reads.invalidateCreators()
		}
	}
}

// isManagedGroupHashKey reports whether key is a managed-group hash
// ("imsub:group:<chatID>"), excluding the sibling namespaces such as
// "imsub:group:tracked:<chatID>" whose remainder contains further segments.
func isManagedGroupHashKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "imsub:group:")
	return ok && rest != "" && !strings.Contains(rest, ":")
}

// isCreatorHashKey reports whether key is a creator hash
// ("imsub:creator:<id>"), excluding "imsub:creator:subscribers:<id>" and
// friends whose remainder contains further segments.
func isCreatorHashKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "imsub:creator:")
	return ok && rest != "" && !strings.Contains(rest, ":")
}
