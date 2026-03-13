package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

// ConsentRecord loads the stored consent record for a Telegram user.
func (s *Store) ConsentRecord(ctx context.Context, telegramUserID int64) (core.ConsentRecord, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, keyConsentRecord(telegramUserID)).Result()
	if err != nil {
		return core.ConsentRecord{}, false, fmt.Errorf("redis hgetall consent record: %w", err)
	}
	if len(vals) == 0 {
		return core.ConsentRecord{}, false, nil
	}
	grantedAt := parseGroupTime(vals["granted_at"])
	return core.ConsentRecord{
		TelegramUserID: telegramUserID,
		Language:       vals["language"],
		PolicyVersion:  vals["policy_version"],
		GrantedAt:      grantedAt,
	}, true, nil
}

// SaveConsentRecord persists explicit user consent.
func (s *Store) SaveConsentRecord(ctx context.Context, record core.ConsentRecord) error {
	if record.GrantedAt.IsZero() {
		record.GrantedAt = time.Now().UTC()
	}
	if err := s.rdb.HSet(ctx, keyConsentRecord(record.TelegramUserID), map[string]string{
		"telegram_user_id": strconv.FormatInt(record.TelegramUserID, 10),
		"language":         record.Language,
		"policy_version":   record.PolicyVersion,
		"granted_at":       record.GrantedAt.UTC().Format(time.RFC3339),
	}).Err(); err != nil {
		return fmt.Errorf("redis hset consent record: %w", err)
	}
	return nil
}

// DeleteConsentRecord removes the user's stored consent.
func (s *Store) DeleteConsentRecord(ctx context.Context, telegramUserID int64) error {
	if err := s.rdb.Del(ctx, keyConsentRecord(telegramUserID)).Err(); err != nil {
		return fmt.Errorf("redis del consent record: %w", err)
	}
	return nil
}

// ListUntrackedMembershipsForUser returns all untracked observations for a Telegram user.
func (s *Store) ListUntrackedMembershipsForUser(ctx context.Context, telegramUserID int64) ([]core.UntrackedGroupMember, error) {
	groups, err := s.ListManagedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed groups: %w", err)
	}
	out := make([]core.UntrackedGroupMember, 0)
	for _, group := range groups {
		vals, err := s.rdb.HGetAll(ctx, keyTrackedGroupMemberMeta(group.ChatID, telegramUserID)).Result()
		if err != nil {
			return nil, fmt.Errorf("redis hgetall group member meta: %w", err)
		}
		if len(vals) == 0 || vals["state"] != "untracked" {
			continue
		}
		out = append(out, core.UntrackedGroupMember{
			ChatID:         group.ChatID,
			TelegramUserID: telegramUserID,
			Source:         vals["source"],
			FirstSeenAt:    parseGroupTime(vals["first_seen_at"]),
			LastSeenAt:     parseGroupTime(vals["last_seen_at"]),
			LastStatus:     vals["last_status"],
		})
	}
	return out, nil
}

// DeleteAllUntrackedMembershipsForUser removes all untracked observations tied to a Telegram user.
func (s *Store) DeleteAllUntrackedMembershipsForUser(ctx context.Context, telegramUserID int64) (int, error) {
	items, err := s.ListUntrackedMembershipsForUser(ctx, telegramUserID)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if err := s.RemoveUntrackedGroupMember(ctx, item.ChatID, telegramUserID); err != nil {
			return 0, fmt.Errorf("remove untracked group member %d from %d: %w", telegramUserID, item.ChatID, err)
		}
	}
	return len(items), nil
}

// DeleteOAuthStatesForUser removes any pending OAuth-state records tied to the Telegram user.
func (s *Store) DeleteOAuthStatesForUser(ctx context.Context, telegramUserID int64) (int, error) {
	stateKeys, err := s.rdb.SMembers(ctx, keyPrivacyOAuthStates(telegramUserID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redis smembers privacy oauth states: %w", err)
	}
	if len(stateKeys) == 0 {
		return 0, nil
	}
	pipe := s.rdb.TxPipeline()
	for _, stateKey := range stateKeys {
		pipe.Del(ctx, stateKey)
	}
	pipe.Del(ctx, keyPrivacyOAuthStates(telegramUserID))
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redis exec delete oauth states for user: %w", err)
	}
	return len(stateKeys), nil
}

