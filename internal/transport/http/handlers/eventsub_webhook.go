package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/events"
)

const (
	eventStatusUnknown                          = "unknown"
	eventStatusError                            = "error"
	eventStatusBadBody                          = "bad_body"
	eventStatusInvalidSignature                 = "invalid_signature"
	eventStatusMissingMessageType               = "missing_message_type"
	eventStatusInvalidJSON                      = "invalid_json"
	eventStatusChallengeOK                      = "challenge_ok"
	eventStatusMissingMessageID                 = "missing_message_id"
	eventStatusRedisError                       = "redis_error"
	eventStatusDuplicate                        = "duplicate"
	eventStatusRevocation                       = "revocation"
	eventStatusNotificationSubscribeStoreFailed = "notification_subscribe_store_failed"
	eventStatusNotificationSubscribe            = "notification_subscribe"
	eventStatusNotificationSubEndFailed         = "notification_subscription_end_failed"
	eventStatusNotificationSubEnd               = "notification_subscription_end"
	eventStatusNotificationBanFailed            = "notification_ban_failed"
	eventStatusNotificationBan                  = "notification_ban"
	eventStatusNotificationUnbanFailed          = "notification_unban_failed"
	eventStatusNotificationUnban                = "notification_unban"
	eventStatusNotificationOther                = "notification_other"
	eventStatusIgnoredMessageType               = "ignored_message_type"
)

