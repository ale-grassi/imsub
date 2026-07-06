// Package redis implements Redis-backed persistence adapters.
//
// # Pipelining policy
//
// Multi-key writes default to non-transactional [redis.Pipeliner] batches:
// on Upstash every command is billed, and MULTI/EXEC adds two commands per
// batch. A plain pipeline may apply a prefix of its commands if the
// connection drops mid-batch, so this is only done where partial state is
// tolerable: readers filter dangling index entries, the integrity-audit job
// repairs the tracked-group reverse index, callers retry on error, and TTLs
// reap leftovers. TxPipeline is reserved for writes whose partial application
// would be silently harmful and has no repair path (privacy deletion, backup
// bookkeeping, creator/group registration).
package redis
