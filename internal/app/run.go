package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"imsub/internal/adapter/redis"
	"imsub/internal/adapter/s3"
	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/jobs"
	"imsub/internal/operator"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/observability"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/http/handlers"
	"imsub/internal/transport/http/server"
	telegrambot "imsub/internal/transport/telegram/bot"
	telegramclient "imsub/internal/transport/telegram/client"
	telegramgroups "imsub/internal/transport/telegram/groups"
	telegrammtproto "imsub/internal/transport/telegram/mtproto"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/valyala/fasthttp"
	"golang.org/x/sync/errgroup"
)

const (
	telegramRetryMaxAttempts = 3
	telegramRetryExponent    = 2
	telegramRetryStartDelay  = 250 * time.Millisecond
	telegramRetryMaxDelay    = 3 * time.Second
)

func telegramAllowedUpdates() []string {
	return []string{"message", "callback_query", "chat_join_request", "chat_member", "my_chat_member"}
}

// Run executes the service composition root.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	if err := i18n.Ensure(); err != nil {
		return fmt.Errorf("i18n init failed: %w", err)
	}

	logger := newLogger(cfg.DebugLogs)
	s, err := redis.NewStore(cfg.RedisURL, logger)
	if err != nil {
		return fmt.Errorf("redis error: %w", err)
	}

	if err := s.EnsureSchema(context.Background()); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			logger.Warn("redis close failed after schema init error", "err", closeErr)
		}
		return fmt.Errorf("schema init failed: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Warn("redis close failed", "err", err)
		}
	}()

	httpClient := &http.Client{Timeout: 20 * time.Second}
	twitchAPI := twitch.NewClient(cfg, httpClient)
	tgLimiter := ratelimit.NewRateLimiter(25, time.Second)
	defer tgLimiter.Close()
	metrics := observability.New()
	operatorReadModel := operator.NewReadModel()
	eventSink := events.MultiSink{
		Sinks: []events.EventSink{
			metrics,
			operatorReadModel,
			observability.NewEventLogger(logger),
		},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tgBot, tgHandler, tgUpdates, err := initTelegramRuntime(ctx, telegramRuntimeDeps{
		config:  cfg,
		limiter: tgLimiter,
		logger:  logger,
	})
	if err != nil {
		return err
	}

	eventSubSvc := core.NewEventSubService(s, twitchAPI, logger)
	var blocklistSvc *core.CreatorBlocklistService
	reconcileSvc := core.NewReconcilerService(s, func(ctx context.Context, creator core.Creator) (int, error) {
		count, err := eventSubSvc.DumpCurrentSubscribers(ctx, creator)
		if err != nil {
			return count, fmt.Errorf("dump current subscribers: %w", err)
		}
		if blocklistSvc != nil {
			if _, err := blocklistSvc.SyncCreatorBlocklist(ctx, creator); err != nil {
				return count, fmt.Errorf("sync creator blocklist: %w", err)
			}
		}
		return count, nil
	}, logger)
	subscriptionSvc := core.NewSubscriptionService(s)
	oauthSvc := core.NewOAuthService(s, twitchAPI)
	creatorSvc := core.NewCreatorService(s, eventSubSvc, logger)
	creatorStatusUC := usecase.NewCreatorStatusUseCase(creatorSvc, eventSink)
	viewerOAuthUC := usecase.NewViewerOAuthUseCase(oauthSvc, eventSink)
	creatorOAuthUC := usecase.NewCreatorOAuthUseCase(oauthSvc, eventSink)
	privacyUC := usecase.NewPrivacyUseCase(s, cfg.PrivacyPolicyVersion, cfg.PrivacyReceiptTTL)
	groupRegistrationUC := usecase.NewGroupRegistrationUseCase(s, eventSink)
	groupPolicyUpdateUC := usecase.NewGroupPolicyUpdateUseCase(s, eventSink)
	groupLanguageUpdateUC := usecase.NewGroupLanguageUpdateUseCase(s, eventSink)
	creatorActivationUC := usecase.NewCreatorActivationUseCase(eventSubSvc, eventSink)
	subscriptionEndUC := usecase.NewSubscriptionEndUseCase(subscriptionSvc, eventSink)
	jobRunner := jobs.NewRunner(logger, eventSink)
	subscriberTask := jobs.NewSubscriberTask(reconcileSvc)
	eventSubTask := jobs.NewEventSubTask(eventSubSvc)
	tgClient := telegramclient.New(tgBot, tgLimiter, logger)
	tgClient.SetObserver(eventSink)
	tgGroups := telegramgroups.New(tgBot, tgLimiter, logger, s, eventSink)
	groupUnregistrationUC := usecase.NewGroupUnregistrationUseCase(s, eventSubSvc, eventSink)
	gracePolicyTask := jobs.NewGracePolicyTask(s, tgGroups, logger)
	integrityTask := jobs.NewIntegrityAuditTask(s, logger, eventSink)
	blocklistSvc = core.NewCreatorBlocklistService(s, twitchAPI, tgGroups, logger)
	var groupBootstrapSvc *core.GroupBootstrapService
	if cfg.MTProtoEnabled() {
		mtprotoClient, err := telegrammtproto.New(cfg.TelegramMTProtoAppID, cfg.TelegramMTProtoHash, cfg.TelegramMTProtoSession)
		if err != nil {
			return fmt.Errorf("telegram mtproto init failed: %w", err)
		}
		if _, err := mtprotoClient.Validate(ctx); err != nil {
			return fmt.Errorf("telegram mtproto validation failed: %w", err)
		}
		groupBootstrapSvc = core.NewGroupBootstrapService(s, tgGroups, mtprotoClient, eventSink, logger)
	} else {
		logger.Info("telegram mtproto bootstrap disabled; groups will not dump pre-existing members on registration")
	}

	flowController := telegrambot.New(telegrambot.Dependencies{
		Config:              cfg,
		Store:               s,
		TelegramLimiter:     tgLimiter,
		Logger:              logger,
		TelegramBot:         tgBot,
		TelegramHandler:     tgHandler,
		TelegramClient:      tgClient,
		TelegramGroups:      tgGroups,
		CreatorStatus:       creatorStatusUC,
		CreatorBlocklist:    blocklistSvc,
		ViewerOAuth:         viewerOAuthUC,
		CreatorOAuth:        creatorOAuthUC,
		GroupRegistration:   groupRegistrationUC,
		GroupUnregistration: groupUnregistrationUC,
		GroupPolicyUpdate:   groupPolicyUpdateUC,
		GroupLanguageUpdate: groupLanguageUpdateUC,
		GroupBootstrap:      groupBootstrapSvc,
		CreatorActivation:   creatorActivationUC,
		SubscriptionEnd:     subscriptionEndUC,
		Privacy:             privacyUC,
		Events:              eventSink,
	})
	viewerAccessUC := usecase.NewViewerAccessUseCase(core.NewViewerService(s, flowController.ViewerGroupOps(), logger, eventSink), eventSink)
	resetSvc := core.NewResetService(s, flowController.KickFromGroup, logger)
	resetSvc.SetEventSubCleaner(eventSubSvc)
	flowController.SetViewerAccessUseCase(viewerAccessUC)
	flowController.SetResetUseCase(usecase.NewResetUseCase(resetSvc, eventSink))
	subscriptionGraceTask := jobs.NewSubscriptionGraceTask(s, tgGroups, flowController, logger)
	memberCleanupTask := jobs.NewMemberCleanupTask(s, tgGroups, flowController, logger)
	productMetricsTask := jobs.NewProductMetricsSnapshotTask(s, metrics)
	creatorMetricsTask := jobs.NewCreatorMetricsTask(s, metrics, logger)
	privacyRetentionTask := jobs.NewPrivacyRetentionTask(s, cfg.UntrackedRetention)
	var backupTask jobs.Task
	if cfg.BackupEnabled() {
		s3Client, err := s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
		if err != nil {
			return fmt.Errorf("s3 error: %w", err)
		}
		backupTask = jobs.NewRedisBackupTask(s, s3Client, logger, metrics)
	}
	eventSubSvc.SetObserver(eventSink)
	blocklistSvc.SetObserver(eventSink)
	eventSubSvc.SetNotifier(flowController)
	eventSubSvc.SyncReconnectRequiredGauge(context.Background())
	flowController.RegisterTelegramHandlers()

	httpController := handlers.New(handlers.Dependencies{
		Config:            cfg,
		Store:             s,
		Logger:            logger,
		Events:            eventSink,
		TelegramUpdates:   tgUpdates,
		ViewerOAuth:       flowController.HandleViewerOAuthCallback,
		CreatorOAuth:      flowController.HandleCreatorOAuthCallback,
		SubscriptionStart: flowController.HandleSubscriptionStart,
		SubscriptionEnd:   flowController.HandleSubscriptionEnd,
		BlocklistBan:      blocklistSvc.HandleBanEvent,
		BlocklistUnban:    blocklistSvc.HandleUnbanEvent,
	})

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		err := tgHandler.Start()
		if err != nil && gctx.Err() == nil {
			return fmt.Errorf("telegram handler stopped unexpectedly: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(gctx), 5*time.Second)
		defer stopCancel()
		if err := tgHandler.StopWithContext(stopCtx); err != nil {
			logger.Warn("telegram handler stop failed", "err", err)
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(gctx), 5*time.Second)
		defer stopCancel()
		if err := flowController.WaitBackground(stopCtx); err != nil {
			logger.Warn("telegram background work wait failed", "err", err)
		}
		return nil
	})
	g.Go(func() error {
		return server.Run(gctx, server.Dependencies{
			Config:  cfg,
			Store:   s,
			Logger:  logger,
			Metrics: metrics,
			Handlers: server.Handlers{
				OAuthStart:      httpController.OAuthStart,
				TwitchCallback:  httpController.TwitchCallback,
				EventSubWebhook: httpController.EventSubWebhook,
				TelegramWebhook: httpController.TelegramWebhook,
			},
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         eventSubTask,
			InitialDelay: 3 * time.Second,
			Interval:     1 * time.Hour,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     subscriberTask,
			Interval: 15 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     integrityTask,
			Interval: 20 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     gracePolicyTask,
			Interval: 1 * time.Hour,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     subscriptionGraceTask,
			Interval: 15 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     memberCleanupTask,
			Interval: 1 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     productMetricsTask,
			Interval: 5 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     creatorMetricsTask,
			Interval: 5 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     privacyRetentionTask,
			Interval: 12 * time.Hour,
		})
	})
	if backupTask != nil {
		g.Go(func() error {
			return jobRunner.RunScheduled(gctx, jobs.Schedule{
				Task:         backupTask,
				InitialDelay: 30 * time.Second,
				Interval:     cfg.BackupInterval,
			})
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("errgroup wait: %w", err)
	}
	return nil
}

type telegramRuntimeDeps struct {
	config  config.Config
	limiter *ratelimit.RateLimiter
	logger  *slog.Logger
}

type telegramWebhookDeps struct {
	config  config.Config
	bot     *telego.Bot
	limiter *ratelimit.RateLimiter
	logger  *slog.Logger
}

func initTelegramRuntime(ctx context.Context, deps telegramRuntimeDeps) (*telego.Bot, *telegohandler.BotHandler, chan telego.Update, error) {
	bot, err := telego.NewBot(deps.config.TelegramBotToken, telego.WithAPICaller(newTelegramAPICaller()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("telegram init failed: %w", err)
	}

	if err := configureBotCommands(ctx, bot, deps.limiter); err != nil {
		return nil, nil, nil, fmt.Errorf("telegram commands setup failed: %w", err)
	}

	var (
		updates   <-chan telego.Update
		tgUpdates chan telego.Update
	)
	if deps.config.TelegramWebhookSecret != "" {
		tgUpdates = make(chan telego.Update, 256)
		updates = tgUpdates
	} else {
		updates, err = bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{AllowedUpdates: telegramAllowedUpdates()})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("telegram polling failed: %w", err)
		}
	}

	tgHandler, err := telegohandler.NewBotHandler(bot, updates)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("telegram handler init failed: %w", err)
	}

	if deps.config.TelegramWebhookSecret != "" {
		if err := setTelegramWebhook(ctx, telegramWebhookDeps{
			config:  deps.config,
			bot:     bot,
			limiter: deps.limiter,
			logger:  deps.logger,
		}); err != nil {
			return nil, nil, nil, err
		}
	}

	return bot, tgHandler, tgUpdates, nil
}

