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

const (
	twitchEventSubResultOK     = "ok"
	twitchEventSubResultFailed = "failed"
	viewerInviteReasonNone     = "none"
)

// Metrics holds all Prometheus collectors used by the application.
type Metrics struct {
	registry                *prometheus.Registry
	requestsTotal           *prometheus.CounterVec
	requestDuration         *prometheus.HistogramVec
	requestsInFlight        prometheus.Gauge
	telegramDailyActive     prometheus.Gauge
	linkedViewers           prometheus.Gauge
	linkedCreators          prometheus.Gauge
	managedGroups           prometheus.Gauge
	creatorInfo             *prometheus.GaugeVec
	creatorManagedGroups    *prometheus.GaugeVec
	creatorGroupPolicy      *prometheus.GaugeVec
	creatorGroupLanguage    *prometheus.GaugeVec
	creatorGraceSetting     *prometheus.GaugeVec
	creatorBanSyncEnabled   *prometheus.GaugeVec
	creatorLastBanSyncAt    *prometheus.GaugeVec
	creatorSubscribers      *prometheus.GaugeVec
	creatorBlockedUsers     *prometheus.GaugeVec
	creatorTracked          *prometheus.GaugeVec
	creatorUntracked        *prometheus.GaugeVec
	creatorReconnectReq     *prometheus.GaugeVec
	oauthStartsTotal        *prometheus.CounterVec
	oauthCallbacksTotal     *prometheus.CounterVec
	eventsubTotal           *prometheus.CounterVec
	twitchEventSubTotal     *prometheus.CounterVec
	telegramWebhook         *prometheus.CounterVec
	backgroundJobsTotal     *prometheus.CounterVec
	backgroundJobTime       *prometheus.HistogramVec
	backgroundJobState      *prometheus.GaugeVec
	backgroundJobItems      *prometheus.CounterVec
	backgroundJobInterval   *prometheus.GaugeVec
	backgroundJobTimeout    *prometheus.GaugeVec
	backgroundJobLastStart  *prometheus.GaugeVec
	backgroundJobLastFinish *prometheus.GaugeVec
	redisBackupRuns         *prometheus.CounterVec
	redisBackupTime         *prometheus.HistogramVec
	redisBackupKeys         prometheus.Gauge
	redisBackupBytes        prometheus.Gauge
	redisCommands           *prometheus.CounterVec
	creatorTokenRefresh     *prometheus.CounterVec
	creatorBlocklistSync    *prometheus.CounterVec
	creatorBlockEnforce     *prometheus.CounterVec
	creatorAuthChange       *prometheus.CounterVec
	creatorsReconnect       prometheus.Gauge
	creatorReconnectDM      *prometheus.CounterVec
	resetExecutions         *prometheus.CounterVec
	resetGroupTargets       *prometheus.CounterVec
	groupRegistrations      *prometheus.CounterVec
	groupUnregistrations    *prometheus.CounterVec
	creatorActivation       *prometheus.CounterVec
	subscriptionEnd         *prometheus.CounterVec
	reconcileRepairs        *prometheus.CounterVec
	viewerOAuth             *prometheus.CounterVec
	creatorOAuth            *prometheus.CounterVec
	creatorStatus           *prometheus.CounterVec
	viewerAccess            *prometheus.CounterVec
	viewerJoinTargets       *prometheus.CounterVec
	viewerInviteLinks       *prometheus.CounterVec
	telegramCommands        *prometheus.CounterVec
	telegramCommandTime     *prometheus.HistogramVec
	telegramCallbacks       *prometheus.CounterVec
	telegramCallbackTime    *prometheus.HistogramVec
	telegramAPIErrors       *prometheus.CounterVec
	telegramKickActions     *prometheus.CounterVec
	telegramMTProtoBoot     *prometheus.CounterVec
	startupPhaseDuration    *prometheus.HistogramVec
	startupTotalDuration    *prometheus.HistogramVec
	startupReady            prometheus.Gauge
	startupReadyAt          prometheus.Gauge
	startupPhaseLast        *prometheus.GaugeVec
	startupTotalLast        *prometheus.GaugeVec
	startupCount            *prometheus.CounterVec
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
		creatorInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_info",
				Help: "Creator identity metadata for dashboard selection.",
			},
			[]string{"creator_id", "display_name", "login"},
		),
		creatorManagedGroups: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_managed_groups",
				Help: "Current managed Telegram groups per creator.",
			},
			[]string{"creator_id"},
		),
		creatorGroupPolicy: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_group_policy_groups",
				Help: "Current managed-group count by creator and policy.",
			},
			[]string{"creator_id", "policy"},
		),
		creatorGroupLanguage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_group_language_groups",
				Help: "Current managed-group count by creator and language.",
			},
			[]string{"creator_id", "language"},
		),
		creatorGraceSetting: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_subscription_end_grace",
				Help: "Current creator subscription-end grace setting.",
			},
			[]string{"creator_id", "grace"},
		),
		creatorBanSyncEnabled: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_blocklist_sync_enabled",
				Help: "Whether creator Twitch blocklist sync is enabled.",
			},
			[]string{"creator_id"},
		),
		creatorLastBanSyncAt: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_last_ban_sync_timestamp_seconds",
				Help: "Unix timestamp in seconds of the creator's last successful Twitch ban sync.",
			},
			[]string{"creator_id"},
		),
		creatorSubscribers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_subscribers",
				Help: "Current subscriber count per creator.",
			},
			[]string{"creator_id"},
		),
		creatorBlockedUsers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_blocked_users",
				Help: "Current blocked-user count per creator.",
			},
			[]string{"creator_id"},
		),
		creatorTracked: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_tracked_members",
				Help: "Current tracked Telegram group members summed across a creator's groups.",
			},
			[]string{"creator_id"},
		),
		creatorUntracked: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_untracked_members",
				Help: "Current untracked Telegram group members summed across a creator's groups.",
			},
			[]string{"creator_id"},
		),
		creatorReconnectReq: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_creator_reconnect_required",
				Help: "Whether a creator currently requires reconnect.",
			},
			[]string{"creator_id"},
		),
		oauthStartsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_oauth_starts_total",
				Help: "OAuth start page requests by mode and result.",
			},
			[]string{"mode", "result"},
		),
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
		twitchEventSubTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_twitch_eventsub_notifications_total",
				Help: "Creator-scoped Twitch EventSub notification processing results.",
			},
			[]string{"creator_id", "subscription_type", "result"},
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
		backgroundJobState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_background_job_state",
				Help: "Last known state of a background job: 1=running, 2=ok, 3=failed, 4=partial_failure, 5=timeout.",
			},
			[]string{"job"},
		),
		backgroundJobItems: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_background_job_items_total",
				Help: "Per-run item counts reported by background jobs (e.g. kicked, repaired, processed).",
			},
			[]string{"job", "kind", "result"},
		),
		backgroundJobInterval: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_background_job_schedule_interval_seconds",
				Help: "Configured interval in seconds for a scheduled background job.",
			},
			[]string{"job"},
		),
		backgroundJobTimeout: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_background_job_schedule_timeout_seconds",
				Help: "Configured timeout in seconds for a scheduled background job.",
			},
			[]string{"job"},
		),
		backgroundJobLastStart: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_background_job_last_start_timestamp_seconds",
				Help: "Unix timestamp in seconds of the most recent observed background job start.",
			},
			[]string{"job"},
		),
		backgroundJobLastFinish: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "imsub_background_job_last_finish_timestamp_seconds",
				Help: "Unix timestamp in seconds of the most recent observed background job completion.",
			},
			[]string{"job"},
		),
		redisBackupRuns: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_redis_backup_runs_total",
				Help: "Redis backup runs by result.",
			},
			[]string{"result"},
		),
		redisBackupTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_redis_backup_duration_seconds",
				Help:    "Redis backup end-to-end duration in seconds by result.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 180, 300},
			},
			[]string{"result"},
		),
		redisBackupKeys: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_redis_backup_exported_keys",
			Help: "Key count from the most recent successful Redis backup snapshot.",
		}),
		redisBackupBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_redis_backup_uploaded_bytes",
			Help: "Uploaded object size in bytes from the most recent successful Redis backup snapshot.",
		}),
		redisCommands: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_redis_commands_total",
				Help: "Redis commands issued by background job, command name, and result.",
			},
			[]string{"job", "command", "result"},
		),
		creatorTokenRefresh: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_token_refresh_total",
				Help: "Creator token refresh attempts by result.",
			},
			[]string{"creator_id", "result"},
		),
		creatorBlocklistSync: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_blocklist_sync_total",
				Help: "Creator blocklist sync item counts by result.",
			},
			[]string{"creator_id", "result"},
		),
		creatorBlockEnforce: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_blocklist_enforcement_total",
				Help: "Creator blocklist enforcement actions by result.",
			},
			[]string{"creator_id", "result"},
		),
		creatorAuthChange: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_auth_state_transitions_total",
				Help: "Creator auth state transitions by source and destination.",
			},
			[]string{"creator_id", "from", "to", "reason"},
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
			[]string{"creator_id", "result"},
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
			[]string{"creator_id", "result"},
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
			[]string{"creator_id", "result"},
		),
		creatorStatus: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_creator_status_total",
				Help: "Creator status workflow results.",
			},
			[]string{"creator_id", "result"},
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
				Help: "Viewer invite-link creation attempts by result and normalized reason.",
			},
			[]string{"result", "reason"},
		),
		telegramCommands: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_commands_total",
				Help: "Telegram slash command usage by command and chat type.",
			},
			[]string{"command", "chat_type"},
		),
		telegramCommandTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_telegram_command_response_duration_seconds",
				Help:    "Time from receiving a Telegram slash command to the first successful bot response, or command termination without a reply.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30},
			},
			[]string{"command", "chat_type", "result"},
		),
		telegramCallbacks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_callbacks_total",
				Help: "Telegram callback query usage by domain and verb.",
			},
			[]string{"domain", "verb"},
		),
		telegramCallbackTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_telegram_callback_response_duration_seconds",
				Help:    "Time from receiving a Telegram callback query to the first completed callback response path.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30},
			},
			[]string{"domain", "verb", "result"},
		),
		telegramAPIErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_api_errors_total",
				Help: "Telegram API call failures by method and normalized reason.",
			},
			[]string{"method", "reason"},
		),
		telegramKickActions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_kick_actions_total",
				Help: "Telegram member removal attempts by reason and result.",
			},
			[]string{"reason", "result"},
		),
		telegramMTProtoBoot: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "imsub_telegram_mtproto_bootstrap_total",
				Help: "Initial MTProto bootstrap sync attempts by outcome.",
			},
			[]string{"outcome"},
		),
		startupPhaseDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_startup_phase_duration_seconds",
				Help:    "Duration of each synchronous startup phase in seconds.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 7.5, 10, 15},
			},
			[]string{"phase", "result"},
		),
		startupTotalDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "imsub_startup_total_duration_seconds",
				Help:    "End-to-end startup duration in seconds from process boot to readiness.",
				Buckets: []float64{0.5, 1, 2, 3, 4, 5, 6, 7.5, 10, 12.5, 15, 20, 30},
			},
			[]string{"result"},
		),
		startupReady: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_startup_ready",
			Help: "Whether the app has completed startup and is ready to serve (0/1).",
		}),
		startupReadyAt: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "imsub_startup_ready_timestamp_seconds",
			Help: "Unix timestamp of the moment startup completed (0 until ready).",
		}),
		startupPhaseLast: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imsub_startup_last_phase_duration_seconds",
			Help: "Last observed duration for each startup phase, retained until the next run.",
		}, []string{"phase", "result"}),
		startupTotalLast: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "imsub_startup_last_total_duration_seconds",
			Help: "Last observed end-to-end startup duration, retained until the next run.",
		}, []string{"result"}),
		startupCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "imsub_startup_count_total",
			Help: "Total number of startup attempts by result. Counts every process boot that reaches the readiness step.",
		}, []string{"result"}),
	}

	m.registry.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestsInFlight,
		m.telegramDailyActive,
		m.linkedViewers,
		m.linkedCreators,
		m.managedGroups,
		m.creatorInfo,
		m.creatorManagedGroups,
		m.creatorGroupPolicy,
		m.creatorGroupLanguage,
		m.creatorGraceSetting,
		m.creatorBanSyncEnabled,
		m.creatorLastBanSyncAt,
		m.creatorSubscribers,
		m.creatorBlockedUsers,
		m.creatorTracked,
		m.creatorUntracked,
		m.creatorReconnectReq,
		m.oauthStartsTotal,
		m.oauthCallbacksTotal,
		m.eventsubTotal,
		m.twitchEventSubTotal,
		m.telegramWebhook,
		m.backgroundJobsTotal,
		m.backgroundJobTime,
		m.backgroundJobState,
		m.backgroundJobItems,
		m.backgroundJobInterval,
		m.backgroundJobTimeout,
		m.backgroundJobLastStart,
		m.backgroundJobLastFinish,
		m.redisBackupRuns,
		m.redisBackupTime,
		m.redisBackupKeys,
		m.redisBackupBytes,
		m.redisCommands,
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
		m.telegramCommandTime,
		m.telegramCallbacks,
		m.telegramCallbackTime,
		m.telegramAPIErrors,
		m.telegramKickActions,
		m.telegramMTProtoBoot,
		m.startupPhaseDuration,
		m.startupTotalDuration,
		m.startupReady,
		m.startupReadyAt,
		m.startupPhaseLast,
		m.startupTotalLast,
		m.startupCount,
	)

	return m
}

