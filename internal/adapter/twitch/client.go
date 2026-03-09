package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"
)

var (
	errTokenExchange       = errors.New("token exchange failed")
	errEmptyToken          = errors.New("empty access token in response")
	errEmptyRefresh        = errors.New("empty refresh token")
	errTokenRefresh        = errors.New("refresh token failed")
	errEmptyRefreshedToken = errors.New("empty refreshed access token")
	errUsersEndpoint       = errors.New("users endpoint failed")
	errNoUserData          = errors.New("no user data returned by Twitch")
	errAppToken            = errors.New("app token failed")
	errEmptyAppToken       = errors.New("empty app access token")
	errEventSubCreate      = errors.New("eventsub create failed")
	errEventSubList        = errors.New("eventsub list failed")
	errEventSubDelete      = errors.New("eventsub delete failed")
	errSubList             = errors.New("subscriptions list failed")
)

const appTokenRefreshSkew = 30 * time.Second

var _ core.TwitchAPI = (*Client)(nil)

// Client is the production Twitch API client that makes real HTTP calls.
type Client struct {
	cfg    config.Config
	client *http.Client
	now    func() time.Time

	appTokenMu      sync.Mutex
	appToken        string
	appTokenExpires time.Time
	appTokenFetchCh chan struct{}
}

// NewClient creates a Twitch API client backed by real HTTP requests.
func NewClient(cfg config.Config, client *http.Client) *Client {
	return &Client{
		cfg:    cfg,
		client: client,
		now:    time.Now,
	}
}

func responseBodyString(resp *http.Response) (string, error) {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	return string(b), nil
}

func (c *Client) postOAuthToken(ctx context.Context, values url.Values, endpointErr error) (core.TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return core.TokenResponse{}, fmt.Errorf("create oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req) //nolint:gosec // req URL is a hardcoded Twitch URL
	if err != nil {
		return core.TokenResponse{}, fmt.Errorf("do oauth token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return core.TokenResponse{}, fmt.Errorf("%w: status %d: read body: %w", endpointErr, resp.StatusCode, readErr)
		}
		return core.TokenResponse{}, fmt.Errorf("%w: status %d: %s", endpointErr, resp.StatusCode, body)
	}

	var tr core.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return core.TokenResponse{}, fmt.Errorf("decode oauth token response: %w", err)
	}
	return tr, nil
}

func (c *Client) appTokenValidLocked(now time.Time) bool {
	return c.appToken != "" && now.Add(appTokenRefreshSkew).Before(c.appTokenExpires)
}

func (c *Client) cacheAppTokenLocked(token string, expiresIn int) {
	c.appToken = token
	if expiresIn > 0 {
		c.appTokenExpires = c.now().Add(time.Duration(expiresIn) * time.Second)
		return
	}
	c.appTokenExpires = time.Time{}
}

func (c *Client) invalidateAppToken(token string) {
	c.appTokenMu.Lock()
	defer c.appTokenMu.Unlock()
	if token != "" && c.appToken != token {
		return
	}
	c.appToken = ""
	c.appTokenExpires = time.Time{}
}

func (c *Client) fetchAppToken(ctx context.Context) (string, error) {
	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("grant_type", "client_credentials")

	out, err := c.postOAuthToken(ctx, values, errAppToken)
	if err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errEmptyAppToken
	}

	c.appTokenMu.Lock()
	c.cacheAppTokenLocked(out.AccessToken, out.ExpiresIn)
	c.appTokenMu.Unlock()
	return out.AccessToken, nil
}

func (c *Client) appAuthToken(ctx context.Context) (string, error) {
	for {
		now := c.now()

		c.appTokenMu.Lock()
		if c.appTokenValidLocked(now) {
			token := c.appToken
			c.appTokenMu.Unlock()
			return token, nil
		}

		previousToken := c.appToken
		previousExpiry := c.appTokenExpires
		if waitCh := c.appTokenFetchCh; waitCh != nil {
			c.appTokenMu.Unlock()
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("wait for app token refresh: %w", ctx.Err())
			case <-waitCh:
				continue
			}
		}

		waitCh := make(chan struct{})
		c.appTokenFetchCh = waitCh
		c.appTokenMu.Unlock()

		token, err := c.fetchAppToken(ctx)

		c.appTokenMu.Lock()
		c.appTokenFetchCh = nil
		close(waitCh)
		c.appTokenMu.Unlock()

		if err == nil {
			return token, nil
		}
		if previousToken != "" && now.Before(previousExpiry) {
			return previousToken, nil
		}
		return "", err
	}
}

