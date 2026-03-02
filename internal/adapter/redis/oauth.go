package redis

import (
	"context"
	"encoding/json"
	"time"

	"imsub/internal/core"
)

// --- OAuth state ---

// SaveOAuthState persists an OAuth state payload with a time-to-live.
func (s *Store) SaveOAuthState(ctx context.Context, state string, payload core.OAuthStatePayload, ttl time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, keyOAuthState(state), string(raw), ttl).Err()
}

// OAuthState retrieves the OAuth state payload for the given state token.
func (s *Store) OAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	raw, err := s.rdb.Get(ctx, keyOAuthState(state)).Result()
	if err != nil {
		return core.OAuthStatePayload{}, err
	}
	var payload core.OAuthStatePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return core.OAuthStatePayload{}, err
	}
	return payload, nil
}

// DeleteOAuthState atomically retrieves and deletes the OAuth state payload.
func (s *Store) DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error) {
	raw, err := s.rdb.GetDel(ctx, keyOAuthState(state)).Result()
	if err != nil {
		return core.OAuthStatePayload{}, err
	}
	var payload core.OAuthStatePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return core.OAuthStatePayload{}, err
	}
	return payload, nil
}
