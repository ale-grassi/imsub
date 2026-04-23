package core

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"imsub/internal/transport/telegram/mtproto"
)

type memberTagSyncStoreStub struct {
	group              ManagedGroup
	groupOK            bool
	listGroups         []ManagedGroup
	identities         map[int64]UserIdentity
	trackedByGroup     map[int64][]int64
	untrackedByGroup   map[int64][]UntrackedGroupMember
	managedTagsByGroup map[int64][]ManagedMemberTag
	upserted           []ManagedMemberTag
	removed            [][2]int64
}

func (s *memberTagSyncStoreStub) ManagedGroupByChatID(context.Context, int64) (ManagedGroup, bool, error) {
	return s.group, s.groupOK, nil
}

func (s *memberTagSyncStoreStub) ListManagedGroups(context.Context) ([]ManagedGroup, error) {
	return append([]ManagedGroup(nil), s.listGroups...), nil
}

func (s *memberTagSyncStoreStub) UserIdentity(_ context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	identity, ok := s.identities[telegramUserID]
	return identity, ok, nil
}

func (s *memberTagSyncStoreStub) ListTrackedGroupMemberIDs(_ context.Context, chatID int64) ([]int64, error) {
	return append([]int64(nil), s.trackedByGroup[chatID]...), nil
}

func (s *memberTagSyncStoreStub) ListUntrackedGroupMembers(_ context.Context, chatID int64) ([]UntrackedGroupMember, error) {
	return append([]UntrackedGroupMember(nil), s.untrackedByGroup[chatID]...), nil
}

func (s *memberTagSyncStoreStub) ListManagedMemberTags(_ context.Context, chatID int64) ([]ManagedMemberTag, error) {
	return append([]ManagedMemberTag(nil), s.managedTagsByGroup[chatID]...), nil
}

func (s *memberTagSyncStoreStub) UpsertManagedMemberTag(_ context.Context, item ManagedMemberTag) error {
	s.upserted = append(s.upserted, item)
	return nil
}

func (s *memberTagSyncStoreStub) RemoveManagedMemberTag(_ context.Context, chatID, telegramUserID int64) error {
	s.removed = append(s.removed, [2]int64{chatID, telegramUserID})
	return nil
}

type memberTagSetterStub struct {
	calls []ManagedMemberTag
}

func (s *memberTagSetterStub) SetMemberTag(_ context.Context, groupChatID, telegramUserID int64, tag string) error {
	s.calls = append(s.calls, ManagedMemberTag{ChatID: groupChatID, TelegramUserID: telegramUserID, Tag: tag})
	return nil
}

type memberTagMemberSnapshotStub struct {
	selfID int64
	dump   map[int64][]mtproto.Member
	err    error
}

func (s *memberTagMemberSnapshotStub) DumpMembersByChatID(_ context.Context, chatID int64) ([]mtproto.Member, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]mtproto.Member(nil), s.dump[chatID]...), nil
}

func (s *memberTagMemberSnapshotStub) SelfUserID() int64 {
	return s.selfID
}

func TestMemberTagSyncServiceSyncGroupSetsTrackedAndUntrackedAndClearsStale(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchDisplayName: "Viewer One"},
		},
		trackedByGroup: map[int64][]int64{100: {10}},
		untrackedByGroup: map[int64][]UntrackedGroupMember{
			100: {{ChatID: 100, TelegramUserID: 20}},
		},
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 30, Tag: "old"},
			},
		},
	}
	setter := &memberTagSetterStub{}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)
	svc.now = func() time.Time { return time.Unix(100, 0).UTC() }

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("SyncGroup() error = %v", err)
	}
	wantCalls := []ManagedMemberTag{
		{ChatID: 100, TelegramUserID: 10, Tag: "Viewer One"},
		{ChatID: 100, TelegramUserID: 20, Tag: "Untracked"},
		{ChatID: 100, TelegramUserID: 30, Tag: ""},
	}
	gotCalls := append([]ManagedMemberTag(nil), setter.calls...)
	slices.SortFunc(gotCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	slices.SortFunc(wantCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	if !slices.EqualFunc(gotCalls, wantCalls, func(a, b ManagedMemberTag) bool {
		return a.ChatID == b.ChatID && a.TelegramUserID == b.TelegramUserID && a.Tag == b.Tag
	}) {
		t.Fatalf("setter calls = %+v, want %+v", gotCalls, wantCalls)
	}
	if counts.Set != 2 || counts.Cleared != 1 {
		t.Fatalf("counts = %+v, want set=2 cleared=1", counts)
	}
}

func TestMemberTagSyncServiceCleanupGroupClearsOwnedTags(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 10, Tag: "viewer"},
				{ChatID: 100, TelegramUserID: 20, Tag: "Untracked"},
			},
		},
	}
	setter := &memberTagSetterStub{}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)

	counts, err := svc.CleanupGroup(t.Context(), 100)
	if err != nil {
		t.Fatalf("CleanupGroup() error = %v", err)
	}
	if counts.Cleared != 2 {
		t.Fatalf("counts = %+v, want cleared=2", counts)
	}
	for _, call := range setter.calls {
		if call.Tag != "" {
			t.Fatalf("clear call tag = %q, want empty", call.Tag)
		}
	}
}

