package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type oauthFakeStore struct {
	Store
	saveViewerFn func(ctx context.Context, telegramUserID int64, twitchUserID, twitchLogin, language string) (int64, error)
	getOwnedFn   func(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	upsertFn     func(ctx context.Context, c Creator) error
}

func (f *oauthFakeStore) SaveUserIdentityOnly(ctx context.Context, telegramUserID int64, twitchUserID, twitchLogin, language string) (int64, error) {
	if f.saveViewerFn != nil {
		return f.saveViewerFn(ctx, telegramUserID, twitchUserID, twitchLogin, language)
	}
	return 0, nil
}

func (f *oauthFakeStore) UpsertCreator(ctx context.Context, c Creator) error {
	if f.upsertFn != nil {
		return f.upsertFn(ctx, c)
	}
	return nil
}

func (f *oauthFakeStore) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error) {
	if f.getOwnedFn != nil {
		return f.getOwnedFn(ctx, ownerTelegramID)
	}
	return Creator{}, false, nil
}

type fakeAPI struct {
	exchangeFn  func(ctx context.Context, code string) (TokenResponse, error)
	fetchUserFn func(ctx context.Context, token string) (id, login, displayName string, err error)
}

func (f *fakeAPI) ExchangeCode(ctx context.Context, code string) (TokenResponse, error) {
	if f.exchangeFn != nil {
		return f.exchangeFn(ctx, code)
	}
	return TokenResponse{}, nil
}

func (f *fakeAPI) RefreshToken(_ context.Context, _ string) (TokenResponse, error) {
	return TokenResponse{}, nil
}

func (f *fakeAPI) FetchUser(ctx context.Context, userToken string) (id, login, displayName string, err error) {
	if f.fetchUserFn != nil {
		return f.fetchUserFn(ctx, userToken)
	}
	return "", "", "", nil
}

func (f *fakeAPI) AppToken(_ context.Context) (string, error) {
	return "", nil
}

func (f *fakeAPI) CreateEventSub(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (f *fakeAPI) EnabledEventSubTypes(_ context.Context, _, _ string) (map[string]bool, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeAPI) ListSubscriberPage(_ context.Context, _, _, _ string) (userIDs []string, nextCursor string, err error) {
	return nil, "", errors.New("not implemented")
}

func TestLinkViewerSuccess(t *testing.T) {
	t.Parallel()

	svc := NewOAuth(
		&oauthFakeStore{
			saveViewerFn: func(_ context.Context, telegramUserID int64, twitchUserID, twitchLogin, language string) (int64, error) {
				if telegramUserID != 7 || twitchUserID != "tw-1" || twitchLogin != "login1" || language != "en" {
					t.Fatalf("saveViewerFn() args = tg=%d tw=%q login=%q lang=%q, want tg=7 tw=\"tw-1\" login=\"login1\" lang=\"en\"", telegramUserID, twitchUserID, twitchLogin, language)
				}
				return 42, nil
			},
		},
		&fakeAPI{
			exchangeFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{AccessToken: "token"}, nil
			},
			fetchUserFn: func(_ context.Context, _ string) (id, login, displayName string, err error) {
				return "tw-1", "login1", "Display1", nil
			},
		},
	)

	got, err := svc.LinkViewer(t.Context(), "abc", OAuthStatePayload{TelegramUserID: 7}, "en")
	if err != nil {
		t.Fatalf("LinkViewer error: %v", err)
	}
	if got.DisplacedUserID != 42 {
		t.Errorf("LinkViewer() DisplacedUserID = %d, want 42", got.DisplacedUserID)
	}
	if got.TwitchUserID != "tw-1" {
		t.Errorf("LinkViewer() TwitchUserID = %q, want \"tw-1\"", got.TwitchUserID)
	}
	if got.TwitchLogin != "login1" {
		t.Errorf("LinkViewer() TwitchLogin = %q, want \"login1\"", got.TwitchLogin)
	}
	if got.TwitchDisplayName != "Display1" {
		t.Errorf("LinkViewer() TwitchDisplayName = %q, want \"Display1\"", got.TwitchDisplayName)
	}
}

func TestLinkCreatorScopeMissing(t *testing.T) {
	t.Parallel()

	svc := NewOAuth(
		&oauthFakeStore{},
		&fakeAPI{
			exchangeFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{AccessToken: "token", Scope: []string{"user:read:email"}}, nil
			},
		},
	)

	_, err := svc.LinkCreator(t.Context(), "abc", OAuthStatePayload{TelegramUserID: 1})
	var flowErr *FlowError
	if !errors.As(err, &flowErr) {
		t.Fatalf("LinkCreator() returned error %v, want FlowError", err)
	}
	if flowErr.Kind != KindScopeMissing {
		t.Fatalf("LinkCreator() returned FlowError with Kind %q, want KindScopeMissing", flowErr.Kind)
	}
}

