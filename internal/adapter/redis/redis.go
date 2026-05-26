package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"imsub/internal/events"

	"github.com/redis/go-redis/v9"
)

const schemaVersionCurrent = 4

// Store implements [Store] backed by Redis.
type Store struct {
	rdb             *redis.Client
	logger          *slog.Logger
	commandObserver CommandObserver
}

// CommandObserver receives low-cardinality Redis command telemetry.
type CommandObserver interface {
	ObserveRedisCommand(ctx context.Context, job, command, result string, count int)
}

// NewStore connects to Redis and returns a ready [Store].
func NewStore(redisURL string, logger *slog.Logger) (*Store, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("redis parse url: %w", err)
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	store := &Store{rdb: client, logger: logger}
	client.AddHook(redisCommandMetricsHook{store: store})
	client.AddHook(backupTrackingHook{store: store})
	return store, nil
}

// SetCommandObserver configures optional Redis command telemetry.
func (s *Store) SetCommandObserver(observer CommandObserver) {
	if s == nil {
		return
	}
	s.commandObserver = observer
}

func (s *Store) log() *slog.Logger {
	if s == nil || s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// Ping verifies the Redis connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping check: %w", err)
	}
	return nil
}

// Close terminates the Redis connection.
func (s *Store) Close() error {
	if err := s.rdb.Close(); err != nil {
		return fmt.Errorf("redis close: %w", err)
	}
	return nil
}

// EnsureSchema initializes the Redis schema version if absent.
func (s *Store) EnsureSchema(ctx context.Context) error {
	val, err := s.rdb.Get(ctx, keySchemaVersion()).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if setErr := s.rdb.Set(ctx, keySchemaVersion(), strconv.Itoa(schemaVersionCurrent), 0).Err(); setErr != nil {
				return fmt.Errorf("redis set schema version (init): %w", setErr)
			}
			return nil
		}
		return fmt.Errorf("redis get schema version: %w", err)
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("parse schema version: %w", err)
	}
	if v == schemaVersionCurrent {
		return nil
	}
	if setErr := s.rdb.Set(ctx, keySchemaVersion(), strconv.Itoa(schemaVersionCurrent), 0).Err(); setErr != nil {
		return fmt.Errorf("redis set schema version (upgrade): %w", setErr)
	}
	return nil
}

// --- Redis key helpers ---

func keyOAuthState(state string) string       { return "imsub:oauth:" + state }
func keyEventMessage(messageID string) string { return "imsub:eventmsg:" + messageID }
func keyUserIdentity(telegramUserID int64) string {
	return "imsub:user:" + strconv.FormatInt(telegramUserID, 10)
}
func keyUserTrackedGroups(telegramUserID int64) string {
	return "imsub:user:groups:tracked:" + strconv.FormatInt(telegramUserID, 10)
}
func keyUsersSet() string                            { return "imsub:users" }
func keyCreatorSubscribers(creatorID string) string  { return "imsub:creator:subscribers:" + creatorID }
func keyCreatorBlockedUsers(creatorID string) string { return "imsub:creator:blocked:" + creatorID }
func keyTwitchToTelegram(twitchUserID string) string { return "imsub:twitch_to_tg:" + twitchUserID }
func keyCreator(creatorID string) string             { return "imsub:creator:" + creatorID }
func keyCreatorsSet() string                         { return "imsub:creators" }
func keyCreatorByOwner(ownerTelegramID int64) string {
	return "imsub:creator:by_owner:" + strconv.FormatInt(ownerTelegramID, 10)
}
func keySchemaVersion() string { return "imsub:schema_version" }
func keyManagedGroup(chatID int64) string {
	return "imsub:group:" + strconv.FormatInt(chatID, 10)
}
func keyManagedGroupsSet() string { return "imsub:groups" }
func keyManagedGroupsByCreator(creatorID string) string {
	return "imsub:groups:by_creator:" + creatorID
}
func keyTelegramActiveUsers() string { return "imsub:metrics:telegram_active_users" }
func keyTrackedGroupMembers(chatID int64) string {
	return "imsub:group:tracked:" + strconv.FormatInt(chatID, 10)
}
func keyIntegrityTrackedReverseIndexProcessed(runID string) string {
	return "imsub:integrity:tracked_reverse_index:processed:" + runID
}
func keyUntrackedGroupMembers(chatID int64) string {
	return "imsub:group:untracked:" + strconv.FormatInt(chatID, 10)
}
func keyTrackedGroupMemberMeta(chatID, telegramUserID int64) string {
	return "imsub:group:member:" + strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(telegramUserID, 10)
}
func keyManagedMemberTags(chatID int64) string {
	return "imsub:group:member_tags:" + strconv.FormatInt(chatID, 10)
}
func keyManagedMemberTag(chatID, telegramUserID int64) string {
	return "imsub:group:member_tag:" + strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(telegramUserID, 10)
}
func keyMemberCleanupJob(jobID string) string { return "imsub:cleanup_job:" + jobID }
func keyPendingMemberCleanupJobs() string     { return "imsub:cleanup_jobs:pending" }
func keyMemberCleanupJobSeq() string          { return "imsub:cleanup_jobs:seq" }
func keyMemberCleanupJobLock(jobID string) string {
	return "imsub:cleanup_job_lock:" + jobID
}
func keySubscriptionEndGraceJob(jobID string) string { return "imsub:sub_end_grace:" + jobID }
func keySubscriptionEndGraceDue() string             { return "imsub:sub_end_grace:due" }
func keySubscriptionEndGraceLock(jobID string) string {
	return "imsub:sub_end_grace_lock:" + jobID
}
func keyConsentRecord(telegramUserID int64) string {
	return "imsub:privacy:consent:" + strconv.FormatInt(telegramUserID, 10)
}
func keyPrivacyOAuthStates(telegramUserID int64) string {
	return "imsub:privacy:oauth_states:" + strconv.FormatInt(telegramUserID, 10)
}
func keyPrivacyReceipts(telegramUserID int64) string {
	return "imsub:privacy:receipts:" + strconv.FormatInt(telegramUserID, 10)
}
func keyPrivacyReceipt(telegramUserID int64, receiptID string) string {
	return "imsub:privacy:receipt:" + strconv.FormatInt(telegramUserID, 10) + ":" + receiptID
}

