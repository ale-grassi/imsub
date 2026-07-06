package redis

import (
	"testing"
	"time"

	"imsub/internal/core"
)

func TestBackupDirtyTrackingDedupsUntilRotation(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.rdb.Set(ctx, "imsub:dedup", "v1", 0).Err(); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:dedup").Result(); err != nil || !ok {
		t.Fatalf("dirty member after first write = (%t, %v), want true nil", ok, err)
	}

	// Empty the live dirty set out-of-band so a re-added mark is observable.
	if err := s.rdb.SRem(skipBackupTracking(ctx), keyBackupDirty(), "imsub:dedup").Err(); err != nil {
		t.Fatalf("srem dirty: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:dedup", "v2", 0).Err(); err != nil {
		t.Fatalf("second set: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:dedup").Result(); err != nil || ok {
		t.Fatalf("dirty member after deduped write = (%t, %v), want false nil", ok, err)
	}

	// After a tracking rotation the next write must mark the key again.
	s.rotateTrackingGeneration()
	if err := s.rdb.Set(ctx, "imsub:dedup", "v3", 0).Err(); err != nil {
		t.Fatalf("third set: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:dedup").Result(); err != nil || !ok {
		t.Fatalf("dirty member after rotation = (%t, %v), want true nil", ok, err)
	}
}

func TestBackupDeletedTrackingDedupsUntilRotation(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.rdb.Del(ctx, "imsub:gone").Err(); err != nil {
		t.Fatalf("first del: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), "imsub:gone").Result(); err != nil || !ok {
		t.Fatalf("deleted member after first del = (%t, %v), want true nil", ok, err)
	}

	if err := s.rdb.SRem(skipBackupTracking(ctx), keyBackupDeleted(), "imsub:gone").Err(); err != nil {
		t.Fatalf("srem deleted: %v", err)
	}
	if err := s.rdb.Del(ctx, "imsub:gone").Err(); err != nil {
		t.Fatalf("second del: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), "imsub:gone").Result(); err != nil || ok {
		t.Fatalf("deleted member after deduped del = (%t, %v), want false nil", ok, err)
	}

	s.rotateTrackingGeneration()
	if err := s.rdb.Del(ctx, "imsub:gone").Err(); err != nil {
		t.Fatalf("third del: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), "imsub:gone").Result(); err != nil || !ok {
		t.Fatalf("deleted member after rotation = (%t, %v), want true nil", ok, err)
	}
}

func TestOAuthStateKeysExcludedFromBackupTracking(t *testing.T) {
	t.Parallel()

	if isBackupExportedKey(keyOAuthState("abc123")) {
		t.Fatalf("isBackupExportedKey(%q) = true, want false", keyOAuthState("abc123"))
	}

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.SaveOAuthState(ctx, "abc123", core.OAuthStatePayload{TelegramUserID: 7}, time.Minute); err != nil {
		t.Fatalf("SaveOAuthState failed: %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), keyOAuthState("abc123")).Result(); err != nil || ok {
		t.Fatalf("oauth state in dirty set = (%t, %v), want absent", ok, err)
	}
	// The per-user privacy index of state keys stays exported; only the state
	// payloads themselves are excluded.
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), keyPrivacyOAuthStates(7)).Result(); err != nil || !ok {
		t.Fatalf("privacy oauth index in dirty set = (%t, %v), want present", ok, err)
	}
}

func TestJobLockKeysExcludedFromBackupTracking(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		keyMemberCleanupJobLock("42"),
		keySubscriptionEndGraceLock("creator:viewer"),
	} {
		if isBackupExportedKey(key) {
			t.Fatalf("isBackupExportedKey(%q) = true, want false", key)
		}
	}

	s := newTestStore(t)
	ctx := t.Context()
	if claimed, err := s.ClaimMemberCleanupJob(ctx, "42", 0); err != nil || !claimed {
		t.Fatalf("ClaimMemberCleanupJob = (%t, %v), want (true, nil)", claimed, err)
	}
	if n, err := s.rdb.SCard(skipBackupTracking(ctx), keyBackupDirty()).Result(); err != nil || n != 0 {
		t.Fatalf("dirty set size after lock claim = (%d, %v), want 0 nil", n, err)
	}
}
