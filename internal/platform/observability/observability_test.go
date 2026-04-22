package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imsub/internal/events"
)

func TestNilSafety(t *testing.T) {
	t.Parallel()

	var m *Metrics
	m.OAuthStart("viewer", "ok")
	m.OAuthCallback("viewer", "ok")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TwitchEventSubNotification("creator-1", "notification", "channel.subscribe", "notification_subscribe")
	m.TelegramWebhookResult("ok")
	m.BackgroundJobSchedule("audit", "60", "30")
	m.BackgroundJob("audit", "ok", time.Millisecond)
	m.BackgroundJobStarted("audit", "1713489600000")
	m.BackgroundJobFinished("audit", "ok", time.Millisecond, "1713489601000")
	m.RedisBackup("ok", time.Millisecond, 3, 128)
	m.ResetExecution("viewer", "ok")
	m.ResetGroupTargets("tracked", 2)
	m.GroupRegistration("registered")
	m.GroupUnregistration("unregistered")
	m.CreatorActivation("creator-1", "success")
	m.SubscriptionEnd("applied")
	m.ReconciliationRepair("tracked_reverse_index", "ok", 2)
	m.ViewerOAuth("success")
	m.CreatorOAuth("creator-1", "success")
	m.CreatorStatus("creator-1", "loaded")
	m.ViewerAccess("linked")
	m.ViewerJoinTargets("invite_groups", 2)
	m.ViewerInviteLink("ok", "none")
	m.TelegramCommand("start", "private")
	m.TelegramCommandResponse("start", "private", "ok", 150*time.Millisecond)
	m.TelegramCallback("group", "pick")
	m.TelegramCallbackResponse("group", "pick", "registered", 200*time.Millisecond)
	m.TelegramAPIError("answer_callback_query", "message_too_long")
	m.TelegramKickAction("group_policy", "ok")
	m.TelegramMTProtoBootstrap("ok")
	m.TelegramDailyActiveUsers(4)
	m.LinkedViewerAccounts(5)
	m.LinkedCreatorAccounts(2)
	m.ManagedGroups(3)
	m.CreatorInfo("creator-1", "Alpha", "alpha")
	m.CreatorManagedGroups("creator-1", 2)
	m.CreatorGroupPolicyCount("creator-1", "kick", 1)
	m.CreatorGroupLanguageCount("creator-1", "en", 2)
	m.CreatorSubscriptionEndGrace("creator-1", "48h")
	m.CreatorBanSyncEnabled("creator-1", true)
	m.CreatorLastBanSyncAt("creator-1", time.Unix(1700000000, 0))
	m.CreatorSubscribers("creator-1", 10)
	m.CreatorBlockedUsers("creator-1", 1)
	m.CreatorTrackedMembers("creator-1", 4)
	m.CreatorUntrackedMembers("creator-1", 3)
	m.CreatorReconnectRequired("creator-1", true)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("m.Handler().ServeHTTP status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMetricsExposure(t *testing.T) {
	t.Parallel()

	m := New()
	m.OAuthStart("viewer", "ok")
	m.OAuthCallback("viewer", "success")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TwitchEventSubNotification("creator-1", "notification", "channel.subscribe", "notification_subscribe")
	m.TelegramWebhookResult("ok")
	m.BackgroundJobSchedule("integrity_audit", "1200", "900")
	m.BackgroundJob("integrity_audit", "ok", 120*time.Millisecond)
	m.BackgroundJobStarted("integrity_audit", "1713489600000")
	m.BackgroundJobFinished("integrity_audit", "ok", 120*time.Millisecond, "1713489601000")
	m.RedisBackup("ok", 140*time.Millisecond, 7, 1024)
	m.ResetExecution("viewer", "ok")
	m.ResetGroupTargets("tracked", 2)
	m.GroupRegistration("registered")
	m.GroupUnregistration("unregistered")
	m.CreatorActivation("creator-1", "success")
	m.SubscriptionEnd("applied")
	m.ReconciliationRepair("tracked_reverse_index", "ok", 2)
	m.ViewerOAuth("success")
	m.CreatorOAuth("creator-1", "success")
	m.CreatorStatus("creator-1", "loaded")
	m.ViewerAccess("linked")
	m.ViewerJoinTargets("invite_groups", 2)
	m.ViewerInviteLink("ok", "none")
	m.TelegramCommand("start", "private")
	m.TelegramCommandResponse("start", "private", "ok", 150*time.Millisecond)
	m.TelegramCallback("group", "pick")
	m.TelegramCallbackResponse("group", "pick", "registered", 200*time.Millisecond)
	m.TelegramAPIError("answer_callback_query", "message_too_long")
	m.TelegramKickAction("group_policy", "ok")
	m.TelegramMTProtoBootstrap("ok")
	m.TelegramDailyActiveUsers(4)
	m.LinkedViewerAccounts(5)
	m.LinkedCreatorAccounts(2)
	m.ManagedGroups(3)
	m.CreatorInfo("creator-1", "Alpha", "alpha")
	m.CreatorManagedGroups("creator-1", 2)
	m.CreatorGroupPolicyCount("creator-1", "kick", 1)
	m.CreatorGroupLanguageCount("creator-1", "en", 2)
	m.CreatorSubscriptionEndGrace("creator-1", "48h")
	m.CreatorBanSyncEnabled("creator-1", true)
	m.CreatorLastBanSyncAt("creator-1", time.Unix(1700000000, 0))
	m.CreatorSubscribers("creator-1", 10)
	m.CreatorBlockedUsers("creator-1", 1)
	m.CreatorTrackedMembers("creator-1", 4)
	m.CreatorUntrackedMembers("creator-1", 3)
	m.CreatorReconnectRequired("creator-1", true)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("m.Handler().ServeHTTP status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	needles := []string{
		"imsub_oauth_callbacks_total",
		"imsub_oauth_starts_total",
		"imsub_eventsub_messages_total",
		"imsub_twitch_eventsub_notifications_total",
		"imsub_telegram_webhook_updates_total",
		"imsub_background_jobs_total",
		"imsub_background_job_schedule_interval_seconds",
		"imsub_background_job_schedule_timeout_seconds",
		"imsub_background_job_last_start_timestamp_seconds",
		"imsub_background_job_last_finish_timestamp_seconds",
		"imsub_redis_backup_runs_total",
		"imsub_redis_backup_duration_seconds",
		"imsub_redis_backup_exported_keys",
		"imsub_redis_backup_uploaded_bytes",
		"imsub_reset_executions_total",
		"imsub_reset_group_targets_total",
		"imsub_group_registrations_total",
		"imsub_group_unregistrations_total",
		"imsub_creator_activation_total",
		"imsub_subscription_end_total",
		"imsub_reconciliation_repairs_total",
		"imsub_viewer_oauth_total",
		"imsub_creator_oauth_total",
		"imsub_creator_status_total",
		"imsub_viewer_access_total",
		"imsub_viewer_join_targets_total",
		"imsub_viewer_invite_links_total",
		"imsub_telegram_commands_total",
		"imsub_telegram_command_response_duration_seconds",
		"imsub_telegram_callbacks_total",
		"imsub_telegram_callback_response_duration_seconds",
		"imsub_telegram_api_errors_total",
		"imsub_telegram_kick_actions_total",
		"imsub_telegram_mtproto_bootstrap_total",
		"imsub_telegram_daily_active_users",
		"imsub_linked_viewer_accounts",
		"imsub_linked_creator_accounts",
		"imsub_managed_groups",
		"imsub_creator_info",
		"imsub_creator_managed_groups",
		"imsub_creator_group_policy_groups",
		"imsub_creator_group_language_groups",
		"imsub_creator_subscription_end_grace",
		"imsub_creator_blocklist_sync_enabled",
		"imsub_creator_last_ban_sync_timestamp_seconds",
		"imsub_creator_subscribers",
		"imsub_creator_blocked_users",
		"imsub_creator_tracked_members",
		"imsub_creator_untracked_members",
		"imsub_creator_reconnect_required",
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Errorf("m.Handler() output missing %q", needle)
		}
	}
}

