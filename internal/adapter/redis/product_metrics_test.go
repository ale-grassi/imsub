package redis

import (
	"testing"
	"time"

	"imsub/internal/core"
)

func TestProductMetricCounts(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.TrackTelegramActiveUser(ctx, 11, time.Unix(100, 0).UTC()); err != nil {
		t.Fatalf("TrackTelegramActiveUser(11) failed: %v", err)
	}
	if err := s.TrackTelegramActiveUser(ctx, 22, time.Unix(200, 0).UTC()); err != nil {
		t.Fatalf("TrackTelegramActiveUser(22) failed: %v", err)
	}
	if err := s.TrackTelegramActiveUser(ctx, 11, time.Unix(300, 0).UTC()); err != nil {
		t.Fatalf("TrackTelegramActiveUser(11 again) failed: %v", err)
	}

	gotActive, err := s.CountTelegramActiveUsersSince(ctx, time.Unix(150, 0).UTC())
	if err != nil {
		t.Fatalf("CountTelegramActiveUsersSince failed: %v", err)
	}
	if gotActive != 2 {
		t.Fatalf("CountTelegramActiveUsersSince = %d, want 2", gotActive)
	}

	if _, err := s.SaveUserIdentityOnly(ctx, 7, "tw-7", "viewer7", "Viewer7", "en"); err != nil {
		t.Fatalf("SaveUserIdentityOnly failed: %v", err)
	}
	if err := s.UpsertCreator(ctx, creatorFixture("c1", 7)); err != nil {
		t.Fatalf("UpsertCreator failed: %v", err)
	}
	if err := s.UpsertManagedGroup(ctx, managedGroupFixture(1001, "c1")); err != nil {
		t.Fatalf("UpsertManagedGroup failed: %v", err)
	}

	gotViewers, err := s.CountLinkedViewerAccounts(ctx)
	if err != nil {
		t.Fatalf("CountLinkedViewerAccounts failed: %v", err)
	}
	if gotViewers != 1 {
		t.Fatalf("CountLinkedViewerAccounts = %d, want 1", gotViewers)
	}
	gotCreators, err := s.CountLinkedCreatorAccounts(ctx)
	if err != nil {
		t.Fatalf("CountLinkedCreatorAccounts failed: %v", err)
	}
	if gotCreators != 1 {
		t.Fatalf("CountLinkedCreatorAccounts = %d, want 1", gotCreators)
	}
	gotGroups, err := s.CountManagedGroups(ctx)
	if err != nil {
		t.Fatalf("CountManagedGroups failed: %v", err)
	}
	if gotGroups != 1 {
		t.Fatalf("CountManagedGroups = %d, want 1", gotGroups)
	}
}

func creatorFixture(id string, ownerTelegramID int64) core.Creator {
	return core.Creator{
		ID:              id,
		TwitchLogin:     id,
		OwnerTelegramID: ownerTelegramID,
		AccessToken:     "token",
		RefreshToken:    "refresh",
		UpdatedAt:       time.Unix(500, 0).UTC(),
	}
}

func managedGroupFixture(chatID int64, creatorID string) core.ManagedGroup {
	return core.ManagedGroup{
		ChatID:       chatID,
		CreatorID:    creatorID,
		GroupName:    "VIP",
		RegisteredAt: time.Unix(500, 0).UTC(),
	}
}
