package httputil //nolint:revive // intentional naming

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "nil_request",
			req:  nil,
			want: "",
		},
		{
			name: "fly_header_precedence",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("Fly-Request-Id", " fly-id ")
				r.Header.Set("X-Request-Id", "x-id")
				return r
			}(),
			want: "fly-id",
		},
		{
			name: "x_request_id_fallback",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Request-Id", " x-id ")
				return r
			}(),
			want: "x-id",
		},
		{
			name: "missing_headers",
			req:  httptest.NewRequest(http.MethodGet, "/", nil),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequestID(tc.req); got != tc.want {
				t.Errorf("RequestID(req) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	for i := 0; i < 3; i++ {
		got := NewRequestID()
		if len(got) == 0 {
			t.Fatalf("NewRequestID() = %q, want non-empty string", got)
		}
		if _, err := hex.DecodeString(got); err != nil {
			t.Fatalf("NewRequestID() = %q, want hex-encoded string; decode error = %v", got, err)
		}
	}
}

func TestRequestIDContext(t *testing.T) {
	t.Parallel()

	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("RequestIDFromContext(nil) = %q, want %q", got, "")
	}

	base := context.Background()
	ctx := WithRequestID(base, "abc123")
	if got := RequestIDFromContext(ctx); got != "abc123" {
		t.Errorf("RequestIDFromContext(WithRequestID(ctx, %q)) = %q, want %q", "abc123", got, "abc123")
	}

	if got := WithRequestID(nil, "abc123"); got != nil { //nolint:staticcheck
		t.Errorf("WithRequestID(nil, %q) = %v, want nil", "abc123", got)
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "nil_request",
			req:  nil,
			want: unknownLabel,
		},
		{
			name: "fly_client_ip",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("Fly-Client-IP", " 203.0.113.10 ")
				return r
			}(),
			want: "203.0.113.10",
		},
		{
			name: "x_forwarded_for",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-For", "198.51.100.3, 10.0.0.1")
				return r
			}(),
			want: "198.51.100.3",
		},
		{
			name: "remote_addr_host_port",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "192.0.2.7:1234"
				return r
			}(),
			want: "192.0.2.7",
		},
		{
			name: "remote_addr_fallback",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "192.0.2.8"
				return r
			}(),
			want: "192.0.2.8",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientIP(tc.req); got != tc.want {
				t.Errorf("ClientIP(req) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusRecorder(t *testing.T) {
	t.Parallel()

	base := httptest.NewRecorder()
	rec := &StatusRecorder{
		ResponseWriter: base,
		Status:         http.StatusOK,
	}

	rec.WriteHeader(http.StatusCreated)
	if rec.Status != http.StatusCreated {
		t.Errorf("(*StatusRecorder).Status = %d, want %d", rec.Status, http.StatusCreated)
	}
	if got := base.Result().StatusCode; got != http.StatusCreated {
		t.Errorf("base.Result().StatusCode = %d, want %d", got, http.StatusCreated)
	}

	if _, err := rec.Write([]byte("hello")); err != nil {
		t.Fatalf("(*StatusRecorder).Write(%q) error = %v, want nil", "hello", err)
	}
	if rec.Bytes != 5 {
		t.Errorf("(*StatusRecorder).Bytes = %d, want %d", rec.Bytes, 5)
	}

	if got := rec.Unwrap(); got != base {
		t.Errorf("(*StatusRecorder).Unwrap() = %T, want %T", got, base)
	}
}

func TestRouteLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "nil_request",
			req:  nil,
			want: unknownLabel,
		},
		{
			name: "pattern_precedence",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/path", nil)
				r.Pattern = "/users/{id}"
				return r
			}(),
			want: "/users/{id}",
		},
		{
			name: "path_fallback",
			req:  httptest.NewRequest(http.MethodGet, "/path", nil),
			want: "/path",
		},
		{
			name: "empty_path",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.URL.Path = " "
				return r
			}(),
			want: unknownLabel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RouteLabel(tc.req); got != tc.want {
				t.Errorf("RouteLabel(req) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLabelOrUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "blank", in: "", want: unknownLabel},
		{name: "spaces", in: "   ", want: unknownLabel},
		{name: "trimmed", in: " value ", want: "value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LabelOrUnknown(tc.in); got != tc.want {
				t.Errorf("LabelOrUnknown(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
