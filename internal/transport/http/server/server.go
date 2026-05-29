package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"imsub/internal/events"
	"imsub/internal/platform/config"
	"imsub/internal/platform/observability"
	httphandlers "imsub/internal/transport/http/handlers"
)

type healthStore interface {
	Ping(ctx context.Context) error
}

// ReadinessChecker reports whether the app has completed startup.
// A nil ReadinessChecker is treated as always ready to preserve legacy behavior.
type ReadinessChecker interface {
	Ready() bool
}

const repoHomepageURL = "https://github.com/ale-grassi/imsub"

// Handlers groups route handlers consumed by the HTTP transport runtime.
type Handlers struct {
	OAuthStart      http.HandlerFunc
	TwitchCallback  http.HandlerFunc
	EventSubWebhook http.HandlerFunc
	TelegramWebhook http.HandlerFunc
}

// Dependencies configures HTTP server construction and lifecycle.
type Dependencies struct {
	Config    config.Config
	Store     healthStore
	Logger    *slog.Logger
	Metrics   *observability.Metrics
	Readiness ReadinessChecker
	Handlers  Handlers
}

func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func newMux(deps Dependencies) *http.ServeMux {
	mux := http.NewServeMux()
	sensitiveLimiter := httphandlers.NewFixedWindowRateLimiter(120, 30, time.Minute)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, repoHomepageURL, http.StatusFound)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx := events.WithForegroundOperationContext(r.Context(), "http_healthz")
		if deps.Readiness != nil && !deps.Readiness.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("starting"))
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := deps.Store.Ping(checkCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			// A write error here only means the client connection closed early.
			_, _ = w.Write([]byte("redis unreachable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		// A write error here only means the client connection closed early.
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /auth/start/{state}", httphandlers.RateLimit(sensitiveLimiter, withForegroundOperation("http_oauth_start", deps.Handlers.OAuthStart)))
	mux.Handle("GET /auth/callback", httphandlers.RateLimit(sensitiveLimiter, withForegroundOperation("http_oauth_callback", deps.Handlers.TwitchCallback)))
	mux.Handle("POST "+deps.Config.TwitchWebhookPath, httphandlers.RateLimit(sensitiveLimiter, withForegroundOperation("http_eventsub_webhook", deps.Handlers.EventSubWebhook)))
	if deps.Config.TelegramWebhookSecret != "" {
		mux.Handle("POST "+deps.Config.TelegramWebhookPath, httphandlers.RateLimit(sensitiveLimiter, withForegroundOperation("http_telegram_webhook", deps.Handlers.TelegramWebhook)))
	}
	if deps.Config.MetricsEnabled && deps.Metrics != nil {
		mux.Handle("GET "+deps.Config.MetricsPath, deps.Metrics.Handler())
	}

	return mux
}

func withForegroundOperation(operation string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(events.WithForegroundOperationContext(r.Context(), operation)))
	})
}

func newHandler(deps Dependencies, logger *slog.Logger) http.Handler {
	logger = loggerOrDefault(logger)

	handler := httphandlers.SecurityHeaders(newMux(deps))
	if deps.Metrics != nil {
		quietRoutes := []string{"GET /healthz", "GET " + deps.Config.MetricsPath}
		handler = deps.Metrics.Middleware(logger, quietRoutes, handler)
	}
	return httphandlers.RequestIDMiddleware(handler)
}

// Run starts the HTTP server and shuts it down when ctx is canceled.
func Run(ctx context.Context, deps Dependencies) error {
	logger := loggerOrDefault(deps.Logger)

	srv := &http.Server{
		Addr:              deps.Config.ListenAddr,
		Handler:           newHandler(deps, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- srv.ListenAndServe()
	}()

	logger.Info("http server listening", "addr", deps.Config.ListenAddr)

	select {
	case err := <-serveErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		serveErr := <-serveErrCh
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve on shutdown: %w", serveErr)
		}
		if shutdownErr != nil {
			return fmt.Errorf("shutdown server: %w", shutdownErr)
		}
		return nil
	}
}
