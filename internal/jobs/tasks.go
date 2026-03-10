package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
)

const taskResultFailed = "failed"

type subscriberReconciler interface {
	ReconcileSubscribersOnce(ctx context.Context) error
}

type eventSubReconciler interface {
	ReconcileEventSubsOnce(ctx context.Context) error
}

type gracePolicyStore interface {
	ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error)
	ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error)
	RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

type groupKicker interface {
	KickFromGroup(ctx context.Context, groupChatID, telegramUserID int64, reason core.KickReason) error
}

type memberCleanupStore interface {
	ListPendingMemberCleanupJobs(ctx context.Context) ([]core.MemberCleanupJob, error)
	ClaimMemberCleanupJob(ctx context.Context, jobID string, ttl time.Duration) (bool, error)
	SaveMemberCleanupJob(ctx context.Context, job core.MemberCleanupJob) error
}

type memberCleanupNotifier interface {
	NotifyMemberCleanupComplete(ctx context.Context, result core.MemberCleanupResult) error
}

type subscriptionGraceStore interface {
	ListDueSubscriptionEndGrace(ctx context.Context, now time.Time, limit int64) ([]core.PendingSubscriptionEndGrace, error)
	ClaimSubscriptionEndGrace(ctx context.Context, jobID string, ttl time.Duration) (bool, error)
	DeleteSubscriptionEndGrace(ctx context.Context, creatorID, twitchUserID string) error
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error)
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

type subscriptionGraceNotifier interface {
	NotifySubscriptionGraceExpired(ctx context.Context, result core.ExpiredSubscriptionGraceResult) error
}

type productMetricsSnapshotStore interface {
	PruneTelegramActiveUsersBefore(ctx context.Context, before time.Time) error
	CountTelegramActiveUsersSince(ctx context.Context, since time.Time) (int, error)
	CountLinkedViewerAccounts(ctx context.Context) (int, error)
	CountLinkedCreatorAccounts(ctx context.Context) (int, error)
	CountManagedGroups(ctx context.Context) (int, error)
}

type productMetricsSink interface {
	TelegramDailyActiveUsers(count int)
	LinkedViewerAccounts(count int)
	LinkedCreatorAccounts(count int)
	ManagedGroups(count int)
}

type subscriberTask struct {
	reconciler subscriberReconciler
}

// NewSubscriberTask builds the subscriber-cache reconciliation task.
func NewSubscriberTask(r subscriberReconciler) Task {
	return subscriberTask{reconciler: r}
}

func (t subscriberTask) Name() string { return "reconcile_subscribers" }

func (t subscriberTask) Run(ctx context.Context) error {
	if t.reconciler == nil {
		return nil
	}
	if err := t.reconciler.ReconcileSubscribersOnce(ctx); err != nil {
		return fmt.Errorf("reconcile subscribers once: %w", err)
	}
	return nil
}

func (t subscriberTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrListActiveCreators):
		return "list_active_creators_failed"
	case errors.Is(err, core.ErrPartialReconcile):
		return "partial_failure"
	default:
		return taskResultFailed
	}
}

type eventSubTask struct {
	reconciler eventSubReconciler
}

type gracePolicyTask struct {
	store      gracePolicyStore
	kicker     groupKicker
	logger     *slog.Logger
	now        func() time.Time
	graceAfter time.Duration
}

type memberCleanupTask struct {
	store            memberCleanupStore
	kicker           groupKicker
	notifier         memberCleanupNotifier
	logger           *slog.Logger
	lockTTL          time.Duration
	maxTargetsPerRun int
}

type subscriptionGraceTask struct {
	store    subscriptionGraceStore
	kicker   groupKicker
	notifier subscriptionGraceNotifier
	logger   *slog.Logger
	lockTTL  time.Duration
	maxJobs  int64
	now      func() time.Time
}

type productMetricsSnapshotTask struct {
	store  productMetricsSnapshotStore
	sink   productMetricsSink
	now    func() time.Time
	window time.Duration
}

