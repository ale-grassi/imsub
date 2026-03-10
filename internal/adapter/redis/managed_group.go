package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"imsub/internal/core"
)

func parseGroupTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func (s *Store) parseManagedGroup(vals map[string]string, chatID int64) core.ManagedGroup {
	group := core.ManagedGroup{
		ChatID:       chatID,
		CreatorID:    vals["creator_id"],
		GroupName:    vals["group_name"],
		Policy:       core.GroupPolicy(vals["policy"]),
		RegisteredAt: parseGroupTime(vals["registered_at"]),
		UpdatedAt:    parseGroupTime(vals["updated_at"]),
	}
	if rawThreadID := vals["registration_thread_id"]; rawThreadID != "" {
		threadID, err := strconv.Atoi(rawThreadID)
		if err == nil && threadID > 0 {
			group.RegistrationThreadID = threadID
		}
	}
	if group.Policy == "" {
		group.Policy = core.GroupPolicyObserve
	}
	return group
}

// ManagedGroupByChatID returns the managed group for chatID, if present.
func (s *Store) ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, keyManagedGroup(chatID)).Result()
	if err != nil {
		return core.ManagedGroup{}, false, fmt.Errorf("redis hgetall managed group: %w", err)
	}
	if len(vals) == 0 {
		return core.ManagedGroup{}, false, nil
	}
	return s.parseManagedGroup(vals, chatID), true, nil
}

// ListManagedGroupsByCreator returns all managed groups linked to creatorID.
func (s *Store) ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error) {
	ids, err := s.rdb.SMembers(ctx, keyManagedGroupsByCreator(creatorID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers managed groups by creator: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	out := make([]core.ManagedGroup, 0, len(ids))
	for _, raw := range ids {
		chatID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListManagedGroupsByCreator invalid chat id, skipping", "creator_id", creatorID, "chat_id_raw", raw, "error", parseErr)
			continue
		}
		group, ok, getErr := s.ManagedGroupByChatID(ctx, chatID)
		if getErr != nil {
			return nil, getErr
		}
		if !ok {
			continue
		}
		out = append(out, group)
	}
	return out, nil
}

// ListManagedGroups returns all managed groups.
func (s *Store) ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error) {
	ids, err := s.rdb.SMembers(ctx, keyManagedGroupsSet()).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers managed groups: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]core.ManagedGroup, 0, len(ids))
	for _, raw := range ids {
		chatID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListManagedGroups invalid chat id, skipping", "chat_id_raw", raw, "error", parseErr)
			continue
		}
		group, ok, getErr := s.ManagedGroupByChatID(ctx, chatID)
		if getErr != nil {
			return nil, getErr
		}
		if ok {
			out = append(out, group)
		}
	}
	return out, nil
}

// ListTrackedGroupIDsForUser returns tracked group chat IDs for telegramUserID.
func (s *Store) ListTrackedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, error) {
	rawIDs, err := s.rdb.SMembers(ctx, keyUserTrackedGroups(telegramUserID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers user tracked groups: %w", err)
	}
	if len(rawIDs) == 0 {
		return nil, nil
	}
	out := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		chatID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListTrackedGroupIDsForUser invalid chat id, skipping", "telegram_user_id", telegramUserID, "chat_id_raw", raw, "error", parseErr)
			continue
		}
		out = append(out, chatID)
	}
	return out, nil
}

// ListTrackedGroupMemberIDs returns tracked Telegram user IDs for a managed group.
func (s *Store) ListTrackedGroupMemberIDs(ctx context.Context, chatID int64) ([]int64, error) {
	rawIDs, err := s.rdb.SMembers(ctx, keyTrackedGroupMembers(chatID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers tracked group members: %w", err)
	}
	if len(rawIDs) == 0 {
		return nil, nil
	}
	out := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		telegramUserID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListTrackedGroupMemberIDs invalid telegram user id, skipping", "chat_id", chatID, "telegram_user_id_raw", raw, "error", parseErr)
			continue
		}
		out = append(out, telegramUserID)
	}
	return out, nil
}

// UpsertManagedGroup creates or updates a managed group record and its indices.
func (s *Store) UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error {
	if group.Policy == "" {
		group.Policy = core.GroupPolicyObserve
	}
	now := time.Now().UTC()
	if group.RegisteredAt.IsZero() {
		group.RegisteredAt = now
	}
	group.UpdatedAt = now

	existing, ok, err := s.ManagedGroupByChatID(ctx, group.ChatID)
	if err != nil {
		return err
	}

	fields := map[string]string{
		"chat_id":       strconv.FormatInt(group.ChatID, 10),
		"creator_id":    group.CreatorID,
		"group_name":    group.GroupName,
		"policy":        string(group.Policy),
		"registered_at": group.RegisteredAt.UTC().Format(time.RFC3339),
		"updated_at":    group.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if group.RegistrationThreadID > 0 {
		fields["registration_thread_id"] = strconv.Itoa(group.RegistrationThreadID)
	}

	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, keyManagedGroup(group.ChatID), fields)
	pipe.SAdd(ctx, keyManagedGroupsSet(), strconv.FormatInt(group.ChatID, 10))
	pipe.SAdd(ctx, keyManagedGroupsByCreator(group.CreatorID), strconv.FormatInt(group.ChatID, 10))
	if ok && existing.CreatorID != "" && existing.CreatorID != group.CreatorID {
		pipe.SRem(ctx, keyManagedGroupsByCreator(existing.CreatorID), strconv.FormatInt(group.ChatID, 10))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec upsert managed group: %w", err)
	}
	return nil
}

// DeleteManagedGroup removes a managed group and its tracked/untracked indices.
func (s *Store) DeleteManagedGroup(ctx context.Context, chatID int64) error {
	group, ok, err := s.ManagedGroupByChatID(ctx, chatID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	tracked, err := s.rdb.SMembers(ctx, keyTrackedGroupMembers(chatID)).Result()
	if err != nil {
		return fmt.Errorf("redis smembers tracked group members: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	for _, raw := range tracked {
		tgID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			s.log().Warn("DeleteManagedGroup invalid tracked user id, skipping reverse-index cleanup", "chat_id", chatID, "telegram_user_id_raw", raw, "error", parseErr)
			continue
		}
		pipe.SRem(ctx, keyUserTrackedGroups(tgID), strconv.FormatInt(chatID, 10))
	}
	pipe.Del(ctx, keyManagedGroup(chatID))
	pipe.SRem(ctx, keyManagedGroupsSet(), strconv.FormatInt(chatID, 10))
	pipe.SRem(ctx, keyManagedGroupsByCreator(group.CreatorID), strconv.FormatInt(chatID, 10))
	pipe.Del(ctx, keyTrackedGroupMembers(chatID))
	pipe.Del(ctx, keyUntrackedGroupMembers(chatID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec delete managed group: %w", err)
	}
	return nil
}
