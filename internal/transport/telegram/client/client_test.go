package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"imsub/internal/events"

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
	caller.assertJSONField(t, "sendMessage", "parse_mode", telego.ModeHTML)
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
		MessageThreadID: 456,
	})

	caller.assertJSONField(t, "sendMessageDraft", "message_thread_id", float64(456))
	caller.assertJSONField(t, "sendMessageDraft", "parse_mode", telego.ModeHTML)
}

func TestSendTransformsConfiguredEmojiForHTMLByDefault(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(`{"message_id":99,"date":0,"chat":{"id":100,"type":"private"}}`),
		},
	}
	c := newTestClient(t, caller)

	c.Send(t.Context(), 100, "⏳ Checking", nil)

	caller.assertJSONFieldContains(t, "sendMessage", "text", `<tg-emoji emoji-id="5386367538735104399">⏳</tg-emoji>`)
	caller.assertJSONField(t, "sendMessage", "parse_mode", telego.ModeHTML)
}

func TestSendTransformsPlayPauseEmojiForHTML(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(`{"message_id":99,"date":0,"chat":{"id":100,"type":"private"}}`),
		},
	}
	c := newTestClient(t, caller)

	c.Send(t.Context(), 100, "▶️ Start ⏸️ Stop", nil)

	caller.assertJSONFieldContains(t, "sendMessage", "text", `<tg-emoji emoji-id="5348125953090403204">▶️</tg-emoji>`)
	caller.assertJSONFieldContains(t, "sendMessage", "text", `<tg-emoji emoji-id="5359543311897998264">⏸️</tg-emoji>`)
}

func TestSendLeavesPlainTextEmojiUntouched(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(`{"message_id":99,"date":0,"chat":{"id":100,"type":"private"}}`),
		},
	}
	c := newTestClient(t, caller)

	c.Send(t.Context(), 100, "⏳ Checking", &MessageOptions{DisableHTML: true})

	caller.assertJSONField(t, "sendMessage", "text", "⏳ Checking")
	caller.assertJSONFieldMissing(t, "sendMessage", "parse_mode")
}

func TestSendLeavesHTMLEmojiUntouchedWhenDisabled(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(`{"message_id":99,"date":0,"chat":{"id":100,"type":"private"}}`),
		},
	}
	c := newTestClient(t, caller)

	c.Send(t.Context(), 100, "⏳ Checking", &MessageOptions{
		DisableCustomEmoji: true,
	})

	caller.assertJSONField(t, "sendMessage", "text", "⏳ Checking")
	caller.assertJSONField(t, "sendMessage", "parse_mode", telego.ModeHTML)
}

func TestEditUsesHTMLByDefault(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"editMessageText": json.RawMessage(`true`),
		},
	}
	c := newTestClient(t, caller)

	c.Edit(t.Context(), 100, 10, "hello", nil)

	caller.assertJSONField(t, "editMessageText", "parse_mode", telego.ModeHTML)
}

func TestEditCanDisableHTML(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"editMessageText": json.RawMessage(`true`),
		},
	}
	c := newTestClient(t, caller)

	c.Edit(t.Context(), 100, 10, "hello", &MessageOptions{DisableHTML: true})

	caller.assertJSONFieldMissing(t, "editMessageText", "parse_mode")
}

func TestSendDraftCanDisableHTML(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		results: map[string]json.RawMessage{
			"sendMessageDraft": json.RawMessage(`true`),
		},
	}
	c := newTestClient(t, caller)

	c.SendDraft(t.Context(), 100, 7, "draft", &MessageOptions{DisableHTML: true})

	caller.assertJSONFieldMissing(t, "sendMessageDraft", "parse_mode")
}

func TestAnswerCallbackEmitsNormalizedAPIError(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		errs: map[string]error{
			"answerCallbackQuery": errors.New(`request call: 400 "Bad Request: MESSAGE_TOO_LONG"`),
		},
	}
	c := newTestClient(t, caller)
	sink := &testEventSink{}
	c.SetObserver(sink)

	c.AnswerCallback(t.Context(), "cb-id", "way too long", true)

	evts := sink.snapshot()
	if len(evts) != 1 {
		t.Fatalf("emitted events = %d, want 1", len(evts))
	}
	if evts[0].Name != events.NameTelegramAPIError {
		t.Fatalf("event name = %q, want %q", evts[0].Name, events.NameTelegramAPIError)
	}
	if got := evts[0].Fields["method"]; got != "answer_callback_query" {
		t.Fatalf("event method = %q, want answer_callback_query", got)
	}
	if got := evts[0].Fields["reason"]; got != "message_too_long" {
		t.Fatalf("event reason = %q, want message_too_long", got)
	}
}

func TestEditForbiddenReturnsZeroAndEmitsForbiddenAPIError(t *testing.T) {
	t.Parallel()

	caller := &recordingCaller{
		errs: map[string]error{
			"editMessageText": errors.New(`request call: 403 "Forbidden: message can't be edited"`),
		},
	}
	c := newTestClient(t, caller)
	sink := &testEventSink{}
	c.SetObserver(sink)

	if got := c.Edit(t.Context(), 100, 10, "hello", nil); got != 0 {
		t.Fatalf("Edit() = %d, want 0 on forbidden", got)
	}

	evts := sink.snapshot()
	if len(evts) != 1 {
		t.Fatalf("emitted events = %d, want 1", len(evts))
	}
	if evts[0].Name != events.NameTelegramAPIError {
		t.Fatalf("event name = %q, want %q", evts[0].Name, events.NameTelegramAPIError)
	}
	if got := evts[0].Fields["method"]; got != "edit_message_text" {
		t.Fatalf("event method = %q, want edit_message_text", got)
	}
	if got := evts[0].Fields["reason"]; got != "forbidden" {
		t.Fatalf("event reason = %q, want forbidden", got)
	}
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
	errs    map[string]error
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
	if err, ok := c.errs[method]; ok {
		return nil, err
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

type testEventSink struct {
	mu     sync.Mutex
	events []events.Event
}

func (s *testEventSink) Emit(_ context.Context, evt events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

func (s *testEventSink) snapshot() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]events.Event(nil), s.events...)
}

func (c *recordingCaller) assertJSONFieldContains(t *testing.T, method, field, wantSubstring string) {
	t.Helper()

	payload, ok := c.request[method]
	if !ok {
		t.Fatalf("request for method %q not recorded", method)
	}
	got, ok := payload[field].(string)
	if !ok {
		t.Fatalf("%s payload[%q] = %#v, want string containing %q", method, field, payload[field], wantSubstring)
	}
	if !strings.Contains(got, wantSubstring) {
		t.Fatalf("%s payload[%q] = %q, want substring %q", method, field, got, wantSubstring)
	}
}

func (c *recordingCaller) assertJSONFieldMissing(t *testing.T, method, field string) {
	t.Helper()

	payload, ok := c.request[method]
	if !ok {
		t.Fatalf("request for method %q not recorded", method)
	}
	if got, ok := payload[field]; ok {
		t.Fatalf("%s payload[%q] = %#v, want field omitted", method, field, got)
	}
}
