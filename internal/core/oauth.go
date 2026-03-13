package core

import (
	"context"
	"errors"
	"slices"
	"time"
)

var errMissingScope = errors.New("missing required scope")
var (
	errReconnectWithoutCreator  = errors.New("creator reconnect requested without existing creator")
	errReconnectCreatorMismatch = errors.New("reconnect returned a different creator account")
)

// Kind identifies a user-visible OAuth flow failure mode.
type Kind string

const (
	// KindTokenExchange indicates token exchange failure.
	KindTokenExchange Kind = "token_exchange"
	// KindUserInfo indicates Twitch user fetch failure.
	KindUserInfo Kind = "user_info"
	// KindSave indicates viewer-link persistence failure.
	KindSave Kind = "save"
	// KindScopeMissing indicates missing required OAuth scopes.
	KindScopeMissing Kind = "scope_missing"
	// KindStore indicates creator-link persistence failure.
	KindStore Kind = "store"
	// KindCreatorMismatch indicates reconnect used a different Twitch creator account.
	KindCreatorMismatch Kind = "creator_mismatch"
)

// FlowError wraps an OAuth flow failure with a stable Kind.
type FlowError struct {
	Kind  Kind
	Cause error
}

// Error returns a stable flow error string including the wrapped cause when present.
func (e *FlowError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return string(e.Kind)
	}
	return string(e.Kind) + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *FlowError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ViewerResult contains successful viewer OAuth-link outputs.
type ViewerResult struct {
	TwitchUserID      string
	TwitchLogin       string
	TwitchDisplayName string
	DisplacedUserID   int64
}

// CreatorResult contains successful creator OAuth-link outputs.
type CreatorResult struct {
	Creator                Creator
	BroadcasterDisplayName string
}

type oauthStore interface {
	SaveUserIdentityOnly(ctx context.Context, telegramUserID int64, twitchUserID, twitchLogin, twitchDisplayName, language string) (displacedUserID int64, err error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	UpsertCreator(ctx context.Context, c Creator) error
}

// OAuthService runs OAuth business logic independent from transport/UI concerns.
type OAuthService struct {
	store oauthStore
	api   TwitchAPI
	now   func() time.Time
}

// NewOAuthService creates an OAuth service with a UTC clock.
func NewOAuthService(store oauthStore, api TwitchAPI) *OAuthService {
	return &OAuthService{
		store: store,
		api:   api,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// LinkViewer completes viewer OAuth linking and persists viewer identity.
func (o *OAuthService) LinkViewer(ctx context.Context, code string, payload OAuthStatePayload, lang string) (ViewerResult, error) {
	tok, err := o.api.ExchangeCode(ctx, code)
	if err != nil {
		return ViewerResult{}, &FlowError{Kind: KindTokenExchange, Cause: err}
	}

	twitchUserID, twitchLogin, twitchDisplayName, err := o.api.FetchUser(ctx, tok.AccessToken)
	if err != nil {
		return ViewerResult{}, &FlowError{Kind: KindUserInfo, Cause: err}
	}

	displacedUserID, err := o.store.SaveUserIdentityOnly(ctx, payload.TelegramUserID, twitchUserID, twitchLogin, twitchDisplayName, lang)
	if err != nil {
		return ViewerResult{}, &FlowError{Kind: KindSave, Cause: err}
	}

	return ViewerResult{
		TwitchUserID:      twitchUserID,
		TwitchLogin:       twitchLogin,
		TwitchDisplayName: twitchDisplayName,
		DisplacedUserID:   displacedUserID,
	}, nil
}

// LinkCreator completes creator OAuth linking and upserts creator data.
func (o *OAuthService) LinkCreator(ctx context.Context, code string, payload OAuthStatePayload) (CreatorResult, error) {
	tok, err := o.api.ExchangeCode(ctx, code)
	if err != nil {
		return CreatorResult{}, &FlowError{Kind: KindTokenExchange, Cause: err}
	}
	if !slices.Contains(tok.Scope, ScopeChannelReadSubscriptions) {
		return CreatorResult{}, &FlowError{Kind: KindScopeMissing, Cause: errMissingScope}
	}
	if !slices.Contains(tok.Scope, ScopeModerationRead) {
		return CreatorResult{}, &FlowError{Kind: KindScopeMissing, Cause: errMissingScope}
	}

	broadcasterID, broadcasterLogin, broadcasterDisplayName, err := o.api.FetchUser(ctx, tok.AccessToken)
	if err != nil {
		return CreatorResult{}, &FlowError{Kind: KindUserInfo, Cause: err}
	}

	now := o.now()
	creator := Creator{
		ID:                broadcasterID,
		TwitchLogin:       broadcasterLogin,
		TwitchDisplayName: broadcasterDisplayName,
		OwnerTelegramID:   payload.TelegramUserID,
		AccessToken:       tok.AccessToken,
		RefreshToken:      tok.RefreshToken,
		GrantedScopes:     append([]string(nil), tok.Scope...),
		UpdatedAt:         now,
		AuthStatus:        CreatorAuthHealthy,
		AuthStatusAt:      now,
	}
	if payload.Reconnect {
		existing, ok, err := o.store.OwnedCreatorForUser(ctx, payload.TelegramUserID)
		if err != nil {
			return CreatorResult{}, &FlowError{Kind: KindStore, Cause: err}
		}
		if !ok {
			return CreatorResult{}, &FlowError{Kind: KindStore, Cause: errReconnectWithoutCreator}
		}
		if existing.ID != broadcasterID {
			return CreatorResult{}, &FlowError{Kind: KindCreatorMismatch, Cause: errReconnectCreatorMismatch}
		}
		creator.LastSyncAt = existing.LastSyncAt
		creator.LastNoticeAt = existing.LastNoticeAt
	}
	if err := o.store.UpsertCreator(ctx, creator); err != nil {
		return CreatorResult{}, &FlowError{Kind: KindStore, Cause: err}
	}

	return CreatorResult{
		Creator:                creator,
		BroadcasterDisplayName: broadcasterDisplayName,
	}, nil
}