func TestBackgroundJobStateAndItems(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(context.Background(), events.Event{
		Name: events.NameBackgroundJobSchedule,
		Fields: map[string]string{
			"job":              "demo",
			"interval_seconds": "300",
			"timeout_seconds":  "60",
		},
	})
	m.Emit(context.Background(), events.Event{
		Name:   events.NameBackgroundJobStarted,
		Fields: map[string]string{"job": "demo", "started_at_unix_ms": "1713489600000"},
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `imsub_background_job_state{job="demo"} 1`) {
		t.Fatalf("state gauge not set to running: %s", rec.Body.String())
	}

	m.Emit(context.Background(), events.Event{
		Name:     events.NameBackgroundJob,
		Outcome:  "ok",
		Fields:   map[string]string{"job": "demo", "finished_at_unix_ms": "1713489601000"},
		Duration: 10 * time.Millisecond,
	})
	m.Emit(context.Background(), events.Event{
		Name:    events.NameBackgroundJobItems,
		Outcome: "ok",
		Fields:  map[string]string{"job": "demo", "kind": "processed"},
		Count:   3,
	})

	rec = httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, needle := range []string{
		`imsub_background_job_state{job="demo"} 2`,
		`imsub_background_job_schedule_interval_seconds{job="demo"} 300`,
		`imsub_background_job_schedule_timeout_seconds{job="demo"} 60`,
		`imsub_background_job_last_start_timestamp_seconds{job="demo"} 1.7134896e+09`,
		`imsub_background_job_last_finish_timestamp_seconds{job="demo"} 1.713489601e+09`,
		`imsub_background_job_items_total{job="demo",kind="processed",result="ok"} 3`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("metrics output missing %q: %s", needle, body)
		}
	}
}

