package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"imsub/internal/events"
)

type eventsubFakeStore struct {
	listActiveCreatorsFn  func(ctx context.Context) ([]Creator, error)
	reconnectCountFn      func(ctx context.Context) (int, error)
	updateTokensFn        func(ctx context.Context, creatorID, accessToken, refreshToken string, grantedScopes []string) error
	markReconnectFn       func(ctx context.Context, creatorID, errorCode string, at time.Time) (bool, error)
	markHealthyFn         func(ctx context.Context, creatorID string, at time.Time) error
	updateLastSyncFn      func(ctx context.Context, creatorID string, at time.Time) error
	updateLastNoticeFn    func(ctx context.Context, creatorID string, at time.Time) error
	newSubscriberDumpKey  func(creatorID string) string
	addToSubscriberDumpFn func(ctx context.Context, tmpKey string, userIDs []string) error
	finalizeDumpFn        func(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	cleanupDumpFn         func(ctx context.Context, tmpKey string)
}

func (f *eventsubFakeStore) ListActiveCreators(ctx context.Context) ([]Creator, error) {
	if f.listActiveCreatorsFn != nil {
		return f.listActiveCreatorsFn(ctx)
	}
	return nil, nil
}

func (f *eventsubFakeStore) CreatorAuthReconnectRequiredCount(ctx context.Context) (int, error) {
	if f.reconnectCountFn != nil {
		return f.reconnectCountFn(ctx)
	}
	return 0, nil
}

func (f *eventsubFakeStore) UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string, grantedScopes []string) error {
	if f.updateTokensFn != nil {
		return f.updateTokensFn(ctx, creatorID, accessToken, refreshToken, grantedScopes)
	}
	return nil
}

func (f *eventsubFakeStore) MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (bool, error) {
	if f.markReconnectFn != nil {
		return f.markReconnectFn(ctx, creatorID, errorCode, at)
	}
	return true, nil
}

func (f *eventsubFakeStore) MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error {
	if f.markHealthyFn != nil {
		return f.markHealthyFn(ctx, creatorID, at)
	}
	return nil
}

func (f *eventsubFakeStore) UpdateCreatorLastSync(ctx context.Context, creatorID string, at time.Time) error {
	if f.updateLastSyncFn != nil {
		return f.updateLastSyncFn(ctx, creatorID, at)
	}
	return nil
}

func (f *eventsubFakeStore) UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error {
	if f.updateLastNoticeFn != nil {
		return f.updateLastNoticeFn(ctx, creatorID, at)
	}
	return nil
}

func (f *eventsubFakeStore) NewSubscriberDumpKey(creatorID string) string {
	if f.newSubscriberDumpKey != nil {
		return f.newSubscriberDumpKey(creatorID)
	}
	return "tmp:" + creatorID
}

func (f *eventsubFakeStore) AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error {
	if f.addToSubscriberDumpFn != nil {
		return f.addToSubscriberDumpFn(ctx, tmpKey, userIDs)
	}
	return nil
}

func (f *eventsubFakeStore) FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error {
	if f.finalizeDumpFn != nil {
		return f.finalizeDumpFn(ctx, creatorID, tmpKey, hasData)
	}
	return nil
}

func (f *eventsubFakeStore) CleanupSubscriberDump(ctx context.Context, tmpKey string) {
	if f.cleanupDumpFn != nil {
		f.cleanupDumpFn(ctx, tmpKey)
	}
}

type eventSubFakeTwitch struct {
	exchangeCodeFn       func(ctx context.Context, code string) (TokenResponse, error)
	refreshTokenFn       func(ctx context.Context, refreshToken string) (TokenResponse, error)
	fetchUserFn          func(ctx context.Context, userToken string) (id, login, displayName string, err error)
	createEventSubFn     func(ctx context.Context, broadcasterID, eventType, version string) error
	enabledEventSubFn    func(ctx context.Context, creatorID string) (map[string]bool, error)
	listSubscriberPageFn func(ctx context.Context, accessToken, broadcasterID, cursor string) (userIDs []string, nextCursor string, err error)
	listBannedUserPageFn func(ctx context.Context, accessToken, broadcasterID, cursor string) (userIDs []string, nextCursor string, err error)
	listEventSubsFn      func(ctx context.Context, opts ListEventSubsOpts) ([]EventSubSubscription, error)
	deleteEventSubFn     func(ctx context.Context, subscriptionID string) error
}

