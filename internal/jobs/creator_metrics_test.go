package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"imsub/internal/core"
)

type creatorMetricsStoreStub struct {
	listCreatorsFn        func(context.Context) ([]core.Creator, error)
	listManagedGroupsFn   func(context.Context, string) ([]core.ManagedGroup, error)
	subscriberCountFn     func(context.Context, string) (int64, error)
	blockedUserCountFn    func(context.Context, string) (int64, error)
	trackedGroupCountFn   func(context.Context, int64) (int, error)
	untrackedGroupCountFn func(context.Context, int64) (int, error)
}

func (s creatorMetricsStoreStub) ListCreators(ctx context.Context) ([]core.Creator, error) {
	return s.listCreatorsFn(ctx)
}

func (s creatorMetricsStoreStub) ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error) {
	return s.listManagedGroupsFn(ctx, creatorID)
}

func (s creatorMetricsStoreStub) CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error) {
	return s.subscriberCountFn(ctx, creatorID)
}

func (s creatorMetricsStoreStub) CreatorBlockedUserCount(ctx context.Context, creatorID string) (int64, error) {
	return s.blockedUserCountFn(ctx, creatorID)
}

func (s creatorMetricsStoreStub) CountTrackedGroupMembers(ctx context.Context, chatID int64) (int, error) {
	return s.trackedGroupCountFn(ctx, chatID)
}

func (s creatorMetricsStoreStub) CountUntrackedGroupMembers(ctx context.Context, chatID int64) (int, error) {
	return s.untrackedGroupCountFn(ctx, chatID)
}

type creatorSnapshot struct {
	displayName string
	login       string
	managed     int
	subscribers int
	blocked     int
	tracked     int
	untracked   int
	reconnect   bool
}

type creatorMetricsSinkStub struct {
	resetCalls int
	snapshots  map[string]creatorSnapshot
}

func (s *creatorMetricsSinkStub) ResetCreatorSnapshotMetrics() {
	s.resetCalls++
	s.snapshots = map[string]creatorSnapshot{}
}

func (s *creatorMetricsSinkStub) snapshotFor(creatorID string) creatorSnapshot {
	if s.snapshots == nil {
		s.snapshots = map[string]creatorSnapshot{}
	}
	return s.snapshots[creatorID]
}

func (s *creatorMetricsSinkStub) store(creatorID string, snapshot creatorSnapshot) {
	if s.snapshots == nil {
		s.snapshots = map[string]creatorSnapshot{}
	}
	s.snapshots[creatorID] = snapshot
}

