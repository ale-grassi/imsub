package tgerr

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mymmrac/telego/telegoapi"
)

func TestIsForbidden(t *testing.T) {
	t.Parallel()

	if !IsForbidden(&telegoapi.Error{ErrorCode: 403, Description: "Forbidden: bot was blocked"}) {
		t.Error("IsForbidden(telegoapi.Error{ErrorCode: 403}) = false, want true")
	}
	if !IsForbidden(fmt.Errorf("wrapped: %w", &telegoapi.Error{ErrorCode: 403})) {
		t.Error("IsForbidden(wrapped telegoapi.Error{ErrorCode: 403}) = false, want true")
	}
	if IsForbidden(nil) {
		t.Error("IsForbidden(nil) = true, want false")
	}
	if IsForbidden(errors.New("some other error")) {
		t.Error("IsForbidden(errors.New(\"some other error\")) = true, want false")
	}
}

func TestIsBadRequest(t *testing.T) {
	t.Parallel()

	if !IsBadRequest(&telegoapi.Error{ErrorCode: 400, Description: "Bad Request: message not modified"}) {
		t.Error("IsBadRequest(telegoapi.Error{ErrorCode: 400}) = false, want true")
	}
	if IsBadRequest(nil) {
		t.Error("IsBadRequest(nil) = true, want false")
	}
	if IsBadRequest(errors.New("some other error")) {
		t.Error("IsBadRequest(errors.New(\"some other error\")) = true, want false")
	}
}
