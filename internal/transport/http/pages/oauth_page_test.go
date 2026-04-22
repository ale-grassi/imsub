package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imsub/internal/platform/i18n"
)

func TestRenderOAuthPageSuccess(t *testing.T) {
	t.Parallel()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	rec := httptest.NewRecorder()
	RenderOAuthSuccess(rec, OAuthSuccessPage{
		Lang:     "en",
		Title:    "Title",
		Message:  "Message",
		Username: "alice",
		NextStep: "Return to Telegram.",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("RenderOAuthSuccess(...).StatusCode = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Title") || !strings.Contains(body, "Message") || !strings.Contains(body, "alice") || !strings.Contains(body, "Return to Telegram.") {
		t.Errorf("RenderOAuthSuccess(...).Body = %q, want body containing title, message, username, and next step", body)
	}
}

func TestRenderOAuthPageError(t *testing.T) {
	t.Parallel()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	rec := httptest.NewRecorder()
	RenderOAuthError(rec, OAuthErrorPage{
		Lang:     "en",
		Status:   http.StatusBadRequest,
		Title:    "Problem",
		Message:  "Something failed",
		NextStep: "Use /start.",
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("RenderOAuthError(page).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Problem") || !strings.Contains(body, "Something failed") || !strings.Contains(body, "Use ") {
		t.Errorf("RenderOAuthError(page).Body = %q, want body containing title, message, and next step", body)
	}
	if !strings.Contains(body, `<code class="inline-command">/start</code>`) {
		t.Errorf("RenderOAuthError(page).Body = %q, want slash command highlighted as inline code", body)
	}
	if strings.Contains(body, `class="steps"`) || strings.Contains(body, `class="hint"`) {
		t.Errorf("RenderOAuthError(page).Body = %q, want minimal layout without steps or hint panels", body)
	}
}

func TestRenderOAuthPageLaunch(t *testing.T) {
	t.Parallel()
	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	rec := httptest.NewRecorder()
	RenderOAuthLaunch(rec, OAuthLaunchPage{
		Lang:        "en",
		Title:       "Connect Twitch",
		Message:     "Open Twitch.",
		OAuthURL:    "https://example.com/oauth",
		ButtonLabel: "Continue to Twitch",
		CopyLabel:   "Copy link",
		CopyIdle:    "If Twitch does not open, copy the link.",
		CopyDone:    "Link copied.",
	})

	body := rec.Body.String()
	if !strings.Contains(body, "Continue to Twitch") || !strings.Contains(body, "Copy link") || !strings.Contains(body, "https://example.com/oauth") {
		t.Errorf("RenderOAuthLaunch(...).Body = %q, want launch controls and OAuth URL", body)
	}
	if !strings.Contains(body, `id="copy-url" class="copy-box"`) {
		t.Errorf("RenderOAuthLaunch(...).Body = %q, want selectable URL copy box", body)
	}
}
