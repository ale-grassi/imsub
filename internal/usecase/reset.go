// Package usecase contains application-layer orchestration that coordinates
// domain services, observability, and transport-facing results.
package usecase

import (
	"context"
	"errors"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

// ResetScope identifies the requested reset workflow.
type ResetScope string

const (
	// ResetScopeViewer executes viewer-only reset behavior.
	ResetScopeViewer ResetScope = "viewer"
	// ResetScopeCreator executes creator-only reset behavior.
	ResetScopeCreator ResetScope = "creator"
	// ResetScopeBoth executes both viewer and creator reset behavior.
	ResetScopeBoth ResetScope = "both"
)

var errUnsupportedResetScope = errors.New("unsupported reset scope")

type resetService interface {
	LoadScopes(ctx context.Context, telegramUserID int64) (core.ScopeState, error)
	CountViewerGroups(ctx context.Context, telegramUserID int64) (int, error)
	CountCreatorGroups(ctx context.Context, telegramUserID int64) (int, error)
	ExecuteViewerReset(ctx context.Context, telegramUserID int64) (core.ViewerResetResult, error)
	ExecuteCreatorReset(ctx context.Context, telegramUserID int64, action core.CreatorResetGroupAction) (core.CreatorResetResult, error)
	ExecuteBothReset(ctx context.Context, telegramUserID int64, action core.CreatorResetGroupAction) (core.BothResetResult, error)
}

// ResetResult is a transport-friendly summary of a reset execution.
type ResetResult struct {
	Scope           ResetScope
	Empty           bool
	ViewerLogin     string
	GroupCount      int
	GroupResolution core.GroupResolutionStats
	DeletedCount    int
	DeletedNames    []string
	CreatorCleanup  core.CreatorGroupCleanupSummary
}

// ResetUseCase coordinates reset execution and observability.
type ResetUseCase struct {
	svc    resetService
	events events.EventSink
}

// NewResetUseCase builds a reset use case.
func NewResetUseCase(svc resetService, sink events.EventSink) *ResetUseCase {
	return &ResetUseCase{svc: svc, events: events.EnsureSink(sink)}
}

// LoadScopes resolves current reset scopes.
func (u *ResetUseCase) LoadScopes(ctx context.Context, telegramUserID int64) (core.ScopeState, error) {
	scopes, err := u.svc.LoadScopes(ctx, telegramUserID)
	if err != nil {
		return core.ScopeState{}, fmt.Errorf("load reset scopes: %w", err)
	}
	return scopes, nil
}

// CountViewerGroups returns the reset target count for viewer-linked groups.
func (u *ResetUseCase) CountViewerGroups(ctx context.Context, telegramUserID int64) (int, error) {
	count, err := u.svc.CountViewerGroups(ctx, telegramUserID)
	if err != nil {
		return 0, fmt.Errorf("count viewer groups: %w", err)
	}
	return count, nil
}

// CountCreatorGroups returns the managed-group count for creator-owned groups.
func (u *ResetUseCase) CountCreatorGroups(ctx context.Context, telegramUserID int64) (int, error) {
	count, err := u.svc.CountCreatorGroups(ctx, telegramUserID)
	if err != nil {
		return 0, fmt.Errorf("count creator groups: %w", err)
	}
	return count, nil
}

// Execute runs the requested reset scope and records reset metrics.
func (u *ResetUseCase) Execute(ctx context.Context, telegramUserID int64, scope ResetScope, action core.CreatorResetGroupAction) (ResetResult, error) {
	switch scope {
	case ResetScopeViewer:
		return u.executeViewer(ctx, telegramUserID)
	case ResetScopeCreator:
		return u.executeCreator(ctx, telegramUserID, action)
	case ResetScopeBoth:
		return u.executeBoth(ctx, telegramUserID, action)
	default:
		return ResetResult{}, fmt.Errorf("%w: %s", errUnsupportedResetScope, scope)
	}
}

func (u *ResetUseCase) executeViewer(ctx context.Context, telegramUserID int64) (ResetResult, error) {
	res, err := u.svc.ExecuteViewerReset(ctx, telegramUserID)
	if err != nil {
		u.recordExecution(ctx, ResetScopeViewer, "failed")
		return ResetResult{}, fmt.Errorf("execute viewer reset: %w", err)
	}
	if !res.HasIdentity {
		u.recordExecution(ctx, ResetScopeViewer, "empty")
		return ResetResult{Scope: ResetScopeViewer, Empty: true}, nil
	}
	u.recordResolution(ctx, res.GroupResolution)
	u.recordExecution(ctx, ResetScopeViewer, "ok")
	return ResetResult{
		Scope:           ResetScopeViewer,
		ViewerLogin:     res.Identity.TwitchLogin,
		GroupCount:      res.GroupCount,
		GroupResolution: res.GroupResolution,
	}, nil
}

func (u *ResetUseCase) executeCreator(ctx context.Context, telegramUserID int64, action core.CreatorResetGroupAction) (ResetResult, error) {
	res, err := u.svc.ExecuteCreatorReset(ctx, telegramUserID, action)
	if err != nil {
		u.recordExecution(ctx, ResetScopeCreator, "failed")
		return ResetResult{}, fmt.Errorf("execute creator reset: %w", err)
	}
	if res.DeletedCount == 0 {
		u.recordExecution(ctx, ResetScopeCreator, "empty")
		return ResetResult{Scope: ResetScopeCreator, Empty: true}, nil
	}
	u.recordCreatorCleanup(ctx, res.CreatorCleanup)
	u.recordExecution(ctx, ResetScopeCreator, "ok")
	return ResetResult{
		Scope:          ResetScopeCreator,
		DeletedCount:   res.DeletedCount,
		DeletedNames:   append([]string(nil), res.DeletedNames...),
		CreatorCleanup: res.CreatorCleanup,
	}, nil
}

func (u *ResetUseCase) executeBoth(ctx context.Context, telegramUserID int64, action core.CreatorResetGroupAction) (ResetResult, error) {
	res, err := u.svc.ExecuteBothReset(ctx, telegramUserID, action)
	if err != nil {
		u.recordExecution(ctx, ResetScopeBoth, "failed")
		return ResetResult{}, fmt.Errorf("execute both reset: %w", err)
	}
	if !res.HasIdentity && res.DeletedCount == 0 {
		u.recordExecution(ctx, ResetScopeBoth, "empty")
		return ResetResult{Scope: ResetScopeBoth, Empty: true}, nil
	}
	u.recordResolution(ctx, res.GroupResolution)
	u.recordCreatorCleanup(ctx, res.CreatorCleanup)
	u.recordExecution(ctx, ResetScopeBoth, "ok")
	out := ResetResult{
		Scope:           ResetScopeBoth,
		GroupCount:      res.GroupCount,
		GroupResolution: res.GroupResolution,
		DeletedCount:    res.DeletedCount,
		DeletedNames:    append([]string(nil), res.DeletedNames...),
		CreatorCleanup:  res.CreatorCleanup,
	}
	if res.HasIdentity {
		out.ViewerLogin = res.Identity.TwitchLogin
	}
	return out, nil
}

func (u *ResetUseCase) recordExecution(ctx context.Context, scope ResetScope, result string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameResetExecuted,
		Outcome: result,
		Fields: map[string]string{
			"scope": string(scope),
		},
	})
}

func (u *ResetUseCase) recordResolution(ctx context.Context, stats core.GroupResolutionStats) {
	u.emitTargetCount(ctx, "tracked", stats.TrackedCount)
	u.emitTargetCount(ctx, "canonical", stats.CanonicalCount)
	u.emitTargetCount(ctx, "canonical_added", stats.CanonicalAddedCount)
	u.emitTargetCount(ctx, "final", stats.TotalCount)
}

func (u *ResetUseCase) emitTargetCount(ctx context.Context, source string, count int) {
	if count <= 0 {
		return
	}
	u.events.Emit(ctx, events.Event{
		Name: events.NameResetGroupTarget,
		Fields: map[string]string{
			"source": source,
		},
		Count: count,
	})
}

func (u *ResetUseCase) recordCreatorCleanup(ctx context.Context, cleanup core.CreatorGroupCleanupSummary) {
	u.emitTargetCount(ctx, "creator_groups", cleanup.ManagedGroupCount)
	u.emitTargetCount(ctx, "creator_tracked_memberships", cleanup.TargetedMembershipCount)
	u.emitTargetCount(ctx, "creator_kick_failures", cleanup.KickFailureCount)
}
