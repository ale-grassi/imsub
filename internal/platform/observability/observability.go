package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"imsub/internal/events"
	"imsub/internal/platform/httputil"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus collectors used by the application.
type Metrics struct {
	registry             *prometheus.Registry
	requestsTotal        *prometheus.CounterVec
	requestDuration      *prometheus.HistogramVec
	requestsInFlight     prometheus.Gauge
	telegramDailyActive  prometheus.Gauge
	linkedViewers        prometheus.Gauge
	linkedCreators       prometheus.Gauge
	managedGroups        prometheus.Gauge
	oauthCallbacksTotal  *prometheus.CounterVec
	eventsubTotal        *prometheus.CounterVec
	telegramWebhook      *prometheus.CounterVec
	backgroundJobsTotal  *prometheus.CounterVec
	backgroundJobTime    *prometheus.HistogramVec
	creatorTokenRefresh  *prometheus.CounterVec
	creatorBlocklistSync *prometheus.CounterVec
	creatorBlockEnforce  *prometheus.CounterVec
	creatorAuthChange    *prometheus.CounterVec
	creatorsReconnect    prometheus.Gauge
	creatorReconnectDM   *prometheus.CounterVec
	resetExecutions      *prometheus.CounterVec
	resetGroupTargets    *prometheus.CounterVec
	groupRegistrations   *prometheus.CounterVec
	groupUnregistrations *prometheus.CounterVec
	creatorActivation    *prometheus.CounterVec
	subscriptionEnd      *prometheus.CounterVec
	reconcileRepairs     *prometheus.CounterVec
	viewerOAuth          *prometheus.CounterVec
	creatorOAuth         *prometheus.CounterVec
	creatorStatus        *prometheus.CounterVec
	viewerAccess         *prometheus.CounterVec
	viewerJoinTargets    *prometheus.CounterVec
	viewerInviteLinks    *prometheus.CounterVec
	telegramCommands     *prometheus.CounterVec
	telegramKickActions  *prometheus.CounterVec
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
		telegramDailyActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_telegram_daily_active_users",
			Help: "Rolling 24h unique Telegram users who actively interacted with the bot.",
		}),
		linkedViewers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_linked_viewer_accounts",
			Help: "Current count of linked viewer identities.",
		}),
		linkedCreators: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_linked_creator_accounts",
			Help: "Current count of linked creator accounts.",
		}),
		managedGroups: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_managed_groups",
			Help: "Current count of managed Telegram groups.",
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
		creatorTokenRefresh: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_token_refresh_total",
				Help: "Creator token refresh attempts by result.",
			},
			[]string{"result"},
		),
		creatorBlocklistSync: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_blocklist_sync_total",
				Help: "Creator blocklist sync item counts by result.",
			},
			[]string{"result"},
		),
		creatorBlockEnforce: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_blocklist_enforcement_total",
				Help: "Creator blocklist enforcement actions by result.",
			},
			[]string{"result"},
		),
		creatorAuthChange: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_auth_state_transitions_total",
				Help: "Creator auth state transitions by source and destination.",
			},
			[]string{"from", "to", "reason"},
		),
		creatorsReconnect: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_creators_reconnect_required",
			Help: "Current number of creators marked as reconnect required.",
		}),
		creatorReconnectDM: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_reconnect_notifications_total",
				Help: "Creator reconnect-required DM notification attempts by result.",
			},
			[]string{"result"},
		),
		resetExecutions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_reset_executions_total",
				Help: "Reset executions by scope and result.",
			},
			[]string{"scope", "result"},
		),
		resetGroupTargets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_reset_group_targets_total",
				Help: "Viewer reset group target counts by source.",
			},
			[]string{"source"},
		),
		groupRegistrations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_group_registrations_total",
				Help: "Group registration attempts by outcome.",
			},
			[]string{"outcome"},
		),
		groupUnregistrations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_group_unregistrations_total",
				Help: "Group unregistration attempts by outcome.",
			},
			[]string{"outcome"},
		),
		creatorActivation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_activation_total",
				Help: "Creator activation workflow results.",
			},
			[]string{"result"},
		),
		subscriptionEnd: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_subscription_end_total",
				Help: "Subscription-end workflow results.",
			},
			[]string{"result"},
		),
		reconcileRepairs: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_reconciliation_repairs_total",
				Help: "Reconciliation repair counts by repair type and outcome.",
			},
			[]string{"repair", "outcome"},
		),
		viewerOAuth: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_viewer_oauth_total",
				Help: "Viewer OAuth completion results.",
			},
			[]string{"result"},
		),
		creatorOAuth: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_oauth_total",
				Help: "Creator OAuth completion results.",
			},
			[]string{"result"},
		),
		creatorStatus: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_status_total",
				Help: "Creator status workflow results.",
			},
			[]string{"result"},
		),
		viewerAccess: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_viewer_access_total",
				Help: "Viewer access workflow results.",
			},
			[]string{"result"},
		),
		viewerJoinTargets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_viewer_join_targets_total",
				Help: "Viewer join-target counts by kind.",
			},
			[]string{"kind"},
		),
		viewerInviteLinks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_viewer_invite_links_total",
				Help: "Viewer invite-link creation attempts by result.",
			},
			[]string{"result"},
		),
		telegramCommands: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_commands_total",
				Help: "Telegram slash command usage by command and chat type.",
			},
			[]string{"command", "chat_type"},
		),
		telegramKickActions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_kick_actions_total",
				Help: "Telegram member removal attempts by reason and result.",
			},
			[]string{"reason", "result"},
		),
	}

	m.registry.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestsInFlight,
		m.telegramDailyActive,
		m.linkedViewers,
		m.linkedCreators,
		m.managedGroups,
		m.oauthCallbacksTotal,
		m.eventsubTotal,
		m.telegramWebhook,
		m.backgroundJobsTotal,
		m.backgroundJobTime,
		m.creatorTokenRefresh,
		m.creatorBlocklistSync,
		m.creatorBlockEnforce,
		m.creatorAuthChange,
		m.creatorsReconnect,
		m.creatorReconnectDM,
		m.resetExecutions,
		m.resetGroupTargets,
		m.groupRegistrations,
		m.groupUnregistrations,
		m.creatorActivation,
		m.subscriptionEnd,
		m.reconcileRepairs,
		m.viewerOAuth,
		m.creatorOAuth,
		m.creatorStatus,
		m.viewerAccess,
		m.viewerJoinTargets,
		m.viewerInviteLinks,
		m.telegramCommands,
		m.telegramKickActions,
	)

	return m
}

