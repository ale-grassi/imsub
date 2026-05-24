package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"imsub/internal/transport/telegram/mtproto"
)

const untrackedMemberTag = "Untracked"

type memberTagSyncStore interface {
	ManagedGroupByChatID(ctx context.Context, chatID int64) (ManagedGroup, bool, error)
	ListManagedGroups(ctx context.Context) ([]ManagedGroup, error)
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	ListTrackedGroupMemberIDs(ctx context.Context, chatID int64) ([]int64, error)
	ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]UntrackedGroupMember, error)
	ListManagedMemberTags(ctx context.Context, chatID int64) ([]ManagedMemberTag, error)
	UpsertManagedMemberTag(ctx context.Context, item ManagedMemberTag) error
	RemoveManagedMemberTag(ctx context.Context, chatID, telegramUserID int64) error
}

type memberTagSetter interface {
	SetMemberTag(ctx context.Context, groupChatID, telegramUserID int64, tag string) error
}

type memberTagMemberSnapshot interface {
	DumpMembersByChatID(ctx context.Context, chatID int64) ([]mtproto.Member, error)
	SelfUserID() int64
}

// MemberTagSyncService applies and reconciles Telegram member tags managed by ImSub.
type MemberTagSyncService struct {
	store     memberTagSyncStore
	setter    memberTagSetter
	bootstrap *GroupBootstrapService
	snapshot  memberTagMemberSnapshot
	logger    *slog.Logger
	now       func() time.Time
}

// MemberTagSyncCounts reports a single sync run.
type MemberTagSyncCounts struct {
	Groups  int
	Set     int
	Cleared int
	Noop    int
	Errors  int
}

// NewMemberTagSyncService creates a member-tag sync service.
func NewMemberTagSyncService(store memberTagSyncStore, setter memberTagSetter, bootstrap *GroupBootstrapService, snapshot memberTagMemberSnapshot, logger *slog.Logger) *MemberTagSyncService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemberTagSyncService{
		store:     store,
		setter:    setter,
		bootstrap: bootstrap,
		snapshot:  snapshot,
		logger:    logger,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// ApplyTrackedMemberTag applies the tracked-member tag when per-group sync is enabled.
func (s *MemberTagSyncService) ApplyTrackedMemberTag(ctx context.Context, group ManagedGroup, telegramUserID int64) error {
	if !group.MemberTagSyncEnabled {
		return nil
	}
	identity, ok, err := s.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return fmt.Errorf("load user identity: %w", err)
	}
	if !ok {
		return nil
	}
	return s.setOwnedTag(ctx, group.ChatID, telegramUserID, trackedMemberTag(identity))
}

// ApplyUntrackedMemberTag applies the Untracked tag when per-group sync is enabled.
func (s *MemberTagSyncService) ApplyUntrackedMemberTag(ctx context.Context, group ManagedGroup, telegramUserID int64) error {
	if !group.MemberTagSyncEnabled {
		return nil
	}
	return s.setOwnedTag(ctx, group.ChatID, telegramUserID, untrackedMemberTag)
}

// ClearManagedTag clears an ImSub-owned tag for a specific member.
func (s *MemberTagSyncService) ClearManagedTag(ctx context.Context, chatID, telegramUserID int64) error {
	if s == nil || s.store == nil || s.setter == nil || chatID == 0 || telegramUserID == 0 {
		return nil
	}
	if err := s.setter.SetMemberTag(ctx, chatID, telegramUserID, ""); err != nil {
		if !isMemberTagUserGone(err) {
			return fmt.Errorf("clear member tag: %w", err)
		}
		// Telegram already has no clearable member here; drop our stale marker so
		// the periodic reconciler does not retry the same impossible clear forever.
		if err := s.store.RemoveManagedMemberTag(ctx, chatID, telegramUserID); err != nil {
			return fmt.Errorf("remove stale managed member tag: %w", err)
		}
		return nil
	}
	if err := s.store.RemoveManagedMemberTag(ctx, chatID, telegramUserID); err != nil {
		return fmt.Errorf("remove managed member tag: %w", err)
	}
	return nil
}

func isMemberTagUserGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "user_not_participant") || strings.Contains(msg, "user not participant")
}

// SyncGroup refreshes and reconciles member tags for one group.
func (s *MemberTagSyncService) SyncGroup(ctx context.Context, chatID int64, fullRefresh bool) (MemberTagSyncCounts, error) {
	if s == nil || s.store == nil || s.setter == nil {
		return MemberTagSyncCounts{}, nil
	}
	group, ok, err := s.store.ManagedGroupByChatID(ctx, chatID)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("load managed group: %w", err)
	}
	if !ok || !group.MemberTagSyncEnabled {
		return MemberTagSyncCounts{}, nil
	}
	if fullRefresh && s.bootstrap != nil {
		if err := s.bootstrap.BootstrapGroup(ctx, group); err != nil {
			return MemberTagSyncCounts{}, fmt.Errorf("bootstrap group: %w", err)
		}
	}
	return s.syncKnownMembers(ctx, group)
}

// CleanupGroup clears all ImSub-owned tags for one group.
func (s *MemberTagSyncService) CleanupGroup(ctx context.Context, chatID int64) (MemberTagSyncCounts, error) {
	if s == nil || s.store == nil || s.setter == nil || chatID == 0 {
		return MemberTagSyncCounts{}, nil
	}
	owned, err := s.store.ListManagedMemberTags(ctx, chatID)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("list managed member tags: %w", err)
	}
	counts := MemberTagSyncCounts{}
	for _, item := range owned {
		if err := s.ClearManagedTag(ctx, chatID, item.TelegramUserID); err != nil {
			counts.Errors++
			s.logger.Warn("clear managed member tag failed", "chat_id", chatID, "telegram_user_id", item.TelegramUserID, "error", err)
			continue
		}
		counts.Cleared++
	}
	return counts, nil
}

// SyncEnabledGroups reconciles member tags for all enabled groups.
func (s *MemberTagSyncService) SyncEnabledGroups(ctx context.Context) (MemberTagSyncCounts, error) {
	if s == nil || s.store == nil || s.setter == nil {
		return MemberTagSyncCounts{}, nil
	}
	groups, err := s.store.ListManagedGroups(ctx)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("list managed groups: %w", err)
	}
	total := MemberTagSyncCounts{}
	for _, group := range groups {
		if !group.MemberTagSyncEnabled {
			continue
		}
		total.Groups++
		counts, syncErr := s.syncKnownMembers(ctx, group)
		total.Set += counts.Set
		total.Cleared += counts.Cleared
		total.Noop += counts.Noop
		total.Errors += counts.Errors
		if syncErr != nil {
			total.Errors++
			s.logger.Warn("member tag group sync failed", "chat_id", group.ChatID, "creator_id", group.CreatorID, "error", syncErr)
		}
	}
	return total, nil
}

