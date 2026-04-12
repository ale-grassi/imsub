package core

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"imsub/internal/events"
)

type viewerFakeStore struct {
	getIdentityFn           func(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	listActiveCreatorsFn    func(ctx context.Context) ([]Creator, error)
	listActiveCreatorGroups func(ctx context.Context) ([]ActiveCreatorGroups, error)
	listGroupsFn            func(ctx context.Context, creatorID string) ([]ManagedGroup, error)
	isSubscriberFn          func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	isBlockedFn             func(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	removeMembershipFn      func(ctx context.Context, chatID, telegramUserID int64) error
	addMembershipFn         func(ctx context.Context, chatID, telegramUserID int64) error
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

func (f *viewerFakeStore) ListActiveCreatorGroups(ctx context.Context) ([]ActiveCreatorGroups, error) {
	if f.listActiveCreatorGroups != nil {
		return f.listActiveCreatorGroups(ctx)
	}
	creators, err := f.ListActiveCreators(ctx)
	if err != nil || len(creators) == 0 {
		return nil, err
	}

	out := make([]ActiveCreatorGroups, 0, len(creators))
	for _, creator := range creators {
		groups, groupErr := f.ListManagedGroupsByCreator(ctx, creator.ID)
		if groupErr != nil {
			return nil, groupErr
		}
		out = append(out, ActiveCreatorGroups{
			Creator: creator,
			Groups:  groups,
		})
	}
	return out, nil
}

func (f *viewerFakeStore) IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isSubscriberFn != nil {
		return f.isSubscriberFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
}

func (f *viewerFakeStore) IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error) {
	if f.isBlockedFn != nil {
		return f.isBlockedFn(ctx, creatorID, twitchUserID)
	}
	return false, nil
}

func (f *viewerFakeStore) ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]ManagedGroup, error) {
	if f.listGroupsFn != nil {
		return f.listGroupsFn(ctx, creatorID)
	}
	return nil, nil
}

func (f *viewerFakeStore) RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error {
	if f.removeMembershipFn != nil {
		return f.removeMembershipFn(ctx, chatID, telegramUserID)
	}
	return nil
}

func (f *viewerFakeStore) AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, _ string, _ time.Time) error {
	if f.addMembershipFn != nil {
		return f.addMembershipFn(ctx, chatID, telegramUserID)
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

type viewerObserverStub struct {
	events []events.Event
}

func (o *viewerObserverStub) Emit(_ context.Context, evt events.Event) {
	o.events = append(o.events, evt)
}

func TestBuildJoinTargets(t *testing.T) {
	t.Parallel()

	added := make([]int64, 0)
	removed := make([]int64, 0)
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1", TwitchLogin: "zeta"},
					{ID: "c2", TwitchLogin: "alpha"},
					{ID: "c3", TwitchLogin: "beta"},
				}, nil
			},
			listGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
				switch creatorID {
				case "c1":
					return []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "Group Z"}}, nil
				case "c2":
					return []ManagedGroup{{ChatID: 102, CreatorID: "c2", GroupName: "Group A"}}, nil
				case "c3":
					return []ManagedGroup{{ChatID: 103, CreatorID: "c3", GroupName: "Group B"}}, nil
				}
				return nil, nil
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
			addMembershipFn: func(_ context.Context, chatID, _ int64) error {
				added = append(added, chatID)
				return nil
			},
			removeMembershipFn: func(_ context.Context, chatID, _ int64) error {
				removed = append(removed, chatID)
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
	if !slices.Equal(added, []int64{101}) {
		t.Errorf("BuildJoinTargets(7, tw-1) added memberships mismatch: got=%v want=%v", added, []int64{101})
	}
	if !slices.Equal(removed, []int64{103}) {
		t.Errorf("BuildJoinTargets(7, tw-1) removed memberships mismatch: got=%v want=%v", removed, []int64{103})
	}
}

func TestBuildJoinTargetsListError(t *testing.T) {
	t.Parallel()

	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return nil, errors.New("boom")
			},
		},
		&fakeGroupOps{},
		nil,
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

