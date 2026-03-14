package usecase

import (
	"context"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

const (
	creatorActivationResultSuccess      = "success"
	creatorActivationResultEventSubFail = "eventsub_failed"
	creatorActivationResultDumpFail     = "dump_failed"
)

type creatorActivationService interface {
	EnsureEventSubForCreators(ctx context.Context, creators []core.Creator) error
	DumpCurrentSubscribers(ctx context.Context, creator core.Creator) (int, error)
}

// CreatorActivationResult is the application-layer result for creator activation.
type CreatorActivationResult struct {
	ResultLabel     string
	SubscriberCount int
}

// CreatorActivationUseCase coordinates first-group creator activation.
type CreatorActivationUseCase struct {
	svc    creatorActivationService
	events events.EventSink
}

// NewCreatorActivationUseCase builds a creator activation use case.
func NewCreatorActivationUseCase(svc creatorActivationService, sink events.EventSink) *CreatorActivationUseCase {
	return &CreatorActivationUseCase{svc: svc, events: events.EnsureSink(sink)}
}

// Activate ensures EventSub subscriptions and refreshes the subscriber cache.
func (u *CreatorActivationUseCase) Activate(ctx context.Context, creator core.Creator) (CreatorActivationResult, error) {
	if err := u.svc.EnsureEventSubForCreators(ctx, []core.Creator{creator}); err != nil {
		u.recordResult(ctx, creator.ID, creatorActivationResultEventSubFail)
		return CreatorActivationResult{ResultLabel: creatorActivationResultEventSubFail}, fmt.Errorf("ensure eventsub for creator %s: %w", creator.ID, err)
	}
	count, err := u.svc.DumpCurrentSubscribers(ctx, creator)
	if err != nil {
		u.recordResult(ctx, creator.ID, creatorActivationResultDumpFail)
		return CreatorActivationResult{ResultLabel: creatorActivationResultDumpFail}, fmt.Errorf("dump subscribers for creator %s: %w", creator.ID, err)
	}
	u.recordResult(ctx, creator.ID, creatorActivationResultSuccess)
	return CreatorActivationResult{
		ResultLabel:     creatorActivationResultSuccess,
		SubscriberCount: count,
	}, nil
}

func (u *CreatorActivationUseCase) recordResult(ctx context.Context, creatorID, result string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameCreatorActivation,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}
