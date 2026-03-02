package handlers

import (
	"context"
	"log/slog"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/httputil"

	"github.com/mymmrac/telego"
)

type controllerStore interface {
	OAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error)
	DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error)
	MarkEventProcessed(ctx context.Context, messageID string, ttl time.Duration) (alreadyProcessed bool, err error)
	AddCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error
}

type metricsObserver interface {
	TelegramWebhookResult(result string)
	OAuthCallback(mode, result string)
	EventSubMessage(messageType, subscriptionType, result string)
}

type viewerOAuthHandler func(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, twitchDisplayName string, err error)
type creatorOAuthHandler func(ctx context.Context, code string, payload core.OAuthStatePayload, lang string) (label string, creatorName string, err error)
type subEndHandler func(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) error

// Dependencies configure HTTP controller construction.
type Dependencies struct {
	Config          config.Config
	Store           controllerStore
	Logger          *slog.Logger
	Observer        metricsObserver
	TelegramUpdates chan<- telego.Update
	ViewerOAuth     viewerOAuthHandler
	CreatorOAuth    creatorOAuthHandler
	SubscriptionEnd subEndHandler
}

// Controller handles OAuth start/callback, EventSub, and Telegram webhooks.
type Controller struct {
	cfg     config.Config
	store   controllerStore
	logger  *slog.Logger
	obs     metricsObserver
	updates chan<- telego.Update
	viewer  viewerOAuthHandler
	creator creatorOAuthHandler
	subEnd  subEndHandler
}

// New creates an HTTP Controller from the provided dependencies.
func New(deps Dependencies) *Controller {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		cfg:     deps.Config,
		store:   deps.Store,
		logger:  logger,
		obs:     deps.Observer,
		updates: deps.TelegramUpdates,
		viewer:  deps.ViewerOAuth,
		creator: deps.CreatorOAuth,
		subEnd:  deps.SubscriptionEnd,
	}
}

func (c *Controller) logCtx(ctx context.Context) *slog.Logger {
	l := c.logger
	if l == nil {
		l = slog.Default()
	}
	if rid := httputil.RequestIDFromContext(ctx); rid != "" {
		return l.With("request_id", rid)
	}
	return l
}