// ResetCreatorSnapshotMetrics clears creator-scoped snapshot gauges before repopulation.
func (m *Metrics) ResetCreatorSnapshotMetrics() {
	if m == nil {
		return
	}
	m.creatorInfo.Reset()
	m.creatorManagedGroups.Reset()
	m.creatorGroupPolicy.Reset()
	m.creatorGroupLanguage.Reset()
	m.creatorGraceSetting.Reset()
	m.creatorBanSyncEnabled.Reset()
	m.creatorLastBanSyncAt.Reset()
	m.creatorSubscribers.Reset()
	m.creatorBlockedUsers.Reset()
	m.creatorTracked.Reset()
	m.creatorUntracked.Reset()
	m.creatorReconnectReq.Reset()
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

// CreatorInfo records creator identity metadata for dashboard selection.
func (m *Metrics) CreatorInfo(creatorID, displayName, login string) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorInfo.WithLabelValues(creatorID, httputil.LabelOrUnknown(displayName), httputil.LabelOrUnknown(login)).Set(1)
}

// CreatorManagedGroups sets the current managed-group count for a creator.
func (m *Metrics) CreatorManagedGroups(creatorID string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorManagedGroups.WithLabelValues(creatorID).Set(float64(count))
}

// CreatorGroupPolicyCount sets the current managed-group count by policy for a creator.
func (m *Metrics) CreatorGroupPolicyCount(creatorID, policy string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorGroupPolicy.WithLabelValues(creatorID, httputil.LabelOrUnknown(policy)).Set(float64(count))
}

