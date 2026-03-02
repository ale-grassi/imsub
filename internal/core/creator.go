package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type eventSubChecker interface {
	IsEventSubActiveForCreator(ctx context.Context, creatorID string) (bool, error)
}

// EventSubState describes EventSub health for a creator.
type EventSubState string

const (
	// EventSubUnknown means EventSub status could not be determined.
	EventSubUnknown EventSubState = "unknown"
	// EventSubActive means required EventSub subscriptions are active.
	EventSubActive EventSubState = "active"
	// EventSubInactive means at least one required EventSub subscription is missing.
	EventSubInactive EventSubState = "inactive"
)

// Status summarizes creator runtime state.
type Status struct {
	EventSub           EventSubState
	SubscriberCount    int64
	HasSubscriberCount bool
}

type creatorStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (Creator, bool, error)
	CreatorSubscriberCount(ctx context.Context, creatorID string) (int64, error)
}

// CreatorService provides creator domain/application read operations.
type CreatorService struct {
	store   creatorStore
	checker eventSubChecker
	log     *slog.Logger
}

// NewCreator builds a CreatorService with optional logger fallback.
func NewCreator(store creatorStore, checker eventSubChecker, logger *slog.Logger) *CreatorService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreatorService{
		store:   store,
		checker: checker,
		log:     logger,
	}
}

// LoadOwnedCreator returns the owned creator for the given telegram user
func (c *CreatorService) LoadOwnedCreator(ctx context.Context, telegramUserID int64) (Creator, bool, error) {
	return c.store.OwnedCreatorForUser(ctx, telegramUserID)
}

// LoadStatus returns the current event sub and subscriber status.
func (c *CreatorService) LoadStatus(ctx context.Context, creatorID string) (Status, error) {
	status := Status{
		EventSub: EventSubUnknown,
	}

	var errs []error
	active, err := c.checker.IsEventSubActiveForCreator(ctx, creatorID)
	switch {
	case err != nil:
		c.log.Warn("creator status eventsub check failed", "creator_id", creatorID, "error", err)
		errs = append(errs, fmt.Errorf("eventsub check: %w", err))
	case active:
		status.EventSub = EventSubActive
	default:
		status.EventSub = EventSubInactive
	}

	count, err := c.store.CreatorSubscriberCount(ctx, creatorID)
	if err != nil {
		c.log.Warn("creator status subscriber count failed", "creator_id", creatorID, "error", err)
		errs = append(errs, fmt.Errorf("subscriber count: %w", err))
		return status, errors.Join(errs...)
	}
	status.SubscriberCount = count
	status.HasSubscriberCount = true
	return status, errors.Join(errs...)
}
