package core

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
)

type reconcileFakeStore struct {
	listActiveCreatorsFn func(ctx context.Context) ([]Creator, error)
}

func (f *reconcileFakeStore) ListActiveCreators(ctx context.Context) ([]Creator, error) {
	if f.listActiveCreatorsFn != nil {
		return f.listActiveCreatorsFn(ctx)
	}
	return nil, nil
}

func TestReconcileSubscribersOnceOK(t *testing.T) {
	t.Parallel()

	var calls int64
	svc := NewReconcilerService(
		&reconcileFakeStore{
			listActiveCreatorsFn: func(context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1"},
					{ID: "c2"},
				}, nil
			},
		},
		func(context.Context, Creator) (int, error) {
			atomic.AddInt64(&calls, 1)
			return 1, nil
		},
		slog.New(slog.DiscardHandler),
	)

	if err := svc.ReconcileSubscribersOnce(t.Context()); err != nil {
		t.Fatalf("ReconcileSubscribersOnce() returned error %v, want nil", err)
	}
	if calls != 2 {
		t.Errorf("ReconcileSubscribersOnce() dump call count = %d, want %d", calls, 2)
	}
}

func TestReconcileSubscribersOnceListError(t *testing.T) {
	t.Parallel()

	svc := NewReconcilerService(
		&reconcileFakeStore{
			listActiveCreatorsFn: func(context.Context) ([]Creator, error) {
				return nil, errors.New("redis down")
			},
		},
		func(context.Context, Creator) (int, error) { return 0, nil },
		nil,
	)

	err := svc.ReconcileSubscribersOnce(t.Context())
	if !errors.Is(err, ErrListActiveCreators) {
		t.Fatalf("ReconcileSubscribersOnce() returned error %v, want error matching %v", err, ErrListActiveCreators)
	}
}

func TestReconcileSubscribersOncePartialFailure(t *testing.T) {
	t.Parallel()

	svc := NewReconcilerService(
		&reconcileFakeStore{
			listActiveCreatorsFn: func(context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1"},
					{ID: "c2"},
				}, nil
			},
		},
		func(_ context.Context, c Creator) (int, error) {
			if c.ID == "c2" {
				return 0, errors.New("twitch error")
			}
			return 1, nil
		},
		nil,
	)

	err := svc.ReconcileSubscribersOnce(t.Context())
	if !errors.Is(err, ErrPartialReconcile) {
		t.Fatalf("ReconcileSubscribersOnce() returned error %v, want error matching %v", err, ErrPartialReconcile)
	}
}

func TestReconcileSubscribersOnceRunsCreatorsConcurrently(t *testing.T) {
	t.Parallel()

	creators := []Creator{
		{ID: "c1"},
		{ID: "c2"},
		{ID: "c3"},
		{ID: "c4"},
	}
	started := make(chan string, len(creators))
	release := make(chan struct{})
	var running int64
	var maxRunning int64

	svc := NewReconcilerService(
		&reconcileFakeStore{
			listActiveCreatorsFn: func(context.Context) ([]Creator, error) {
				return creators, nil
			},
		},
		func(_ context.Context, c Creator) (int, error) {
			nowRunning := atomic.AddInt64(&running, 1)
			for {
				observed := atomic.LoadInt64(&maxRunning)
				if nowRunning <= observed || atomic.CompareAndSwapInt64(&maxRunning, observed, nowRunning) {
					break
				}
			}
			started <- c.ID
			<-release
			atomic.AddInt64(&running, -1)
			return 1, nil
		},
		slog.New(slog.DiscardHandler),
	)

	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = svc.ReconcileSubscribersOnce(t.Context())
	}()

	for range creators {
		<-started
	}
	if got := atomic.LoadInt64(&maxRunning); got < 2 {
		t.Fatalf("max concurrent dumps = %d, want at least 2", got)
	}
	close(release)
	wg.Wait()

	if runErr != nil {
		t.Fatalf("ReconcileSubscribersOnce() returned error %v, want nil", runErr)
	}
}
