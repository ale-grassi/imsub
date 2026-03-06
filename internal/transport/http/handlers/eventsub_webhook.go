package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
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
	eventStatusNotificationSubscriptionGift     = "notification_subscription_gift"
	eventStatusNotificationSubEndFailed         = "notification_subscription_end_failed"
	eventStatusNotificationSubEnd               = "notification_subscription_end"
	eventStatusNotificationOther                = "notification_other"
	eventStatusIgnoredMessageType               = "ignored_message_type"
)

// EventSubWebhook verifies and processes Twitch EventSub webhook deliveries.
func (c *Controller) EventSubWebhook(w http.ResponseWriter, r *http.Request) {
	logger := c.logCtx(r.Context())
	logger.Debug("eventsub webhook received", "method", r.Method, "path", r.URL.Path)
	messageType := strings.TrimSpace(r.Header.Get("Twitch-Eventsub-Message-Type"))
	subscriptionType := eventStatusUnknown
	result := eventStatusError
	defer func() {
		if c.obs != nil {
			c.obs.EventSubMessage(messageType, subscriptionType, result)
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
	logger.Debug("eventsub webhook parsed",
		"message_type", messageType,
		"sub_type", env.Subscription.Type,
		"broadcaster_id", env.Subscription.Condition.BroadcasterUserID,
		"user_id", env.Event.UserID,
		"user_login", env.Event.UserLogin,
	)

	if messageType == "webhook_callback_verification" {
		logger.Debug("eventsub webhook challenge accepted", "sub_type", env.Subscription.Type, "broadcaster_id", env.Subscription.Condition.BroadcasterUserID)
		w.Header().Set("Content-Type", "text/plain")
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

	ctx := r.Context()
	alreadyProcessed, err := c.store.MarkEventProcessed(ctx, messageID, 24*time.Hour)
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

	switch messageType {
	case "revocation":
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
			result = eventStatusNotificationSubscribe
		case core.EventTypeChannelSubGift:
			// Gift events fire per gifter, not per recipient. Each individual
			// recipient triggers a separate channel.subscribe event which is
			// handled above, so no subscriber-cache mutation is needed here.
			logger.Info("eventsub gift sub received",
				"broadcaster_id", env.Subscription.Condition.BroadcasterUserID,
				"gifter_id", env.Event.UserID,
				"gifter_login", env.Event.UserLogin,
			)
			result = eventStatusNotificationSubscriptionGift
		case core.EventTypeChannelSubEnd:
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
			result = eventStatusNotificationSubEnd
		default:
			result = eventStatusNotificationOther
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		result = eventStatusIgnoredMessageType
		w.WriteHeader(http.StatusNoContent)
	}
}
