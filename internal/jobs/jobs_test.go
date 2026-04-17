package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
)

type fakeStore struct {
	listCreatorsFn     func(ctx context.Context) ([]core.Creator, error)
	activeWithoutGroup func(ctx context.Context, creators []core.Creator) (int, error)
	repairReverseIndex func(ctx context.Context) (int, int, int, int, error)
	listManagedGroups  func(ctx context.Context) ([]core.ManagedGroup, error)
	listUntracked      func(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error)
	removeUntracked    func(ctx context.Context, chatID, telegramUserID int64) error
	addTracked         func(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	countActiveUsers   func(ctx context.Context, since time.Time) (int, error)
	countViewers       func(ctx context.Context) (int, error)
	countCreators      func(ctx context.Context) (int, error)
	countManaged       func(ctx context.Context) (int, error)
}

type fakeMemberCleanupStore struct {
	saveJob func(ctx context.Context, job core.MemberCleanupJob) error
}

type fakeSubscriptionGraceStore struct {
	listDueFn             func(ctx context.Context, now time.Time, limit int64) ([]core.PendingSubscriptionEndGrace, error)
	claimFn               func(ctx context.Context, jobID string, ttl time.Duration) (bool, error)
	deleteFn              func(ctx context.Context, creatorID, twitchUserID string) error
	isSubscriberFn        func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	listManagedGroupsByFn func(ctx context.Context, creatorID string) ([]core.ManagedGroup, error)
	removeTrackedMemberFn func(ctx context.Context, chatID, telegramUserID int64) error
}

type fakeSubscriptionGraceNotifier struct {
	results []core.ExpiredSubscriptionGraceResult
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

func (f *fakeStore) RepairTrackedGroupReverseIndex(ctx context.Context) (indexUsers, repairedUsers, missingLinks, staleLinks int, err error) {
	if f.repairReverseIndex != nil {
		return f.repairReverseIndex(ctx)
	}
	return 0, 0, 0, 0, nil
}

func (f *fakeStore) ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error) {
	if f.listManagedGroups != nil {
		return f.listManagedGroups(ctx)
	}
	return nil, nil
}

func (f *fakeStore) ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
	if f.listUntracked != nil {
		return f.listUntracked(ctx, chatID)
	}
	return nil, nil
}

func (f *fakeStore) RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error {
	if f.removeUntracked != nil {
		return f.removeUntracked(ctx, chatID, telegramUserID)
	}
	return nil
}

func (f *fakeStore) AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error {
	if f.addTracked != nil {
		return f.addTracked(ctx, chatID, telegramUserID, source, at)
	}
	return nil
}

func (f *fakeStore) CountTelegramActiveUsersSince(ctx context.Context, since time.Time) (int, error) {
	if f.countActiveUsers != nil {
		return f.countActiveUsers(ctx, since)
	}
	return 0, nil
}

func (f *fakeStore) PruneTelegramActiveUsersBefore(context.Context, time.Time) error {
	return nil
}

func (f *fakeStore) CountLinkedViewerAccounts(ctx context.Context) (int, error) {
	if f.countViewers != nil {
		return f.countViewers(ctx)
	}
	return 0, nil
}

func (f *fakeStore) CountLinkedCreatorAccounts(ctx context.Context) (int, error) {
	if f.countCreators != nil {
		return f.countCreators(ctx)
	}
	return 0, nil
}

func (f *fakeStore) CountManagedGroups(ctx context.Context) (int, error) {
	if f.countManaged != nil {
		return f.countManaged(ctx)
	}
	return 0, nil
}

func (f *fakeMemberCleanupStore) ListPendingMemberCleanupJobs(context.Context) ([]core.MemberCleanupJob, error) {
	return nil, nil
}

