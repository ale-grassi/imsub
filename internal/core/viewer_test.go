package core

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type viewerFakeStore struct {
	Store
	getIdentityFn        func(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	listActiveCreatorsFn func(ctx context.Context) ([]Creator, error)
	isSubscriberFn       func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	removeMembershipFn   func(ctx context.Context, telegramUserID int64, creatorID string) error
	addMembershipFn      func(ctx context.Context, telegramUserID int64, creatorID string) error
}

func (f *viewerFakeStore) UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	if f.getIdentityFn != nil {
		return f.getIdentityFn(ctx, telegramUserID)
	}
	return UserIdentity{}, false, nil
}

func (f *viewerFakeStore) ListActiveCreators(ctx context.Context) ([]Creator, error) {
	if f.listActiveCreatorsFn != nil {
		return f.listActiveCreatorsFn(ctx)
	}
	return nil, nil
}

func (f *viewerFakeStore) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isSubscriberFn != nil {
		return f.isSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
}

func (f *viewerFakeStore) RemoveUserCreatorByTelegram(ctx context.Context, telegramUserID int64, creatorID string) error {
	if f.removeMembershipFn != nil {
		return f.removeMembershipFn(ctx, telegramUserID, creatorID)
	}
	return nil
}

func (f *viewerFakeStore) AddUserCreatorMembership(ctx context.Context, telegramUserID int64, creatorID string) error {
	if f.addMembershipFn != nil {
		return f.addMembershipFn(ctx, telegramUserID, creatorID)
	}
	return nil
}

type fakeGroupOps struct {
	isMemberFn     func(ctx context.Context, groupChatID, telegramUserID int64) bool
	createInviteFn func(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error)
}

func (f *fakeGroupOps) IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	if f.isMemberFn != nil {
		return f.isMemberFn(ctx, groupChatID, telegramUserID)
	}
	return false
}

func (f *fakeGroupOps) CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	if f.createInviteFn != nil {
		return f.createInviteFn(ctx, groupChatID, telegramUserID, name)
	}
	return "", nil
}

func TestBuildJoinTargets(t *testing.T) {
	t.Parallel()

	added := make([]string, 0)
	removed := make([]string, 0)
	svc := NewViewer(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1", Name: "zeta", GroupChatID: 101, GroupName: "Group Z"},
					{ID: "c2", Name: "alpha", GroupChatID: 102, GroupName: "Group A"},
					{ID: "c3", Name: "beta", GroupChatID: 103, GroupName: "Group B"},
				}, nil
			},
			isSubscriberFn: func(_ context.Context, creatorID, _ string) (bool, error) {
				switch creatorID {
				case "c1", "c2":
					return true, nil
				case "c3":
					return false, nil
				}
				return false, nil
			},
			addMembershipFn: func(_ context.Context, _ int64, creatorID string) error {
				added = append(added, creatorID)
				return nil
			},
			removeMembershipFn: func(_ context.Context, _ int64, creatorID string) error {
				removed = append(removed, creatorID)
				return nil
			},
		},
		&fakeGroupOps{
			isMemberFn: func(_ context.Context, groupChatID, _ int64) bool {
				return groupChatID == 102 // already in alpha group
			},
			createInviteFn: func(_ context.Context, _ int64, _ int64, name string) (string, error) {
				return "https://invite/" + name, nil
			},
		},
		nil,
	)

	got, err := svc.BuildJoinTargets(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("BuildJoinTargets(%d, %q) returned error %v, want nil", 7, "tw-1", err)
	}

	if !slices.Equal(got.ActiveCreatorNames, []string{"alpha", "zeta"}) {
		t.Errorf("BuildJoinTargets(7, tw-1) active names mismatch: got=%v want=%v", got.ActiveCreatorNames, []string{"alpha", "zeta"})
	}
	if len(got.JoinLinks) != 1 || got.JoinLinks[0].CreatorName != "zeta" {
		t.Errorf("BuildJoinTargets() JoinLinks = %+v, want 1 link with CreatorName=\"zeta\"", got.JoinLinks)
	}
	if !slices.Equal(added, []string{"c1"}) {
		t.Errorf("BuildJoinTargets(7, tw-1) added memberships mismatch: got=%v want=%v", added, []string{"c1"})
	}
	if !slices.Equal(removed, []string{"c3"}) {
		t.Errorf("BuildJoinTargets(7, tw-1) removed memberships mismatch: got=%v want=%v", removed, []string{"c3"})
	}
}

func TestBuildJoinTargetsListError(t *testing.T) {
	t.Parallel()

	svc := NewViewer(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return nil, errors.New("boom")
			},
		},
		&fakeGroupOps{},
		nil,
	)

	got, err := svc.BuildJoinTargets(t.Context(), 7, "tw-1")
	if err == nil {
		t.Fatalf("BuildJoinTargets(%d, %q) returned error nil, want non-nil error", 7, "tw-1")
	}
	if len(got.ActiveCreatorNames) != 0 || len(got.JoinLinks) != 0 {
		t.Fatalf("BuildJoinTargets(%d, %q) = %+v, want empty targets", 7, "tw-1", got)
	}
}