// SavePrivacyReceipt stores a privacy receipt with retention applied.
func (s *Store) SavePrivacyReceipt(ctx context.Context, receipt core.PrivacyReceipt, retention time.Duration) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal privacy receipt: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, keyPrivacyReceipt(receipt.TelegramUserID, receipt.ID), raw, retention)
	pipe.SAdd(ctx, keyPrivacyReceipts(receipt.TelegramUserID), receipt.ID)
	if retention > 0 {
		pipe.Expire(ctx, keyPrivacyReceipts(receipt.TelegramUserID), retention)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec save privacy receipt: %w", err)
	}
	return nil
}

// ListPrivacyReceipts loads retained privacy receipts for a Telegram user.
func (s *Store) ListPrivacyReceipts(ctx context.Context, telegramUserID int64) ([]core.PrivacyReceipt, error) {
	ids, err := s.rdb.SMembers(ctx, keyPrivacyReceipts(telegramUserID)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers privacy receipts: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]core.PrivacyReceipt, 0, len(ids))
	for _, id := range ids {
		raw, err := s.rdb.Get(ctx, keyPrivacyReceipt(telegramUserID, id)).Bytes()
		if errors.Is(err, redis.Nil) {
			_ = s.rdb.SRem(ctx, keyPrivacyReceipts(telegramUserID), id).Err()
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("redis get privacy receipt: %w", err)
		}
		var receipt core.PrivacyReceipt
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return nil, fmt.Errorf("unmarshal privacy receipt: %w", err)
		}
		out = append(out, receipt)
	}
	return out, nil
}

// DeletePrivacyArtifacts removes privacy-related records for a user while keeping one receipt when requested.
func (s *Store) DeletePrivacyArtifacts(ctx context.Context, telegramUserID int64, keepReceiptID string) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, keyConsentRecord(telegramUserID))
	stateKeys, err := s.rdb.SMembers(ctx, keyPrivacyOAuthStates(telegramUserID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis smembers privacy oauth states: %w", err)
	}
	for _, stateKey := range stateKeys {
		pipe.Del(ctx, stateKey)
	}
	pipe.Del(ctx, keyPrivacyOAuthStates(telegramUserID))

	receiptIDs, err := s.rdb.SMembers(ctx, keyPrivacyReceipts(telegramUserID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis smembers privacy receipts: %w", err)
	}
	for _, receiptID := range receiptIDs {
		if receiptID == keepReceiptID {
			continue
		}
		pipe.Del(ctx, keyPrivacyReceipt(telegramUserID, receiptID))
		pipe.SRem(ctx, keyPrivacyReceipts(telegramUserID), receiptID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec delete privacy artifacts: %w", err)
	}
	return nil
}

// PurgeExpiredPrivacyData removes stale untracked observations and expired receipt index references.
func (s *Store) PurgeExpiredPrivacyData(ctx context.Context, untrackedRetention time.Duration) (int, error) {
	purged := 0
	if untrackedRetention > 0 {
		deadline := time.Now().UTC().Add(-untrackedRetention)
		groups, err := s.ListManagedGroups(ctx)
		if err != nil {
			return 0, fmt.Errorf("list managed groups: %w", err)
		}
		for _, group := range groups {
			items, err := s.ListUntrackedGroupMembers(ctx, group.ChatID)
			if err != nil {
				return purged, err
			}
			for _, item := range items {
				if item.LastSeenAt.IsZero() || item.LastSeenAt.After(deadline) {
					continue
				}
				if err := s.RemoveUntrackedGroupMember(ctx, item.ChatID, item.TelegramUserID); err != nil {
					return purged, err
				}
				purged++
			}
		}
	}
	return purged, nil
}
