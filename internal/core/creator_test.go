package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type creatorFakeStore struct {
	Store
	getOwnedFn func(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	countFn    func(ctx context.Context, creatorID string) (int64, error)
}

func (f *creatorFakeStore) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error) {
	if f.getOwnedFn != nil {
		return f.getOwnedFn(ctx, ownerTelegramID)
	}
	return Creator{}, false, nil
}

func (f *creatorFakeStore) CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error) {
	if f.countFn != nil {
		return f.countFn(ctx, creatorID)
	}
	return 0, nil
}

type fakeChecker struct {
	activeFn func(ctx context.Context, creatorID string) (bool, error)
}

func (f *fakeChecker) IsEventSubActiveForCreator(ctx context.Context, creatorID string) (bool, error) {
	if f.activeFn != nil {
		return f.activeFn(ctx, creatorID)
	}
	return false, nil
}

func TestLoadStatus(t *testing.T) {
	t.Parallel()

	wantAuthAt := time.Date(2026, 3, 7, 17, 0, 0, 0, time.UTC)
	wantSyncAt := time.Date(2026, 3, 7, 17, 5, 0, 0, time.UTC)
	svc := NewCreator(
		&creatorFakeStore{
			countFn: func(_ context.Context, _ string) (int64, error) {
				return 12, nil
			},
		},
		&fakeChecker{
			activeFn: func(_ context.Context, _ string) (bool, error) {
				return true, nil
			},
		},
		nil,
	)

	status, err := svc.LoadStatus(t.Context(), Creator{
		ID:            "c1",
		AuthStatus:    CreatorAuthReconnectRequired,
		AuthErrorCode: "token_refresh_failed",
		AuthStatusAt:  wantAuthAt,
		LastSyncAt:    wantSyncAt,
	})
	if err != nil {
		t.Fatalf("LoadStatus(%q) returned error %v, want nil", "c1", err)
	}
	if got, want := status.EventSub, EventSubActive; got != want {
		t.Errorf("LoadStatus(%q).EventSub = %q, want %q", "c1", got, want)
	}
	if got, want := status.HasSubscriberCount, true; got != want {
		t.Errorf("LoadStatus(%q).HasSubscriberCount = %t, want %t", "c1", got, want)
	}
	if got, want := status.SubscriberCount, int64(12); got != want {
		t.Errorf("LoadStatus(%q).SubscriberCount = %d, want %d", "c1", got, want)
	}
	if got, want := status.Auth, CreatorAuthReconnectRequired; got != want {
		t.Errorf("LoadStatus(%q).Auth = %q, want %q", "c1", got, want)
	}
	if got, want := status.AuthStatusAt, wantAuthAt; !got.Equal(want) {
		t.Errorf("LoadStatus(%q).AuthStatusAt = %v, want %v", "c1", got, want)
	}
	if got, want := status.LastSyncAt, wantSyncAt; !got.Equal(want) {
		t.Errorf("LoadStatus(%q).LastSyncAt = %v, want %v", "c1", got, want)
	}
}

func TestLoadStatusErrorsDegradeToUnknown(t *testing.T) {
	t.Parallel()

	svc := NewCreator(
		&creatorFakeStore{
			countFn: func(_ context.Context, _ string) (int64, error) {
				return 0, errors.New("count failed")
			},
		},
		&fakeChecker{
			activeFn: func(_ context.Context, _ string) (bool, error) {
				return false, errors.New("check failed")
			},
		},
		nil,
	)

	status, err := svc.LoadStatus(t.Context(), Creator{ID: "c1"})
	if err == nil {
		t.Fatalf("LoadStatus(%q) returned nil error, want non-nil", "c1")
	}
	if got, want := status.EventSub, EventSubUnknown; got != want {
		t.Errorf("LoadStatus(%q).EventSub = %q, want %q", "c1", got, want)
	}
	if got, want := status.HasSubscriberCount, false; got != want {
		t.Errorf("LoadStatus(%q).HasSubscriberCount = %t, want %t", "c1", got, want)
	}
}
