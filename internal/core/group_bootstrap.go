package core

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"imsub/internal/events"
	"imsub/internal/transport/telegram/mtproto"
)

type groupBootstrapStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
	UpsertUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string, at time.Time) error
	RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

type groupBootstrapGroupOps interface {
	CreateBootstrapInviteLink(ctx context.Context, groupChatID int64) (string, error)
	KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64, reason KickReason) error
}

type groupBootstrapMTProto interface {
	DumpMembersViaInvite(ctx context.Context, inviteLink string) ([]mtproto.Member, error)
	SelfUserID() int64
}

// GroupBootstrapService performs the initial MTProto member dump for a newly linked group.
type GroupBootstrapService struct {
	store       groupBootstrapStore
	groupOps    groupBootstrapGroupOps
	mtproto     groupBootstrapMTProto
	god         *GodAccessChecker
	events      events.EventSink
	logger      *slog.Logger
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
	retryDelays []time.Duration
}

// NewGroupBootstrapService creates the bootstrap-sync service.
func NewGroupBootstrapService(store groupBootstrapStore, groupOps groupBootstrapGroupOps, mt groupBootstrapMTProto, god *GodAccessChecker, sink events.EventSink, logger *slog.Logger) *GroupBootstrapService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GroupBootstrapService{
		store:    store,
		groupOps: groupOps,
		mtproto:  mt,
		god:      god,
		events:   events.EnsureSink(sink),
		logger:   logger,
		now: func() time.Time {
			return time.Now().UTC()
		},
		sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		retryDelays: []time.Duration{2 * time.Second, 4 * time.Second},
	}
}

type groupBootstrapCounts struct {
	tracked   int
	untracked int
	kicked    int
}

// BootstrapGroup dumps existing members for a newly linked group and applies its policy silently.
func (s *GroupBootstrapService) BootstrapGroup(ctx context.Context, group ManagedGroup) error {
	if s == nil || s.store == nil || s.groupOps == nil || s.mtproto == nil {
		return nil
	}

	var lastErr error
	for attempt := 0; attempt <= len(s.retryDelays); attempt++ {
		counts, err := s.bootstrapAttempt(ctx, group)
		if err == nil {
			s.emit(ctx, "ok", group, counts, attempt+1)
			return nil
		}
		lastErr = err
		outcome := classifyBootstrapError(err)
		s.emit(ctx, outcome, group, groupBootstrapCounts{}, attempt+1)

		if attempt == len(s.retryDelays) {
			break
		}
		s.emit(ctx, "retry", group, groupBootstrapCounts{}, attempt+1)
		if sleepErr := s.sleep(ctx, s.retryDelays[attempt]); sleepErr != nil {
			return sleepErr
		}
	}
	s.emit(ctx, "failed", group, groupBootstrapCounts{}, len(s.retryDelays)+1)
	return lastErr
}

