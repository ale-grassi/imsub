package ui

import (
	"strings"
	"testing"

	"imsub/internal/platform/i18n"
)

func TestProfileAndButtons(t *testing.T) {
	t.Parallel()

	htmlOut := TwitchProfileHTML(`a/b & "x"`, `A/B & "X"`)
	if !strings.Contains(htmlOut, "https://twitch.tv/a%2Fb") {
		t.Errorf("TwitchProfileHTML(%q) = %q, want path-escaped URL", `a/b & "x"`, htmlOut)
	}
	if !strings.Contains(htmlOut, "&amp;") {
		t.Errorf("TwitchProfileHTML(%q) = %q, want escaped HTML entities", `a/b & "x"`, htmlOut)
	}

	cb := CallbackButton("Refresh", "action:refresh_viewer")
	if cb.CallbackData != "action:refresh_viewer" || cb.Text != "Refresh" {
		t.Errorf("CallbackButton(%q, %q) = %+v, want Text=%q CallbackData=%q", "Refresh", "action:refresh_viewer", cb, "Refresh", "action:refresh_viewer")
	}
	ub := URLButton("Open", "https://example.com")
	if ub.URL != "https://example.com" || ub.Text != "Open" {
		t.Errorf("URLButton(%q, %q) = %+v, want Text=%q URL=%q", "Open", "https://example.com", ub, "Open", "https://example.com")
	}

	link := LinkButton("Connect", "https://example.com")
	if link.IconCustomEmojiID != linkButtonEmojiID {
		t.Errorf("LinkButton(%q, %q) icon = %q, want %q", "Connect", "https://example.com", link.IconCustomEmojiID, linkButtonEmojiID)
	}
	if link.Style != "primary" {
		t.Errorf("LinkButton(%q, %q) style = %q, want %q", "Connect", "https://example.com", link.Style, "primary")
	}

	copyButton := CopyLinkButton("Copy link", "https://example.com")
	if copyButton.IconCustomEmojiID != "" {
		t.Errorf("CopyLinkButton(%q, %q) icon = %q, want empty", "Copy link", "https://example.com", copyButton.IconCustomEmojiID)
	}
	if copyButton.CopyText == nil || copyButton.CopyText.Text != "https://example.com" {
		t.Errorf("CopyLinkButton(%q, %q) copy_text = %+v, want text %q", "Copy link", "https://example.com", copyButton.CopyText, "https://example.com")
	}

	group := GroupButton("VIP Lounge", "creator:group:1")
	if group.IconCustomEmojiID != groupButtonEmojiID {
		t.Errorf("GroupButton(%q, %q) icon = %q, want %q", "VIP Lounge", "creator:group:1", group.IconCustomEmojiID, groupButtonEmojiID)
	}

	back := BackButton("Back", "action:back")
	if back.IconCustomEmojiID != backButtonEmojiID {
		t.Errorf("BackButton(%q, %q) icon = %q, want %q", "Back", "action:back", back.IconCustomEmojiID, backButtonEmojiID)
	}
}

func TestLinkedStatusDetailsNoGroupsMessage(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	got := LinkedStatusDetailsHTML("en", []string{"Creator One"}, false)
	if !strings.Contains(got, "No new Telegram groups available") {
		t.Errorf("LinkedStatusDetailsHTML(%q, %v, %t) = %q, want message containing %q", "en", []string{"Creator One"}, false, got, "No new Telegram groups available")
	}
}
