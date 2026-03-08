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

const (
	// General.
	msgErrLoadStatus   = "err_load_status"
	msgErrReset        = "err_reset"
	msgUserGenericName = "user_generic_name"

	// Callbacks.
	msgCbRefreshed = "cb_refreshed"

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
	msgGroupWarnSettingsIntro  = "group_warn_settings_intro"  //nolint:gosec // i18n key, not a credential
	msgGroupCheckingSettings   = "group_checking_settings"
	msgGroupSettingsOK         = "group_settings_ok"

	// Creator flow.
	msgErrCreatorLink            = "err_creator_link"
	msgCreatorRegisterInfo       = "creator_register_info"
	msgCreatorRegisteredNoGroup  = "creator_registered_no_group_html"
	msgCreatorRegistered         = "creator_registered_html"
	msgCreatorEventSubActive     = "creator_eventsub_active"
	msgCreatorEventSubInactive   = "creator_eventsub_inactive"
	msgCreatorEventSubUnknown    = "creator_eventsub_unknown"
	msgCreatorEventSubFail       = "creator_eventsub_fail"
	msgCreatorAuthHealthy        = "creator_auth_healthy"
	msgCreatorAuthReconnect      = "creator_auth_reconnect_required"
	msgCreatorSubscribersPending = "creator_subscribers_pending"
	msgCreatorSubscribersReady   = "creator_subscribers_ready"
	msgCreatorGroupsNone         = "creator_groups_none"
	msgCreatorExchangeFail       = "creator_exchange_fail"
	msgCreatorReconnectInfo      = "creator_reconnect_info"
	msgCreatorReconnectMismatch  = "creator_reconnect_mismatch"
	msgCreatorReconnectNeeded    = "creator_reconnect_needed"
	msgCreatorScopeMissing       = "creator_scope_missing"
	msgCreatorUserInfoFail       = "creator_userinfo_fail"
	msgCreatorStoreFail          = "creator_store_fail"

	// Viewer flow.
	msgLinkPromptHTML    = "link_prompt_html"
	msgOAuthExchangeFail = "oauth_exchange_fail"
	msgOAuthUserInfoFail = "oauth_userinfo_fail"
	msgOAuthSaveFail     = "oauth_save_fail"
	msgSubEndPartial     = "sub_end_partial"

	// Reset flow.
	msgResetNothingHTML        = "reset_nothing_html"
	msgResetDoneViewerHTML     = "reset_done_viewer_html"
	msgResetDoneCreatorHTML    = "reset_done_creator_html"
	msgResetDoneBothHTML       = "reset_done_both_html"
	msgResetChooseScopeHTML    = "reset_choose_scope_html"
	msgResetConfirmViewerHTML  = "reset_confirm_viewer_html"
	msgResetConfirmCreatorHTML = "reset_confirm_creator_html"
	msgResetConfirmBothHTML    = "reset_confirm_both_html"
	msgResetExitHTML           = "reset_exit_html"

	// Buttons.
	btnRegisterCreatorOpen = "btn_register_creator_open"
	btnReconnectCreator    = "btn_reconnect_creator"
	btnLinkTwitch          = "btn_link_twitch"
	btnCopyLink            = "btn_copy_link"
	btnJoin                = "btn_join"
	btnResetViewerData     = "btn_reset_viewer_data"
	btnResetCreatorData    = "btn_reset_creator_data"
	btnResetAllData        = "btn_reset_all_data"
	btnResetConfirm        = "btn_reset_confirm"
	btnBack                = "btn_back"
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

	registerCallback(ui.ActionRefreshViewer, c.handleViewerStart)
	registerCallback(ui.ActionRefreshCreator, c.handleCreatorRegistrationStart)
	registerCallback(ui.ActionRegisterCreator, c.handleCreatorRegistrationStart)
	registerCallback(ui.ActionReconnectCreator, c.handleCreatorReconnectStart)
	registerCallback(ui.ActionResetConfirm, c.handleResetPrompt)
	registerCallback(ui.ActionResetPickViewer, c.handleResetViewerConfirmPrompt)
	registerCallback(ui.ActionResetPickCreator, c.handleResetCreatorConfirmPrompt)
	registerCallback(ui.ActionResetPickBoth, c.handleResetBothConfirmPrompt)
	registerCallback(ui.ActionResetPickerBack, c.handleResetBackToMenu)
	registerCallback(ui.ActionResetPickerCancel, c.handleResetCancel)
	registerCallback(ui.ActionResetConfirmBack, c.handleResetBack)
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
	if q.Data == ui.ActionRefreshViewer || q.Data == ui.ActionRefreshCreator {
		callbackText = i18n.Translate(lang, msgCbRefreshed)
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
