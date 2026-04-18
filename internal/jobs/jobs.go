package jobs

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strconv"
	"sync"
	"time"

	"imsub/internal/events"
)

// ErrInvalidInterval indicates that a schedule interval is not strictly positive.
var ErrInvalidInterval = errors.New("jobs: invalid interval")

// Task is a named background work unit.
type Task interface {
	Name() string
	Run(ctx context.Context) error
	Classify(err error) string
}

// Reporter is an optional Task extension that produces a per-run summary of
// work performed. Values are merged into the background_job event Fields and
// emitted as imsub_background_job_items_total counts.
type Reporter interface {
	// Report returns a fresh snapshot of the last run's counters (kind -> count)
	// and resets internal state for the next run.
	Report() map[string]int
}

// Schedule configures how a task should be run.
type Schedule struct {
	Task         Task
	InitialDelay time.Duration
	Interval     time.Duration
	// Timeout, when > 0, bounds a single task execution via context.WithTimeout.
	// Set Timeout < Interval to guarantee no overlap between runs.
	Timeout time.Duration
}

// Runner executes scheduled background tasks and emits shared job events.
type Runner struct {
	logger *slog.Logger
	events events.EventSink
}

// NewRunner creates a background job runner.
func NewRunner(logger *slog.Logger, sink events.EventSink) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{logger: logger, events: sink}
}

// RunScheduled runs a scheduled task until ctx is done.
func (r *Runner) RunScheduled(ctx context.Context, schedule Schedule) error {
	if schedule.Interval <= 0 {
		r.logger.Warn("background task not started: non-positive interval", "task", taskName(schedule.Task), "interval", schedule.Interval)
		return ErrInvalidInterval
	}
	if schedule.Timeout > 0 && schedule.Timeout >= schedule.Interval {
		r.logger.Warn("background task timeout >= interval; runs may overlap if timeout is not reached early", "task", taskName(schedule.Task), "timeout", schedule.Timeout, "interval", schedule.Interval)
	}
	if schedule.InitialDelay > 0 {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(schedule.InitialDelay):
		}
	}

	r.runTask(ctx, schedule)

	ticker := time.NewTicker(schedule.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.runTask(ctx, schedule)
		}
	}
}

func (r *Runner) runTask(ctx context.Context, schedule Schedule) {
	task := schedule.Task
	if task == nil {
		return
	}

	r.emit(ctx, events.Event{
		Name:   events.NameBackgroundJobStarted,
		Fields: map[string]string{"job": task.Name()},
	})

	runCtx := ctx
	var cancel context.CancelFunc
	if schedule.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, schedule.Timeout)
	}
	start := time.Now()
	err := task.Run(runCtx)
	duration := time.Since(start)
	if cancel != nil {
		cancel()
	}

	result := "ok"
	if err != nil {
		result = task.Classify(err)
		// Override generic "failed" to "timeout" when the deadline clearly expired,
		// so callers don't each need to special-case context.DeadlineExceeded.
		if result == taskResultFailed && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			result = taskResultTimeout
		}
		r.logger.Warn("background task failed", "task", task.Name(), "error", err, "result", result)
	}

	fields := map[string]string{"job": task.Name()}
	var items map[string]int
	if reporter, ok := task.(Reporter); ok {
		items = reporter.Report()
		for kind, n := range items {
			fields["items_"+kind] = strconv.Itoa(n)
		}
	}

	r.emit(ctx, events.Event{
		Name:     events.NameBackgroundJob,
		Outcome:  result,
		Fields:   fields,
		Duration: duration,
	})

	for kind, n := range items {
		r.emit(ctx, events.Event{
			Name:    events.NameBackgroundJobItems,
			Outcome: result,
			Fields:  map[string]string{"job": task.Name(), "kind": kind},
			Count:   n,
		})
	}
}

func (r *Runner) emit(ctx context.Context, evt events.Event) {
	if r.events == nil {
		return
	}
	r.events.Emit(ctx, evt)
}

func taskName(task Task) string {
	if task == nil {
		return "unknown"
	}
	return task.Name()
}

// runCounts is a goroutine-safe per-run counter bag shared via pointer between
// value-receiver method calls on a Task.
type runCounts struct {
	mu sync.Mutex
	m  map[string]int
}

func newRunCounts() *runCounts {
	return &runCounts{m: make(map[string]int)}
}

func (c *runCounts) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string]int)
}

func (c *runCounts) add(kind string, n int) {
	if c == nil || n == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]int)
	}
	c.m[kind] += n
}

func (c *runCounts) inc(kind string) { c.add(kind, 1) }

// snapshot returns the current counters and resets state.
func (c *runCounts) snapshot() map[string]int {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.m))
	maps.Copy(out, c.m)
	c.m = make(map[string]int)
	return out
}
