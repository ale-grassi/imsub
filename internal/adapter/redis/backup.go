package redis

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"
)

const backupScanCount = 100

var errInvalidBackupTTL = errors.New("invalid ttl_ms")

// BackupEntry is a single logical Redis key dump record.
type BackupEntry struct {
	Key   string `json:"key"`
	Dump  string `json:"dump"`
	TTLMS int64  `json:"ttl_ms"`
}

// ExportBackup streams a gzip-compressed JSON Lines backup of all imsub keys.
func (s *Store) ExportBackup(ctx context.Context, w io.Writer) (int, error) {
	gz := gzip.NewWriter(w)
	enc := json.NewEncoder(gz)

	var (
		cursor uint64
		count  int
	)
	for {
		keys, nextCursor, err := s.rdb.Scan(ctx, cursor, "imsub:*", backupScanCount).Result()
		if err != nil {
			_ = gz.Close()
			return count, fmt.Errorf("scan imsub keys: %w", err)
		}
		if err := s.writeBackupBatch(ctx, enc, keys, &count); err != nil {
			_ = gz.Close()
			return count, err
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	if err := gz.Close(); err != nil {
		return count, fmt.Errorf("close backup gzip stream: %w", err)
	}
	return count, nil
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
	for {
		var entry BackupEntry
		if err := dec.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return count, fmt.Errorf("decode backup entry: %w", err)
		}
		dump, err := base64.StdEncoding.DecodeString(entry.Dump)
		if err != nil {
			return count, fmt.Errorf("decode backup dump for %q: %w", entry.Key, err)
		}
		ttl, err := restoreTTL(entry.TTLMS)
		if err != nil {
			return count, fmt.Errorf("%w for %q", err, entry.Key)
		}
		if err := s.rdb.RestoreReplace(ctx, entry.Key, ttl, string(dump)).Err(); err != nil {
			return count, fmt.Errorf("restore key %q: %w", entry.Key, err)
		}
		count++
	}
	return count, nil
}

func (s *Store) writeBackupBatch(ctx context.Context, enc *json.Encoder, keys []string, count *int) error {
	if len(keys) == 0 {
		return nil
	}
	pipe := s.rdb.Pipeline()
	dumps := make([]*redis.StringCmd, len(keys))
	ttls := make([]*redis.DurationCmd, len(keys))
	for i, key := range keys {
		dumps[i] = pipe.Dump(ctx, key)
		ttls[i] = pipe.PTTL(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
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