type eventSubFakeNotifier struct {
	notified []Creator
	err      error
}

func (n *eventSubFakeNotifier) NotifyCreatorReconnectRequired(_ context.Context, creator Creator) error {
	n.notified = append(n.notified, creator)
	return n.err
}

type eventSubFakeObserver struct {
	events []events.Event
}

func (o *eventSubFakeObserver) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func (m *eventSubFakeTwitch) ExchangeCode(ctx context.Context, code string) (TokenResponse, error) {
	if m.exchangeCodeFn != nil {
		return m.exchangeCodeFn(ctx, code)
	}
	return TokenResponse{}, nil
}

func (m *eventSubFakeTwitch) RefreshToken(ctx context.Context, refreshToken string) (TokenResponse, error) {
	if m.refreshTokenFn != nil {
		return m.refreshTokenFn(ctx, refreshToken)
	}
	return TokenResponse{}, nil
}

func (m *eventSubFakeTwitch) FetchUser(ctx context.Context, userToken string) (id, login, displayName string, err error) {
	if m.fetchUserFn != nil {
		return m.fetchUserFn(ctx, userToken)
	}
	return "", "", "", nil
}

func (m *eventSubFakeTwitch) CreateEventSub(ctx context.Context, broadcasterID, eventType, version string) error {
	if m.createEventSubFn != nil {
		return m.createEventSubFn(ctx, broadcasterID, eventType, version)
	}
	return nil
}

func (m *eventSubFakeTwitch) EnabledEventSubTypes(ctx context.Context, creatorID string) (map[string]bool, error) {
	if m.enabledEventSubFn != nil {
		return m.enabledEventSubFn(ctx, creatorID)
	}
	return map[string]bool{EventTypeChannelSubscribe: true, EventTypeChannelSubEnd: true}, nil
}

func (m *eventSubFakeTwitch) ListSubscriberPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	if m.listSubscriberPageFn != nil {
		return m.listSubscriberPageFn(ctx, accessToken, broadcasterID, cursor)
	}
	return nil, "", nil
}

func (m *eventSubFakeTwitch) ListBannedUserPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	if m.listBannedUserPageFn != nil {
		return m.listBannedUserPageFn(ctx, accessToken, broadcasterID, cursor)
	}
	return nil, "", nil
}

func (m *eventSubFakeTwitch) ListEventSubs(ctx context.Context, opts ListEventSubsOpts) ([]EventSubSubscription, error) {
	if m.listEventSubsFn != nil {
		return m.listEventSubsFn(ctx, opts)
	}
	return nil, nil
}

func (m *eventSubFakeTwitch) DeleteEventSub(ctx context.Context, subscriptionID string) error {
	if m.deleteEventSubFn != nil {
		return m.deleteEventSubFn(ctx, subscriptionID)
	}
	return nil
}

func TestEnsureEventSubForCreators(t *testing.T) {
	t.Parallel()

	var got []string
	svc := NewEventSubService(
		&eventsubFakeStore{},
		&eventSubFakeTwitch{
			createEventSubFn: func(_ context.Context, broadcasterID, eventType, version string) error {
				if version != "1" {
					t.Fatalf("createEventSubFn() version = %q, want \"1\"", version)
				}
				got = append(got, broadcasterID+":"+eventType)
				return nil
			},
		},
		slog.New(slog.DiscardHandler),
	)

	err := svc.EnsureEventSubForCreators(t.Context(), []Creator{
		{ID: "c1"},
		{ID: "c2"},
	})
	if err != nil {
		t.Fatalf("EnsureEventSubForCreators(creators) returned error %v, want nil", err)
	}
	want := []string{
		"c1:" + EventTypeChannelSubscribe,
		"c1:" + EventTypeChannelSubEnd,
		"c2:" + EventTypeChannelSubscribe,
		"c2:" + EventTypeChannelSubEnd,
	}
	if !slices.Equal(got, want) {
		t.Errorf("EnsureEventSubForCreators(creators) create calls = %v, want %v", got, want)
	}
}

