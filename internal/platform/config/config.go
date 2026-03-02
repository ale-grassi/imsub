package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrMissingEnv indicates one or more required environment variables are not set.
var ErrMissingEnv = errors.New("missing env vars")

// Config holds runtime configuration sourced from environment variables.
type Config struct {
	TelegramBotToken      string
	TelegramWebhookSecret string
	TelegramWebhookPath   string
	TwitchClientID        string
	TwitchClientSecret    string
	TwitchEventSubSecret  string
	PublicBaseURL         string
	TwitchWebhookPath     string
	ListenAddr            string
	RedisURL              string
	DebugLogs             bool
	MetricsEnabled        bool
	MetricsPath           string
}

// Load reads, normalizes, and validates configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken:      os.Getenv("IMSUB_TELEGRAM_BOT_TOKEN"),
		TelegramWebhookSecret: os.Getenv("IMSUB_TELEGRAM_WEBHOOK_SECRET"),
		TelegramWebhookPath:   os.Getenv("IMSUB_TELEGRAM_WEBHOOK_PATH"),
		TwitchClientID:        os.Getenv("IMSUB_TWITCH_CLIENT_ID"),
		TwitchClientSecret:    os.Getenv("IMSUB_TWITCH_CLIENT_SECRET"),
		TwitchEventSubSecret:  os.Getenv("IMSUB_TWITCH_EVENTSUB_SECRET"),
		PublicBaseURL:         strings.TrimRight(os.Getenv("IMSUB_PUBLIC_BASE_URL"), "/"),
		TwitchWebhookPath:     os.Getenv("IMSUB_TWITCH_WEBHOOK_PATH"),
		ListenAddr:            os.Getenv("IMSUB_LISTEN_ADDR"),
		RedisURL:              os.Getenv("IMSUB_REDIS_URL"),
		DebugLogs:             IsTrueEnv(os.Getenv("IMSUB_DEBUG_LOGS")),
		MetricsEnabled:        !IsFalseEnv(os.Getenv("IMSUB_METRICS_ENABLED")),
		MetricsPath:           os.Getenv("IMSUB_METRICS_PATH"),
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.TwitchWebhookPath == "" {
		cfg.TwitchWebhookPath = "/webhooks/twitch"
	}
	if !strings.HasPrefix(cfg.TwitchWebhookPath, "/") {
		cfg.TwitchWebhookPath = "/" + cfg.TwitchWebhookPath
	}
	if cfg.TelegramWebhookPath == "" {
		cfg.TelegramWebhookPath = "/webhooks/telegram"
	}
	if !strings.HasPrefix(cfg.TelegramWebhookPath, "/") {
		cfg.TelegramWebhookPath = "/" + cfg.TelegramWebhookPath
	}
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = "/metrics"
	}
	if !strings.HasPrefix(cfg.MetricsPath, "/") {
		cfg.MetricsPath = "/" + cfg.MetricsPath
	}

	required := []struct {
		key string
		val string
	}{
		{key: "IMSUB_TELEGRAM_BOT_TOKEN", val: cfg.TelegramBotToken},
		{key: "IMSUB_TWITCH_CLIENT_ID", val: cfg.TwitchClientID},
		{key: "IMSUB_TWITCH_CLIENT_SECRET", val: cfg.TwitchClientSecret},
		{key: "IMSUB_TWITCH_EVENTSUB_SECRET", val: cfg.TwitchEventSubSecret},
		{key: "IMSUB_PUBLIC_BASE_URL", val: cfg.PublicBaseURL},
		{key: "IMSUB_REDIS_URL", val: cfg.RedisURL},
	}
	missing := make([]string, 0, len(required))
	for _, req := range required {
		if strings.TrimSpace(req.val) == "" {
			missing = append(missing, req.key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing env vars %s: %w", strings.Join(missing, ", "), ErrMissingEnv)
	}

	return cfg, nil
}

// IsTrueEnv reports whether v matches a truthy environment value.
func IsTrueEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "debug":
		return true
	default:
		return false
	}
}

// IsFalseEnv reports whether v matches a falsy environment value.
func IsFalseEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}
