package core

import (
	"context"
)

// TwitchAPI abstracts Twitch HTTP API calls, enabling mock-based testing
// without real network requests.
type TwitchAPI interface {
	ExchangeCode(ctx context.Context, code string) (TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (TokenResponse, error)
	FetchUser(ctx context.Context, userToken string) (id, login, displayName string, err error)
	AppToken(ctx context.Context) (string, error)
	CreateEventSub(ctx context.Context, appToken, broadcasterID, eventType, version string) error
	EnabledEventSubTypes(ctx context.Context, appToken, creatorID string) (map[string]bool, error)
	ListSubscriberPage(ctx context.Context, accessToken, broadcasterID, cursor string) (userIDs []string, nextCursor string, err error)
}