// NewEventSubTask builds the EventSub reconciliation task.
func NewEventSubTask(r eventSubReconciler) Task {
	return eventSubTask{reconciler: r}
}

// NewGracePolicyTask builds the periodic enforcement task for grace-period
// unverified-member policies.
func NewGracePolicyTask(store gracePolicyStore, kicker groupKicker, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return gracePolicyTask{
		store:      store,
		kicker:     kicker,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
		graceAfter: 7 * 24 * time.Hour,
	}
}

// NewMemberCleanupTask builds the periodic task that drains background tracked-member cleanup jobs.
func NewMemberCleanupTask(store memberCleanupStore, kicker groupKicker, notifier memberCleanupNotifier, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return memberCleanupTask{
		store:            store,
		kicker:           kicker,
		notifier:         notifier,
		logger:           logger,
		lockTTL:          15 * time.Minute,
		maxTargetsPerRun: 50,
	}
}

// NewSubscriptionGraceTask builds the periodic sweep that enforces delayed
// subscription-end removals.
func NewSubscriptionGraceTask(store subscriptionGraceStore, kicker groupKicker, notifier subscriptionGraceNotifier, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return subscriptionGraceTask{
		store:    store,
		kicker:   kicker,
		notifier: notifier,
		logger:   logger,
		lockTTL:  10 * time.Minute,
		maxJobs:  100,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// NewProductMetricsSnapshotTask builds the periodic sync for product snapshot gauges.
func NewProductMetricsSnapshotTask(store productMetricsSnapshotStore, sink productMetricsSink) Task {
	return productMetricsSnapshotTask{
		store:  store,
		sink:   sink,
		now:    func() time.Time { return time.Now().UTC() },
		window: 24 * time.Hour,
	}
}

func (t eventSubTask) Name() string { return "reconcile_eventsubs" }

func (t eventSubTask) Run(ctx context.Context) error {
	if t.reconciler == nil {
		return nil
	}
	if err := t.reconciler.ReconcileEventSubsOnce(ctx); err != nil {
		return fmt.Errorf("reconcile eventsubs once: %w", err)
	}
	return nil
}

func (t eventSubTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}

func (t gracePolicyTask) Name() string { return "enforce_group_grace_policy" }

func (t gracePolicyTask) Run(ctx context.Context) error {
	if t.store == nil || t.kicker == nil {
		return nil
	}

	groups, err := t.store.ListManagedGroups(ctx)
	if err != nil {
		return fmt.Errorf("list managed groups: %w", err)
	}

	deadline := t.now().Add(-t.graceAfter)
	var partialErrs []error
	for _, group := range groups {
		if group.Policy != core.GroupPolicyGraceWeek {
			continue
		}
		untracked, err := t.store.ListUntrackedGroupMembers(ctx, group.ChatID)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("list untracked group members for %d: %w", group.ChatID, err))
			t.logger.Warn("grace policy list untracked members failed", "chat_id", group.ChatID, "error", err)
			continue
		}
		for _, member := range untracked {
			if member.FirstSeenAt.IsZero() || member.FirstSeenAt.After(deadline) {
				continue
			}
			if err := t.kicker.KickFromGroup(ctx, group.ChatID, member.TelegramUserID, core.KickReasonGroupGracePolicy); err != nil {
				partialErrs = append(partialErrs, fmt.Errorf("kick unverified member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
				t.logger.Warn("grace policy kick failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
				continue
			}
			if err := t.store.RemoveUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
				partialErrs = append(partialErrs, fmt.Errorf("remove untracked group member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
				t.logger.Warn("grace policy untracked cleanup failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
			}
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t gracePolicyTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return "partial_failure"
	default:
		return taskResultFailed
	}
}

func (t memberCleanupTask) Name() string { return "process_member_cleanup_jobs" }

func (t memberCleanupTask) Run(ctx context.Context) error {
	if t.store == nil || t.kicker == nil {
		return nil
	}
	jobs, err := t.store.ListPendingMemberCleanupJobs(ctx)
	if err != nil {
		return fmt.Errorf("list pending member cleanup jobs: %w", err)
	}
	var partialErrs []error
	for _, job := range jobs {
		claimed, err := t.store.ClaimMemberCleanupJob(ctx, job.ID, t.lockTTL)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("claim member cleanup job %s: %w", job.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		result, done, runErr := t.processJob(ctx, job)
		if runErr != nil {
			partialErrs = append(partialErrs, fmt.Errorf("process member cleanup job %s: %w", job.ID, runErr))
			continue
		}
		if !done {
			continue
		}
		if t.notifier != nil {
			if err := t.notifier.NotifyMemberCleanupComplete(ctx, result); err != nil {
				t.logger.Warn("member cleanup completion notification failed", "job_id", job.ID, "owner_telegram_id", result.OwnerTelegramID, "error", err)
			}
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t productMetricsSnapshotTask) Name() string { return "sync_product_metrics_snapshot" }

func (t productMetricsSnapshotTask) Run(ctx context.Context) error {
	if t.store == nil || t.sink == nil {
		return nil
	}
	now := t.now()
	cutoff := now.Add(-t.window)
	if err := t.store.PruneTelegramActiveUsersBefore(ctx, cutoff); err != nil {
		return fmt.Errorf("prune telegram active users: %w", err)
	}
	dau, err := t.store.CountTelegramActiveUsersSince(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("count telegram active users: %w", err)
	}
	viewers, err := t.store.CountLinkedViewerAccounts(ctx)
	if err != nil {
		return fmt.Errorf("count linked viewer accounts: %w", err)
	}
	creators, err := t.store.CountLinkedCreatorAccounts(ctx)
	if err != nil {
		return fmt.Errorf("count linked creator accounts: %w", err)
	}
	groups, err := t.store.CountManagedGroups(ctx)
	if err != nil {
		return fmt.Errorf("count managed groups: %w", err)
	}

	t.sink.TelegramDailyActiveUsers(dau)
	t.sink.LinkedViewerAccounts(viewers)
	t.sink.LinkedCreatorAccounts(creators)
	t.sink.ManagedGroups(groups)
	return nil
}

func (t productMetricsSnapshotTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}

func (t memberCleanupTask) processJob(ctx context.Context, job core.MemberCleanupJob) (core.MemberCleanupResult, bool, error) {
	limit := t.maxTargetsPerRun
	if limit <= 0 || limit > len(job.Targets) {
		limit = len(job.Targets)
	}
	succeeded := 0
	remaining := make([]core.MemberCleanupTarget, 0, len(job.Targets))
	permanentFailures := 0
	for idx, target := range job.Targets {
		if idx >= limit {
			remaining = append(remaining, job.Targets[idx:]...)
			break
		}
		reason := core.KickReasonGroupUnregistration
		if job.Kind == core.MemberCleanupKindCreatorReset {
			reason = core.KickReasonCreatorReset
		}
		if err := t.kicker.KickFromGroup(ctx, target.ChatID, target.TelegramUserID, reason); err != nil {
			target.Attempts++
			if target.Attempts >= target.MaxAttempts {
				permanentFailures++
				t.logger.Warn("member cleanup target exhausted", "job_id", job.ID, "chat_id", target.ChatID, "telegram_user_id", target.TelegramUserID, "attempts", target.Attempts, "error", err)
				continue
			}
			remaining = append(remaining, target)
			t.logger.Warn("member cleanup target failed", "job_id", job.ID, "chat_id", target.ChatID, "telegram_user_id", target.TelegramUserID, "attempts", target.Attempts, "error", err)
			continue
		}
		succeeded++
	}
	job.SucceededCount += succeeded
	failed := job.TotalTargets - job.SucceededCount
	done := len(remaining) == 0
	job.Targets = remaining
	if done {
		if permanentFailures > 0 {
			job.Status = core.MemberCleanupStatusExhausted
		} else {
			job.Status = core.MemberCleanupStatusDone
		}
	}
	if err := t.store.SaveMemberCleanupJob(ctx, job); err != nil {
		return core.MemberCleanupResult{}, false, fmt.Errorf("save member cleanup job: %w", err)
	}
	return core.MemberCleanupResult{
		Kind:              job.Kind,
		Status:            job.Status,
		OwnerTelegramID:   job.OwnerTelegramID,
		CreatorLogin:      job.CreatorLogin,
		GroupName:         job.GroupName,
		ManagedGroupCount: job.ManagedGroupCount,
		TargetedCount:     job.TotalTargets,
		SucceededCount:    job.SucceededCount,
		FailedCount:       failed,
	}, done, nil
}

func (t memberCleanupTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return "partial_failure"
	default:
		return taskResultFailed
	}
}

func (t subscriptionGraceTask) Name() string { return "process_subscription_end_grace" }

func (t subscriptionGraceTask) Run(ctx context.Context) error {
	if t.store == nil || t.kicker == nil {
		return nil
	}
	jobs, err := t.store.ListDueSubscriptionEndGrace(ctx, t.now(), t.maxJobs)
	if err != nil {
		return fmt.Errorf("list due subscription-end grace jobs: %w", err)
	}
	var partialErrs []error
	for _, job := range jobs {
		claimed, err := t.store.ClaimSubscriptionEndGrace(ctx, job.ID, t.lockTTL)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("claim subscription-end grace job %s: %w", job.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		if err := t.processJob(ctx, job); err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("process subscription-end grace job %s: %w", job.ID, err))
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t subscriptionGraceTask) processJob(ctx context.Context, job core.PendingSubscriptionEndGrace) error {
	subscriber, err := t.store.IsCreatorSubscriber(ctx, job.CreatorID, job.TwitchUserID)
	if err != nil {
		return fmt.Errorf("check creator subscriber: %w", err)
	}
	if subscriber {
		if err := t.store.DeleteSubscriptionEndGrace(ctx, job.CreatorID, job.TwitchUserID); err != nil {
			return fmt.Errorf("delete subscription-end grace for resubscribed viewer: %w", err)
		}
		return nil
	}

	groups, err := t.store.ListManagedGroupsByCreator(ctx, job.CreatorID)
	if err != nil {
		return fmt.Errorf("list managed groups by creator: %w", err)
	}
	for _, group := range groups {
		if err := t.kicker.KickFromGroup(ctx, group.ChatID, job.TelegramUserID, core.KickReasonSubscriptionGrace); err != nil {
			t.logger.Warn("subscription grace kick failed", "creator_id", job.CreatorID, "chat_id", group.ChatID, "telegram_user_id", job.TelegramUserID, "error", err)
			continue
		}
		if err := t.store.RemoveTrackedGroupMember(ctx, group.ChatID, job.TelegramUserID); err != nil {
			t.logger.Warn("subscription grace tracked membership cleanup failed", "creator_id", job.CreatorID, "chat_id", group.ChatID, "telegram_user_id", job.TelegramUserID, "error", err)
		}
	}
	if err := t.store.DeleteSubscriptionEndGrace(ctx, job.CreatorID, job.TwitchUserID); err != nil {
		return fmt.Errorf("delete subscription-end grace job: %w", err)
	}
	if t.notifier != nil && job.TelegramUserID != 0 && len(groups) > 0 {
		lang := job.Language
		if lang == "" {
			lang = "en"
		}
		if err := t.notifier.NotifySubscriptionGraceExpired(ctx, core.ExpiredSubscriptionGraceResult{
			TelegramUserID:   job.TelegramUserID,
			Language:         lang,
			ViewerLogin:      job.ViewerLogin,
			BroadcasterLogin: job.CreatorLogin,
		}); err != nil {
			t.logger.Warn("subscription grace expired notification failed", "creator_id", job.CreatorID, "telegram_user_id", job.TelegramUserID, "error", err)
		}
	}
	return nil
}

func (t subscriptionGraceTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return "partial_failure"
	default:
		return taskResultFailed
	}
}

type integrityAuditStore interface {
	ListCreators(ctx context.Context) ([]core.Creator, error)
	ActiveCreatorIDsWithoutGroup(ctx context.Context, creators []core.Creator) (int, error)
	RepairTrackedGroupReverseIndex(ctx context.Context) (indexUsers, repairedUsers, missingLinks, staleLinks int, err error)
}

type integrityAuditTask struct {
	store  integrityAuditStore
	logger *slog.Logger
	events events.EventSink
}

// NewIntegrityAuditTask builds the integrity audit and repair task.
func NewIntegrityAuditTask(store integrityAuditStore, logger *slog.Logger, sink events.EventSink) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return integrityAuditTask{store: store, logger: logger, events: sink}
}

func (t integrityAuditTask) Name() string { return "integrity_audit" }

func (t integrityAuditTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, errListCreators):
		return "list_creators_failed"
	case errors.Is(err, errReadActiveSet):
		return "active_set_read_failed"
	case errors.Is(err, errRepairTrackedIndex):
		return "tracked_reverse_index_repair_failed"
	default:
		return taskResultFailed
	}
}

var (
	errListCreators       = errors.New("list creators failed")
	errReadActiveSet      = errors.New("read active creator set failed")
	errRepairTrackedIndex = errors.New("repair tracked reverse index failed")
)

func (t integrityAuditTask) Run(ctx context.Context) error {
	if t.store == nil {
		return nil
	}

	creators, err := t.store.ListCreators(ctx)
	if err != nil {
		t.logger.Warn("integrity audit list creators failed", "error", err)
		return fmt.Errorf("list creators: %w", errors.Join(errListCreators, err))
	}

	activeNoGroup, err := t.store.ActiveCreatorIDsWithoutGroup(ctx, creators)
	if err != nil {
		t.logger.Warn("integrity audit active creator set read failed", "error", err)
		return fmt.Errorf("read active creator set: %w", errors.Join(errReadActiveSet, err))
	}

	indexUsers, repairedUsers, missingLinks, staleLinks, err := t.store.RepairTrackedGroupReverseIndex(ctx)
	if err != nil {
		t.logger.Warn("integrity audit tracked reverse index repair failed", "error", err)
		return fmt.Errorf("repair tracked reverse index: %w", errors.Join(errRepairTrackedIndex, err))
	}

	reconnectRequired := 0
	for _, creator := range creators {
		if creator.AuthStatus == core.CreatorAuthReconnectRequired {
			reconnectRequired++
		}
	}

	t.logger.Info("integrity audit done",
		"creators", len(creators),
		"active_without_group", activeNoGroup,
		"creators_reconnect_required", reconnectRequired,
		"index_users", indexUsers,
		"index_repaired_users", repairedUsers,
		"index_missing_links", missingLinks,
		"index_stale_links", staleLinks,
	)

	if t.events != nil {
		t.emitTrackedReverseIndexCount(ctx, "ok", repairedUsers)
		t.emitTrackedReverseIndexCount(ctx, "missing_links", missingLinks)
		t.emitTrackedReverseIndexCount(ctx, "stale_links", staleLinks)
		t.emitTrackedReverseIndexCount(ctx, "indexed_users", indexUsers)
	}
	return nil
}

func (t integrityAuditTask) emitTrackedReverseIndexCount(ctx context.Context, outcome string, count int) {
	if t.events == nil || count <= 0 {
		return
	}
	t.events.Emit(ctx, events.Event{
		Name:    events.NameReconciliationRepair,
		Outcome: outcome,
		Fields:  map[string]string{"repair": "tracked_reverse_index"},
		Count:   count,
	})
}
