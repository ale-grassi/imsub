// Package events defines the shared event model used across use cases and lower-level workflows.
package events

import (
	"context"
	"time"
)

// Shared event names emitted across use cases, transport, jobs, and lower-level workflows.
const (
	NameResetExecuted               = "reset_executed"
	NameResetGroupTarget            = "reset_group_target"
	NameGroupRegistration           = "group_registration"
	NameGroupUnregistration         = "group_unregistration"
	NameGroupPolicyUpdate           = "group_policy_update"
	NameGroupLanguageUpdate         = "group_language_update"
	NameCreatorActivation           = "creator_activation"
	NameSubscriptionEnd             = "subscription_end"
	NameViewerOAuth                 = "viewer_oauth"
	NameViewerAccess                = "viewer_access"
	NameViewerJoinTarget            = "viewer_join_target"
	NameViewerInviteLink            = "viewer_invite_link"
	NameCreatorOAuth                = "creator_oauth"
	NameCreatorStatus               = "creator_status"
	NameTelegramCommand             = "telegram_command"
	NameTelegramCommandResponse     = "telegram_command_response"
	NameTelegramCallback            = "telegram_callback"
	NameTelegramCallbackResponse    = "telegram_callback_response"
	NameTelegramAPIError            = "telegram_api_error"
	NameTelegramKickAction          = "telegram_kick_action"
	NameTelegramMTProtoBootstrap    = "telegram_mtproto_bootstrap"
	NameCreatorTokenRefresh         = "creator_token_refresh"
	NameCreatorBlocklistSync        = "creator_blocklist_sync"
	NameCreatorBlocklistEnforcement = "creator_blocklist_enforcement"
	NameCreatorAuthTransition       = "creator_auth_transition"
	NameCreatorsReconnectRequired   = "creators_reconnect_required"
	NameCreatorReconnectNotice      = "creator_reconnect_notification"
	NameBackgroundJob               = "background_job"
	NameBackgroundJobStarted        = "background_job_started"
	NameBackgroundJobItems          = "background_job_items"
	NameReconciliationRepair        = "reconciliation_repair"
	NameOAuthStart                  = "oauth_start"
	NameOAuthCallback               = "oauth_callback"
	NameEventSubMessage             = "eventsub_message"
	NameTelegramWebhook             = "telegram_webhook"
)

// Event is a small cross-layer event emitted by application and domain workflows.
type Event struct {
	Name     string
	Outcome  string
	Fields   map[string]string
	Count    int
	Duration time.Duration
}

// EventSink consumes emitted events.
type EventSink interface {
	Emit(ctx context.Context, evt Event)
}

// NoopSink discards all emitted events.
type NoopSink struct{}

// Emit discards the provided event.
func (NoopSink) Emit(context.Context, Event) {}

// EnsureSink replaces a nil sink with a no-op sink.
func EnsureSink(sink EventSink) EventSink {
	if sink == nil {
		return NoopSink{}
	}
	return sink
}

// MultiSink fans out events to multiple sinks.
type MultiSink struct {
	Sinks []EventSink
}

// Emit sends the event to all configured sinks.
func (m MultiSink) Emit(ctx context.Context, evt Event) {
	for _, sink := range m.Sinks {
		if sink == nil {
			continue
		}
		sink.Emit(ctx, evt)
	}
}