func TestLinkCreatorUpsertSetsUpdatedAt(t *testing.T) {
	t.Parallel()

	wantNow := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	var saved Creator
	svc := NewOAuth(
		&oauthFakeStore{
			upsertFn: func(_ context.Context, c Creator) error {
				saved = c
				return nil
			},
		},
		&fakeAPI{
			exchangeFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{
					AccessToken:  "at",
					RefreshToken: "rt",
					Scope:        []string{ScopeChannelReadSubscriptions},
				}, nil
			},
			fetchUserFn: func(_ context.Context, _ string) (id, login, displayName string, err error) {
				return "creator-1", "creator_login", "Creator Display", nil
			},
		},
	)
	svc.now = func() time.Time { return wantNow }

	got, err := svc.LinkCreator(t.Context(), "abc", OAuthStatePayload{TelegramUserID: 99})
	if err != nil {
		t.Fatalf("LinkCreator error: %v", err)
	}
	if !saved.UpdatedAt.Equal(wantNow) {
		t.Errorf("LinkCreator() saved UpdatedAt = %v, want %v", saved.UpdatedAt, wantNow)
	}
	if got.Creator.ID != "creator-1" {
		t.Errorf("LinkCreator() Creator.ID = %q, want \"creator-1\"", got.Creator.ID)
	}
	if got.BroadcasterDisplayName != "Creator Display" {
		t.Errorf("LinkCreator() BroadcasterDisplayName = %q, want \"Creator Display\"", got.BroadcasterDisplayName)
	}
	if got.Creator.AuthStatus != CreatorAuthHealthy {
		t.Errorf("LinkCreator() Creator.AuthStatus = %q, want %q", got.Creator.AuthStatus, CreatorAuthHealthy)
	}
}

func TestLinkCreatorReconnectPreservesGroupAndClearsAuthDegradation(t *testing.T) {
	t.Parallel()

	var saved Creator
	svc := NewOAuth(
		&oauthFakeStore{
			getOwnedFn: func(_ context.Context, ownerTelegramID int64) (Creator, bool, error) {
				if ownerTelegramID != 99 {
					t.Fatalf("OwnedCreatorForUser() ownerTelegramID = %d, want 99", ownerTelegramID)
				}
				return Creator{
					ID:              "creator-1",
					Name:            "creator_login",
					OwnerTelegramID: 99,
					GroupChatID:     1234,
					GroupName:       "VIP Group",
					AuthStatus:      CreatorAuthReconnectRequired,
					AuthErrorCode:   "token_refresh_failed",
				}, true, nil
			},
			upsertFn: func(_ context.Context, c Creator) error {
				saved = c
				return nil
			},
		},
		&fakeAPI{
			exchangeFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{
					AccessToken:  "new-at",
					RefreshToken: "new-rt",
					Scope:        []string{ScopeChannelReadSubscriptions},
				}, nil
			},
			fetchUserFn: func(_ context.Context, _ string) (id, login, displayName string, err error) {
				return "creator-1", "creator_login", "Creator Display", nil
			},
		},
	)

	got, err := svc.LinkCreator(t.Context(), "abc", OAuthStatePayload{TelegramUserID: 99, Reconnect: true})
	if err != nil {
		t.Fatalf("LinkCreator reconnect error: %v", err)
	}
	if got.Creator.GroupChatID != 1234 || got.Creator.GroupName != "VIP Group" {
		t.Fatalf("LinkCreator reconnect group = (%d, %q), want (1234, %q)", got.Creator.GroupChatID, got.Creator.GroupName, "VIP Group")
	}
	if got.Creator.AuthStatus != CreatorAuthHealthy {
		t.Fatalf("LinkCreator reconnect auth status = %q, want %q", got.Creator.AuthStatus, CreatorAuthHealthy)
	}
	if saved.GroupChatID != 1234 || saved.GroupName != "VIP Group" {
		t.Fatalf("UpsertCreator saved group = (%d, %q), want (1234, %q)", saved.GroupChatID, saved.GroupName, "VIP Group")
	}
}

func TestLinkCreatorReconnectRejectsDifferentCreator(t *testing.T) {
	t.Parallel()

	svc := NewOAuth(
		&oauthFakeStore{
			getOwnedFn: func(_ context.Context, _ int64) (Creator, bool, error) {
				return Creator{ID: "creator-1", OwnerTelegramID: 99}, true, nil
			},
		},
		&fakeAPI{
			exchangeFn: func(_ context.Context, _ string) (TokenResponse, error) {
				return TokenResponse{
					AccessToken:  "new-at",
					RefreshToken: "new-rt",
					Scope:        []string{ScopeChannelReadSubscriptions},
				}, nil
			},
			fetchUserFn: func(_ context.Context, _ string) (id, login, displayName string, err error) {
				return "creator-2", "creator_login", "Creator Display", nil
			},
		},
	)

	_, err := svc.LinkCreator(t.Context(), "abc", OAuthStatePayload{TelegramUserID: 99, Reconnect: true})
	var flowErr *FlowError
	if !errors.As(err, &flowErr) {
		t.Fatalf("LinkCreator reconnect mismatch error = %v, want FlowError", err)
	}
	if flowErr.Kind != KindCreatorMismatch {
		t.Fatalf("LinkCreator reconnect mismatch kind = %q, want %q", flowErr.Kind, KindCreatorMismatch)
	}
}
