package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var errNilContext = errors.New("nil context")

type eventSubStore interface {
	ListActiveCreators(ctx context.Context) ([]Creator, error)
	CreatorAuthReconnectRequiredCount(ctx context.Context) (int, error)
	NewSubscriberDumpKey(creatorID string) string
	AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error
	FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	CleanupSubscriberDump(ctx context.Context, tmpKey string)
	UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string) error
	MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (transitioned bool, err error)
	MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastSync(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error
}

type creatorReconnectNotifier interface {
	NotifyCreatorReconnectRequired(ctx context.Context, creator Creator) error
}

type eventSubObserver interface {
	CreatorTokenRefresh(result string)
	CreatorAuthTransition(from, to, reason string)
	CreatorsReconnectRequired(count int)
	CreatorReconnectNotification(result string)
}

const creatorAuthErrorTokenRefreshFailed = "token_refresh_failed"

// EventSub manages EventSub lifecycle checks, creation, and subscriber dumps.
type EventSub struct {
	store          eventSubStore
	twitch         TwitchAPI
	log            *slog.Logger
	bootstrapDelay time.Duration
	notifier       creatorReconnectNotifier
	observer       eventSubObserver
}

// NewEventSub creates an EventSub service with default timings.
func NewEventSub(store eventSubStore, twitchAPI TwitchAPI, logger *slog.Logger) *EventSub {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventSub{
		store:          store,
		twitch:         twitchAPI,
		log:            logger,
		bootstrapDelay: 3 * time.Second,
	}
}

// SetNotifier wires a reconnect-required notifier into EventSub flows.
func (e *EventSub) SetNotifier(notifier creatorReconnectNotifier) {
	e.notifier = notifier
}

// SetObserver wires metrics/observability hooks into EventSub flows.
func (e *EventSub) SetObserver(observer eventSubObserver) {
	e.observer = observer
}

// SyncReconnectRequiredGauge refreshes the reconnect-required gauge from storage.
func (e *EventSub) SyncReconnectRequiredGauge(ctx context.Context) {
	if e == nil || e.observer == nil {
		return
	}
	count, err := e.store.CreatorAuthReconnectRequiredCount(ctx)
	if err != nil {
		e.log.Warn("eventsub reconnect-required gauge sync failed", "error", err)
		return
	}
	e.observer.CreatorsReconnectRequired(count)
}

// BootstrapEventSub verifies and repairs EventSub subscriptions for active creators.
func (e *EventSub) BootstrapEventSub(ctx context.Context) {
	select {
	case <-time.After(e.bootstrapDelay):
	case <-ctx.Done():
		return
	}

	creators, err := e.store.ListActiveCreators(ctx)
	if err != nil {
		e.log.Warn("eventsub bootstrap listActiveCreators failed", "error", err)
		return
	}
	if len(creators) == 0 {
		e.log.Info("eventsub bootstrap: no active creators to verify")
		return
	}

	inactive := e.FindInactiveEventSubCreators(ctx, creators)
	if len(inactive) == 0 {
		e.log.Info("eventsub bootstrap verify: all active creators healthy", "count", len(creators))
		return
	}

	e.log.Info("eventsub bootstrap: inactive active-creators found, attempting repair", "inactive", len(inactive), "total", len(creators))
	if err := e.EnsureEventSubForCreators(ctx, inactive); err != nil {
		e.log.Warn("eventsub bootstrap repair failed", "error", err)
		return
	}

	afterRepairInactive := e.FindInactiveEventSubCreators(ctx, creators)
	if len(afterRepairInactive) == 0 {
		e.log.Info("eventsub bootstrap repair: all active creators healthy", "count", len(creators))
		return
	}
	for _, c := range afterRepairInactive {
		e.log.Warn("eventsub bootstrap: creator still inactive", "creator_id", c.ID, "creator_name", c.Name)
	}
}