func newTelegramAPICaller() telegoapi.Caller {
	return &telegoapi.RetryCaller{
		Caller:            telegoapi.FastHTTPCaller{Client: &fasthttp.Client{}},
		MaxAttempts:       telegramRetryMaxAttempts,
		ExponentBase:      telegramRetryExponent,
		StartDelay:        telegramRetryStartDelay,
		MaxDelay:          telegramRetryMaxDelay,
		RateLimit:         telegoapi.RetryRateLimitWaitOrAbort,
		BufferRequestData: true,
	}
}

func configureBotCommands(ctx context.Context, bot *telego.Bot, tgLimiter *ratelimit.RateLimiter) error {
	if err := tgLimiter.Wait(ctx, 0); err != nil {
		return fmt.Errorf("limiter wait for bot commands: %w", err)
	}
	if err := bot.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: []telego.BotCommand{
			{Command: "start", Description: "Open user dashboard"},
			{Command: "creator", Description: "Register creator account"},
			{Command: "linkgroup", Description: "Link this group to creator"},
			{Command: "unlinkgroup", Description: "Unlink this group from creator"},
			{Command: "reset", Description: "Clear your linked data"},
			{Command: "info", Description: "About this bot"},
		},
	}); err != nil {
		return fmt.Errorf("set my commands: %w", err)
	}
	if err := tgLimiter.Wait(ctx, 0); err != nil {
		return fmt.Errorf("limiter wait for bot short description: %w", err)
	}
	if err := bot.SetMyShortDescription(ctx, &telego.SetMyShortDescriptionParams{
		ShortDescription: "Subscribers-only Telegram groups, powered by Twitch.",
	}); err != nil {
		return fmt.Errorf("set my short description: %w", err)
	}
	if err := tgLimiter.Wait(ctx, 0); err != nil {
		return fmt.Errorf("limiter wait for bot description: %w", err)
	}
	if err := bot.SetMyDescription(ctx, &telego.SetMyDescriptionParams{
		Description: "ImSub manages access to private Telegram groups based on active Twitch subscriptions.\n\nHow it works\n• Creators link a Twitch channel and a Telegram group\n• Viewers connect their Twitch account and get invite links\n• Access is granted and revoked automatically\n\nCommands\n/start — connect Twitch and see available groups\n/creator — set up a creator account\n/reset — delete your linked data\n/info — about this bot\n\nProject: github.com/ale-grassi/imsub\nLicense: MIT",
	}); err != nil {
		return fmt.Errorf("set my description: %w", err)
	}
	return nil
}

func setTelegramWebhook(ctx context.Context, deps telegramWebhookDeps) error {
	webhookURL := deps.config.PublicBaseURL + deps.config.TelegramWebhookPath
	if err := deps.limiter.Wait(ctx, 0); err != nil {
		return fmt.Errorf("set webhook rate limit wait failed: %w", err)
	}
	if err := deps.bot.SetWebhook(ctx, &telego.SetWebhookParams{
		URL:            webhookURL,
		SecretToken:    deps.config.TelegramWebhookSecret,
		AllowedUpdates: telegramAllowedUpdates(),
	}); err != nil {
		return fmt.Errorf("set webhook failed: %w", err)
	}
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("telegram webhook set", "url", webhookURL)
	return nil
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
