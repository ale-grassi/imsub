package groups

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"imsub/internal/core"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
)

func TestClientNilSafety(t *testing.T) {
	t.Parallel()

	var nilClient *Client
	if err := nilClient.KickFromGroup(t.Context(), 1, 2, "test"); err != nil {
		t.Errorf("(*Client).KickFromGroup(nil, groupChatID=%d, telegramUserID=%d) returned error %v, want nil", 1, 2, err)
	}
	nilClient.KickDisplacedUser(t.Context(), 2)
	if nilClient.IsGroupMember(t.Context(), 1, 2) {
		t.Errorf("(*Client).IsGroupMember(nil, groupChatID=%d, telegramUserID=%d) = true, want false", 1, 2)
	}
	if _, err := nilClient.CreateInviteLink(t.Context(), 1, 2, "name"); err == nil {
		t.Error("(*Client).CreateInviteLink(nil, groupChatID=1, telegramUserID=2, name=\"name\") = nil error, want non-nil")
	}
	if _, err := nilClient.CreateBootstrapInviteLink(t.Context(), 1); err == nil {
		t.Error("(*Client).CreateBootstrapInviteLink(nil, groupChatID=1) = nil error, want non-nil")
	}

	c := &Client{}
	if err := c.KickFromGroup(t.Context(), 1, 2, "test"); err != nil {
		t.Errorf("(*Client).KickFromGroup(empty, groupChatID=%d, telegramUserID=%d) returned error %v, want nil", 1, 2, err)
	}
	c.KickDisplacedUser(t.Context(), 2)
	if c.IsGroupMember(t.Context(), 1, 2) {
		t.Errorf("(*Client).IsGroupMember(empty, groupChatID=%d, telegramUserID=%d) = true, want false", 1, 2)
	}
	if _, err := c.CreateInviteLink(t.Context(), 1, 2, "name"); err == nil {
		t.Error("(*Client).CreateInviteLink(empty, groupChatID=1, telegramUserID=2, name=\"name\") = nil error, want non-nil")
	}
	if _, err := c.CreateBootstrapInviteLink(t.Context(), 1); err == nil {
		t.Error("(*Client).CreateBootstrapInviteLink(empty, groupChatID=1) = nil error, want non-nil")
	}
}

func TestCreateInviteLinkUsesJoinRequestLink(t *testing.T) {
	t.Parallel()

	caller := &groupOpsRecordingCaller{
		results: map[string]json.RawMessage{
			"createChatInviteLink": json.RawMessage(`{"invite_link":"https://t.me/+invite"}`),
		},
	}
	bot, err := telego.NewBot("123456:"+strings.Repeat("a", 35), telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("telego.NewBot() error = %v", err)
	}
	client := New(bot, nil, nil, nil, nil)

	if _, err := client.CreateInviteLink(t.Context(), 100, 200, "viewer"); err != nil {
		t.Fatalf("CreateInviteLink() error = %v", err)
	}
	caller.assertJSONField(t, "createChatInviteLink", "creates_join_request", true)
	caller.assertJSONField(t, "createChatInviteLink", "name", "imsub-200-viewer")
	caller.assertExpireDateNear(t, "createChatInviteLink", core.ViewerInviteLinkTTL)
	if _, ok := caller.request["createChatInviteLink"]["member_limit"]; ok {
		t.Fatal("createChatInviteLink payload unexpectedly included member_limit")
	}
}

func TestCreateBootstrapInviteLinkUsesSingleUseDirectLink(t *testing.T) {
	t.Parallel()

	caller := &groupOpsRecordingCaller{
		results: map[string]json.RawMessage{
			"createChatInviteLink": json.RawMessage(`{"invite_link":"https://t.me/+bootstrap"}`),
		},
	}
	bot, err := telego.NewBot("123456:"+strings.Repeat("a", 35), telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("telego.NewBot() error = %v", err)
	}
	client := New(bot, nil, nil, nil, nil)

	if _, err := client.CreateBootstrapInviteLink(t.Context(), 100); err != nil {
		t.Fatalf("CreateBootstrapInviteLink() error = %v", err)
	}
	caller.assertJSONField(t, "createChatInviteLink", "member_limit", float64(1))
	caller.assertJSONField(t, "createChatInviteLink", "name", "imsub-bootstrap")
	caller.assertExpireDateNear(t, "createChatInviteLink", core.BootstrapInviteLinkTTL)
	if _, ok := caller.request["createChatInviteLink"]["creates_join_request"]; ok {
		t.Fatal("createChatInviteLink payload unexpectedly included creates_join_request=false")
	}
}

type groupOpsRecordingCaller struct {
	results map[string]json.RawMessage
	request map[string]map[string]any
}

func (c *groupOpsRecordingCaller) Call(_ context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]
	if c.request == nil {
		c.request = make(map[string]map[string]any)
	}
	if len(data.BodyRaw) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(data.BodyRaw, &payload); err != nil {
			return nil, err
		}
		c.request[method] = payload
	}

	result := json.RawMessage(`true`)
	if got, ok := c.results[method]; ok {
		result = got
	}
	return &telegoapi.Response{Ok: true, Result: result}, nil
}

func (c *groupOpsRecordingCaller) assertJSONField(t *testing.T, method, field string, want any) {
	t.Helper()

	payload, ok := c.request[method]
	if !ok {
		t.Fatalf("request for method %q not recorded", method)
	}
	if got, ok := payload[field]; !ok || got != want {
		t.Fatalf("%s payload[%q] = %#v, want %#v", method, field, got, want)
	}
}

func (c *groupOpsRecordingCaller) assertExpireDateNear(t *testing.T, method string, ttl time.Duration) {
	t.Helper()

	payload, ok := c.request[method]
	if !ok {
		t.Fatalf("request for method %q not recorded", method)
	}
	raw, ok := payload["expire_date"].(float64)
	if !ok {
		t.Fatalf("%s payload missing expire_date: %#v", method, payload)
	}
	got := time.Unix(int64(raw), 0)
	want := time.Now().Add(ttl)
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > 5*time.Second {
		t.Fatalf("%s expire_date = %v, want within 5s of %v", method, got, want)
	}
}