// TelegramDailyActiveUsers sets the rolling 24h unique Telegram active-user gauge.
func (m *Metrics) TelegramDailyActiveUsers(count int) {
	if m == nil {
		return
	}
	m.telegramDailyActive.Set(float64(count))
}

// LinkedViewerAccounts sets the current linked-viewer gauge.
func (m *Metrics) LinkedViewerAccounts(count int) {
	if m == nil {
		return
	}
	m.linkedViewers.Set(float64(count))
}

// LinkedCreatorAccounts sets the current linked-creator gauge.
func (m *Metrics) LinkedCreatorAccounts(count int) {
	if m == nil {
		return
	}
	m.linkedCreators.Set(float64(count))
}

// ManagedGroups sets the current managed-groups gauge.
func (m *Metrics) ManagedGroups(count int) {
	if m == nil {
		return
	}
	m.managedGroups.Set(float64(count))
}

// CreatorTokenRefresh records creator token refresh attempts.
func (m *Metrics) CreatorTokenRefresh(result string) {
	if m == nil {
		return
	}
	m.creatorTokenRefresh.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// CreatorBlocklistSync records creator blocklist sync counts by result.
func (m *Metrics) CreatorBlocklistSync(result string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.creatorBlocklistSync.WithLabelValues(httputil.LabelOrUnknown(result)).Add(float64(count))
}

// CreatorBlocklistEnforcement records creator blocklist enforcement actions by result.
func (m *Metrics) CreatorBlocklistEnforcement(result string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.creatorBlockEnforce.WithLabelValues(httputil.LabelOrUnknown(result)).Add(float64(count))
}

// CreatorAuthTransition records a creator auth state transition.
func (m *Metrics) CreatorAuthTransition(from, to, reason string) {
	if m == nil {
		return
	}
	m.creatorAuthChange.WithLabelValues(
		httputil.LabelOrUnknown(from),
		httputil.LabelOrUnknown(to),
		httputil.LabelOrUnknown(reason),
	).Inc()
}

// CreatorsReconnectRequired sets the current reconnect-required creator gauge.
func (m *Metrics) CreatorsReconnectRequired(count int) {
	if m == nil {
		return
	}
	m.creatorsReconnect.Set(float64(count))
}

// CreatorReconnectNotification records reconnect-required owner notifications.
func (m *Metrics) CreatorReconnectNotification(result string) {
	if m == nil {
		return
	}
	m.creatorReconnectDM.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// ResetExecution records reset executions by scope and result.
func (m *Metrics) ResetExecution(scope, result string) {
	if m == nil {
		return
	}
	m.resetExecutions.WithLabelValues(httputil.LabelOrUnknown(scope), httputil.LabelOrUnknown(result)).Inc()
}

// ResetGroupTargets records viewer reset target groups by source.
func (m *Metrics) ResetGroupTargets(source string, groups int) {
	if m == nil || groups <= 0 {
		return
	}
	m.resetGroupTargets.WithLabelValues(httputil.LabelOrUnknown(source)).Add(float64(groups))
}

// GroupRegistration records a group registration attempt by outcome.
func (m *Metrics) GroupRegistration(outcome string) {
	if m == nil {
		return
	}
	m.groupRegistrations.WithLabelValues(httputil.LabelOrUnknown(outcome)).Inc()
}

// GroupUnregistration records a group unregistration attempt by outcome.
func (m *Metrics) GroupUnregistration(outcome string) {
	if m == nil {
		return
	}
	m.groupUnregistrations.WithLabelValues(httputil.LabelOrUnknown(outcome)).Inc()
}

// CreatorActivation records creator activation outcomes.
func (m *Metrics) CreatorActivation(result string) {
	if m == nil {
		return
	}
	m.creatorActivation.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// SubscriptionEnd records subscription-end workflow outcomes.
func (m *Metrics) SubscriptionEnd(result string) {
	if m == nil {
		return
	}
	m.subscriptionEnd.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// ReconciliationRepair records reconciliation repair counts by type and outcome.
func (m *Metrics) ReconciliationRepair(repair, outcome string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.reconcileRepairs.WithLabelValues(httputil.LabelOrUnknown(repair), httputil.LabelOrUnknown(outcome)).Add(float64(count))
}

// ViewerOAuth records viewer OAuth completion results.
func (m *Metrics) ViewerOAuth(result string) {
	if m == nil {
		return
	}
	m.viewerOAuth.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// CreatorOAuth records creator OAuth completion results.
func (m *Metrics) CreatorOAuth(result string) {
	if m == nil {
		return
	}
	m.creatorOAuth.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// CreatorStatus records creator status workflow results.
func (m *Metrics) CreatorStatus(result string) {
	if m == nil {
		return
	}
	m.creatorStatus.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// ViewerAccess records linked-viewer workflow results.
func (m *Metrics) ViewerAccess(result string) {
	if m == nil {
		return
	}
	m.viewerAccess.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// ViewerJoinTargets records viewer join-target counts by kind.
func (m *Metrics) ViewerJoinTargets(kind string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.viewerJoinTargets.WithLabelValues(httputil.LabelOrUnknown(kind)).Add(float64(count))
}

// ViewerInviteLink records viewer invite-link creation attempts.
func (m *Metrics) ViewerInviteLink(result string) {
	if m == nil {
		return
	}
	m.viewerInviteLinks.WithLabelValues(httputil.LabelOrUnknown(result)).Inc()
}

// TelegramCommand records a slash command invocation.
func (m *Metrics) TelegramCommand(command, chatType string) {
	if m == nil {
		return
	}
	m.telegramCommands.WithLabelValues(
		httputil.LabelOrUnknown(command),
		httputil.LabelOrUnknown(chatType),
	).Inc()
}

// TelegramKickAction records a Telegram kick action by reason and result.
func (m *Metrics) TelegramKickAction(reason, result string) {
	if m == nil {
		return
	}
	m.telegramKickActions.WithLabelValues(
		httputil.LabelOrUnknown(reason),
		httputil.LabelOrUnknown(result),
	).Inc()
}

// Emit projects application events into observability metrics.
func (m *Metrics) Emit(_ context.Context, evt events.Event) {
	if m == nil {
		return
	}
	switch evt.Name {
	case events.NameResetExecuted:
		m.ResetExecution(evt.Fields["scope"], evt.Outcome)
	case events.NameResetGroupTarget:
		m.ResetGroupTargets(evt.Fields["source"], evt.Count)
	case events.NameGroupRegistration:
		m.GroupRegistration(evt.Outcome)
	case events.NameGroupUnregistration:
		m.GroupUnregistration(evt.Outcome)
	case events.NameCreatorActivation:
		m.CreatorActivation(evt.Outcome)
	case events.NameSubscriptionEnd:
		m.SubscriptionEnd(evt.Outcome)
	case events.NameViewerOAuth:
		m.ViewerOAuth(evt.Outcome)
	case events.NameViewerJoinTarget:
		m.ViewerJoinTargets(evt.Fields["kind"], evt.Count)
	case events.NameViewerInviteLink:
		m.ViewerInviteLink(evt.Outcome)
	case events.NameCreatorTokenRefresh:
		m.CreatorTokenRefresh(evt.Outcome)
	case events.NameCreatorBlocklistSync:
		m.CreatorBlocklistSync(evt.Outcome, evt.Count)
	case events.NameCreatorBlocklistEnforcement:
		m.CreatorBlocklistEnforcement(evt.Outcome, evt.Count)
	case events.NameCreatorAuthTransition:
		m.CreatorAuthTransition(evt.Fields["from"], evt.Fields["to"], evt.Fields["reason"])
	case events.NameCreatorsReconnectRequired:
		m.CreatorsReconnectRequired(evt.Count)
	case events.NameCreatorReconnectNotice:
		m.CreatorReconnectNotification(evt.Outcome)
	case events.NameBackgroundJob:
		m.BackgroundJob(evt.Fields["job"], evt.Outcome, evt.Duration)
	case events.NameReconciliationRepair:
		m.ReconciliationRepair(evt.Fields["repair"], evt.Outcome, evt.Count)
	case events.NameOAuthCallback:
		m.OAuthCallback(evt.Fields["mode"], evt.Outcome)
	case events.NameEventSubMessage:
		m.EventSubMessage(evt.Fields["message_type"], evt.Fields["subscription_type"], evt.Outcome)
	case events.NameTelegramWebhook:
		m.TelegramWebhookResult(evt.Outcome)
	case events.NameCreatorOAuth:
		m.CreatorOAuth(evt.Outcome)
	case events.NameCreatorStatus:
		m.CreatorStatus(evt.Outcome)
	case events.NameViewerAccess:
		m.ViewerAccess(evt.Outcome)
	case events.NameTelegramCommand:
		m.TelegramCommand(evt.Fields["command"], evt.Fields["chat_type"])
	case events.NameTelegramKickAction:
		m.TelegramKickAction(evt.Fields["reason"], evt.Outcome)
	}
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