// CreatorGroupLanguageCount sets the current managed-group count by language for a creator.
func (m *Metrics) CreatorGroupLanguageCount(creatorID, language string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorGroupLanguage.WithLabelValues(creatorID, httputil.LabelOrUnknown(language)).Set(float64(count))
}

// CreatorSubscriptionEndGrace sets the current subscription-end grace setting for a creator.
func (m *Metrics) CreatorSubscriptionEndGrace(creatorID, grace string) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorGraceSetting.WithLabelValues(creatorID, httputil.LabelOrUnknown(grace)).Set(1)
}

// CreatorBanSyncEnabled sets whether creator blocklist sync is enabled.
func (m *Metrics) CreatorBanSyncEnabled(creatorID string, enabled bool) {
	if m == nil || creatorID == "" {
		return
	}
	value := 0.0
	if enabled {
		value = 1
	}
	m.creatorBanSyncEnabled.WithLabelValues(creatorID).Set(value)
}

// CreatorLastBanSyncAt records the last successful creator ban sync timestamp.
func (m *Metrics) CreatorLastBanSyncAt(creatorID string, at time.Time) {
	if m == nil || creatorID == "" || at.IsZero() {
		return
	}
	m.creatorLastBanSyncAt.WithLabelValues(creatorID).Set(float64(at.Unix()))
}

