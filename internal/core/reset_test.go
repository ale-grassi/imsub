package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
)

type resetFakeStore struct {
	Store
	userCreatorIDs       map[int64][]string
	creatorsByID         map[string]Creator
	deleteAllUserDataErr error
	getIdentityFn        func(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	getCreatorFn         func(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)

	deleteCreatorCount int
	deleteCreatorNames []string
	deleteCreatorErr   error

	deleteAllCalledWith int64
}

func (f *resetFakeStore) UserCreatorIDs(_ context.Context, telegramUserID int64) ([]string, error) {
	return append([]string(nil), f.userCreatorIDs[telegramUserID]...), nil
}

func (f *resetFakeStore) UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error) {
	if f.getIdentityFn != nil {
		return f.getIdentityFn(ctx, telegramUserID)
	}
	return UserIdentity{}, false, nil
}

func (f *resetFakeStore) OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error) {
	if f.getCreatorFn != nil {
		return f.getCreatorFn(ctx, ownerTelegramID)
	}
	return Creator{}, false, nil
}

func (f *resetFakeStore) LoadCreatorsByIDs(_ context.Context, ids []string, filter func(Creator) bool) ([]Creator, error) {
	out := make([]Creator, 0, len(ids))
	for _, id := range ids {
		c, ok := f.creatorsByID[id]
		if !ok {
			continue
		}
		if filter != nil && !filter(c) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *resetFakeStore) DeleteAllUserData(_ context.Context, telegramUserID int64) error {
	f.deleteAllCalledWith = telegramUserID
	return f.deleteAllUserDataErr
}

func (f *resetFakeStore) DeleteCreatorData(_ context.Context, _ int64) (int, []string, error) {
	return f.deleteCreatorCount, append([]string(nil), f.deleteCreatorNames...), f.deleteCreatorErr
}

func TestSubLinkedGroupIDsForUser(t *testing.T) {
	t.Parallel()

	st := &resetFakeStore{
		userCreatorIDs: map[int64][]string{
			7: {"c2", "c1", "c3"},
		},
		creatorsByID: map[string]Creator{
			"c1": {ID: "c1", GroupChatID: 222},
			"c2": {ID: "c2", GroupChatID: 111},
			"c3": {ID: "c3", GroupChatID: 0},
		},
	}
	svc := NewResetter(st, func(context.Context, int64, int64) error { return nil }, nil)

	got, err := svc.SubLinkedGroupIDsForUser(t.Context(), 7)
	if err != nil {
		t.Fatalf("SubLinkedGroupIDsForUser(%d) returned error %v, want nil", 7, err)
	}
	want := []int64{111, 222}
	if !slices.Equal(got, want) {
		t.Errorf("SubLinkedGroupIDsForUser(%d) = %v, want %v", 7, got, want)
	}
}

func TestResetViewerDataAndRevokeGroupAccess(t *testing.T) {
	t.Parallel()

	st := &resetFakeStore{
		userCreatorIDs: map[int64][]string{
			9: {"c1", "c2"},
		},
		creatorsByID: map[string]Creator{
			"c1": {ID: "c1", GroupChatID: 300},
			"c2": {ID: "c2", GroupChatID: 100},
		},
	}
	var kicked []int64
	svc := NewResetter(
		st,
		func(_ context.Context, groupChatID int64, _ int64) error {
			kicked = append(kicked, groupChatID)
			if groupChatID == 300 {
				return errors.New("telegram failure")
			}
			return nil
		},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)

	count, err := svc.ResetViewerDataAndRevokeGroupAccess(t.Context(), 9)
	if err != nil {
		t.Fatalf("ResetViewerDataAndRevokeGroupAccess(%d) returned error %v, want nil", 9, err)
	}
	if count != 2 {
		t.Errorf("ResetViewerDataAndRevokeGroupAccess(%d) count = %d, want %d", 9, count, 2)
	}
	if !slices.Equal(kicked, []int64{100, 300}) {
		t.Errorf("ResetViewerDataAndRevokeGroupAccess(%d) kicked groups = %v, want %v", 9, kicked, []int64{100, 300})
	}
	if st.deleteAllCalledWith != 9 {
		t.Errorf("ResetViewerDataAndRevokeGroupAccess(%d) DeleteAllUserData arg = %d, want %d", 9, st.deleteAllCalledWith, 9)
	}
}

func TestDeleteCreatorDataPassthrough(t *testing.T) {
	t.Parallel()

	st := &resetFakeStore{
		deleteCreatorCount: 1,
		deleteCreatorNames: []string{"creator-a"},
	}
	svc := NewResetter(st, func(context.Context, int64, int64) error { return nil }, nil)

	count, names, err := svc.DeleteCreatorData(t.Context(), 42)
	if err != nil {
		t.Fatalf("DeleteCreatorData(%d) returned error %v, want nil", 42, err)
	}
	if count != 1 || !slices.Equal(names, []string{"creator-a"}) {
		t.Errorf("DeleteCreatorData(%d) = (count=%d, names=%v), want (count=%d, names=%v)", 42, count, names, 1, []string{"creator-a"})
	}
}

func TestExecuteBothReset(t *testing.T) {
	t.Parallel()

	svc := NewResetter(
		&resetFakeStore{
			getIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
				return UserIdentity{TwitchLogin: "viewer1"}, true, nil
			},
			deleteCreatorCount: 2,
			deleteCreatorNames: []string{"c1", "c2"},
		},
		func(context.Context, int64, int64) error { return nil },
		nil,
	)

	// Override linked groups for deterministic count in this unit test.
	svc.store = &resetFakeStore{
		getIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
			return UserIdentity{TwitchLogin: "viewer1"}, true, nil
		},
		userCreatorIDs: map[int64][]string{7: {"c1", "c2", "c3"}},
		creatorsByID: map[string]Creator{
			"c1": {ID: "c1", GroupChatID: 1},
			"c2": {ID: "c2", GroupChatID: 2},
			"c3": {ID: "c3", GroupChatID: 3},
		},
		deleteCreatorCount: 2,
		deleteCreatorNames: []string{"c1", "c2"},
	}

	got, err := svc.ExecuteBothReset(t.Context(), 7)
	if err != nil {
		t.Fatalf("ExecuteBothReset(%d) returned error %v, want nil", 7, err)
	}
	type comparableResult struct {
		HasIdentity  bool
		Identity     UserIdentity
		GroupCount   int
		DeletedCount int
	}
	gotCore := comparableResult{
		HasIdentity:  got.HasIdentity,
		Identity:     got.Identity,
		GroupCount:   got.GroupCount,
		DeletedCount: got.DeletedCount,
	}
	wantCore := comparableResult{
		HasIdentity:  true,
		Identity:     UserIdentity{TwitchLogin: "viewer1"},
		GroupCount:   3,
		DeletedCount: 2,
	}
	if gotCore != wantCore {
		t.Errorf("ExecuteBothReset(%d) core = %+v, want %+v", 7, gotCore, wantCore)
	}
	if !slices.Equal(got.DeletedNames, []string{"c1", "c2"}) {
		t.Errorf("ExecuteBothReset(%d).DeletedNames = %v, want %v", 7, got.DeletedNames, []string{"c1", "c2"})
	}
}

func TestExecuteViewerResetNoIdentity(t *testing.T) {
	t.Parallel()

	svc := NewResetter(&resetFakeStore{}, func(context.Context, int64, int64) error { return nil }, nil)
	got, err := svc.ExecuteViewerReset(t.Context(), 1)
	if err != nil {
		t.Fatalf("ExecuteViewerReset(%d) returned error %v, want nil", 1, err)
	}
	if got.HasIdentity {
		t.Errorf("ExecuteViewerReset(%d).HasIdentity = %t, want %t", 1, got.HasIdentity, false)
	}
}

func TestLoadScopesPropagatesError(t *testing.T) {
	t.Parallel()

	svc := NewResetter(
		&resetFakeStore{
			getIdentityFn: func(context.Context, int64) (UserIdentity, bool, error) {
				return UserIdentity{}, false, errors.New("boom")
			},
		},
		func(context.Context, int64, int64) error { return nil },
		nil,
	)
	_, err := svc.LoadScopes(t.Context(), 1)
	if err == nil {
		t.Fatal("LoadScopes(1) error = nil, want non-nil")
	}
}
