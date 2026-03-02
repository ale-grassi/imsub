package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/mymmrac/telego"
)

// TelegramWebhook validates and enqueues incoming Telegram webhook updates.
func (c *Controller) TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	result := "error"
	defer func() {
		if c.obs != nil {
			c.obs.TelegramWebhookResult(result)
		}
	}()

	if c.cfg.TelegramWebhookSecret != "" {
		if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != c.cfg.TelegramWebhookSecret {
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
