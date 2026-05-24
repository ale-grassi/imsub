package redis

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	backupScanCount = 100

	// BackupKindFull exports every application Redis key.
	BackupKindFull = "full"
	// BackupKindIncremental exports changed keys and delete tombstones since the current full baseline.
	BackupKindIncremental = "incremental"

	backupFormat = "imsub-redis-backup"
)

var (
	errInvalidBackupTTL       = errors.New("invalid ttl_ms")
	errInvalidBackupFormat    = errors.New("invalid backup format")
	errInvalidBackupKind      = errors.New("invalid backup kind")
	errInvalidBackupEntryType = errors.New("invalid backup entry type")
)

// BackupEntry is a single logical Redis backup record.
type BackupEntry struct {
	Type  string `json:"type,omitempty"`
	Key   string `json:"key"`
	Dump  string `json:"dump,omitempty"`
	TTLMS int64  `json:"ttl_ms,omitempty"`
}

type backupManifest struct {
	Format      string `json:"format"`
	Kind        string `json:"kind"`
	CreatedAt   string `json:"created_at"`
	BaseFullKey string `json:"base_full_key,omitempty"`
}

type backupAttempt struct {
	dirtyKey   string
	deletedKey string
}

// ShouldCreateFullBackup reports whether the next scheduled backup should be a full baseline.
func (s *Store) ShouldCreateFullBackup(ctx context.Context, now time.Time, fullInterval time.Duration) (bool, error) {
	if fullInterval <= 0 {
		return true, nil
	}
	raw, err := s.rdb.Get(skipBackupTracking(ctx), keyBackupBaseFullAt()).Result()
	if errors.Is(err, redis.Nil) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get backup base timestamp: %w", err)
	}
	baseAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return true, fmt.Errorf("parse backup base timestamp %q: %w", raw, err)
	}
	return !now.UTC().Before(baseAt.Add(fullInterval)), nil
}

// CreateBackup streams a gzip-compressed JSON Lines backup. Full backups export all application keys;
// incremental backups export keys rotated from the dirty set and tombstones from the deleted set.
func (s *Store) CreateBackup(ctx context.Context, w io.Writer, kind, objectKey string, createdAt time.Time) (int, string, error) {
	attempt, err := s.rotateBackupTracking(ctx, createdAt)
	if err != nil {
		return 0, "", err
	}
	count, exportErr := s.writeBackup(ctx, w, kind, objectKey, createdAt, attempt)
	return count, attemptToken(attempt), exportErr
}

// FinishBackup finalizes or requeues the rotated tracking sets from CreateBackup.
func (s *Store) FinishBackup(ctx context.Context, token string, uploaded bool, kind, objectKey string, createdAt time.Time) error {
	attempt := parseAttemptToken(token)
	if uploaded {
		pipe := s.rdb.TxPipeline()
		pipe.Del(skipBackupTracking(ctx), attempt.dirtyKey, attempt.deletedKey)
		if kind == BackupKindFull {
			pipe.Set(skipBackupTracking(ctx), keyBackupBaseFullKey(), objectKey, 0)
			pipe.Set(skipBackupTracking(ctx), keyBackupBaseFullAt(), createdAt.UTC().Format(time.RFC3339Nano), 0)
		}
		if _, err := pipe.Exec(skipBackupTracking(ctx)); err != nil {
			return fmt.Errorf("finish backup tracking: %w", err)
		}
		return nil
	}
	return s.requeueBackupTracking(ctx, attempt)
}

// ExportBackup streams a gzip-compressed full backup of all imsub keys.
func (s *Store) ExportBackup(ctx context.Context, w io.Writer) (int, error) {
	return s.writeBackup(ctx, w, BackupKindFull, "", time.Now().UTC(), backupAttempt{})
}

// RestoreBackup reads a gzip-compressed JSON Lines backup and restores keys with REPLACE.
func (s *Store) RestoreBackup(ctx context.Context, r io.Reader) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("open backup gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	dec := json.NewDecoder(gz)
	count := 0
	var manifest backupManifest
	if err := dec.Decode(&manifest); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.Format != backupFormat {
		return 0, fmt.Errorf("%w: %q", errInvalidBackupFormat, manifest.Format)
	}
	for {
		var entry BackupEntry
		if err := dec.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return count, fmt.Errorf("decode backup entry: %w", err)
		}
		restored, err := s.applyBackupEntry(ctx, entry)
		if err != nil {
			return count, err
		}
		if restored {
			count++
		}
	}
	return count, nil
}