func TestDumpCurrentSubscribersRefreshOnUnauthorized(t *testing.T) {
	t.Parallel()

	var (
		added         []string
		cleanupKey    string
		finalized     bool
		finalHasData  bool
		updatedTokens bool
	)
	svc := NewEventSubService(
		&eventsubFakeStore{
			newSubscriberDumpKey: func(creatorID string) string { return "tmp:" + creatorID },
			addToSubscriberDumpFn: func(_ context.Context, _ string, userIDs []string) error {
				added = append(added, userIDs...)
				return nil
			},
			finalizeDumpFn: func(_ context.Context, creatorID, tmpKey string, hasData bool) error {
				if creatorID != "c1" || tmpKey != "tmp:c1" {
					t.Fatalf("finalizeDumpFn() args = creator=%q key=%q, want creator=\"c1\" key=\"tmp:c1\"", creatorID, tmpKey)
				}
				finalized = true
				finalHasData = hasData
				return nil
			},
			cleanupDumpFn: func(_ context.Context, tmpKey string) { cleanupKey = tmpKey },
			updateTokensFn: func(_ context.Context, creatorID, accessToken, refreshToken string, grantedScopes []string) error {
				updatedTokens = true
				if creatorID != "c1" || accessToken != "fresh" || refreshToken != "fresh-r" {
					t.Fatalf("updateTokensFn() args = creator=%q access=%q refresh=%q, want creator=\"c1\" access=\"fresh\" refresh=\"fresh-r\"", creatorID, accessToken, refreshToken)
				}
				if len(grantedScopes) != 0 {
					t.Fatalf("updateTokensFn() scopes = %v, want empty when refresh response omits scopes", grantedScopes)
				}
				return nil
			},
		},
		&eventSubFakeTwitch{
			listSubscriberPageFn: func(_ context.Context, accessToken, _ string, _ string) ([]string, string, error) {
				if accessToken == "expired" {
					return nil, "", fmt.Errorf("%w: 401", ErrUnauthorized)
				}
				return []string{"u1", "u2"}, "", nil
			},
			refreshTokenFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{AccessToken: "fresh", RefreshToken: "fresh-r"}, nil
			},
		},
		slog.New(slog.DiscardHandler),
	)

	total, err := svc.DumpCurrentSubscribers(t.Context(), Creator{
		ID: "c1", AccessToken: "expired", RefreshToken: "r1",
	})
	if err != nil {
		t.Fatalf("DumpCurrentSubscribers(creator) returned error %v, want nil", err)
	}
	if total != 2 {
		t.Errorf("DumpCurrentSubscribers(creator) total = %d, want %d", total, 2)
	}
	if !slices.Equal(added, []string{"u1", "u2"}) {
		t.Errorf("DumpCurrentSubscribers(creator) added IDs = %v, want %v", added, []string{"u1", "u2"})
	}
	if !finalized || !finalHasData {
		t.Errorf("DumpCurrentSubscribers(creator) finalized=(%v, hasData=%v), want finalized=true and hasData=true", finalized, finalHasData)
	}
	if cleanupKey != "tmp:c1" {
		t.Errorf("DumpCurrentSubscribers(creator) cleanup key = %q, want %q", cleanupKey, "tmp:c1")
	}
	if !updatedTokens {
		t.Errorf("DumpCurrentSubscribers(creator) updatedTokens = %t, want %t", updatedTokens, true)
	}
}

