package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"imsub/internal/core"
)

const privacyExportSchemaVersion = "1"

type privacyStore interface {
	ConsentRecord(ctx context.Context, telegramUserID int64) (core.ConsentRecord, bool, error)
	SaveConsentRecord(ctx context.Context, record core.ConsentRecord) error
	DeleteConsentRecord(ctx context.Context, telegramUserID int64) error
	UserIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]core.ManagedGroup, error)
	ListTrackedGroupIDsForUser(ctx context.Context, telegramUserID int64) ([]int64, error)
	ListUntrackedMembershipsForUser(ctx context.Context, telegramUserID int64) ([]core.UntrackedGroupMember, error)
	DeleteAllUntrackedMembershipsForUser(ctx context.Context, telegramUserID int64) (int, error)
	DeleteOAuthStatesForUser(ctx context.Context, telegramUserID int64) (int, error)
	ListPrivacyReceipts(ctx context.Context, telegramUserID int64) ([]core.PrivacyReceipt, error)
	SavePrivacyReceipt(ctx context.Context, receipt core.PrivacyReceipt, retention time.Duration) error
	DeletePrivacyArtifacts(ctx context.Context, telegramUserID int64, keepReceiptID string) error
}

// PrivacyUseCase coordinates consent, export generation, and receipt persistence.
type PrivacyUseCase struct {
	store            privacyStore
	now              func() time.Time
	policyVersion    string
	receiptRetention time.Duration
}

// NewPrivacyUseCase creates a privacy use case with stable defaults.
func NewPrivacyUseCase(store privacyStore, policyVersion string, receiptRetention time.Duration) *PrivacyUseCase {
	if policyVersion == "" {
		policyVersion = "v1"
	}
	if receiptRetention <= 0 {
		receiptRetention = 30 * 24 * time.Hour
	}
	return &PrivacyUseCase{
		store:            store,
		now:              func() time.Time { return time.Now().UTC() },
		policyVersion:    policyVersion,
		receiptRetention: receiptRetention,
	}
}

// Consent returns the persisted consent record for the Telegram user.
func (u *PrivacyUseCase) Consent(ctx context.Context, telegramUserID int64) (core.ConsentRecord, bool, error) {
	record, ok, err := u.store.ConsentRecord(ctx, telegramUserID)
	if err != nil {
		return core.ConsentRecord{}, false, fmt.Errorf("load consent: %w", err)
	}
	return record, ok, nil
}

// GrantConsent records explicit user consent.
func (u *PrivacyUseCase) GrantConsent(ctx context.Context, telegramUserID int64, lang string) (core.ConsentRecord, error) {
	record := core.ConsentRecord{
		TelegramUserID: telegramUserID,
		Language:       lang,
		PolicyVersion:  u.policyVersion,
		GrantedAt:      u.now(),
	}
	if err := u.store.SaveConsentRecord(ctx, record); err != nil {
		return core.ConsentRecord{}, fmt.Errorf("save consent: %w", err)
	}
	return record, nil
}

// Export builds a machine-readable snapshot of the personal data currently held for the user.
func (u *PrivacyUseCase) Export(ctx context.Context, telegramUserID int64) (core.PrivacyExport, error) {
	out := core.PrivacyExport{
		SchemaVersion: privacyExportSchemaVersion,
		ExportedAt:    u.now(),
		User: core.PrivacyExportUser{
			TelegramUserID: telegramUserID,
		},
	}
	if consent, ok, err := u.store.ConsentRecord(ctx, telegramUserID); err != nil {
		return core.PrivacyExport{}, fmt.Errorf("load consent: %w", err)
	} else if ok {
		out.Consent = &consent
	}
	if viewer, ok, err := u.store.UserIdentity(ctx, telegramUserID); err != nil {
		return core.PrivacyExport{}, fmt.Errorf("load viewer identity: %w", err)
	} else if ok {
		out.Viewer = &viewer
	}
	if creator, ok, err := u.store.OwnedCreatorForUser(ctx, telegramUserID); err != nil {
		return core.PrivacyExport{}, fmt.Errorf("load creator: %w", err)
	} else if ok {
		out.Creator = exportSafeCreator(creator)
		groups, err := u.store.ListManagedGroupsByCreator(ctx, creator.ID)
		if err != nil {
			return core.PrivacyExport{}, fmt.Errorf("list managed groups: %w", err)
		}
		out.ManagedGroups = groups
	}
	trackedGroupIDs, err := u.store.ListTrackedGroupIDsForUser(ctx, telegramUserID)
	if err != nil {
		return core.PrivacyExport{}, fmt.Errorf("list tracked group ids: %w", err)
	}
	out.TrackedGroupIDs = trackedGroupIDs
	untracked, err := u.store.ListUntrackedMembershipsForUser(ctx, telegramUserID)
	if err != nil {
		return core.PrivacyExport{}, fmt.Errorf("list untracked memberships: %w", err)
	}
	out.UntrackedMemberships = untracked
	receipts, err := u.store.ListPrivacyReceipts(ctx, telegramUserID)
	if err != nil {
		return core.PrivacyExport{}, fmt.Errorf("list privacy receipts: %w", err)
	}
	out.Receipts = receipts
	return out, nil
}

