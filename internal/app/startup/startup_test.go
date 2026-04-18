package startup

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

type recordedPhase struct {
	phase    string
	result   string
	duration time.Duration
}

type recordedReady struct {
	result   string
	total    time.Duration
	readyAt  time.Time
	observed bool
}

type fakeMetrics struct {
	phases []recordedPhase
	ready  recordedReady
}

func (m *fakeMetrics) StartupPhase(phase, result string, d time.Duration) {
	m.phases = append(m.phases, recordedPhase{phase: phase, result: result, duration: d})
}

func (m *fakeMetrics) StartupReady(result string, total time.Duration, readyAt time.Time) {
	m.ready = recordedReady{result: result, total: total, readyAt: readyAt, observed: true}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRecorderPhaseOKAndFailed(t *testing.T) {
	t.Parallel()
	m := &fakeMetrics{}
	r := NewRecorderAt(newTestLogger(), m, time.Now())

	if err := r.Phase("redis_connect", func() error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("Phase() ok-case error = %v, want nil", err)
	}

	wantErr := errors.New("boom")
	if err := r.Phase("schema_ensure", func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("Phase() failed-case error = %v, want %v", err, wantErr)
	}

	if len(m.phases) != 2 {
		t.Fatalf("len(phases) = %d, want 2", len(m.phases))
	}
	if m.phases[0].phase != "redis_connect" || m.phases[0].result != "ok" {
		t.Errorf("phases[0] = %+v, want phase=redis_connect result=ok", m.phases[0])
	}
	if m.phases[0].duration < 5*time.Millisecond {
		t.Errorf("phases[0].duration = %s, want >= 5ms", m.phases[0].duration)
	}
	if m.phases[1].phase != "schema_ensure" || m.phases[1].result != "failed" {
		t.Errorf("phases[1] = %+v, want phase=schema_ensure result=failed", m.phases[1])
	}
}

func TestRecorderReadyEmitsTotal(t *testing.T) {
	t.Parallel()
	m := &fakeMetrics{}
	bootedAt := time.Now().Add(-250 * time.Millisecond)
	r := NewRecorderAt(newTestLogger(), m, bootedAt)

	r.Ready("ok")
	if !m.ready.observed {
		t.Fatal("StartupReady not called")
	}
	if m.ready.result != "ok" {
		t.Errorf("ready.result = %q, want ok", m.ready.result)
	}
	if m.ready.total < 200*time.Millisecond {
		t.Errorf("ready.total = %s, want >= 200ms", m.ready.total)
	}

	m.ready = recordedReady{}
	r.Ready("ok")
	if m.ready.observed {
		t.Error("second Ready() call should be a no-op")
	}
}

func TestRecorderNilSafe(t *testing.T) {
	t.Parallel()
	var r *Recorder
	wantErr := errors.New("x")
	if err := r.Phase("anything", func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Errorf("nil Recorder Phase() error = %v, want %v", err, wantErr)
	}
	r.Ready("ok")
}
