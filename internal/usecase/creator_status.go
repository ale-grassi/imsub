package usecase

import (
	"context"
	"errors"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

type creatorStatusService interface {
	LoadOwnedCreator(ctx context.Context, telegramUserID int64) (core.Creator, bool, error)
	LoadManagedGroups(ctx context.Context, creatorID string) ([]core.ManagedGroup, error)
	LoadStatus(ctx context.Context, creator core.Creator) (core.Status, error)
}

// CreatorStatusResult is the application-layer result for creator status loading.
type CreatorStatusResult struct {
	HasCreator  bool
	Creator     core.Creator
	Groups      []core.ManagedGroup
	Status      core.Status
	GroupsError error
	StatusError error
	IsDegraded  bool
}

// CreatorStatusUseCase coordinates creator ownership and status loading.
type CreatorStatusUseCase struct {
	svc    creatorStatusService
	events events.EventSink
}

// NewCreatorStatusUseCase builds a creator status use case.
func NewCreatorStatusUseCase(svc creatorStatusService, sink events.EventSink) *CreatorStatusUseCase {
	return &CreatorStatusUseCase{svc: svc, events: events.EnsureSink(sink)}
}

// LoadOwnedCreator resolves creator ownership without loading runtime status.
func (u *CreatorStatusUseCase) LoadOwnedCreator(ctx context.Context, telegramUserID int64) (core.Creator, bool, error) {
	creator, found, loadErr := u.svc.LoadOwnedCreator(ctx, telegramUserID)
	if loadErr != nil {
		return core.Creator{}, false, fmt.Errorf("load owned creator: %w", loadErr)
	}
	return creator, found, nil
}

// LoadStatus resolves creator ownership, groups, and runtime status.
func (u *CreatorStatusUseCase) LoadStatus(ctx context.Context, telegramUserID int64) (CreatorStatusResult, error) {
	creator, found, loadErr := u.svc.LoadOwnedCreator(ctx, telegramUserID)
	if loadErr != nil {
		u.recordResult(ctx, "", "failed")
		return CreatorStatusResult{}, fmt.Errorf("load owned creator: %w", loadErr)
	}
	if !found {
		u.recordResult(ctx, "", "unlinked")
		return CreatorStatusResult{}, nil
	}

	groups, groupsErr := u.svc.LoadManagedGroups(ctx, creator.ID)
	status, statusErr := u.svc.LoadStatus(ctx, creator)
	outcome := "loaded"
	if groupsErr != nil || statusErr != nil {
		outcome = "degraded"
	}
	u.recordResult(ctx, creator.ID, outcome)

	result := CreatorStatusResult{
		HasCreator:  true,
		Creator:     creator,
		Groups:      groups,
		Status:      status,
		GroupsError: groupsErr,
		StatusError: statusErr,
		IsDegraded:  groupsErr != nil || statusErr != nil,
	}
	if result.IsDegraded {
		return result, result.Error()
	}
	return result, nil
}

func (u *CreatorStatusUseCase) recordResult(ctx context.Context, creatorID, result string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameCreatorStatus,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}

func (r CreatorStatusResult) Error() error {
	return errors.Join(r.GroupsError, r.StatusError)
}
