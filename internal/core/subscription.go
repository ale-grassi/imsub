package core

import (
	"context"

	"imsub/internal/platform/i18n"
)

type subscriptionStore interface {
	RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error
	Creator(ctx context.Context, creatorID string) (Creator, bool, error)
	RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (telegramUserID int64, found bool, err error)
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
}

// Subscription handles subscriber-end processing and derived notifications.
type Subscription struct {
	store subscriptionStore
}

// NewSubscription creates a Subscription service.
func NewSubscription(store subscriptionStore) *Subscription {
	return &Subscription{store: store}
}

// EndResult captures the direct result of processing a sub-end event.
type EndResult struct {
	TelegramUserID   int64
	Found            bool
	GroupChatID      int64
	BroadcasterLogin string
	IdentityLanguage string
	HasIdentityLang  bool
}

// PreparedEnd is transport-ready data for subscription-end side effects.
type PreparedEnd struct {
	Found            bool
	TelegramUserID   int64
	GroupChatID      int64
	Language         string
	BroadcasterLogin string
	ViewerLogin      string
}

// ProcessEnd applies subscriber-end effects and returns raw domain outcomes.
func (s *Subscription) ProcessEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID string) (EndResult, error) {
	if err := s.store.RemoveCreatorSubscriber(ctx, broadcasterID, twitchUserID); err != nil {
		return EndResult{}, err
	}

	creator, creatorFound, err := s.store.Creator(ctx, broadcasterID)
	if err != nil {
		return EndResult{}, err
	}
	if broadcasterLogin == "" && creatorFound {
		broadcasterLogin = creator.Name
	}

	telegramUserID, found, err := s.store.RemoveUserCreatorByTwitch(ctx, twitchUserID, broadcasterID)
	if err != nil {
		return EndResult{}, err
	}
	if !found {
		return EndResult{Found: false}, nil
	}

	identity, hasIdentity, err := s.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return EndResult{}, err
	}
	out := EndResult{
		TelegramUserID:   telegramUserID,
		Found:            true,
		GroupChatID:      creator.GroupChatID,
		BroadcasterLogin: broadcasterLogin,
	}
	if hasIdentity {
		out.IdentityLanguage = identity.Language
		out.HasIdentityLang = identity.Language != ""
	}
	return out, nil
}

// PrepareEnd converts subscriber-end outcomes into transport-ready data.
func (s *Subscription) PrepareEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) (PreparedEnd, error) {
	res, err := s.ProcessEnd(ctx, broadcasterID, broadcasterLogin, twitchUserID)
	if err != nil {
		return PreparedEnd{}, err
	}
	if !res.Found {
		return PreparedEnd{Found: false}, nil
	}

	lang := "en"
	if res.HasIdentityLang {
		lang = i18n.NormalizeLanguage(res.IdentityLanguage)
	}

	return PreparedEnd{
		Found:            true,
		TelegramUserID:   res.TelegramUserID,
		GroupChatID:      res.GroupChatID,
		Language:         lang,
		BroadcasterLogin: res.BroadcasterLogin,
		ViewerLogin:      twitchLogin,
	}, nil
}
