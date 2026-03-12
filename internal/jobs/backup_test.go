package jobs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeBackupExporter struct {
	count int
	data  []byte
	err   error
}

type fakeBackupUploader struct {
	key         string
	size        int64
	contentType string
	body        []byte
	err         error
}

type fakeBackupMetrics struct {
	result    string
	duration  time.Duration
	keyCount  int
	sizeBytes int64
	calls     int
}

func (f fakeBackupExporter) ExportBackup(_ context.Context, w io.Writer) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	if _, err := w.Write(f.data); err != nil {
		return 0, err
	}
	return f.count, nil
}

func (f *fakeBackupUploader) Upload(_ context.Context, key string, r io.Reader, size int64, contentType string) error {
	if f.err != nil {
		return f.err
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.key = key
	f.size = size
	f.contentType = contentType
	f.body = body
	return nil
}

func (f *fakeBackupMetrics) RedisBackup(result string, d time.Duration, keyCount int, sizeBytes int64) {
	f.result = result
	f.duration = d
	f.keyCount = keyCount
	f.sizeBytes = sizeBytes
	f.calls++
}

func TestRedisBackupTaskUploadsSnapshot(t *testing.T) {
	t.Parallel()

	uploader := &fakeBackupUploader{}
	metrics := &fakeBackupMetrics{}
	taskIface := NewRedisBackupTask(fakeBackupExporter{
		count: 3,
		data:  []byte("backup-bytes"),
	}, uploader, slog.New(slog.DiscardHandler), metrics)

	task, ok := taskIface.(redisBackupTask)
	if !ok {
		t.Fatalf("NewRedisBackupTask() type = %T, want redisBackupTask", taskIface)
	}
	task.now = func() time.Time { return time.Date(2026, 3, 12, 15, 4, 5, 0, time.UTC) }

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if uploader.key != "backups/imsub-2026-03-12T15-04-05Z.jsonl.gz" {
		t.Fatalf("upload key = %q, want backup object key", uploader.key)
	}
	if uploader.contentType != "application/gzip" {
		t.Fatalf("upload content type = %q, want application/gzip", uploader.contentType)
	}
	if uploader.size != int64(len("backup-bytes")) {
		t.Fatalf("upload size = %d, want %d", uploader.size, len("backup-bytes"))
	}
	if !bytes.Equal(uploader.body, []byte("backup-bytes")) {
		t.Fatalf("upload body = %q, want %q", uploader.body, "backup-bytes")
	}
	if metrics.calls != 1 || metrics.result != "ok" {
		t.Fatalf("metrics = %+v, want one ok call", *metrics)
	}
	if metrics.keyCount != 3 || metrics.sizeBytes != int64(len("backup-bytes")) {
		t.Fatalf("metrics payload = (%d, %d), want (%d, %d)", metrics.keyCount, metrics.sizeBytes, 3, len("backup-bytes"))
	}
}

func TestRedisBackupTaskClassifiesFailures(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	metrics := &fakeBackupMetrics{}
	task := NewRedisBackupTask(fakeBackupExporter{err: wantErr}, &fakeBackupUploader{}, nil, metrics)

	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if got := task.Classify(err); got != taskResultFailed {
		t.Fatalf("Classify() = %q, want %q", got, taskResultFailed)
	}
	if metrics.calls != 1 || metrics.result != taskResultFailed || metrics.keyCount != 0 || metrics.sizeBytes != 0 {
		t.Fatalf("metrics = %+v, want one failed zeroed call", *metrics)
	}
}

func TestRedisBackupTaskRecordsUploadFailureMetrics(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("upload failed")
	metrics := &fakeBackupMetrics{}
	task := NewRedisBackupTask(fakeBackupExporter{
		count: 2,
		data:  []byte("backup-bytes"),
	}, &fakeBackupUploader{err: wantErr}, nil, metrics)

	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if metrics.calls != 1 || metrics.result != taskResultFailed || metrics.keyCount != 0 || metrics.sizeBytes != 0 {
		t.Fatalf("metrics = %+v, want one failed zeroed call", *metrics)
	}
}
