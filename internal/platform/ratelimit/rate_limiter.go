package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrNilLimiter indicates that a nil or uninitialized limiter was used.
	ErrNilLimiter = errors.New("ratelimit: nil limiter")
	// ErrNilContext indicates that Wait was called with a nil context.
	ErrNilContext = errors.New("ratelimit: nil context")
)

// RateLimiter enforces global and per-chat pacing for Telegram requests.
type RateLimiter struct {
	globalTicker    *time.Ticker
	perChatInterval time.Duration

	mu          sync.Mutex
	perChatNext map[int64]time.Time
}

// NewRateLimiter constructs a RateLimiter with sane defaults.
func NewRateLimiter(globalRPS int, perChatInterval time.Duration) *RateLimiter {
	if globalRPS <= 0 {
		globalRPS = 25
	}
	globalInterval := time.Second / time.Duration(globalRPS)
	if globalInterval <= 0 {
		globalInterval = 40 * time.Millisecond
	}
	return &RateLimiter{
		globalTicker:    time.NewTicker(globalInterval),
		perChatInterval: perChatInterval,
		perChatNext:     make(map[int64]time.Time),
	}
}

// Close stops the global ticker used by the limiter.
func (l *RateLimiter) Close() {
	if l == nil || l.globalTicker == nil {
		return
	}
	l.globalTicker.Stop()
}

// Wait blocks until global and per-chat rate limits allow one request.
func (l *RateLimiter) Wait(ctx context.Context, chatID int64) error {
	if ctx == nil {
		return ErrNilContext
	}
	if l == nil || l.globalTicker == nil {
		return ErrNilLimiter
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.globalTicker.C:
	}

	if chatID == 0 {
		return nil
	}

	delay := l.reserveChatDelay(chatID)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *RateLimiter) reserveChatDelay(chatID int64) time.Duration {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perChatNext == nil {
		l.perChatNext = make(map[int64]time.Time)
	}

	// Prune stale entries periodically to prevent unbounded map growth.
	if len(l.perChatNext) > 1000 {
		for id, t := range l.perChatNext {
			if t.Before(now) {
				delete(l.perChatNext, id)
			}
		}
	}

	next := l.perChatNext[chatID]
	base := now
	if next.After(base) {
		base = next
	}
	l.perChatNext[chatID] = base.Add(l.perChatInterval)
	return base.Sub(now)
}
