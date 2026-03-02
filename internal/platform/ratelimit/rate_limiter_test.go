package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRateLimiterReserveChatDelay(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter(1000, 20*time.Millisecond)
	t.Cleanup(l.Close)

	if d := l.reserveChatDelay(1); d < 0 {
		t.Fatalf("reserveChatDelay(%d) first call = %v, want non-negative", 1, d)
	}
	if d := l.reserveChatDelay(1); d <= 0 {
		t.Fatalf("reserveChatDelay(%d) second call = %v, want positive", 1, d)
	}
	if d := l.reserveChatDelay(2); d < 0 {
		t.Fatalf("reserveChatDelay(%d) = %v, want non-negative", 2, d)
	}
}

func TestRateLimiterWaitCanceledContext(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter(1, time.Second)
	t.Cleanup(l.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := l.Wait(ctx, 0); err == nil {
		t.Fatal("Wait(ctx, 0) = nil, want non-nil error")
	}
}

func TestRateLimiterWaitPerChatSpacing(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter(1000, 10*time.Millisecond)
	t.Cleanup(l.Close)

	ctx := t.Context()
	if err := l.Wait(ctx, 123); err != nil {
		t.Fatalf("Wait(ctx, %d) first call error = %v, want nil", 123, err)
	}
	start := time.Now()
	if err := l.Wait(ctx, 123); err != nil {
		t.Fatalf("Wait(ctx, %d) second call error = %v, want nil", 123, err)
	}
	if elapsed := time.Since(start); elapsed < 8*time.Millisecond {
		t.Fatalf("Wait(ctx, %d) second call elapsed = %v, want >= %v", 123, elapsed, 8*time.Millisecond)
	}
}

func TestRateLimiterCloseNilSafe(t *testing.T) {
	t.Parallel()

	var l *RateLimiter
	l.Close()
}

func TestRateLimiterWaitNilLimiter(t *testing.T) {
	t.Parallel()

	var l *RateLimiter
	if err := l.Wait(t.Context(), 0); !errors.Is(err, ErrNilLimiter) {
		t.Fatalf("Wait(ctx, 0) error = %v, want errors.Is(err, ErrNilLimiter)", err)
	}
}

func TestRateLimiterWaitNilContext(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter(10, time.Millisecond)
	t.Cleanup(l.Close)

	if err := l.Wait(nil, 0); !errors.Is(err, ErrNilContext) { //nolint:staticcheck
		t.Fatalf("Wait(nil, 0) error = %v, want errors.Is(err, ErrNilContext)", err)
	}
}
