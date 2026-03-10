package core

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"imsub/internal/events"
)

type blocklistFakeStore struct {
	creator                Creator
	ownedCreator           Creator
	ownedCreatorOK         bool
	updateEnabledCalls     []bool
	blockedDumpAdds        [][]string
	finalized              bool
	finalHasData           bool
	lastBanSyncUpdated     bool
	managedGroups          []ManagedGroup
	resolveTelegramUserID  int64
	resolveTelegramUserOK  bool
	removedTrackedGroupIDs []int64
	addedBlockedUsers      []string
	removedBlockedUsers    []string
}

func (f *blocklistFakeStore) Creator(context.Context, string) (Creator, bool, error) {
	return f.creator, f.creator.ID != "", nil
}

func (f *blocklistFakeStore) OwnedCreatorForUser(context.Context, int64) (Creator, bool, error) {
	return f.ownedCreator, f.ownedCreatorOK, nil
}

func (f *blocklistFakeStore) UpdateCreatorBlocklistSyncEnabled(_ context.Context, _ string, enabled bool) error {
	f.updateEnabledCalls = append(f.updateEnabledCalls, enabled)
	return nil
}

func (f *blocklistFakeStore) UpdateCreatorTokens(context.Context, string, string, string, []string) error {
	return nil
}

func (f *blocklistFakeStore) MarkCreatorAuthReconnectRequired(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}

func (f *blocklistFakeStore) MarkCreatorAuthHealthy(context.Context, string, time.Time) error {
	return nil
}

func (f *blocklistFakeStore) UpdateCreatorLastReconnectNotice(context.Context, string, time.Time) error {
	return nil
}

func (f *blocklistFakeStore) UpdateCreatorLastBanSync(context.Context, string, time.Time) error {
	f.lastBanSyncUpdated = true
	return nil
}

func (f *blocklistFakeStore) NewCreatorBlocklistDumpKey(string) string { return "tmp:blocklist" }

func (f *blocklistFakeStore) AddToCreatorBlocklistDump(_ context.Context, _ string, userIDs []string) error {
	f.blockedDumpAdds = append(f.blockedDumpAdds, append([]string(nil), userIDs...))
	return nil
}

func (f *blocklistFakeStore) FinalizeCreatorBlocklistDump(_ context.Context, _ string, _ string, hasData bool) error {
	f.finalized = true
	f.finalHasData = hasData
	return nil
}

func (f *blocklistFakeStore) CleanupCreatorBlocklistDump(context.Context, string) {}

func (f *blocklistFakeStore) AddCreatorBlockedUser(_ context.Context, _ string, twitchUserID string) error {
	f.addedBlockedUsers = append(f.addedBlockedUsers, twitchUserID)
	return nil
}

func (f *blocklistFakeStore) RemoveCreatorBlockedUser(_ context.Context, _ string, twitchUserID string) error {
	f.removedBlockedUsers = append(f.removedBlockedUsers, twitchUserID)
	return nil
}

func (f *blocklistFakeStore) ListManagedGroupsByCreator(context.Context, string) ([]ManagedGroup, error) {
	return append([]ManagedGroup(nil), f.managedGroups...), nil
}

func (f *blocklistFakeStore) ResolveTelegramUserIDByTwitch(context.Context, string) (int64, bool, error) {
	return f.resolveTelegramUserID, f.resolveTelegramUserOK, nil
}

func (f *blocklistFakeStore) RemoveTrackedGroupMember(_ context.Context, chatID, _ int64) error {
	f.removedTrackedGroupIDs = append(f.removedTrackedGroupIDs, chatID)
	return nil
}

type blocklistFakeTwitch struct {
	listBannedFn   func(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error)
	refreshTokenFn func(ctx context.Context, refreshToken string) (TokenResponse, error)
}

func (f *blocklistFakeTwitch) ExchangeCode(context.Context, string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("not implemented")
}
func (f *blocklistFakeTwitch) RefreshToken(ctx context.Context, refreshToken string) (TokenResponse, error) {
	if f.refreshTokenFn != nil {
		return f.refreshTokenFn(ctx, refreshToken)
	}
	return TokenResponse{}, errors.New("not implemented")
}
func (f *blocklistFakeTwitch) FetchUser(context.Context, string) (id, login, displayName string, err error) {
	return "", "", "", errors.New("not implemented")
}
func (f *blocklistFakeTwitch) CreateEventSub(context.Context, string, string, string) error {
	return errors.New("not implemented")
}
func (f *blocklistFakeTwitch) EnabledEventSubTypes(context.Context, string) (map[string]bool, error) {
	return nil, errors.New("not implemented")
}
func (f *blocklistFakeTwitch) ListSubscriberPage(context.Context, string, string, string) ([]string, string, error) {
	return nil, "", errors.New("not implemented")
}
func (f *blocklistFakeTwitch) ListBannedUserPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	if f.listBannedFn != nil {
		return f.listBannedFn(ctx, accessToken, broadcasterID, cursor)
	}
	return nil, "", errors.New("not implemented")
}
func (f *blocklistFakeTwitch) ListEventSubs(context.Context, ListEventSubsOpts) ([]EventSubSubscription, error) {
	return nil, errors.New("not implemented")
}
func (f *blocklistFakeTwitch) DeleteEventSub(context.Context, string) error {
	return errors.New("not implemented")
}

type blocklistFakeRevoker struct {
	kicked []int64
}

func (f *blocklistFakeRevoker) KickFromGroup(_ context.Context, groupChatID int64, _ int64, _ KickReason) error {
	f.kicked = append(f.kicked, groupChatID)
	return nil
}

