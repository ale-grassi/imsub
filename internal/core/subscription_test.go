package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type subscriptionFakeStore struct {
	removeCreatorSubscriberFn func(ctx context.Context, creatorID, twitchUserID string) error
	getCreatorFn              func(ctx context.Context, creatorID string) (Creator, bool, error)
	listManagedGroupsFn       func(ctx context.Context, creatorID string) ([]ManagedGroup, error)
	removeByTwitchFn          func(ctx context.Context, twitchUserID, creatorID string) (int64, bool, error)
	resolveByTwitchFn         func(ctx context.Context, twitchUserID string) (int64, bool, error)
	getUserIdentityFn         func(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	upsertGraceFn             func(ctx context.Context, job PendingSubscriptionEndGrace) (PendingSubscriptionEndGrace, error)
	deleteGraceFn             func(ctx context.Context, creatorID, twitchUserID string) error
}

func (f *subscriptionFakeStore) RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error {
	if f.removeCreatorSubscriberFn != nil {
		return f.removeCreatorSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return nil
}

func (f *subscriptionFakeStore) Creator(ctx context.Context, creatorID string) (Creator, bool, error) {
	if f.getCreatorFn != nil {
		return f.getCreatorFn(ctx, creatorID)
	}
	return Creator{}, false, nil
}

func (f *subscriptionFakeStore) RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (int64, bool, error) {
	if f.removeByTwitchFn != nil {
		return f.removeByTwitchFn(ctx, twitchUserID, creatorID)
	}
	return 0, false, nil
}

func (f *subscriptionFakeStore) ResolveTelegramUserIDByTwitch(ctx context.Context, twitchUserID string) (int64, bool, error) {
	if f.resolveByTwitchFn != nil {
		return f.resolveByTwitchFn(ctx, twitchUserID)
	}
	return 0, false, nil
}

func (f *subscriptionFakeStore) ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]ManagedGroup, error) {
	if f.listManagedGroupsFn != nil {
		return f.listManagedGroupsFn(ctx, creatorID)
	}
	return nil, nil
}

func (f *subscriptionFakeStore) UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	if f.getUserIdentityFn != nil {
		return f.getUserIdentityFn(ctx, telegramUserID)
	}
	return UserIdentity{}, false, nil
}

func (f *subscriptionFakeStore) UpsertSubscriptionEndGrace(ctx context.Context, job PendingSubscriptionEndGrace) (PendingSubscriptionEndGrace, error) {
	if f.upsertGraceFn != nil {
		return f.upsertGraceFn(ctx, job)
	}
	return job, nil
}

func (f *subscriptionFakeStore) DeleteSubscriptionEndGrace(ctx context.Context, creatorID, twitchUserID string) error {
	if f.deleteGraceFn != nil {
		return f.deleteGraceFn(ctx, creatorID, twitchUserID)
	}
	return nil
}

func TestProcessEndFound(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			if creatorID != "c1" {
				t.Fatalf("getCreatorFn() creatorID = %q, want \"c1\"", creatorID)
			}
			return Creator{ID: "c1", TwitchLogin: "streamer1"}, true, nil
		},
		listManagedGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
			return []ManagedGroup{{ChatID: 123, CreatorID: creatorID, GroupName: "VIP"}}, nil
		},
		removeByTwitchFn: func(_ context.Context, twitchUserID, creatorID string) (int64, bool, error) {
			if twitchUserID != "tw-1" || creatorID != "c1" {
				t.Fatalf("removeByTwitchFn() args = twitch=%q creator=%q, want twitch=\"tw-1\" creator=\"c1\"", twitchUserID, creatorID)
			}
			return 777, true, nil
		},
		getUserIdentityFn: func(_ context.Context, telegramUserID int64) (UserIdentity, bool, error) {
			if telegramUserID != 777 {
				t.Fatalf("getUserIdentityFn() telegramUserID = %d, want 777", telegramUserID)
			}
			return UserIdentity{TelegramUserID: 777, Language: "it"}, true, nil
		},
	})

	got, err := svc.ProcessEnd(t.Context(), "c1", "", "tw-1")
	if err != nil {
		t.Fatalf("ProcessEnd error: %v", err)
	}
	if !got.Found {
		t.Fatalf("ProcessEnd() Found = %t, want true", got.Found)
	}
	if got.TelegramUserID != 777 || len(got.GroupChatIDs) != 1 || got.GroupChatIDs[0] != 123 {
		t.Errorf("ProcessEnd() = %+v, want TelegramUserID=777, GroupChatIDs=[123]", got)
	}
	if got.BroadcasterLogin != "streamer1" {
		t.Errorf("ProcessEnd() BroadcasterLogin = %q, want %q", got.BroadcasterLogin, "streamer1")
	}
	if !got.HasIdentityLang || got.IdentityLanguage != "it" {
		t.Errorf("ProcessEnd() Language = %+v, want HasIdentityLang=true, IdentityLanguage=\"it\"", got)
	}
}

func TestProcessEndNotFound(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 0, false, nil
		},
	})

	got, err := svc.ProcessEnd(t.Context(), "c1", "streamer1", "tw-1")
	if err != nil {
		t.Fatalf("ProcessEnd error: %v", err)
	}
	if got.Found {
		t.Fatalf("ProcessEnd() Found = %t, want false", got.Found)
	}
}

