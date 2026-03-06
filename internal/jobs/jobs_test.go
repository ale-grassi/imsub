package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"imsub/internal/core"
)

type fakeStore struct {
	listCreatorsFn     func(ctx context.Context) ([]core.Creator, error)
	activeWithoutGroup func(ctx context.Context, creators []core.Creator) (int, error)
	repairReverseIndex func(ctx context.Context, creators []core.Creator) (int, int, int, int, error)
}

func (f *fakeStore) ListCreators(ctx context.Context) ([]core.Creator, error) {
	if f.listCreatorsFn != nil {
		return f.listCreatorsFn(ctx)
	}
	return nil, nil
}

func (f *fakeStore) ActiveCreatorIDsWithoutGroup(ctx context.Context, creators []core.Creator) (int, error) {
	if f.activeWithoutGroup != nil {
		return f.activeWithoutGroup(ctx, creators)
	}
	return 0, nil
}

func (f *fakeStore) RepairUserCreatorReverseIndex(ctx context.Context, creators []core.Creator) (indexUsers, repairedUsers, missingLinks, staleLinks int, err error) {
	if f.repairReverseIndex != nil {
		return f.repairReverseIndex(ctx, creators)
	}
	return 0, 0, 0, 0, nil
}

type fakeReconciler struct {
	mu     sync.Mutex
	result string
	calls  int
}

func (f *fakeReconciler) ReconcileSubscribersOnce(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.result != "ok" && f.result != "" {
		if f.result == "partial_failure" {
			return core.ErrPartialReconcile
		}
		return errors.New(f.result)
	}
	return nil
}

func (f *fakeReconciler) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeObserver struct {
	mu         sync.Mutex
	lastJob    string
	lastResult string
	calls      int
}

func (f *fakeObserver) BackgroundJob(job, result string, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastJob = job
	f.lastResult = result
}

func (f *fakeObserver) snapshot() (calls int, job, result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastJob, f.lastResult
}

func TestReconcileSubscribersOnceRecordsObserverResult(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	reconcile := &fakeReconciler{result: "partial_failure"}
	j := New(nil, reconcile, nil, obs)

	_ = j.ReconcileSubscribersOnce(t.Context())

	calls, job, result := obs.snapshot()
	if calls != 1 {
		t.Fatalf("expected 1 observer call, got %d", calls)
	}
	if job != "reconcile_subscribers" {
		t.Errorf("Observer() job = %q, want \"reconcile_subscribers\"", job)
	}
	if result != "partial_failure" {
		t.Errorf("Observer() result = %q, want \"partial_failure\"", result)
	}
}

func TestRunSubscriberReconcilerStopsOnCancel(t *testing.T) {
	t.Parallel()

	reconcile := &fakeReconciler{result: "ok"}
	j := New(nil, reconcile, nil, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = j.RunSubscriberReconciler(ctx, 5*time.Millisecond)
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if reconcile.callCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reconcile.callCount() == 0 {
		t.Fatal("expected at least one reconcile call")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("RunSubscriberReconciler did not stop after cancel")
	}
}

func TestRunIntegrityAuditOnceRecordsFailureResult(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	store := &fakeStore{
		listCreatorsFn: func(_ context.Context) ([]core.Creator, error) {
			return nil, errors.New("boom")
		},
	}
	j := New(store, nil, nil, obs)

	_ = j.RunIntegrityAuditOnce(t.Context())

	calls, job, result := obs.snapshot()
	if calls != 1 {
		t.Fatalf("expected 1 observer call, got %d", calls)
	}
	if job != "integrity_audit" {
		t.Errorf("Observer() job = %q, want \"integrity_audit\"", job)
	}
	if result != "list_creators_failed" {
		t.Errorf("Observer() result = %q, want \"list_creators_failed\"", result)
	}
}
