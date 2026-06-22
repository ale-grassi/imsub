package jobs

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
)

type fakeStore struct {
	listCreatorsFn     func(ctx context.Context) ([]core.Creator, error)
	creatorFn          func(ctx context.Context, creatorID string) (core.Creator, bool, error)
	activeWithoutGroup func(ctx context.Context, creators []core.Creator) (int, error)
	repairReverseIndex func(ctx context.Context) (int, int, int, int, error)
	listManagedGroups  func(ctx context.Context) ([]core.ManagedGroup, error)
	listUntracked      func(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error)
	removeUntracked    func(ctx context.Context, chatID, telegramUserID int64) error
	addTracked         func(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	userIdentityFn     func(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error)
	isSubscriberFn     func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	isBlockedFn        func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	countActiveUsers   func(ctx context.Context, since time.Time) (int, error)
	countViewers       func(ctx context.Context) (int, error)
	countCreators      func(ctx context.Context) (int, error)
	countManaged       func(ctx context.Context) (int, error)
}

// newGracePolicyFakeStore returns a fakeStore preconfigured with the minimal
// defaults that the grace/kick policy tasks expect (creator lookups succeed).
// Individual tests override the hooks they care about.
func newGracePolicyFakeStore() *fakeStore {
	return &fakeStore{
		creatorFn: func(_ context.Context, creatorID string) (core.Creator, bool, error) {
			return core.Creator{ID: creatorID}, true, nil
		},
	}
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

func (f *fakeStore) Creator(ctx context.Context, creatorID string) (core.Creator, bool, error) {
	if f.creatorFn != nil {
		return f.creatorFn(ctx, creatorID)
	}
	return core.Creator{}, false, nil
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

func (f *fakeStore) UserIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
	if f.userIdentityFn != nil {
		return f.userIdentityFn(ctx, telegramUserID)
	}
	return core.UserIdentity{}, false, nil
}

func (f *fakeStore) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isSubscriberFn != nil {
		return f.isSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
}

func (f *fakeStore) IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isBlockedFn != nil {
		return f.isBlockedFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
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
	events    []events.Event
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
	f.events = append(f.events, evt)
}

func (f *fakeObserver) snapshot() (calls int, evt events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastEvent
}

func (f *fakeObserver) all() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]events.Event(nil), f.events...)
}

func assertRunIDPresent(t *testing.T, evt events.Event) {
	t.Helper()
	if evt.Fields["run_id"] == "" {
		t.Fatalf("%s: run_id missing, fields=%+v", evt.Name, evt.Fields)
	}
}

