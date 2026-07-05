package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseMemberTagsRefreshOptionsRequiresChatID(t *testing.T) {
	t.Parallel()

	_, err := parseMemberTagsRefreshOptions(nil)
	if err == nil {
		t.Fatal("parseMemberTagsRefreshOptions() error = nil, want non-nil")
	}
	if !errors.Is(err, errAdminChatIDNeeded) {
		t.Fatalf("parseMemberTagsRefreshOptions() error = %v, want errAdminChatIDNeeded", err)
	}
}

func TestParseMemberTagsRefreshOptionsParsesChatIDTimeoutAndEnable(t *testing.T) {
	t.Parallel()

	opts, err := parseMemberTagsRefreshOptions([]string{"-chat-id", "-100123", "-timeout", "45s", "-enable"})
	if err != nil {
		t.Fatalf("parseMemberTagsRefreshOptions() error = %v", err)
	}
	if opts.ChatID != -100123 {
		t.Fatalf("ChatID = %d, want -100123", opts.ChatID)
	}
	if opts.Timeout != 45*time.Second {
		t.Fatalf("Timeout = %v, want 45s", opts.Timeout)
	}
	if !opts.Enable {
		t.Fatal("Enable = false, want true")
	}
}

func TestParseMemberTagsRefreshOptionsRejectsExtraArgs(t *testing.T) {
	t.Parallel()

	_, err := parseMemberTagsRefreshOptions([]string{"-chat-id", "-100123", "extra"})
	if err == nil {
		t.Fatal("parseMemberTagsRefreshOptions() error = nil, want non-nil")
	}
	if !errors.Is(err, errAdminUsage) {
		t.Fatalf("parseMemberTagsRefreshOptions() error = %v, want errAdminUsage", err)
	}
}

func TestRunAdminCommandRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := RunAdminCommand([]string{"public-command"}, nil)
	if err == nil {
		t.Fatal("RunAdminCommand() error = nil, want non-nil")
	}
	if !errors.Is(err, errAdminUsage) {
		t.Fatalf("RunAdminCommand() error = %v, want errAdminUsage", err)
	}
	if !strings.Contains(err.Error(), "member-tags-refresh") {
		t.Fatalf("RunAdminCommand() error = %q, want usage", err)
	}
}