func TestResolveJoinPlanDoesNotMutateTrackedMembership(t *testing.T) {
	t.Parallel()

	addCalls := 0
	removeCalls := 0
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1", TwitchLogin: "alpha"},
					{ID: "c2", TwitchLogin: "beta"},
				}, nil
			},
			listGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
				switch creatorID {
				case "c1":
					return []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "A"}}, nil
				case "c2":
					return []ManagedGroup{{ChatID: 202, CreatorID: "c2", GroupName: "B"}}, nil
				}
				return nil, nil
			},
			isSubscriberFn: func(_ context.Context, creatorID, _ string) (bool, error) {
				return creatorID == "c1", nil
			},
			addMembershipFn: func(_ context.Context, _, _ int64) error {
				addCalls++
				return nil
			},
			removeMembershipFn: func(_ context.Context, _, _ int64) error {
				removeCalls++
				return nil
			},
		},
		&fakeGroupOps{
			isMemberFn: func(_ context.Context, _, _ int64) bool { return false },
		},
		nil,
		nil,
	)

	got, err := svc.resolveJoinPlan(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("resolveJoinPlan() error = %v", err)
	}
	if !slices.Equal(got.activeCreatorNames, []string{"alpha"}) {
		t.Fatalf("activeCreatorNames = %v, want [alpha]", got.activeCreatorNames)
	}
	if len(got.inviteGroups) != 1 || got.inviteGroups[0].group.ChatID != 101 {
		t.Fatalf("inviteGroups = %+v, want one invite group for 101", got.inviteGroups)
	}
	if !slices.Equal(got.untrackedGroups, []int64{202}) {
		t.Fatalf("untrackedGroups = %v, want [202]", got.untrackedGroups)
	}
	if addCalls != 0 || removeCalls != 0 {
		t.Fatalf("resolveJoinPlan mutated tracked membership: addCalls=%d removeCalls=%d", addCalls, removeCalls)
	}
}

func TestBuildJoinTargetsRecordsMetrics(t *testing.T) {
	t.Parallel()

	obs := &viewerObserverStub{}
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{
					{ID: "c1", TwitchLogin: "alpha"},
					{ID: "c2", TwitchLogin: "beta"},
				}, nil
			},
			listGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
				switch creatorID {
				case "c1":
					return []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "A"}}, nil
				case "c2":
					return []ManagedGroup{{ChatID: 202, CreatorID: "c2", GroupName: "B"}}, nil
				}
				return nil, nil
			},
			isSubscriberFn: func(_ context.Context, creatorID, _ string) (bool, error) {
				return creatorID == "c1", nil
			},
		},
		&fakeGroupOps{
			isMemberFn: func(_ context.Context, _, _ int64) bool { return false },
			createInviteFn: func(_ context.Context, _ int64, _ int64, _ string) (string, error) {
				return "https://invite", nil
			},
		},
		nil,
		obs,
	)

	got, err := svc.BuildJoinTargets(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("BuildJoinTargets() error = %v", err)
	}
	if len(got.JoinLinks) != 1 {
		t.Fatalf("JoinLinks = %+v, want 1 link", got.JoinLinks)
	}

	wantEvents := []events.Event{
		{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "active_creators"}, Count: 1},
		{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "invite_groups"}, Count: 1},
		{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "cache_removes"}, Count: 1},
		{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "cache_adds"}, Count: 1},
		{Name: events.NameViewerInviteLink, Outcome: "ok", Fields: map[string]string{"reason": "none"}},
		{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "join_links"}, Count: 1},
	}
	if !slices.EqualFunc(obs.events, wantEvents, func(a, b events.Event) bool {
		return a.Name == b.Name && a.Outcome == b.Outcome && a.Count == b.Count && viewerMapsEqual(a.Fields, b.Fields)
	}) {
		t.Fatalf("events = %+v, want %+v", obs.events, wantEvents)
	}
}

func TestBuildJoinTargetsRecordsInviteFailureReason(t *testing.T) {
	t.Parallel()

	obs := &viewerObserverStub{}
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{{ID: "c1", TwitchLogin: "alpha"}}, nil
			},
			listGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
				if creatorID != "c1" {
					return nil, nil
				}
				return []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "A"}}, nil
			},
			isSubscriberFn: func(_ context.Context, creatorID, _ string) (bool, error) {
				return creatorID == "c1", nil
			},
		},
		&fakeGroupOps{
			isMemberFn: func(_ context.Context, _, _ int64) bool { return false },
			createInviteFn: func(_ context.Context, _ int64, _ int64, _ string) (string, error) {
				return "", &InviteLinkError{Reason: InviteLinkErrorReasonBadRequest, Err: errors.New("bad invite")}
			},
		},
		nil,
		obs,
	)

	got, err := svc.BuildJoinTargets(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("BuildJoinTargets() error = %v", err)
	}
	if len(got.JoinLinks) != 0 {
		t.Fatalf("JoinLinks = %+v, want no links on invite failure", got.JoinLinks)
	}
	if !slices.ContainsFunc(obs.events, func(evt events.Event) bool {
		return evt.Name == events.NameViewerInviteLink && evt.Outcome == "failed" && evt.Fields["reason"] == "bad_request"
	}) {
		t.Fatalf("events = %+v, want failed invite metric with bad_request reason", obs.events)
	}
}

