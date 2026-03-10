package bot

import (
	"testing"

	"imsub/internal/events"

	"github.com/mymmrac/telego"
)

func TestStartCommandTracksActivityAndEmitsCommandEvent(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 1,
		Message: &telego.Message{
			MessageID: 10,
			Text:      "/start",
			Chat: telego.Chat{
				ID:   42,
				Type: telego.ChatTypePrivate,
			},
			From: &telego.User{
				ID:           42,
				FirstName:    "Viewer",
				LanguageCode: "en",
			},
		},
	})

	touches := h.store.activeUserTouches()
	if len(touches) != 1 || touches[0] != 42 {
		t.Fatalf("active user touches = %v, want [42]", touches)
	}

	var found bool
	for _, evt := range h.events.snapshot() {
		if evt.Name == events.NameTelegramCommand &&
			evt.Fields["command"] == "start" &&
			evt.Fields["chat_type"] == "private" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected telegram command event for /start")
	}
}
