package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// AddTrackedGroupMember records telegramUserID as a tracked member of chatID.
func (s *Store) AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tgStr := strconv.FormatInt(telegramUserID, 10)
	chatStr := strconv.FormatInt(chatID, 10)
	metaKey := keyTrackedGroupMemberMeta(chatID, telegramUserID)

	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyTrackedGroupMembers(chatID), tgStr)
	pipe.SRem(ctx, keyUntrackedGroupMembers(chatID), tgStr)
	pipe.SAdd(ctx, keyUserTrackedGroups(telegramUserID), chatStr)
	pipe.HSet(ctx, metaKey, map[string]string{
		"state":         "tracked",
		"source":        source,
		"first_seen_at": at.UTC().Format(time.RFC3339),
		"last_seen_at":  at.UTC().Format(time.RFC3339),
		"last_status":   "member",
		"telegram_user": tgStr,
		"group_chat_id": chatStr,
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec add tracked group member: %w", err)
	}
	return nil
}

// RemoveTrackedGroupMember removes telegramUserID from the tracked set for chatID.
func (s *Store) RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error {
	tgStr := strconv.FormatInt(telegramUserID, 10)
	pipe := s.rdb.TxPipeline()
	pipe.SRem(ctx, keyTrackedGroupMembers(chatID), tgStr)
	pipe.SRem(ctx, keyUserTrackedGroups(telegramUserID), strconv.FormatInt(chatID, 10))
	pipe.Del(ctx, keyTrackedGroupMemberMeta(chatID, telegramUserID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec remove tracked group member: %w", err)
	}
	return nil
}

// IsTrackedGroupMember reports whether telegramUserID is tracked in chatID.
func (s *Store) IsTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) (bool, error) {
	res, err := s.rdb.SIsMember(ctx, keyTrackedGroupMembers(chatID), strconv.FormatInt(telegramUserID, 10)).Result()
	if err != nil {
		return false, fmt.Errorf("redis sismember tracked group member: %w", err)
	}
	return res, nil
}
