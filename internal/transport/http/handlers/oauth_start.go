package handlers

import (
	"net/http"
	"strings"

	"imsub/internal/adapter/twitch"
	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/transport/http/pages"
)

const (
	oauthStartResultMissingState = "missing_state"
	oauthStartResultUnknownMode  = "unknown_mode"
	oauthStartResultOK           = "ok"
)

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
		pages.RenderOAuthError(w, oauthInvalidLinkPage("en", oauthPageRoleUnknown))
		return
	}

	payload, err := c.store.OAuthState(ctx, state)
	if err != nil {
		resultLabel = oauthStartResultMissingState
		pages.RenderOAuthError(w, oauthInvalidLinkPage("en", oauthPageRoleUnknown))
		return
	}
	lang := oauthPageLanguage(payload)
	role := oauthPageRoleFromPayload(payload)

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
		pages.RenderOAuthError(w, oauthInvalidLinkPage(lang, oauthPageRoleUnknown))
		return
	}

	oauthURL := twitch.OAuthURL(c.cfg.TwitchClientID, c.cfg.PublicBaseURL+"/auth/callback", state, scope)
	resultLabel = oauthStartResultOK
	pages.RenderOAuthLaunch(w, oauthLaunchPage(lang, role, oauthURL))
}
