package core

import (
	"context"
	"errors"
	"testing"
)

type subscriptionFakeStore struct {
	Store
	removeCreatorSubscriberFn func(ctx context.Context, creatorID, twitchUserID string) error
	getCreatorFn              func(ctx context.Context, creatorID string) (Creator, bool, error)
	removeByTwitchFn          func(ctx context.Context, twitchUserID, creatorID string) (int64, bool, error)
	getUserIdentityFn         func(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
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

func (f *subscriptionFakeStore) UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	if f.getUserIdentityFn != nil {
		return f.getUserIdentityFn(ctx, telegramUserID)
	}
	return UserIdentity{}, false, nil
}

func TestProcessEndFound(t *testing.T) {
	t.Parallel()

	svc := NewSubscription(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			if creatorID != "c1" {
				t.Fatalf("getCreatorFn() creatorID = %q, want \"c1\"", creatorID)
			}
			return Creator{ID: "c1", Name: "streamer1", GroupChatID: 123}, true, nil
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
	if got.TelegramUserID != 777 || got.GroupChatID != 123 {
		t.Errorf("ProcessEnd() = %+v, want TelegramUserID=777, GroupChatID=123", got)
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

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
		getCreatorFn: func(_ context.Context, creatorID string) (Creator, bool, error) {
			return Creator{ID: creatorID, Name: "creator_login", GroupChatID: 100}, true, nil
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

func TestPrepareEndNotFound(t *testing.T) {
	t.Parallel()

	svc := NewSubscription(&subscriptionFakeStore{
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

	svc := NewSubscription(&subscriptionFakeStore{
		removeByTwitchFn: func(context.Context, string, string) (int64, bool, error) {
			return 0, false, errors.New("boom")
		},
	})

	_, err := svc.PrepareEnd(t.Context(), "c1", "creator", "u1", "viewer")
	if err == nil {
		t.Fatalf("PrepareEnd(%q, %q, %q, %q) returned error nil, want error from removeByTwitchFn", "c1", "creator", "u1", "viewer")
	}
}