type blocklistFakeObserver struct {
	events []events.Event
}

func (f *blocklistFakeObserver) Emit(_ context.Context, evt events.Event) {
	f.events = append(f.events, evt)
}

func TestCreatorBlocklistSyncCreatorBlocklist(t *testing.T) {
	t.Parallel()

	store := &blocklistFakeStore{
		managedGroups:         []ManagedGroup{{ChatID: 1001}, {ChatID: 1002}},
		resolveTelegramUserID: 77,
		resolveTelegramUserOK: true,
	}
	revoker := &blocklistFakeRevoker{}
	observer := &blocklistFakeObserver{}
	svc := NewCreatorBlocklistService(store, &blocklistFakeTwitch{
		listBannedFn: func(_ context.Context, _, _, cursor string) ([]string, string, error) {
			if cursor == "" {
				return []string{"tw-1"}, "next", nil
			}
			return []string{"tw-2"}, "", nil
		},
	}, revoker, nil)
	svc.SetObserver(observer)

	count, err := svc.SyncCreatorBlocklist(t.Context(), Creator{
		ID:                   "creator-1",
		AccessToken:          "token",
		BlocklistSyncEnabled: true,
	})
	if err != nil {
		t.Fatalf("SyncCreatorBlocklist() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("SyncCreatorBlocklist() count = %d, want 2", count)
	}
	if !store.finalized || !store.finalHasData || !store.lastBanSyncUpdated {
		t.Fatalf("sync state finalized=%v hasData=%v lastBanSyncUpdated=%v, want all true", store.finalized, store.finalHasData, store.lastBanSyncUpdated)
	}
	if !slices.Equal(store.removedTrackedGroupIDs, []int64{1001, 1002, 1001, 1002}) {
		t.Fatalf("removed tracked groups = %v, want repeated enforcement for both banned users", store.removedTrackedGroupIDs)
	}
	if !slices.Equal(revoker.kicked, []int64{1001, 1002, 1001, 1002}) {
		t.Fatalf("kicked groups = %v, want repeated enforcement for both banned users", revoker.kicked)
	}
	if len(observer.events) != 3 {
		t.Fatalf("observer events = %+v, want 3 events", observer.events)
	}
	if observer.events[0].Name != events.NameCreatorBlocklistEnforcement || observer.events[0].Count != 2 {
		t.Fatalf("observer first event = %+v, want blocklist enforcement count 2", observer.events[0])
	}
	if observer.events[1].Name != events.NameCreatorBlocklistEnforcement || observer.events[1].Count != 2 {
		t.Fatalf("observer second event = %+v, want blocklist enforcement count 2", observer.events[1])
	}
	if observer.events[2].Name != events.NameCreatorBlocklistSync || observer.events[2].Outcome != "ok" || observer.events[2].Count != 2 {
		t.Fatalf("observer final event = %+v, want blocklist sync ok count 2", observer.events[2])
	}
}

func TestCreatorBlocklistToggleEnablesSync(t *testing.T) {
	t.Parallel()

	store := &blocklistFakeStore{
		ownedCreator: Creator{
			ID:                   "creator-1",
			OwnerTelegramID:      77,
			GrantedScopes:        []string{ScopeModerationRead},
			BlocklistSyncEnabled: false,
		},
		ownedCreatorOK: true,
	}
	svc := NewCreatorBlocklistService(store, &blocklistFakeTwitch{
		listBannedFn: func(_ context.Context, _, _, _ string) ([]string, string, error) {
			return nil, "", nil
		},
	}, nil, nil)

	creator, count, err := svc.ToggleBlocklistSync(t.Context(), 77, true)
	if err != nil {
		t.Fatalf("ToggleBlocklistSync() error = %v", err)
	}
	if count != 0 || len(store.updateEnabledCalls) != 1 || !store.updateEnabledCalls[0] || !creator.BlocklistSyncEnabled {
		t.Fatalf("toggle result = creator=%+v count=%d updates=%v, want enabled state persisted", creator, count, store.updateEnabledCalls)
	}
}

func TestCreatorBlocklistToggleRequiresModerationScope(t *testing.T) {
	t.Parallel()

	store := &blocklistFakeStore{
		ownedCreator: Creator{
			ID:              "creator-1",
			OwnerTelegramID: 77,
		},
		ownedCreatorOK: true,
	}
	svc := NewCreatorBlocklistService(store, &blocklistFakeTwitch{}, nil, nil)

	_, _, err := svc.ToggleBlocklistSync(t.Context(), 77, true)
	if !errors.Is(err, ErrCreatorModerationScopeMissing) {
		t.Fatalf("ToggleBlocklistSync() error = %v, want ErrCreatorModerationScopeMissing", err)
	}
	if len(store.updateEnabledCalls) != 0 {
		t.Fatalf("UpdateCreatorBlocklistSyncEnabled calls = %v, want none", store.updateEnabledCalls)
	}
}

func TestHandleBanEventIgnoresTimeouts(t *testing.T) {
	t.Parallel()

	store := &blocklistFakeStore{
		creator: Creator{
			ID:                   "creator-1",
			BlocklistSyncEnabled: true,
		},
	}
	svc := NewCreatorBlocklistService(store, &blocklistFakeTwitch{}, nil, nil)

	if err := svc.HandleBanEvent(t.Context(), "creator-1", "tw-1", false); err != nil {
		t.Fatalf("HandleBanEvent() error = %v, want nil", err)
	}
	if len(store.addedBlockedUsers) != 0 {
		t.Fatalf("added blocked users = %v, want none for timeouts", store.addedBlockedUsers)
	}
}
