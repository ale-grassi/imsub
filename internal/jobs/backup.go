package jobs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

const (
	redisBackupKindFull        = "full"
	redisBackupKindIncremental = "incremental"
)

type backupExporter interface {
	ShouldCreateFullBackup(ctx context.Context, now time.Time, fullInterval time.Duration) (bool, error)
	CreateBackup(ctx context.Context, w io.Writer, kind, objectKey string, createdAt time.Time) (int, string, error)
	FinishBackup(ctx context.Context, token string, uploaded bool, kind, objectKey string, createdAt time.Time) error
}

type backupUploader interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type backupMetrics interface {
	RedisBackup(result string, d time.Duration, keyCount int, sizeBytes int64)
}

type redisBackupTask struct {
	exporter  backupExporter
	storage   backupUploader
	metrics   backupMetrics
	logger    *slog.Logger
	now       func() time.Time
	fullEvery time.Duration
}

// NewRedisBackupTask builds the periodic Redis backup uploader.
func NewRedisBackupTask(exporter backupExporter, storage backupUploader, logger *slog.Logger, metrics backupMetrics) Task {
	return NewRedisBackupTaskWithFullInterval(exporter, storage, logger, metrics, 168*time.Hour)
}

// NewRedisBackupTaskWithFullInterval builds the periodic Redis backup uploader with a full baseline cadence.
func NewRedisBackupTaskWithFullInterval(exporter backupExporter, storage backupUploader, logger *slog.Logger, metrics backupMetrics, fullEvery time.Duration) Task {
	if logger == nil {
		logger = slog.Default()
	}
	if fullEvery <= 0 {
		fullEvery = 168 * time.Hour
	}
	return redisBackupTask{
		exporter:  exporter,
		storage:   storage,
		metrics:   metrics,
		logger:    logger,
		now:       func() time.Time { return time.Now().UTC() },
		fullEvery: fullEvery,
	}
}

func (t redisBackupTask) Name() string { return "backup_redis" }

func (t redisBackupTask) Run(ctx context.Context) error {
	if t.exporter == nil || t.storage == nil {
		return nil
	}
	startedAt := time.Now()
	createdAt := t.now().UTC()
	full, err := t.exporter.ShouldCreateFullBackup(ctx, createdAt, t.fullEvery)
	if err != nil {
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("choose backup kind: %w", err)
	}
	kind := redisBackupKindIncremental
	nameKind := redisBackupKindIncremental
	if full {
		kind = redisBackupKindFull
		nameKind = redisBackupKindFull
	}
	objectKey := fmt.Sprintf("backups/%s/imsub-%s-%s.jsonl.gz", kind, nameKind, createdAt.Format("2006-01-02T15-04-05Z"))

	var buf bytes.Buffer
	keyCount, token, err := t.exporter.CreateBackup(ctx, &buf, kind, objectKey, createdAt)
	if err != nil {
		if token != "" {
			if finishErr := t.exporter.FinishBackup(ctx, token, false, kind, objectKey, createdAt); finishErr != nil {
				t.logger.Warn("redis backup tracking requeue failed", "object_key", objectKey, "error", finishErr)
			}
		}
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("export backup: %w", err)
	}

	if err := t.storage.Upload(ctx, objectKey, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/gzip"); err != nil {
		if finishErr := t.exporter.FinishBackup(ctx, token, false, kind, objectKey, createdAt); finishErr != nil {
			t.logger.Warn("redis backup tracking requeue failed", "object_key", objectKey, "error", finishErr)
		}
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("upload backup: %w", err)
	}
	if err := t.exporter.FinishBackup(ctx, token, true, kind, objectKey, createdAt); err != nil {
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("finish backup: %w", err)
	}
	// These metrics complement the runner-emitted background-job series with
	// backup-specific payload details such as key count and uploaded size.
	if t.metrics != nil {
		t.metrics.RedisBackup("ok", time.Since(startedAt), keyCount, int64(buf.Len()))
	}

	t.logger.Info("redis backup uploaded", "object_key", objectKey, "kind", kind, "keys", keyCount, "size_bytes", buf.Len())
	return nil
}

func (t redisBackupTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}