func (s *MemberTagSyncService) syncKnownMembers(ctx context.Context, group ManagedGroup) (MemberTagSyncCounts, error) {
	liveMembers, hasSnapshot, err := s.snapshotRegularMembers(ctx, group.ChatID)
	if err != nil {
		s.logger.Warn("member tag live-members snapshot failed; falling back to stored membership", "chat_id", group.ChatID, "error", err)
	}
	trackedIDs, err := s.store.ListTrackedGroupMemberIDs(ctx, group.ChatID)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("list tracked group members: %w", err)
	}
	untracked, err := s.store.ListUntrackedGroupMembers(ctx, group.ChatID)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("list untracked group members: %w", err)
	}
	desired := make(map[int64]string, len(trackedIDs)+len(untracked))
	for _, telegramUserID := range trackedIDs {
		if hasSnapshot {
			if _, ok := liveMembers[telegramUserID]; !ok {
				continue
			}
		}
		identity, ok, identityErr := s.store.UserIdentity(ctx, telegramUserID)
		if identityErr != nil {
			return MemberTagSyncCounts{}, fmt.Errorf("load tracked user identity %d: %w", telegramUserID, identityErr)
		}
		if !ok {
			continue
		}
		desired[telegramUserID] = trackedMemberTag(identity)
	}
	for _, member := range untracked {
		if hasSnapshot {
			if _, ok := liveMembers[member.TelegramUserID]; !ok {
				continue
			}
		}
		desired[member.TelegramUserID] = untrackedMemberTag
	}

	owned, err := s.store.ListManagedMemberTags(ctx, group.ChatID)
	if err != nil {
		return MemberTagSyncCounts{}, fmt.Errorf("list managed member tags: %w", err)
	}
	existing := make(map[int64]string, len(owned))
	for _, item := range owned {
		existing[item.TelegramUserID] = item.Tag
	}

	counts := MemberTagSyncCounts{}
	for telegramUserID, tag := range desired {
		if existing[telegramUserID] == tag {
			counts.Noop++
			continue
		}
		if err := s.setOwnedTag(ctx, group.ChatID, telegramUserID, tag); err != nil {
			counts.Errors++
			s.logger.Warn("set managed member tag failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "tag", tag, "error", err)
			continue
		}
		counts.Set++
	}
	for telegramUserID := range existing {
		if _, ok := desired[telegramUserID]; ok {
			continue
		}
		if err := s.ClearManagedTag(ctx, group.ChatID, telegramUserID); err != nil {
			counts.Errors++
			s.logger.Warn("clear stale managed member tag failed", "chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
			continue
		}
		counts.Cleared++
	}
	return counts, nil
}

func (s *MemberTagSyncService) snapshotRegularMembers(ctx context.Context, chatID int64) (map[int64]struct{}, bool, error) {
	if s == nil || s.snapshot == nil || chatID == 0 {
		return nil, false, nil
	}
	dumped, err := s.snapshot.DumpMembersByChatID(ctx, chatID)
	if err != nil {
		return nil, false, fmt.Errorf("dump members by chat id: %w", err)
	}
	selfUserID := s.snapshot.SelfUserID()
	members := make(map[int64]struct{}, len(dumped))
	for _, member := range dumped {
		if member.TelegramUserID == 0 || member.TelegramUserID == selfUserID || member.IsBot {
			continue
		}
		switch member.Role {
		case mtproto.MemberRoleMember, mtproto.MemberRoleRestricted:
			members[member.TelegramUserID] = struct{}{}
		case mtproto.MemberRoleAdmin, mtproto.MemberRoleCreator:
			continue
		}
	}
	return members, true, nil
}

func (s *MemberTagSyncService) setOwnedTag(ctx context.Context, chatID, telegramUserID int64, tag string) error {
	if s == nil || s.store == nil || s.setter == nil || chatID == 0 || telegramUserID == 0 {
		return nil
	}
	tag = sanitizeMemberTag(tag)
	if tag == "" {
		return nil
	}
	if err := s.setter.SetMemberTag(ctx, chatID, telegramUserID, tag); err != nil {
		return fmt.Errorf("set member tag: %w", err)
	}
	if err := s.store.UpsertManagedMemberTag(ctx, ManagedMemberTag{
		ChatID:         chatID,
		TelegramUserID: telegramUserID,
		Tag:            tag,
		UpdatedAt:      s.now(),
	}); err != nil {
		return fmt.Errorf("upsert managed member tag: %w", err)
	}
	return nil
}

func trackedMemberTag(identity UserIdentity) string {
	if name := strings.TrimSpace(identity.TwitchDisplayName); name != "" {
		return name
	}
	return identity.TwitchLogin
}

func sanitizeMemberTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if utf8.RuneCountInString(tag) <= 16 {
		return tag
	}
	out := make([]rune, 0, 16)
	for _, r := range tag {
		out = append(out, r)
		if len(out) == 16 {
			break
		}
	}
	return strings.TrimSpace(string(out))
}