func assertRunTimestamps(t *testing.T, evt events.Event, wantStart, wantFinish bool) {
	t.Helper()
	if wantStart && evt.Fields["started_at_unix_ms"] == "" {
		t.Fatalf("%s: started_at_unix_ms missing, fields=%+v", evt.Name, evt.Fields)
	}
	if wantFinish && evt.Fields["finished_at_unix_ms"] == "" {
		t.Fatalf("%s: finished_at_unix_ms missing, fields=%+v", evt.Name, evt.Fields)
	}
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
	if evt.Fields["job"] != taskNameReconcileSubs {
		t.Errorf("Emit() job = %q, want %q", evt.Fields["job"], taskNameReconcileSubs)
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{
			{ChatID: 100, Policy: core.GroupPolicyGraceWeek},
			{ChatID: 101, Policy: core.GroupPolicyObserve},
		}, nil
	}
	store.listUntracked = func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
		if chatID != 100 {
			return nil, nil
		}
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			{ChatID: 100, TelegramUserID: 11, FirstSeenAt: time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)},
		}, nil
	}
	store.removeUntracked = func(_ context.Context, chatID, telegramUserID int64) error {
		removed = append(removed, [2]int64{chatID, telegramUserID})
		return nil
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyGraceWeek}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			{ChatID: 100, TelegramUserID: 11, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		}, nil
	}
	store.removeUntracked = func(_ context.Context, _ int64, telegramUserID int64) error {
		if telegramUserID == 10 {
			return errors.New("cleanup boom")
		}
		return nil
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

type trackedAdd struct {
	chatID int64
	userID int64
	source string
}

func TestGracePolicyTaskRescuesEligibleExistingMembers(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	var trackedAdds []trackedAdd
	var removed [][2]int64
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, CreatorID: "c1", Policy: core.GroupPolicyGraceWeek}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10, FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		}, nil
	}
	store.userIdentityFn = func(_ context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
		return core.UserIdentity{TelegramUserID: telegramUserID, TwitchUserID: "tw-10"}, true, nil
	}
	store.isSubscriberFn = func(_ context.Context, creatorID, twitchUserID string) (bool, error) {
		return creatorID == "c1" && twitchUserID == "tw-10", nil
	}
	store.addTracked = func(_ context.Context, chatID, telegramUserID int64, source string, _ time.Time) error {
		trackedAdds = append(trackedAdds, trackedAdd{chatID: chatID, userID: telegramUserID, source: source})
		return nil
	}
	store.removeUntracked = func(_ context.Context, chatID, telegramUserID int64) error {
		removed = append(removed, [2]int64{chatID, telegramUserID})
		return nil
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
	if got := kicker.snapshot(); len(got) != 0 {
		t.Fatalf("kicks = %#v, want no kick for rescued member", got)
	}
	if len(trackedAdds) != 1 || trackedAdds[0].chatID != 100 || trackedAdds[0].userID != 10 || trackedAdds[0].source != core.SourceGracePolicyRescue {
		t.Fatalf("trackedAdds = %#v, want one grace_policy_rescue add", trackedAdds)
	}
	if len(removed) != 1 || removed[0] != [2]int64{100, 10} {
		t.Fatalf("removed = %#v, want rescued member cleanup", removed)
	}
	if report := task.Report(); report["rescued"] != 1 || report["removed"] != 1 {
		t.Fatalf("report = %#v, want rescued=1 removed=1", report)
	}
}

func TestKickPolicyTaskRescuesEligibleExistingMembers(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	var trackedAdds []trackedAdd
	var removed [][2]int64
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, CreatorID: "c1", Policy: core.GroupPolicyKick}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10},
		}, nil
	}
	store.userIdentityFn = func(_ context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
		return core.UserIdentity{TelegramUserID: telegramUserID, TwitchUserID: "tw-10"}, true, nil
	}
	store.isSubscriberFn = func(_ context.Context, creatorID, twitchUserID string) (bool, error) {
		return creatorID == "c1" && twitchUserID == "tw-10", nil
	}
	store.addTracked = func(_ context.Context, chatID, telegramUserID int64, source string, _ time.Time) error {
		trackedAdds = append(trackedAdds, trackedAdd{chatID: chatID, userID: telegramUserID, source: source})
		return nil
	}
	store.removeUntracked = func(_ context.Context, chatID, telegramUserID int64) error {
		removed = append(removed, [2]int64{chatID, telegramUserID})
		return nil
	}

	taskIface := NewKickPolicyTask(store, kicker, nil, nil)
	task, ok := taskIface.(kickPolicyTask)
	if !ok {
		t.Fatalf("NewKickPolicyTask() type = %T, want kickPolicyTask", taskIface)
	}

	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := kicker.snapshot(); len(got) != 0 {
		t.Fatalf("kicks = %#v, want no kick for rescued member", got)
	}
	if len(trackedAdds) != 1 || trackedAdds[0].chatID != 100 || trackedAdds[0].userID != 10 || trackedAdds[0].source != core.SourceKickPolicyRescue {
		t.Fatalf("trackedAdds = %#v, want one kick_policy_rescue add", trackedAdds)
	}
	if len(removed) != 1 || removed[0] != [2]int64{100, 10} {
		t.Fatalf("removed = %#v, want rescued member cleanup", removed)
	}
	if report := task.Report(); report["rescued"] != 1 || report["removed"] != 1 {
		t.Fatalf("report = %#v, want rescued=1 removed=1", report)
	}
}