// EventSubWebhook verifies and processes Twitch EventSub webhook deliveries.
func (c *Controller) EventSubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := c.logCtx(r.Context())
	logger.Debug("eventsub webhook received", "method", r.Method, "path", r.URL.Path)
	messageType := strings.TrimSpace(r.Header.Get("Twitch-Eventsub-Message-Type"))
	subscriptionType := eventStatusUnknown
	creatorID := eventStatusUnknown
	result := eventStatusError
	defer func() {
		if c.events != nil {
			c.events.Emit(ctx, events.Event{
				Name:    events.NameEventSubMessage,
				Outcome: result,
				Fields: map[string]string{
					"message_type":      messageType,
					"subscription_type": subscriptionType,
					"creator_id":        creatorID,
				},
			})
		}
	}()

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		result = eventStatusBadBody
		WriteHTTPError(w, BadRequestError("bad body", err))
		return
	}
	if !twitch.VerifyEventSubSignature(c.cfg.TwitchEventSubSecret, r.Header, body) {
		logger.Debug("eventsub signature verification failed", "message_id", r.Header.Get("Twitch-Eventsub-Message-Id"), "message_type", r.Header.Get("Twitch-Eventsub-Message-Type"))
		result = eventStatusInvalidSignature
		WriteHTTPError(w, UnauthorizedError("invalid signature", nil))
		return
	}

	if messageType == "" {
		result = eventStatusMissingMessageType
		WriteHTTPError(w, BadRequestError("missing message type", nil))
		return
	}

	var env twitch.EventSubEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		result = eventStatusInvalidJSON
		WriteHTTPError(w, BadRequestError("invalid json", err))
		return
	}
	subscriptionType = env.Subscription.Type
	creatorID = env.Subscription.Condition.BroadcasterUserID
	logger.Debug("eventsub webhook parsed",
		"message_type", messageType,
		"sub_type", env.Subscription.Type,
		"broadcaster_id", env.Subscription.Condition.BroadcasterUserID,
		"user_id", env.Event.UserID,
		"user_login", env.Event.UserLogin,
	)

	if messageType == "webhook_callback_verification" {
		logger.Debug("eventsub webhook challenge accepted", "sub_type", env.Subscription.Type, "broadcaster_id", env.Subscription.Condition.BroadcasterUserID)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		// A write error here only means the client connection closed early.
		_, _ = w.Write([]byte(env.Challenge))
		result = eventStatusChallengeOK
		return
	}

	messageID := r.Header.Get("Twitch-Eventsub-Message-Id")
	if messageID == "" {
		result = eventStatusMissingMessageID
		WriteHTTPError(w, BadRequestError("missing message id", nil))
		return
	}

	alreadyProcessed, err := c.store.EventProcessed(ctx, messageID)
	if err != nil {
		result = eventStatusRedisError
		WriteHTTPError(w, BadGatewayError("redis error", err))
		return
	}
	if alreadyProcessed {
		logger.Debug("eventsub duplicate ignored", "message_id", messageID)
		w.WriteHeader(http.StatusOK)
		// A write error here only means the client connection closed early.
		_, _ = w.Write([]byte("duplicate ignored"))
		result = eventStatusDuplicate
		return
	}

	markProcessed := func() bool {
		alreadyProcessed, err := c.store.MarkEventProcessed(ctx, messageID, 24*time.Hour)
		if err != nil {
			result = eventStatusRedisError
			WriteHTTPError(w, BadGatewayError("redis error", err))
			return false
		}
		if alreadyProcessed {
			logger.Debug("eventsub duplicate observed after processing", "message_id", messageID)
		}
		return true
	}

	switch messageType {
	case "revocation":
		if !markProcessed() {
			return
		}
		logger.Warn("eventsub revocation received", "type", env.Subscription.Type, "creator_id", env.Subscription.Condition.BroadcasterUserID)
		w.WriteHeader(http.StatusNoContent)
		result = eventStatusRevocation
	case "notification":
		logger.Debug("eventsub notification received", "type", env.Subscription.Type, "broadcaster_id", env.Subscription.Condition.BroadcasterUserID, "user_id", env.Event.UserID)
		switch env.Subscription.Type {
		case core.EventTypeChannelSubscribe:
			if err := c.store.AddCreatorSubscriber(ctx, env.Subscription.Condition.BroadcasterUserID, env.Event.UserID); err != nil {
				result = eventStatusNotificationSubscribeStoreFailed
				WriteHTTPError(w, BadGatewayError("store error", err))
				return
			}
			// The subscriber cache update is the authoritative side effect here.
			// Proactive DMs are best-effort and must not fail the webhook.
			if c.subStart != nil {
				if err := c.subStart(
					ctx,
					env.Subscription.Condition.BroadcasterUserID,
					env.Event.BroadcasterUserLogin,
					env.Event.UserID,
					env.Event.UserLogin,
				); err != nil {
					logger.Warn("subscription start follow-up failed",
						"broadcaster_id", env.Subscription.Condition.BroadcasterUserID,
						"user_id", env.Event.UserID,
						"error", err,
					)
				}
			}
			if !markProcessed() {
				return
			}
			result = eventStatusNotificationSubscribe
		case core.EventTypeChannelSubEnd:
			// Subscription end revokes access, so a processing failure must fail
			// the webhook instead of silently skipping enforcement.
			if err := c.subEnd(
				ctx,
				env.Subscription.Condition.BroadcasterUserID,
				env.Event.BroadcasterUserLogin,
				env.Event.UserID,
				env.Event.UserLogin,
			); err != nil {
				result = eventStatusNotificationSubEndFailed
				WriteHTTPError(w, BadGatewayError("processing failed", err))
				return
			}
			if !markProcessed() {
				return
			}
			result = eventStatusNotificationSubEnd
		case core.EventTypeChannelBan:
			if c.blockBan != nil {
				if err := c.blockBan(ctx, env.Subscription.Condition.BroadcasterUserID, env.Event.UserID, env.Event.IsPermanent); err != nil {
					result = eventStatusNotificationBanFailed
					WriteHTTPError(w, BadGatewayError("processing failed", err))
					return
				}
			}
			if !markProcessed() {
				return
			}
			result = eventStatusNotificationBan
		case core.EventTypeChannelUnban:
			if c.blockUnban != nil {
				if err := c.blockUnban(ctx, env.Subscription.Condition.BroadcasterUserID, env.Event.UserID, true); err != nil {
					result = eventStatusNotificationUnbanFailed
					WriteHTTPError(w, BadGatewayError("processing failed", err))
					return
				}
			}
			if !markProcessed() {
				return
			}
			result = eventStatusNotificationUnban
		default:
			if !markProcessed() {
				return
			}
			result = eventStatusNotificationOther
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		if !markProcessed() {
			return
		}
		result = eventStatusIgnoredMessageType
		w.WriteHeader(http.StatusNoContent)
	}
}
