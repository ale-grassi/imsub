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
	m.OAuthCallback("viewer", "ok")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TelegramWebhookResult("ok")
	m.BackgroundJob("audit", "ok", time.Millisecond)
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
	m.ViewerInviteLink("ok")
	m.TelegramCommand("start", "private")
	m.TelegramCommandResponse("start", "private", "ok", 150*time.Millisecond)
	m.TelegramAPIError("answer_callback_query", "message_too_long")
	m.TelegramKickAction("group_policy", "ok")
	m.TelegramMTProtoBootstrap("ok")
	m.TelegramDailyActiveUsers(4)
	m.LinkedViewerAccounts(5)
	m.LinkedCreatorAccounts(2)
	m.ManagedGroups(3)
	m.CreatorInfo("creator-1", "Alpha", "alpha")
	m.CreatorManagedGroups("creator-1", 2)
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
	m.OAuthCallback("viewer", "success")
	m.EventSubMessage("notification", "channel.subscribe", "ok")
	m.TelegramWebhookResult("ok")
	m.BackgroundJob("integrity_audit", "ok", 120*time.Millisecond)
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
	m.ViewerInviteLink("ok")
	m.TelegramCommand("start", "private")
	m.TelegramCommandResponse("start", "private", "ok", 150*time.Millisecond)
	m.TelegramAPIError("answer_callback_query", "message_too_long")
	m.TelegramKickAction("group_policy", "ok")
	m.TelegramMTProtoBootstrap("ok")
	m.TelegramDailyActiveUsers(4)
	m.LinkedViewerAccounts(5)
	m.LinkedCreatorAccounts(2)
	m.ManagedGroups(3)
	m.CreatorInfo("creator-1", "Alpha", "alpha")
	m.CreatorManagedGroups("creator-1", 2)
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
		"imsub_eventsub_messages_total",
		"imsub_telegram_webhook_updates_total",
		"imsub_background_jobs_total",
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
		"imsub_telegram_api_errors_total",
		"imsub_telegram_kick_actions_total",
		"imsub_telegram_mtproto_bootstrap_total",
		"imsub_telegram_daily_active_users",
		"imsub_linked_viewer_accounts",
		"imsub_linked_creator_accounts",
		"imsub_managed_groups",
		"imsub_creator_info",
		"imsub_creator_managed_groups",
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
	m.Emit(t.Context(), events.Event{Name: events.NameViewerJoinTarget, Fields: map[string]string{"kind": "invite_groups"}, Count: 2})
	m.Emit(t.Context(), events.Event{Name: events.NameViewerInviteLink, Outcome: "ok"})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `imsub_viewer_join_targets_total{kind="invite_groups"} 2`) {
		t.Fatalf("metrics output missing projected viewer_join_target event: %s", body)
	}
	if !strings.Contains(body, `imsub_viewer_invite_links_total{result="ok"} 1`) {
		t.Fatalf("metrics output missing projected viewer_invite_link event: %s", body)
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