func keyBackupDirty() string       { return "imsub:backup:dirty" }
func keyBackupDeleted() string     { return "imsub:backup:deleted" }
func keyBackupBaseFullKey() string { return "imsub:backup:base_full_key" }
func keyBackupBaseFullAt() string  { return "imsub:backup:base_full_at" }

func isTemporaryDumpKey(key string) bool {
	return strings.Contains(key, ":tmp:") &&
		(strings.HasPrefix(key, "imsub:creator:subscribers:") ||
			strings.HasPrefix(key, "imsub:creator:blocked:"))
}

type backupSkipTrackingKey struct{}

func skipBackupTracking(ctx context.Context) context.Context {
	return context.WithValue(ctx, backupSkipTrackingKey{}, true)
}

func backupTrackingSkipped(ctx context.Context) bool {
	v, _ := ctx.Value(backupSkipTrackingKey{}).(bool)
	return v
}

type redisCommandMetricsHook struct {
	store *Store
}

var _ redis.Hook = redisCommandMetricsHook{}

func (h redisCommandMetricsHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h redisCommandMetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		h.observe(ctx, redisCommandName(cmd), redisCommandResult(err), 1)
		return err
	}
}

func (h redisCommandMetricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			h.observe(ctx, redisCommandName(cmd), redisCommandResult(cmd.Err()), 1)
		}
		return err
	}
}

func (h redisCommandMetricsHook) observe(ctx context.Context, command, result string, count int) {
	if h.store == nil || h.store.commandObserver == nil || count == 0 {
		return
	}
	job := "foreground"
	if bg, ok := events.BackgroundJobFromContext(ctx); ok && strings.TrimSpace(bg.Job) != "" {
		job = bg.Job
	}
	h.store.commandObserver.ObserveRedisCommand(ctx, job, command, result, count)
}

func redisCommandName(cmd redis.Cmder) string {
	if cmd == nil {
		return "unknown"
	}
	name := strings.ToLower(strings.TrimSpace(cmd.Name()))
	if name == "" {
		return "unknown"
	}
	return name
}

func redisCommandResult(err error) string {
	if err == nil || errors.Is(err, redis.Nil) {
		return "ok"
	}
	return "error"
}

type backupTrackingHook struct {
	store *Store
}

var _ redis.Hook = backupTrackingHook{}

func (h backupTrackingHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h backupTrackingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && !backupTrackingSkipped(ctx) {
			h.trackCommand(ctx, cmd)
		}
		return err
	}
}

