package groupops

import (
	"testing"
)

func TestClientNilSafety(t *testing.T) {
	t.Parallel()

	var nilClient *Client
	if err := nilClient.KickFromGroup(t.Context(), 1, 2); err != nil {
		t.Errorf("(*Client).KickFromGroup(nil, groupChatID=%d, telegramUserID=%d) returned error %v, want nil", 1, 2, err)
	}
	nilClient.KickDisplacedUser(t.Context(), 2)
	if nilClient.IsGroupMember(t.Context(), 1, 2) {
		t.Errorf("(*Client).IsGroupMember(nil, groupChatID=%d, telegramUserID=%d) = true, want false", 1, 2)
	}
	if _, err := nilClient.CreateInviteLink(t.Context(), 1, 2, "name"); err == nil {
		t.Error("(*Client).CreateInviteLink(nil, groupChatID=1, telegramUserID=2, name=\"name\") = nil error, want non-nil")
	}

	c := &Client{}
	if err := c.KickFromGroup(t.Context(), 1, 2); err != nil {
		t.Errorf("(*Client).KickFromGroup(empty, groupChatID=%d, telegramUserID=%d) returned error %v, want nil", 1, 2, err)
	}
	c.KickDisplacedUser(t.Context(), 2)
	if c.IsGroupMember(t.Context(), 1, 2) {
		t.Errorf("(*Client).IsGroupMember(empty, groupChatID=%d, telegramUserID=%d) = true, want false", 1, 2)
	}
	if _, err := c.CreateInviteLink(t.Context(), 1, 2, "name"); err == nil {
		t.Error("(*Client).CreateInviteLink(empty, groupChatID=1, telegramUserID=2, name=\"name\") = nil error, want non-nil")
	}
}
