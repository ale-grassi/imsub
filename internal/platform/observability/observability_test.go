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
	m.ResetExecution("viewer", "ok")
	m.ResetGroupTargets("tracked", 2)
	m.GroupRegistration("registered")
	m.GroupUnregistration("unregistered")
	m.CreatorActivation("success")
	m.SubscriptionEnd("applied")
	m.ReconciliationRepair("tracked_reverse_index", "ok", 2)
	m.ViewerOAuth("success")
	m.CreatorOAuth("success")
	m.CreatorStatus("loaded")
	m.ViewerAccess("linked")
	m.ViewerJoinTargets("invite_groups", 2)
	m.ViewerInviteLink("ok")

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
	m.ResetExecution("viewer", "ok")
	m.ResetGroupTargets("tracked", 2)
	m.GroupRegistration("registered")
	m.GroupUnregistration("unregistered")
	m.CreatorActivation("success")
	m.SubscriptionEnd("applied")
	m.ReconciliationRepair("tracked_reverse_index", "ok", 2)
	m.ViewerOAuth("success")
	m.CreatorOAuth("success")
	m.CreatorStatus("loaded")
	m.ViewerAccess("linked")
	m.ViewerJoinTargets("invite_groups", 2)
	m.ViewerInviteLink("ok")

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
	}
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Errorf("m.Handler() output missing %q", needle)
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

func TestEmitProjectsEventSubEvents(t *testing.T) {
	t.Parallel()

	m := New()
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorTokenRefresh, Outcome: "ok"})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorAuthTransition, Fields: map[string]string{
		"from":   "healthy",
		"to":     "reconnect_required",
		"reason": "token_refresh_failed",
	}})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorsReconnectRequired, Count: 3})
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorReconnectNotice, Outcome: "failed"})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	needles := []string{
		`imsub_creator_token_refresh_total{result="ok"} 1`,
		`imsub_creator_auth_state_transitions_total{from="healthy",reason="token_refresh_failed",to="reconnect_required"} 1`,
		`imsub_creators_reconnect_required 3`,
		`imsub_creator_reconnect_notifications_total{result="failed"} 1`,
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
	m.Emit(t.Context(), events.Event{Name: events.NameCreatorActivation, Outcome: "success"})
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
		`imsub_creator_activation_total{result="success"} 1`,
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
