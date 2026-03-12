package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ErrMissingEnv indicates one or more required environment variables are not set.
var ErrMissingEnv = errors.New("missing env vars")

var errInvalidBackupInterval = errors.New("IMSUB_BACKUP_INTERVAL must be > 0")

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
	S3Endpoint            string
	S3Bucket              string
	S3AccessKeyID         string
	S3SecretAccessKey     string
	S3Region              string
	BackupInterval        time.Duration
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
		S3Endpoint:            strings.TrimSpace(os.Getenv("IMSUB_S3_ENDPOINT")),
		S3Bucket:              strings.TrimSpace(os.Getenv("IMSUB_S3_BUCKET")),
		S3AccessKeyID:         strings.TrimSpace(os.Getenv("IMSUB_S3_ACCESS_KEY_ID")),
		S3SecretAccessKey:     strings.TrimSpace(os.Getenv("IMSUB_S3_SECRET_ACCESS_KEY")),
		S3Region:              strings.TrimSpace(os.Getenv("IMSUB_S3_REGION")),
	}

	backupIntervalRaw := strings.TrimSpace(os.Getenv("IMSUB_BACKUP_INTERVAL"))
	if backupIntervalRaw == "" {
		cfg.BackupInterval = 6 * time.Hour
	} else {
		parsedInterval, err := time.ParseDuration(backupIntervalRaw)
		if err != nil {
			return Config{}, fmt.Errorf("parse IMSUB_BACKUP_INTERVAL: %w", err)
		}
		cfg.BackupInterval = parsedInterval
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
	if cfg.S3Region == "" {
		cfg.S3Region = "auto"
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
	if anyBackupConfigSet(cfg) {
		backupRequired := []struct {
			key string
			val string
		}{
			{key: "IMSUB_S3_ENDPOINT", val: cfg.S3Endpoint},
			{key: "IMSUB_S3_BUCKET", val: cfg.S3Bucket},
			{key: "IMSUB_S3_ACCESS_KEY_ID", val: cfg.S3AccessKeyID},
			{key: "IMSUB_S3_SECRET_ACCESS_KEY", val: cfg.S3SecretAccessKey},
		}
		backupMissing := make([]string, 0, len(backupRequired))
		for _, req := range backupRequired {
			if strings.TrimSpace(req.val) == "" {
				backupMissing = append(backupMissing, req.key)
			}
		}
		if len(backupMissing) > 0 {
			return Config{}, fmt.Errorf("missing env vars %s: %w", strings.Join(backupMissing, ", "), ErrMissingEnv)
		}
		if cfg.BackupInterval <= 0 {
			return Config{}, errInvalidBackupInterval
		}
	}

	return cfg, nil
}

// BackupEnabled reports whether S3-backed periodic backups are configured.
func (c Config) BackupEnabled() bool {
	return c.S3Endpoint != "" &&
		c.S3Bucket != "" &&
		c.S3AccessKeyID != "" &&
		c.S3SecretAccessKey != ""
}

func anyBackupConfigSet(cfg Config) bool {
	return cfg.S3Endpoint != "" ||
		cfg.S3Bucket != "" ||
		cfg.S3AccessKeyID != "" ||
		cfg.S3SecretAccessKey != ""
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
