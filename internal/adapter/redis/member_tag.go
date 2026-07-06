package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

// UpsertManagedMemberTag records a Telegram member tag currently owned by ImSub.
func (s *Store) UpsertManagedMemberTag(ctx context.Context, item core.ManagedMemberTag) error {
	if item.ChatID == 0 || item.TelegramUserID == 0 {
		return nil
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	tgStr := strconv.FormatInt(item.TelegramUserID, 10)
	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyManagedMemberTags(item.ChatID), tgStr)
	pipe.HSet(ctx, keyManagedMemberTag(item.ChatID, item.TelegramUserID), map[string]string{
		"chat_id":          strconv.FormatInt(item.ChatID, 10),
		"telegram_user_id": tgStr,
		"tag":              item.Tag,
		"updated_at":       item.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec upsert managed member tag: %w", err)
	}
	return nil
}

// RemoveManagedMemberTag removes ImSub ownership metadata for a member tag.
func (s *Store) RemoveManagedMemberTag(ctx context.Context, chatID, telegramUserID int64) error {
	pipe := s.rdb.TxPipeline()
	pipe.SRem(ctx, keyManagedMemberTags(chatID), strconv.FormatInt(telegramUserID, 10))
	pipe.Del(ctx, keyManagedMemberTag(chatID, telegramUserID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec remove managed member tag: %w", err)
	}
	return nil
}

// RemoveManagedMemberTagsForGroup removes all ImSub ownership metadata for member tags in a group.
func (s *Store) RemoveManagedMemberTagsForGroup(ctx context.Context, chatID int64) error {
	rawIDs, err := s.rdb.SMembers(ctx, keyManagedMemberTags(chatID)).Result()
	if err != nil {
		return fmt.Errorf("redis smembers managed member tags: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	for _, rawID := range rawIDs {
		telegramUserID, parseErr := strconv.ParseInt(rawID, 10, 64)
		if parseErr != nil {
			s.log().Warn("RemoveManagedMemberTagsForGroup invalid telegram user id, skipping", "chat_id", chatID, "telegram_user_id_raw", rawID, "error", parseErr)
			continue
		}
		pipe.Del(ctx, keyManagedMemberTag(chatID, telegramUserID))
	}
	pipe.Del(ctx, keyManagedMemberTags(chatID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec remove managed member tags for group: %w", err)
	}
	return nil
}

// ListManagedMemberTags returns all member tags currently managed by ImSub for a group.
func (s *Store) ListManagedMemberTags(ctx context.Context, chatID int64) ([]core.ManagedMemberTag, error) {
	rawIDs, err := s.rdb.SMembers(ctx, keyManagedMemberTags(chatID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers managed member tags: %w", err)
	}
	if len(rawIDs) == 0 {
		return nil, nil
	}

	telegramUserIDs := make([]int64, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		telegramUserID, parseErr := strconv.ParseInt(rawID, 10, 64)
		if parseErr != nil {
			s.log().Warn("ListManagedMemberTags invalid telegram user id, skipping", "chat_id", chatID, "telegram_user_id_raw", rawID, "error", parseErr)
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
		cmds[i] = pipe.HGetAll(ctx, keyManagedMemberTag(chatID, telegramUserID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis exec managed member tags: %w", err)
	}

	out := make([]core.ManagedMemberTag, 0, len(telegramUserIDs))
	for i, telegramUserID := range telegramUserIDs {
		vals, getErr := cmds[i].Result()
		if getErr != nil {
			return nil, fmt.Errorf("redis hgetall managed member tag: %w", getErr)
		}
		if len(vals) == 0 {
			continue
		}
		out = append(out, core.ManagedMemberTag{
			ChatID:         chatID,
			TelegramUserID: telegramUserID,
			Tag:            vals["tag"],
			UpdatedAt:      parseGroupTime(vals["updated_at"]),
		})
	}
	return out, nil
}