func (c *Client) doAppAuthenticatedRequest(ctx context.Context, build func(token string) (*http.Request, error)) (*http.Response, error) {
	token, err := c.appAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get app token: %w", err)
	}

	req, err := build(token)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req) //nolint:gosec // req URL is a hardcoded Twitch URL
	if err != nil {
		return nil, fmt.Errorf("do app-authenticated request: %w", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_ = resp.Body.Close()

	c.invalidateAppToken(token)
	token, err = c.appAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh app token after unauthorized: %w", err)
	}

	req, err = build(token)
	if err != nil {
		return nil, err
	}
	resp, err = c.client.Do(req) //nolint:gosec // req URL is a hardcoded Twitch URL
	if err != nil {
		return nil, fmt.Errorf("retry app-authenticated request: %w", err)
	}
	return resp, nil
}

// ExchangeCode implements the core.TwitchAPI interface.
func (c *Client) ExchangeCode(ctx context.Context, code string) (core.TokenResponse, error) {
	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", c.cfg.PublicBaseURL+"/auth/callback")

	tr, err := c.postOAuthToken(ctx, values, errTokenExchange)
	if err != nil {
		return core.TokenResponse{}, err
	}
	if tr.AccessToken == "" {
		return core.TokenResponse{}, errEmptyToken
	}
	return tr, nil
}

// RefreshToken implements the core.TwitchAPI interface.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (core.TokenResponse, error) {
	if refreshToken == "" {
		return core.TokenResponse{}, errEmptyRefresh
	}

	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)

	tr, err := c.postOAuthToken(ctx, values, errTokenRefresh)
	if err != nil {
		return core.TokenResponse{}, err
	}
	if tr.AccessToken == "" {
		return core.TokenResponse{}, errEmptyRefreshedToken
	}
	return tr, nil
}

// FetchUser implements the core.TwitchAPI interface.
func (c *Client) FetchUser(ctx context.Context, userToken string) (id, login, displayName string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/users", nil)
	if err != nil {
		return "", "", "", fmt.Errorf("create users request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Client-Id", c.cfg.TwitchClientID)

	resp, err := c.client.Do(req) //nolint:gosec // req URL is a hardcoded Twitch URL
	if err != nil {
		return "", "", "", fmt.Errorf("do users request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return "", "", "", fmt.Errorf("%w: status %d: read body: %w", errUsersEndpoint, resp.StatusCode, readErr)
		}
		return "", "", "", fmt.Errorf("%w: status %d: %s", errUsersEndpoint, resp.StatusCode, body)
	}

	var ur UsersResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return "", "", "", fmt.Errorf("decode users response: %w", err)
	}
	if len(ur.Data) == 0 {
		return "", "", "", errNoUserData
	}
	return ur.Data[0].ID, ur.Data[0].Login, ur.Data[0].DisplayName, nil
}

// CreateEventSub implements the core.TwitchAPI interface.
func (c *Client) CreateEventSub(ctx context.Context, broadcasterID, eventType, version string) error {
	payload := map[string]any{
		"type":    eventType,
		"version": version,
		"condition": map[string]string{
			"broadcaster_user_id": broadcasterID,
		},
		"transport": map[string]string{
			"method":   "webhook",
			"callback": c.cfg.PublicBaseURL + c.cfg.TwitchWebhookPath,
			"secret":   c.cfg.TwitchEventSubSecret,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal eventsub payload: %w", err)
	}

	resp, err := c.doAppAuthenticatedRequest(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.twitch.tv/helix/eventsub/subscriptions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create eventsub request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Client-Id", c.cfg.TwitchClientID)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return fmt.Errorf("do eventsub request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusConflict {
		return nil
	}
	respBody, readErr := responseBodyString(resp)
	if readErr != nil {
		return fmt.Errorf("%w: status %d: read body: %w", errEventSubCreate, resp.StatusCode, readErr)
	}
	return fmt.Errorf("%w: status %d: %s", errEventSubCreate, resp.StatusCode, respBody)
}

// EnabledEventSubTypes implements the core.TwitchAPI interface.
func (c *Client) EnabledEventSubTypes(ctx context.Context, creatorID string) (map[string]bool, error) {
	found := map[string]bool{
		core.EventTypeChannelSubscribe: false,
		core.EventTypeChannelSubEnd:    false,
		core.EventTypeChannelSubGift:   false,
	}
	var cursor string
	for {
		endpoint := "https://api.twitch.tv/helix/eventsub/subscriptions?status=enabled&first=100"
		endpoint += "&user_id=" + url.QueryEscape(creatorID)
		if cursor != "" {
			endpoint += "&after=" + url.QueryEscape(cursor)
		}
		resp, err := c.doAppAuthenticatedRequest(ctx, func(token string) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("create eventsub list request: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Client-Id", c.cfg.TwitchClientID)
			return req, nil
		})
		if err != nil {
			return nil, fmt.Errorf("do eventsub list request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := responseBodyString(resp)
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("%w: status %d: read body: %w", errEventSubList, resp.StatusCode, readErr)
			}
			return nil, fmt.Errorf("%w: status %d: %s", errEventSubList, resp.StatusCode, body)
		}

		var list EventSubListResponse
		err = json.NewDecoder(resp.Body).Decode(&list)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode eventsub list response: %w", err)
		}
		for _, sub := range list.Data {
			if sub.Condition.BroadcasterUserID != creatorID {
				continue
			}
			if _, ok := found[sub.Type]; ok {
				found[sub.Type] = true
			}
		}
		if found[core.EventTypeChannelSubscribe] && found[core.EventTypeChannelSubEnd] && found[core.EventTypeChannelSubGift] {
			return found, nil
		}
		if list.Pagination.Cursor == "" {
			return found, nil
		}
		cursor = list.Pagination.Cursor
	}
}

