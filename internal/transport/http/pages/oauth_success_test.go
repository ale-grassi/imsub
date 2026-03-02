package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderOAuthSuccess(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	RenderOAuthSuccess(rec, "Title", "Message", "alice")

	if rec.Code != http.StatusOK {
		t.Errorf("RenderOAuthSuccess(...).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Title") || !strings.Contains(body, "Message") || !strings.Contains(body, "alice") {
		t.Errorf("RenderOAuthSuccess(...).Body = %q, want body containing %q, %q, and %q", body, "Title", "Message", "alice")
	}
}

func TestRenderOAuthError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	RenderOAuthError(rec, OAuthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Problem",
		Message: "Something failed",
		Hint:    "Return to Telegram.",
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("RenderOAuthError(page).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Problem") || !strings.Contains(body, "Something failed") || !strings.Contains(body, "Return to Telegram.") {
		t.Errorf("RenderOAuthError(page).Body = %q, want body containing title, message, and hint", body)
	}
}
