package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"imsub/internal/events"
)

var errNilContext = errors.New("nil context")

type eventSubStore interface {
	ListActiveCreators(ctx context.Context) ([]Creator, error)
	CreatorAuthReconnectRequiredCount(ctx context.Context) (int, error)
	NewSubscriberDumpKey(creatorID string) string
	AddToSubscriberDump(ctx context.Context, tmpKey string, userIDs []string) error
	FinalizeSubscriberDump(ctx context.Context, creatorID, tmpKey string, hasData bool) error
	CleanupSubscriberDump(ctx context.Context, tmpKey string)
	UpdateCreatorTokens(ctx context.Context, creatorID, accessToken, refreshToken string, grantedScopes []string) error
	MarkCreatorAuthReconnectRequired(ctx context.Context, creatorID, errorCode string, at time.Time) (transitioned bool, err error)
	MarkCreatorAuthHealthy(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastSync(ctx context.Context, creatorID string, at time.Time) error
	UpdateCreatorLastReconnectNotice(ctx context.Context, creatorID string, at time.Time) error
}

type creatorReconnectNotifier interface {
	NotifyCreatorReconnectRequired(ctx context.Context, creator Creator) error
}

const creatorAuthErrorTokenRefreshFailed = "token_refresh_failed"
const eventSubDeletePause = 50 * time.Millisecond

// EventSubService manages EventSub lifecycle checks, creation, and subscriber dumps.
type EventSubService struct {
	store    eventSubStore
	twitch   TwitchAPI
	log      *slog.Logger
	notifier creatorReconnectNotifier
	observer events.EventSink
}

// NewEventSubService creates an EventSub service with default timings.
func NewEventSubService(store eventSubStore, twitchAPI TwitchAPI, logger *slog.Logger) *EventSubService {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventSubService{
		store:  store,
		twitch: twitchAPI,
		log:    logger,
	}
}

// SetNotifier wires a reconnect-required notifier into EventSub flows.
func (e *EventSubService) SetNotifier(notifier creatorReconnectNotifier) {
	e.notifier = notifier
}

// SetObserver wires metrics/observability hooks into EventSub flows.
func (e *EventSubService) SetObserver(observer events.EventSink) {
	e.observer = observer
}

// SyncReconnectRequiredGauge refreshes the reconnect-required gauge from storage.
func (e *EventSubService) SyncReconnectRequiredGauge(ctx context.Context) {
	if e == nil || e.observer == nil {
		return
	}
	count, err := e.store.CreatorAuthReconnectRequiredCount(ctx)
	if err != nil {
		e.log.Warn("eventsub reconnect-required gauge sync failed", "error", err)
		return
	}
	e.observer.Emit(ctx, events.Event{
		Name:  events.NameCreatorsReconnectRequired,
		Count: count,
	})
}

// ReconcileEventSubsOnce performs a single reconciliation pass: removes orphaned
// EventSub subscriptions and creates missing ones for active creators.
func (e *EventSubService) ReconcileEventSubsOnce(ctx context.Context) error {
	creators, err := e.store.ListActiveCreators(ctx)
	if err != nil {
		return fmt.Errorf("list active creators: %w", err)
	}

	subs, err := e.twitch.ListEventSubs(ctx, ListEventSubsOpts{})
	if err != nil {
		return fmt.Errorf("list eventsubs: %w", err)
	}

	activeSet := make(map[string]struct{}, len(creators))
	for _, c := range creators {
		activeSet[c.ID] = struct{}{}
	}

	// Delete orphaned subscriptions.
	deletedOrphan := false
	for _, sub := range subs {
		if _, ok := activeSet[sub.BroadcasterID]; ok {
			continue
		}
		if deletedOrphan {
			if err := sleepContext(ctx, eventSubDeletePause); err != nil {
				return fmt.Errorf("pause orphaned eventsub deletion: %w", err)
			}
		}
		e.log.Info("deleting orphaned eventsub", "sub_id", sub.ID, "broadcaster_id", sub.BroadcasterID, "type", sub.Type)
		if err := e.twitch.DeleteEventSub(ctx, sub.ID); err != nil {
			e.log.Warn("delete orphaned eventsub failed", "sub_id", sub.ID, "error", err)
		}
		deletedOrphan = true
	}

	// Find active creators missing required subscriptions.
	if len(creators) == 0 {
		e.log.Info("eventsub reconcile: no active creators")
		return nil
	}

	enabledByCreator := make(map[string]map[string]bool, len(creators))
	for _, sub := range subs {
		if sub.Status != "enabled" {
			continue
		}
		if _, ok := activeSet[sub.BroadcasterID]; !ok {
			continue
		}
		enabledTypes := enabledByCreator[sub.BroadcasterID]
		if enabledTypes == nil {
			enabledTypes = make(map[string]bool, 3)
			enabledByCreator[sub.BroadcasterID] = enabledTypes
		}
		enabledTypes[sub.Type] = true
	}

	inactive := make([]Creator, 0, len(creators))
	for _, creator := range creators {
		enabledTypes := enabledByCreator[creator.ID]
		missing := false
		for _, eventType := range requiredEventSubTypes(creator) {
			if !enabledTypes[eventType] {
				missing = true
				break
			}
		}
		if missing {
			inactive = append(inactive, creator)
		}
	}
	if len(inactive) == 0 {
		e.log.Info("eventsub reconcile: all active creators healthy", "count", len(creators))
		return nil
	}

	e.log.Info("eventsub reconcile: repairing inactive creators", "inactive", len(inactive), "total", len(creators))
	if err := e.EnsureEventSubForCreators(ctx, inactive); err != nil {
		return fmt.Errorf("ensure eventsubs: %w", err)
	}
	return nil
}

// DeleteEventSubsForCreator removes all EventSub subscriptions for a given creator.
// Best-effort: logs failures but continues.
func (e *EventSubService) DeleteEventSubsForCreator(ctx context.Context, creatorID string) error {
	subs, err := e.twitch.ListEventSubs(ctx, ListEventSubsOpts{UserID: creatorID})
	if err != nil {
		return fmt.Errorf("list eventsubs for delete: %w", err)
	}
	deletedAny := false
	for _, sub := range subs {
		if sub.BroadcasterID != creatorID {
			continue
		}
		if deletedAny {
			if err := sleepContext(ctx, eventSubDeletePause); err != nil {
				return fmt.Errorf("pause creator eventsub deletion: %w", err)
			}
		}
		e.log.Info("deleting eventsub for creator", "sub_id", sub.ID, "broadcaster_id", creatorID, "type", sub.Type)
		if err := e.twitch.DeleteEventSub(ctx, sub.ID); err != nil {
			e.log.Warn("delete eventsub for creator failed", "sub_id", sub.ID, "creator_id", creatorID, "error", err)
		}
		deletedAny = true
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("context done: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// FindInactiveEventSubCreators returns creators missing required EventSub subscriptions.
func (e *EventSubService) FindInactiveEventSubCreators(ctx context.Context, creators []Creator) []Creator {
	inactive := make([]Creator, 0, len(creators))
	for _, c := range creators {
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		active, err := e.IsEventSubActiveForCreator(checkCtx, c.ID)
		cancel()
		if err != nil {
			e.log.Warn("eventsub verify failed", "creator_id", c.ID, "creator_login", c.TwitchLogin, "error", err)
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
func (e *EventSubService) EnsureEventSubForCreators(ctx context.Context, creators []Creator) error {
	if len(creators) == 0 {
		return nil
	}
	for _, c := range creators {
		for _, eventType := range requiredEventSubTypes(c) {
			e.log.Debug("ensuring eventsub", "creator_id", c.ID, "type", eventType)
			if err := e.twitch.CreateEventSub(ctx, c.ID, eventType, "1"); err != nil {
				return fmt.Errorf("creating %s for creator %s: %w", eventType, c.ID, err)
			}
		}
	}
	return nil
}

// IsEventSubActiveForCreator reports whether required EventSub types are active.
func (e *EventSubService) IsEventSubActiveForCreator(ctx context.Context, creatorID string) (bool, error) {
	foundTypes, err := e.twitch.EnabledEventSubTypes(ctx, creatorID)
	if err != nil {
		return false, fmt.Errorf("fetch enabled eventsub types: %w", err)
	}
	for _, t := range []string{EventTypeChannelSubscribe, EventTypeChannelSubEnd} {
		if !foundTypes[t] {
			e.log.Debug("eventsub active check missing type", "type", t, "creator_id", creatorID)
			return false, nil
		}
	}
	e.log.Debug("eventsub active check verified", "creator_id", creatorID)
	return true, nil
}

func requiredEventSubTypes(creator Creator) []string {
	types := []string{EventTypeChannelSubscribe, EventTypeChannelSubEnd}
	if creator.BlocklistSyncEnabled {
		types = append(types, EventTypeChannelBan, EventTypeChannelUnban)
	}
	return types
}

// DumpCurrentSubscribers refreshes the cached subscriber set for creator and returns count.
func (e *EventSubService) DumpCurrentSubscribers(ctx context.Context, creator Creator) (int, error) {
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
	dumpIDs := make([]string, 0, dumpIDChunkSize)
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
			var flushed bool
			dumpIDs, flushed, err = appendDumpIDs(dumpIDs, userIDs, func(ids []string) error {
				return e.store.AddToSubscriberDump(ctx, tmpKey, ids)
			})
			if err != nil {
				return total, fmt.Errorf("add to subscriber dump: %w", err)
			}
			wroteAny = wroteAny || flushed
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	flushed, err := flushDumpIDs(dumpIDs, func(ids []string) error {
		return e.store.AddToSubscriberDump(ctx, tmpKey, ids)
	})
	if err != nil {
		return total, fmt.Errorf("add to subscriber dump: %w", err)
	}
	wroteAny = wroteAny || flushed
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

func (e *EventSubService) refreshCreatorAccessToken(ctx context.Context, creator Creator) (Creator, error) {
	return refreshCreatorAccessToken(ctx, creator, e.twitch, e.store, func(result string) {
		e.emitTokenRefresh(ctx, creator.ID, result)
	})
}

func (e *EventSubService) clearCreatorReconnectRequired(ctx context.Context, creator Creator, at time.Time) error {
	if creator.AuthStatus != CreatorAuthReconnectRequired {
		return nil
	}
	if err := e.store.MarkCreatorAuthHealthy(ctx, creator.ID, at); err != nil {
		return fmt.Errorf("mark creator auth healthy: %w", err)
	}
	e.emitAuthTransition(ctx, creator.ID, string(CreatorAuthReconnectRequired), string(CreatorAuthHealthy), creator.AuthErrorCode)
	e.SyncReconnectRequiredGauge(ctx)
	return nil
}

func (e *EventSubService) markCreatorReconnectRequired(ctx context.Context, creator Creator, errorCode string) {
	at := time.Now().UTC()
	transitioned, err := e.store.MarkCreatorAuthReconnectRequired(ctx, creator.ID, errorCode, at)
	if err != nil {
		e.log.Warn("mark creator auth reconnect required failed", "creator_id", creator.ID, "error", err)
		return
	}
	if !transitioned {
		return
	}
	e.emitAuthTransition(ctx, creator.ID, string(CreatorAuthHealthy), string(CreatorAuthReconnectRequired), errorCode)
	e.SyncReconnectRequiredGauge(ctx)
	if e.notifier == nil {
		return
	}
	if err := e.notifier.NotifyCreatorReconnectRequired(ctx, creator); err != nil {
		e.log.Warn("notify creator reconnect required failed", "creator_id", creator.ID, "owner_telegram_id", creator.OwnerTelegramID, "error", err)
		e.emitReconnectNotification(ctx, creator.ID, "failed")
		return
	}
	if err := e.store.UpdateCreatorLastReconnectNotice(ctx, creator.ID, at); err != nil {
		e.log.Warn("update creator last reconnect notice failed", "creator_id", creator.ID, "error", err)
	}
	e.emitReconnectNotification(ctx, creator.ID, "ok")
}

func isUnauthorized(err error) bool {
	return err != nil && errors.Is(err, ErrUnauthorized)
}

func (e *EventSubService) emitTokenRefresh(ctx context.Context, creatorID, result string) {
	if e == nil || e.observer == nil {
		return
	}
	e.observer.Emit(ctx, events.Event{
		Name:    events.NameCreatorTokenRefresh,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}

func (e *EventSubService) emitAuthTransition(ctx context.Context, creatorID, from, to, reason string) {
	if e == nil || e.observer == nil {
		return
	}
	e.observer.Emit(ctx, events.Event{
		Name: events.NameCreatorAuthTransition,
		Fields: map[string]string{
			"creator_id": creatorID,
			"from":       from,
			"to":         to,
			"reason":     reason,
		},
	})
}

func (e *EventSubService) emitReconnectNotification(ctx context.Context, creatorID, result string) {
	if e == nil || e.observer == nil {
		return
	}
	e.observer.Emit(ctx, events.Event{
		Name:    events.NameCreatorReconnectNotice,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}
