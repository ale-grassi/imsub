package handlers

import (
	"errors"
	"net/http"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/http/pages"
)

// TwitchCallback completes OAuth callback processing for viewer and creator flows.
func (c *Controller) TwitchCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := c.logCtx(r.Context())
	logger.Debug("twitch callback received", "method", r.Method, "path", r.URL.Path, "has_state", r.URL.Query().Get("state") != "", "has_code", r.URL.Query().Get("code") != "")
	modeLabel := eventStatusUnknown
	resultLabel := eventStatusError
	defer func() {
		if c.events != nil {
			c.events.Emit(ctx, events.Event{
				Name:    events.NameOAuthCallback,
				Outcome: resultLabel,
				Fields:  map[string]string{"mode": modeLabel},
			})
		}
	}()
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	role := oauthPageRoleUnknown
	lang := i18n.DefaultLanguage
	if state != "" {
		if previewPayload, err := c.store.OAuthState(ctx, state); err == nil {
			role = oauthPageRoleFromPayload(previewPayload)
			lang = oauthPageLanguage(previewPayload)
		}
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		resultLabel = "denied"
		pages.RenderOAuthError(w, oauthAuthIncompletePage(lang, role))
		return
	}

	if state == "" || code == "" {
		resultLabel = "missing_params"
		pages.RenderOAuthError(w, oauthAuthIncompletePage(lang, role))
		return
	}

	payload, err := c.store.DeleteOAuthState(ctx, state)
	if err != nil {
		resultLabel = "state_missing"
		pages.RenderOAuthError(w, oauthInvalidLinkPage(lang, role))
		return
	}
	lang = i18n.NormalizeLanguage(payload.Language)
	role = oauthPageRoleFromPayload(payload)

	switch payload.Mode {
	case core.OAuthModeViewer:
		modeLabel = string(core.OAuthModeViewer)
		logger.Debug("twitch callback", "mode", "viewer", "telegram_user_id", payload.TelegramUserID)
		label, displayName, flowErr := c.viewer(ctx, code, payload, lang)
		if flowErr != nil {
			var fe *core.FlowError
			if errors.As(flowErr, &fe) {
				switch fe.Kind {
				case core.KindTokenExchange, core.KindUserInfo, core.KindScopeMissing, core.KindStore, core.KindCreatorMismatch:
					pages.RenderOAuthError(w, oauthTemporaryFailurePage(lang, role, http.StatusBadGateway))
				case core.KindSave:
					pages.RenderOAuthError(w, oauthTemporaryFailurePage(lang, role, http.StatusConflict))
				}
			} else {
				pages.RenderOAuthError(w, oauthTemporaryFailurePage(lang, role, http.StatusBadGateway))
			}
			resultLabel = label
			return
		}
		pages.RenderOAuthSuccess(w, oauthSuccessPage(lang, role, displayName))
		resultLabel = label
	case core.OAuthModeCreator:
		modeLabel = string(core.OAuthModeCreator)
		logger.Debug("twitch callback", "mode", "creator", "telegram_user_id", payload.TelegramUserID)
		label, creatorName, flowErr := c.creator(ctx, code, payload, lang)
		if flowErr != nil {
			var fe *core.FlowError
			if errors.As(flowErr, &fe) {
				switch fe.Kind {
				case core.KindScopeMissing:
					pages.RenderOAuthError(w, oauthCreatorPermissionPage(lang, role))
				case core.KindCreatorMismatch:
					pages.RenderOAuthError(w, oauthCreatorMismatchPage(lang))
				case core.KindTokenExchange, core.KindUserInfo, core.KindSave, core.KindStore:
					pages.RenderOAuthError(w, oauthTemporaryFailurePage(lang, role, http.StatusBadGateway))
				}
			} else {
				pages.RenderOAuthError(w, oauthTemporaryFailurePage(lang, role, http.StatusBadGateway))
			}
			resultLabel = label
			return
		}
		pages.RenderOAuthSuccess(w, oauthSuccessPage(lang, role, creatorName))
		resultLabel = label
	default:
		modeLabel = string(payload.Mode)
		resultLabel = "unknown_mode"
		pages.RenderOAuthError(w, oauthInvalidLinkPage(lang, oauthPageRoleUnknown))
	}
}
