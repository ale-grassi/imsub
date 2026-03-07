package client

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
)

func TestClientNilSafety(t *testing.T) {
	t.Parallel()

	var c *Client
	if got := c.Send(t.Context(), 100, "hello", nil); got != 0 {
		t.Errorf("(*Client).Send(nil, 100, %q, nil) = %d, want 0", "hello", got)
	}
	c.Edit(t.Context(), 100, 10, "text", nil)
	if got := c.Reply(t.Context(), 100, 10, "text", nil); got != 10 {
		t.Errorf("(*Client).Reply(nil, 100, 10, %q, nil) = %d, want 10", "text", got)
	}
	if got := c.Reply(t.Context(), 100, 0, "text", nil); got != 0 {
		t.Errorf("(*Client).Reply(nil, 100, 0, %q, nil) = %d, want 0", "text", got)
	}
	c.Delete(t.Context(), 100, 10)
	c.AnswerCallback(t.Context(), "cb-id", "ok", true)
}

func TestClientEmptySafety(t *testing.T) {
	t.Parallel()

	c := &Client{}
	if got := c.Send(t.Context(), 100, "hello", nil); got != 0 {
		t.Errorf("(*Client).Send(empty, 100, %q, nil) = %d, want 0", "hello", got)
	}
	c.Edit(t.Context(), 100, 10, "text", nil)
	if got := c.Reply(t.Context(), 100, 10, "text", nil); got != 10 {
		t.Errorf("(*Client).Reply(empty, 100, 10, %q, nil) = %d, want 10", "text", got)
	}
	if got := c.Reply(t.Context(), 100, 0, "text", nil); got != 0 {
		t.Errorf("(*Client).Reply(empty, 100, 0, %q, nil) = %d, want 0", "text", got)
	}
	c.Delete(t.Context(), 100, 10)
	c.AnswerCallback(t.Context(), "cb-id", "ok", true)
}

func TestSendIncludesMessageThreadID(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(`{"message_id":99,"date":0,"chat":{"id":100,"type":"private"}}`),
		},
	}
	c := newTestClient(t, caller)

	got := c.Send(t.Context(), 100, "hello", &MessageOptions{MessageThreadID: 123})
	if got != 99 {
		t.Fatalf("Send() message id = %d, want 99", got)
	}
	caller.assertJSONField(t, "sendMessage", "message_thread_id", float64(123))
}

func TestSendDraftIncludesMessageThreadID(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessageDraft": json.RawMessage(`true`),
		},
	}
	c := newTestClient(t, caller)

	c.SendDraft(t.Context(), 100, 7, "draft", &MessageOptions{
		ParseMode:       telego.ModeHTML,
		MessageThreadID: 456,
	})

	caller.assertJSONField(t, "sendMessageDraft", "message_thread_id", float64(456))
	caller.assertJSONField(t, "sendMessageDraft", "parse_mode", telego.ModeHTML)
}

func newTestClient(t *testing.T, caller telegoapi.Caller) *Client {
	t.Helper()

	bot, err := telego.NewBot("123456:"+strings.Repeat("a", 35), telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("telego.NewBot() error = %v", err)
	}
	return New(bot, nil, nil)
}

type recordingCaller struct {
	results map[string]json.RawMessage
	request map[string]map[string]any
}

func (c *recordingCaller) Call(_ context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
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

func (c *recordingCaller) assertJSONField(t *testing.T, method, field string, want any) {
	t.Helper()

	payload, ok := c.request[method]
	if !ok {
		t.Fatalf("request for method %q not recorded", method)
	}
	if got, ok := payload[field]; !ok || got != want {
		t.Fatalf("%s payload[%q] = %#v, want %#v", method, field, got, want)
	}
}
