package twitch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
			client.appToken = "app-token"
			client.appTokenExpires = time.Now().Add(time.Hour)

			err := client.CreateEventSub(t.Context(), "b1", core.EventTypeChannelSubscribe, "1")
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
			if req.URL.Query().Get("user_id") != "111" {
				t.Errorf("EnabledEventSubTypes request user_id = %q, want %q", req.URL.Query().Get("user_id"), "111")
			}
			switch call {
			case 1:
				return response(http.StatusOK, `{
					"data":[{"type":"channel.subscribe","condition":{"broadcaster_user_id":"111"}}],
					"pagination":{"cursor":"c2"}
				}`), nil
			case 2:
				return response(http.StatusOK, `{
					"data":[{"type":"channel.subscription.end","condition":{"broadcaster_user_id":"111"}},{"type":"channel.subscription.gift","condition":{"broadcaster_user_id":"111"}}],
					"pagination":{"cursor":""}
				}`), nil
			default:
				t.Errorf("EnabledEventSubTypes request call count = %d, want <= 2", call)
				return nil, errors.New("unexpected call")
			}
		}),
	})
	client.appToken = "app-token"
	client.appTokenExpires = time.Now().Add(time.Hour)

	got, err := client.EnabledEventSubTypes(t.Context(), "111")
	if err != nil {
		t.Fatalf("EnabledEventSubTypes returned error: %v", err)
	}
	if !got[core.EventTypeChannelSubscribe] || !got[core.EventTypeChannelSubEnd] || !got[core.EventTypeChannelSubGift] {
		t.Errorf("EnabledEventSubTypes(%q) = %#v, want all required types enabled", "111", got)
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

func TestAppAuthTokenCachesUntilExpiry(t *testing.T) {
	t.Parallel()

	var tokenCalls int32
	client := NewClient(testConfig(), &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "id.twitch.tv" || req.URL.Path != "/oauth2/token" {
				t.Fatalf("unexpected URL %q", req.URL.String())
			}
			atomic.AddInt32(&tokenCalls, 1)
			return response(http.StatusOK, `{"access_token":"app-1","expires_in":3600}`), nil
		}),
	})

	got1, err := client.appAuthToken(t.Context())
	if err != nil {
		t.Fatalf("appAuthToken() first error: %v", err)
	}
	got2, err := client.appAuthToken(t.Context())
	if err != nil {
		t.Fatalf("appAuthToken() second error: %v", err)
	}

	if got1 != "app-1" || got2 != "app-1" {
		t.Fatalf("appAuthToken() tokens = %q, %q, want app-1 both times", got1, got2)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
	}
}

func TestAppAuthTokenRefreshesWhenNearExpiry(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	now := base
	var tokenCalls int32
	client := NewClient(testConfig(), &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			call := atomic.AddInt32(&tokenCalls, 1)
			switch call {
			case 1:
				return response(http.StatusOK, `{"access_token":"app-1","expires_in":3600}`), nil
			case 2:
				return response(http.StatusOK, `{"access_token":"app-2","expires_in":3600}`), nil
			default:
				t.Fatalf("unexpected token call #%d", call)
				return nil, errors.New("unexpected call")
			}
		}),
	})
	client.now = func() time.Time { return now }

	got1, err := client.appAuthToken(t.Context())
	if err != nil {
		t.Fatalf("appAuthToken() first error: %v", err)
	}

	now = base.Add(time.Hour - appTokenRefreshSkew + time.Second)
	got2, err := client.appAuthToken(t.Context())
	if err != nil {
		t.Fatalf("appAuthToken() refresh error: %v", err)
	}

	if got1 != "app-1" || got2 != "app-2" {
		t.Fatalf("appAuthToken() tokens = %q, %q, want app-1 then app-2", got1, got2)
	}
	if atomic.LoadInt32(&tokenCalls) != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", tokenCalls)
	}
}

func TestAppAuthTokenConcurrentRefreshCollapsesCalls(t *testing.T) {
	t.Parallel()

	var tokenCalls int32
	release := make(chan struct{})
	client := NewClient(testConfig(), &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "id.twitch.tv" {
				t.Fatalf("unexpected URL %q", req.URL.String())
			}
			atomic.AddInt32(&tokenCalls, 1)
			<-release
			return response(http.StatusOK, `{"access_token":"app-1","expires_in":3600}`), nil
		}),
	})

	const callers = 8
	var wg sync.WaitGroup
	results := make(chan string, callers)
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			token, err := client.appAuthToken(context.Background())
			if err != nil {
				errs <- err
				return
			}
			results <- token
		})
	}

	close(release)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("appAuthToken() concurrent error: %v", err)
	}
	for token := range results {
		if token != "app-1" {
			t.Fatalf("appAuthToken() concurrent token = %q, want app-1", token)
		}
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", atomic.LoadInt32(&tokenCalls))
	}
}

func TestCreateEventSubUnauthorizedRefreshesAndRetries(t *testing.T) {
	t.Parallel()

	var (
		tokenCalls  int32
		createCalls int32
	)
	client := NewClient(testConfig(), &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "id.twitch.tv":
				call := atomic.AddInt32(&tokenCalls, 1)
				if call == 1 {
					return response(http.StatusOK, `{"access_token":"stale-token","expires_in":3600}`), nil
				}
				if call == 2 {
					return response(http.StatusOK, `{"access_token":"fresh-token","expires_in":3600}`), nil
				}
				t.Fatalf("unexpected token call #%d", call)
				return nil, errors.New("unexpected token call")
			case "api.twitch.tv":
				call := atomic.AddInt32(&createCalls, 1)
				auth := req.Header.Get("Authorization")
				switch call {
				case 1:
					if auth != "Bearer stale-token" {
						t.Fatalf("first create auth = %q, want %q", auth, "Bearer stale-token")
					}
					return response(http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
				case 2:
					if auth != "Bearer fresh-token" {
						t.Fatalf("second create auth = %q, want %q", auth, "Bearer fresh-token")
					}
					return response(http.StatusAccepted, `{"ok":true}`), nil
				default:
					t.Fatalf("unexpected create call #%d", call)
					return nil, errors.New("unexpected create call")
				}
			default:
				t.Fatalf("unexpected host %q", req.URL.Host)
				return nil, errors.New("unexpected host")
			}
		}),
	})

	err := client.CreateEventSub(t.Context(), "b1", core.EventTypeChannelSubscribe, "1")
	if err != nil {
		t.Fatalf("CreateEventSub() error: %v", err)
	}
	if atomic.LoadInt32(&tokenCalls) != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", tokenCalls)
	}
	if atomic.LoadInt32(&createCalls) != 2 {
		t.Fatalf("create endpoint calls = %d, want 2", createCalls)
	}
}
