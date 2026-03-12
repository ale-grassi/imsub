package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"imsub/internal/adapter/s3"
)

func TestLoadRestoreConfigReadsDotEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	data := "" +
		"IMSUB_REDIS_URL=redis://localhost:6379/0\n" +
		"IMSUB_S3_ENDPOINT=fly.storage.tigris.dev\n" +
		"IMSUB_S3_BUCKET=imsub-backups\n" +
		"IMSUB_S3_ACCESS_KEY_ID=ak\n" +
		"IMSUB_S3_SECRET_ACCESS_KEY=sk\n"
	if err := os.WriteFile(envPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := loadRestoreConfig(envPath)
	if err != nil {
		t.Fatalf("loadRestoreConfig() error = %v", err)
	}
	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Fatalf("RedisURL = %q, want redis URL", cfg.RedisURL)
	}
	if cfg.S3Region != "auto" {
		t.Fatalf("S3Region = %q, want auto", cfg.S3Region)
	}
}

func TestLoadRestoreConfigRequiresKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IMSUB_REDIS_URL=redis://localhost:6379/0\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_, err := loadRestoreConfig(envPath)
	if err == nil {
		t.Fatal("loadRestoreConfig() error = nil, want non-nil")
	}
}

func TestSelectLatestBackupKey(t *testing.T) {
	t.Parallel()

	key, err := selectLatestBackupKey([]s3.ObjectInfo{
		{Key: "backups/a.jsonl.gz", LastModified: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)},
		{Key: "backups/b.jsonl.gz", LastModified: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("selectLatestBackupKey() error = %v", err)
	}
	if key != "backups/b.jsonl.gz" {
		t.Fatalf("selectLatestBackupKey() = %q, want latest key", key)
	}
}