// CreatorSubscribers sets the current subscriber count for a creator.
func (m *Metrics) CreatorSubscribers(creatorID string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorSubscribers.WithLabelValues(creatorID).Set(float64(count))
}

// CreatorBlockedUsers sets the current blocked-user count for a creator.
func (m *Metrics) CreatorBlockedUsers(creatorID string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorBlockedUsers.WithLabelValues(creatorID).Set(float64(count))
}

// CreatorTrackedMembers sets the current tracked-member count for a creator.
func (m *Metrics) CreatorTrackedMembers(creatorID string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorTracked.WithLabelValues(creatorID).Set(float64(count))
}

// CreatorUntrackedMembers sets the current untracked-member count for a creator.
func (m *Metrics) CreatorUntrackedMembers(creatorID string, count int) {
	if m == nil || creatorID == "" {
		return
	}
	m.creatorUntracked.WithLabelValues(creatorID).Set(float64(count))
}

// CreatorReconnectRequired sets whether a creator currently requires reconnect.
func (m *Metrics) CreatorReconnectRequired(creatorID string, required bool) {
	if m == nil || creatorID == "" {
		return
	}
	value := 0.0
	if required {
		value = 1
	}
	m.creatorReconnectReq.WithLabelValues(creatorID).Set(value)
}

