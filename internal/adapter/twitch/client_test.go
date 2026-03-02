package twitch

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"imsub/internal/core"
	"imsub/internal/platform/config"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func testConfig() config.Config {
	return config.Config{
		TwitchClientID:       "cid",
		TwitchClientSecret:   "csecret",
		PublicBaseURL:        "https://example.com",
		TwitchWebhookPath:    "/webhooks/twitch",
		TwitchEventSubSecret: "evt-secret",
	}
}

func TestExchangeCode(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	client := NewClient(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Errorf("ExchangeCode request method = %q, want %q", req.Method, http.MethodPost)
			}
			if req.URL.Host != "id.twitch.tv" || req.URL.Path != "/oauth2/token" {
				t.Errorf("ExchangeCode request URL = %q, want host=%q path=%q", req.URL.String(), "id.twitch.tv", "/oauth2/token")
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			raw := string(body)
			for _, needle := range []string{
				"client_id=cid",
				"client_secret=csecret",
				"grant_type=authorization_code",
				"code=abc",
				"redirect_uri=https%3A%2F%2Fexample.com%2Fauth%2Fcallback",
			} {
				if !strings.Contains(raw, needle) {
					t.Errorf("ExchangeCode request body = %q, want substring %q", raw, needle)
				}
			}
			return response(http.StatusOK, `{"access_token":"at","refresh_token":"rt","scope":["s1"]}`), nil
		}),
	})

	got, err := client.ExchangeCode(t.Context(), "abc")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	want := core.TokenResponse{
		AccessToken:  "at",
		RefreshToken: "rt",
		Scope:        []string{"s1"},
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !slices.Equal(got.Scope, want.Scope) {
		t.Errorf("ExchangeCode(%q) = %+v, want %+v", "abc", got, want)
	}
}

func TestCreateEventSubAcceptedAndConflict(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	statuses := []int{http.StatusAccepted, http.StatusConflict}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			client := NewClient(cfg, &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Method != http.MethodPost {
						t.Errorf("CreateEventSub request method = %q, want %q", req.Method, http.MethodPost)
					}
					if req.URL.Host != "api.twitch.tv" || req.URL.Path != "/helix/eventsub/subscriptions" {
						t.Errorf("CreateEventSub request URL = %q, want host=%q path=%q", req.URL.String(), "api.twitch.tv", "/helix/eventsub/subscriptions")
					}
					return response(status, `{"ok":true}`), nil
				}),
			})

			err := client.CreateEventSub(t.Context(), "app-token", "b1", core.EventTypeChannelSubscribe, "1")
			if err != nil {
				t.Fatalf("CreateEventSub(%d) error: %v", status, err)
			}
		})
	}
}

func TestEnabledEventSubTypesPagination(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	call := 0
	client := NewClient(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			call++
			if req.Method != http.MethodGet {
				t.Errorf("EnabledEventSubTypes request method = %q, want %q", req.Method, http.MethodGet)
			}
			if req.URL.Host != "api.twitch.tv" || req.URL.Path != "/helix/eventsub/subscriptions" {
				t.Errorf("EnabledEventSubTypes request URL = %q, want host=%q path=%q", req.URL.String(), "api.twitch.tv", "/helix/eventsub/subscriptions")
			}
			switch call {
			case 1:
				return response(http.StatusOK, `{
					"data":[{"type":"channel.subscribe","condition":{"broadcaster_user_id":"111"}}],
					"pagination":{"cursor":"c2"}
				}`), nil
			case 2:
				return response(http.StatusOK, `{
					"data":[{"type":"channel.subscription.end","condition":{"broadcaster_user_id":"111"}}],
					"pagination":{"cursor":""}
				}`), nil
			default:
				t.Errorf("EnabledEventSubTypes request call count = %d, want <= 2", call)
				return nil, nil
			}
		}),
	})

	got, err := client.EnabledEventSubTypes(t.Context(), "app-token", "111")
	if err != nil {
		t.Fatalf("EnabledEventSubTypes returned error: %v", err)
	}
	if !got[core.EventTypeChannelSubscribe] || !got[core.EventTypeChannelSubEnd] {
		t.Errorf("EnabledEventSubTypes(%q) = %#v, want both required types enabled", "111", got)
	}
}

func TestListSubscriberPageUnauthorized(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	client := NewClient(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "api.twitch.tv" || req.URL.Path != "/helix/subscriptions" {
				t.Errorf("ListSubscriberPage request URL = %q, want host=%q path=%q", req.URL.String(), "api.twitch.tv", "/helix/subscriptions")
			}
			return response(http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
		}),
	})

	_, _, err := client.ListSubscriberPage(t.Context(), "access", "broadcaster", "")
	if !errors.Is(err, core.ErrUnauthorized) {
		t.Errorf("ListSubscriberPage(%q, %q, %q) error = %v, want errors.Is(..., core.ErrUnauthorized)=true", "access", "broadcaster", "", err)
	}
}

func TestListSubscriberPageSuccess(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	client := NewClient(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("broadcaster_id") != "broadcaster" {
				t.Errorf("ListSubscriberPage request query broadcaster_id = %q, want %q", req.URL.Query().Get("broadcaster_id"), "broadcaster")
			}
			return response(http.StatusOK, `{
				"data":[{"user_id":"u1"},{"user_id":"u2"}],
				"pagination":{"cursor":"next"}
			}`), nil
		}),
	})

	userIDs, cursor, err := client.ListSubscriberPage(t.Context(), "access", "broadcaster", "")
	if err != nil {
		t.Fatalf("ListSubscriberPage returned error: %v", err)
	}
	if len(userIDs) != 2 || userIDs[0] != "u1" || userIDs[1] != "u2" {
		t.Errorf("ListSubscriberPage(%q, %q, %q) userIDs = %#v, want %#v", "access", "broadcaster", "", userIDs, []string{"u1", "u2"})
	}
	if cursor != "next" {
		t.Errorf("ListSubscriberPage(%q, %q, %q) cursor = %q, want %q", "access", "broadcaster", "", cursor, "next")
	}
}
