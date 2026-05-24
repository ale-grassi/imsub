package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "tg-token")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_ID", "1001")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_HASH", "mtproto-hash")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_SESSION", "c2Vzc2lvbg==")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "tw-client")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "tw-secret")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "eventsub-secret")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "https://example.com")
	t.Setenv("IMSUB_REDIS_URL", "redis://localhost:6379/0")
}

func TestLoadMissingEnvOrder(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_ID", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_HASH", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_SESSION", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "")
	t.Setenv("IMSUB_REDIS_URL", "")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() error = nil, want non-nil (cfg=%+v)", cfg)
	}

	if !errors.Is(err, ErrMissingEnv) {
		t.Fatalf("Load() error type mismatch: got %v, want errors.Is(_, ErrMissingEnv)=true", err)
	}
	for _, env := range []string{
		"IMSUB_TELEGRAM_BOT_TOKEN",
		"IMSUB_TWITCH_CLIENT_ID",
		"IMSUB_TWITCH_CLIENT_SECRET",
		"IMSUB_TWITCH_EVENTSUB_SECRET",
		"IMSUB_PUBLIC_BASE_URL",
		"IMSUB_REDIS_URL",
	} {
		if !strings.Contains(err.Error(), env) {
			t.Errorf("Load() error = %q, want to mention %q", err.Error(), env)
		}
	}
}

func TestLoadAllowsMTProtoToBeUnset(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "tg-token")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_ID", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_HASH", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_SESSION", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "tw-client")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "tw-secret")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "eventsub-secret")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "https://example.com")
	t.Setenv("IMSUB_REDIS_URL", "redis://localhost:6379/0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.MTProtoEnabled() {
		t.Fatal("Load().MTProtoEnabled() = true, want false")
	}
}

func TestLoadRequiresCompleteMTProtoWhenPartiallyConfigured(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "tg-token")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_ID", "1001")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_API_HASH", "")
	t.Setenv("IMSUB_TELEGRAM_MTPROTO_SESSION", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "tw-client")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "tw-secret")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "eventsub-secret")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "https://example.com")
	t.Setenv("IMSUB_REDIS_URL", "redis://localhost:6379/0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing mtproto env error")
	}
	if !errors.Is(err, ErrMissingEnv) {
		t.Fatalf("Load() error = %v, want ErrMissingEnv", err)
	}
	for _, env := range []string{
		"IMSUB_TELEGRAM_MTPROTO_API_HASH",
		"IMSUB_TELEGRAM_MTPROTO_SESSION",
	} {
		if !strings.Contains(err.Error(), env) {
			t.Fatalf("Load() error = %q, want to mention %q", err.Error(), env)
		}
	}
}

func TestLoadDefaultsAndNormalization(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "https://example.com/")
	t.Setenv("IMSUB_LISTEN_ADDR", "")
	t.Setenv("IMSUB_TWITCH_WEBHOOK_PATH", "hooks/twitch")
	t.Setenv("IMSUB_TELEGRAM_WEBHOOK_PATH", "hooks/tg")
	t.Setenv("IMSUB_METRICS_PATH", "")
	t.Setenv("IMSUB_METRICS_ENABLED", "")
	t.Setenv("IMSUB_DEBUG_LOGS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.PublicBaseURL != "https://example.com" {
		t.Errorf("Load().PublicBaseURL = %q, want %q", cfg.PublicBaseURL, "https://example.com")
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("Load().ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.TwitchWebhookPath != "/hooks/twitch" {
		t.Errorf("Load().TwitchWebhookPath = %q, want %q", cfg.TwitchWebhookPath, "/hooks/twitch")
	}
	if cfg.TelegramWebhookPath != "/hooks/tg" {
		t.Errorf("Load().TelegramWebhookPath = %q, want %q", cfg.TelegramWebhookPath, "/hooks/tg")
	}
	if cfg.TelegramMTProtoAppID != 1001 {
		t.Errorf("Load().TelegramMTProtoAppID = %d, want %d", cfg.TelegramMTProtoAppID, 1001)
	}
	if cfg.TelegramMTProtoHash != "mtproto-hash" {
		t.Errorf("Load().TelegramMTProtoHash = %q, want %q", cfg.TelegramMTProtoHash, "mtproto-hash")
	}
	if cfg.TelegramMTProtoSession != "c2Vzc2lvbg==" {
		t.Errorf("Load().TelegramMTProtoSession = %q, want %q", cfg.TelegramMTProtoSession, "c2Vzc2lvbg==")
	}
	if cfg.MetricsPath != "/metrics" {
		t.Errorf("Load().MetricsPath = %q, want %q", cfg.MetricsPath, "/metrics")
	}
	if !cfg.MetricsEnabled {
		t.Errorf("Load().MetricsEnabled = %v, want %v", cfg.MetricsEnabled, true)
	}
	if cfg.DebugLogs {
		t.Errorf("Load().DebugLogs = %v, want %v", cfg.DebugLogs, false)
	}
	if cfg.S3Region != "auto" {
		t.Errorf("Load().S3Region = %q, want %q", cfg.S3Region, "auto")
	}
	if cfg.BackupInterval != 6*time.Hour {
		t.Errorf("Load().BackupInterval = %s, want %s", cfg.BackupInterval, 6*time.Hour)
	}
	if cfg.FullBackupInterval != 168*time.Hour {
		t.Errorf("Load().FullBackupInterval = %s, want %s", cfg.FullBackupInterval, 168*time.Hour)
	}
}

