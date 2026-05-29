package app

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
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
	"imsub/internal/app/readiness"
	"imsub/internal/app/startup"
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
	bootStart := time.Now()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	if err := i18n.Ensure(); err != nil {
		return fmt.Errorf("i18n init failed: %w", err)
	}

	logger := newLogger(cfg.DebugLogs)
	metrics := observability.New()
	startupRec := startup.NewRecorderAt(logger, metrics, bootStart)

	var s *redis.Store
	if err := startupRec.Phase("redis_connect", func() error {
		var perr error
		s, perr = redis.NewStore(cfg.RedisURL, logger)
		if perr != nil {
			return fmt.Errorf("create redis store: %w", perr)
		}
		s.SetCommandObserver(metrics)
		return nil
	}); err != nil {
		startupRec.Ready("failed")
		return fmt.Errorf("redis error: %w", err)
	}

	if err := startupRec.Phase("redis_schema_ensure", func() error {
		schemaCtx, schemaCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer schemaCancel()
		return s.EnsureSchema(schemaCtx)
	}); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			logger.Warn("redis close failed after schema init error", "err", closeErr)
		}
		startupRec.Ready("failed")
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

	var (
		tgBot     *telego.Bot
		tgHandler *telegohandler.BotHandler
		tgUpdates chan telego.Update
	)
	if err := startupRec.Phase("telegram_runtime_init", func() error {
		var perr error
		tgBot, tgHandler, tgUpdates, perr = initTelegramRuntime(ctx, telegramRuntimeDeps{
			config:   cfg,
			limiter:  tgLimiter,
			logger:   logger,
			recorder: startupRec,
		})
		if perr != nil {
			return fmt.Errorf("init telegram runtime: %w", perr)
		}
		return nil
	}); err != nil {
		startupRec.Ready("failed")
		return fmt.Errorf("telegram runtime init failed: %w", err)
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
	oauthSvc := core.NewOAuthService(s, twitchAPI)
	creatorSvc := core.NewCreatorService(s, eventSubSvc, logger)
	creatorStatusUC := usecase.NewCreatorStatusUseCase(creatorSvc, eventSink)
	viewerOAuthUC := usecase.NewViewerOAuthUseCase(oauthSvc, eventSink)
	creatorOAuthUC := usecase.NewCreatorOAuthUseCase(oauthSvc, eventSink)
	privacyUC := usecase.NewPrivacyUseCase(s, cfg.PrivacyPolicyVersion, cfg.PrivacyReceiptTTL)
	groupRegistrationUC := usecase.NewGroupRegistrationUseCase(s, eventSink)
	groupPolicyUpdateUC := usecase.NewGroupPolicyUpdateUseCase(s, eventSink)
	groupLanguageUpdateUC := usecase.NewGroupLanguageUpdateUseCase(s, eventSink)
	groupMemberTagUpdateUC := usecase.NewGroupMemberTagUpdateUseCase(s, eventSink)
	creatorActivationUC := usecase.NewCreatorActivationUseCase(eventSubSvc, eventSink)
	jobRunner := jobs.NewRunner(logger, eventSink)
	subscriberTask := jobs.NewSubscriberTask(reconcileSvc)
	eventSubTask := jobs.NewEventSubTask(eventSubSvc)
	tgClient := telegramclient.New(tgBot, tgLimiter, logger)
	tgClient.SetObserver(eventSink)
	tgGroups := telegramgroups.New(tgBot, tgLimiter, logger, s, eventSink)
	var godAccess = core.NewGodAccessChecker(cfg.GodTelegramUserIDs...)
	integrityTask := jobs.NewIntegrityAuditTask(s, logger, eventSink)
	blocklistSvc = core.NewCreatorBlocklistService(s, twitchAPI, tgGroups, logger)
	var groupBootstrapSvc *core.GroupBootstrapService
	var mtprotoClient *telegrammtproto.Client
	if cfg.MTProtoEnabled() {
		if err := startupRec.Phase("mtproto_init", func() error {
			var perr error
			mtprotoClient, perr = telegrammtproto.New(cfg.TelegramMTProtoAppID, cfg.TelegramMTProtoHash, cfg.TelegramMTProtoSession)
			if perr != nil {
				return fmt.Errorf("create mtproto client: %w", perr)
			}
			return nil
		}); err != nil {
			startupRec.Ready("failed")
			return fmt.Errorf("telegram mtproto init failed: %w", err)
		}
		var mtprotoSelfID int64
		if err := startupRec.Phase("mtproto_validate", func() error {
			validateCtx, validateCancel := context.WithTimeout(ctx, 10*time.Second)
			defer validateCancel()
			var perr error
			mtprotoSelfID, perr = mtprotoClient.Validate(validateCtx)
			if perr != nil {
				return fmt.Errorf("validate mtproto client: %w", perr)
			}
			return nil
		}); err != nil {
			startupRec.Ready("failed")
			return fmt.Errorf("telegram mtproto validation failed: %w", err)
		}
		godAccess = godAccess.WithIDs(mtprotoSelfID)
		groupBootstrapSvc = core.NewGroupBootstrapService(s, tgGroups, mtprotoClient, godAccess, eventSink, logger)
	} else {
		logger.Info("telegram mtproto bootstrap disabled; groups will not dump pre-existing members on registration")
	}
	subscriptionSvc := core.NewSubscriptionService(s, godAccess)
	memberTagSyncSvc := core.NewMemberTagSyncService(s, tgGroups, groupBootstrapSvc, mtprotoClient, logger)
	subscriptionEndUC := usecase.NewSubscriptionEndUseCase(subscriptionSvc, eventSink)
	groupUnregistrationUC := usecase.NewGroupUnregistrationUseCase(s, eventSubSvc, godAccess, eventSink)
	gracePolicyTask := jobs.NewGracePolicyTask(s, tgGroups, godAccess, logger)
	kickPolicyTask := jobs.NewKickPolicyTask(s, tgGroups, godAccess, logger)
	memberTagSyncTask := jobs.NewMemberTagSyncTask(memberTagSyncSvc)

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
		GodAccess:           godAccess,
		ViewerOAuth:         viewerOAuthUC,
		CreatorOAuth:        creatorOAuthUC,
		GroupRegistration:   groupRegistrationUC,
		GroupUnregistration: groupUnregistrationUC,
		GroupPolicyUpdate:   groupPolicyUpdateUC,
		GroupLanguageUpdate: groupLanguageUpdateUC,
		GroupMemberTagSync:  groupMemberTagUpdateUC,
		GroupBootstrap:      groupBootstrapSvc,
		CreatorActivation:   creatorActivationUC,
		SubscriptionEnd:     subscriptionEndUC,
		Privacy:             privacyUC,
		MemberTagSync:       memberTagSyncSvc,
		Events:              eventSink,
	})
	viewerAccessUC := usecase.NewViewerAccessUseCase(core.NewViewerService(s, flowController.ViewerGroupOps(), godAccess, logger, eventSink), godAccess, eventSink)
	resetSvc := core.NewResetService(s, flowController.KickFromGroup, godAccess, logger)
	resetSvc.SetEventSubCleaner(eventSubSvc)
	flowController.SetViewerAccessUseCase(viewerAccessUC)
	flowController.SetResetUseCase(usecase.NewResetUseCase(resetSvc, eventSink))
	subscriptionGraceTask := jobs.NewSubscriptionGraceTask(s, tgGroups, flowController, godAccess, logger)
	memberCleanupTask := jobs.NewMemberCleanupTask(s, tgGroups, flowController, godAccess, logger)
	productMetricsTask := jobs.NewProductMetricsSnapshotTask(s, metrics)
	creatorMetricsTask := jobs.NewCreatorMetricsTask(s, metrics, logger)
	privacyRetentionTask := jobs.NewPrivacyRetentionTask(s, cfg.UntrackedRetention)
	var backupTask jobs.Task
	if cfg.BackupEnabled() {
		var s3Client *s3.Client
		if err := startupRec.Phase("s3_client_init", func() error {
			var perr error
			s3Client, perr = s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
			if perr != nil {
				return fmt.Errorf("create s3 client: %w", perr)
			}
			return nil
		}); err != nil {
			startupRec.Ready("failed")
			return fmt.Errorf("s3 error: %w", err)
		}
		backupTask = jobs.NewRedisBackupTaskWithFullInterval(s, s3Client, logger, metrics, cfg.FullBackupInterval)
	}
	eventSubSvc.SetObserver(eventSink)
	blocklistSvc.SetObserver(eventSink)
	eventSubSvc.SetNotifier(flowController)
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

	readyFlag := readiness.New()

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
			Config:    cfg,
			Store:     s,
			Logger:    logger,
			Metrics:   metrics,
			Readiness: readyFlag,
			Handlers: server.Handlers{
				OAuthStart:      httpController.OAuthStart,
				TwitchCallback:  httpController.TwitchCallback,
				EventSubWebhook: httpController.EventSubWebhook,
				TelegramWebhook: httpController.TelegramWebhook,
			},
		})
	})

	if cfg.TelegramWebhookSecret != "" {
		if err := startupRec.Phase("telegram_webhook_set", func() error {
			webhookCtx, webhookCancel := context.WithTimeout(gctx, 5*time.Second)
			defer webhookCancel()
			return setTelegramWebhook(webhookCtx, telegramWebhookDeps{
				config:  cfg,
				bot:     tgBot,
				limiter: tgLimiter,
				logger:  logger,
			})
		}); err != nil {
			startupRec.Ready("failed")
			cancel()
			_ = g.Wait()
			return fmt.Errorf("telegram webhook setup failed: %w", err)
		}
	}

	readyFlag.MarkReady()
	startupRec.Ready("ok")

	g.Go(func() error {
		if err := configureBotCommands(gctx, tgBot, tgLimiter, startupRec); err != nil {
			logger.Warn("telegram metadata setup failed", "err", err)
		}
		return nil
	})
	g.Go(func() error {
		syncCtx, syncCancel := context.WithTimeout(gctx, 5*time.Second)
		defer syncCancel()
		_ = startupRec.Phase("reconnect_gauge_sync", func() error {
			eventSubSvc.SyncReconnectRequiredGauge(syncCtx)
			return nil
		})
		return nil
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         eventSubTask,
			InitialDelay: jitteredDelay(30 * time.Second),
			Interval:     1 * time.Hour,
			Timeout:      10 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         subscriberTask,
			InitialDelay: jitteredDelay(60 * time.Second),
			Interval:     15 * time.Minute,
			Timeout:      10 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         integrityTask,
			InitialDelay: jitteredDelay(120 * time.Second),
			Interval:     2 * time.Hour,
			Timeout:      15 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         gracePolicyTask,
			InitialDelay: jitteredDelay(120 * time.Second),
			Interval:     1 * time.Hour,
			Timeout:      15 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         kickPolicyTask,
			InitialDelay: jitteredDelay(60 * time.Second),
			Interval:     15 * time.Minute,
			Timeout:      10 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         memberTagSyncTask,
			InitialDelay: jitteredDelay(90 * time.Second),
			Interval:     15 * time.Minute,
			Timeout:      10 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     subscriptionGraceTask,
			Interval: 15 * time.Minute,
			Timeout:  10 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:     memberCleanupTask,
			Interval: 1 * time.Minute,
			Timeout:  45 * time.Second,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         productMetricsTask,
			InitialDelay: jitteredDelay(60 * time.Second),
			Interval:     5 * time.Minute,
			Timeout:      2 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         creatorMetricsTask,
			InitialDelay: jitteredDelay(60 * time.Second),
			Interval:     5 * time.Minute,
			Timeout:      4 * time.Minute,
		})
	})
	g.Go(func() error {
		return jobRunner.RunScheduled(gctx, jobs.Schedule{
			Task:         privacyRetentionTask,
			InitialDelay: jitteredDelay(300 * time.Second),
			Interval:     12 * time.Hour,
			Timeout:      2 * time.Hour,
		})
	})
	if backupTask != nil {
		g.Go(func() error {
			return jobRunner.RunScheduled(gctx, jobs.Schedule{
				Task:         backupTask,
				InitialDelay: jitteredDelay(30 * time.Second),
				Interval:     cfg.BackupInterval,
				Timeout:      25 * time.Minute,
			})
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("errgroup wait: %w", err)
	}
	return nil
}

type telegramRuntimeDeps struct {
	config   config.Config
	limiter  *ratelimit.RateLimiter
	logger   *slog.Logger
	recorder *startup.Recorder
}

type telegramWebhookDeps struct {
	config  config.Config
	bot     *telego.Bot
	limiter *ratelimit.RateLimiter
	logger  *slog.Logger
}

func initTelegramRuntime(ctx context.Context, deps telegramRuntimeDeps) (*telego.Bot, *telegohandler.BotHandler, chan telego.Update, error) {
	var bot *telego.Bot
	if err := deps.recorder.Phase("telegram_bot_init", func() error {
		var perr error
		bot, perr = telego.NewBot(deps.config.TelegramBotToken, telego.WithAPICaller(newTelegramAPICaller()))
		if perr != nil {
			return fmt.Errorf("create telegram bot: %w", perr)
		}
		return nil
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("telegram init failed: %w", err)
	}

	var (
		updates   <-chan telego.Update
		tgUpdates chan telego.Update
	)
	if deps.config.TelegramWebhookSecret != "" {
		tgUpdates = make(chan telego.Update, 256)
		updates = tgUpdates
	} else {
		var err error
		updates, err = bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{AllowedUpdates: telegramAllowedUpdates()})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("telegram polling failed: %w", err)
		}
	}

	var tgHandler *telegohandler.BotHandler
	if err := deps.recorder.Phase("telegram_handler_init", func() error {
		var perr error
		tgHandler, perr = telegohandler.NewBotHandler(bot, updates)
		if perr != nil {
			return fmt.Errorf("create telegram handler: %w", perr)
		}
		return nil
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("telegram handler init failed: %w", err)
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

func configureBotCommands(ctx context.Context, bot *telego.Bot, tgLimiter *ratelimit.RateLimiter, recorder *startup.Recorder) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return recorder.Phase("telegram_set_commands", func() error {
			if err := tgLimiter.Wait(gctx, 0); err != nil {
				return fmt.Errorf("limiter wait for bot commands: %w", err)
			}
			if err := bot.SetMyCommands(gctx, &telego.SetMyCommandsParams{
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
			return nil
		})
	})
	g.Go(func() error {
		return recorder.Phase("telegram_set_short_description", func() error {
			if err := tgLimiter.Wait(gctx, 0); err != nil {
				return fmt.Errorf("limiter wait for bot short description: %w", err)
			}
			if err := bot.SetMyShortDescription(gctx, &telego.SetMyShortDescriptionParams{
				ShortDescription: "Subscribers-only Telegram groups, powered by Twitch.",
			}); err != nil {
				return fmt.Errorf("set my short description: %w", err)
			}
			return nil
		})
	})
	g.Go(func() error {
		return recorder.Phase("telegram_set_description", func() error {
			if err := tgLimiter.Wait(gctx, 0); err != nil {
				return fmt.Errorf("limiter wait for bot description: %w", err)
			}
			if err := bot.SetMyDescription(gctx, &telego.SetMyDescriptionParams{
				Description: "ImSub manages access to private Telegram groups based on active Twitch subscriptions.\n\nHow it works\n• Creators link a Twitch channel and a Telegram group\n• Viewers connect their Twitch account and get invite links\n• Access is granted and revoked automatically\n\nCommands\n/start — connect Twitch and see available groups\n/creator — set up a creator account\n/reset — delete your linked data\n/info — about this bot\n\nProject: github.com/ale-grassi/imsub\nLicense: MIT",
			}); err != nil {
				return fmt.Errorf("set my description: %w", err)
			}
			return nil
		})
	})
	if err := g.Wait(); err != nil {
		return fmt.Errorf("configure bot commands: %w", err)
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

// A jittered delay returns the input duration with plus or minus 20 percent random jitter.
// Durations less than or equal to zero are returned as-is so immediate runs stay immediate.
func jitteredDelay(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	span := float64(d) * 0.4
	var randomBytes [8]byte
	if _, err := crand.Read(randomBytes[:]); err != nil {
		return d
	}
	unit := float64(binary.BigEndian.Uint64(randomBytes[:])) / float64(^uint64(0))
	offset := unit*span - span/2
	return d + time.Duration(offset)
}

func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