func TestDumpCurrentSubscribersBatchesDumpWrites(t *testing.T) {
	t.Parallel()

	firstPage := make([]string, dumpIDChunkSize-1)
	for i := range firstPage {
		firstPage[i] = fmt.Sprintf("u-%d", i)
	}
	secondPage := []string{"u-final"}
	var writes [][]string
	svc := NewEventSubService(
		&eventsubFakeStore{
			addToSubscriberDumpFn: func(_ context.Context, _ string, userIDs []string) error {
				writes = append(writes, append([]string(nil), userIDs...))
				return nil
			},
		},
		&eventSubFakeTwitch{
			listSubscriberPageFn: func(_ context.Context, _, _, cursor string) ([]string, string, error) {
				if cursor == "" {
					return firstPage, "next", nil
				}
				return secondPage, "", nil
			},
		},
		nil,
	)

	total, err := svc.DumpCurrentSubscribers(t.Context(), Creator{ID: "c1", AccessToken: "token"})
	if err != nil {
		t.Fatalf("DumpCurrentSubscribers() error = %v", err)
	}
	if total != dumpIDChunkSize {
		t.Fatalf("DumpCurrentSubscribers() total = %d, want %d", total, dumpIDChunkSize)
	}
	if len(writes) != 1 {
		t.Fatalf("dump write count = %d, want 1", len(writes))
	}
	if len(writes[0]) != dumpIDChunkSize {
		t.Fatalf("dump write size = %d, want %d", len(writes[0]), dumpIDChunkSize)
	}
}

func TestIsEventSubActiveForCreator(t *testing.T) {
	t.Parallel()

	svc := NewEventSubService(
		&eventsubFakeStore{},
		&eventSubFakeTwitch{
			enabledEventSubFn: func(_ context.Context, _ string) (map[string]bool, error) {
				return map[string]bool{
					EventTypeChannelSubscribe: true,
					EventTypeChannelSubEnd:    false,
				}, nil
			},
		},
		nil,
	)

	active, err := svc.IsEventSubActiveForCreator(t.Context(), "creator-1")
	if err != nil {
		t.Fatalf("IsEventSubActiveForCreator(%q) returned error %v, want nil", "creator-1", err)
	}
	if active {
		t.Errorf("IsEventSubActiveForCreator(%q) = %t, want %t", "creator-1", active, false)
	}
}

func TestDumpCurrentSubscribersPropagatesStoreErrors(t *testing.T) {
	t.Parallel()

	svc := NewEventSubService(
		&eventsubFakeStore{
			newSubscriberDumpKey: func(string) string { return "tmp:c1" },
			addToSubscriberDumpFn: func(context.Context, string, []string) error {
				return errors.New("write failed")
			},
		},
		&eventSubFakeTwitch{
			listSubscriberPageFn: func(_ context.Context, _, _, _ string) ([]string, string, error) {
				return []string{"u1"}, "", nil
			},
		},
		nil,
	)

	_, err := svc.DumpCurrentSubscribers(t.Context(), Creator{ID: "c1", AccessToken: "token"})
	if err == nil {
		t.Fatal("DumpCurrentSubscribers(c1) error = nil, want non-nil")
	}
}

