package handlers

import (
	"errors"
	"net/http"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/http/pages"
)

var (
	oauthErrorDenied = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Twitch authorization canceled",
		Message: "Twitch authorization did not complete.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Start the same connection flow again.",
			"Approve the Twitch prompt when it opens.",
		},
		Hint: "If you closed the browser or denied access, the previous attempt will not finish.",
	}
	oauthErrorMissingResponse = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Missing Twitch response",
		Message: "The Twitch callback did not include the required details.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Start the same connection flow again.",
			"Complete the Twitch login in the same browser session.",
		},
		Hint: "If you opened an old or partial link, restart the flow from Telegram.",
	}
	oauthErrorExpiredLink = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Twitch link expired",
		Message: "This Twitch authorization link expired, was already used, or was cleared before Twitch redirected back.",
		Steps:   oauthNeutralRecoverySteps,
		Hint:    "This link can only be used once. Go back to Telegram to start again.",
	}
	oauthErrorUnknownLinkType = oauthErrorPage{
		Status:  http.StatusBadRequest,
		Title:   "Unknown link type",
		Message: "This Twitch link could not be recognized.",
		Steps:   oauthNeutralRecoverySteps,
		Hint:    "If you opened an older link, discard it and restart from Telegram.",
	}
	oauthErrorViewerSaveFailed = oauthErrorPage{
		Status:  http.StatusConflict,
		Title:   "Could not link account",
		Message: "Your Twitch account could not be linked right now.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Use /start to get a new Twitch link.",
			"If the wrong Twitch account was used, run /reset before trying again.",
		},
	}
	oauthErrorVerificationFailed = oauthErrorPage{
		Status:  http.StatusBadGateway,
		Title:   "Verification failed",
		Message: "ImSub could not finish Twitch verification.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Use /start to retry in a moment.",
		},
		Hint: "If the problem keeps happening, wait a moment and run /start again.",
	}
	oauthErrorMissingCreatorScope = oauthErrorPage{
		Status:  http.StatusForbidden,
		Title:   "Missing Twitch permission",
		Message: "The required Twitch creator permission was not granted.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Use /creator to start the creator connection again.",
			"Approve the requested Twitch permissions.",
		},
	}
	oauthErrorCreatorSetupFailed = oauthErrorPage{
		Status:  http.StatusBadGateway,
		Title:   "Creator setup failed",
		Message: "ImSub could not finish creator setup.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Use /creator to retry the creator connection.",
		},
		Hint: "If the problem keeps happening, wait a moment and run /creator again.",
	}
	oauthErrorCreatorMismatch = oauthErrorPage{
		Status:  http.StatusConflict,
		Title:   "Wrong Twitch creator account",
		Message: "This reconnect used a different Twitch creator account than the one already linked.",
		Steps: []string{
			"Go back to the Telegram chat with ImSub.",
			"Use /creator with the creator account that is already linked.",
			"If you want to replace the linked creator account, run /reset first.",
		},
	}
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
