package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	"imsub/internal/events"

	"github.com/mymmrac/telego"
)

// TelegramWebhook validates and enqueues incoming Telegram webhook updates.
func (c *Controller) TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result := "error"
	defer func() {
		if c.events != nil {
			c.events.Emit(ctx, events.Event{
				Name:    events.NameTelegramWebhook,
				Outcome: result,
			})
		}
	}()

	if c.cfg.TelegramWebhookSecret != "" {
		headerToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(headerToken), []byte(c.cfg.TelegramWebhookSecret)) != 1 {
			result = "unauthorized"
			WriteHTTPError(w, UnauthorizedError("invalid telegram secret token", nil))
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		result = "bad_body"
		WriteHTTPError(w, BadRequestError("bad body", err))
		return
	}

	var update telego.Update
	if err := json.Unmarshal(body, &update); err != nil {
		result = "invalid_json"
		WriteHTTPError(w, BadRequestError("invalid json", err))
		return
	}

	if c.updates == nil {
		result = "updates_channel_unavailable"
		WriteHTTPError(w, ServiceUnavailableError("telegram updates channel unavailable", nil))
		return
	}

	select {
	case c.updates <- update:
		result = "ok"
		w.WriteHeader(http.StatusOK)
		// A write error here only means the client connection closed early.
		_, _ = w.Write([]byte("ok"))
	default:
		result = "queue_full"
		WriteHTTPError(w, ServiceUnavailableError("telegram queue full", nil))
	}
}
