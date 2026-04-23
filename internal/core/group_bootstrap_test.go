package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"imsub/internal/events"
	"imsub/internal/transport/telegram/mtproto"
)

type bootstrapStoreStub struct {
	creators        map[string]Creator
	identities      map[int64]UserIdentity
	subscribers     map[string]map[string]bool
	blocked         map[string]map[string]bool
	trackedAdds     []int64
	trackedRemovals []int64
	untrackedAdds   []int64
	untrackedDelete []int64
}

func (s *bootstrapStoreStub) UserIdentity(_ context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	identity, ok := s.identities[telegramUserID]
	return identity, ok, nil
}

func (s *bootstrapStoreStub) Creator(_ context.Context, creatorID string) (Creator, bool, error) {
	creator, ok := s.creators[creatorID]
	return creator, ok, nil
}

func (s *bootstrapStoreStub) IsCreatorSubscriber(_ context.Context, creatorID, twitchUserID string) (bool, error) {
	return s.subscribers[creatorID][twitchUserID], nil
}

func (s *bootstrapStoreStub) IsCreatorBlocked(_ context.Context, creatorID, twitchUserID string) (bool, error) {
	return s.blocked[creatorID][twitchUserID], nil
}

func (s *bootstrapStoreStub) AddTrackedGroupMember(_ context.Context, _ int64, telegramUserID int64, _ string, _ time.Time) error {
	s.trackedAdds = append(s.trackedAdds, telegramUserID)
	return nil
}

func (s *bootstrapStoreStub) RemoveTrackedGroupMember(_ context.Context, _ int64, telegramUserID int64) error {
	s.trackedRemovals = append(s.trackedRemovals, telegramUserID)
	return nil
}

func (s *bootstrapStoreStub) UpsertUntrackedGroupMember(_ context.Context, _ int64, telegramUserID int64, _, _ string, _ time.Time) error {
	s.untrackedAdds = append(s.untrackedAdds, telegramUserID)
	return nil
}

func (s *bootstrapStoreStub) RemoveUntrackedGroupMember(_ context.Context, _ int64, telegramUserID int64) error {
	s.untrackedDelete = append(s.untrackedDelete, telegramUserID)
	return nil
}

type bootstrapGroupOpsStub struct {
	inviteCalls int
	kicks       []int64
	reasons     []KickReason
}

func (s *bootstrapGroupOpsStub) CreateBootstrapInviteLink(context.Context, int64) (string, error) {
	s.inviteCalls++
	return "https://t.me/+bootstrap", nil
}

func (s *bootstrapGroupOpsStub) KickFromGroup(_ context.Context, _ int64, telegramUserID int64, reason KickReason) error {
	s.kicks = append(s.kicks, telegramUserID)
	s.reasons = append(s.reasons, reason)
	return nil
}

type bootstrapMTProtoStub struct {
	selfID int64
	dumps  [][]mtproto.Member
	errs   []error
	calls  int
}

func (s *bootstrapMTProtoStub) DumpMembersViaInvite(context.Context, string) ([]mtproto.Member, error) {
	idx := s.calls
	s.calls++
	if idx < len(s.errs) && s.errs[idx] != nil {
		return nil, s.errs[idx]
	}
	if idx < len(s.dumps) {
		return s.dumps[idx], nil
	}
	return nil, nil
}

func (s *bootstrapMTProtoStub) SelfUserID() int64 {
	return s.selfID
}

type bootstrapEventSink struct {
	events []events.Event
}

func (s *bootstrapEventSink) Emit(_ context.Context, evt events.Event) {
	s.events = append(s.events, evt)
}

