// Package httputil provides shared HTTP utilities used across platform and
// transport layers: request ID propagation, client IP extraction, response
// status recording, route labeling, and label helpers for metrics.
package httputil //nolint:revive // intentional naming

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const unknownLabel = "unknown"

// --- Request ID ---

type requestIDKeyType struct{}

var requestIDKey requestIDKeyType

// RequestID extracts an existing request ID from Fly-Request-Id or X-Request-Id headers.
func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if rid := strings.TrimSpace(r.Header.Get("Fly-Request-Id")); rid != "" {
		return rid
	}
	if rid := strings.TrimSpace(r.Header.Get("X-Request-Id")); rid != "" {
		return rid
	}
	return ""
}

// NewRequestID generates a cryptographically random hex request ID.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// --- Request ID context ---

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, rid string) context.Context {
	if ctx == nil || rid == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, rid)
}

// RequestIDFromContext retrieves the request ID from a context, or "".
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return v
}

// --- Client IP ---

// ClientIP extracts the client IP address from common proxy headers,
// falling back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if r == nil {
		return unknownLabel
	}
	if ip := strings.TrimSpace(r.Header.Get("Fly-Client-IP")); ip != "" {
		return ip
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return unknownLabel
}

// --- Status recorder ---

// StatusRecorder is an http.ResponseWriter wrapper that captures the
// response status code and byte count.
type StatusRecorder struct {
	http.ResponseWriter
	Status int
	Bytes  int
}

// WriteHeader captures the status code before delegating to the wrapped writer.
func (r *StatusRecorder) WriteHeader(code int) {
	r.Status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write captures the byte count before delegating to the wrapped writer.
func (r *StatusRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.Bytes += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility.
func (r *StatusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// --- Route label ---

// RouteLabel returns the matched route pattern or request path for use
// as a Prometheus metric label.
func RouteLabel(r *http.Request) string {
	if r == nil {
		return unknownLabel
	}
	pattern := strings.TrimSpace(r.Pattern)
	if pattern != "" {
		return pattern
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		return unknownLabel
	}
	return path
}

// LabelOrUnknown returns v trimmed, or "unknown" if blank.
func LabelOrUnknown(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return unknownLabel
	}
	return v
}