func TestKickPolicyTaskKicksUntrackedMembers(t *testing.T) {
	t.Parallel()

	kicker := &fakeGroupKicker{}
	var removed [][2]int64
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{
			{ChatID: 100, Policy: core.GroupPolicyKick},
			{ChatID: 101, Policy: core.GroupPolicyObserve},
			{ChatID: 102, Policy: core.GroupPolicyGraceWeek},
		}, nil
	}
	store.listUntracked = func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
		if chatID != 100 {
			return nil, errors.New("unexpected list for non-kick group")
		}
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10},
			{ChatID: 100, TelegramUserID: 11},
		}, nil
	}
	store.removeUntracked = func(_ context.Context, chatID, telegramUserID int64) error {
		removed = append(removed, [2]int64{chatID, telegramUserID})
		return nil
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 42},
			{ChatID: 100, TelegramUserID: 11},
		}, nil
	}
	store.addTracked = func(_ context.Context, _, telegramUserID int64, _ string, _ time.Time) error {
		trackedAdds = append(trackedAdds, telegramUserID)
		return nil
	}
	store.removeUntracked = func(_ context.Context, _, telegramUserID int64) error {
		removed = append(removed, telegramUserID)
		return nil
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{
			{ChatID: 100, TelegramUserID: 10},
			{ChatID: 100, TelegramUserID: 11},
		}, nil
	}
	store.removeUntracked = func(_ context.Context, _, telegramUserID int64) error {
		removed = append(removed, telegramUserID)
		return nil
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{
			{ChatID: 100, Policy: core.GroupPolicyKick},
			{ChatID: 200, Policy: core.GroupPolicyKick},
		}, nil
	}
	store.listUntracked = func(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
		if chatID == 100 {
			return nil, errors.New("list boom")
		}
		return []core.UntrackedGroupMember{{ChatID: 200, TelegramUserID: 22}}, nil
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
	store := newGracePolicyFakeStore()
	store.listManagedGroups = func(context.Context) ([]core.ManagedGroup, error) {
		return []core.ManagedGroup{{ChatID: 100, Policy: core.GroupPolicyKick}}, nil
	}
	store.listUntracked = func(_ context.Context, _ int64) ([]core.UntrackedGroupMember, error) {
		return []core.UntrackedGroupMember{{ChatID: 100, TelegramUserID: 10}}, nil
	}
	store.removeUntracked = func(_ context.Context, _, _ int64) error {
		return errors.New("cleanup boom")
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

type slowTask struct {
	sleep  time.Duration
	called bool
}

func (s *slowTask) Name() string { return "slow_task" }
func (s *slowTask) Run(ctx context.Context) error {
	s.called = true
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.sleep):
		return nil
	}
}
func (s *slowTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}

type panicTask struct{}

func (panicTask) Name() string { return "panic_task" }
func (panicTask) Run(context.Context) error {
	panic("boom")
}
func (panicTask) Classify(error) string { return taskResultFailed }

