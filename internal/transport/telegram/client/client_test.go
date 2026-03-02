package client

import "testing"

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