// CreatorTokenRefresh records creator token refresh attempts.
func (m *Metrics) CreatorTokenRefresh(creatorID, result string) {
	if m == nil {
		return
	}
	m.creatorTokenRefresh.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Inc()
}

// CreatorBlocklistSync records creator blocklist sync counts by result.
func (m *Metrics) CreatorBlocklistSync(creatorID, result string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.creatorBlocklistSync.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Add(float64(count))
}

// CreatorBlocklistEnforcement records creator blocklist enforcement actions by result.
func (m *Metrics) CreatorBlocklistEnforcement(creatorID, result string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.creatorBlockEnforce.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Add(float64(count))
}

// CreatorAuthTransition records a creator auth state transition.
func (m *Metrics) CreatorAuthTransition(creatorID, from, to, reason string) {
	if m == nil {
		return
	}
	m.creatorAuthChange.WithLabelValues(
		creatorID,
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
func (m *Metrics) CreatorReconnectNotification(creatorID, result string) {
	if m == nil {
		return
	}
	m.creatorReconnectDM.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Inc()
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
func (m *Metrics) CreatorActivation(creatorID, result string) {
	if m == nil {
		return
	}
	m.creatorActivation.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Inc()
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
func (m *Metrics) CreatorOAuth(creatorID, result string) {
	if m == nil {
		return
	}
	m.creatorOAuth.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Inc()
}

// CreatorStatus records creator status workflow results.
func (m *Metrics) CreatorStatus(creatorID, result string) {
	if m == nil {
		return
	}
	m.creatorStatus.WithLabelValues(creatorID, httputil.LabelOrUnknown(result)).Inc()
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
func (m *Metrics) ViewerInviteLink(result, reason string) {
	if m == nil {
		return
	}
	if httputil.LabelOrUnknown(result) == "ok" && strings.TrimSpace(reason) == "" {
		reason = viewerInviteReasonNone
	}
	m.viewerInviteLinks.WithLabelValues(httputil.LabelOrUnknown(result), httputil.LabelOrUnknown(reason)).Inc()
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

// TelegramCommandResponse records latency from command receipt to its first successful response.
func (m *Metrics) TelegramCommandResponse(command, chatType, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.telegramCommandTime.WithLabelValues(
		httputil.LabelOrUnknown(command),
		httputil.LabelOrUnknown(chatType),
		httputil.LabelOrUnknown(result),
	).Observe(d.Seconds())
}

// TelegramCallback records a callback query invocation.
func (m *Metrics) TelegramCallback(domain, verb string) {
	if m == nil {
		return
	}
	m.telegramCallbacks.WithLabelValues(
		httputil.LabelOrUnknown(domain),
		httputil.LabelOrUnknown(verb),
	).Inc()
}

// TelegramCallbackResponse records callback handling latency.
func (m *Metrics) TelegramCallbackResponse(domain, verb, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.telegramCallbackTime.WithLabelValues(
		httputil.LabelOrUnknown(domain),
		httputil.LabelOrUnknown(verb),
		httputil.LabelOrUnknown(result),
	).Observe(d.Seconds())
}

// TelegramAPIError records a Telegram API failure by method and normalized reason.
func (m *Metrics) TelegramAPIError(method, reason string) {
	if m == nil {
		return
	}
	m.telegramAPIErrors.WithLabelValues(
		httputil.LabelOrUnknown(method),
		httputil.LabelOrUnknown(reason),
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

// StartupPhase records the duration and result of a synchronous startup phase.
// It updates the histogram for long-term percentile analysis and a sticky gauge
// with the last observed value for point-in-time inspection.
func (m *Metrics) StartupPhase(phase, result string, d time.Duration) {
	if m == nil {
		return
	}
	phaseLabel := httputil.LabelOrUnknown(phase)
	resultLabel := httputil.LabelOrUnknown(result)
	m.startupPhaseDuration.WithLabelValues(phaseLabel, resultLabel).Observe(d.Seconds())
	m.startupPhaseLast.WithLabelValues(phaseLabel, resultLabel).Set(d.Seconds())
}

// StartupReady marks the process as ready and records total startup duration.
// Maintains a histogram (for distributions) and a sticky gauge + counter so
// dashboards remain informative between process starts.
func (m *Metrics) StartupReady(result string, total time.Duration, readyAt time.Time) {
	if m == nil {
		return
	}
	resultLabel := httputil.LabelOrUnknown(result)
	m.startupTotalDuration.WithLabelValues(resultLabel).Observe(total.Seconds())
	m.startupTotalLast.WithLabelValues(resultLabel).Set(total.Seconds())
	m.startupCount.WithLabelValues(resultLabel).Inc()
	if result == "ok" {
		m.startupReady.Set(1)
		m.startupReadyAt.Set(float64(readyAt.Unix()))
	}
}

// TelegramMTProtoBootstrap records MTProto bootstrap attempts by outcome.
func (m *Metrics) TelegramMTProtoBootstrap(outcome string) {
	if m == nil {
		return
	}
	m.telegramMTProtoBoot.WithLabelValues(httputil.LabelOrUnknown(outcome)).Inc()
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
		m.CreatorActivation(evt.Fields["creator_id"], evt.Outcome)
	case events.NameSubscriptionEnd:
		m.SubscriptionEnd(evt.Outcome)
	case events.NameViewerOAuth:
		m.ViewerOAuth(evt.Outcome)
	case events.NameViewerJoinTarget:
		m.ViewerJoinTargets(evt.Fields["kind"], evt.Count)
	case events.NameViewerInviteLink:
		m.ViewerInviteLink(evt.Outcome, evt.Fields["reason"])
	case events.NameCreatorTokenRefresh:
		m.CreatorTokenRefresh(evt.Fields["creator_id"], evt.Outcome)
	case events.NameCreatorBlocklistSync:
		m.CreatorBlocklistSync(evt.Fields["creator_id"], evt.Outcome, evt.Count)
	case events.NameCreatorBlocklistEnforcement:
		m.CreatorBlocklistEnforcement(evt.Fields["creator_id"], evt.Outcome, evt.Count)
	case events.NameCreatorAuthTransition:
		m.CreatorAuthTransition(evt.Fields["creator_id"], evt.Fields["from"], evt.Fields["to"], evt.Fields["reason"])
	case events.NameCreatorsReconnectRequired:
		m.CreatorsReconnectRequired(evt.Count)
	case events.NameCreatorReconnectNotice:
		m.CreatorReconnectNotification(evt.Fields["creator_id"], evt.Outcome)
	case events.NameBackgroundJobSchedule:
		m.BackgroundJobSchedule(evt.Fields["job"], evt.Fields["interval_seconds"], evt.Fields["timeout_seconds"])
	case events.NameBackgroundJob:
		m.BackgroundJobFinished(evt.Fields["job"], evt.Outcome, evt.Duration, evt.Fields["finished_at_unix_ms"])
	case events.NameBackgroundJobStarted:
		m.BackgroundJobStarted(evt.Fields["job"], evt.Fields["started_at_unix_ms"])
	case events.NameBackgroundJobItems:
		m.BackgroundJobItems(evt.Fields["job"], evt.Fields["kind"], evt.Outcome, evt.Count)
	case events.NameReconciliationRepair:
		m.ReconciliationRepair(evt.Fields["repair"], evt.Outcome, evt.Count)
	case events.NameOAuthStart:
		m.OAuthStart(evt.Fields["mode"], evt.Outcome)
	case events.NameOAuthCallback:
		m.OAuthCallback(evt.Fields["mode"], evt.Outcome)
	case events.NameEventSubMessage:
		m.EventSubMessage(evt.Fields["message_type"], evt.Fields["subscription_type"], evt.Outcome)
		m.TwitchEventSubNotification(evt.Fields["creator_id"], evt.Fields["message_type"], evt.Fields["subscription_type"], evt.Outcome)
	case events.NameTelegramWebhook:
		m.TelegramWebhookResult(evt.Outcome)
	case events.NameCreatorOAuth:
		m.CreatorOAuth(evt.Fields["creator_id"], evt.Outcome)
	case events.NameCreatorStatus:
		m.CreatorStatus(evt.Fields["creator_id"], evt.Outcome)
	case events.NameViewerAccess:
		m.ViewerAccess(evt.Outcome)
	case events.NameTelegramCommand:
		m.TelegramCommand(evt.Fields["command"], evt.Fields["chat_type"])
	case events.NameTelegramCommandResponse:
		m.TelegramCommandResponse(evt.Fields["command"], evt.Fields["chat_type"], evt.Outcome, evt.Duration)
	case events.NameTelegramCallback:
		m.TelegramCallback(evt.Fields["domain"], evt.Fields["verb"])
	case events.NameTelegramCallbackResponse:
		m.TelegramCallbackResponse(evt.Fields["domain"], evt.Fields["verb"], evt.Outcome, evt.Duration)
	case events.NameTelegramAPIError:
		m.TelegramAPIError(evt.Fields["method"], evt.Fields["reason"])
	case events.NameTelegramKickAction:
		m.TelegramKickAction(evt.Fields["reason"], evt.Outcome)
	case events.NameTelegramMTProtoBootstrap:
		m.TelegramMTProtoBootstrap(evt.Outcome)
	}
}

// Handler returns an HTTP handler that serves Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// OAuthStart records an OAuth start page request by mode and result.
func (m *Metrics) OAuthStart(mode, result string) {
	if m == nil {
		return
	}
	m.oauthStartsTotal.WithLabelValues(httputil.LabelOrUnknown(mode), httputil.LabelOrUnknown(result)).Inc()
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

// TwitchEventSubNotification records creator-scoped Twitch EventSub notification outcomes.
func (m *Metrics) TwitchEventSubNotification(creatorID, messageType, subscriptionType, outcome string) {
	if m == nil {
		return
	}
	if httputil.LabelOrUnknown(messageType) != "notification" {
		return
	}
	result, ok := normalizeTwitchEventSubResult(subscriptionType, outcome)
	if !ok {
		return
	}
	m.twitchEventSubTotal.WithLabelValues(
		httputil.LabelOrUnknown(creatorID),
		httputil.LabelOrUnknown(subscriptionType),
		result,
	).Inc()
}

func normalizeTwitchEventSubResult(subscriptionType, outcome string) (string, bool) {
	switch subscriptionType {
	case "channel.subscribe":
		switch outcome {
		case "notification_subscribe":
			return twitchEventSubResultOK, true
		case "notification_subscribe_store_failed":
			return twitchEventSubResultFailed, true
		}
	case "channel.subscription.end":
		switch outcome {
		case "notification_subscription_end":
			return twitchEventSubResultOK, true
		case "notification_subscription_end_failed":
			return twitchEventSubResultFailed, true
		}
	case "channel.ban":
		switch outcome {
		case "notification_ban":
			return twitchEventSubResultOK, true
		case "notification_ban_failed":
			return twitchEventSubResultFailed, true
		}
	case "channel.unban":
		switch outcome {
		case "notification_unban":
			return twitchEventSubResultOK, true
		case "notification_unban_failed":
			return twitchEventSubResultFailed, true
		}
	}
	return "", false
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

// BackgroundJobSchedule stores the configured schedule metadata for a background job.
func (m *Metrics) BackgroundJobSchedule(job, intervalSeconds, timeoutSeconds string) {
	if m == nil {
		return
	}
	jobLabel := httputil.LabelOrUnknown(job)
	if interval, err := strconv.ParseFloat(intervalSeconds, 64); err == nil {
		m.backgroundJobInterval.WithLabelValues(jobLabel).Set(interval)
	}
	if timeout, err := strconv.ParseFloat(timeoutSeconds, 64); err == nil {
		m.backgroundJobTimeout.WithLabelValues(jobLabel).Set(timeout)
	}
}

// BackgroundJobStarted marks the last known background job state as currently running.
func (m *Metrics) BackgroundJobStarted(job, startedAtUnixMS string) {
	if m == nil {
		return
	}
	jobLabel := httputil.LabelOrUnknown(job)
	m.backgroundJobState.WithLabelValues(jobLabel).Set(backgroundJobStateCode("running"))
	if startedAt, err := strconv.ParseFloat(startedAtUnixMS, 64); err == nil {
		m.backgroundJobLastStart.WithLabelValues(jobLabel).Set(startedAt / 1000)
	}
}

// BackgroundJobFinished records a completed background job execution and updates the last-known-state gauge.
func (m *Metrics) BackgroundJobFinished(job, result string, d time.Duration, finishedAtUnixMS string) {
	if m == nil {
		return
	}
	jobLabel := httputil.LabelOrUnknown(job)
	m.BackgroundJob(job, result, d)
	m.backgroundJobState.WithLabelValues(jobLabel).Set(backgroundJobStateCode(result))
	if finishedAt, err := strconv.ParseFloat(finishedAtUnixMS, 64); err == nil {
		m.backgroundJobLastFinish.WithLabelValues(jobLabel).Set(finishedAt / 1000)
	}
}

// BackgroundJobItems increments a per-kind counter for the given background job run.
func (m *Metrics) BackgroundJobItems(job, kind, result string, n int) {
	if m == nil || n == 0 {
		return
	}
	m.backgroundJobItems.WithLabelValues(
		httputil.LabelOrUnknown(job),
		httputil.LabelOrUnknown(kind),
		httputil.LabelOrUnknown(result),
	).Add(float64(n))
}

func backgroundJobStateCode(result string) float64 {
	switch result {
	case "running":
		return 1
	case "ok":
		return 2
	case "partial_failure":
		return 4
	case "timeout":
		return 5
	default:
		return 3
	}
}

// RedisBackup records a Redis backup run and snapshots the latest successful size metrics.
func (m *Metrics) RedisBackup(result string, d time.Duration, keyCount int, sizeBytes int64) {
	if m == nil {
		return
	}
	result = httputil.LabelOrUnknown(result)
	m.redisBackupRuns.WithLabelValues(result).Inc()
	m.redisBackupTime.WithLabelValues(result).Observe(d.Seconds())
	if result != "ok" {
		return
	}
	m.redisBackupKeys.Set(float64(keyCount))
	m.redisBackupBytes.Set(float64(sizeBytes))
}

// ObserveRedisCommand records Redis commands issued by app workflows.
func (m *Metrics) ObserveRedisCommand(_ context.Context, job, command, result string, count int) {
	if m == nil || count == 0 {
		return
	}
	m.redisCommands.WithLabelValues(
		httputil.LabelOrUnknown(job),
		httputil.LabelOrUnknown(command),
		httputil.LabelOrUnknown(result),
	).Add(float64(count))
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
