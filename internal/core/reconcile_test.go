package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type reconcileFakeStore struct {
	Store
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

	calls := 0
	svc := NewReconciler(
		&reconcileFakeStore{
			listActiveCreatorsFn: func(context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1"},
					{ID: "c2"},
				}, nil
			},
		},
		func(context.Context, Creator) (int, error) {
			calls++
			return 1, nil
		},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
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

	svc := NewReconciler(
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

	svc := NewReconciler(
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
