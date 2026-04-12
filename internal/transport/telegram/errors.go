// Package telegram provides shared Telegram transport helpers.
package telegram

import (
	"errors"
	"net/http"

	"github.com/mymmrac/telego/telegoapi"
)

// IsForbidden reports whether err is a Telegram API 403 Forbidden response.
func IsForbidden(err error) bool {
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == http.StatusForbidden
	}
	return false
}

// IsBadRequest reports whether err is a Telegram API 400 Bad Request response.
func IsBadRequest(err error) bool {
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == http.StatusBadRequest
	}
	return false
}

// IsTooManyRequests reports whether err is a Telegram API 429 Too Many Requests response.
func IsTooManyRequests(err error) bool {
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == http.StatusTooManyRequests
	}
	return false
}
