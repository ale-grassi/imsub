//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"imsub/internal/adapter/redis"

	redislib "github.com/redis/go-redis/v9"
)

func TestRedisBackupRoundTripWithRealRedis(t *testing.T) {
	store, client := newRealRedisBackupStore(t)
	ctx := t.Context()

	if err := client.Set(ctx, "imsub:string", "hello", 0).Err(); err != nil {
		t.Fatalf("seed string key: %v", err)
	}
	if err := client.HSet(ctx, "imsub:hash", map[string]any{"a": "1", "b": "2"}).Err(); err != nil {
		t.Fatalf("seed hash key: %v", err)
	}
	if err := client.SAdd(ctx, "imsub:set", "u1", "u2").Err(); err != nil {
		t.Fatalf("seed set key: %v", err)
	}
	if err := client.ZAdd(ctx, "imsub:zset", redislib.Z{Score: 1, Member: "m1"}, redislib.Z{Score: 2, Member: "m2"}).Err(); err != nil {
		t.Fatalf("seed zset key: %v", err)
	}
	if err := client.Set(ctx, "imsub:ttl", "expiring", 2*time.Minute).Err(); err != nil {
		t.Fatalf("seed ttl key: %v", err)
	}
	if err := client.Set(ctx, "other:key", "skip", 0).Err(); err != nil {
		t.Fatalf("seed foreign key: %v", err)
	}

	var backup bytes.Buffer
	count, err := store.ExportBackup(ctx, &backup)
	if err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if count != 5 {
		t.Fatalf("ExportBackup() count = %d, want 5", count)
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB() error = %v", err)
	}

	restored, err := store.RestoreBackup(ctx, bytes.NewReader(backup.Bytes()))
	if err != nil {
		t.Fatalf("RestoreBackup() error = %v", err)
	}
	if restored != 5 {
		t.Fatalf("RestoreBackup() count = %d, want 5", restored)
	}

	if got, err := client.Get(ctx, "imsub:string").Result(); err != nil || got != "hello" {
		t.Fatalf("restored imsub:string = (%q, %v), want (%q, nil)", got, err, "hello")
	}
	if got, err := client.HGetAll(ctx, "imsub:hash").Result(); err != nil || got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("restored imsub:hash = (%v, %v), want fields", got, err)
	}
	setMembers, err := client.SMembers(ctx, "imsub:set").Result()
	if err != nil {
		t.Fatalf("SMembers() error = %v", err)
	}
	slices.Sort(setMembers)
	if !slices.Equal(setMembers, []string{"u1", "u2"}) {
		t.Fatalf("restored imsub:set = %v, want [u1 u2]", setMembers)
	}
	zsetMembers, err := client.ZRangeWithScores(ctx, "imsub:zset", 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRangeWithScores() error = %v", err)
	}
	if len(zsetMembers) != 2 || zsetMembers[0].Member != "m1" || zsetMembers[1].Member != "m2" {
		t.Fatalf("restored imsub:zset = %#v, want two members", zsetMembers)
	}
	if ttl, err := client.PTTL(ctx, "imsub:ttl").Result(); err != nil || ttl <= 0 {
		t.Fatalf("restored imsub:ttl ttl = (%v, %v), want > 0", ttl, err)
	}
	if exists, err := client.Exists(ctx, "other:key").Result(); err != nil || exists != 0 {
		t.Fatalf("restored foreign key exists = (%d, %v), want 0", exists, err)
	}
}

func TestRedisIncrementalBackupRestoreWithRealRedis(t *testing.T) {
	store, client := newRealRedisBackupStore(t)
	ctx := t.Context()

	if err := store.AddCreatorSubscriber(ctx, "creator-1", "sub-1"); err != nil {
		t.Fatalf("AddCreatorSubscriber(sub-1) error = %v", err)
	}
	if err := store.AddCreatorBlockedUser(ctx, "creator-1", "blocked-1"); err != nil {
		t.Fatalf("AddCreatorBlockedUser(blocked-1) error = %v", err)
	}
	if err := client.Set(ctx, "other:key", "skip", 0).Err(); err != nil {
		t.Fatalf("seed foreign key: %v", err)
	}

	var full bytes.Buffer
	if _, err := store.ExportBackup(ctx, &full); err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if err := client.Del(ctx, "imsub:backup:dirty", "imsub:backup:deleted").Err(); err != nil {
		t.Fatalf("clear backup tracking after full export: %v", err)
	}
	if err := client.Set(ctx, "imsub:backup:base_full_key", "backups/full/base.jsonl.gz", 0).Err(); err != nil {
		t.Fatalf("seed base full key: %v", err)
	}

	if err := store.AddCreatorSubscriber(ctx, "creator-1", "sub-2"); err != nil {
		t.Fatalf("AddCreatorSubscriber(sub-2) error = %v", err)
	}
	if err := store.FinalizeCreatorBlocklistDump(ctx, "creator-1", "", false); err != nil {
		t.Fatalf("FinalizeCreatorBlocklistDump(delete) error = %v", err)
	}

	var incremental bytes.Buffer
	if _, _, err := store.CreateBackup(ctx, &incremental, redis.BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Now().UTC()); err != nil {
		t.Fatalf("CreateBackup(incremental) error = %v", err)
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB() error = %v", err)
	}
	if _, err := store.RestoreBackup(ctx, bytes.NewReader(full.Bytes())); err != nil {
		t.Fatalf("RestoreBackup(full) error = %v", err)
	}
	if _, err := store.RestoreBackup(ctx, bytes.NewReader(incremental.Bytes())); err != nil {
		t.Fatalf("RestoreBackup(incremental) error = %v", err)
	}

	subs, err := client.SMembers(ctx, "imsub:creator:subscribers:creator-1").Result()
	if err != nil {
		t.Fatalf("SMembers(subscribers) error = %v", err)
	}
	slices.Sort(subs)
	if !slices.Equal(subs, []string{"sub-1", "sub-2"}) {
		t.Fatalf("restored subscribers = %v, want [sub-1 sub-2]", subs)
	}
	if exists, err := client.Exists(ctx, "imsub:creator:blocked:creator-1").Result(); err != nil || exists != 0 {
		t.Fatalf("restored blocklist exists = (%d, %v), want 0 nil", exists, err)
	}
	if exists, err := client.Exists(ctx, "other:key").Result(); err != nil || exists != 0 {
		t.Fatalf("restored foreign key exists = (%d, %v), want 0 nil", exists, err)
	}
}

func newRealRedisBackupStore(t *testing.T) (*redis.Store, *redislib.Client) {
	t.Helper()

	redisURL := strings.TrimSpace(os.Getenv("IMSUB_TEST_REDIS_URL"))
	if redisURL == "" {
		t.Skip("IMSUB_TEST_REDIS_URL is not set")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store, err := redis.NewStore(redisURL, logger)
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}

	opts, err := redislib.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse IMSUB_TEST_REDIS_URL: %v", err)
	}
	client := redislib.NewClient(opts)
	if err := client.Ping(t.Context()).Err(); err != nil {
		t.Fatalf("ping IMSUB_TEST_REDIS_URL: %v", err)
	}
	if err := client.FlushDB(t.Context()).Err(); err != nil {
		t.Fatalf("flush test redis db before test: %v", err)
	}

	t.Cleanup(func() {
		if err := client.FlushDB(context.Background()).Err(); err != nil {
			t.Logf("flush test redis db after test: %v", err)
		}
		if err := client.Close(); err != nil {
			t.Logf("close raw redis client: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Logf("close backup store: %v", err)
		}
	})

	return store, client
}
