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
	removedGroups      []int64
	disabledGroups     []int64
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

func (s *memberTagSyncStoreStub) RemoveManagedMemberTagsForGroup(_ context.Context, chatID int64) error {
	s.removedGroups = append(s.removedGroups, chatID)
	delete(s.managedTagsByGroup, chatID)
	return nil
}

func (s *memberTagSyncStoreStub) UpdateManagedGroupMemberTagSyncEnabled(_ context.Context, chatID int64, enabled bool) error {
	if !enabled {
		s.disabledGroups = append(s.disabledGroups, chatID)
	}
	if s.group.ChatID == chatID {
		s.group.MemberTagSyncEnabled = enabled
	}
	for i := range s.listGroups {
		if s.listGroups[i].ChatID == chatID {
			s.listGroups[i].MemberTagSyncEnabled = enabled
		}
	}
	return nil
}

type memberTagSetterStub struct {
	calls []ManagedMemberTag
	errs  map[int64]error
}

func (s *memberTagSetterStub) SetMemberTag(_ context.Context, groupChatID, telegramUserID int64, tag string) error {
	s.calls = append(s.calls, ManagedMemberTag{ChatID: groupChatID, TelegramUserID: telegramUserID, Tag: tag})
	if s.errs != nil {
		return s.errs[telegramUserID]
	}
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

func TestMemberTagSyncServiceClearManagedTagDropsStaleRecordWhenUserGone(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{}
	setter := &memberTagSetterStub{
		errs: map[int64]error{
			10: errors.New(`set chat member tag: telego: setChatMemberTag: internal execution: request call: 400 "Bad Request: USER_NOT_PARTICIPANT"`),
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)

	if err := svc.ClearManagedTag(t.Context(), 100, 10); err != nil {
		t.Fatalf("ClearManagedTag() error = %v", err)
	}
	if len(store.removed) != 1 || store.removed[0] != [2]int64{100, 10} {
		t.Fatalf("removed = %+v, want stale tag removed", store.removed)
	}
}

func TestMemberTagSyncServiceSyncGroupFallbackUserGoneRemovesMetadataWithoutError(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 10, Tag: "old"},
			},
		},
	}
	setter := &memberTagSetterStub{
		errs: map[int64]error{
			10: errors.New(`telego: setChatMemberTag: internal execution: request call: 400 "Bad Request: USER_NOT_PARTICIPANT"`),
		},
	}
	snapshot := &memberTagMemberSnapshotStub{err: errors.New("snapshot failed")}
	svc := NewMemberTagSyncService(store, setter, nil, snapshot, nil)

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("SyncGroup() error = %v", err)
	}
	if counts.Cleared != 1 || counts.Errors != 0 {
		t.Fatalf("counts = %+v, want cleared=1 errors=0", counts)
	}
	if len(setter.calls) != 1 || setter.calls[0].Tag != "" {
		t.Fatalf("setter calls = %+v, want one Telegram clear", setter.calls)
	}
	if len(store.removed) != 1 || store.removed[0] != [2]int64{100, 10} {
		t.Fatalf("removed = %+v, want stale metadata removed", store.removed)
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

func TestMemberTagSyncServiceApplyUntrackedMemberTagPermissionFailureDisablesGroup(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {{ChatID: 100, TelegramUserID: 10, Tag: "old"}},
		},
	}
	setter := &memberTagSetterStub{
		errs: map[int64]error{
			10: errors.New(`telego: setChatMemberTag: request call: 400 "Bad Request: CHAT_CREATOR_REQUIRED"`),
			11: errors.New(`telego: setChatMemberTag: request call: 400 "Bad Request: CHAT_CREATOR_REQUIRED"`),
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)

	err := svc.ApplyUntrackedMemberTag(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true}, 10)
	if err == nil {
		t.Fatal("ApplyUntrackedMemberTag() error = nil, want disabled error")
	}
	if !IsMemberTagSyncDisabledError(err) {
		t.Fatalf("ApplyUntrackedMemberTag() error = %v, want disabled error", err)
	}
	if len(store.disabledGroups) != 1 || store.disabledGroups[0] != 100 {
		t.Fatalf("disabledGroups = %+v, want [100]", store.disabledGroups)
	}
	if len(store.removedGroups) != 1 || store.removedGroups[0] != 100 {
		t.Fatalf("removedGroups = %+v, want [100]", store.removedGroups)
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
	if counts.TrackedStored != 2 || counts.UntrackedStored != 1 || counts.DesiredTracked != 1 || counts.DesiredUntracked != 1 || counts.ExistingTags != 1 {
		t.Fatalf("diagnostic counts = %+v, want stored tracked=2 untracked=1 desired tracked=1 untracked=1 existing=1", counts)
	}
	if counts.SnapshotMembers != 2 || counts.SnapshotFilteredTracked != 1 || counts.SnapshotFilteredUntracked != 0 {
		t.Fatalf("snapshot diagnostic counts = %+v, want members=2 filtered tracked=1 filtered untracked=0", counts)
	}
	if len(store.removed) != 1 || store.removed[0] != [2]int64{100, 30} {
		t.Fatalf("removed = %+v, want stale metadata removed without Telegram clear", store.removed)
	}
}

