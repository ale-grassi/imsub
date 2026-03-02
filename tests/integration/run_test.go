//go:build integration
// +build integration

package integration

import (
	"errors"
	"testing"

	"imsub/internal/app"
	"imsub/internal/platform/config"
)

func TestRunFailsFastOnMissingConfig(t *testing.T) {
	t.Setenv("IMSUB_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_ID", "")
	t.Setenv("IMSUB_TWITCH_CLIENT_SECRET", "")
	t.Setenv("IMSUB_TWITCH_EVENTSUB_SECRET", "")
	t.Setenv("IMSUB_PUBLIC_BASE_URL", "")
	t.Setenv("IMSUB_REDIS_URL", "")

	err := app.Run()
	if err == nil {
		t.Fatal("app.Run() returned error nil, want config error")
	}
	if !errors.Is(err, config.ErrMissingEnv) {
		t.Fatalf("app.Run() returned error %v, want wrapped config.ErrMissingEnv", err)
	}
}