func (s *GroupBootstrapService) bootstrapAttempt(ctx context.Context, group ManagedGroup) (groupBootstrapCounts, error) {
	inviteLink, err := s.groupOps.CreateBootstrapInviteLink(ctx, group.ChatID)
	if err != nil {
		return groupBootstrapCounts{}, fmt.Errorf("create bootstrap invite link: %w", err)
	}

	membersDump, err := s.mtproto.DumpMembersViaInvite(ctx, inviteLink)
	if err != nil {
		if classifyBootstrapError(err) == "leave_failed" {
			if cleanupErr := s.cleanupMTProtoUser(ctx, group.ChatID); cleanupErr != nil {
				s.logger.Warn("mtproto bootstrap cleanup kick failed", "chat_id", group.ChatID, "telegram_user_id", s.mtproto.SelfUserID(), "error", cleanupErr)
			}
		}
		return groupBootstrapCounts{}, fmt.Errorf("dump members via invite: %w", err)
	}

	now := s.now()
	counts := groupBootstrapCounts{}
	for _, member := range membersDump {
		if shouldSkipBootstrapMember(member, s.mtproto.SelfUserID()) {
			continue
		}

		eligible, err := s.isEligibleTrackedMember(ctx, group.CreatorID, member.TelegramUserID)
		if err != nil {
			return counts, fmt.Errorf("classify member %d: %w", member.TelegramUserID, err)
		}
		if eligible {
			if err := s.store.AddTrackedGroupMember(ctx, group.ChatID, member.TelegramUserID, "mtproto_bootstrap", now); err != nil {
				return counts, fmt.Errorf("track member %d: %w", member.TelegramUserID, err)
			}
			if err := s.store.RemoveUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
				return counts, fmt.Errorf("clear untracked member %d: %w", member.TelegramUserID, err)
			}
			counts.tracked++
			continue
		}

		if err := s.store.RemoveTrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
			return counts, fmt.Errorf("remove stale tracked member %d: %w", member.TelegramUserID, err)
		}
		if err := s.store.UpsertUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID, "mtproto_bootstrap", mtproto.StatusText(member), now); err != nil {
			return counts, fmt.Errorf("upsert untracked member %d: %w", member.TelegramUserID, err)
		}
		counts.untracked++

		if group.Policy != GroupPolicyKick {
			continue
		}
		if err := s.groupOps.KickFromGroup(ctx, group.ChatID, member.TelegramUserID, KickReasonGroupPolicy); err != nil {
			return counts, fmt.Errorf("kick bootstrap member %d: %w", member.TelegramUserID, err)
		}
		if err := s.store.RemoveUntrackedGroupMember(ctx, group.ChatID, member.TelegramUserID); err != nil {
			return counts, fmt.Errorf("cleanup kicked untracked member %d: %w", member.TelegramUserID, err)
		}
		counts.kicked++
	}
	return counts, nil
}

func (s *GroupBootstrapService) isEligibleTrackedMember(ctx context.Context, creatorID string, telegramUserID int64) (bool, error) {
	if s.god != nil && s.god.IsGodTelegramUser(telegramUserID) {
		return true, nil
	}
	identity, found, err := s.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return false, fmt.Errorf("load user identity: %w", err)
	}
	if !found || identity.TwitchUserID == "" {
		return false, nil
	}

	subscribed, err := s.store.IsCreatorSubscriber(ctx, creatorID, identity.TwitchUserID)
	if err != nil {
		return false, fmt.Errorf("check creator subscriber: %w", err)
	}
	if !subscribed {
		return false, nil
	}

	blocked, err := s.store.IsCreatorBlocked(ctx, creatorID, identity.TwitchUserID)
	if err != nil {
		return false, fmt.Errorf("check creator blocked: %w", err)
	}
	return !blocked, nil
}

func (s *GroupBootstrapService) cleanupMTProtoUser(ctx context.Context, chatID int64) error {
	if selfUserID := s.mtproto.SelfUserID(); selfUserID != 0 {
		if err := s.groupOps.KickFromGroup(ctx, chatID, selfUserID, KickReasonMTProtoCleanup); err != nil {
			return fmt.Errorf("kick mtproto bootstrap user: %w", err)
		}
	}
	return nil
}

func (s *GroupBootstrapService) emit(ctx context.Context, outcome string, group ManagedGroup, counts groupBootstrapCounts, attempt int) {
	fields := map[string]string{
		"chat_id":    strconv.FormatInt(group.ChatID, 10),
		"creator_id": group.CreatorID,
		"policy":     string(group.Policy),
		"attempt":    strconv.Itoa(attempt),
	}
	if counts.tracked > 0 {
		fields["tracked"] = strconv.Itoa(counts.tracked)
	}
	if counts.untracked > 0 {
		fields["untracked"] = strconv.Itoa(counts.untracked)
	}
	if counts.kicked > 0 {
		fields["kicked"] = strconv.Itoa(counts.kicked)
	}
	s.events.Emit(ctx, events.Event{
		Name:    events.NameTelegramMTProtoBootstrap,
		Outcome: outcome,
		Fields:  fields,
	})
}

func classifyBootstrapError(err error) string {
	if err == nil {
		return "ok"
	}
	if stage, ok := mtproto.Stage(err); ok {
		return stage
	}
	return "failed"
}

func shouldSkipBootstrapMember(member mtproto.Member, selfUserID int64) bool {
	if member.TelegramUserID == 0 || member.TelegramUserID == selfUserID || member.IsBot {
		return true
	}
	return member.Role == mtproto.MemberRoleAdmin || member.Role == mtproto.MemberRoleCreator
}