func TestRunScheduledWrapsTimeout(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	task := &slowTask{sleep: 200 * time.Millisecond}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     task,
			Interval: 500 * time.Millisecond,
			Timeout:  10 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		foundFinish := false
		for _, evt := range obs.all() {
			if evt.Name == events.NameBackgroundJob {
				foundFinish = true
				break
			}
		}
		if foundFinish {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	var finish events.Event
	for _, e := range obs.all() {
		if e.Name == events.NameBackgroundJob {
			finish = e
			break
		}
	}
	if finish.Name == "" {
		t.Fatalf("no background_job finish event, events=%+v", obs.all())
	}
	if finish.Outcome != "timeout" {
		t.Errorf("finish.Outcome = %q, want %q", finish.Outcome, "timeout")
	}
	assertRunIDPresent(t, finish)
	assertRunTimestamps(t, finish, true, true)
}

func TestRunScheduledRecoversTaskPanic(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     panicTask{},
			Interval: 500 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, evt := range obs.all() {
			if evt.Name == events.NameBackgroundJob {
				cancel()
				<-done
				if evt.Outcome != taskResultPanic {
					t.Fatalf("panic task outcome = %q, want %q", evt.Outcome, taskResultPanic)
				}
				assertRunIDPresent(t, evt)
				assertRunTimestamps(t, evt, true, true)
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("no background_job finish event, events=%+v", obs.all())
}

func TestRunScheduledEmitsStartEvent(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	reconcile := &fakeReconciler{result: "ok"}
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
		if calls, _ := obs.snapshot(); calls >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	evts := obs.all()
	if len(evts) < 3 {
		t.Fatalf("events = %d, want >= 3", len(evts))
	}
	if evts[0].Name != events.NameBackgroundJobSchedule {
		t.Errorf("events[0].Name = %q, want %q", evts[0].Name, events.NameBackgroundJobSchedule)
	}
	if evts[1].Name != events.NameBackgroundJobStarted {
		t.Errorf("events[1].Name = %q, want %q", evts[1].Name, events.NameBackgroundJobStarted)
	}
	if evts[2].Name != events.NameBackgroundJob {
		t.Errorf("events[2].Name = %q, want %q", evts[2].Name, events.NameBackgroundJob)
	}
	if evts[0].Fields["job"] == "" || evts[0].Fields["interval_seconds"] == "" {
		t.Fatalf("schedule event missing fields: %+v", evts[0])
	}
	assertRunIDPresent(t, evts[1])
	if evts[1].Fields["run_id"] != evts[2].Fields["run_id"] {
		t.Fatalf("start/finish run_id mismatch: start=%q finish=%q", evts[1].Fields["run_id"], evts[2].Fields["run_id"])
	}
	assertRunTimestamps(t, evts[1], true, false)
	assertRunTimestamps(t, evts[2], true, true)
}

func TestRunScheduledEmitsScheduleEvent(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	reconcile := &fakeReconciler{result: "ok"}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:         NewSubscriberTask(reconcile),
			InitialDelay: 20 * time.Millisecond,
			Interval:     5 * time.Minute,
			Timeout:      30 * time.Second,
		})
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(obs.all()) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	evts := obs.all()
	if len(evts) == 0 {
		t.Fatal("no events emitted")
	}
	if evts[0].Name != events.NameBackgroundJobSchedule {
		t.Fatalf("events[0].Name = %q, want %q", evts[0].Name, events.NameBackgroundJobSchedule)
	}
	if evts[0].Fields["job"] != taskNameReconcileSubs {
		t.Fatalf("schedule job = %q, want %q", evts[0].Fields["job"], taskNameReconcileSubs)
	}
	if evts[0].Fields["interval_seconds"] != "300" {
		t.Fatalf("schedule interval_seconds = %q, want %q", evts[0].Fields["interval_seconds"], "300")
	}
	if evts[0].Fields["timeout_seconds"] != "30" {
		t.Fatalf("schedule timeout_seconds = %q, want %q", evts[0].Fields["timeout_seconds"], "30")
	}
}

type reporterTask struct {
	items map[string]int
}

func (r *reporterTask) Name() string              { return "reporter_task" }
func (r *reporterTask) Run(context.Context) error { return nil }
func (r *reporterTask) Classify(error) string     { return "ok" }
func (r *reporterTask) Report() map[string]int {
	out := make(map[string]int, len(r.items))
	maps.Copy(out, r.items)
	return out
}

func TestRunScheduledEmitsItemsFromReporter(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	task := &reporterTask{items: map[string]int{"processed": 3, "failed": 1}}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     task,
			Interval: 500 * time.Millisecond,
		})
	}()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls, _ := obs.snapshot(); calls >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	var finish events.Event
	itemCounts := map[string]events.Event{}
	for _, e := range obs.all() {
		switch e.Name {
		case events.NameBackgroundJob:
			if finish.Name == "" {
				finish = e
			}
		case events.NameBackgroundJobItems:
			if _, seen := itemCounts[e.Fields["kind"]]; !seen {
				itemCounts[e.Fields["kind"]] = e
			}
		}
	}
	if finish.Fields["items_processed"] != "3" {
		t.Errorf("finish.Fields[items_processed] = %q, want %q", finish.Fields["items_processed"], "3")
	}
	if finish.Fields["items_failed"] != "1" {
		t.Errorf("finish.Fields[items_failed] = %q, want %q", finish.Fields["items_failed"], "1")
	}
	assertRunIDPresent(t, finish)
	assertRunTimestamps(t, finish, true, true)
	if got := itemCounts["processed"]; got.Count != 3 || got.Outcome != "ok" {
		t.Errorf("items[processed] = %+v, want count=3 outcome=ok", got)
	}
	if got := itemCounts["failed"]; got.Count != 1 || got.Outcome != "ok" {
		t.Errorf("items[failed] = %+v, want count=1 outcome=ok", got)
	}
	if got := itemCounts["processed"]; got.Fields["run_id"] != finish.Fields["run_id"] {
		t.Errorf("items[processed].run_id = %q, want %q", got.Fields["run_id"], finish.Fields["run_id"])
	}
	if got := itemCounts["failed"]; got.Fields["run_id"] != finish.Fields["run_id"] {
		t.Errorf("items[failed].run_id = %q, want %q", got.Fields["run_id"], finish.Fields["run_id"])
	}
}

