package handlers

import (
	"net/http"
	"strings"

	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/transport/http/pages"
)

// renderOAuthError writes a user-facing OAuth error page.
func renderOAuthError(w http.ResponseWriter, page oauthErrorPage) {
	pages.RenderOAuthError(w, page)
}

type oauthErrorPage = pages.OAuthErrorPage

const (
	oauthStartResultMissingState = "missing_state"
	oauthStartResultUnknownMode  = "unknown_mode"
	oauthStartResultOK           = "ok"
)

// oauthNeutralRecoverySteps are the role-agnostic recovery steps rendered on
// OAuth error pages where the state payload (and therefore the mode) is not
// yet resolved or has already been consumed.
var oauthNeutralRecoverySteps = []string{
	"Go back to the Telegram chat where you opened this link.",
	"Restart the same connection flow to get a new link.",
}

// OAuthStart validates state and renders the Twitch authorization launch page.
func (c *Controller) OAuthStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := r.PathValue("state")
	modeLabel := eventStatusUnknown
	resultLabel := eventStatusError
	defer func() {
		if c.events != nil {
			c.events.Emit(ctx, events.Event{
				Name:    events.NameOAuthStart,
				Outcome: resultLabel,
				Fields:  map[string]string{"mode": modeLabel},
			})
		}
	}()
	if strings.TrimSpace(state) == "" {
		resultLabel = oauthStartResultMissingState
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Missing Twitch link",
			Message: "This Twitch link is incomplete.",
			Steps:   oauthNeutralRecoverySteps,
			Hint:    "If you tapped an incomplete or outdated link, restart from Telegram.",
		})
		return
	}

	payload, err := c.store.OAuthState(ctx, state)
	if err != nil {
		resultLabel = oauthStartResultMissingState
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Twitch authorization link expired",
			Message: "This Twitch authorization link expired before it was opened.",
			Steps:   oauthNeutralRecoverySteps,
			Hint:    "If the link expired, go back to Telegram and start again.",
		})
		return
	}

	scope := ""
	switch payload.Mode {
	case core.OAuthModeViewer:
		modeLabel = string(core.OAuthModeViewer)
		scope = ""
	case core.OAuthModeCreator:
		modeLabel = string(core.OAuthModeCreator)
		scope = strings.Join([]string{core.ScopeChannelReadSubscriptions, core.ScopeModerationRead}, " ")
	default:
		modeLabel = string(payload.Mode)
		resultLabel = oauthStartResultUnknownMode
		renderOAuthError(w, oauthErrorPage{
			Status:  http.StatusBadRequest,
			Title:   "Unknown link type",
			Message: "This Twitch link could not be recognized.",
			Steps:   oauthNeutralRecoverySteps,
			Hint:    "Restart from Telegram to get a valid link.",
		})
		return
	}

	oauthURL := twitch.OAuthURL(c.cfg.TwitchClientID, c.cfg.PublicBaseURL+"/auth/callback", state, scope)
	resultLabel = oauthStartResultOK
	c.renderOAuthLaunchPage(w, oauthURL)
}