func TestLoadParsesGodTelegramUserIDs(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_GOD_TELEGRAM_USER_IDS", "7, 42,7, 99")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !slices.Equal(cfg.GodTelegramUserIDs, []int64{7, 42, 99}) {
		t.Fatalf("Load().GodTelegramUserIDs = %v, want [7 42 99]", cfg.GodTelegramUserIDs)
	}
}

func TestLoadRejectsInvalidGodTelegramUserIDs(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_GOD_TELEGRAM_USER_IDS", "7, nope")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want parse failure")
	}
	if !strings.Contains(err.Error(), "IMSUB_GOD_TELEGRAM_USER_IDS") {
		t.Fatalf("Load() error = %q, want IMSUB_GOD_TELEGRAM_USER_IDS mention", err.Error())
	}
}

func TestLoadBackupConfigEnabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_S3_ENDPOINT", "https://fly.storage.tigris.dev")
	t.Setenv("IMSUB_S3_BUCKET", "imsub-backups")
	t.Setenv("IMSUB_S3_ACCESS_KEY_ID", "ak")
	t.Setenv("IMSUB_S3_SECRET_ACCESS_KEY", "sk")
	t.Setenv("IMSUB_S3_REGION", "")
	t.Setenv("IMSUB_BACKUP_INTERVAL", "12h")
	t.Setenv("IMSUB_FULL_BACKUP_INTERVAL", "72h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !cfg.BackupEnabled() {
		t.Fatal("Load().BackupEnabled() = false, want true")
	}
	if cfg.BackupInterval != 12*time.Hour {
		t.Fatalf("Load().BackupInterval = %s, want %s", cfg.BackupInterval, 12*time.Hour)
	}
	if cfg.FullBackupInterval != 72*time.Hour {
		t.Fatalf("Load().FullBackupInterval = %s, want %s", cfg.FullBackupInterval, 72*time.Hour)
	}
	if cfg.S3Region != "auto" {
		t.Fatalf("Load().S3Region = %q, want %q", cfg.S3Region, "auto")
	}
}

func TestLoadBackupConfigRequiresCompleteCredentials(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_S3_ENDPOINT", "fly.storage.tigris.dev")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing backup env error")
	}
	if !errors.Is(err, ErrMissingEnv) {
		t.Fatalf("Load() error = %v, want ErrMissingEnv", err)
	}
	for _, env := range []string{
		"IMSUB_S3_BUCKET",
		"IMSUB_S3_ACCESS_KEY_ID",
		"IMSUB_S3_SECRET_ACCESS_KEY",
	} {
		if !strings.Contains(err.Error(), env) {
			t.Fatalf("Load() error = %q, want to mention %q", err.Error(), env)
		}
	}
}

func TestLoadBackupConfigRejectsInvalidInterval(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_S3_ENDPOINT", "fly.storage.tigris.dev")
	t.Setenv("IMSUB_S3_BUCKET", "imsub-backups")
	t.Setenv("IMSUB_S3_ACCESS_KEY_ID", "ak")
	t.Setenv("IMSUB_S3_SECRET_ACCESS_KEY", "sk")
	t.Setenv("IMSUB_BACKUP_INTERVAL", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid backup interval")
	}
	if !strings.Contains(err.Error(), "IMSUB_BACKUP_INTERVAL") {
		t.Fatalf("Load() error = %q, want to mention IMSUB_BACKUP_INTERVAL", err.Error())
	}
}

func TestLoadBackupConfigRejectsInvalidFullInterval(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_S3_ENDPOINT", "fly.storage.tigris.dev")
	t.Setenv("IMSUB_S3_BUCKET", "imsub-backups")
	t.Setenv("IMSUB_S3_ACCESS_KEY_ID", "ak")
	t.Setenv("IMSUB_S3_SECRET_ACCESS_KEY", "sk")
	t.Setenv("IMSUB_FULL_BACKUP_INTERVAL", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid full backup interval")
	}
	if !strings.Contains(err.Error(), "IMSUB_FULL_BACKUP_INTERVAL") {
		t.Fatalf("Load() error = %q, want to mention IMSUB_FULL_BACKUP_INTERVAL", err.Error())
	}
}

func TestLoadBackupDefaultsAloneDoNotEnableValidation(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("IMSUB_S3_REGION", "auto")
	t.Setenv("IMSUB_BACKUP_INTERVAL", "6h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.BackupEnabled() {
		t.Fatal("Load().BackupEnabled() = true, want false")
	}
	if cfg.S3Region != "auto" {
		t.Fatalf("Load().S3Region = %q, want %q", cfg.S3Region, "auto")
	}
	if cfg.BackupInterval != 6*time.Hour {
		t.Fatalf("Load().BackupInterval = %s, want %s", cfg.BackupInterval, 6*time.Hour)
	}
	if cfg.FullBackupInterval != 168*time.Hour {
		t.Fatalf("Load().FullBackupInterval = %s, want %s", cfg.FullBackupInterval, 168*time.Hour)
	}
}

func TestEnvParsers(t *testing.T) {
	t.Parallel()

	trueVals := []string{"1", "true", "YES", "On", "debug"}
	for _, v := range trueVals {
		if !IsTrueEnv(v) {
			t.Errorf("IsTrueEnv(%q) = false, want true", v)
		}
	}
	if IsTrueEnv("nope") {
		t.Error("IsTrueEnv(\"nope\") = true, want false")
	}

	falseVals := []string{"0", "false", "NO", "off"}
	for _, v := range falseVals {
		if !IsFalseEnv(v) {
			t.Errorf("IsFalseEnv(%q) = false, want true", v)
		}
	}
	if IsFalseEnv("enabled") {
		t.Error("IsFalseEnv(\"enabled\") = true, want false")
	}
}