func (s *Store) writeBackup(ctx context.Context, w io.Writer, kind, objectKey string, createdAt time.Time, attempt backupAttempt) (int, error) {
	gz := gzip.NewWriter(w)
	enc := json.NewEncoder(gz)
	baseFullKey := objectKey
	if kind == BackupKindIncremental {
		key, err := s.rdb.Get(skipBackupTracking(ctx), keyBackupBaseFullKey()).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			_ = gz.Close()
			return 0, fmt.Errorf("get backup base key: %w", err)
		}
		baseFullKey = key
	}
	if err := enc.Encode(backupManifest{
		Format:      backupFormat,
		Kind:        kind,
		CreatedAt:   createdAt.UTC().Format(time.RFC3339Nano),
		BaseFullKey: baseFullKey,
	}); err != nil {
		_ = gz.Close()
		return 0, fmt.Errorf("encode backup manifest: %w", err)
	}

	var count int
	switch kind {
	case BackupKindFull:
		var cursor uint64
		for {
			keys, nextCursor, err := s.rdb.Scan(skipBackupTracking(ctx), cursor, "imsub:*", backupScanCount).Result()
			if err != nil {
				_ = gz.Close()
				return count, fmt.Errorf("scan imsub keys: %w", err)
			}
			keys = slices.DeleteFunc(keys, func(key string) bool { return !isBackupExportedKey(key) })
			if err := s.writeBackupBatch(ctx, enc, keys, &count); err != nil {
				_ = gz.Close()
				return count, err
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	case BackupKindIncremental:
		keys, err := s.rdb.SMembers(skipBackupTracking(ctx), attempt.dirtyKey).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			_ = gz.Close()
			return count, fmt.Errorf("read rotated dirty keys: %w", err)
		}
		slices.Sort(keys)
		keys = slices.Compact(keys)
		keys = slices.DeleteFunc(keys, func(key string) bool { return !isBackupExportedKey(key) })
		if err := s.writeBackupBatch(ctx, enc, keys, &count); err != nil {
			_ = gz.Close()
			return count, err
		}
		deleted, err := s.rdb.SMembers(skipBackupTracking(ctx), attempt.deletedKey).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			_ = gz.Close()
			return count, fmt.Errorf("read rotated deleted keys: %w", err)
		}
		slices.Sort(deleted)
		for _, key := range slices.Compact(deleted) {
			if !isBackupExportedKey(key) {
				continue
			}
			exists, err := s.rdb.Exists(skipBackupTracking(ctx), key).Result()
			if err != nil {
				_ = gz.Close()
				return count, fmt.Errorf("check deleted key %q existence: %w", key, err)
			}
			if exists > 0 {
				continue
			}
			if err := enc.Encode(BackupEntry{Type: "delete", Key: key}); err != nil {
				_ = gz.Close()
				return count, fmt.Errorf("encode backup tombstone for %q: %w", key, err)
			}
			count++
		}
	default:
		_ = gz.Close()
		return count, fmt.Errorf("%w: %q", errInvalidBackupKind, kind)
	}
	if err := gz.Close(); err != nil {
		return count, fmt.Errorf("close backup gzip stream: %w", err)
	}
	return count, nil
}

func (s *Store) applyBackupEntry(ctx context.Context, entry BackupEntry) (bool, error) {
	switch entry.Type {
	case "", "key":
		dump, err := base64.StdEncoding.DecodeString(entry.Dump)
		if err != nil {
			return false, fmt.Errorf("decode backup dump for %q: %w", entry.Key, err)
		}
		ttl, err := restoreTTL(entry.TTLMS)
		if err != nil {
			return false, fmt.Errorf("%w for %q", err, entry.Key)
		}
		if err := s.rdb.RestoreReplace(skipBackupTracking(ctx), entry.Key, ttl, string(dump)).Err(); err != nil {
			return false, fmt.Errorf("restore key %q: %w", entry.Key, err)
		}
		return true, nil
	case "delete":
		if err := s.rdb.Del(skipBackupTracking(ctx), entry.Key).Err(); err != nil {
			return false, fmt.Errorf("restore delete %q: %w", entry.Key, err)
		}
		return true, nil
	default:
		return false, fmt.Errorf("%w %q for %q", errInvalidBackupEntryType, entry.Type, entry.Key)
	}
}