func TestRedisBackupMetricsKeepLastSuccessfulSnapshot(t *testing.T) {
	t.Parallel()

	m := New()
	m.RedisBackup("ok", 2*time.Second, 11, 2048)
	m.RedisBackup("failed", 3*time.Second, 0, 0)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	needles := []string{
		`imsub_redis_backup_runs_total{result="ok"} 1`,
		`imsub_redis_backup_runs_total{result="failed"} 1`,
		`imsub_redis_backup_duration_seconds_count{result="ok"} 1`,
		`imsub_redis_backup_duration_seconds_count{result="failed"} 1`,
		`imsub_redis_backup_exported_keys 11`,
		`imsub_redis_backup_uploaded_bytes 2048`,
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("metrics output missing backup metric %q: %s", needle, body)
		}
	}
}

func TestEmitProjectsViewerEvents(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(t.Context(), events.Event{Name: events.NameOAuthStart, Outcome: "ok", Fields: map[string]string{"mode": "viewer"}})
	m.Emit(t.Context(), events.Event{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "invite_groups"}, Count: 2})
	m.Emit(t.Context(), events.Event{Name: events.NameViewerInviteLink, Outcome: "ok", Fields: map[string]string{"reason": "none"}})
	m.Emit(t.Context(), events.Event{Name: events.NameViewerInviteLink, Outcome: "failed", Fields: map[string]string{"reason": "bad_request"}})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `imsub_oauth_starts_total{mode="viewer",result="ok"} 1`) {
		t.Fatalf("metrics output missing projected oauth_start event: %s", body)
	}
	if !strings.Contains(body, `imsub_viewer_join_targets_total{kind="invite_groups"} 2`) {
		t.Fatalf("metrics output missing projected viewer_join_target event: %s", body)
	}
	if !strings.Contains(body, `imsub_viewer_invite_links_total{reason="none",result="ok"} 1`) {
		t.Fatalf("metrics output missing projected viewer_invite_link event: %s", body)
	}
	if !strings.Contains(body, `imsub_viewer_invite_links_total{reason="bad_request",result="failed"} 1`) {
		t.Fatalf("metrics output missing projected failed viewer_invite_link event: %s", body)
	}
}

