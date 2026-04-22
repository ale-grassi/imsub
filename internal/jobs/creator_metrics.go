package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"imsub/internal/core"
)

type creatorMetricsStore interface {
	ListCreators(ctx context.Context) ([]core.Creator, error)
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error)
	CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error)
	CreatorBlockedUserCount(ctx context.Context, creatorID string) (int64, error)
	CountTrackedGroupMembers(ctx context.Context, chatID int64) (int, error)
	CountUntrackedGroupMembers(ctx context.Context, chatID int64) (int, error)
}

type creatorMetricsSink interface {
	ResetCreatorSnapshotMetrics()
	CreatorInfo(creatorID, displayName, login string)
	CreatorManagedGroups(creatorID string, count int)
	CreatorGroupPolicyCount(creatorID, policy string, count int)
	CreatorGroupLanguageCount(creatorID, language string, count int)
	CreatorSubscriptionEndGrace(creatorID, grace string)
	CreatorBanSyncEnabled(creatorID string, enabled bool)
	CreatorLastBanSyncAt(creatorID string, at time.Time)
	CreatorSubscribers(creatorID string, count int)
	CreatorBlockedUsers(creatorID string, count int)
	CreatorTrackedMembers(creatorID string, count int)
	CreatorUntrackedMembers(creatorID string, count int)
	CreatorReconnectRequired(creatorID string, required bool)
}

type creatorMetricsTask struct {
	store  creatorMetricsStore
	sink   creatorMetricsSink
	logger *slog.Logger
}

// NewCreatorMetricsTask builds the periodic creator metrics snapshot task.
func NewCreatorMetricsTask(store creatorMetricsStore, sink creatorMetricsSink, logger *slog.Logger) Task {
	if logger == nil {
		logger = slog.Default()
	}
	return creatorMetricsTask{
		store:  store,
		sink:   sink,
		logger: logger,
	}
}

func (t creatorMetricsTask) Name() string { return "creator_metrics_snapshot" }

func (t creatorMetricsTask) Run(ctx context.Context) error {
	if t.store == nil || t.sink == nil {
		return nil
	}

	creators, err := t.store.ListCreators(ctx)
	if err != nil {
		return fmt.Errorf("list creators: %w", err)
	}

	t.sink.ResetCreatorSnapshotMetrics()

	for _, creator := range creators {
		t.sink.CreatorInfo(creator.ID, creator.TwitchDisplayName, creator.TwitchLogin)
		t.sink.CreatorSubscriptionEndGrace(creator.ID, string(creator.SubscriptionEndGrace))
		t.sink.CreatorBanSyncEnabled(creator.ID, creator.BlocklistSyncEnabled)
		t.sink.CreatorLastBanSyncAt(creator.ID, creator.LastBanSyncAt)

		groups, err := t.store.ListManagedGroupsByCreator(ctx, creator.ID)
		if err != nil {
			t.logger.Warn("creator metrics list managed groups failed", "creator_id", creator.ID, "error", err)
			continue
		}

		subscribers, err := t.store.CreatorSubscriberCount(ctx, creator.ID)
		if err != nil {
			t.logger.Warn("creator metrics subscriber count failed", "creator_id", creator.ID, "error", err)
			continue
		}

		blockedUsers, err := t.store.CreatorBlockedUserCount(ctx, creator.ID)
		if err != nil {
			t.logger.Warn("creator metrics blocked-user count failed", "creator_id", creator.ID, "error", err)
			continue
		}

		t.sink.CreatorManagedGroups(creator.ID, len(groups))
		policyCounts := make(map[string]int, len(groups))
		languageCounts := make(map[string]int, len(groups))
		for _, group := range groups {
			policyCounts[string(group.Policy)]++
			languageCounts[group.Language]++
		}
		for policy, count := range policyCounts {
			t.sink.CreatorGroupPolicyCount(creator.ID, policy, count)
		}
		for language, count := range languageCounts {
			t.sink.CreatorGroupLanguageCount(creator.ID, language, count)
		}
		t.sink.CreatorSubscribers(creator.ID, int(subscribers))
		t.sink.CreatorBlockedUsers(creator.ID, int(blockedUsers))
		t.sink.CreatorReconnectRequired(creator.ID, creator.AuthStatus == core.CreatorAuthReconnectRequired)

		trackedMembers := 0
		untrackedMembers := 0
		groupCountsOK := true
		for _, group := range groups {
			trackedCount, err := t.store.CountTrackedGroupMembers(ctx, group.ChatID)
			if err != nil {
				t.logger.Warn("creator metrics tracked-member count failed", "creator_id", creator.ID, "chat_id", group.ChatID, "error", err)
				groupCountsOK = false
				break
			}
			untrackedCount, err := t.store.CountUntrackedGroupMembers(ctx, group.ChatID)
			if err != nil {
				t.logger.Warn("creator metrics untracked-member count failed", "creator_id", creator.ID, "chat_id", group.ChatID, "error", err)
				groupCountsOK = false
				break
			}
			trackedMembers += trackedCount
			untrackedMembers += untrackedCount
		}
		if !groupCountsOK {
			continue
		}

		t.sink.CreatorTrackedMembers(creator.ID, trackedMembers)
		t.sink.CreatorUntrackedMembers(creator.ID, untrackedMembers)
	}

	return nil
}
func (t creatorMetricsTask) Classify(err error) string {
	if err != nil {
		return taskResultFailed
	}
	return "ok"
}
