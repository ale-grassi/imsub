package usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
)

type privacyTestStore struct {
	consent                     core.ConsentRecord
	consentOK                   bool
	viewer                      core.UserIdentity
	viewerOK                    bool
	creator                     core.Creator
	creatorOK                   bool
	managedGroups               []core.ManagedGroup
	trackedGroupIDs             []int64
	untrackedMemberships        []core.UntrackedGroupMember
	receipts                    []core.PrivacyReceipt
	deleteUntrackedCount        int
	deleteOAuthStatesCount      int
	savedReceipt                core.PrivacyReceipt
	savedReceiptRetention       time.Duration
	deletePrivacyArtifactsCalls int
}

func (s *privacyTestStore) ConsentRecord(context.Context, int64) (core.ConsentRecord, bool, error) {
	return s.consent, s.consentOK, nil
}

func (s *privacyTestStore) SaveConsentRecord(context.Context, core.ConsentRecord) error { return nil }
func (s *privacyTestStore) DeleteConsentRecord(context.Context, int64) error            { return nil }

func (s *privacyTestStore) UserIdentity(context.Context, int64) (core.UserIdentity, bool, error) {
	return s.viewer, s.viewerOK, nil
}

func (s *privacyTestStore) OwnedCreatorForUser(context.Context, int64) (core.Creator, bool, error) {
	return s.creator, s.creatorOK, nil
}

func (s *privacyTestStore) ListManagedGroupsByCreator(context.Context, string) ([]core.ManagedGroup, error) {
	return append([]core.ManagedGroup(nil), s.managedGroups...), nil
}

func (s *privacyTestStore) ListTrackedGroupIDsForUser(context.Context, int64) ([]int64, error) {
	return append([]int64(nil), s.trackedGroupIDs...), nil
}

func (s *privacyTestStore) ListUntrackedMembershipsForUser(context.Context, int64) ([]core.UntrackedGroupMember, error) {
	return append([]core.UntrackedGroupMember(nil), s.untrackedMemberships...), nil
}

func (s *privacyTestStore) DeleteAllUntrackedMembershipsForUser(context.Context, int64) (int, error) {
	return s.deleteUntrackedCount, nil
}

func (s *privacyTestStore) DeleteOAuthStatesForUser(context.Context, int64) (int, error) {
	return s.deleteOAuthStatesCount, nil
}

func (s *privacyTestStore) ListPrivacyReceipts(context.Context, int64) ([]core.PrivacyReceipt, error) {
	return append([]core.PrivacyReceipt(nil), s.receipts...), nil
}

func (s *privacyTestStore) SavePrivacyReceipt(_ context.Context, receipt core.PrivacyReceipt, retention time.Duration) error {
	s.savedReceipt = receipt
	s.savedReceiptRetention = retention
	return nil
}

func (s *privacyTestStore) DeletePrivacyArtifacts(context.Context, int64, string) error {
	s.deletePrivacyArtifactsCalls++
	return nil
}

func TestPrivacyExportOmitsCreatorTokens(t *testing.T) {
	t.Parallel()

	store := &privacyTestStore{
		creatorOK: true,
		creator: core.Creator{
			ID:              "creator-1",
			TwitchLogin:     "broadcaster",
			OwnerTelegramID: 42,
			AccessToken:     "secret-access",
			RefreshToken:    "secret-refresh",
			GrantedScopes:   []string{"scope:a", "scope:b"},
		},
	}
	uc := NewPrivacyUseCase(store, "v1", 24*time.Hour)

	exported, err := uc.Export(t.Context(), 42)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if exported.Creator == nil {
		t.Fatal("Export() creator = nil, want populated creator")
	}
	if exported.Creator.ID != "creator-1" || exported.Creator.OwnerTelegramID != 42 {
		t.Fatalf("Export() creator = %+v, want id and owner fields preserved", exported.Creator)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("json.Marshal(exported) error = %v", err)
	}
	if strings.Contains(string(raw), "access_token") || strings.Contains(string(raw), "refresh_token") {
		t.Fatalf("Export() JSON should omit oauth tokens, got %s", raw)
	}
}

func TestRecordDeletionReceiptUsesUniqueRandomizedID(t *testing.T) {
	t.Parallel()

	store := &privacyTestStore{
		deleteUntrackedCount:   2,
		deleteOAuthStatesCount: 1,
	}
	now := time.Unix(1700000000, 123)
	uc := NewPrivacyUseCase(store, "v1", 7*24*time.Hour)
	uc.now = func() time.Time { return now }

	first, err := uc.RecordDeletionReceipt(t.Context(), 42, ResetResult{Scope: ResetScopeViewer})
	if err != nil {
		t.Fatalf("first RecordDeletionReceipt() error = %v", err)
	}
	second, err := uc.RecordDeletionReceipt(t.Context(), 42, ResetResult{Scope: ResetScopeViewer})
	if err != nil {
		t.Fatalf("second RecordDeletionReceipt() error = %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("RecordDeletionReceipt() IDs should differ, got %q", first.ID)
	}
	if !strings.HasPrefix(first.ID, "1700000000000000123-") {
		t.Fatalf("RecordDeletionReceipt() ID = %q, want unixnano prefix", first.ID)
	}
	if store.savedReceiptRetention != 7*24*time.Hour {
		t.Fatalf("SavePrivacyReceipt retention = %v, want %v", store.savedReceiptRetention, 7*24*time.Hour)
	}
	if store.deletePrivacyArtifactsCalls != 2 {
		t.Fatalf("DeletePrivacyArtifacts calls = %d, want 2", store.deletePrivacyArtifactsCalls)
	}
}
