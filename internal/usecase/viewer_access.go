package usecase

import (
	"context"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

type viewerAccessService interface {
	LoadIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error)
	BuildJoinTargets(ctx context.Context, telegramUserID int64, twitchUserID string) (core.JoinTargets, error)
	BuildJoinTargetsForCreator(ctx context.Context, creatorID string, telegramUserID int64, twitchUserID string) (core.JoinTargets, error)
}

// ViewerAccessResult is the application-layer result for the linked viewer flow.
type ViewerAccessResult struct {
	HasIdentity bool
	Identity    core.UserIdentity
	Targets     core.JoinTargets
	AccessMode  ViewerAccessMode
}

// ViewerAccessUseCase coordinates linked-viewer access loading.
type ViewerAccessUseCase struct {
	svc    viewerAccessService
	god    *core.GodAccessChecker
	events events.EventSink
}

// NewViewerAccessUseCase builds a viewer access use case.
func NewViewerAccessUseCase(svc viewerAccessService, god *core.GodAccessChecker, sink events.EventSink) *ViewerAccessUseCase {
	return &ViewerAccessUseCase{svc: svc, god: god, events: events.EnsureSink(sink)}
}

// LoadIdentity resolves viewer identity without loading join targets.
func (u *ViewerAccessUseCase) LoadIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
	identity, found, err := u.svc.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		return core.UserIdentity{}, false, fmt.Errorf("load viewer identity: %w", err)
	}
	return identity, found, nil
}

// LoadAccess resolves linked viewer identity and join targets.
func (u *ViewerAccessUseCase) LoadAccess(ctx context.Context, telegramUserID int64) (ViewerAccessResult, error) {
	identity, found, err := u.svc.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		u.recordResult(ctx, "failed")
		return ViewerAccessResult{}, fmt.Errorf("load viewer identity: %w", err)
	}
	if !found {
		if u.god != nil && u.god.IsGodTelegramUser(telegramUserID) {
			targets, err := u.svc.BuildJoinTargets(ctx, telegramUserID, "")
			if err != nil {
				u.recordResult(ctx, "failed")
				return ViewerAccessResult{}, fmt.Errorf("build god join targets: %w", err)
			}
			u.recordResult(ctx, "god")
			return ViewerAccessResult{
				Targets:     targets,
				AccessMode:  ViewerAccessModeGod,
				HasIdentity: false,
			}, nil
		}
		u.recordResult(ctx, "unlinked")
		return ViewerAccessResult{AccessMode: ViewerAccessModeUnlinked}, nil
	}

	targets, err := u.svc.BuildJoinTargets(ctx, telegramUserID, identity.TwitchUserID)
	if err != nil {
		u.recordResult(ctx, "failed")
		return ViewerAccessResult{}, fmt.Errorf("build join targets: %w", err)
	}
	u.recordResult(ctx, "linked")
	return ViewerAccessResult{
		HasIdentity: true,
		Identity:    identity,
		Targets:     targets,
		AccessMode:  ViewerAccessModeLinked,
	}, nil
}

// LoadAccessForCreator resolves linked viewer identity and join targets for one creator only.
func (u *ViewerAccessUseCase) LoadAccessForCreator(ctx context.Context, creatorID string, telegramUserID int64) (ViewerAccessResult, error) {
	identity, found, err := u.svc.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		return ViewerAccessResult{}, fmt.Errorf("load viewer identity: %w", err)
	}
	if !found {
		if u.god != nil && u.god.IsGodTelegramUser(telegramUserID) {
			targets, err := u.svc.BuildJoinTargetsForCreator(ctx, creatorID, telegramUserID, "")
			if err != nil {
				return ViewerAccessResult{}, fmt.Errorf("build creator god join targets: %w", err)
			}
			return ViewerAccessResult{Targets: targets, AccessMode: ViewerAccessModeGod}, nil
		}
		return ViewerAccessResult{AccessMode: ViewerAccessModeUnlinked}, nil
	}

	targets, err := u.svc.BuildJoinTargetsForCreator(ctx, creatorID, telegramUserID, identity.TwitchUserID)
	if err != nil {
		return ViewerAccessResult{}, fmt.Errorf("build creator join targets: %w", err)
	}
	return ViewerAccessResult{
		HasIdentity: true,
		Identity:    identity,
		Targets:     targets,
		AccessMode:  ViewerAccessModeLinked,
	}, nil
}

func (u *ViewerAccessUseCase) recordResult(ctx context.Context, result string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameViewerAccess,
		Outcome: result,
	})
}
