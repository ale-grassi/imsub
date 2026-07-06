package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

// UpsertUntrackedGroupMember records telegramUserID as observed but untracked in chatID.
func (s *Store) UpsertUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tgStr := strconv.FormatInt(telegramUserID, 10)
	metaKey := keyTrackedGroupMemberMeta(chatID, telegramUserID)

	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyUntrackedGroupMembers(chatID), tgStr)
	// HSETNX keeps the original first_seen_at without a read round trip and
	// without a read-modify-write race between concurrent upserts.
	pipe.HSetNX(ctx, metaKey, "first_seen_at", at.UTC().Format(time.RFC3339))
	pipe.HSet(ctx, metaKey, map[string]string{
		"state":         "untracked",
		"source":        source,
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

// ListUntrackedGroupMembers returns the observed-but-untracked users for chatID.
func (s *Store) ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
	rawIDs, err := s.rdb.SMembers(ctx, keyUntrackedGroupMembers(chatID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers untracked group members: %w", err)
	}
	if len(rawIDs) == 0 {
		return nil, nil
	}

	telegramUserIDs := make([]int64, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		telegramUserID, parseErr := strconv.ParseInt(rawID, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListUntrackedGroupMembers invalid telegram user id, skipping", "chat_id", chatID, "telegram_user_id_raw", rawID, "error", parseErr)
			continue
		}
		telegramUserIDs = append(telegramUserIDs, telegramUserID)
	}
	if len(telegramUserIDs) == 0 {
		return nil, nil
	}

	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(telegramUserIDs))
	for i, telegramUserID := range telegramUserIDs {
		cmds[i] = pipe.HGetAll(ctx, keyTrackedGroupMemberMeta(chatID, telegramUserID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis exec untracked group member meta: %w", err)
	}

	out := make([]core.UntrackedGroupMember, 0, len(telegramUserIDs))
	for i, telegramUserID := range telegramUserIDs {
		vals, getErr := cmds[i].Result()
		if getErr != nil {
			return nil, fmt.Errorf("redis hgetall untracked group member meta: %w", getErr)
		}
		if len(vals) == 0 || vals["state"] != "untracked" {
			continue
		}

		out = append(out, core.UntrackedGroupMember{
			ChatID:         chatID,
			TelegramUserID: telegramUserID,
			Source:         vals["source"],
			FirstSeenAt:    parseGroupTime(vals["first_seen_at"]),
			LastSeenAt:     parseGroupTime(vals["last_seen_at"]),
			LastStatus:     vals["last_status"],
		})
	}
	return out, nil
}