func (f *fakeMemberCleanupStore) ClaimMemberCleanupJob(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (f *fakeMemberCleanupStore) SaveMemberCleanupJob(ctx context.Context, job core.MemberCleanupJob) error {
	if f.saveJob != nil {
		return f.saveJob(ctx, job)
	}
	return nil
}

func (f *fakeSubscriptionGraceStore) ListDueSubscriptionEndGrace(ctx context.Context, now time.Time, limit int64) ([]core.PendingSubscriptionEndGrace, error) {
	if f.listDueFn != nil {
		return f.listDueFn(ctx, now, limit)
	}
	return nil, nil
}

func (f *fakeSubscriptionGraceStore) ClaimSubscriptionEndGrace(ctx context.Context, jobID string, ttl time.Duration) (bool, error) {
	if f.claimFn != nil {
		return f.claimFn(ctx, jobID, ttl)
	}
	return true, nil
}

func (f *fakeSubscriptionGraceStore) DeleteSubscriptionEndGrace(ctx context.Context, creatorID, twitchUserID string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, creatorID, twitchUserID)
	}
	return nil
}

func (f *fakeSubscriptionGraceStore) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isSubscriberFn != nil {
		return f.isSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
}

func (f *fakeSubscriptionGraceStore) ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error) {
	if f.listManagedGroupsByFn != nil {
		return f.listManagedGroupsByFn(ctx, creatorID)
	}
	return nil, nil
}

func (f *fakeSubscriptionGraceStore) RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error {
	if f.removeTrackedMemberFn != nil {
		return f.removeTrackedMemberFn(ctx, chatID, telegramUserID)
	}
	return nil
}

func (f *fakeSubscriptionGraceNotifier) NotifySubscriptionGraceExpired(_ context.Context, result core.ExpiredSubscriptionGraceResult) error {
	f.results = append(f.results, result)
	return nil
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
	mu        sync.Mutex
	lastEvent events.Event
	calls     int
}

type fakeProductMetricsSink struct {
	dailyActive    int
	linkedViewers  int
	linkedCreators int
	managedGroups  int
}

type fakeGroupKicker struct {
	mu    sync.Mutex
	kicks [][2]int64
}

func (f *fakeGroupKicker) KickFromGroup(_ context.Context, groupChatID, telegramUserID int64, _ core.KickReason) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kicks = append(f.kicks, [2]int64{groupChatID, telegramUserID})
	return nil
}

func (f *fakeGroupKicker) snapshot() [][2]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]int64(nil), f.kicks...)
}

func (f *fakeObserver) Emit(_ context.Context, evt events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastEvent = evt
}

func (f *fakeObserver) snapshot() (calls int, evt events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastEvent
}

func (f *fakeProductMetricsSink) TelegramDailyActiveUsers(count int) { f.dailyActive = count }
func (f *fakeProductMetricsSink) LinkedViewerAccounts(count int)     { f.linkedViewers = count }
func (f *fakeProductMetricsSink) LinkedCreatorAccounts(count int)    { f.linkedCreators = count }
func (f *fakeProductMetricsSink) ManagedGroups(count int)            { f.managedGroups = count }

func TestRunScheduledRecordsObserverResult(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	reconcile := &fakeReconciler{result: "partial_failure"}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     NewSubscriberTask(reconcile),
			Interval: 5 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		calls, _ := obs.snapshot()
		if calls > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	calls, evt := obs.snapshot()
	if calls == 0 {
		t.Fatal("expected at least one observer call")
	}
	if evt.Name != events.NameBackgroundJob {
		t.Errorf("Emit() name = %q, want %q", evt.Name, events.NameBackgroundJob)
	}
	if evt.Fields["job"] != "reconcile_subscribers" {
		t.Errorf("Emit() job = %q, want \"reconcile_subscribers\"", evt.Fields["job"])
	}
	if evt.Outcome != "partial_failure" {
		t.Errorf("Emit() outcome = %q, want \"partial_failure\"", evt.Outcome)
	}
	if evt.Duration <= 0 {
		t.Errorf("Emit() duration = %v, want > 0", evt.Duration)
	}
}

func TestProductMetricsSnapshotTaskSyncsCounts(t *testing.T) {
	t.Parallel()

	sink := &fakeProductMetricsSink{}
	task := NewProductMetricsSnapshotTask(&fakeStore{
		countActiveUsers: func(context.Context, time.Time) (int, error) { return 7, nil },
		countViewers:     func(context.Context) (int, error) { return 11, nil },
		countCreators:    func(context.Context) (int, error) { return 3, nil },
		countManaged:     func(context.Context) (int, error) { return 5, nil },
	}, sink)

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if sink.dailyActive != 7 || sink.linkedViewers != 11 || sink.linkedCreators != 3 || sink.managedGroups != 5 {
		t.Fatalf("snapshot sink = %+v, want dau=7 viewers=11 creators=3 groups=5", *sink)
	}
}

