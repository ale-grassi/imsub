package observability

import (
	"context"
	"log/slog"

	"imsub/internal/events"
)

// EventLogger writes selected shared events to structured logs.
type EventLogger struct {
	logger *slog.Logger
}

// NewEventLogger creates an event logger sink.
func NewEventLogger(logger *slog.Logger) *EventLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventLogger{logger: logger}
}

// Emit logs selected events with low-cardinality structured fields.
func (l *EventLogger) Emit(ctx context.Context, evt events.Event) {
	if l == nil || l.logger == nil {
		return
	}

	level := slog.LevelDebug
	switch evt.Name {
	case events.NameBackgroundJob, events.NameBackgroundJobStarted, events.NameReconciliationRepair, events.NameCreatorActivation, events.NameSubscriptionEnd, events.NameCreatorAuthTransition, events.NameCreatorReconnectNotice, events.NameCreatorTokenRefresh:
		level = slog.LevelInfo
	}

	attrs := []any{
		"event_name", evt.Name,
		"outcome", evt.Outcome,
		"count", evt.Count,
		"duration_ms", evt.Duration.Milliseconds(),
	}
	for k, v := range evt.Fields {
		attrs = append(attrs, k, v)
	}
	l.logger.Log(ctx, level, "event", attrs...)
}
