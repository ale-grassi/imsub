package handlers

import (
	"fmt"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/http/pages"
)

type oauthPageRole string

const (
	oauthPageRoleUnknown          oauthPageRole = "unknown"
	oauthPageRoleViewer           oauthPageRole = "viewer"
	oauthPageRoleCreator          oauthPageRole = "creator"
	oauthPageRoleCreatorReconnect oauthPageRole = "creator_reconnect"

	webOAuthLaunchViewerTitleKey    = "web_oauth_launch_viewer_title"
	webOAuthLaunchViewerMessageKey  = "web_oauth_launch_viewer_message"
	webOAuthSuccessViewerTitleKey   = "web_oauth_success_viewer_title"
	webOAuthSuccessViewerMessageKey = "web_oauth_success_viewer_message"
	webOAuthSuccessViewerNextKey    = "web_oauth_success_viewer_next"
)

func oauthPageRoleFromPayload(payload core.OAuthStatePayload) oauthPageRole {
	switch payload.Mode {
	case core.OAuthModeViewer:
		return oauthPageRoleViewer
	case core.OAuthModeCreator:
		if payload.Reconnect {
			return oauthPageRoleCreatorReconnect
		}
		return oauthPageRoleCreator
	default:
		return oauthPageRoleUnknown
	}
}

func oauthPageLanguage(payload core.OAuthStatePayload) string {
	return i18n.NormalizeLanguage(payload.Language)
}

func oauthRetryCommand(role oauthPageRole) string {
	switch role {
	case oauthPageRoleViewer:
		return "/start"
	case oauthPageRoleCreator, oauthPageRoleCreatorReconnect:
		return "/creator"
	case oauthPageRoleUnknown:
		return ""
	default:
		return ""
	}
}

func oauthLaunchPage(lang string, role oauthPageRole, oauthURL string) pages.OAuthLaunchPage {
	var titleKey string
	var messageKey string
	switch role {
	case oauthPageRoleUnknown, oauthPageRoleViewer:
		titleKey = webOAuthLaunchViewerTitleKey
		messageKey = webOAuthLaunchViewerMessageKey
	case oauthPageRoleCreator:
		titleKey = "web_oauth_launch_creator_title"
		messageKey = "web_oauth_launch_creator_message"
	case oauthPageRoleCreatorReconnect:
		titleKey = "web_oauth_launch_creator_reconnect_title"
		messageKey = "web_oauth_launch_creator_reconnect_message"
	}

	return pages.OAuthLaunchPage{
		Lang:        lang,
		Title:       i18n.Translate(lang, titleKey),
		Message:     i18n.Translate(lang, messageKey),
		OAuthURL:    oauthURL,
		ButtonLabel: i18n.Translate(lang, "web_oauth_launch_button"),
		CopyLabel:   i18n.Translate(lang, "web_oauth_launch_copy_button"),
		CopyIdle:    i18n.Translate(lang, "web_oauth_launch_copy_idle"),
		CopyDone:    i18n.Translate(lang, "web_oauth_launch_copy_done"),
	}
}

func oauthSuccessPage(lang string, role oauthPageRole, username string) pages.OAuthSuccessPage {
	var titleKey string
	var messageKey string
	var nextKey string
	switch role {
	case oauthPageRoleUnknown, oauthPageRoleViewer:
		titleKey = webOAuthSuccessViewerTitleKey
		messageKey = webOAuthSuccessViewerMessageKey
		nextKey = webOAuthSuccessViewerNextKey
	case oauthPageRoleCreator:
		titleKey = "web_oauth_success_creator_title"
		messageKey = "web_oauth_success_creator_message"
		nextKey = "web_oauth_success_creator_next"
	case oauthPageRoleCreatorReconnect:
		titleKey = "web_oauth_success_creator_reconnect_title"
		messageKey = "web_oauth_success_creator_reconnect_message"
		nextKey = "web_oauth_success_creator_reconnect_next"
	}

	return pages.OAuthSuccessPage{
		Lang:     lang,
		Title:    i18n.Translate(lang, titleKey),
		Message:  i18n.Translate(lang, messageKey),
		Username: username,
		NextStep: i18n.Translate(lang, nextKey),
	}
}

func oauthInvalidLinkPage(lang string, role oauthPageRole) pages.OAuthErrorPage {
	return pages.OAuthErrorPage{
		Lang:     lang,
		Status:   httpStatusInvalidLink,
		Title:    i18n.Translate(lang, "web_oauth_invalid_link_title"),
		Message:  i18n.Translate(lang, "web_oauth_invalid_link_message"),
		NextStep: oauthRetryNextStep(lang, role),
	}
}

func oauthAuthIncompletePage(lang string, role oauthPageRole) pages.OAuthErrorPage {
	return pages.OAuthErrorPage{
		Lang:     lang,
		Status:   httpStatusAuthIncomplete,
		Title:    i18n.Translate(lang, "web_oauth_auth_incomplete_title"),
		Message:  i18n.Translate(lang, "web_oauth_auth_incomplete_message"),
		NextStep: oauthRetryNextStep(lang, role),
	}
}

func oauthCreatorPermissionPage(lang string, role oauthPageRole) pages.OAuthErrorPage {
	return pages.OAuthErrorPage{
		Lang:     lang,
		Status:   httpStatusCreatorPermission,
		Title:    i18n.Translate(lang, "web_oauth_auth_incomplete_title"),
		Message:  i18n.Translate(lang, "web_oauth_auth_incomplete_message"),
		NextStep: oauthFormat(lang, "web_oauth_problem_next_creator_permissions", oauthRetryCommand(role)),
	}
}

func oauthTemporaryFailurePage(lang string, role oauthPageRole, status int) pages.OAuthErrorPage {
	nextKey := "web_oauth_problem_next_generic"
	args := []any{}
	if cmd := oauthRetryCommand(role); cmd != "" {
		nextKey = "web_oauth_problem_next_retry_command"
		args = append(args, cmd)
	}
	return pages.OAuthErrorPage{
		Lang:     lang,
		Status:   status,
		Title:    i18n.Translate(lang, "web_oauth_temporary_failure_title"),
		Message:  i18n.Translate(lang, "web_oauth_temporary_failure_message"),
		NextStep: oauthFormat(lang, nextKey, args...),
	}
}

func oauthCreatorMismatchPage(lang string) pages.OAuthErrorPage {
	return pages.OAuthErrorPage{
		Lang:     lang,
		Status:   httpStatusCreatorMismatch,
		Title:    i18n.Translate(lang, "web_oauth_creator_mismatch_title"),
		Message:  i18n.Translate(lang, "web_oauth_creator_mismatch_message"),
		NextStep: oauthFormat(lang, "web_oauth_creator_mismatch_next", "/creator", "/reset"),
	}
}

func oauthRetryNextStep(lang string, role oauthPageRole) string {
	if cmd := oauthRetryCommand(role); cmd != "" {
		return oauthFormat(lang, "web_oauth_problem_next_command", cmd)
	}
	return i18n.Translate(lang, "web_oauth_problem_next_generic")
}

func oauthFormat(lang, key string, args ...any) string {
	return fmt.Sprintf(i18n.Translate(lang, key), args...)
}

const (
	httpStatusInvalidLink       = 400
	httpStatusAuthIncomplete    = 400
	httpStatusCreatorPermission = 403
	httpStatusCreatorMismatch   = 409
)
