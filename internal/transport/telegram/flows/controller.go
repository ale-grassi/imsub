package flows

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"imsub/internal/application"
	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/groupops"

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
	msgGroupNotGroup           = "group_not_group"
	msgGroupNotAdmin           = "group_not_admin"
	msgGroupNotCreator         = "group_not_creator"
	msgGroupRegistered         = "group_registered"
	msgGroupRegisteredDM       = "group_registered_dm"
	msgGroupAlreadyLinked      = "group_already_linked"
	msgGroupDifferentLinked    = "group_different_linked"
	msgGroupTakenByOther       = "group_taken_by_other"
	msgGroupWarnPublic         = "group_warn_public"          //nolint:gosec // i18n key, not a credential
	msgGroupWarnJoinByReq      = "group_warn_join_by_request" //nolint:gosec // i18n key, not a credential
	msgGroupWarnUntrackedUsers = "group_warn_untracked_users" //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNotAdmin    = "group_warn_bot_not_admin"   //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNoInvite    = "group_warn_bot_no_invite"   //nolint:gosec // i18n key, not a credential
	msgGroupWarnBotNoRestrict  = "group_warn_bot_no_restrict" //nolint:gosec // i18n key, not a credential
	msgGroupWarnSettingsIntro  = "group_warn_settings_intro"  //nolint:gosec // i18n key, not a credential
	msgGroupCheckingSettings   = "group_checking_settings"
	msgGroupSettingsOK         = "group_settings_ok"
	msgGroupBotStatusChanged   = "group_bot_status_changed"
	msgGroupUnregistered       = "group_unregistered"
	msgGroupUnregisterNotOwner = "group_unregister_not_owner"

	// Buttons.
	btnBack     = "btn_back"
	btnCopyLink = "btn_copy_link"
)

// Dependencies configure Telegram flows controller construction.
type Dependencies struct {
	Config          config.Config
	Store           controllerStore
	TelegramLimiter *ratelimit.RateLimiter
	Logger          *slog.Logger
	TelegramBot     *telego.Bot
	TelegramHandler *tghandler.BotHandler
	App             *application.Runtime
}

type controllerStore interface {
	UserIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error)
	SaveOAuthState(ctx context.Context, state string, payload core.OAuthStatePayload, ttl time.Duration) error
	DeleteOAuthState(ctx context.Context, state string) (core.OAuthStatePayload, error)
	Creator(ctx context.Context, creatorID string) (core.Creator, bool, error)
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error)
	DeleteManagedGroup(ctx context.Context, chatID int64) error
	CountUntrackedGroupMembers(ctx context.Context, chatID int64) (int, error)
	IsTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) (bool, error)
	AddTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source string, at time.Time) error
	RemoveTrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
	UpsertUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64, source, status string, at time.Time) error
	RemoveUntrackedGroupMember(ctx context.Context, chatID, telegramUserID int64) error
}

// Controller owns Telegram business flows and callback orchestration.
type Controller struct {
	cfg       config.Config
	store     controllerStore
	tgLimiter *ratelimit.RateLimiter
	logger    *slog.Logger

	tg               *telego.Bot
	tgHandler        *tghandler.BotHandler
	telegramClient   *client.Client
	telegramGroupOps *groupops.Client

	app *application.TelegramRuntime

	backgroundWG sync.WaitGroup
}

// New creates a Telegram flows Controller from dependencies.
func New(deps Dependencies) *Controller {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	c := &Controller{
		cfg:       deps.Config,
		store:     deps.Store,
		tgLimiter: deps.TelegramLimiter,
		logger:    logger,
		tg:        deps.TelegramBot,
		tgHandler: deps.TelegramHandler,
	}
	if deps.App != nil {
		c.app = deps.App.BindTelegram(c.ViewerGroupOps(), c.KickFromGroup)
	}
	return c
}

func (c *Controller) log() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.Default()
	}
	return c.logger
}

func (c *Controller) runBackground(ctx context.Context, fn func(context.Context)) {
	if c == nil || fn == nil {
		return
	}
	c.backgroundWG.Go(func() {
		fn(ctx)
	})
}

// WaitBackground blocks until detached Telegram follow-up work completes or ctx ends.
func (c *Controller) WaitBackground(ctx context.Context) error {
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
	controller *Controller
}

func (g viewerGroupOps) IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	return g.controller.isGroupMember(ctx, groupChatID, telegramUserID)
}

func (g viewerGroupOps) CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	return g.controller.createInviteLink(ctx, groupChatID, telegramUserID, name)
}

// ViewerGroupOps returns group operations used by viewer business logic.
func (c *Controller) ViewerGroupOps() core.GroupOps {
	return viewerGroupOps{controller: c}
}
