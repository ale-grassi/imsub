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
		Steps:   []string{"Go back to Telegram.", "Use /start."},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("RenderOAuthError(page).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Problem") || !strings.Contains(body, "Something failed") || !strings.Contains(body, "Return to Telegram.") || !strings.Contains(body, "Use /start.") {
		t.Errorf("RenderOAuthError(page).Body = %q, want body containing title, message, hint, and steps", body)
	}
}

func TestRenderOAuthErrorOmitsHintWhenEmpty(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	RenderOAuthError(rec, OAuthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Problem",
		Message: "Something failed",
		Steps:   []string{"Go back to Telegram.", "Use /start."},
	})

	body := rec.Body.String()
	if strings.Contains(body, `class="hint"`) {
		t.Errorf("RenderOAuthError(page without hint).Body = %q, want no hint paragraph", body)
	}
	if !strings.Contains(body, "Use /start.") {
		t.Errorf("RenderOAuthError(page without hint).Body = %q, want body containing steps", body)
	}
}