// ListEventSubs implements the core.TwitchAPI interface.
func (c *Client) ListEventSubs(ctx context.Context, opts core.ListEventSubsOpts) ([]core.EventSubSubscription, error) {
	var all []core.EventSubSubscription
	var cursor string
	for {
		endpoint := "https://api.twitch.tv/helix/eventsub/subscriptions?first=100"
		if opts.UserID != "" {
			endpoint += "&user_id=" + url.QueryEscape(opts.UserID)
		}
		if cursor != "" {
			endpoint += "&after=" + url.QueryEscape(cursor)
		}
		resp, err := c.doAppAuthenticatedRequest(ctx, func(token string) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("create eventsub list request: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Client-Id", c.cfg.TwitchClientID)
			return req, nil
		})
		if err != nil {
			return nil, fmt.Errorf("do eventsub list request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := responseBodyString(resp)
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("%w: status %d: read body: %w", errEventSubList, resp.StatusCode, readErr)
			}
			return nil, fmt.Errorf("%w: status %d: %s", errEventSubList, resp.StatusCode, body)
		}

		var list EventSubListResponse
		err = json.NewDecoder(resp.Body).Decode(&list)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode eventsub list response: %w", err)
		}
		for _, sub := range list.Data {
			all = append(all, core.EventSubSubscription{
				ID:            sub.ID,
				Status:        sub.Status,
				Type:          sub.Type,
				BroadcasterID: sub.Condition.BroadcasterUserID,
			})
		}
		if list.Pagination.Cursor == "" {
			break
		}
		cursor = list.Pagination.Cursor
	}
	return all, nil
}

// DeleteEventSub implements the core.TwitchAPI interface.
func (c *Client) DeleteEventSub(ctx context.Context, subscriptionID string) error {
	endpoint := "https://api.twitch.tv/helix/eventsub/subscriptions?id=" + url.QueryEscape(subscriptionID)
	resp, err := c.doAppAuthenticatedRequest(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("create eventsub delete request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Client-Id", c.cfg.TwitchClientID)
		return req, nil
	})
	if err != nil {
		return fmt.Errorf("do eventsub delete request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	body, readErr := responseBodyString(resp)
	if readErr != nil {
		return fmt.Errorf("%w: status %d: read body: %w", errEventSubDelete, resp.StatusCode, readErr)
	}
	return fmt.Errorf("%w: status %d: %s", errEventSubDelete, resp.StatusCode, body)
}

// ListSubscriberPage implements the core.TwitchAPI interface.
func (c *Client) ListSubscriberPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	endpoint := fmt.Sprintf("https://api.twitch.tv/helix/subscriptions?broadcaster_id=%s&first=100", url.QueryEscape(broadcasterID))
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create subscriptions request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", c.cfg.TwitchClientID)

	resp, err := c.client.Do(req) //nolint:gosec // req URL is a hardcoded Twitch URL
	if err != nil {
		return nil, "", fmt.Errorf("do subscriptions request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("subscriptions list status 401: %w", core.ErrUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return nil, "", fmt.Errorf("%w: status %d: read body: %w", errSubList, resp.StatusCode, readErr)
		}
		return nil, "", fmt.Errorf("%w: status %d: %s", errSubList, resp.StatusCode, body)
	}

	var sr SubscriptionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, "", fmt.Errorf("decode subscriptions response: %w", err)
	}
	userIDs := make([]string, 0, len(sr.Data))
	for _, sub := range sr.Data {
		userIDs = append(userIDs, sub.UserID)
	}
	return userIDs, sr.Pagination.Cursor, nil
}
