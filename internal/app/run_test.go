package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"imsub/internal/platform/config"

	"github.com/mymmrac/telego/telegoapi"
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

func TestNewTelegramAPICallerUsesRetryPolicy(t *testing.T) {
	t.Parallel()

	caller := newTelegramAPICaller()

	retryCaller, ok := caller.(*telegoapi.RetryCaller)
	if !ok {
		t.Fatalf("newTelegramAPICaller() type = %T, want *telegoapi.RetryCaller", caller)
	}
	if retryCaller.MaxAttempts != telegramRetryMaxAttempts {
		t.Errorf("RetryCaller.MaxAttempts = %d, want %d", retryCaller.MaxAttempts, telegramRetryMaxAttempts)
	}
	if retryCaller.ExponentBase != telegramRetryExponent {
		t.Errorf("RetryCaller.ExponentBase = %v, want %v", retryCaller.ExponentBase, telegramRetryExponent)
	}
	if retryCaller.StartDelay != telegramRetryStartDelay {
		t.Errorf("RetryCaller.StartDelay = %s, want %s", retryCaller.StartDelay, telegramRetryStartDelay)
	}
	if retryCaller.MaxDelay != telegramRetryMaxDelay {
		t.Errorf("RetryCaller.MaxDelay = %s, want %s", retryCaller.MaxDelay, telegramRetryMaxDelay)
	}
	if retryCaller.RateLimit != telegoapi.RetryRateLimitWaitOrAbort {
		t.Errorf("RetryCaller.RateLimit = %v, want %v", retryCaller.RateLimit, telegoapi.RetryRateLimitWaitOrAbort)
	}
	if !retryCaller.BufferRequestData {
		t.Error("RetryCaller.BufferRequestData = false, want true")
	}
	if _, ok := retryCaller.Caller.(telegoapi.FastHTTPCaller); !ok {
		t.Errorf("RetryCaller.Caller type = %T, want telegoapi.FastHTTPCaller", retryCaller.Caller)
	}
}

func TestTelegramRetryConstantsStayConservative(t *testing.T) {
	t.Parallel()

	if telegramRetryMaxAttempts < 2 {
		t.Fatalf("telegramRetryMaxAttempts = %d, want at least 2", telegramRetryMaxAttempts)
	}
	if telegramRetryStartDelay <= 0 {
		t.Fatalf("telegramRetryStartDelay = %s, want positive", telegramRetryStartDelay)
	}
	if telegramRetryMaxDelay < telegramRetryStartDelay {
		t.Fatalf("telegramRetryMaxDelay = %s, want >= %s", telegramRetryMaxDelay, telegramRetryStartDelay)
	}
	if telegramRetryMaxDelay < 45*time.Second {
		t.Fatalf("telegramRetryMaxDelay = %s, want >= 45s", telegramRetryMaxDelay)
	}
	if telegramRetryMaxDelay > 2*time.Minute {
		t.Fatalf("telegramRetryMaxDelay = %s, want <= 2m", telegramRetryMaxDelay)
	}
}

func TestTelegramAllowedUpdatesIncludesMembershipUpdates(t *testing.T) {
	t.Parallel()

	got := telegramAllowedUpdates()
	want := []string{"message", "callback_query", "chat_join_request", "chat_member", "my_chat_member"}
	if !slices.Equal(got, want) {
		t.Fatalf("telegramAllowedUpdates() = %v, want %v", got, want)
	}
}
