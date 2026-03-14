package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"imsub/internal/adapter/s3"
)

func TestLoadBackupConfigReadsDotEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	data := "" +
		"IMSUB_S3_ENDPOINT=fly.storage.tigris.dev\n" +
		"IMSUB_S3_BUCKET=imsub-backups\n" +
		"IMSUB_S3_ACCESS_KEY_ID=ak\n" +
		"IMSUB_S3_SECRET_ACCESS_KEY=sk\n"
	if err := os.WriteFile(envPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := loadBackupConfig(envPath)
	if err != nil {
		t.Fatalf("loadBackupConfig() error = %v", err)
	}
	if cfg.S3Region != "auto" {
		t.Fatalf("S3Region = %q, want auto", cfg.S3Region)
	}
}

func TestLoadBackupConfigRequiresKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IMSUB_REDIS_URL=redis://localhost:6379/0\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_, err := loadBackupConfig(envPath)
	if err == nil {
		t.Fatal("loadBackupConfig() error = nil, want non-nil")
	}
}

func TestLoadMTProtoConfigReadsDotEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env.dev")
	data := "" +
		"IMSUB_TELEGRAM_MTPROTO_API_ID=12345\n" +
		"IMSUB_TELEGRAM_MTPROTO_API_HASH=hash-value\n"
	if err := os.WriteFile(envPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write .env.dev: %v", err)
	}

	cfg, err := loadMTProtoConfig(envPath)
	if err != nil {
		t.Fatalf("loadMTProtoConfig() error = %v", err)
	}
	if cfg.AppID != 12345 {
		t.Fatalf("AppID = %d, want 12345", cfg.AppID)
	}
	if cfg.AppHash != "hash-value" {
		t.Fatalf("AppHash = %q, want hash-value", cfg.AppHash)
	}
}

func TestLoadMTProtoConfigRequiresKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env.dev")
	if err := os.WriteFile(envPath, []byte("IMSUB_TELEGRAM_MTPROTO_API_ID=\n"), 0o600); err != nil {
		t.Fatalf("write .env.dev: %v", err)
	}

	_, err := loadMTProtoConfig(envPath)
	if err == nil {
		t.Fatal("loadMTProtoConfig() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "IMSUB_TELEGRAM_MTPROTO_API_ID") || !strings.Contains(err.Error(), "IMSUB_TELEGRAM_MTPROTO_API_HASH") {
		t.Fatalf("loadMTProtoConfig() error = %v, want mtproto env mentions", err)
	}
}

func TestResolveRedisURLUsesOverride(t *testing.T) {
	t.Parallel()

	got, err := resolveRedisURL("/nonexistent/.env", "redis://override:6379/0")
	if err != nil {
		t.Fatalf("resolveRedisURL() error = %v", err)
	}
	if got != "redis://override:6379/0" {
		t.Fatalf("resolveRedisURL() = %q, want override", got)
	}
}

func TestResolveRedisURLReadsEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IMSUB_REDIS_URL=redis://localhost:6379/0\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	got, err := resolveRedisURL(envPath, "")
	if err != nil {
		t.Fatalf("resolveRedisURL() error = %v", err)
	}
	if got != "redis://localhost:6379/0" {
		t.Fatalf("resolveRedisURL() = %q, want env redis URL", got)
	}
}

func TestResolveRedisURLRequiresEnvRedisURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IMSUB_S3_BUCKET=ignored\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_, err := resolveRedisURL(envPath, "")
	if err == nil {
		t.Fatal("resolveRedisURL() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "IMSUB_REDIS_URL") {
		t.Fatalf("resolveRedisURL() error = %v, want IMSUB_REDIS_URL mention", err)
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

func TestUsageErrorIncludesBackupCommands(t *testing.T) {
	t.Parallel()

	err := usageError("missing command")
	if err == nil {
		t.Fatal("usageError() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "imsub-admin backup-download") {
		t.Fatalf("usageError() = %q, want backup-download usage", err)
	}
	if !strings.Contains(err.Error(), "imsub-admin backup-load") {
		t.Fatalf("usageError() = %q, want backup-load usage", err)
	}
	if !strings.Contains(err.Error(), "imsub-admin mtproto-session") {
		t.Fatalf("usageError() = %q, want mtproto-session usage", err)
	}
}
