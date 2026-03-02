package redis

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

var _ core.Store = (*Store)(nil)

const schemaVersionCurrent = 1

// Store implements [Store] backed by Redis.
type Store struct {
	rdb    *redis.Client
	logger *slog.Logger
}

// NewStore connects to Redis and returns a ready [Store].
func NewStore(redisURL string, logger *slog.Logger) (*Store, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
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
	return s.rdb.Ping(ctx).Err()
}

// Close terminates the Redis connection.
func (s *Store) Close() error {
	return s.rdb.Close()
}

// EnsureSchema initializes the Redis schema version if absent.
func (s *Store) EnsureSchema(ctx context.Context) error {
	val, err := s.rdb.Get(ctx, keySchemaVersion()).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return s.rdb.Set(ctx, keySchemaVersion(), strconv.Itoa(schemaVersionCurrent), 0).Err()
		}
		return err
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return err
	}
	if v == schemaVersionCurrent {
		return nil
	}
	return s.rdb.Set(ctx, keySchemaVersion(), strconv.Itoa(schemaVersionCurrent), 0).Err()
}

// --- Redis key helpers ---

func keyOAuthState(state string) string       { return "imsub:oauth:" + state }
func keyEventMessage(messageID string) string { return "imsub:eventmsg:" + messageID }
func keyUserIdentity(telegramUserID int64) string {
	return "imsub:user:" + strconv.FormatInt(telegramUserID, 10)
}
func keyUserCreators(telegramUserID int64) string {
	return "imsub:user:creators:" + strconv.FormatInt(telegramUserID, 10)
}
func keyUsersSet() string                            { return "imsub:users" }
func keyCreatorMembers(creatorID string) string      { return "imsub:creator:members:" + creatorID }
func keyCreatorSubscribers(creatorID string) string  { return "imsub:creator:subscribers:" + creatorID }
func keyTwitchToTelegram(twitchUserID string) string { return "imsub:twitch_to_tg:" + twitchUserID }
func keyCreator(creatorID string) string             { return "imsub:creator:" + creatorID }
func keyCreatorsSet() string                         { return "imsub:creators" }
func keyCreatorByOwner(ownerTelegramID int64) string {
	return "imsub:creator:by_owner:" + strconv.FormatInt(ownerTelegramID, 10)
}
func keyActiveCreatorsSet() string { return "imsub:creators:active" }
func keySchemaVersion() string     { return "imsub:schema_version" }

// --- Lua scripts ---

var linkViewerIdentityScript = redis.NewScript(`
local existing = redis.call("HGET", KEYS[1], "twitch_user_id")
if existing and existing ~= "" and existing ~= ARGV[2] then
  return redis.error_reply("DIFFERENT_TWITCH")
end
redis.call("HSET", KEYS[1],
  "twitch_user_id", ARGV[2],
  "twitch_login", ARGV[3],
  "language", ARGV[4],
  "verified_at", ARGV[5]
)
redis.call("SET", KEYS[2], ARGV[1])
redis.call("SADD", KEYS[3], ARGV[1])
return 1
`)

var linkViewerCreatorScript = redis.NewScript(`
local existing = redis.call("HGET", KEYS[1], "twitch_user_id")
if existing and existing ~= "" and existing ~= ARGV[2] then
  return redis.error_reply("DIFFERENT_TWITCH")
end
redis.call("HSET", KEYS[1],
  "twitch_user_id", ARGV[2],
  "twitch_login", ARGV[3],
  "language", ARGV[4],
  "verified_at", ARGV[5]
)
redis.call("SET", KEYS[2], ARGV[1])
redis.call("SADD", KEYS[3], ARGV[1])
redis.call("SADD", KEYS[4], ARGV[1])
redis.call("SADD", KEYS[5], ARGV[6])
return 1
`)
