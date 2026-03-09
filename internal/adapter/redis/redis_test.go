package redis

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	logger := slog.New(slog.DiscardHandler)
	return &Store{rdb: client, logger: logger}
}

func TestRemoveUserCreatorByTwitchRemovesTrackedGroupMembership(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if _, err := s.SaveUserIdentityOnly(ctx, 7, "tw-7", "login7", "en"); err != nil {
		t.Fatalf("SaveUserIdentityOnly failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 111, CreatorID: "creator-1", GroupName: "VIP"}); err != nil {
		t.Fatalf("UpsertManagedGroup failed: %v", err)
	}
	if err := s.AddTrackedGroupMember(ctx, 111, 7, "test", time.Now().UTC()); err != nil {
		t.Fatalf("AddTrackedGroupMember failed: %v", err)
	}

	telegramUserID, found, err := s.RemoveUserCreatorByTwitch(ctx, "tw-7", "creator-1")
	if err != nil {
		t.Fatalf("RemoveUserCreatorByTwitch failed: %v", err)
	}
	if !found || telegramUserID != 7 {
		t.Fatalf("RemoveUserCreatorByTwitch = (%d, %t), want (7, true)", telegramUserID, found)
	}

	tracked, err := s.IsTrackedGroupMember(ctx, 111, 7)
	if err != nil {
		t.Fatalf("IsTrackedGroupMember failed: %v", err)
	}
	if tracked {
		t.Fatal("expected tracked group membership to be removed")
	}
}

func TestListTrackedGroupIDsForUserRoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.AddTrackedGroupMember(ctx, 222, 42, "test", time.Now().UTC()); err != nil {
		t.Fatalf("AddTrackedGroupMember 222 failed: %v", err)
	}
	if err := s.AddTrackedGroupMember(ctx, 111, 42, "test", time.Now().UTC()); err != nil {
		t.Fatalf("AddTrackedGroupMember 111 failed: %v", err)
	}

	got, err := s.ListTrackedGroupIDsForUser(ctx, 42)
	if err != nil {
		t.Fatalf("ListTrackedGroupIDsForUser failed: %v", err)
	}
	slices.Sort(got)
	want := []int64{111, 222}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected tracked groups: got %v want %v", got, want)
	}
}

func TestDeleteAllUserDataRemovesTrackedGroupLinks(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.rdb.HSet(ctx, keyUserIdentity(42), map[string]any{
		"twitch_user_id": "tw-42",
		"twitch_login":   "login42",
		"language":       "en",
		"verified_at":    "2026-01-01T00:00:00Z",
	}).Err(); err != nil {
		t.Fatalf("seed user identity failed: %v", err)
	}
	if err := s.rdb.Set(ctx, keyTwitchToTelegram("tw-42"), "42", 0).Err(); err != nil {
		t.Fatalf("seed twitch mapping failed: %v", err)
	}
	if err := s.rdb.SAdd(ctx, keyUsersSet(), "42").Err(); err != nil {
		t.Fatalf("seed users set failed: %v", err)
	}
	if err := s.AddTrackedGroupMember(ctx, 500, 42, "test", time.Now().UTC()); err != nil {
		t.Fatalf("seed tracked group failed: %v", err)
	}
	metaKey := keyTrackedGroupMemberMeta(500, 42)
	if exists, err := s.rdb.Exists(ctx, metaKey).Result(); err != nil || exists != 1 {
		t.Fatalf("tracked member meta key should exist before delete, exists=%d err=%v", exists, err)
	}

	if err := s.DeleteAllUserData(ctx, 42); err != nil {
		t.Fatalf("DeleteAllUserData failed: %v", err)
	}

	if exists, err := s.rdb.Exists(ctx, keyUserIdentity(42)).Result(); err != nil || exists != 0 {
		t.Fatalf("identity key should be deleted, exists=%d err=%v", exists, err)
	}
	if exists, err := s.rdb.Exists(ctx, keyTwitchToTelegram("tw-42")).Result(); err != nil || exists != 0 {
		t.Fatalf("twitch mapping should be deleted, exists=%d err=%v", exists, err)
	}
	if groups, _ := s.rdb.SMembers(ctx, keyUserTrackedGroups(42)).Result(); len(groups) != 0 {
		t.Fatalf("tracked groups reverse index should be empty, got %v", groups)
	}
	if members, _ := s.rdb.SMembers(ctx, keyTrackedGroupMembers(500)).Result(); slices.Contains(members, "42") {
		t.Fatalf("tracked group members should not contain 42, got %v", members)
	}
	if exists, err := s.rdb.Exists(ctx, metaKey).Result(); err != nil || exists != 0 {
		t.Fatalf("tracked member meta key should be deleted, exists=%d err=%v", exists, err)
	}
}

