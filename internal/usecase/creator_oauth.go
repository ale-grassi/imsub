package usecase

import (
	"context"
	"errors"
	"fmt"

	"imsub/internal/core"
	"imsub/internal/events"
)

const (
	creatorOAuthResultSuccess             = "success"
	creatorOAuthResultStoreFailed         = "store_failed"
	creatorOAuthResultTokenExchangeFailed = "token_exchange_failed"
	creatorOAuthResultUserInfoFailed      = "userinfo_failed"
	creatorOAuthResultScopeMissing        = "scope_missing"
	creatorOAuthResultMismatch            = "creator_mismatch"
)

type creatorOAuthService interface {
	LinkCreator(ctx context.Context, code string, payload core.OAuthStatePayload) (core.CreatorResult, error)
}

// CreatorOAuthResult is the application-layer result for creator OAuth completion.
type CreatorOAuthResult struct {
	ResultLabel            string
	Creator                core.Creator
	BroadcasterDisplayName string
}

// CreatorOAuthUseCase coordinates creator OAuth completion and outcome events.
type CreatorOAuthUseCase struct {
	svc    creatorOAuthService
	events events.EventSink
}

// NewCreatorOAuthUseCase builds a creator OAuth use case.
func NewCreatorOAuthUseCase(svc creatorOAuthService, sink events.EventSink) *CreatorOAuthUseCase {
	return &CreatorOAuthUseCase{svc: svc, events: events.EnsureSink(sink)}
}

// Complete finishes creator OAuth linking and emits a stable outcome event.
func (u *CreatorOAuthUseCase) Complete(ctx context.Context, code string, payload core.OAuthStatePayload) (CreatorOAuthResult, error) {
	res, err := u.svc.LinkCreator(ctx, code, payload)
	if err != nil {
		label := creatorOAuthResultStoreFailed
		var fe *core.FlowError
		if errors.As(err, &fe) {
			switch fe.Kind {
			case core.KindTokenExchange:
				label = creatorOAuthResultTokenExchangeFailed
			case core.KindScopeMissing:
				label = creatorOAuthResultScopeMissing
			case core.KindUserInfo:
				label = creatorOAuthResultUserInfoFailed
			case core.KindCreatorMismatch:
				label = creatorOAuthResultMismatch
			case core.KindStore, core.KindSave:
				label = creatorOAuthResultStoreFailed
			}
		}
		u.recordResult(ctx, "", label)
		return CreatorOAuthResult{ResultLabel: label}, fmt.Errorf("complete creator oauth: %w", err)
	}

	u.recordResult(ctx, res.Creator.ID, creatorOAuthResultSuccess)
	return CreatorOAuthResult{
		ResultLabel:            creatorOAuthResultSuccess,
		Creator:                res.Creator,
		BroadcasterDisplayName: res.BroadcasterDisplayName,
	}, nil
}

func (u *CreatorOAuthUseCase) recordResult(ctx context.Context, creatorID, result string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameCreatorOAuth,
		Outcome: result,
		Fields:  map[string]string{"creator_id": creatorID},
	})
}
