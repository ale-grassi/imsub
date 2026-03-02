package handlers

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError carries transport-safe status/message while preserving root cause.
type HTTPError struct {
	Status  int
	Message string
	Cause   error
}

// Error returns the message with its wrapped cause, if present.
func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

// Unwrap returns the wrapped cause.
func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WriteHTTPError writes a transport-safe HTTP response for err.
func WriteHTTPError(w http.ResponseWriter, err error) {
	var he *HTTPError
	if errors.As(err, &he) {
		http.Error(w, he.Message, he.Status)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// BadRequestError builds a 400 HTTPError.
func BadRequestError(message string, cause error) error {
	return &HTTPError{Status: http.StatusBadRequest, Message: message, Cause: cause}
}

// UnauthorizedError builds a 401 HTTPError.
func UnauthorizedError(message string, cause error) error {
	return &HTTPError{Status: http.StatusUnauthorized, Message: message, Cause: cause}
}

// ForbiddenError builds a 403 HTTPError.
func ForbiddenError(message string, cause error) error {
	return &HTTPError{Status: http.StatusForbidden, Message: message, Cause: cause}
}

// BadGatewayError builds a 502 HTTPError.
func BadGatewayError(message string, cause error) error {
	return &HTTPError{Status: http.StatusBadGateway, Message: message, Cause: cause}
}

// ServiceUnavailableError builds a 503 HTTPError.
func ServiceUnavailableError(message string, cause error) error {
	return &HTTPError{Status: http.StatusServiceUnavailable, Message: message, Cause: cause}
}

// ConflictError builds a 409 HTTPError.
func ConflictError(message string, cause error) error {
	return &HTTPError{Status: http.StatusConflict, Message: message, Cause: cause}
}
