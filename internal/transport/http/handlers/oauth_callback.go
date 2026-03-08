package handlers

import (
	"errors"
	"net/http"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/http/pages"
)

var (
	oauthErrorDenied = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Twitch authorization canceled",
		Message: "Twitch authorization did not complete.",
		Hint:    "Return to Telegram and start the connection again.",
	}
	oauthErrorMissingResponse = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Missing Twitch response",
		Message: "The Twitch callback did not include the required details.",
		Hint:    "Return to Telegram and try the connection again.",
	}
	oauthErrorExpiredLink = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Twitch link expired",
		Message: "This Twitch link has expired or was already used.",
		Hint:    "Return to Telegram and request a new link.",
	}
	oauthErrorUnknownLinkType = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Unknown link type",
		Message: "This Twitch link could not be recognized.",
		Hint:    "Return to Telegram and start the connection flow again.",
	}
	oauthErrorViewerSaveFailed = oauthErrorPage{
		Status:  http.StatusConflict,
		Title:   "Could not link account",
		Message: "Your Twitch account could not be linked right now.",
		Hint:    "Return to Telegram and try again. If the wrong Twitch account was used, run /reset first.",
	}
	oauthErrorVerificationFailed = oauthErrorPage{
		Status:  http.StatusBadGateway,
		Title:   "Verification failed",
		Message: "ImSub could not finish Twitch verification.",
		Hint:    "Return to Telegram and try again in a moment.",
	}
	oauthErrorMissingCreatorScope = oauthErrorPage{
		Status:  http.StatusForbidden,
		Title:   "Missing Twitch permission",
		Message: "The required Twitch creator permission was not granted.",
		Hint:    "Return to Telegram, start /creator again, and approve the requested access.",
	}
	oauthErrorCreatorSetupFailed = oauthErrorPage{
		Status:  http.StatusBadGateway,
		Title:   "Creator setup failed",
		Message: "ImSub could not finish creator setup.",
		Hint:    "Return to Telegram and try /creator again in a moment.",
	}
	oauthErrorCreatorMismatch = oauthErrorPage{
		Status:  http.StatusConflict,
		Title:   "Wrong Twitch creator account",
		Message: "This reconnect used a different Twitch creator account than the one already linked.",
		Hint:    "Return to Telegram. If you want to replace the creator account, run /reset first.",
	}
)

// TwitchCallback completes OAuth callback processing for viewer and creator flows.
func (c *Controller) TwitchCallback(w http.ResponseWriter, r *http.Request) {
	logger := c.logCtx(r.Context())
	logger.Debug("twitch callback received", "method", r.Method, "path", r.URL.Path, "has_state", r.URL.Query().Get("state") != "", "has_code", r.URL.Query().Get("code") != "")
	modeLabel := eventStatusUnknown
	resultLabel := eventStatusError
	defer func() {
		if c.obs != nil {
			c.obs.OAuthCallback(modeLabel, resultLabel)
		}
	}()
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		resultLabel = "denied"
		renderOAuthError(w, oauthErrorDenied)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		resultLabel = "missing_params"
		renderOAuthError(w, oauthErrorMissingResponse)
		return
	}

	ctx := r.Context()
	payload, err := c.store.DeleteOAuthState(ctx, state)
	if err != nil {
		resultLabel = "state_missing"
		renderOAuthError(w, oauthErrorExpiredLink)
		return
	}
	lang := i18n.NormalizeLanguage(payload.Language)

	switch payload.Mode {
	case core.OAuthModeViewer:
		modeLabel = string(core.OAuthModeViewer)
		logger.Debug("twitch callback", "mode", "viewer", "telegram_user_id", payload.TelegramUserID)
		label, displayName, flowErr := c.viewer(ctx, code, payload, lang)
		if flowErr != nil {
			var fe *core.FlowError
			if errors.As(flowErr, &fe) {
				switch fe.Kind {
				case core.KindSave:
					renderOAuthError(w, oauthErrorViewerSaveFailed)
				case core.KindTokenExchange, core.KindUserInfo, core.KindScopeMissing, core.KindStore, core.KindCreatorMismatch:
					renderOAuthError(w, oauthErrorVerificationFailed)
				}
			} else {
				renderOAuthError(w, oauthErrorVerificationFailed)
			}
			resultLabel = label
			return
		}
		pages.RenderOAuthSuccess(w, "Account linked", "Your Twitch account has been linked successfully.", displayName)
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
					renderOAuthError(w, oauthErrorMissingCreatorScope)
				case core.KindCreatorMismatch:
					renderOAuthError(w, oauthErrorCreatorMismatch)
				case core.KindTokenExchange, core.KindUserInfo, core.KindSave, core.KindStore:
					renderOAuthError(w, oauthErrorCreatorSetupFailed)
				}
			} else {
				renderOAuthError(w, oauthErrorCreatorSetupFailed)
			}
			resultLabel = label
			return
		}
		pages.RenderOAuthSuccess(w, "Creator registered", "You can now return to Telegram to manage your groups.", creatorName)
		resultLabel = label
	default:
		modeLabel = string(payload.Mode)
		resultLabel = "unknown_mode"
		renderOAuthError(w, oauthErrorUnknownLinkType)
	}
}