// FindInactiveEventSubCreators returns creators missing required EventSub subscriptions.
func (e *EventSub) FindInactiveEventSubCreators(ctx context.Context, creators []Creator) []Creator {
	appToken, err := e.twitch.AppToken(ctx)
	if err != nil {
		e.log.Warn("eventsub verify app token fetch failed", "error", err)
		return creators
	}

	inactive := make([]Creator, 0, len(creators))
	for _, c := range creators {
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		active, err := e.IsEventSubActiveForCreatorWithToken(checkCtx, appToken, c.ID)
		cancel()
		if err != nil {
			e.log.Warn("eventsub verify failed", "creator_id", c.ID, "creator_name", c.Name, "error", err)
			inactive = append(inactive, c)
			continue
		}
		if !active {
			inactive = append(inactive, c)
		}
	}
	return inactive
}

// EnsureEventSubForCreators creates required EventSub subscriptions for creators.
func (e *EventSub) EnsureEventSubForCreators(ctx context.Context, creators []Creator) error {
	if len(creators) == 0 {
		return nil
	}
	appToken, err := e.twitch.AppToken(ctx)
	if err != nil {
		return fmt.Errorf("app token for ensure eventsub: %w", err)
	}
	for _, c := range creators {
		for _, eventType := range []string{EventTypeChannelSubscribe, EventTypeChannelSubEnd, EventTypeChannelSubGift} {
			e.log.Debug("ensuring eventsub", "creator_id", c.ID, "type", eventType)
			if err := e.twitch.CreateEventSub(ctx, appToken, c.ID, eventType, "1"); err != nil {
				return fmt.Errorf("creating %s for creator %s: %w", eventType, c.ID, err)
			}
		}
	}
	return nil
}

// IsEventSubActiveForCreator reports whether required EventSub types are active.
func (e *EventSub) IsEventSubActiveForCreator(ctx context.Context, creatorID string) (bool, error) {
	appToken, err := e.twitch.AppToken(ctx)
	if err != nil {
		return false, fmt.Errorf("app token for active eventsub check: %w", err)
	}
	return e.IsEventSubActiveForCreatorWithToken(ctx, appToken, creatorID)
}

// IsEventSubActiveForCreatorWithToken reports active status using a provided app token.
func (e *EventSub) IsEventSubActiveForCreatorWithToken(ctx context.Context, appToken, creatorID string) (bool, error) {
	foundTypes, err := e.twitch.EnabledEventSubTypes(ctx, appToken, creatorID)
	if err != nil {
		return false, fmt.Errorf("fetch enabled eventsub types: %w", err)
	}
	for _, t := range []string{EventTypeChannelSubscribe, EventTypeChannelSubEnd, EventTypeChannelSubGift} {
		if !foundTypes[t] {
			e.log.Debug("eventsub active check missing type", "type", t, "creator_id", creatorID)
			return false, nil
		}
	}
	e.log.Debug("eventsub active check verified", "creator_id", creatorID)
	return true, nil
}

