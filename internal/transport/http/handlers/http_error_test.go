package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteHTTPErrorUsesHTTPError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	WriteHTTPError(rec, BadRequestError("bad request", errors.New("boom")))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("WriteHTTPError(BadRequestError(...)).StatusCode = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "bad request") {
		t.Errorf("WriteHTTPError(BadRequestError(...)).Body = %q, want body containing %q", rec.Body.String(), "bad request")
	}
}

func TestWriteHTTPErrorFallsBackTo500(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	WriteHTTPError(rec, errors.New("generic"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("WriteHTTPError(errors.New(\"generic\")).StatusCode = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Errorf("WriteHTTPError(errors.New(\"generic\")).Body = %q, want body containing %q", rec.Body.String(), "internal error")
	}
}
