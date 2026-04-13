package core

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/platform/i18n"
)

type subscriptionStore interface {
	RemoveCreatorSubscriber(ctx context.Context, creatorID, twitchUserID string) error
	Creator(ctx context.Context, creatorID string) (Creator, bool, error)
	ListManagedGroupsByCreator(ctx context.Context, creatorID string) ([]ManagedGroup, error)
	RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (telegramUserID int64, found bool, err error)
	ResolveTelegramUserIDByTwitch(ctx context.Context, twitchUserID string) (telegramUserID int64, found bool, err error)
	UserIdentity(ctx context.Context, telegramUserID int64) (UserIdentity, bool, error)
	UpsertSubscriptionEndGrace(ctx context.Context, job PendingSubscriptionEndGrace) (PendingSubscriptionEndGrace, error)
	DeleteSubscriptionEndGrace(ctx context.Context, creatorID, twitchUserID string) error
}

// SubscriptionService handles subscriber-end processing and derived notifications.
type SubscriptionService struct {
	store subscriptionStore
	now   func() time.Time
	god   *GodAccessChecker
}

// NewSubscriptionService creates a subscription service.
func NewSubscriptionService(store subscriptionStore, god *GodAccessChecker) *SubscriptionService {
	return &SubscriptionService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		god:   god,
	}
}

// EndResult captures the direct result of processing a sub-end event.
type EndResult struct {
	Mode             SubscriptionEndMode
	TelegramUserID   int64
	Found            bool
	GroupChatIDs     []int64
	BroadcasterLogin string
	IdentityLanguage string
	HasIdentityLang  bool
	GraceUntil       time.Time
}

// PreparedEnd is transport-ready data for subscription-end side effects.
type PreparedEnd struct {
	Found            bool
	Mode             SubscriptionEndMode
	TelegramUserID   int64
	GroupChatIDs     []int64
	Language         string
	BroadcasterLogin string
	ViewerLogin      string
	GraceUntil       time.Time
}

// ProcessEnd applies subscriber-end effects and returns raw domain outcomes.
func (s *SubscriptionService) ProcessEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID string) (EndResult, error) {
	if err := s.store.RemoveCreatorSubscriber(ctx, broadcasterID, twitchUserID); err != nil {
		return EndResult{}, fmt.Errorf("remove creator subscriber: %w", err)
	}

	creator, creatorFound, err := s.store.Creator(ctx, broadcasterID)
	if err != nil {
		return EndResult{}, fmt.Errorf("load creator: %w", err)
	}
	if broadcasterLogin == "" && creatorFound {
		broadcasterLogin = creator.TwitchLogin
	}
	groups, err := s.store.ListManagedGroupsByCreator(ctx, broadcasterID)
	if err != nil {
		return EndResult{}, fmt.Errorf("list managed groups by creator: %w", err)
	}

	out := EndResult{
		Mode:             SubscriptionEndModeImmediate,
		Found:            true,
		GroupChatIDs:     make([]int64, 0, len(groups)),
		BroadcasterLogin: broadcasterLogin,
	}

	var (
		telegramUserID int64
		found          bool
	)
	if creator.SubscriptionEndGrace.Enabled() {
		telegramUserID, found, err = s.store.ResolveTelegramUserIDByTwitch(ctx, twitchUserID)
		if err != nil {
			return EndResult{}, fmt.Errorf("resolve telegram user by twitch: %w", err)
		}
		if !found {
			return EndResult{Found: false}, nil
		}
		out.Mode = SubscriptionEndModeGrace
		out.GraceUntil = s.now().Add(creator.SubscriptionEndGrace.Duration())
	} else {
		telegramUserID, found, err = s.store.RemoveUserCreatorByTwitch(ctx, twitchUserID, broadcasterID)
		if err != nil {
			return EndResult{}, fmt.Errorf("remove user creator by twitch: %w", err)
		}
		if !found {
			return EndResult{Found: false}, nil
		}
	}
	// God-listed users keep global access even after subscriber-derived access is
	// revoked. The store mutations above are still intentional: subscription
	// state should be cleaned up, but downstream kicks and notifications must be
	// suppressed for these operator-bypassed accounts.
	if s.god != nil && s.god.IsGodTelegramUser(telegramUserID) {
		return EndResult{Found: false}, nil
	}

	identity, hasIdentity, err := s.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return EndResult{}, fmt.Errorf("load user identity: %w", err)
	}
	out.TelegramUserID = telegramUserID
	for _, group := range groups {
		out.GroupChatIDs = append(out.GroupChatIDs, group.ChatID)
	}
	if hasIdentity {
		out.IdentityLanguage = identity.Language
		out.HasIdentityLang = identity.Language != ""
	}
	return out, nil
}

// PrepareEnd converts subscriber-end outcomes into transport-ready data.
func (s *SubscriptionService) PrepareEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) (PreparedEnd, error) {
	res, err := s.ProcessEnd(ctx, broadcasterID, broadcasterLogin, twitchUserID)
	if err != nil {
		return PreparedEnd{}, fmt.Errorf("process end: %w", err)
	}
	if !res.Found {
		return PreparedEnd{Found: false}, nil
	}

	lang := "en"
	if res.HasIdentityLang {
		lang = i18n.NormalizeLanguage(res.IdentityLanguage)
	}

	if res.Mode == SubscriptionEndModeGrace {
		if _, err := s.store.UpsertSubscriptionEndGrace(ctx, PendingSubscriptionEndGrace{
			CreatorID:      broadcasterID,
			CreatorLogin:   res.BroadcasterLogin,
			TwitchUserID:   twitchUserID,
			TelegramUserID: res.TelegramUserID,
			ViewerLogin:    twitchLogin,
			Language:       lang,
			DueAt:          res.GraceUntil,
		}); err != nil {
			return PreparedEnd{}, fmt.Errorf("upsert subscription-end grace: %w", err)
		}
	}

	return PreparedEnd{
		Found:            true,
		Mode:             res.Mode,
		TelegramUserID:   res.TelegramUserID,
		GroupChatIDs:     res.GroupChatIDs,
		Language:         lang,
		BroadcasterLogin: res.BroadcasterLogin,
		ViewerLogin:      twitchLogin,
		GraceUntil:       res.GraceUntil,
	}, nil
}

// CancelGrace removes any pending delayed subscription-end removal for the
// creator/viewer pair.
func (s *SubscriptionService) CancelGrace(ctx context.Context, broadcasterID, twitchUserID string) error {
	if err := s.store.DeleteSubscriptionEndGrace(ctx, broadcasterID, twitchUserID); err != nil {
		return fmt.Errorf("delete subscription-end grace: %w", err)
	}
	return nil
}