// DumpCurrentSubscribers refreshes the cached subscriber set for creator and returns count.
func (e *EventSub) DumpCurrentSubscribers(ctx context.Context, creator Creator) (int, error) {
	if ctx == nil {
		return 0, errNilContext
	}
	total := 0
	var cursor string
	tmpKey := e.store.NewSubscriberDumpKey(creator.ID)
	cleanupCtx := context.WithoutCancel(ctx)
	defer e.store.CleanupSubscriberDump(cleanupCtx, tmpKey)
	refreshed := false
	wroteAny := false
	for {
		userIDs, nextCursor, err := e.twitch.ListSubscriberPage(ctx, creator.AccessToken, creator.ID, cursor)
		if err != nil && !refreshed && isUnauthorized(err) {
			updated, refreshErr := e.refreshCreatorAccessToken(ctx, creator)
			if refreshErr != nil {
				e.markCreatorReconnectRequired(ctx, creator, creatorAuthErrorTokenRefreshFailed)
				return total, fmt.Errorf("refresh access token on dump: %w", refreshErr)
			}
			creator = updated
			refreshed = true
			continue
		}
		if err != nil {
			return total, fmt.Errorf("list subscriber page: %w", err)
		}

		total += len(userIDs)
		if len(userIDs) > 0 {
			if err := e.store.AddToSubscriberDump(ctx, tmpKey, userIDs); err != nil {
				return total, fmt.Errorf("add to subscriber dump: %w", err)
			}
			wroteAny = true
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	if err := e.store.FinalizeSubscriberDump(ctx, creator.ID, tmpKey, wroteAny); err != nil {
		return total, fmt.Errorf("finalize subscriber dump: %w", err)
	}
	now := time.Now().UTC()
	if err := e.store.UpdateCreatorLastSync(ctx, creator.ID, now); err != nil {
		return total, fmt.Errorf("update creator last sync: %w", err)
	}
	if err := e.clearCreatorReconnectRequired(ctx, creator, now); err != nil {
		return total, err
	}
	return total, nil
}

func (e *EventSub) refreshCreatorAccessToken(ctx context.Context, creator Creator) (Creator, error) {
	tok, err := e.twitch.RefreshToken(ctx, creator.RefreshToken)
	if err != nil {
		if e.observer != nil {
			e.observer.CreatorTokenRefresh("failed")
		}
		return creator, fmt.Errorf("refresh token call: %w", err)
	}
	if e.observer != nil {
		e.observer.CreatorTokenRefresh("ok")
	}
	if err := e.store.UpdateCreatorTokens(ctx, creator.ID, tok.AccessToken, tok.RefreshToken); err != nil {
		return creator, fmt.Errorf("update creator tokens in store: %w", err)
	}
	now := time.Now().UTC()
	if err := e.clearCreatorReconnectRequired(ctx, creator, now); err != nil {
		return creator, err
	}
	creator.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		creator.RefreshToken = tok.RefreshToken
	}
	creator.AuthStatus = CreatorAuthHealthy
	creator.AuthErrorCode = ""
	creator.AuthStatusAt = now
	return creator, nil
}

func (e *EventSub) clearCreatorReconnectRequired(ctx context.Context, creator Creator, at time.Time) error {
	if creator.AuthStatus != CreatorAuthReconnectRequired {
		return nil
	}
	if err := e.store.MarkCreatorAuthHealthy(ctx, creator.ID, at); err != nil {
		return fmt.Errorf("mark creator auth healthy: %w", err)
	}
	if e.observer != nil {
		e.observer.CreatorAuthTransition(string(CreatorAuthReconnectRequired), string(CreatorAuthHealthy), creator.AuthErrorCode)
	}
	e.SyncReconnectRequiredGauge(ctx)
	return nil
}

func (e *EventSub) markCreatorReconnectRequired(ctx context.Context, creator Creator, errorCode string) {
	at := time.Now().UTC()
	transitioned, err := e.store.MarkCreatorAuthReconnectRequired(ctx, creator.ID, errorCode, at)
	if err != nil {
		e.log.Warn("mark creator auth reconnect required failed", "creator_id", creator.ID, "error", err)
		return
	}
	if !transitioned {
		return
	}
	if e.observer != nil {
		e.observer.CreatorAuthTransition(string(CreatorAuthHealthy), string(CreatorAuthReconnectRequired), errorCode)
	}
	e.SyncReconnectRequiredGauge(ctx)
	if e.notifier == nil {
		return
	}
	if err := e.notifier.NotifyCreatorReconnectRequired(ctx, creator); err != nil {
		e.log.Warn("notify creator reconnect required failed", "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "error", err)
		if e.observer != nil {
			e.observer.CreatorReconnectNotification("failed")
		}
		return
	}
	if err := e.store.UpdateCreatorLastReconnectNotice(ctx, creator.ID, at); err != nil {
		e.log.Warn("update creator last reconnect notice failed", "creator_id", creator.ID, "error", err)
	}
	if e.observer != nil {
		e.observer.CreatorReconnectNotification("ok")
	}
}

func isUnauthorized(err error) bool {
	return err != nil && errors.Is(err, ErrUnauthorized)
}