func TestGroupBootstrapTracksEligibleAndObservesUnknownMembers(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{
		creators: map[string]Creator{
			"creator-1": {ID: "creator-1"},
		},
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchUserID: "tw-10"},
		},
		subscribers: map[string]map[string]bool{
			"creator-1": {"tw-10": true},
		},
		blocked: map[string]map[string]bool{
			"creator-1": {},
		},
	}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		dumps: [][]mtproto.Member{{
			{TelegramUserID: 10, Role: mtproto.MemberRoleMember},
			{TelegramUserID: 20, Role: mtproto.MemberRoleMember},
			{TelegramUserID: 30, Role: mtproto.MemberRoleAdmin},
			{TelegramUserID: 40, Role: mtproto.MemberRoleMember, IsBot: true},
			{TelegramUserID: 999, Role: mtproto.MemberRoleMember},
		}},
	}
	eventsSink := &bootstrapEventSink{}
	svc := NewGroupBootstrapService(store, groupOps, mt, nil, eventsSink, nil)

	if err := svc.BootstrapGroup(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "creator-1", Policy: GroupPolicyObserve}); err != nil {
		t.Fatalf("BootstrapGroup() error = %v, want nil", err)
	}

	if got := store.trackedAdds; len(got) != 1 || got[0] != 10 {
		t.Fatalf("tracked adds = %v, want [10]", got)
	}
	if got := store.untrackedAdds; len(got) != 1 || got[0] != 20 {
		t.Fatalf("untracked adds = %v, want [20]", got)
	}
	if len(groupOps.kicks) != 0 {
		t.Fatalf("kicks = %v, want none", groupOps.kicks)
	}
	if len(eventsSink.events) == 0 || eventsSink.events[len(eventsSink.events)-1].Outcome != "ok" {
		t.Fatalf("events = %+v, want final ok event", eventsSink.events)
	}
}

func TestGroupBootstrapKickPolicyRemovesUnknownMembers(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{
		creators: map[string]Creator{
			"creator-1": {ID: "creator-1"},
		},
		subscribers: map[string]map[string]bool{"creator-1": {}},
		blocked:     map[string]map[string]bool{"creator-1": {}},
	}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		dumps:  [][]mtproto.Member{{{TelegramUserID: 20, Role: mtproto.MemberRoleMember}}},
	}
	svc := NewGroupBootstrapService(store, groupOps, mt, nil, nil, nil)

	if err := svc.BootstrapGroup(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "creator-1", Policy: GroupPolicyKick}); err != nil {
		t.Fatalf("BootstrapGroup() error = %v, want nil", err)
	}

	if got := groupOps.kicks; len(got) != 1 || got[0] != 20 {
		t.Fatalf("kicks = %v, want [20]", got)
	}
	if got := store.untrackedDelete; len(got) == 0 || got[len(got)-1] != 20 {
		t.Fatalf("untracked delete = %v, want cleanup for 20", got)
	}
}

func TestGroupBootstrapBlockedSubscriberStaysUntracked(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{
		creators: map[string]Creator{
			"creator-1": {ID: "creator-1", BlocklistSyncEnabled: true},
		},
		identities: map[int64]UserIdentity{
			10: {TelegramUserID: 10, TwitchUserID: "tw-10"},
		},
		subscribers: map[string]map[string]bool{
			"creator-1": {"tw-10": true},
		},
		blocked: map[string]map[string]bool{
			"creator-1": {"tw-10": true},
		},
	}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		dumps:  [][]mtproto.Member{{{TelegramUserID: 10, Role: mtproto.MemberRoleMember}}},
	}
	svc := NewGroupBootstrapService(store, groupOps, mt, nil, nil, nil)

	if err := svc.BootstrapGroup(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "creator-1", Policy: GroupPolicyObserve}); err != nil {
		t.Fatalf("BootstrapGroup() error = %v, want nil", err)
	}
	if len(store.trackedAdds) != 0 {
		t.Fatalf("tracked adds = %v, want none", store.trackedAdds)
	}
	if got := store.untrackedAdds; len(got) != 1 || got[0] != 10 {
		t.Fatalf("untracked adds = %v, want [10]", got)
	}
}

