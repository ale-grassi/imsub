package jobs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

type backupExporter interface {
	ExportBackup(ctx context.Context, w io.Writer) (int, error)
}

type backupUploader interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

type backupMetrics interface {
	RedisBackup(result string, d time.Duration, keyCount int, sizeBytes int64)
}

type redisBackupTask struct {
	exporter backupExporter
	storage  backupUploader
	metrics  backupMetrics
	logger   *slog.Logger
	now      func() time.Time
}

// NewRedisBackupTask builds the periodic Redis backup uploader.
func NewRedisBackupTask(exporter backupExporter, storage backupUploader, logger *slog.Logger, metrics backupMetrics) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return redisBackupTask{
		exporter: exporter,
		storage:  storage,
		metrics:  metrics,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (t redisBackupTask) Name() string { return "backup_redis" }

func (t redisBackupTask) Run(ctx context.Context) error {
	if t.exporter == nil || t.storage == nil {
		return nil
	}
	startedAt := time.Now()
	var buf bytes.Buffer
	keyCount, err := t.exporter.ExportBackup(ctx, &buf)
	if err != nil {
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("export backup: %w", err)
	}

	objectKey := fmt.Sprintf("backups/imsub-%s.jsonl.gz", t.now().UTC().Format("2006-01-02T15-04-05Z"))
	if err := t.storage.Upload(ctx, objectKey, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/gzip"); err != nil {
		if t.metrics != nil {
			t.metrics.RedisBackup(taskResultFailed, time.Since(startedAt), 0, 0)
		}
		return fmt.Errorf("upload backup: %w", err)
	}
	// These metrics complement the runner-emitted background-job series with
	// backup-specific payload details such as key count and uploaded size.
	if t.metrics != nil {
		t.metrics.RedisBackup("ok", time.Since(startedAt), keyCount, int64(buf.Len()))
	}

	t.logger.Info("redis backup uploaded", "object_key", objectKey, "keys", keyCount, "size_bytes", buf.Len())
	return nil
}

func (t redisBackupTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}