func TestDumpCurrentSubscribersMarksReconnectRequiredOnceOnRefreshFailure(t *testing.T) {
	t.Parallel()

	var (
		markCalls   int
		noticeCalls int
	)
	notifier := &eventSubFakeNotifier{}
	observer := &eventSubFakeObserver{}
	svc := NewEventSubService(
		&eventsubFakeStore{
			markReconnectFn: func(_ context.Context, creatorID, errorCode string, _ time.Time) (bool, error) {
				markCalls++
				if creatorID != "c1" || errorCode != creatorAuthErrorTokenRefreshFailed {
					t.Fatalf("markReconnectFn() args = creatorID=%q errorCode=%q", creatorID, errorCode)
				}
				return true, nil
			},
			updateLastNoticeFn: func(_ context.Context, creatorID string, _ time.Time) error {
				noticeCalls++
				if creatorID != "c1" {
					t.Fatalf("updateLastNoticeFn() creatorID = %q, want %q", creatorID, "c1")
				}
				return nil
			},
			reconnectCountFn: func(context.Context) (int, error) { return 1, nil },
		},
		&eventSubFakeTwitch{
			listSubscriberPageFn: func(_ context.Context, _, _, _ string) ([]string, string, error) {
				return nil, "", fmt.Errorf("%w: 401", ErrUnauthorized)
			},
			refreshTokenFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{}, errors.New("refresh failed")
			},
		},
		nil,
	)
	svc.SetNotifier(notifier)
	svc.SetObserver(observer)

	_, err := svc.DumpCurrentSubscribers(t.Context(), Creator{
		ID:              "c1",
		OwnerTelegramID: 77,
		AccessToken:     "expired",
		RefreshToken:    "refresh",
	})
	if err == nil {
		t.Fatal("DumpCurrentSubscribers() error = nil, want non-nil")
	}
	if markCalls != 1 {
		t.Fatalf("mark reconnect calls = %d, want 1", markCalls)
	}
	if noticeCalls != 1 {
		t.Fatalf("notice timestamp calls = %d, want 1", noticeCalls)
	}
	if len(notifier.notified) != 1 || notifier.notified[0].ID != "c1" {
		t.Fatalf("notified creators = %+v, want one creator c1", notifier.notified)
	}
	wantEvents := []events.Event{
		{Name: events.NameCreatorTokenRefresh, Outcome: "failed", Fields: map[string]string{"creator_id": "c1"}},
		{Name: events.NameCreatorAuthTransition, Fields: map[string]string{
			"creator_id": "c1",
			"from":       string(CreatorAuthHealthy),
			"to":         string(CreatorAuthReconnectRequired),
			"reason":     creatorAuthErrorTokenRefreshFailed,
		}},
		{Name: events.NameCreatorsReconnectRequired, Count: 1},
		{Name: events.NameCreatorReconnectNotice, Outcome: "ok", Fields: map[string]string{"creator_id": "c1"}},
	}
	if !slices.EqualFunc(observer.events, wantEvents, func(a, b events.Event) bool {
		return a.Name == b.Name && a.Outcome == b.Outcome && a.Count == b.Count && eventSubMapsEqual(a.Fields, b.Fields)
	}) {
		t.Fatalf("events = %+v, want %+v", observer.events, wantEvents)
	}
}

func eventSubMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestDeleteEventSubsForCreator(t *testing.T) {
	t.Parallel()

	var deleted []string
	svc := NewEventSubService(
		&eventsubFakeStore{},
		&eventSubFakeTwitch{
			listEventSubsFn: func(_ context.Context, opts ListEventSubsOpts) ([]EventSubSubscription, error) {
				if opts.UserID != "c1" {
					t.Fatalf("listEventSubsFn() opts.UserID = %q, want %q", opts.UserID, "c1")
				}
				return []EventSubSubscription{
					{ID: "sub1", Type: EventTypeChannelSubscribe, BroadcasterID: "c1"},
					{ID: "sub2", Type: EventTypeChannelSubEnd, BroadcasterID: "c2"},
				}, nil
			},
			deleteEventSubFn: func(_ context.Context, subID string) error {
				deleted = append(deleted, subID)
				return nil
			},
		},
		slog.New(slog.DiscardHandler),
	)

	err := svc.DeleteEventSubsForCreator(t.Context(), "c1")
	if err != nil {
		t.Fatalf("DeleteEventSubsForCreator(c1) returned error %v, want nil", err)
	}
	want := []string{"sub1"}
	if !slices.Equal(deleted, want) {
		t.Errorf("DeleteEventSubsForCreator(c1) deleted = %v, want %v", deleted, want)
	}
}