func TestProcessEndStoreError(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 0, false, errors.New("redis down")
		},
	})

	_, err := svc.ProcessEnd(t.Context(), "c1", "streamer1", "tw-1")
	if err == nil {
		t.Fatalf("ProcessEnd(%q, %q, %q) returned error nil, want error from removeByTwitchFn", "c1", "streamer1", "tw-1")
	}
}

func TestProcessEndRemoveSubscriberError(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		removeCreatorSubscriberFn: func(context.Context, string, string) error {
			return errors.New("remove subscriber failed")
		},
	})

	_, err := svc.ProcessEnd(t.Context(), "c1", "streamer1", "tw-1")
	if err == nil {
		t.Fatalf("ProcessEnd(%q, %q, %q) returned error nil, want error from RemoveCreatorSubscriber", "c1", "streamer1", "tw-1")
	}
}

func TestProcessEndGetCreatorError(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		getCreatorFn: func(context.Context, string) (Creator, bool, error) {
			return Creator{}, false, errors.New("creator lookup failed")
		},
	})

	_, err := svc.ProcessEnd(t.Context(), "c1", "streamer1", "tw-1")
	if err == nil {
		t.Fatalf("ProcessEnd(%q, %q, %q) returned error nil, want error from Creator lookup", "c1", "streamer1", "tw-1")
	}
}

func TestProcessEndGetIdentityError(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			return Creator{ID: creatorID}, true, nil
		},
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 777, true, nil
		},
		getUserIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
			return UserIdentity{}, false, errors.New("identity lookup failed")
		},
	})

	_, err := svc.ProcessEnd(t.Context(), "c1", "streamer1", "tw-1")
	if err == nil {
		t.Fatalf("ProcessEnd(%q, %q, %q) returned error nil, want error from UserIdentity lookup", "c1", "streamer1", "tw-1")
	}
}

func TestPrepareEndFoundResult(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			return Creator{ID: creatorID, TwitchLogin: "creator_login"}, true, nil
		},
		listManagedGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
			return []ManagedGroup{{ChatID: 100, CreatorID: creatorID, GroupName: "VIP"}}, nil
		},
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 10, true, nil
		},
		getUserIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
			return UserIdentity{Language: "it-IT"}, true, nil
		},
	})

	got, err := svc.PrepareEnd(t.Context(), "c1", "creator", "u1", "viewer_login")
	if err != nil {
		t.Fatalf("PrepareEnd error: %v", err)
	}
	if !got.Found {
		t.Fatalf("PrepareEnd() Found = %t, want true", got.Found)
	}
	if got.Language != "it" {
		t.Errorf("PrepareEnd() Language = %q, want %q", got.Language, "it")
	}
	if got.ViewerLogin != "viewer_login" || got.BroadcasterLogin != "creator" {
		t.Errorf("PrepareEnd() = %+v, want ViewerLogin=\"viewer_login\", BroadcasterLogin=\"creator\"", got)
	}
}

func TestPrepareEndGraceResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	var saved PendingSubscriptionEndGrace
	svc := NewSubscriptionService(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			return Creator{ID: creatorID, TwitchLogin: "creator_login", SubscriptionEndGrace: SubscriptionEndGrace24h}, true, nil
		},
		listManagedGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
			return []ManagedGroup{{ChatID: 100, CreatorID: creatorID, GroupName: "VIP"}}, nil
		},
		resolveByTwitchFn: func(context.Context, string) (int64, bool, error) {
			return 10, true, nil
		},
		getUserIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
			return UserIdentity{Language: "it-IT"}, true, nil
		},
		upsertGraceFn: func(_ context.Context, job PendingSubscriptionEndGrace) (PendingSubscriptionEndGrace, error) {
			saved = job
			return job, nil
		},
	})
	svc.now = func() time.Time { return now }

	got, err := svc.PrepareEnd(t.Context(), "c1", "creator", "u1", "viewer_login")
	if err != nil {
		t.Fatalf("PrepareEnd error: %v", err)
	}
	if got.Mode != SubscriptionEndModeGrace {
		t.Fatalf("PrepareEnd() mode = %q, want %q", got.Mode, SubscriptionEndModeGrace)
	}
	if !got.GraceUntil.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("PrepareEnd() grace until = %v, want %v", got.GraceUntil, now.Add(24*time.Hour))
	}
	if saved.TelegramUserID != 10 || saved.ViewerLogin != "viewer_login" {
		t.Fatalf("saved grace job = %+v, want telegram user 10 and viewer login", saved)
	}
}

func TestPrepareEndNotFound(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 0, false, nil
		},
	})

	got, err := svc.PrepareEnd(t.Context(), "c1", "creator", "u1", "viewer")
	if err != nil {
		t.Fatalf("PrepareEnd error: %v", err)
	}
	if got.Found {
		t.Fatalf("PrepareEnd() Found = %t, want false", got.Found)
	}
}

func TestPrepareEndPropagatesError(t *testing.T) {
	t.Parallel()

	svc := NewSubscriptionService(&subscriptionFakeStore{
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 0, false, errors.New("boom")
		},
	})

	_, err := svc.PrepareEnd(t.Context(), "c1", "creator", "u1", "viewer")
	if err == nil {
		t.Fatalf("PrepareEnd(%q, %q, %q, %q) returned error nil, want error from removeByTwitchFn", "c1", "creator", "u1", "viewer")
	}
}
