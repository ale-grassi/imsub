package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"imsub/internal/core"
)

// --- OAuth state ---

// SaveOAuthState persists an OAuth state payload with a time-to-live.
func (s *Store) SaveOAuthState(ctx context.Context, state string, payload core.OAuthStatePayload, ttl time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("json marshal oauth state: %w", err)
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, keyOAuthState(state), string(raw), ttl)
	if payload.TelegramUserID != 0 {
		pipe.SAdd(ctx, keyPrivacyOAuthStates(payload.TelegramUserID), keyOAuthState(state))
		if ttl > 0 {
			pipe.Expire(ctx, keyPrivacyOAuthStates(payload.TelegramUserID), ttl)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis set oauth state: %w", err)
	}
	return nil
}

// OAuthState retrieves the OAuth state payload for the given state token.
func (s *Store) OAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	raw, err := s.rdb.Get(ctx, keyOAuthState(state)).Result()
	if err != nil {
		return core.OAuthStatePayload{}, fmt.Errorf("redis get oauth state: %w", err)
	}
	var payload core.OAuthStatePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return core.OAuthStatePayload{}, fmt.Errorf("json unmarshal oauth state: %w", err)
	}
	return payload, nil
}

// DeleteOAuthState atomically retrieves and deletes the OAuth state payload.
func (s *Store) DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	raw, err := s.rdb.GetDel(ctx, keyOAuthState(state)).Result()
	if err != nil {
		return core.OAuthStatePayload{}, fmt.Errorf("redis getdel oauth state: %w", err)
	}
	var payload core.OAuthStatePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return core.OAuthStatePayload{}, fmt.Errorf("json unmarshal oauth state (delete): %w", err)
	}
	if payload.TelegramUserID != 0 {
		if err := s.rdb.SRem(ctx, keyPrivacyOAuthStates(payload.TelegramUserID), keyOAuthState(state)).Err(); err != nil {
			return core.OAuthStatePayload{}, fmt.Errorf("redis srem oauth state index: %w", err)
		}
	}
	return payload, nil
}
