package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"imsub/internal/platform/config"
	"imsub/internal/platform/observability"
	httphandlers "imsub/internal/transport/http/handlers"
)

type healthStore interface {
	Ping(ctx context.Context) error
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
	Config   config.Config
	Store    healthStore
	Logger   *slog.Logger
	Metrics  *observability.Metrics
	Handlers Handlers
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
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
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
	mux.Handle("GET /auth/start/{state}", httphandlers.RateLimit(sensitiveLimiter, deps.Handlers.OAuthStart))
	mux.Handle("GET /auth/callback", httphandlers.RateLimit(sensitiveLimiter, deps.Handlers.TwitchCallback))
	mux.Handle("POST "+deps.Config.TwitchWebhookPath, httphandlers.RateLimit(sensitiveLimiter, deps.Handlers.EventSubWebhook))
	if deps.Config.TelegramWebhookSecret != "" {
		mux.Handle("POST "+deps.Config.TelegramWebhookPath, httphandlers.RateLimit(sensitiveLimiter, deps.Handlers.TelegramWebhook))
	}
	if deps.Config.MetricsEnabled && deps.Metrics != nil {
		mux.Handle("GET "+deps.Config.MetricsPath, deps.Metrics.Handler())
	}

	return mux
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
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		serveErr := <-serveErrCh
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return nil
	}
}