func TestReconcileEventSubsOnce(t *testing.T) {
	t.Parallel()

	var (
		deleted []string
		created []string
	)
	svc := NewEventSubService(
		&eventsubFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{{ID: "c1"}, {ID: "c2"}}, nil
			},
		},
		&eventSubFakeTwitch{
			listEventSubsFn: func(_ context.Context, opts ListEventSubsOpts) ([]EventSubSubscription, error) {
				if opts.UserID != "" {
					t.Fatalf("listEventSubsFn() opts.UserID = %q, want empty", opts.UserID)
				}
				return []EventSubSubscription{
					{ID: "sub1", Status: "enabled", Type: EventTypeChannelSubscribe, BroadcasterID: "c1"},
					{ID: "sub2", Status: "enabled", Type: EventTypeChannelSubEnd, BroadcasterID: "c1"},
					{ID: "sub4", Status: "enabled", Type: EventTypeChannelSubscribe, BroadcasterID: "c2"},
					{ID: "sub-pending", Status: "webhook_callback_verification_pending", Type: EventTypeChannelSubEnd, BroadcasterID: "c2"},
					{ID: "sub-orphan", Status: "enabled", Type: EventTypeChannelSubscribe, BroadcasterID: "c-gone"},
				}, nil
			},
			deleteEventSubFn: func(_ context.Context, subID string) error {
				deleted = append(deleted, subID)
				return nil
			},
			createEventSubFn: func(_ context.Context, broadcasterID, eventType, _ string) error {
				created = append(created, broadcasterID+":"+eventType)
				return nil
			},
		},
		slog.New(slog.DiscardHandler),
	)

	err := svc.ReconcileEventSubsOnce(t.Context())
	if err != nil {
		t.Fatalf("ReconcileEventSubsOnce() returned error %v, want nil", err)
	}

	// Should delete orphaned sub for c-gone.
	if !slices.Equal(deleted, []string{"sub-orphan"}) {
		t.Errorf("ReconcileEventSubsOnce() deleted = %v, want %v", deleted, []string{"sub-orphan"})
	}

	// Should create missing subs for c2.
	wantCreated := []string{
		"c2:" + EventTypeChannelSubscribe,
		"c2:" + EventTypeChannelSubEnd,
	}
	if !slices.Equal(created, wantCreated) {
		t.Errorf("ReconcileEventSubsOnce() created = %v, want %v", created, wantCreated)
	}
}

func TestReconcileEventSubsOnceCreatesBanSubscriptionsForBlocklistCreators(t *testing.T) {
	t.Parallel()

	var created []string
	svc := NewEventSubService(
		&eventsubFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{{ID: "c1", BlocklistSyncEnabled: true}}, nil
			},
		},
		&eventSubFakeTwitch{
			listEventSubsFn: func(_ context.Context, _ ListEventSubsOpts) ([]EventSubSubscription, error) {
				return []EventSubSubscription{
					{ID: "sub1", Status: "enabled", Type: EventTypeChannelSubscribe, BroadcasterID: "c1"},
					{ID: "sub2", Status: "enabled", Type: EventTypeChannelSubEnd, BroadcasterID: "c1"},
				}, nil
			},
			createEventSubFn: func(_ context.Context, broadcasterID, eventType, _ string) error {
				created = append(created, broadcasterID+":"+eventType)
				return nil
			},
		},
		slog.New(slog.DiscardHandler),
	)

	if err := svc.ReconcileEventSubsOnce(t.Context()); err != nil {
		t.Fatalf("ReconcileEventSubsOnce() returned error %v, want nil", err)
	}

	want := []string{
		"c1:" + EventTypeChannelSubscribe,
		"c1:" + EventTypeChannelSubEnd,
		"c1:" + EventTypeChannelBan,
		"c1:" + EventTypeChannelUnban,
	}
	if !slices.Equal(created, want) {
		t.Fatalf("ReconcileEventSubsOnce() created = %v, want %v", created, want)
	}
}