func TestBuildJoinTargetsSkipsBlockedCreator(t *testing.T) {
	t.Parallel()

	var removed []int64
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				return []Creator{{ID: "c1", TwitchLogin: "alpha", BlocklistSyncEnabled: true}}, nil
			},
			listGroupsFn: func(_ context.Context, creatorID string) ([]ManagedGroup, error) {
				return []ManagedGroup{{ChatID: 101, CreatorID: creatorID, GroupName: "A"}}, nil
			},
			isSubscriberFn: func(_ context.Context, _, _ string) (bool, error) {
				return true, nil
			},
			isBlockedFn: func(_ context.Context, _, _ string) (bool, error) {
				return true, nil
			},
			removeMembershipFn: func(_ context.Context, chatID, _ int64) error {
				removed = append(removed, chatID)
				return nil
			},
		},
		&fakeGroupOps{},
		nil,
		nil,
	)

	got, err := svc.BuildJoinTargets(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("BuildJoinTargets() error = %v", err)
	}
	if len(got.ActiveCreatorNames) != 0 || len(got.JoinLinks) != 0 {
		t.Fatalf("BuildJoinTargets() = %+v, want no active creators or join links for blocked user", got)
	}
	if !slices.Equal(removed, []int64{101}) {
		t.Fatalf("removed memberships = %v, want [101]", removed)
	}
}

func TestBuildJoinTargetsForCreatorScopesToCreator(t *testing.T) {
	t.Parallel()

	var added []int64
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorGroups: func(_ context.Context) ([]ActiveCreatorGroups, error) {
				return []ActiveCreatorGroups{
					{
						Creator: Creator{ID: "c1", TwitchLogin: "alpha"},
						Groups:  []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "VIP"}},
					},
					{
						Creator: Creator{ID: "c2", TwitchLogin: "beta"},
						Groups:  []ManagedGroup{{ChatID: 202, CreatorID: "c2", GroupName: "Plus"}},
					},
				}, nil
			},
			isSubscriberFn: func(_ context.Context, creatorID, twitchUserID string) (bool, error) {
				return creatorID == "c1" && twitchUserID == "tw-1", nil
			},
			addMembershipFn: func(_ context.Context, chatID, _ int64) error {
				added = append(added, chatID)
				return nil
			},
		},
		&fakeGroupOps{
			createInviteFn: func(_ context.Context, groupChatID, telegramUserID int64, name string) (string, error) {
				if groupChatID != 101 || telegramUserID != 7 || name != "alpha" {
					t.Fatalf("CreateInviteLink(%d, %d, %q) got unexpected args", groupChatID, telegramUserID, name)
				}
				return "https://t.me/+alpha", nil
			},
		},
		nil,
		nil,
	)

	got, err := svc.BuildJoinTargetsForCreator(t.Context(), "c1", 7, "tw-1")
	if err != nil {
		t.Fatalf("BuildJoinTargetsForCreator() error = %v", err)
	}
	if !slices.Equal(got.ActiveCreatorNames, []string{"alpha"}) {
		t.Fatalf("BuildJoinTargetsForCreator() active names = %v, want [alpha]", got.ActiveCreatorNames)
	}
	if len(got.JoinLinks) != 1 || got.JoinLinks[0].InviteLink != "https://t.me/+alpha" {
		t.Fatalf("BuildJoinTargetsForCreator() join links = %+v, want one creator-scoped invite", got.JoinLinks)
	}
	if !slices.Equal(added, []int64{101}) {
		t.Fatalf("BuildJoinTargetsForCreator() added memberships = %v, want [101]", added)
	}
}

func TestResolveJoinPlanUsesActiveCreatorGroupsStoreRead(t *testing.T) {
	t.Parallel()

	legacyCreatorReads := 0
	legacyGroupReads := 0
	svc := NewViewerService(
		&viewerFakeStore{
			listActiveCreatorGroups: func(_ context.Context) ([]ActiveCreatorGroups, error) {
				return []ActiveCreatorGroups{{
					Creator: Creator{ID: "c1", TwitchLogin: "alpha"},
					Groups:  []ManagedGroup{{ChatID: 101, CreatorID: "c1", GroupName: "A"}},
				}}, nil
			},
			listActiveCreatorsFn: func(_ context.Context) ([]Creator, error) {
				legacyCreatorReads++
				return nil, nil
			},
			listGroupsFn: func(_ context.Context, _ string) ([]ManagedGroup, error) {
				legacyGroupReads++
				return nil, nil
			},
			isSubscriberFn: func(_ context.Context, _, _ string) (bool, error) {
				return true, nil
			},
		},
		&fakeGroupOps{},
		nil,
		nil,
	)

	got, err := svc.resolveJoinPlan(t.Context(), 7, "tw-1")
	if err != nil {
		t.Fatalf("resolveJoinPlan() error = %v", err)
	}
	if legacyCreatorReads != 0 || legacyGroupReads != 0 {
		t.Fatalf("legacy resolver reads used: creators=%d groups=%d, want 0/0", legacyCreatorReads, legacyGroupReads)
	}
	if !slices.Equal(got.activeCreatorNames, []string{"alpha"}) {
		t.Fatalf("activeCreatorNames = %v, want [alpha]", got.activeCreatorNames)
	}
	if len(got.inviteGroups) != 1 || got.inviteGroups[0].group.ChatID != 101 {
		t.Fatalf("inviteGroups = %+v, want one invite group for 101", got.inviteGroups)
	}
}

func viewerMapsEqual(a, b map[string]string) bool {
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