func TestSanitizeMemberTagTruncatesToSixteenRunes(t *testing.T) {
	t.Parallel()

	got := sanitizeMemberTag("12345678901234567890")
	if got != "1234567890123456" {
		t.Fatalf("sanitizeMemberTag() = %q, want %q", got, "1234567890123456")
	}
}

func TestMemberTagSyncServiceApplyTrackedMemberTagSkipsUnknownIdentity(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{}
	setter := &memberTagSetterStub{}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)
	if err := svc.ApplyTrackedMemberTag(t.Context(), ManagedGroup{ChatID: 100, MemberTagSyncEnabled: true}, 99); err != nil {
		t.Fatalf("ApplyTrackedMemberTag() error = %v", err)
	}
	if len(setter.calls) != 0 {
		t.Fatalf("setter calls = %+v, want none", setter.calls)
	}
}

func TestMemberTagSyncServiceSyncEnabledGroupsNoopsWithoutSetter(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		listGroups: []ManagedGroup{{ChatID: 100, MemberTagSyncEnabled: true}},
		group:      ManagedGroup{ChatID: 100, MemberTagSyncEnabled: true},
		groupOK:    true,
	}
	svc := NewMemberTagSyncService(store, nil, nil, nil, nil)
	_, err := svc.SyncEnabledGroups(context.Background())
	if err != nil {
		t.Fatalf("SyncEnabledGroups() error = %v", err)
	}
}

func TestMemberTagSyncServiceSyncGroupUsesLiveMemberSnapshot(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchDisplayName: "Viewer One"},
			11: {TelegramUserID: 11, TwitchDisplayName: "Viewer Two"},
		},
		trackedByGroup: map[int64][]int64{100: {10, 11}},
		untrackedByGroup: map[int64][]UntrackedGroupMember{
			100: {{ChatID: 100, TelegramUserID: 20}},
		},
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 30, Tag: "old"},
			},
		},
	}
	setter := &memberTagSetterStub{}
	live := &memberTagMemberSnapshotStub{
		dump: map[int64][]mtproto.Member{
			100: {
				{TelegramUserID: 10, Role: mtproto.MemberRoleMember},
				{TelegramUserID: 20, Role: mtproto.MemberRoleMember},
				{TelegramUserID: 11, Role: mtproto.MemberRoleAdmin},
			},
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, live, nil)

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("SyncGroup() error = %v", err)
	}
	wantCalls := []ManagedMemberTag{
		{ChatID: 100, TelegramUserID: 10, Tag: "Viewer One"},
		{ChatID: 100, TelegramUserID: 20, Tag: "Untracked"},
		{ChatID: 100, TelegramUserID: 30, Tag: ""},
	}
	gotCalls := append([]ManagedMemberTag(nil), setter.calls...)
	slices.SortFunc(gotCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	slices.SortFunc(wantCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	if !slices.EqualFunc(gotCalls, wantCalls, func(a, b ManagedMemberTag) bool {
		return a.ChatID == b.ChatID && a.TelegramUserID == b.TelegramUserID && a.Tag == b.Tag
	}) {
		t.Fatalf("setter calls = %+v, want %+v", gotCalls, wantCalls)
	}
	if counts.Set != 2 || counts.Cleared != 1 {
		t.Fatalf("counts = %+v, want set=2 cleared=1", counts)
	}
}

func TestMemberTagSyncServiceSyncGroupFallsBackWhenSnapshotFails(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchDisplayName: "Viewer One"},
		},
		trackedByGroup: map[int64][]int64{100: {10}},
		untrackedByGroup: map[int64][]UntrackedGroupMember{
			100: {{ChatID: 100, TelegramUserID: 20}},
		},
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 30, Tag: "old"},
			},
		},
	}
	setter := &memberTagSetterStub{}
	snapshot := &memberTagMemberSnapshotStub{err: errors.New("snapshot failed")}
	svc := NewMemberTagSyncService(store, setter, nil, snapshot, nil)

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("SyncGroup() error = %v", err)
	}
	wantCalls := []ManagedMemberTag{
		{ChatID: 100, TelegramUserID: 10, Tag: "Viewer One"},
		{ChatID: 100, TelegramUserID: 20, Tag: "Untracked"},
		{ChatID: 100, TelegramUserID: 30, Tag: ""},
	}
	gotCalls := append([]ManagedMemberTag(nil), setter.calls...)
	slices.SortFunc(gotCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	slices.SortFunc(wantCalls, func(a, b ManagedMemberTag) int {
		return cmp.Compare(a.TelegramUserID, b.TelegramUserID)
	})
	if !slices.EqualFunc(gotCalls, wantCalls, func(a, b ManagedMemberTag) bool {
		return a.ChatID == b.ChatID && a.TelegramUserID == b.TelegramUserID && a.Tag == b.Tag
	}) {
		t.Fatalf("setter calls = %+v, want %+v", gotCalls, wantCalls)
	}
	if counts.Set != 2 || counts.Cleared != 1 {
		t.Fatalf("counts = %+v, want set=2 cleared=1", counts)
	}
}