func (s *creatorMetricsSinkStub) CreatorInfo(creatorID, displayName, login string) {
	snap := s.snapshotFor(creatorID)
	snap.displayName = displayName
	snap.login = login
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorManagedGroups(creatorID string, count int) {
	snap := s.snapshotFor(creatorID)
	snap.managed = count
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorSubscribers(creatorID string, count int) {
	snap := s.snapshotFor(creatorID)
	snap.subscribers = count
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorBlockedUsers(creatorID string, count int) {
	snap := s.snapshotFor(creatorID)
	snap.blocked = count
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorTrackedMembers(creatorID string, count int) {
	snap := s.snapshotFor(creatorID)
	snap.tracked = count
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorUntrackedMembers(creatorID string, count int) {
	snap := s.snapshotFor(creatorID)
	snap.untracked = count
	s.store(creatorID, snap)
}

func (s *creatorMetricsSinkStub) CreatorReconnectRequired(creatorID string, required bool) {
	snap := s.snapshotFor(creatorID)
	snap.reconnect = required
	s.store(creatorID, snap)
}

func TestCreatorMetricsTaskAggregatesCreators(t *testing.T) {
	t.Parallel()

	sink := &creatorMetricsSinkStub{}
	task := creatorMetricsTask{
		store: creatorMetricsStoreStub{
			listCreatorsFn: func(context.Context) ([]core.Creator, error) {
				return []core.Creator{
					{ID: "c1", TwitchDisplayName: "Alpha", TwitchLogin: "alpha"},
					{ID: "c2", TwitchDisplayName: "Beta", TwitchLogin: "beta", AuthStatus: core.CreatorAuthReconnectRequired},
				}, nil
			},
			listManagedGroupsFn: func(_ context.Context, creatorID string) ([]core.ManagedGroup, error) {
				switch creatorID {
				case "c1":
					return []core.ManagedGroup{{ChatID: 101}, {ChatID: 102}}, nil
				case "c2":
					return nil, nil
				default:
					return nil, nil
				}
			},
			subscriberCountFn: func(_ context.Context, creatorID string) (int64, error) {
				if creatorID == "c1" {
					return 11, nil
				}
				return 5, nil
			},
			blockedUserCountFn: func(_ context.Context, creatorID string) (int64, error) {
				if creatorID == "c1" {
					return 2, nil
				}
				return 0, nil
			},
			trackedGroupCountFn: func(_ context.Context, chatID int64) (int, error) {
				if chatID == 101 {
					return 3, nil
				}
				return 4, nil
			},
			untrackedGroupCountFn: func(_ context.Context, chatID int64) (int, error) {
				if chatID == 101 {
					return 1, nil
				}
				return 2, nil
			},
		},
		sink:   sink,
		logger: slog.New(slog.DiscardHandler),
	}

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sink.resetCalls != 1 {
		t.Fatalf("ResetCreatorSnapshotMetrics() calls = %d, want 1", sink.resetCalls)
	}

	gotC1 := sink.snapshots["c1"]
	if gotC1.displayName != "Alpha" || gotC1.login != "alpha" || gotC1.managed != 2 || gotC1.subscribers != 11 || gotC1.blocked != 2 || gotC1.tracked != 7 || gotC1.untracked != 3 || gotC1.reconnect {
		t.Fatalf("c1 snapshot = %+v", gotC1)
	}

	gotC2 := sink.snapshots["c2"]
	if gotC2.displayName != "Beta" || gotC2.login != "beta" || gotC2.managed != 0 || gotC2.subscribers != 5 || gotC2.blocked != 0 || gotC2.tracked != 0 || gotC2.untracked != 0 || !gotC2.reconnect {
		t.Fatalf("c2 snapshot = %+v", gotC2)
	}
}

func TestCreatorMetricsTaskContinuesOnCreatorError(t *testing.T) {
	t.Parallel()

	sink := &creatorMetricsSinkStub{}
	task := creatorMetricsTask{
		store: creatorMetricsStoreStub{
			listCreatorsFn: func(context.Context) ([]core.Creator, error) {
				return []core.Creator{
					{ID: "bad", TwitchDisplayName: "Bad", TwitchLogin: "bad"},
					{ID: "good", TwitchDisplayName: "Good", TwitchLogin: "good"},
				}, nil
			},
			listManagedGroupsFn: func(_ context.Context, creatorID string) ([]core.ManagedGroup, error) {
				if creatorID == "bad" {
					return nil, errors.New("boom")
				}
				return []core.ManagedGroup{{ChatID: 201}}, nil
			},
			subscriberCountFn:     func(context.Context, string) (int64, error) { return 2, nil },
			blockedUserCountFn:    func(context.Context, string) (int64, error) { return 1, nil },
			trackedGroupCountFn:   func(context.Context, int64) (int, error) { return 3, nil },
			untrackedGroupCountFn: func(context.Context, int64) (int, error) { return 4, nil },
		},
		sink:   sink,
		logger: slog.New(slog.DiscardHandler),
	}

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, ok := sink.snapshots["bad"]; !ok {
		t.Fatalf("expected info snapshot for bad creator")
	}
	if got := sink.snapshots["good"]; got.managed != 1 || got.tracked != 3 || got.untracked != 4 {
		t.Fatalf("good snapshot = %+v", got)
	}
}