func TestGroupBootstrapRetriesAndThenFailsSilently(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{
		creators: map[string]Creator{
			"creator-1": {ID: "creator-1"},
		},
		subscribers: map[string]map[string]bool{"creator-1": {}},
		blocked:     map[string]map[string]bool{"creator-1": {}},
	}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		errs: []error{
			mtproto.StageError{Stage: "join_failed", Err: errors.New("boom")},
			mtproto.StageError{Stage: "join_failed", Err: errors.New("boom")},
			mtproto.StageError{Stage: "join_failed", Err: errors.New("boom")},
		},
	}
	eventsSink := &bootstrapEventSink{}
	svc := NewGroupBootstrapService(store, groupOps, mt, nil, eventsSink, nil)
	svc.retryDelays = []time.Duration{0, 0}

	err := svc.BootstrapGroup(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "creator-1", Policy: GroupPolicyObserve})
	if err == nil {
		t.Fatal("BootstrapGroup() error = nil, want non-nil")
	}
	if groupOps.inviteCalls != 3 {
		t.Fatalf("invite calls = %d, want 3", groupOps.inviteCalls)
	}
	outcomes := make([]string, 0, len(eventsSink.events))
	for _, evt := range eventsSink.events {
		outcomes = append(outcomes, evt.Outcome)
	}
	want := []string{"join_failed", "retry", "join_failed", "retry", "join_failed", "failed"}
	if len(outcomes) != len(want) {
		t.Fatalf("outcomes = %v, want %v", outcomes, want)
	}
	for i := range want {
		if outcomes[i] != want[i] {
			t.Fatalf("outcomes[%d] = %q, want %q (all=%v)", i, outcomes[i], want[i], outcomes)
		}
	}
}

func TestGroupBootstrapListFailureDoesNotCleanupKickMTProtoUser(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{
		creators: map[string]Creator{
			"creator-1": {ID: "creator-1"},
		},
		subscribers: map[string]map[string]bool{"creator-1": {}},
		blocked:     map[string]map[string]bool{"creator-1": {}},
	}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		errs: []error{
			mtproto.StageError{Stage: "list_failed", Err: errors.New("boom")},
		},
	}
	svc := NewGroupBootstrapService(store, groupOps, mt, nil, nil, nil)

	_, err := svc.bootstrapAttempt(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "creator-1", Policy: GroupPolicyObserve})
	if err == nil {
		t.Fatal("bootstrapAttempt() error = nil, want non-nil")
	}
	if len(groupOps.kicks) != 0 {
		t.Fatalf("cleanup kicks = %v, want none", groupOps.kicks)
	}
}

// TestGroupBootstrapGodMemberEligibleWhenCreatorMissing guards against a
// regression where a missing creator row caused god users under a kick-policy
// group to be kicked: the creatorFound guard skipped IsEligibleTrackedMember
// entirely, so the god fast-path inside it never fired. God users must stay
// eligible regardless of creator existence so orphaned creator rows cannot
// eject them.
func TestGroupBootstrapGodMemberEligibleWhenCreatorMissing(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{}
	groupOps := &bootstrapGroupOpsStub{}
	mt := &bootstrapMTProtoStub{
		selfID: 999,
		dumps:  [][]mtproto.Member{{{TelegramUserID: 42, Role: mtproto.MemberRoleMember}}},
	}
	svc := NewGroupBootstrapService(store, groupOps, mt, NewGodAccessChecker(42), nil, nil)

	if err := svc.BootstrapGroup(t.Context(), ManagedGroup{ChatID: 100, CreatorID: "missing-creator", Policy: GroupPolicyKick}); err != nil {
		t.Fatalf("BootstrapGroup() error = %v, want nil", err)
	}

	if len(groupOps.kicks) != 0 {
		t.Fatalf("god user was kicked: kicks = %v, want none", groupOps.kicks)
	}
	if got := store.trackedAdds; len(got) != 1 || got[0] != 42 {
		t.Fatalf("tracked adds = %v, want [42] (god user promoted)", got)
	}
}