func TestRunScheduledStopsOnCancel(t *testing.T) {
	t.Parallel()

	reconcile := &fakeReconciler{result: "ok"}
	runner := NewRunner(nil, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     NewSubscriberTask(reconcile),
			Interval: 5 * time.Millisecond,
		})
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
		t.Fatal("RunScheduled did not stop after cancel")
	}
}

func TestProductMetricsSnapshotTaskSyncsAllGauges(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		countActiveUsers: func(_ context.Context, since time.Time) (int, error) {
			if since.IsZero() {
				t.Fatal("since should be populated")
			}
			return 7, nil
		},
		countViewers:  func(context.Context) (int, error) { return 11, nil },
		countCreators: func(context.Context) (int, error) { return 3, nil },
		countManaged:  func(context.Context) (int, error) { return 5, nil },
	}
	sink := &fakeProductMetricsSink{}
	taskIface := NewProductMetricsSnapshotTask(store, sink)
	task, ok := taskIface.(productMetricsSnapshotTask)
	if !ok {
		t.Fatalf("NewProductMetricsSnapshotTask() type = %T, want productMetricsSnapshotTask", taskIface)
	}
	task.now = func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) }

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sink.dailyActive != 7 || sink.linkedViewers != 11 || sink.linkedCreators != 3 || sink.managedGroups != 5 {
		t.Fatalf("sink = %+v, want daily=7 viewers=11 creators=3 groups=5", sink)
	}
}

func TestIntegrityAuditTaskClassifiesFailureResult(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		listCreatorsFn: func(_ context.Context) ([]core.Creator, error) {
			return nil, errors.New("boom")
		},
	}
	task := NewIntegrityAuditTask(store, nil, nil)

	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if got := task.Classify(err); got != "list_creators_failed" {
		t.Fatalf("Classify() = %q, want %q", got, "list_creators_failed")
	}
}

func TestGracePolicyTaskKicksExpiredUnverifiedMembers(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	var removed [][2]int64
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{
				{ChatID: 100, Policy: core.GroupPolicyGraceWeek},
				{ChatID: 101, Policy: core.GroupPolicyObserve},
			}, nil
		},
		listUntracked: func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
			if chatID != 100 {
				return nil, nil
			}
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 10, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
				{ChatID: 100, TelegramUserID: 11, FirstSeenAt: time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)},
			}, nil
		},
		removeUntracked: func(_ context.Context, chatID, telegramUserID int64) error {
			removed = append(removed, [2]int64{chatID, telegramUserID})
			return nil
		},
	}

	taskIface := NewGracePolicyTask(store, kicker, nil, nil)
	task, ok := taskIface.(gracePolicyTask)
	if !ok {
		t.Fatalf("NewGracePolicyTask() type = %T, want gracePolicyTask", taskIface)
	}
	task.now = func() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := kicker.snapshot(); len(got) != 1 || got[0] != [2]int64{100, 10} {
		t.Fatalf("kicks = %#v, want only expired member", got)
	}
	if len(removed) != 1 || removed[0] != [2]int64{100, 10} {
		t.Fatalf("removed = %#v, want only expired member", removed)
	}
}

func TestGracePolicyTaskContinuesAfterMemberError(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyGraceWeek}}, nil
		},
		listUntracked: func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 10, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
				{ChatID: 100, TelegramUserID: 11, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			}, nil
		},
		removeUntracked: func(_ context.Context, _ int64, telegramUserID int64) error {
			if telegramUserID == 10 {
				return errors.New("cleanup boom")
			}
			return nil
		},
	}

	taskIface := NewGracePolicyTask(store, kicker, nil, nil)
	task, ok := taskIface.(gracePolicyTask)
	if !ok {
		t.Fatalf("NewGracePolicyTask() type = %T, want gracePolicyTask", taskIface)
	}
	task.now = func() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }

	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want partial failure")
	}
	if got := task.Classify(err); got != "partial_failure" {
		t.Fatalf("Classify() = %q, want %q", got, "partial_failure")
	}
	if got := kicker.snapshot(); len(got) != 2 {
		t.Fatalf("kicks = %#v, want both expired members attempted", got)
	}
}

