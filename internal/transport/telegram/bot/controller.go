package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/telegram/client"
	telegramgroups "imsub/internal/transport/telegram/groups"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
)

const (
	// General.
	msgErrLoadStatus   = "err_load_status"
	msgUserGenericName = "user_generic_name"

	// Help commands.
	msgCmdHelp        = "cmd_help"
	msgCmdHelpBoth    = "cmd_help_both"
	msgCmdHelpCreator = "cmd_help_creator"
	msgCmdHelpViewer  = "cmd_help_viewer"

	// Group registration.
	msgGroupNotGroup                  = "group_not_group"
	msgGroupNotAdmin                  = "group_not_admin"
	msgGroupNotCreator                = "group_not_creator"
	msgGroupRegistered                = "group_registered"
	msgGroupRegisteredDM              = "group_registered_dm"
	msgGroupAlreadyLinked             = "group_already_linked"
	msgGroupDifferentLinked           = "group_different_linked"
	msgGroupTakenByOther              = "group_taken_by_other"
	msgGroupWarnPublic                = "group_warn_public"          //nolint:gosec // i18n key, not a credential
	msgGroupWarnJoinByReq             = "group_warn_join_by_request" //nolint:gosec // i18n key, not a credential
	msgGroupWarnUntrackedUsers        = "group_warn_untracked_users" //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNotAdmin           = "group_warn_bot_not_admin"   //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNoInvite           = "group_warn_bot_no_invite"   //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNoRestrict         = "group_warn_bot_no_restrict" //nolint:gosec // i18n key, not a credential
	msgGroupWarnSettingsIntro         = "group_warn_settings_intro"  //nolint:gosec // i18n key, not a credential
	msgGroupPolicyPrompt              = "group_policy_prompt_html"
	msgGroupPolicyExistingMembers     = "group_policy_existing_members_html"
	msgGroupPolicyObserveLine         = "group_policy_observe_line"
	msgGroupPolicyObserveWarnLine     = "group_policy_observe_warn_line"
	msgGroupPolicyKickLine            = "group_policy_kick_line"
	msgGroupPolicyGraceLine           = "group_policy_grace_line"
	msgGroupUntrackedJoinWarning      = "group_untracked_join_warning_html"
	msgGroupCheckingSettings          = "group_checking_settings"
	msgGroupSettingsOK                = "group_settings_ok"
	msgGroupBotStatusChanged          = "group_bot_status_changed"
	msgGroupBotRemovedOwnerDM         = "group_bot_removed_owner_dm"
	msgGroupBotRemovedLagDM           = "group_bot_removed_cleanup_lag_dm"
	msgGroupUnregistered              = "group_unregistered"
	msgGroupUnregisterPrompt          = "group_unregister_prompt_html"
	msgGroupUnregisteredKicked        = "group_unregistered_kicked_html"
	msgGroupUnregisteredKickAllFailed = "group_unregistered_kick_all_failed_html"
	msgGroupUnregisterNotOwner        = "group_unregister_not_owner"

	// Buttons.
	btnBack                   = "btn_back"
	btnCopyLink               = "btn_copy_link"
	btnGroupPolicyObserve     = "btn_group_policy_observe"
	btnGroupPolicyObserveWarn = "btn_group_policy_observe_warn"
	btnGroupPolicyKick        = "btn_group_policy_kick"
	btnGroupPolicyGrace       = "btn_group_policy_grace"
)

// Dependencies configure Telegram bot construction.
type Dependencies struct {
	Config              config.Config
	Store               controllerStore
	TelegramLimiter     *ratelimit.RateLimiter
	Logger              *slog.Logger
	TelegramBot         *telego.Bot
	TelegramHandler     *tghandler.BotHandler
	TelegramClient      *client.Client
	TelegramGroups      *telegramgroups.Client
	CreatorStatus       *usecase.CreatorStatusUseCase
	CreatorBlocklist    *core.CreatorBlocklistService
	ViewerOAuth         *usecase.ViewerOAuthUseCase
	CreatorOAuth        *usecase.CreatorOAuthUseCase
	ViewerAccess        *usecase.ViewerAccessUseCase
	GroupRegistration   *usecase.GroupRegistrationUseCase
	GroupUnregistration *usecase.GroupUnregistrationUseCase
	GroupPolicyUpdate   *usecase.GroupPolicyUpdateUseCase
	CreatorActivation   *usecase.CreatorActivationUseCase
	SubscriptionEnd     *usecase.SubscriptionEndUseCase
	Reset               *usecase.ResetUseCase
}

type controllerStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error)
	SaveOAuthState(ctx context.Context, state string, payload core.OAuthStatePayload, ttl time.Duration) error
	DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error)
	Creator(ctx context.Context, creatorID string) (core.Creator, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ResolveTelegramUserIDByTwitch(ctx context.Context, twitchUserID string) (int64, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error)
	DeleteManagedGroup(ctx context.Context, chatID int64) error
	IsCreatorBlocked(ctx context.Context, creatorID, twitchUserID string) (bool, error)
	CountUntrackedGroupMembers(ctx context.Context, chatID int64) (int, error)
	IsTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) (bool, error)
	AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
	UpsertUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string, at time.Time) error
	RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
	ListUntrackedGroupMembers(ctx context.Context, chatID int64) ([]core.UntrackedGroupMember, error)
}

// Bot owns Telegram bot flows and callback orchestration.
type Bot struct {
	cfg       config.Config
	store     controllerStore
	tgLimiter *ratelimit.RateLimiter
	logger    *slog.Logger

	tg             *telego.Bot
	tgHandler      *tghandler.BotHandler
	telegramClient *client.Client
	telegramGroups *telegramgroups.Client

	creatorStatus       *usecase.CreatorStatusUseCase
	creatorBlocklist    *core.CreatorBlocklistService
	viewerOAuth         *usecase.ViewerOAuthUseCase
	creatorOAuth        *usecase.CreatorOAuthUseCase
	viewerAccess        *usecase.ViewerAccessUseCase
	groupRegistration   *usecase.GroupRegistrationUseCase
	groupUnregistration *usecase.GroupUnregistrationUseCase
	groupPolicyUpdate   *usecase.GroupPolicyUpdateUseCase
	creatorActivation   *usecase.CreatorActivationUseCase
	subscriptionEnd     *usecase.SubscriptionEndUseCase
	reset               *usecase.ResetUseCase

	backgroundWG sync.WaitGroup
}

// New creates a Telegram bot from dependencies.
func New(deps Dependencies) *Bot {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	c := &Bot{
		cfg:                 deps.Config,
		store:               deps.Store,
		tgLimiter:           deps.TelegramLimiter,
		logger:              logger,
		tg:                  deps.TelegramBot,
		tgHandler:           deps.TelegramHandler,
		telegramClient:      deps.TelegramClient,
		telegramGroups:      deps.TelegramGroups,
		creatorStatus:       deps.CreatorStatus,
		creatorBlocklist:    deps.CreatorBlocklist,
		viewerOAuth:         deps.ViewerOAuth,
		creatorOAuth:        deps.CreatorOAuth,
		viewerAccess:        deps.ViewerAccess,
		groupRegistration:   deps.GroupRegistration,
		groupUnregistration: deps.GroupUnregistration,
		groupPolicyUpdate:   deps.GroupPolicyUpdate,
		creatorActivation:   deps.CreatorActivation,
		subscriptionEnd:     deps.SubscriptionEnd,
		reset:               deps.Reset,
	}
	return c
}

func (c *Bot) log() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.Default()
	}
	return c.logger
}

// SetViewerAccessUseCase wires the Telegram viewer access use case after controller construction.
func (c *Bot) SetViewerAccessUseCase(uc *usecase.ViewerAccessUseCase) {
	if c == nil {
		return
	}
	c.viewerAccess = uc
}

// SetResetUseCase wires the Telegram reset use case after controller construction.
func (c *Bot) SetResetUseCase(uc *usecase.ResetUseCase) {
	if c == nil {
		return
	}
	c.reset = uc
}

func (c *Bot) runBackground(ctx context.Context, fn func(context.Context)) {
	if c == nil || fn == nil {
		return
	}
	c.backgroundWG.Go(func() {
		fn(ctx)
	})
}

// WaitBackground blocks until detached Telegram follow-up work completes or ctx ends.
func (c *Bot) WaitBackground(ctx context.Context) error {
	if c == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		c.backgroundWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context done: %w", ctx.Err())
	}
}

type viewerGroupOps struct {
	controller *Bot
}

func (g viewerGroupOps) IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	return g.controller.isGroupMember(ctx, groupChatID, telegramUserID)
}

func (g viewerGroupOps) CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	return g.controller.createInviteLink(ctx, groupChatID, telegramUserID, name)
}

// ViewerGroupOps returns group operations used by viewer business logic.
func (c *Bot) ViewerGroupOps() core.GroupOps {
	return viewerGroupOps{controller: c}
}
