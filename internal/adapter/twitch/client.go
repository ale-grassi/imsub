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

	"imsub/internal/core"
	"imsub/internal/platform/config"
)

var _ core.TwitchAPI = (*Client)(nil)

// Client is the production Twitch API client that makes real HTTP calls.
type Client struct {
	cfg    config.Config
	client *http.Client
}

// NewClient creates a Twitch API client backed by real HTTP requests.
func NewClient(cfg config.Config, client *http.Client) *Client {
	return &Client{cfg: cfg, client: client}
}

func responseBodyString(resp *http.Response) (string, error) {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ExchangeCode implements the core.TwitchAPI interface.
func (c *Client) ExchangeCode(ctx context.Context, code string) (core.TokenResponse, error) {
	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", c.cfg.PublicBaseURL+"/auth/callback")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return core.TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return core.TokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return core.TokenResponse{}, fmt.Errorf("token exchange status %d: read response body: %w", resp.StatusCode, readErr)
		}
		return core.TokenResponse{}, fmt.Errorf("token exchange status %d: %s", resp.StatusCode, body)
	}

	var tr core.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return core.TokenResponse{}, err
	}
	if tr.AccessToken == "" {
		return core.TokenResponse{}, errors.New("empty access_token in response")
	}
	return tr, nil
}

// RefreshToken implements the core.TwitchAPI interface.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (core.TokenResponse, error) {
	if refreshToken == "" {
		return core.TokenResponse{}, errors.New("empty refresh token")
	}

	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return core.TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return core.TokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return core.TokenResponse{}, fmt.Errorf("refresh token status %d: read response body: %w", resp.StatusCode, readErr)
		}
		return core.TokenResponse{}, fmt.Errorf("refresh token status %d: %s", resp.StatusCode, body)
	}

	var tr core.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return core.TokenResponse{}, err
	}
	if tr.AccessToken == "" {
		return core.TokenResponse{}, errors.New("empty refreshed access token")
	}
	return tr, nil
}

// FetchUser implements the core.TwitchAPI interface.
func (c *Client) FetchUser(ctx context.Context, userToken string) (id, login, displayName string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/users", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Client-Id", c.cfg.TwitchClientID)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return "", "", "", fmt.Errorf("users endpoint status %d: read response body: %w", resp.StatusCode, readErr)
		}
		return "", "", "", fmt.Errorf("users endpoint status %d: %s", resp.StatusCode, body)
	}

	var ur UsersResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return "", "", "", err
	}
	if len(ur.Data) == 0 {
		return "", "", "", errors.New("no user data returned by Twitch")
	}
	return ur.Data[0].ID, ur.Data[0].Login, ur.Data[0].DisplayName, nil
}

// AppToken implements the core.TwitchAPI interface.
func (c *Client) AppToken(ctx context.Context) (string, error) {
	values := url.Values{}
	values.Set("client_id", c.cfg.TwitchClientID)
	values.Set("client_secret", c.cfg.TwitchClientSecret)
	values.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return "", fmt.Errorf("app token status %d: read response body: %w", resp.StatusCode, readErr)
		}
		return "", fmt.Errorf("app token status %d: %s", resp.StatusCode, body)
	}

	var out core.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("empty app access token")
	}
	return out.AccessToken, nil
}

// CreateEventSub implements the core.TwitchAPI interface.
func (c *Client) CreateEventSub(ctx context.Context, appToken, broadcasterID, eventType, version string) error {
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
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.twitch.tv/helix/eventsub/subscriptions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+appToken)
	req.Header.Set("Client-Id", c.cfg.TwitchClientID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusConflict {
		return nil
	}
	respBody, readErr := responseBodyString(resp)
	if readErr != nil {
		return fmt.Errorf("eventsub create status %d: read response body: %w", resp.StatusCode, readErr)
	}
	return fmt.Errorf("eventsub create status %d: %s", resp.StatusCode, respBody)
}

// EnabledEventSubTypes implements the core.TwitchAPI interface.
func (c *Client) EnabledEventSubTypes(ctx context.Context, appToken, creatorID string) (map[string]bool, error) {
	found := map[string]bool{
		core.EventTypeChannelSubscribe: false,
		core.EventTypeChannelSubEnd:    false,
	}
	var cursor string
	for {
		endpoint := "https://api.twitch.tv/helix/eventsub/subscriptions?status=enabled"
		if cursor != "" {
			endpoint += "&after=" + url.QueryEscape(cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+appToken)
		req.Header.Set("Client-Id", c.cfg.TwitchClientID)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, readErr := responseBodyString(resp)
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("eventsub list status %d: read response body: %w", resp.StatusCode, readErr)
			}
			return nil, fmt.Errorf("eventsub list status %d: %s", resp.StatusCode, body)
		}

		var list EventSubListResponse
		err = json.NewDecoder(resp.Body).Decode(&list)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, sub := range list.Data {
			if sub.Condition.BroadcasterUserID != creatorID {
				continue
			}
			if _, ok := found[sub.Type]; ok {
				found[sub.Type] = true
			}
		}
		if found[core.EventTypeChannelSubscribe] && found[core.EventTypeChannelSubEnd] {
			return found, nil
		}
		if list.Pagination.Cursor == "" {
			return found, nil
		}
		cursor = list.Pagination.Cursor
	}
}

// ListSubscriberPage implements the core.TwitchAPI interface.
func (c *Client) ListSubscriberPage(ctx context.Context, accessToken, broadcasterID, cursor string) ([]string, string, error) {
	endpoint := fmt.Sprintf("https://api.twitch.tv/helix/subscriptions?broadcaster_id=%s&first=100", url.QueryEscape(broadcasterID))
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", c.cfg.TwitchClientID)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("subscriptions list status 401: %w", core.ErrUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := responseBodyString(resp)
		if readErr != nil {
			return nil, "", fmt.Errorf("subscriptions list status %d: read response body: %w", resp.StatusCode, readErr)
		}
		return nil, "", fmt.Errorf("subscriptions list status %d: %s", resp.StatusCode, body)
	}

	var sr SubscriptionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, "", err
	}
	userIDs := make([]string, 0, len(sr.Data))
	for _, sub := range sr.Data {
		userIDs = append(userIDs, sub.UserID)
	}
	return userIDs, sr.Pagination.Cursor, nil
}
