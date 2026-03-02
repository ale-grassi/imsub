package flows

import (
	"context"
	"log/slog"

	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/groupops"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
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

// RegisterTelegramHandlers binds Telegram commands, callbacks, and join-request handlers.
func (c *Controller) RegisterTelegramHandlers() {
	if c.tgHandler == nil {
		return
	}

	privateOnly := func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type == telego.ChatTypePrivate && update.Message.From != nil
	}
	registerCallback := func(action string, fn func(ctx context.Context, userID int64, editMsgID int, lang string) string) {
		c.tgHandler.HandleCallbackQuery(func(ctx *tghandler.Context, query telego.CallbackQuery) error {
			c.callbackHandler(ctx, query, fn)
			return nil
		}, tghandler.CallbackDataEqual(action))
	}

	c.tgHandler.HandleMessage(c.onRegisterGroup, tghandler.CommandEqual("registergroup"))
	c.tgHandler.HandleMessage(c.onStartCommand, tghandler.And(tghandler.CommandEqual("start"), privateOnly))
	c.tgHandler.HandleMessage(c.onCreatorCommand, tghandler.And(tghandler.CommandEqual("creator"), privateOnly))
	c.tgHandler.HandleMessage(c.onResetCommand, tghandler.And(tghandler.CommandEqual("reset"), privateOnly))

	registerCallback(ui.ActionRefresh, c.handleViewerStart)
	registerCallback(ui.ActionRegisterCreator, c.handleCreatorRegistrationStart)
	registerCallback(ui.ActionResetConfirm, c.handleResetPrompt)
	registerCallback(ui.ActionResetPickViewer, c.handleResetViewerConfirmPrompt)
	registerCallback(ui.ActionResetPickCreator, c.handleResetCreatorConfirmPrompt)
	registerCallback(ui.ActionResetPickBoth, c.handleResetBothConfirmPrompt)
	registerCallback(ui.ActionResetBack, c.handleResetPrompt)
	registerCallback(ui.ActionResetDoViewer, c.handleResetViewerCommand)
	registerCallback(ui.ActionResetDoCreator, c.handleResetCreatorCommand)
	registerCallback(ui.ActionResetDoBoth, c.handleResetBothCommand)

	c.tgHandler.HandleChatJoinRequest(c.onChatJoinRequest)
	c.tgHandler.HandleMessage(c.onUnknownMessage, tghandler.And(tghandler.AnyMessage(), privateOnly))
}

func (c *Controller) callbackHandler(ctx context.Context, q telego.CallbackQuery, fn func(ctx context.Context, userID int64, editMsgID int, lang string) string) {
	lang := i18n.NormalizeLanguage(q.From.LanguageCode)
	var msgID int
	if q.Message != nil {
		msgID = q.Message.GetMessageID()
	}
	alertErr := fn(ctx, q.From.ID, msgID, lang)
	if alertErr != "" {
		c.answerCallbackAlert(ctx, q.ID, alertErr)
		return
	}

	callbackText := ""
	if q.Data == ui.ActionRefresh {
		callbackText = i18n.Translate(lang, "cb_refreshed")
	}
	c.answerCallback(ctx, q.ID, callbackText)
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
