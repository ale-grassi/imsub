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
	NewSubscriberDumpKey(creatorID string) string
	AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error
	FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	CleanupSubscriberDump(ctx context.Context, tmpKey string)
	UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string) error
}

// EventSub manages EventSub lifecycle checks, creation, and subscriber dumps.
type EventSub struct {
	store          eventSubStore
	twitch         TwitchAPI
	log            *slog.Logger
	bootstrapDelay time.Duration
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
	return total, nil
}

func (e *EventSub) refreshCreatorAccessToken(ctx context.Context, creator Creator) (Creator, error) {
	tok, err := e.twitch.RefreshToken(ctx, creator.RefreshToken)
	if err != nil {
		return creator, fmt.Errorf("refresh token call: %w", err)
	}
	if err := e.store.UpdateCreatorTokens(ctx, creator.ID, tok.AccessToken, tok.RefreshToken); err != nil {
		return creator, fmt.Errorf("update creator tokens in store: %w", err)
	}
	creator.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		creator.RefreshToken = tok.RefreshToken
	}
	return creator, nil
}

func isUnauthorized(err error) bool {
	return err != nil && errors.Is(err, ErrUnauthorized)
}
