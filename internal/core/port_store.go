package core

import (
	"context"
	"time"
)

// Store defines the full data access contract for the application.
type Store interface {
	Ping(ctx context.Context) error
	Close() error
	EnsureSchema(ctx context.Context) error

	// --- User identity and creator-membership operations ---

	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	SaveUserIdentityOnly(ctx context.Context, telegramUserID int64, twitchUserID, twitchLogin, language string) (displacedUserID int64, err error)
	SaveUserCreator(ctx context.Context, telegramUserID int64, creatorID, twitchUserID, twitchLogin, language string) (displacedUserID int64, err error)
	UserCreatorIDs(ctx context.Context, telegramUserID int64) ([]string, error)
	RemoveUserCreatorByTelegram(ctx context.Context, telegramUserID int64, creatorID string) error
	AddUserCreatorMembership(ctx context.Context, telegramUserID int64, creatorID string) error
	RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (telegramUserID int64, found bool, err error)
	DeleteAllUserData(ctx context.Context, telegramUserID int64) error

	// --- Creator CRUD and group binding ---

	Creator(ctx context.Context, creatorID string) (Creator, bool, error)
	ListCreators(ctx context.Context) ([]Creator, error)
	ListActiveCreators(ctx context.Context) ([]Creator, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	LoadCreatorsByIDs(ctx context.Context, ids []string, filter func(Creator) bool) ([]Creator, error)
	UpsertCreator(ctx context.Context, c Creator) error
	DeleteCreatorData(ctx context.Context, ownerTelegramID int64) (deletedCount int, deletedNames []string, err error)
	UpdateCreatorGroup(ctx context.Context, creatorID string, groupChatID int64, groupName string) error
	UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string) error
	MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (transitioned bool, err error)
	MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastSync(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error
	CreatorAuthReconnectRequiredCount(ctx context.Context) (int, error)

	// --- OAuth state ---

	SaveOAuthState(ctx context.Context, state string, payload OAuthStatePayload, ttl time.Duration) error
	OAuthState(ctx context.Context, state string) (OAuthStatePayload, error)
	DeleteOAuthState(ctx context.Context, state string) (OAuthStatePayload, error)

	// --- Subscriber cache and bulk dump ---

	IsCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	AddCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error
	RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error
	CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error)
	NewSubscriberDumpKey(creatorID string) string
	AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error
	FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	CleanupSubscriberDump(ctx context.Context, tmpKey string)

	// --- Event deduplication ---

	MarkEventProcessed(ctx context.Context, messageID string, ttl time.Duration) (alreadyProcessed bool, err error)

	// --- Data consistency audits and repairs ---

	RepairUserCreatorReverseIndex(ctx context.Context, creators []Creator) (indexUsers, repairedUsers, missingLinks, staleLinks int, err error)
	ActiveCreatorIDsWithoutGroup(ctx context.Context, creators []Creator) (int, error)
}
