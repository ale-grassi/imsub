package twitch

// UsersResponse is Twitch /helix/users response body.
type UsersResponse struct {
	Data []struct {
		ID          string `json:"id"`
		Login       string `json:"login"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

// SubscriptionsResponse is Twitch /helix/subscriptions response body.
type SubscriptionsResponse struct {
	Data []struct {
		UserID string `json:"user_id"`
	} `json:"data"`
	Pagination struct {
		Cursor string `json:"cursor"`
	} `json:"pagination"`
}

// EventSubListResponse is Twitch EventSub list response body.
type EventSubListResponse struct {
	Data []struct {
		Type      string `json:"type"`
		Status    string `json:"status"`
		Condition struct {
			BroadcasterUserID string `json:"broadcaster_user_id"`
		} `json:"condition"`
	} `json:"data"`
	Pagination struct {
		Cursor string `json:"cursor"`
	} `json:"pagination"`
}
