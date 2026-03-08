package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"
)

type eventsubFakeStore struct {
	Store
	listActiveCreatorsFn  func(ctx context.Context) ([]Creator, error)
	reconnectCountFn      func(ctx context.Context) (int, error)
	updateTokensFn        func(ctx context.Context, creatorID, accessToken, refreshToken string) error
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

func (f *eventsubFakeStore) UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string) error {
	if f.updateTokensFn != nil {
		return f.updateTokensFn(ctx, creatorID, accessToken, refreshToken)
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
	getAppTokenFn        func(ctx context.Context) (string, error)
	createEventSubFn     func(ctx context.Context, appToken, broadcasterID, eventType, version string) error
	enabledEventSubFn    func(ctx context.Context, appToken, creatorID string) (map[string]bool, error)
	listSubscriberPageFn func(ctx context.Context, accessToken, broadcasterID, cursor string) (userIDs []string, nextCursor string, err error)
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
	refreshes      []string
	transitions    []string
	reconnectGauge []int
	notifications  []string
}

func (o *eventSubFakeObserver) CreatorTokenRefresh(result string) {
	o.refreshes = append(o.refreshes, result)
}

func (o *eventSubFakeObserver) CreatorAuthTransition(from, to, reason string) {
	o.transitions = append(o.transitions, from+"->"+to+":"+reason)
}

func (o *eventSubFakeObserver) CreatorsReconnectRequired(count int) {
	o.reconnectGauge = append(o.reconnectGauge, count)
}

func (o *eventSubFakeObserver) CreatorReconnectNotification(result string) {
	o.notifications = append(o.notifications, result)
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

func (m *eventSubFakeTwitch) AppToken(ctx context.Context) (string, error) {
	if m.getAppTokenFn != nil {
		return m.getAppTokenFn(ctx)
	}
	return "app-token", nil
}

func (m *eventSubFakeTwitch) CreateEventSub(ctx context.Context, appToken, broadcasterID, eventType, version string) error {
	if m.createEventSubFn != nil {
		return m.createEventSubFn(ctx, appToken, broadcasterID, eventType, version)
	}
	return nil
}

func (m *eventSubFakeTwitch) EnabledEventSubTypes(ctx context.Context, appToken, creatorID string) (map[string]bool, error) {
	if m.enabledEventSubFn != nil {
		return m.enabledEventSubFn(ctx, appToken, creatorID)
	}
	return map[string]bool{EventTypeChannelSubscribe: true, EventTypeChannelSubEnd: true, EventTypeChannelSubGift: true}, nil
}

func (m *eventSubFakeTwitch) ListSubscriberPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	if m.listSubscriberPageFn != nil {
		return m.listSubscriberPageFn(ctx, accessToken, broadcasterID, cursor)
	}
	return nil, "", nil
}

func TestEnsureEventSubForCreators(t *testing.T) {
	t.Parallel()

	var got []string
	svc := NewEventSub(
		&eventsubFakeStore{},
		&eventSubFakeTwitch{
			createEventSubFn: func(_ context.Context, appToken, broadcasterID, eventType, version string) error {
				if appToken != "app-token" {
					t.Fatalf("createEventSubFn() appToken = %q, want \"app-token\"", appToken)
				}
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
		"c1:" + EventTypeChannelSubGift,
		"c2:" + EventTypeChannelSubscribe,
		"c2:" + EventTypeChannelSubEnd,
		"c2:" + EventTypeChannelSubGift,
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
	svc := NewEventSub(
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
			updateTokensFn: func(_ context.Context, creatorID, accessToken, refreshToken string) error {
				updatedTokens = true
				if creatorID != "c1" || accessToken != "fresh" || refreshToken != "fresh-r" {
					t.Fatalf("updateTokensFn() args = creator=%q access=%q refresh=%q, want creator=\"c1\" access=\"fresh\" refresh=\"fresh-r\"", creatorID, accessToken, refreshToken)
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

func TestIsEventSubActiveForCreatorWithToken(t *testing.T) {
	t.Parallel()

	svc := NewEventSub(
		&eventsubFakeStore{},
		&eventSubFakeTwitch{
			enabledEventSubFn: func(_ context.Context, _, _ string) (map[string]bool, error) {
				return map[string]bool{
					EventTypeChannelSubscribe: true,
					EventTypeChannelSubEnd:    false,
					EventTypeChannelSubGift:   true,
				}, nil
			},
		},
		nil,
	)

	active, err := svc.IsEventSubActiveForCreatorWithToken(t.Context(), "app-token", "creator-1")
	if err != nil {
		t.Fatalf("IsEventSubActiveForCreatorWithToken(%q, %q) returned error %v, want nil", "app-token", "creator-1", err)
	}
	if active {
		t.Errorf("IsEventSubActiveForCreatorWithToken(%q, %q) = %t, want %t", "app-token", "creator-1", active, false)
	}
}

func TestDumpCurrentSubscribersPropagatesStoreErrors(t *testing.T) {
	t.Parallel()

	svc := NewEventSub(
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
	svc := NewEventSub(
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
	if !slices.Equal(observer.refreshes, []string{"failed"}) {
		t.Fatalf("refresh metrics = %v, want %v", observer.refreshes, []string{"failed"})
	}
	if !slices.Equal(observer.transitions, []string{"healthy->reconnect_required:" + creatorAuthErrorTokenRefreshFailed}) {
		t.Fatalf("auth transitions = %v", observer.transitions)
	}
	if !slices.Equal(observer.notifications, []string{"ok"}) {
		t.Fatalf("notification metrics = %v, want [ok]", observer.notifications)
	}
	if !slices.Equal(observer.reconnectGauge, []int{1}) {
		t.Fatalf("reconnect gauge values = %v, want [1]", observer.reconnectGauge)
	}
}
