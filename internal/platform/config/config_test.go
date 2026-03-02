package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadMissingEnvOrder(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "")
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

func TestLoadDefaultsAndNormalization(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "tg-token")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "tw-client")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "tw-secret")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "eventsub-secret")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "https://example.com/")
	t.Setenv("IMSUB_REDIS_URL", "redis://localhost:6379/0")
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
	if cfg.MetricsPath != "/metrics" {
		t.Errorf("Load().MetricsPath = %q, want %q", cfg.MetricsPath, "/metrics")
	}
	if !cfg.MetricsEnabled {
		t.Errorf("Load().MetricsEnabled = %v, want %v", cfg.MetricsEnabled, true)
	}
	if cfg.DebugLogs {
		t.Errorf("Load().DebugLogs = %v, want %v", cfg.DebugLogs, false)
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
