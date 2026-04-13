package bot

import (
	"context"
	"log/slog"
	"testing"

	"imsub/internal/core"
	"imsub/internal/usecase"
)

type subscriptionStartViewerAccessService struct{}

func (subscriptionStartViewerAccessService) LoadIdentity(context.Context, int64) (core.UserIdentity, bool, error) {
	return core.UserIdentity{}, false, nil
}

func (subscriptionStartViewerAccessService) BuildJoinTargets(context.Context, int64, string) (core.JoinTargets, error) {
	return core.JoinTargets{}, nil
}

func (subscriptionStartViewerAccessService) BuildJoinTargetsForCreator(context.Context, string, int64, string) (core.JoinTargets, error) {
	return core.JoinTargets{}, nil
}

func TestHandleSubscriptionStartReturnsNilForUnlinkedTwitchUser(t *testing.T) {
	t.Parallel()

	b := &Bot{
		store:        &routeTestStore{},
		viewerAccess: usecase.NewViewerAccessUseCase(subscriptionStartViewerAccessService{}, nil, nil),
		logger:       slog.Default(),
	}

	if err := b.HandleSubscriptionStart(t.Context(), "creator-1", "streamer1", "tw-1", "viewer1"); err != nil {
		t.Fatalf("HandleSubscriptionStart() error = %v, want nil", err)
	}
}
