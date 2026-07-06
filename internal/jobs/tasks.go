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

const (
	taskResultFailed         = "failed"
	taskResultPartialFailure = "partial_failure"
	taskResultPanic          = "panic"
	taskResultTimeout        = "timeout"
	taskNameReconcileSubs    = "reconcile_subscribers"
)

type subscriberReconciler interface {
	ReconcileSubscribersOnce(ctx context.Context) error
}

type eventSubReconciler interface {
	ReconcileEventSubsOnce(ctx context.Context) error
}

type gracePolicyStore interface {
	core.PromoteExistingMemberStore
	ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error)
	ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error)
}

type groupKicker interface {
	KickFromGroup(ctx context.Context, groupChatID, telegramUserID int64, reason core.KickReason) error
}

type memberCleanupStore interface {
	ListPendingMemberCleanupJobs(ctx context.Context) ([]core.MemberCleanupJob, error)
	ClaimMemberCleanupJob(ctx context.Context, jobID string, ttl time.Duration) (bool, error)
	ReleaseMemberCleanupJob(ctx context.Context, jobID string) error
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

type privacyRetentionStore interface {
	PurgeExpiredPrivacyData(ctx context.Context, untrackedRetention time.Duration) (int, error)
}

type memberTagSyncer interface {
	SyncEnabledGroups(ctx context.Context) (core.MemberTagSyncCounts, error)
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

func (t subscriberTask) Name() string { return taskNameReconcileSubs }

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
		return taskResultPartialFailure
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
	god        *core.GodAccessChecker
	logger     *slog.Logger
	now        func() time.Time
	graceAfter time.Duration
	counts     *runCounts
}

type kickPolicyTask struct {
	store  gracePolicyStore
	kicker groupKicker
	god    *core.GodAccessChecker
	logger *slog.Logger
	now    func() time.Time
	counts *runCounts
}

type memberCleanupTask struct {
	store            memberCleanupStore
	kicker           groupKicker
	notifier         memberCleanupNotifier
	god              *core.GodAccessChecker
	logger           *slog.Logger
	lockTTL          time.Duration
	maxTargetsPerRun int
	counts           *runCounts
}

type subscriptionGraceTask struct {
	store    subscriptionGraceStore
	kicker   groupKicker
	notifier subscriptionGraceNotifier
	god      *core.GodAccessChecker
	logger   *slog.Logger
	lockTTL  time.Duration
	maxJobs  int64
	now      func() time.Time
	counts   *runCounts
}

type productMetricsSnapshotTask struct {
	store  productMetricsSnapshotStore
	sink   productMetricsSink
	now    func() time.Time
	window time.Duration
}

type privacyRetentionTask struct {
	store              privacyRetentionStore
	untrackedRetention time.Duration
}

type memberTagSyncTask struct {
	syncer memberTagSyncer
	counts *runCounts
}

// NewEventSubTask builds the EventSub reconciliation task.
func NewEventSubTask(r eventSubReconciler) Task {
	return eventSubTask{reconciler: r}
}

// NewGracePolicyTask builds the periodic enforcement task for grace-period
// unverified-member policies.
func NewGracePolicyTask(store gracePolicyStore, kicker groupKicker, god *core.GodAccessChecker, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return gracePolicyTask{
		store:      store,
		kicker:     kicker,
		god:        god,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
		graceAfter: 7 * 24 * time.Hour,
		counts:     newRunCounts(),
	}
}

// NewKickPolicyTask builds the periodic sweep that re-attempts kicks for groups
// whose policy is GroupPolicyKick when an earlier kick failed (e.g. the bot
// temporarily lacked CanRestrictMembers) and the member is still persisted as
// untracked.
func NewKickPolicyTask(store gracePolicyStore, kicker groupKicker, god *core.GodAccessChecker, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return kickPolicyTask{
		store:  store,
		kicker: kicker,
		god:    god,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
		counts: newRunCounts(),
	}
}

// NewMemberCleanupTask builds the periodic task that drains background tracked-member cleanup jobs.
func NewMemberCleanupTask(store memberCleanupStore, kicker groupKicker, notifier memberCleanupNotifier, god *core.GodAccessChecker, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return memberCleanupTask{
		store:            store,
		kicker:           kicker,
		notifier:         notifier,
		god:              god,
		logger:           logger,
		lockTTL:          15 * time.Minute,
		maxTargetsPerRun: 50,
		counts:           newRunCounts(),
	}
}

// NewMemberTagSyncTask builds the periodic background task for Telegram member-tag reconciliation.
func NewMemberTagSyncTask(syncer memberTagSyncer) Task {
	return memberTagSyncTask{
		syncer: syncer,
		counts: newRunCounts(),
	}
}

// NewSubscriptionGraceTask builds the periodic sweep that enforces delayed
// subscription-end removals.
func NewSubscriptionGraceTask(store subscriptionGraceStore, kicker groupKicker, notifier subscriptionGraceNotifier, god *core.GodAccessChecker, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return subscriptionGraceTask{
		store:    store,
		kicker:   kicker,
		notifier: notifier,
		god:      god,
		logger:   logger,
		lockTTL:  10 * time.Minute,
		maxJobs:  100,
		now:      func() time.Time { return time.Now().UTC() },
		counts:   newRunCounts(),
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

// NewPrivacyRetentionTask builds the periodic privacy-retention sweep.
func NewPrivacyRetentionTask(store privacyRetentionStore, untrackedRetention time.Duration) Task {
	return privacyRetentionTask{
		store:              store,
		untrackedRetention: untrackedRetention,
	}
}

func (t memberTagSyncTask) Name() string { return "sync_member_tags" }

func (t memberTagSyncTask) Run(ctx context.Context) error {
	if t.syncer == nil {
		return nil
	}
	t.counts.reset()
	counts, err := t.syncer.SyncEnabledGroups(ctx)
	t.counts.add("groups", counts.Groups)
	t.counts.add("set", counts.Set)
	t.counts.add("cleared", counts.Cleared)
	t.counts.add("noop", counts.Noop)
	t.counts.add("errors", counts.Errors)
	t.counts.add("tracked_stored", counts.TrackedStored)
	t.counts.add("untracked_stored", counts.UntrackedStored)
	t.counts.add("desired_tracked", counts.DesiredTracked)
	t.counts.add("desired_untracked", counts.DesiredUntracked)
	t.counts.add("existing_tags", counts.ExistingTags)
	t.counts.add("snapshot_members", counts.SnapshotMembers)
	t.counts.add("snapshot_missing_tracked", counts.SnapshotMissingTracked)
	t.counts.add("snapshot_missing_untracked", counts.SnapshotMissingUntracked)
	if err != nil {
		return fmt.Errorf("sync enabled member tags: %w", err)
	}
	return nil
}

func (t memberTagSyncTask) Classify(err error) string {
	if err == nil {
		return "ok"
	}
	return taskResultFailed
}

func (t memberTagSyncTask) Report() map[string]int {
	return t.counts.snapshot()
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

func (t privacyRetentionTask) Name() string { return "privacy_retention" }

func (t privacyRetentionTask) Run(ctx context.Context) error {
	if t.store == nil {
		return nil
	}
	_, err := t.store.PurgeExpiredPrivacyData(ctx, t.untrackedRetention)
	if err != nil {
		return fmt.Errorf("purge expired privacy data: %w", err)
	}
	return nil
}

func (t privacyRetentionTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}

func (t gracePolicyTask) Name() string { return "enforce_group_grace_policy" }

func (t gracePolicyTask) Run(ctx context.Context) error {
	deadline := t.now().Add(-t.graceAfter)
	return memberPolicySweep{
		store:        t.store,
		kicker:       t.kicker,
		god:          t.god,
		logger:       t.logger,
		now:          t.now,
		counts:       t.counts,
		logPrefix:    "grace policy",
		policy:       core.GroupPolicyGraceWeek,
		sourceGod:    core.SourceGodListGracePolicy,
		sourceRescue: core.SourceGracePolicyRescue,
		kickReason:   core.KickReasonGroupGracePolicy,
		shouldEvaluate: func(m core.UntrackedGroupMember) bool {
			return !m.FirstSeenAt.IsZero() && !m.FirstSeenAt.After(deadline)
		},
	}.run(ctx)
}

func (t gracePolicyTask) Report() map[string]int { return t.counts.snapshot() }

func (t gracePolicyTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return taskResultPartialFailure
	default:
		return taskResultFailed
	}
}

func (t kickPolicyTask) Name() string { return "enforce_group_kick_policy" }

func (t kickPolicyTask) Run(ctx context.Context) error {
	return memberPolicySweep{
		store:          t.store,
		kicker:         t.kicker,
		god:            t.god,
		logger:         t.logger,
		now:            t.now,
		counts:         t.counts,
		logPrefix:      "kick policy",
		policy:         core.GroupPolicyKick,
		sourceGod:      core.SourceGodListKickPolicy,
		sourceRescue:   core.SourceKickPolicyRescue,
		kickReason:     core.KickReasonGroupPolicy,
		shouldEvaluate: func(core.UntrackedGroupMember) bool { return true },
	}.run(ctx)
}

// memberPolicySweep shares the per-group untracked-member loop between the
// grace and kick policy tasks. Callers supply the distinguishing constants
// via the fields; run iterates managed groups, filters by s.policy, and
// applies god-promote / eligible-rescue / kick in that order.
type memberPolicySweep struct {
	store          gracePolicyStore
	kicker         groupKicker
	god            *core.GodAccessChecker
	logger         *slog.Logger
	now            func() time.Time
	counts         *runCounts
	logPrefix      string
	policy         core.GroupPolicy
	sourceGod      string
	sourceRescue   string
	kickReason     core.KickReason
	shouldEvaluate func(core.UntrackedGroupMember) bool
}

func (s memberPolicySweep) run(ctx context.Context) error {
	if s.store == nil || s.kicker == nil {
		return nil
	}
	s.counts.reset()

	groups, err := s.store.ListManagedGroups(ctx)
	if err != nil {
		return fmt.Errorf("list managed groups: %w", err)
	}

	var partialErrs []error
	for _, group := range groups {
		if group.Policy != s.policy {
			continue
		}
		s.counts.inc("groups")
		untracked, err := s.store.ListUntrackedGroupMembers(ctx, group.ChatID)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("list untracked group members for %d: %w", group.ChatID, err))
			s.counts.inc("errors")
			s.logger.Warn(s.logPrefix+" list untracked members failed", "chat_id", group.ChatID, "error", err)
			continue
		}
		// Lazy-loaded once per group: eligibility checks only depend on the
		// creator's ID and BlocklistSyncEnabled, both constant across the
		// inner loop. Loading on first non-god use keeps god-only groups
		// from incurring the lookup and prevents a creator lookup failure
		// from blocking god-member processing under the same group.
		var (
			creator       core.Creator
			creatorLoaded bool
			creatorUsable bool
		)
		for _, member := range untracked {
			if s.god != nil && s.god.IsGodTelegramUser(member.TelegramUserID) {
				if err := s.store.AddTrackedGroupMember(ctx, group.ChatID, member.TelegramUserID, s.sourceGod, s.now()); err != nil {
					partialErrs = append(partialErrs, fmt.Errorf("track god member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
					s.counts.inc("errors")
					s.logger.Warn(s.logPrefix+" track god member failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
					continue
				}
				if err := s.store.RemoveUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
					partialErrs = append(partialErrs, fmt.Errorf("remove god untracked group member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
					s.counts.inc("errors")
					s.logger.Warn(s.logPrefix+" remove god untracked member failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
				}
				continue
			}
			if !s.shouldEvaluate(member) {
				continue
			}
			if !creatorLoaded {
				loaded, found, loadErr := s.store.Creator(ctx, group.CreatorID)
				creatorLoaded = true
				switch {
				case loadErr != nil:
					partialErrs = append(partialErrs, fmt.Errorf("load creator for %d: %w", group.ChatID, loadErr))
					s.counts.inc("errors")
					s.logger.Warn(s.logPrefix+" load creator failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "error", loadErr)
				case !found:
					partialErrs = append(partialErrs, fmt.Errorf("%w: creator_id=%s chat_id=%d", core.ErrCreatorMissing, group.CreatorID, group.ChatID))
					s.counts.inc("errors")
					s.logger.Warn(s.logPrefix+" creator missing", "chat_id", group.ChatID, "creator_id", group.CreatorID)
				default:
					creator = loaded
					creatorUsable = true
				}
			}
			if !creatorUsable {
				continue
			}
			promoted, err := core.PromoteExistingMemberIfEligibleWithCreator(ctx, s.store, s.god, group, creator, member.TelegramUserID, s.sourceRescue, s.now())
			if promoted {
				s.counts.inc("rescued")
				s.logger.Info(s.logPrefix+" rescued eligible member", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", member.TelegramUserID)
				if err != nil {
					partialErrs = append(partialErrs, fmt.Errorf("rescue cleanup %d from %d: %w", member.TelegramUserID, group.ChatID, err))
					s.counts.inc("errors")
					s.logger.Warn(s.logPrefix+" rescue cleanup failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", member.TelegramUserID, "error", err)
				} else {
					s.counts.inc("removed")
				}
				continue
			}
			if err != nil {
				partialErrs = append(partialErrs, fmt.Errorf("promote eligible member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
				s.counts.inc("errors")
				s.logger.Warn(s.logPrefix+" promote failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "telegram_user_id", member.TelegramUserID, "error", err)
				continue
			}
			if err := s.kicker.KickFromGroup(ctx, group.ChatID, member.TelegramUserID, s.kickReason); err != nil {
				partialErrs = append(partialErrs, fmt.Errorf("kick unverified member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
				s.counts.inc("errors")
				s.logger.Warn(s.logPrefix+" kick failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
				continue
			}
			s.counts.inc("kicked")
			if err := s.store.RemoveUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
				partialErrs = append(partialErrs, fmt.Errorf("remove untracked group member %d from %d: %w", member.TelegramUserID, group.ChatID, err))
				s.counts.inc("errors")
				s.logger.Warn(s.logPrefix+" untracked cleanup failed", "chat_id", group.ChatID, "telegram_user_id", member.TelegramUserID, "error", err)
				continue
			}
			s.counts.inc("removed")
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t kickPolicyTask) Report() map[string]int { return t.counts.snapshot() }

func (t kickPolicyTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return taskResultPartialFailure
	default:
		return taskResultFailed
	}
}

func (t memberCleanupTask) Name() string { return "process_member_cleanup_jobs" }

func (t memberCleanupTask) Run(ctx context.Context) error {
	if t.store == nil || t.kicker == nil {
		return nil
	}
	t.counts.reset()
	jobs, err := t.store.ListPendingMemberCleanupJobs(ctx)
	if err != nil {
		return fmt.Errorf("list pending member cleanup jobs: %w", err)
	}
	var partialErrs []error
	for _, job := range jobs {
		claimed, err := t.store.ClaimMemberCleanupJob(ctx, job.ID, t.lockTTL)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("claim member cleanup job %s: %w", job.ID, err))
			t.counts.inc("errors")
			continue
		}
		if !claimed {
			t.counts.inc("locked")
			continue
		}
		t.counts.inc("processed")
		result, done, runErr := t.processJob(ctx, job)
		if runErr != nil {
			partialErrs = append(partialErrs, fmt.Errorf("process member cleanup job %s: %w", job.ID, runErr))
			t.counts.inc("errors")
			continue
		}
		t.counts.add("succeeded", result.SucceededCount)
		t.counts.add("failed", result.FailedCount)
		if done && t.notifier != nil {
			if err := t.notifier.NotifyMemberCleanupComplete(ctx, result); err != nil {
				t.logger.Warn("member cleanup completion notification failed", "job_id", job.ID, "owner_telegram_id", result.OwnerTelegramID, "error", err)
			}
		}
		// Release the lock after a successful pass so an unfinished job can
		// continue on the next tick instead of stalling for the lock TTL. On
		// processing errors the lock is kept as a natural retry back-off.
		if err := t.store.ReleaseMemberCleanupJob(ctx, job.ID); err != nil {
			t.logger.Warn("member cleanup lock release failed", "job_id", job.ID, "error", err)
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t memberCleanupTask) Report() map[string]int { return t.counts.snapshot() }

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
		if t.god != nil && t.god.IsGodTelegramUser(target.TelegramUserID) {
			job.TotalTargets--
			continue
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
		GroupNames:        append([]string(nil), job.GroupNames...),
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
		return taskResultPartialFailure
	default:
		return taskResultFailed
	}
}

func (t subscriptionGraceTask) Name() string { return "process_subscription_end_grace" }

func (t subscriptionGraceTask) Run(ctx context.Context) error {
	if t.store == nil || t.kicker == nil {
		return nil
	}
	t.counts.reset()
	jobs, err := t.store.ListDueSubscriptionEndGrace(ctx, t.now(), t.maxJobs)
	if err != nil {
		return fmt.Errorf("list due subscription-end grace jobs: %w", err)
	}
	t.counts.add("due", len(jobs))
	var partialErrs []error
	for _, job := range jobs {
		claimed, err := t.store.ClaimSubscriptionEndGrace(ctx, job.ID, t.lockTTL)
		if err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("claim subscription-end grace job %s: %w", job.ID, err))
			t.counts.inc("errors")
			continue
		}
		if !claimed {
			t.counts.inc("locked")
			continue
		}
		if err := t.processJob(ctx, job); err != nil {
			partialErrs = append(partialErrs, fmt.Errorf("process subscription-end grace job %s: %w", job.ID, err))
			t.counts.inc("errors")
		}
	}
	if len(partialErrs) > 0 {
		return errors.Join(append([]error{core.ErrPartialReconcile}, partialErrs...)...)
	}
	return nil
}

func (t subscriptionGraceTask) Report() map[string]int { return t.counts.snapshot() }

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
	if t.god != nil && t.god.IsGodTelegramUser(job.TelegramUserID) {
		if err := t.store.DeleteSubscriptionEndGrace(ctx, job.CreatorID, job.TwitchUserID); err != nil {
			return fmt.Errorf("delete subscription-end grace for god-listed viewer: %w", err)
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
		t.counts.inc("kicked")
		if err := t.store.RemoveTrackedGroupMember(ctx, group.ChatID, job.TelegramUserID); err != nil {
			t.logger.Warn("subscription grace tracked membership cleanup failed", "creator_id", job.CreatorID, "chat_id", group.ChatID, "telegram_user_id", job.TelegramUserID, "error", err)
			continue
		}
		t.counts.inc("removed")
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
		t.counts.inc("notified")
	}
	return nil
}

func (t subscriptionGraceTask) Classify(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, core.ErrPartialReconcile):
		return taskResultPartialFailure
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
	counts *runCounts
}

// NewIntegrityAuditTask builds the integrity audit and repair task.
func NewIntegrityAuditTask(store integrityAuditStore, logger *slog.Logger, sink events.EventSink) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return integrityAuditTask{store: store, logger: logger, events: sink, counts: newRunCounts()}
}

func (t integrityAuditTask) Report() map[string]int { return t.counts.snapshot() }

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
	t.counts.reset()

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

	t.counts.add("creators_checked", len(creators))
	t.counts.add("active_without_group", activeNoGroup)
	t.counts.add("reconnect_required", reconnectRequired)
	t.counts.add("repairs", repairedUsers)
	t.counts.add("missing_links", missingLinks)
	t.counts.add("stale_links", staleLinks)

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
