// Package startup provides phase-level timing instrumentation for application boot.
package startup

import (
	"log/slog"
	"time"
)

// Metrics is the subset of observability.Metrics used for startup instrumentation.
type Metrics interface {
	StartupPhase(phase, result string, d time.Duration)
	StartupReady(result string, total time.Duration, readyAt time.Time)
}

// Recorder times discrete startup phases and reports total boot duration on Ready.
type Recorder struct {
	logger   *slog.Logger
	metrics  Metrics
	bootedAt time.Time
}

// NewRecorder creates a Recorder anchored at the current time.
func NewRecorder(logger *slog.Logger, metrics Metrics) *Recorder {
	return NewRecorderAt(logger, metrics, time.Now())
}

// NewRecorderAt creates a Recorder anchored at bootedAt.
func NewRecorderAt(logger *slog.Logger, metrics Metrics, bootedAt time.Time) *Recorder {
	return &Recorder{
		logger:   logger,
		metrics:  metrics,
		bootedAt: bootedAt,
	}
}

// Phase runs fn, records its duration and result, and returns its error unchanged.
func (r *Recorder) Phase(name string, fn func() error) error {
	if r == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	d := time.Since(start)
	result := "ok"
	if err != nil {
		result = "failed"
	}
	if r.metrics != nil {
		r.metrics.StartupPhase(name, result, d)
	}
	if r.logger != nil {
		r.logger.Info("startup phase",
			"phase", name,
			"result", result,
			"duration_ms", d.Milliseconds(),
		)
	}
	return err
}

// Ready finalizes startup, emitting total duration and a readiness marker.
// Safe to call multiple times; only the first call records metrics.
func (r *Recorder) Ready(result string) {
	if r == nil || r.bootedAt.IsZero() {
		return
	}
	now := time.Now()
	total := now.Sub(r.bootedAt)
	if r.metrics != nil {
		r.metrics.StartupReady(result, total, now)
	}
	if r.logger != nil {
		r.logger.Info("startup complete",
			"result", result,
			"duration_ms", total.Milliseconds(),
		)
	}
	r.bootedAt = time.Time{}
}