// RecordDeletionReceipt stores a retained proof of erasure after reset completes.
func (u *PrivacyUseCase) RecordDeletionReceipt(ctx context.Context, telegramUserID int64, res ResetResult) (core.PrivacyReceipt, error) {
	id, err := newPrivacyReceiptID(u.now())
	if err != nil {
		return core.PrivacyReceipt{}, fmt.Errorf("generate privacy receipt id: %w", err)
	}
	deletedAncillary, err := u.cleanupAfterErasure(ctx, telegramUserID)
	if err != nil {
		return core.PrivacyReceipt{}, err
	}
	receipt := core.PrivacyReceipt{
		ID:             id,
		TelegramUserID: telegramUserID,
		Kind:           "erasure",
		Scope:          string(res.Scope),
		Result:         "ok",
		RequestedAt:    u.now(),
		CompletedAt:    u.now(),
		DeletedViewer:  res.Scope == ResetScopeViewer || res.Scope == ResetScopeBoth,
		DeletedCreator: res.Scope == ResetScopeCreator || res.Scope == ResetScopeBoth,
		DeletedGroups:  res.GroupCount + res.DeletedCount,
		DeletedAncillary: deletedAncillary + res.CreatorCleanup.TargetedMembershipCount +
			res.CreatorCleanup.ManagedGroupCount + res.GroupResolution.TotalCount,
	}
	if err := u.store.SavePrivacyReceipt(ctx, receipt, u.receiptRetention); err != nil {
		return core.PrivacyReceipt{}, fmt.Errorf("save privacy receipt: %w", err)
	}
	return receipt, nil
}

func (u *PrivacyUseCase) cleanupAfterErasure(ctx context.Context, telegramUserID int64) (int, error) {
	deletedAncillary := 0
	oauthStateCount, err := u.store.DeleteOAuthStatesForUser(ctx, telegramUserID)
	if err != nil {
		return 0, fmt.Errorf("delete oauth states: %w", err)
	}
	deletedAncillary += oauthStateCount
	if err := u.store.DeletePrivacyArtifacts(ctx, telegramUserID, ""); err != nil {
		return 0, fmt.Errorf("delete privacy artifacts: %w", err)
	}
	return deletedAncillary, nil
}

func exportSafeCreator(creator core.Creator) *core.PrivacyExportCreator {
	copyScopes := append([]string(nil), creator.GrantedScopes...)
	return &core.PrivacyExportCreator{
		ID:                   creator.ID,
		TwitchLogin:          creator.TwitchLogin,
		TwitchDisplayName:    creator.TwitchDisplayName,
		OwnerTelegramID:      creator.OwnerTelegramID,
		GrantedScopes:        copyScopes,
		UpdatedAt:            creator.UpdatedAt,
		AuthStatus:           creator.AuthStatus,
		AuthErrorCode:        creator.AuthErrorCode,
		AuthStatusAt:         creator.AuthStatusAt,
		LastSyncAt:           creator.LastSyncAt,
		LastBanSyncAt:        creator.LastBanSyncAt,
		LastNoticeAt:         creator.LastNoticeAt,
		BlocklistSyncEnabled: creator.BlocklistSyncEnabled,
		SubscriptionEndGrace: creator.SubscriptionEndGrace,
	}
}

func newPrivacyReceiptID(now time.Time) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("rand read privacy receipt suffix: %w", err)
	}
	return fmt.Sprintf("%d-%s", now.UnixNano(), hex.EncodeToString(suffix[:])), nil
}
