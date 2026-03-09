package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// UpsertUntrackedGroupMember records telegramUserID as observed but untracked in chatID.
func (s *Store) UpsertUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tgStr := strconv.FormatInt(telegramUserID, 10)
	metaKey := keyTrackedGroupMemberMeta(chatID, telegramUserID)
	firstSeen := at.UTC().Format(time.RFC3339)
	existingFirstSeen, _ := s.rdb.HGet(ctx, metaKey, "first_seen_at").Result()
	if existingFirstSeen != "" {
		firstSeen = existingFirstSeen
	}

	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyUntrackedGroupMembers(chatID), tgStr)
	pipe.HSet(ctx, metaKey, map[string]string{
		"state":         "untracked",
		"source":        source,
		"first_seen_at": firstSeen,
		"last_seen_at":  at.UTC().Format(time.RFC3339),
		"last_status":   status,
		"telegram_user": tgStr,
		"group_chat_id": strconv.FormatInt(chatID, 10),
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec upsert untracked group member: %w", err)
	}
	return nil
}

// RemoveUntrackedGroupMember removes telegramUserID from the untracked set for chatID.
func (s *Store) RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error {
	pipe := s.rdb.TxPipeline()
	pipe.SRem(ctx, keyUntrackedGroupMembers(chatID), strconv.FormatInt(telegramUserID, 10))
	pipe.Del(ctx, keyTrackedGroupMemberMeta(chatID, telegramUserID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec remove untracked group member: %w", err)
	}
	return nil
}

// CountUntrackedGroupMembers returns the number of untracked users seen in chatID.
func (s *Store) CountUntrackedGroupMembers(ctx context.Context, chatID int64) (int, error) {
	count, err := s.rdb.SCard(ctx, keyUntrackedGroupMembers(chatID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redis scard untracked group members: %w", err)
	}
	return int(count), nil
}
