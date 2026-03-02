package app

import (
	"errors"
	"testing"

	"imsub/internal/platform/config"
)

func TestRunFailsFastOnMissingConfig(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "")
	t.Setenv("IMSUB_REDIS_URL", "")

	err := Run()
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !errors.Is(err, config.ErrMissingEnv) {
		t.Fatalf("Run() error = %v, want errors.Is(err, config.ErrMissingEnv)=true", err)
	}
}
