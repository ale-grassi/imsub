package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const schemaVersionCurrent = 4

// Store implements [Store] backed by Redis.
type Store struct {
	rdb    *redis.Client
	logger *slog.Logger
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
	return &Store{rdb: client, logger: logger}, nil
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