func TestMemberTagSyncServiceSyncGroupReportsEmptyDesiredWhenSnapshotFiltersAll(t *testing.T) {
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
	}
	setter := &memberTagSetterStub{}
	live := &memberTagMemberSnapshotStub{
		dump: map[int64][]mtproto.Member{
			100: {
				{TelegramUserID: 77, Role: mtproto.MemberRoleMember},
			},
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, live, nil)

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err != nil {
		t.Fatalf("SyncGroup() error = %v", err)
	}
	if counts.Set != 0 || counts.Cleared != 0 || counts.Noop != 0 || counts.Errors != 0 {
		t.Fatalf("operation counts = %+v, want no operations", counts)
	}
	if counts.TrackedStored != 1 || counts.UntrackedStored != 1 || counts.DesiredTracked != 0 || counts.DesiredUntracked != 0 {
		t.Fatalf("diagnostic counts = %+v, want stored tracked=1 untracked=1 desired=0", counts)
	}
	if counts.SnapshotMembers != 1 || counts.SnapshotFilteredTracked != 1 || counts.SnapshotFilteredUntracked != 1 {
		t.Fatalf("snapshot diagnostic counts = %+v, want members=1 filtered tracked=1 filtered untracked=1", counts)
	}
	if len(setter.calls) != 0 {
		t.Fatalf("setter calls = %+v, want none", setter.calls)
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

func TestMemberTagSyncServicePermissionFailureDisablesGroupAndStops(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		group:   ManagedGroup{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
		groupOK: true,
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchDisplayName: "Viewer One"},
			11: {TelegramUserID: 11, TwitchDisplayName: "Viewer Two"},
		},
		trackedByGroup: map[int64][]int64{100: {10, 11}},
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {
				{ChatID: 100, TelegramUserID: 30, Tag: "old"},
			},
		},
	}
	setter := &memberTagSetterStub{
		errs: map[int64]error{
			10: errors.New(`telego: setChatMemberTag: request call: 400 "Bad Request: CHAT_CREATOR_REQUIRED"`),
			11: errors.New(`telego: setChatMemberTag: request call: 400 "Bad Request: CHAT_CREATOR_REQUIRED"`),
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)

	counts, err := svc.SyncGroup(t.Context(), 100, false)
	if err == nil {
		t.Fatal("SyncGroup() error = nil, want permission error")
	}
	if counts.Errors != 1 {
		t.Fatalf("counts = %+v, want one group-level error", counts)
	}
	if len(store.disabledGroups) != 1 || store.disabledGroups[0] != 100 {
		t.Fatalf("disabledGroups = %+v, want [100]", store.disabledGroups)
	}
	if len(store.removedGroups) != 1 || store.removedGroups[0] != 100 {
		t.Fatalf("removedGroups = %+v, want [100]", store.removedGroups)
	}
	if len(setter.calls) != 1 {
		t.Fatalf("setter calls = %+v, want processing stopped after first failure", setter.calls)
	}
}

func TestMemberTagSyncServiceSyncEnabledGroupsContinuesAfterPermissionDisable(t *testing.T) {
	t.Parallel()

	store := &memberTagSyncStoreStub{
		listGroups: []ManagedGroup{
			{ChatID: 100, CreatorID: "c1", MemberTagSyncEnabled: true},
			{ChatID: 200, CreatorID: "c2", MemberTagSyncEnabled: true},
		},
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchDisplayName: "Viewer One"},
			20: {TelegramUserID: 20, TwitchDisplayName: "Viewer Two"},
		},
		trackedByGroup: map[int64][]int64{
			100: {10},
			200: {20},
		},
		managedTagsByGroup: map[int64][]ManagedMemberTag{
			100: {{ChatID: 100, TelegramUserID: 30, Tag: "old"}},
		},
	}
	setter := &memberTagSetterStub{
		errs: map[int64]error{
			10: errors.New(`telego: setChatMemberTag: request call: 400 "Bad Request: CHAT_CREATOR_REQUIRED"`),
		},
	}
	svc := NewMemberTagSyncService(store, setter, nil, nil, nil)

	counts, err := svc.SyncEnabledGroups(t.Context())
	if err != nil {
		t.Fatalf("SyncEnabledGroups() error = %v", err)
	}
	if counts.Groups != 2 || counts.Set != 1 || counts.Errors != 1 {
		t.Fatalf("counts = %+v, want groups=2 set=1 errors=1", counts)
	}
	if len(store.disabledGroups) != 1 || store.disabledGroups[0] != 100 {
		t.Fatalf("disabledGroups = %+v, want [100]", store.disabledGroups)
	}
	if len(setter.calls) != 2 {
		t.Fatalf("setter calls = %+v, want first group failure and second group set", setter.calls)
	}
	if setter.calls[1].ChatID != 200 || setter.calls[1].TelegramUserID != 20 {
		t.Fatalf("second setter call = %+v, want group 200 user 20", setter.calls[1])
	}
}
