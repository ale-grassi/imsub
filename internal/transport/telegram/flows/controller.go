package flows

import (
	"context"
	"log/slog"

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
	btnCopyLink = "btn_copy_link"
)

// Dependencies configure Telegram flows controller construction.
type Dependencies struct {
	Config          config.Config
	Store           core.Store
	TelegramLimiter *ratelimit.RateLimiter
	Logger          *slog.Logger
	TelegramBot     *telego.Bot
	TelegramHandler *tghandler.BotHandler
	Services        Services
	Factories       ServiceFactories
}

// Services are runtime services used by Telegram flows.
type Services struct {
	EventSub     *core.EventSub
	Subscription *core.Subscription
	OAuth        *core.OAuth
	Viewer       *core.Viewer
	Creator      *core.CreatorService
	Reset        *core.Resetter
}

// ServiceFactories builds optional services when concrete instances are not provided.
type ServiceFactories struct {
	Viewer func(groupOps core.GroupOps) *core.Viewer
	Reset  func(kick func(ctx context.Context, groupChatID, telegramUserID int64) error) *core.Resetter
}

// Controller owns Telegram business flows and callback orchestration.
type Controller struct {
	cfg       config.Config
	store     core.Store
	tgLimiter *ratelimit.RateLimiter
	logger    *slog.Logger

	tg               *telego.Bot
	tgHandler        *tghandler.BotHandler
	telegramClient   *client.Client
	telegramGroupOps *groupops.Client

	eventSubSvc     *core.EventSub
	subscriptionSvc *core.Subscription
	oauthSvc        *core.OAuth
	viewerSvc       *core.Viewer
	creatorSvc      *core.CreatorService
	resetSvc        *core.Resetter
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

		eventSubSvc:     deps.Services.EventSub,
		subscriptionSvc: deps.Services.Subscription,
		oauthSvc:        deps.Services.OAuth,
		viewerSvc:       deps.Services.Viewer,
		creatorSvc:      deps.Services.Creator,
		resetSvc:        deps.Services.Reset,
	}
	if c.viewerSvc == nil && deps.Factories.Viewer != nil {
		c.viewerSvc = deps.Factories.Viewer(c.ViewerGroupOps())
	}
	if c.resetSvc == nil && deps.Factories.Reset != nil {
		c.resetSvc = deps.Factories.Reset(c.KickFromGroup)
	}
	return c
}

func (c *Controller) log() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.Default()
	}
	return c.logger
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