func TestKickPolicyTaskKicksUntrackedMembers(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	var removed [][2]int64
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{
				{ChatID: 100, Policy: core.GroupPolicyKick},
				{ChatID: 101, Policy: core.GroupPolicyObserve},
				{ChatID: 102, Policy: core.GroupPolicyGraceWeek},
			}, nil
		},
		listUntracked: func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
			if chatID != 100 {
				return nil, errors.New("unexpected list for non-kick group")
			}
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 10},
				{ChatID: 100, TelegramUserID: 11},
			}, nil
		},
		removeUntracked: func(_ context.Context, chatID, telegramUserID int64) error {
			removed = append(removed, [2]int64{chatID, telegramUserID})
			return nil
		},
	}

	task := NewKickPolicyTask(store, kicker, nil, nil)
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := kicker.snapshot(); len(got) != 2 || got[0] != [2]int64{100, 10} || got[1] != [2]int64{100, 11} {
		t.Fatalf("kicks = %#v, want both untracked members of chat 100", got)
	}
	if len(removed) != 2 || removed[0] != [2]int64{100, 10} || removed[1] != [2]int64{100, 11} {
		t.Fatalf("removed = %#v, want both untracked members cleaned up", removed)
	}
	if task.Classify(nil) != "ok" {
		t.Fatalf("Classify(nil) = %q, want ok", task.Classify(nil))
	}
}

func TestKickPolicyTaskSkipsGodUsers(t *testing.T) {
	t.Parallel()

	god := core.NewGodAccessChecker(42)
	kicker := &fakeGroupKicker{}
	var trackedAdds []int64
	var removed []int64
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
		},
		listUntracked: func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 42},
				{ChatID: 100, TelegramUserID: 11},
			}, nil
		},
		addTracked: func(_ context.Context, _, telegramUserID int64, _ string, _ time.Time) error {
			trackedAdds = append(trackedAdds, telegramUserID)
			return nil
		},
		removeUntracked: func(_ context.Context, _, telegramUserID int64) error {
			removed = append(removed, telegramUserID)
			return nil
		},
	}

	task := NewKickPolicyTask(store, kicker, god, nil)
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := kicker.snapshot(); len(got) != 1 || got[0] != [2]int64{100, 11} {
		t.Fatalf("kicks = %#v, want only non-god member kicked", got)
	}
	if len(trackedAdds) != 1 || trackedAdds[0] != 42 {
		t.Fatalf("trackedAdds = %#v, want god user promoted to tracked", trackedAdds)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %#v, want both untracked entries cleared", removed)
	}
}

type failingKicker struct {
	fail map[int64]bool
	base fakeGroupKicker
}

func (f *failingKicker) KickFromGroup(ctx context.Context, chatID, telegramUserID int64, reason core.KickReason) error {
	if f.fail[telegramUserID] {
		return errors.New("permission denied")
	}
	return f.base.KickFromGroup(ctx, chatID, telegramUserID, reason)
}

func TestKickPolicyTaskContinuesAndReportsPartialFailure(t *testing.T) {
	t.Parallel()

	kicker := &failingKicker{fail: map[int64]bool{10: true}}
	var removed []int64
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
		},
		listUntracked: func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 10},
				{ChatID: 100, TelegramUserID: 11},
			}, nil
		},
		removeUntracked: func(_ context.Context, _, telegramUserID int64) error {
			removed = append(removed, telegramUserID)
			return nil
		},
	}

	task := NewKickPolicyTask(store, kicker, nil, nil)
	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want partial failure")
	}
	if got := task.Classify(err); got != "partial_failure" {
		t.Fatalf("Classify() = %q, want %q", got, "partial_failure")
	}
	if got := kicker.base.snapshot(); len(got) != 1 || got[0] != [2]int64{100, 11} {
		t.Fatalf("kicks = %#v, want only successful kick recorded", got)
	}
	if len(removed) != 1 || removed[0] != 11 {
		t.Fatalf("removed = %#v, want only successfully kicked member cleaned up (failed one must stay for retry)", removed)
	}
}