func TestEmitProjectsTelegramEvents(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(t.Context(), events.Event{
		Name:   events.NameTelegramCommand,
		Fields: map[string]string{"command": "start", "chat_type": "private"},
	})
	m.Emit(t.Context(), events.Event{
		Name:     events.NameTelegramCommandResponse,
		Outcome:  "ok",
		Fields:   map[string]string{"command": "start", "chat_type": "private"},
		Duration: 150 * time.Millisecond,
	})
	m.Emit(t.Context(), events.Event{
		Name:   events.NameTelegramCallback,
		Fields: map[string]string{"domain": "group", "verb": "pick"},
	})
	m.Emit(t.Context(), events.Event{
		Name:     events.NameTelegramCallbackResponse,
		Outcome:  "registered",
		Fields:   map[string]string{"domain": "group", "verb": "pick"},
		Duration: 200 * time.Millisecond,
	})
	m.Emit(t.Context(), events.Event{
		Name:   events.NameTelegramAPIError,
		Fields: map[string]string{"method": "answer_callback_query", "reason": "message_too_long"},
	})
	m.Emit(t.Context(), events.Event{
		Name:   events.NameTelegramAPIError,
		Fields: map[string]string{"method": "edit_message_text", "reason": "forbidden"},
	})
	m.Emit(t.Context(), events.Event{
		Name:    events.NameTelegramKickAction,
		Outcome: "ok",
		Fields:  map[string]string{"reason": "group_policy"},
	})
	m.Emit(t.Context(), events.Event{
		Name:    events.NameTelegramMTProtoBootstrap,
		Outcome: "ok",
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `imsub_telegram_commands_total{chat_type="private",command="start"} 1`) {
		t.Fatalf("metrics output missing projected telegram_command event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_command_response_duration_seconds_count{chat_type="private",command="start",result="ok"} 1`) {
		t.Fatalf("metrics output missing projected telegram_command_response event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_callbacks_total{domain="group",verb="pick"} 1`) {
		t.Fatalf("metrics output missing projected telegram_callback event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_callback_response_duration_seconds_count{domain="group",result="registered",verb="pick"} 1`) {
		t.Fatalf("metrics output missing projected telegram_callback_response event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_api_errors_total{method="answer_callback_query",reason="message_too_long"} 1`) {
		t.Fatalf("metrics output missing projected telegram_api_error event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_api_errors_total{method="edit_message_text",reason="forbidden"} 1`) {
		t.Fatalf("metrics output missing projected forbidden telegram_api_error event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_kick_actions_total{reason="group_policy",result="ok"} 1`) {
		t.Fatalf("metrics output missing projected telegram_kick_action event: %s", body)
	}
	if !strings.Contains(body, `imsub_telegram_mtproto_bootstrap_total{outcome="ok"} 1`) {
		t.Fatalf("metrics output missing projected telegram_mtproto_bootstrap event: %s", body)
	}
}

func TestEmitProjectsEventSubEvents(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(t.Context(), events.Event{
		Name:    events.NameEventSubMessage,
		Outcome: "notification_subscribe",
		Fields: map[string]string{
			"creator_id":        "c1",
			"message_type":      "notification",
			"subscription_type": "channel.subscribe",
		},
	})
	m.Emit(t.Context(), events.Event{
		Name:    events.NameEventSubMessage,
		Outcome: "notification_subscription_end_failed",
		Fields: map[string]string{
			"creator_id":        "c1",
			"message_type":      "notification",
			"subscription_type": "channel.subscription.end",
		},
	})
	m.Emit(t.Context(), events.Event{
		Name:    events.NameEventSubMessage,
		Outcome: "duplicate",
		Fields: map[string]string{
			"creator_id":        "c1",
			"message_type":      "notification",
			"subscription_type": "channel.subscribe",
		},
	})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorTokenRefresh, Outcome: "ok", Fields: map[string]string{"creator_id": "c1"}})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorBlocklistSync, Outcome: "ok", Count: 4, Fields: map[string]string{"creator_id": "c1"}})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorBlocklistEnforcement, Outcome: "ok", Count: 2, Fields: map[string]string{"creator_id": "c1"}})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorAuthTransition, Fields: map[string]string{
		"creator_id": "c1",
		"from":       "healthy",
		"to":         "reconnect_required",
		"reason":     "token_refresh_failed",
	}})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorsReconnectRequired, Count: 3})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorReconnectNotice, Outcome: "failed", Fields: map[string]string{"creator_id": "c1"}})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	needles := []string{
		`imsub_eventsub_messages_total{message_type="notification",result="notification_subscribe",subscription_type="channel.subscribe"} 1`,
		`imsub_eventsub_messages_total{message_type="notification",result="notification_subscription_end_failed",subscription_type="channel.subscription.end"} 1`,
		`imsub_eventsub_messages_total{message_type="notification",result="duplicate",subscription_type="channel.subscribe"} 1`,
		`imsub_twitch_eventsub_notifications_total{creator_id="c1",result="ok",subscription_type="channel.subscribe"} 1`,
		`imsub_twitch_eventsub_notifications_total{creator_id="c1",result="failed",subscription_type="channel.subscription.end"} 1`,
		`imsub_creator_token_refresh_total{creator_id="c1",result="ok"} 1`,
		`imsub_creator_blocklist_sync_total{creator_id="c1",result="ok"} 4`,
		`imsub_creator_blocklist_enforcement_total{creator_id="c1",result="ok"} 2`,
		`imsub_creator_auth_state_transitions_total{creator_id="c1",from="healthy",reason="token_refresh_failed",to="reconnect_required"} 1`,
		`imsub_creators_reconnect_required 3`,
		`imsub_creator_reconnect_notifications_total{creator_id="c1",result="failed"} 1`,
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("metrics output missing projected EventSub event %q: %s", needle, body)
		}
	}
}

func TestEmitProjectsNewApplicationEvents(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorActivation, Outcome: "success", Fields: map[string]string{"creator_id": "c1"}})
	m.Emit(t.Context(), events.Event{Name: events.NameSubscriptionEnd, Outcome: "applied"})
	m.Emit(t.Context(), events.Event{
		Name:    events.NameReconciliationRepair,
		Outcome: "ok",
		Fields:  map[string]string{"repair": "tracked_reverse_index"},
		Count:   3,
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	needles := []string{
		`imsub_creator_activation_total{creator_id="c1",result="success"} 1`,
		`imsub_subscription_end_total{result="applied"} 1`,
		`imsub_reconciliation_repairs_total{outcome="ok",repair="tracked_reverse_index"} 3`,
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("metrics output missing projected application event %q: %s", needle, body)
		}
	}
}

func TestMiddlewareNilDependencies(t *testing.T) {
	t.Parallel()

	m := New()
	handler := m.Middleware(nil, nil, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Middleware(nil,nil,nil) status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