func TestRemoveMembershipCleanupDeletesMetaKey(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.AddTrackedGroupMember(ctx, 111, 7, "test", time.Now().UTC()); err != nil {
		t.Fatalf("AddTrackedGroupMember failed: %v", err)
	}
	trackedMetaKey := keyTrackedGroupMemberMeta(111, 7)
	if err := s.RemoveTrackedGroupMember(ctx, 111, 7); err != nil {
		t.Fatalf("RemoveTrackedGroupMember failed: %v", err)
	}
	if exists, err := s.rdb.Exists(ctx, trackedMetaKey).Result(); err != nil || exists != 0 {
		t.Fatalf("tracked meta key should be deleted, exists=%d err=%v", exists, err)
	}

	if err := s.UpsertUntrackedGroupMember(ctx, 222, 8, "test", "member", time.Now().UTC()); err != nil {
		t.Fatalf("UpsertUntrackedGroupMember failed: %v", err)
	}
	untrackedMetaKey := keyTrackedGroupMemberMeta(222, 8)
	if err := s.RemoveUntrackedGroupMember(ctx, 222, 8); err != nil {
		t.Fatalf("RemoveUntrackedGroupMember failed: %v", err)
	}
	if exists, err := s.rdb.Exists(ctx, untrackedMetaKey).Result(); err != nil || exists != 0 {
		t.Fatalf("untracked meta key should be deleted, exists=%d err=%v", exists, err)
	}
}