func TestKickPolicyTaskListManagedGroupsErrorIsHardFailure(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return nil, errors.New("redis down")
		},
	}

	task := NewKickPolicyTask(store, kicker, nil, nil)
	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if errors.Is(err, core.ErrPartialReconcile) {
		t.Fatalf("Run() error = %v, want a hard failure (not partial)", err)
	}
	if got := task.Classify(err); got != "failed" {
		t.Fatalf("Classify() = %q, want %q", got, "failed")
	}
	if got := kicker.snapshot(); len(got) != 0 {
		t.Fatalf("kicks = %#v, want none", got)
	}
}

func TestKickPolicyTaskListUntrackedErrorIsPartialAndContinuesNextGroup(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{
				{ChatID: 100, Policy: core.GroupPolicyKick},
				{ChatID: 200, Policy: core.GroupPolicyKick},
			}, nil
		},
		listUntracked: func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
			if chatID == 100 {
				return nil, errors.New("list boom")
			}
			return []core.UntrackedGroupMember{{ChatID: 200, TelegramUserID: 22}}, nil
		},
	}

	task := NewKickPolicyTask(store, kicker, nil, nil)
	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want partial failure")
	}
	if got := task.Classify(err); got != "partial_failure" {
		t.Fatalf("Classify() = %q, want %q", got, "partial_failure")
	}
	if got := kicker.snapshot(); len(got) != 1 || got[0] != [2]int64{200, 22} {
		t.Fatalf("kicks = %#v, want only the second group's member kicked", got)
	}
}

func TestKickPolicyTaskGodPathStoreErrorsAreReportedPartial(t *testing.T) {
	t.Parallel()

	god := core.NewGodAccessChecker(42, 43)
	kicker := &fakeGroupKicker{}
	var trackedAdds []int64
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
		},
		listUntracked: func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
			return []core.UntrackedGroupMember{
				{ChatID: 100, TelegramUserID: 42},
				{ChatID: 100, TelegramUserID: 43},
			}, nil
		},
		addTracked: func(_ context.Context, _, telegramUserID int64, _ string, _ time.Time) error {
			trackedAdds = append(trackedAdds, telegramUserID)
			if telegramUserID == 42 {
				return errors.New("track boom")
			}
			return nil
		},
		removeUntracked: func(_ context.Context, _, telegramUserID int64) error {
			if telegramUserID == 43 {
				return errors.New("remove boom")
			}
			return nil
		},
	}

	task := NewKickPolicyTask(store, kicker, god, nil)
	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want partial failure")
	}
	if got := task.Classify(err); got != "partial_failure" {
		t.Fatalf("Classify() = %q, want %q", got, "partial_failure")
	}
	if len(trackedAdds) != 2 {
		t.Fatalf("trackedAdds = %#v, want both god users attempted (first fails, second continues)", trackedAdds)
	}
	if got := kicker.snapshot(); len(got) != 0 {
		t.Fatalf("kicks = %#v, want no god users kicked even on store errors", got)
	}
}

func TestKickPolicyTaskRemoveUntrackedErrorAfterKickIsPartial(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	store := &fakeStore{
		listManagedGroups: func(context.Context) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
		},
		listUntracked: func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
			return []core.UntrackedGroupMember{{ChatID: 100, TelegramUserID: 10}}, nil
		},
		removeUntracked: func(_ context.Context, _, _ int64) error {
			return errors.New("cleanup boom")
		},
	}

	task := NewKickPolicyTask(store, kicker, nil, nil)
	err := task.Run(t.Context())
	if err == nil {
		t.Fatal("Run() error = nil, want partial failure")
	}
	if got := task.Classify(err); got != "partial_failure" {
		t.Fatalf("Classify() = %q, want %q", got, "partial_failure")
	}
	if got := kicker.snapshot(); len(got) != 1 || got[0] != [2]int64{100, 10} {
		t.Fatalf("kicks = %#v, want the member still kicked despite cleanup error", got)
	}
}

