package core

import "fmt"

// InviteLinkErrorReason classifies invite-link creation failures for metrics and UX.
type InviteLinkErrorReason string

const (
	// InviteLinkErrorReasonNone marks successful invite-link creation.
	InviteLinkErrorReasonNone InviteLinkErrorReason = "none"
	// InviteLinkErrorReasonForbidden marks Telegram permission failures.
	InviteLinkErrorReasonForbidden InviteLinkErrorReason = "forbidden"
	// InviteLinkErrorReasonBadRequest marks invalid invite-link requests.
	InviteLinkErrorReasonBadRequest InviteLinkErrorReason = "bad_request"
	// InviteLinkErrorReasonRateLimited marks rate-limit or throttle failures.
	InviteLinkErrorReasonRateLimited InviteLinkErrorReason = "rate_limited"
	// InviteLinkErrorReasonUnknown marks uncategorized invite-link failures.
	InviteLinkErrorReasonUnknown InviteLinkErrorReason = "unknown"
)

// InviteLinkError wraps a join-link creation failure with a normalized reason.
type InviteLinkError struct {
	Reason InviteLinkErrorReason
	Err    error
}

func (e *InviteLinkError) Error() string {
	if e == nil || e.Err == nil {
		return "invite link error"
	}
	return fmt.Sprintf("invite link %s: %v", e.Reason, e.Err)
}

func (e *InviteLinkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
