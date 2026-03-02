package handlers

import (
	"net/http"
	"strings"

	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/transport/http/pages"
)

// renderOAuthError writes a user-facing OAuth error page.
func renderOAuthError(w http.ResponseWriter, page oauthErrorPage) {
	pages.RenderOAuthError(w, page)
}

type oauthErrorPage = pages.OAuthErrorPage

// OAuthStart validates state and renders the Twitch authorization launch page.
func (c *Controller) OAuthStart(w http.ResponseWriter, r *http.Request) {
	state := r.PathValue("state")
	if strings.TrimSpace(state) == "" {
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Missing Twitch link",
			Message: "This Twitch link is incomplete.",
			Hint:    "Return to Telegram and request a new link.",
		})
		return
	}

	payload, err := c.store.OAuthState(r.Context(), state)
	if err != nil {
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Link expired",
			Message: "This Twitch link is no longer valid.",
			Hint:    "Return to Telegram and request a new link.",
		})
		return
	}

	scope := ""
	switch payload.Mode {
	case core.OAuthModeViewer:
		scope = ""
	case core.OAuthModeCreator:
		scope = core.ScopeChannelReadSubscriptions
	default:
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Unknown link type",
			Message: "This Twitch link could not be recognized.",
			Hint:    "Return to Telegram and start the connection flow again.",
		})
		return
	}

	oauthURL := twitch.OAuthURL(c.cfg.TwitchClientID, c.cfg.PublicBaseURL+"/auth/callback", state, scope)
	c.renderOAuthLaunchPage(w, oauthURL)
}