func (h backupTrackingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		if !backupTrackingSkipped(ctx) {
			h.trackCommands(ctx, cmds)
		}
		return err
	}
}

func (h backupTrackingHook) trackCommand(ctx context.Context, cmd redis.Cmder) {
	if h.store == nil || h.store.rdb == nil {
		return
	}
	dirty, deleted := backupTouchedKeys(cmd)
	if len(dirty) == 0 && len(deleted) == 0 {
		return
	}
	ctx = skipBackupTracking(ctx)
	if len(dirty) > 0 {
		if err := h.store.rdb.SAdd(ctx, keyBackupDirty(), stringSliceToAny(dirty)...).Err(); err != nil {
			h.store.log().Warn("backup dirty tracking failed", "error", err)
		}
	}
	if len(deleted) > 0 {
		if err := h.store.rdb.SAdd(ctx, keyBackupDeleted(), stringSliceToAny(deleted)...).Err(); err != nil {
			h.store.log().Warn("backup deleted tracking failed", "error", err)
		}
	}
}

func (h backupTrackingHook) trackCommands(ctx context.Context, cmds []redis.Cmder) {
	if h.store == nil || h.store.rdb == nil || len(cmds) == 0 {
		return
	}
	dirtySet := make(map[string]struct{})
	deletedSet := make(map[string]struct{})
	for _, cmd := range cmds {
		if cmd.Err() != nil {
			continue
		}
		dirty, deleted := backupTouchedKeys(cmd)
		for _, key := range dirty {
			dirtySet[key] = struct{}{}
		}
		for _, key := range deleted {
			deletedSet[key] = struct{}{}
		}
	}
	if len(dirtySet) == 0 && len(deletedSet) == 0 {
		return
	}
	ctx = skipBackupTracking(ctx)
	if len(dirtySet) > 0 {
		if err := h.store.rdb.SAdd(ctx, keyBackupDirty(), stringSliceToAny(mapKeys(dirtySet))...).Err(); err != nil {
			h.store.log().Warn("backup dirty tracking failed", "error", err)
		}
	}
	if len(deletedSet) > 0 {
		if err := h.store.rdb.SAdd(ctx, keyBackupDeleted(), stringSliceToAny(mapKeys(deletedSet))...).Err(); err != nil {
			h.store.log().Warn("backup deleted tracking failed", "error", err)
		}
	}
}

func backupTouchedKeys(cmd redis.Cmder) (dirty []string, deleted []string) {
	args := cmd.Args()
	if len(args) < 2 {
		return nil, nil
	}
	name, _ := args[0].(string)
	name = strings.ToLower(name)
	keyAt := func(i int) (string, bool) {
		if i >= len(args) {
			return "", false
		}
		key, ok := args[i].(string)
		if !ok || !isBackupExportedKey(key) {
			return "", false
		}
		return key, true
	}
	appendKey := func(dst []string, i int) []string {
		if key, ok := keyAt(i); ok {
			dst = append(dst, key)
		}
		return dst
	}
	switch name {
	case "del", "unlink":
		for i := 1; i < len(args); i++ {
			deleted = appendKey(deleted, i)
		}
	case "getdel":
		deleted = appendKey(deleted, 1)
	case "rename", "renamenx":
		deleted = appendKey(deleted, 1)
		dirty = appendKey(dirty, 2)
	case "set", "setex", "psetex", "setnx", "hset", "hdel", "hmset", "sadd", "srem", "zadd", "zrem", "expire", "pexpire", "expireat", "pexpireat", "persist", "restore", "restore-asking":
		dirty = appendKey(dirty, 1)
	}
	return dirty, deleted
}

func stringSliceToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

// --- Lua scripts ---

var linkViewerIdentityScript = redis.NewScript(`
local existing = redis.call("HGET", KEYS[1], "twitch_user_id")
if existing and existing ~= "" and existing ~= ARGV[2] then
  return redis.error_reply("DIFFERENT_TWITCH")
end
redis.call("HSET", KEYS[1],
  "twitch_user_id", ARGV[2],
  "twitch_login", ARGV[3],
  "twitch_display_name", ARGV[4],
  "language", ARGV[5],
  "verified_at", ARGV[6]
)
redis.call("SET", KEYS[2], ARGV[1])
redis.call("SADD", KEYS[3], ARGV[1])
return 1
`)