func TestRepairTrackedGroupReverseIndex(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertCreator(ctx, core.Creator{ID: "c1", Name: "c1", OwnerTelegramID: 900}); err != nil {
		t.Fatalf("UpsertCreator c1 failed: %v", err)
	}
	if err := s.UpsertCreator(ctx, core.Creator{ID: "c2", Name: "c2", OwnerTelegramID: 901}); err != nil {
		t.Fatalf("UpsertCreator c2 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 501, CreatorID: "c1", GroupName: "A"}); err != nil {
		t.Fatalf("UpsertManagedGroup 501 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 502, CreatorID: "c2", GroupName: "B"}); err != nil {
		t.Fatalf("UpsertManagedGroup 502 failed: %v", err)
	}
	if err := s.rdb.SAdd(ctx, keyUsersSet(), "100", "101").Err(); err != nil {
		t.Fatalf("seed users set failed: %v", err)
	}
	if err := s.rdb.SAdd(ctx, keyTrackedGroupMembers(501), "100", "101").Err(); err != nil {
		t.Fatalf("seed group 501 members failed: %v", err)
	}
	if err := s.rdb.SAdd(ctx, keyTrackedGroupMembers(502), "100").Err(); err != nil {
		t.Fatalf("seed group 502 members failed: %v", err)
	}
	if err := s.rdb.SAdd(ctx, keyUserTrackedGroups(100), "502", "999").Err(); err != nil {
		t.Fatalf("seed user 100 tracked groups failed: %v", err)
	}

	indexUsers, repairedUsers, missingLinks, staleLinks, err := s.RepairTrackedGroupReverseIndex(ctx)
	if err != nil {
		t.Fatalf("RepairTrackedGroupReverseIndex failed: %v", err)
	}
	if indexUsers != 2 || repairedUsers != 2 || missingLinks != 2 || staleLinks != 1 {
		t.Fatalf("unexpected repair stats: users=%d repaired=%d missing=%d stale=%d", indexUsers, repairedUsers, missingLinks, staleLinks)
	}

	user100, _ := s.rdb.SMembers(ctx, keyUserTrackedGroups(100)).Result()
	slices.Sort(user100)
	if !slices.Equal(user100, []string{"501", "502"}) {
		t.Fatalf("unexpected user100 tracked groups: %v", user100)
	}
	user101, _ := s.rdb.SMembers(ctx, keyUserTrackedGroups(101)).Result()
	slices.Sort(user101)
	if !slices.Equal(user101, []string{"501"}) {
		t.Fatalf("unexpected user101 tracked groups: %v", user101)
	}
}

func TestListActiveCreatorGroups(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertCreator(ctx, core.Creator{ID: "c2", Name: "beta", OwnerTelegramID: 902}); err != nil {
		t.Fatalf("UpsertCreator c2 failed: %v", err)
	}
	if err := s.UpsertCreator(ctx, core.Creator{ID: "c1", Name: "alpha", OwnerTelegramID: 901}); err != nil {
		t.Fatalf("UpsertCreator c1 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 501, CreatorID: "c1", GroupName: "A"}); err != nil {
		t.Fatalf("UpsertManagedGroup 501 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 502, CreatorID: "c1", GroupName: "B"}); err != nil {
		t.Fatalf("UpsertManagedGroup 502 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 601, CreatorID: "c2", GroupName: "C"}); err != nil {
		t.Fatalf("UpsertManagedGroup 601 failed: %v", err)
	}

	got, err := s.ListActiveCreatorGroups(ctx)
	if err != nil {
		t.Fatalf("ListActiveCreatorGroups failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListActiveCreatorGroups()) = %d, want 2", len(got))
	}
	if got[0].Creator.ID != "c1" || got[1].Creator.ID != "c2" {
		t.Fatalf("creator order = [%s %s], want [c1 c2]", got[0].Creator.ID, got[1].Creator.ID)
	}
	if len(got[0].Groups) != 2 || len(got[1].Groups) != 1 {
		t.Fatalf("group counts = [%d %d], want [2 1]", len(got[0].Groups), len(got[1].Groups))
	}
}

func TestDeleteCreatorData(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.UpsertCreator(ctx, core.Creator{ID: "c1", Name: "c1", OwnerTelegramID: 900}); err != nil {
		t.Fatalf("UpsertCreator c1 failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, core.ManagedGroup{ChatID: 111, CreatorID: "c1", GroupName: "g-111"}); err != nil {
		t.Fatalf("UpsertManagedGroup c1 failed: %v", err)
	}

	count, names, err := s.DeleteCreatorData(ctx, 900)
	if err != nil {
		t.Fatalf("DeleteCreatorData failed: %v", err)
	}
	if count != 1 || !slices.Contains(names, "c1") {
		t.Fatalf("unexpected delete result: count=%d names=%v", count, names)
	}

	_, ok, err := s.Creator(ctx, "c1")
	if err != nil {
		t.Fatalf("Creator after delete failed: %v", err)
	}
	if ok {
		t.Fatal("expected creator to be deleted")
	}
}

func TestUpsertCreatorClearsZeroTimestampFields(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	authAt := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	syncAt := authAt.Add(5 * time.Minute)
	noticeAt := authAt.Add(10 * time.Minute)
	if err := s.UpsertCreator(ctx, core.Creator{
		ID:              "c1",
		Name:            "c1",
		OwnerTelegramID: 900,
		AuthStatus:      core.CreatorAuthReconnectRequired,
		AuthErrorCode:   "token_refresh_failed",
		AuthStatusAt:    authAt,
		LastSyncAt:      syncAt,
		LastNoticeAt:    noticeAt,
	}); err != nil {
		t.Fatalf("UpsertCreator seeded timestamps failed: %v", err)
	}

	if err := s.UpsertCreator(ctx, core.Creator{
		ID:              "c1",
		Name:            "c1",
		OwnerTelegramID: 900,
		AuthStatus:      core.CreatorAuthHealthy,
	}); err != nil {
		t.Fatalf("UpsertCreator clear timestamps failed: %v", err)
	}

	got, ok, err := s.Creator(ctx, "c1")
	if err != nil {
		t.Fatalf("Creator(c1) failed: %v", err)
	}
	if !ok {
		t.Fatal("Creator(c1) not found, want found")
	}
	if !got.AuthStatusAt.IsZero() {
		t.Fatalf("Creator(c1).AuthStatusAt = %v, want zero", got.AuthStatusAt)
	}
	if !got.LastSyncAt.IsZero() {
		t.Fatalf("Creator(c1).LastSyncAt = %v, want zero", got.LastSyncAt)
	}
	if !got.LastNoticeAt.IsZero() {
		t.Fatalf("Creator(c1).LastNoticeAt = %v, want zero", got.LastNoticeAt)
	}

	vals, err := s.rdb.HGetAll(ctx, keyCreator("c1")).Result()
	if err != nil {
		t.Fatalf("HGetAll(c1) failed: %v", err)
	}
	for _, field := range []string{"auth_status_changed_at", "last_subscriber_sync_at", "last_reconnect_notice_at"} {
		if _, ok := vals[field]; ok {
			t.Fatalf("persisted creator field %q still present after clear", field)
		}
	}
}

func TestCreatorLogsInvalidOptionalTimestampFields(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	var logBuf strings.Builder
	s.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	if err := s.rdb.HSet(ctx, keyCreator("c1"), map[string]any{
		"id":                       "c1",
		"name":                     "c1",
		"owner_telegram_id":        "900",
		"updated_at":               "2026-03-07T12:00:00Z",
		"auth_status_changed_at":   "not-a-time",
		"last_subscriber_sync_at":  "also-not-a-time",
		"last_reconnect_notice_at": "still-not-a-time",
	}).Err(); err != nil {
		t.Fatalf("seed creator hash failed: %v", err)
	}

	got, ok, err := s.Creator(ctx, "c1")
	if err != nil {
		t.Fatalf("Creator(c1) failed: %v", err)
	}
	if !ok {
		t.Fatal("Creator(c1) not found, want found")
	}
	if !got.AuthStatusAt.IsZero() || !got.LastSyncAt.IsZero() || !got.LastNoticeAt.IsZero() {
		t.Fatalf("Creator(c1) optional timestamps = %+v, want zero values", got)
	}

	logOutput := logBuf.String()
	for _, field := range []string{"auth_status_changed_at", "last_subscriber_sync_at", "last_reconnect_notice_at"} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("log output %q does not mention invalid field %q", logOutput, field)
		}
	}
}

func TestOAuthStateRoundTrip(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	payload := core.OAuthStatePayload{
		Mode:            core.OAuthModeViewer,
		TelegramUserID:  77,
		Language:        "en",
		PromptMessageID: 12,
	}

	if err := s.SaveOAuthState(ctx, "test-state", payload, 10*time.Minute); err != nil {
		t.Fatalf("SaveOAuthState failed: %v", err)
	}

	raw, err := s.rdb.Get(ctx, keyOAuthState("test-state")).Result()
	if err != nil {
		t.Fatalf("load oauth state failed: %v", err)
	}
	if raw == "" || !strings.Contains(raw, `"mode":"viewer"`) {
		t.Fatalf("unexpected persisted oauth payload: %q", raw)
	}

	got, err := s.DeleteOAuthState(ctx, "test-state")
	if err != nil {
		t.Fatalf("DeleteOAuthState failed: %v", err)
	}
	if got.Mode != core.OAuthModeViewer || got.TelegramUserID != 77 {
		t.Fatalf("unexpected payload: %+v", got)
	}

	_, err = s.rdb.Get(ctx, keyOAuthState("test-state")).Result()
	if err == nil {
		t.Fatal("expected state to be deleted after GetDel")
	}
}

func TestEnsureSchema(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("first EnsureSchema failed: %v", err)
	}

	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema failed: %v", err)
	}
}

func TestMarkEventProcessed(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	dup, err := s.EventProcessed(ctx, "msg-1")
	if err != nil {
		t.Fatalf("EventProcessed before mark failed: %v", err)
	}
	if dup {
		t.Fatal("expected unmarked event to not be duplicate")
	}

	dup, err = s.MarkEventProcessed(ctx, "msg-1", time.Hour)
	if err != nil {
		t.Fatalf("first MarkEventProcessed failed: %v", err)
	}
	if dup {
		t.Fatal("expected first call to not be duplicate")
	}

	dup, err = s.EventProcessed(ctx, "msg-1")
	if err != nil {
		t.Fatalf("EventProcessed after mark failed: %v", err)
	}
	if !dup {
		t.Fatal("expected marked event to be duplicate")
	}

	dup, err = s.MarkEventProcessed(ctx, "msg-1", time.Hour)
	if err != nil {
		t.Fatalf("second MarkEventProcessed failed: %v", err)
	}
	if !dup {
		t.Fatal("expected second call to be duplicate")
	}
}

func TestSubscriberCache(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.AddCreatorSubscriber(ctx, "c1", "tw-1"); err != nil {
		t.Fatalf("AddCreatorSubscriber failed: %v", err)
	}

	ok, err := s.IsCreatorSubscriber(ctx, "c1", "tw-1")
	if err != nil {
		t.Fatalf("IsCreatorSubscriber failed: %v", err)
	}
	if !ok {
		t.Fatal("expected subscriber to be present")
	}

	count, err := s.CreatorSubscriberCount(ctx, "c1")
	if err != nil {
		t.Fatalf("CreatorSubscriberCount failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	if err := s.RemoveCreatorSubscriber(ctx, "c1", "tw-1"); err != nil {
		t.Fatalf("RemoveCreatorSubscriber failed: %v", err)
	}

	ok, err = s.IsCreatorSubscriber(ctx, "c1", "tw-1")
	if err != nil {
		t.Fatalf("IsCreatorSubscriber after remove failed: %v", err)
	}
	if ok {
		t.Fatal("expected subscriber to be absent after removal")
	}
}

type fakeRedisError string

func (e fakeRedisError) Error() string { return string(e) }

func (e fakeRedisError) RedisError() {}

func TestIsDifferentTwitchLinkError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "non redis error", err: context.DeadlineExceeded, want: false},
		{name: "redis exact code", err: fakeRedisError("DIFFERENT_TWITCH"), want: true},
		{name: "redis err prefix", err: fakeRedisError("ERR DIFFERENT_TWITCH"), want: true},
		{name: "redis other code", err: fakeRedisError("ERR SOME_OTHER"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDifferentTwitchLinkError(tc.err)
			if got != tc.want {
				t.Fatalf("unexpected result: got=%v want=%v", got, tc.want)
			}
		})
	}
}