func TestRunScheduledAssignsUniqueRunIDPerExecution(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	reconcile := &fakeReconciler{result: "ok"}
	runner := NewRunner(nil, obs)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.RunScheduled(ctx, Schedule{
			Task:     NewSubscriberTask(reconcile),
			Interval: 10 * time.Millisecond,
		})
	}()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		runIDs := map[string]struct{}{}
		for _, evt := range obs.all() {
			if evt.Name != events.NameBackgroundJob {
				continue
			}
			if evt.Fields["run_id"] != "" {
				runIDs[evt.Fields["run_id"]] = struct{}{}
			}
		}
		if len(runIDs) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	runIDs := map[string]struct{}{}
	for _, evt := range obs.all() {
		if evt.Name != events.NameBackgroundJob {
			continue
		}
		runID := evt.Fields["run_id"]
		if runID == "" {
			t.Fatalf("background_job event missing run_id: %+v", evt)
		}
		runIDs[runID] = struct{}{}
	}
	if len(runIDs) < 2 {
		t.Fatalf("unique run_ids = %d, want at least 2; events=%+v", len(runIDs), obs.all())
	}
}

type memberTagSyncerStub struct {
	counts core.MemberTagSyncCounts
	err    error
}

func (s memberTagSyncerStub) SyncEnabledGroups(context.Context) (core.MemberTagSyncCounts, error) {
	return s.counts, s.err
}

func TestMemberTagSyncTaskReportsDiagnosticCounts(t *testing.T) {
	t.Parallel()

	task := NewMemberTagSyncTask(memberTagSyncerStub{counts: core.MemberTagSyncCounts{
		Groups:                    1,
		Set:                       2,
		Cleared:                   3,
		Noop:                      4,
		Errors:                    5,
		TrackedStored:             6,
		UntrackedStored:           7,
		DesiredTracked:            8,
		DesiredUntracked:          9,
		ExistingTags:              10,
		SnapshotMembers:           11,
		SnapshotFilteredTracked:   12,
		SnapshotFilteredUntracked: 13,
	}})
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	reporter, ok := task.(Reporter)
	if !ok {
		t.Fatal("NewMemberTagSyncTask() does not implement Reporter")
	}
	report := reporter.Report()
	want := map[string]int{
		"groups":                      1,
		"set":                         2,
		"cleared":                     3,
		"noop":                        4,
		"errors":                      5,
		"tracked_stored":              6,
		"untracked_stored":            7,
		"desired_tracked":             8,
		"desired_untracked":           9,
		"existing_tags":               10,
		"snapshot_members":            11,
		"snapshot_filtered_tracked":   12,
		"snapshot_filtered_untracked": 13,
	}
	for kind, n := range want {
		if report[kind] != n {
			t.Fatalf("report[%q] = %d, want %d in %+v", kind, report[kind], n, report)
		}
	}
}
