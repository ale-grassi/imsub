package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"imsub/internal/platform/httputil"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus collectors used by the application.
type Metrics struct {
	registry            *prometheus.Registry
	requestsTotal       *prometheus.CounterVec
	requestDuration     *prometheus.HistogramVec
	requestsInFlight    prometheus.Gauge
	oauthCallbacksTotal *prometheus.CounterVec
	eventsubTotal       *prometheus.CounterVec
	telegramWebhook     *prometheus.CounterVec
	backgroundJobsTotal *prometheus.CounterVec
	backgroundJobTime   *prometheus.HistogramVec
}

// New creates and registers all Prometheus metrics.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_http_requests_total",
				Help: "Total HTTP requests processed by the app.",
			},
			[]string{"method", "route", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "route"},
		),
		requestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_http_requests_in_flight",
			Help: "Current in-flight HTTP requests.",
		}),
		oauthCallbacksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_oauth_callbacks_total",
				Help: "OAuth callbacks by mode and result.",
			},
			[]string{"mode", "result"},
		),
		eventsubTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_eventsub_messages_total",
				Help: "EventSub webhook messages by type and result.",
			},
			[]string{"message_type", "subscription_type", "result"},
		),
		telegramWebhook: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_webhook_updates_total",
				Help: "Telegram webhook update handling results.",
			},
			[]string{"result"},
		),
		backgroundJobsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_background_jobs_total",
				Help: "Background jobs execution count.",
			},
			[]string{"job", "result"},
		),
		backgroundJobTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_background_job_duration_seconds",
				Help:    "Background job duration in seconds.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 180},
			},
			[]string{"job"},
		),
	}

	m.registry.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestsInFlight,
		m.oauthCallbacksTotal,
		m.eventsubTotal,
		m.telegramWebhook,
		m.backgroundJobsTotal,
		m.backgroundJobTime,
	)

	return m
}

// Handler returns an HTTP handler that serves Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// OAuthCallback records an OAuth callback by mode and result.
func (m *Metrics) OAuthCallback(mode, result string) {
	if m == nil {
		return
	}
	m.oauthCallbacksTotal.WithLabelValues(httputil.LabelOrUnknown(mode), httputil.LabelOrUnknown(result)).Inc()
}

// EventSubMessage records an EventSub webhook message.
func (m *Metrics) EventSubMessage(messageType, subscriptionType, result string) {
	if m == nil {
		return
	}
	m.eventsubTotal.WithLabelValues(httputil.LabelOrUnknown(messageType), httputil.LabelOrUnknown(subscriptionType), httputil.LabelOrUnknown(result)).Inc()
}

// TelegramWebhookResult records a Telegram webhook handling result.
func (m *Metrics) TelegramWebhookResult(result string) {
	if m == nil {
		return
	}
	m.telegramWebhook.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// BackgroundJob records a background job execution.
func (m *Metrics) BackgroundJob(job, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.backgroundJobsTotal.WithLabelValues(httputil.LabelOrUnknown(job), httputil.LabelOrUnknown(result)).Inc()
	m.backgroundJobTime.WithLabelValues(httputil.LabelOrUnknown(job)).Observe(d.Seconds())
}

// Middleware returns HTTP middleware that records request metrics and
// logs each request. QuietRoutes lists route patterns that should be
// logged at Debug level instead of Info. If logger is nil, slog.Default()
// is used. If next is nil, http.NotFoundHandler() is used.
func (m *Metrics) Middleware(logger *slog.Logger, quietRoutes []string, next http.Handler) http.Handler {
	if m == nil {
		if next == nil {
			return http.NotFoundHandler()
		}
		return next
	}
	if logger == nil {
		logger = slog.Default()
	}
	if next == nil {
		next = http.NotFoundHandler()
	}
	quiet := make(map[string]bool, len(quietRoutes))
	for _, r := range quietRoutes {
		quiet[r] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.requestsInFlight.Inc()
		defer m.requestsInFlight.Dec()

		rid := httputil.RequestIDFromContext(r.Context())
		rec := &httputil.StatusRecorder{ResponseWriter: w, Status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := httputil.RouteLabel(r)
		method := strings.ToUpper(strings.TrimSpace(r.Method))
		if method == "" {
			method = "UNKNOWN"
		}
		status := strconv.Itoa(rec.Status)
		duration := time.Since(start)
		m.requestsTotal.WithLabelValues(method, route, status).Inc()
		m.requestDuration.WithLabelValues(method, route).Observe(duration.Seconds())

		level := slog.LevelInfo
		if quiet[route] {
			level = slog.LevelDebug
		}
		logCtx := context.WithoutCancel(r.Context())
		logger.Log(logCtx, level, "http request",
			"request_id", rid,
			"method", method,
			"route", route,
			"path", r.URL.Path,
			"status", rec.Status,
			"duration_ms", duration.Milliseconds(),
			"client_ip", httputil.ClientIP(r),
			"bytes", rec.Bytes,
		)
	})
}

// Registry returns the underlying Prometheus registry for testing.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}
