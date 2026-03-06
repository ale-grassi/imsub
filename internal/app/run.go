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
	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/jobs"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/observability"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/http/handlers"
	"imsub/internal/transport/http/server"
	"imsub/internal/transport/telegram/flows"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"golang.org/x/sync/errgroup"
)

func telegramAllowedUpdates() []string {
	return []string{"message", "callback_query", "chat_join_request"}
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

	eventSubSvc := core.NewEventSub(s, twitchAPI, logger)
	reconcileSvc := core.NewReconciler(s, eventSubSvc.DumpCurrentSubscribers, logger)
	jobsSvc := jobs.New(s, reconcileSvc, logger, metrics)
	subscriptionSvc := core.NewSubscription(s)
	oauthSvc := core.NewOAuth(s, twitchAPI)
	creatorSvc := core.NewCreator(s, eventSubSvc, logger)

	flowController := flows.New(flows.Dependencies{
		Config:          cfg,
		Store:           s,
		TelegramLimiter: tgLimiter,
		Logger:          logger,
		TelegramBot:     tgBot,
		TelegramHandler: tgHandler,
		Services: flows.Services{
			EventSub:     eventSubSvc,
			Subscription: subscriptionSvc,
			OAuth:        oauthSvc,
			Creator:      creatorSvc,
		},
		Factories: flows.ServiceFactories{
			Viewer: func(groupOps core.GroupOps) *core.Viewer {
				return core.NewViewer(s, groupOps, logger)
			},
			Reset: func(kick func(ctx context.Context, groupChatID, telegramUserID int64) error) *core.Resetter {
				return core.NewResetter(s, kick, logger)
			},
		},
	})
	flowController.RegisterTelegramHandlers()

	httpController := handlers.New(handlers.Dependencies{
		Config:          cfg,
		Store:           s,
		Logger:          logger,
		Observer:        metrics,
		TelegramUpdates: tgUpdates,
		ViewerOAuth:     flowController.HandleViewerOAuthCallback,
		CreatorOAuth:    flowController.HandleCreatorOAuthCallback,
		SubscriptionEnd: flowController.HandleSubscriptionEnd,
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
		bootstrapCtx, bootstrapCancel := context.WithTimeout(gctx, 2*time.Minute)
		defer bootstrapCancel()
		eventSubSvc.BootstrapEventSub(bootstrapCtx)
		return nil
	})
	g.Go(func() error {
		return jobsSvc.RunSubscriberReconciler(gctx, 15*time.Minute)
	})
	g.Go(func() error {
		return jobsSvc.RunIntegrityAudits(gctx, 20*time.Minute)
	})

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
	bot, err := telego.NewBot(deps.config.TelegramBotToken)
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

func configureBotCommands(ctx context.Context, bot *telego.Bot, tgLimiter *ratelimit.RateLimiter) error {
	if err := tgLimiter.Wait(ctx, 0); err != nil {
		return fmt.Errorf("limiter wait for bot commands: %w", err)
	}
	if err := bot.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: []telego.BotCommand{
			{Command: "start", Description: "Open user dashboard"},
			{Command: "creator", Description: "Register creator account"},
			{Command: "registergroup", Description: "Bind this group to creator"},
			{Command: "reset", Description: "Clear your linked data"},
		},
	}); err != nil {
		return fmt.Errorf("set my commands: %w", err)
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