func (s *Store) writeBackupBatch(ctx context.Context, enc *json.Encoder, keys []string, count *int) error {
	if len(keys) == 0 {
		return nil
	}
	pipe := s.rdb.Pipeline()
	dumps := make([]*redis.StringCmd, len(keys))
	ttls := make([]*redis.DurationCmd, len(keys))
	for i, key := range keys {
		dumps[i] = pipe.Dump(skipBackupTracking(ctx), key)
		ttls[i] = pipe.PTTL(skipBackupTracking(ctx), key)
	}
	if _, err := pipe.Exec(skipBackupTracking(ctx)); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("dump backup batch: %w", err)
	}
	for i, key := range keys {
		dump, err := dumps[i].Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return fmt.Errorf("dump key %q: %w", key, err)
		}
		ttl, err := ttls[i].Result()
		if err != nil {
			return fmt.Errorf("pttl key %q: %w", key, err)
		}
		ttlMS, ok := backupTTLMillis(ttl)
		if !ok {
			continue
		}
		if err := enc.Encode(BackupEntry{
			Type:  "key",
			Key:   key,
			Dump:  base64.StdEncoding.EncodeToString([]byte(dump)),
			TTLMS: ttlMS,
		}); err != nil {
			return fmt.Errorf("encode backup entry for %q: %w", key, err)
		}
		(*count)++
	}
	return nil
}

func (s *Store) rotateBackupTracking(ctx context.Context, createdAt time.Time) (backupAttempt, error) {
	suffix := createdAt.UTC().Format("20060102T150405.000000000")
	attempt := backupAttempt{
		dirtyKey:   "imsub:backup:dirty:rotated:" + suffix,
		deletedKey: "imsub:backup:deleted:rotated:" + suffix,
	}
	ctx = skipBackupTracking(ctx)
	if err := s.rdb.Del(ctx, attempt.dirtyKey, attempt.deletedKey).Err(); err != nil {
		return backupAttempt{}, fmt.Errorf("clear rotated backup tracking keys: %w", err)
	}
	if err := renameIfExists(ctx, s.rdb, keyBackupDirty(), attempt.dirtyKey); err != nil {
		return backupAttempt{}, fmt.Errorf("rotate dirty backup set: %w", err)
	}
	if err := renameIfExists(ctx, s.rdb, keyBackupDeleted(), attempt.deletedKey); err != nil {
		return backupAttempt{}, fmt.Errorf("rotate deleted backup set: %w", err)
	}
	return attempt, nil
}

func (s *Store) requeueBackupTracking(ctx context.Context, attempt backupAttempt) error {
	ctx = skipBackupTracking(ctx)
	dirty, err := s.rdb.SMembers(ctx, attempt.dirtyKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("read rotated dirty keys for requeue: %w", err)
	}
	deleted, err := s.rdb.SMembers(ctx, attempt.deletedKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("read rotated deleted keys for requeue: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	if len(dirty) > 0 {
		pipe.SAdd(ctx, keyBackupDirty(), stringSliceToAny(dirty)...)
	}
	if len(deleted) > 0 {
		pipe.SAdd(ctx, keyBackupDeleted(), stringSliceToAny(deleted)...)
	}
	pipe.Del(ctx, attempt.dirtyKey, attempt.deletedKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("requeue backup tracking: %w", err)
	}
	return nil
}

func renameIfExists(ctx context.Context, rdb *redis.Client, src, dst string) error {
	exists, err := rdb.Exists(ctx, src).Result()
	if err != nil {
		return fmt.Errorf("check key %q before rename: %w", src, err)
	}
	if exists == 0 {
		return nil
	}
	if err := rdb.Rename(ctx, src, dst).Err(); err != nil {
		return fmt.Errorf("rename %q to %q: %w", src, dst, err)
	}
	return nil
}

func attemptToken(attempt backupAttempt) string {
	return attempt.dirtyKey + "\n" + attempt.deletedKey
}

func parseAttemptToken(token string) backupAttempt {
	parts := strings.SplitN(token, "\n", 2)
	attempt := backupAttempt{}
	if len(parts) > 0 {
		attempt.dirtyKey = parts[0]
	}
	if len(parts) > 1 {
		attempt.deletedKey = parts[1]
	}
	return attempt
}

func isBackupExportedKey(key string) bool {
	return strings.HasPrefix(key, "imsub:") && !strings.HasPrefix(key, "imsub:backup:")
}

func backupTTLMillis(ttl time.Duration) (int64, bool) {
	if ttl == -2 {
		return 0, false
	}
	if ttl == -1 {
		return -1, true
	}
	return ttl.Milliseconds(), true
}

func restoreTTL(ttlMS int64) (time.Duration, error) {
	switch {
	case ttlMS < -1:
		return 0, fmt.Errorf("%w: %d", errInvalidBackupTTL, ttlMS)
	case ttlMS == -1:
		return 0, nil
	case ttlMS == 0:
		return time.Millisecond, nil
	default:
		return time.Duration(ttlMS) * time.Millisecond, nil
	}
}