func TestMemberCleanupTaskProcessJobCapsWorkPerRun(t *testing.T) {
	t.Parallel()

	var saved core.MemberCleanupJob
	store := &fakeMemberCleanupStore{
		saveJob: func(_ context.Context, job core.MemberCleanupJob) error {
			saved = job
			return nil
		},
	}
	kicker := &fakeGroupKicker{}
	task := memberCleanupTask{
		store:            store,
		kicker:           kicker,
		logger:           slog.Default(),
		lockTTL:          15 * time.Minute,
		maxTargetsPerRun: 2,
	}
	job := core.MemberCleanupJob{
		ID:              "job-1",
		Kind:            core.MemberCleanupKindGroupUnregistration,
		Status:          core.MemberCleanupStatusPending,
		OwnerTelegramID: 1,
		CreatorLogin:    "creator",
		GroupName:       "group",
		TotalTargets:    3,
		Targets: []core.MemberCleanupTarget{
			{ChatID: 100, TelegramUserID: 10, MaxAttempts: 3},
			{ChatID: 100, TelegramUserID: 11, MaxAttempts: 3},
			{ChatID: 100, TelegramUserID: 12, MaxAttempts: 3},
		},
	}

	result, done, err := task.processJob(t.Context(), job)
	if err != nil {
		t.Fatalf("processJob() error = %v", err)
	}
	if done {
		t.Fatal("processJob() done = true, want false with leftover targets")
	}
	if got := kicker.snapshot(); len(got) != 2 {
		t.Fatalf("kicks = %#v, want 2 attempts", got)
	}
	if saved.SucceededCount != 2 {
		t.Fatalf("saved succeeded = %d, want 2", saved.SucceededCount)
	}
	if len(saved.Targets) != 1 || saved.Targets[0].TelegramUserID != 12 {
		t.Fatalf("saved remaining targets = %#v, want only user 12", saved.Targets)
	}
	if result.SucceededCount != 2 {
		t.Fatalf("result succeeded = %d, want 2", result.SucceededCount)
	}
	if result.FailedCount != 1 {
		t.Fatalf("result failed = %d, want 1 remaining target", result.FailedCount)
	}
}

func TestSubscriptionGraceTaskEnforcesDueJobs(t *testing.T) {
	t.Parallel()

	deleted := 0
	removed := 0
	store := &fakeSubscriptionGraceStore{
		listDueFn: func(_ context.Context, now time.Time, limit int64) ([]core.PendingSubscriptionEndGrace, error) {
			return []core.PendingSubscriptionEndGrace{{
				ID:             "c1:u1",
				CreatorID:      "c1",
				CreatorLogin:   "creator",
				TwitchUserID:   "u1",
				TelegramUserID: 7,
				ViewerLogin:    "viewer",
				Language:       "it",
				DueAt:          now.Add(-time.Minute),
			}}, nil
		},
		deleteFn: func(_ context.Context, creatorID, twitchUserID string) error {
			if creatorID != "c1" || twitchUserID != "u1" {
				t.Fatalf("DeleteSubscriptionEndGrace() = (%q, %q), want (c1, u1)", creatorID, twitchUserID)
			}
			deleted++
			return nil
		},
		listManagedGroupsByFn: func(_ context.Context, creatorID string) ([]core.ManagedGroup, error) {
			return []core.ManagedGroup{{ChatID: 100, CreatorID: creatorID}, {ChatID: 101, CreatorID: creatorID}}, nil
		},
		removeTrackedMemberFn: func(_ context.Context, chatID, telegramUserID int64) error {
			removed++
			return nil
		},
	}
	kicker := &fakeGroupKicker{}
	notifier := &fakeSubscriptionGraceNotifier{}
	task := NewSubscriptionGraceTask(store, kicker, notifier, nil, nil)

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := kicker.snapshot(); len(got) != 2 {
		t.Fatalf("kicks = %#v, want 2 group kicks", got)
	}
	if removed != 2 {
		t.Fatalf("removed tracked members = %d, want 2", removed)
	}
	if deleted != 1 {
		t.Fatalf("deleted jobs = %d, want 1", deleted)
	}
	if len(notifier.results) != 1 || notifier.results[0].TelegramUserID != 7 {
		t.Fatalf("notifications = %+v, want one result for telegram user 7", notifier.results)
	}
}
