package bot

import (
	"strings"
	"testing"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
)

func TestCreatorGroupLineEscapesHTML(t *testing.T) {
	t.Parallel()

	line := CreatorGroupLines("en", `name<&>`, []core.ManagedGroup{{GroupName: `group "x"`}})
	if !strings.Contains(line, "name&lt;&amp;&gt;") {
		t.Errorf("CreatorGroupLines() = %q, want escaped creator name", line)
	}
	if !strings.Contains(line, "group &#34;x&#34;") {
		t.Errorf("CreatorGroupLines() = %q, want escaped group name", line)
	}
}

func TestBuildCreatorPromptView(t *testing.T) {
	t.Parallel()

	view := buildCreatorPromptView("en", "https://example.com", false)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorPromptView() = %+v, want non-empty text and markup", view)
	}
}

func TestBuildCreatorStatusViewNoGroups(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{TwitchLogin: "creator"}, core.Status{}, nil)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorStatusView() = %+v, want non-empty text and markup", view)
	}
	if strings.Contains(view.text, i18n.Translate("en", msgCreatorBlocklistDisabled)) {
		t.Fatalf("buildCreatorStatusView() text = %q, want no blocklist status for inactive creator", view.text)
	}
	if strings.Contains(view.text, "Cached banned users") {
		t.Fatalf("buildCreatorStatusView() text = %q, want no banned user count for inactive creator", view.text)
	}
	for _, row := range view.opts.Markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == creatorBlocklistToggleCallback() {
				t.Fatalf("buildCreatorStatusView() markup = %+v, want no blocklist toggle for inactive creator", view.opts.Markup.InlineKeyboard)
			}
		}
	}
}

func TestBuildCreatorStatusViewWithSingleGroup(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{TwitchLogin: "creator"}, core.Status{HasBannedUserCount: true, BannedUserCount: 2}, []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorStatusView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, i18n.Translate("en", msgCreatorBlocklistDisabled)) {
		t.Fatalf("buildCreatorStatusView() text = %q, want disabled blocklist status", view.text)
	}
	if !strings.Contains(view.text, "Cached banned users") {
		t.Fatalf("buildCreatorStatusView() text = %q, want cached banned users line", view.text)
	}
}

func TestBuildCreatorStatusViewWithMultipleGroups(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{TwitchLogin: "creator"}, core.Status{}, []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}, {ChatID: 2, GroupName: "Patrons"}})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorStatusView() = %+v, want non-empty text and markup", view)
	}
}

func TestBuildCreatorStatusViewWithBlocklistEnabled(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{
		TwitchLogin:          "creator",
		BlocklistSyncEnabled: true,
	}, core.Status{HasBannedUserCount: true, BannedUserCount: 4}, []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}})
	if !strings.Contains(view.text, i18n.Translate("en", msgCreatorBlocklistEnabled)) {
		t.Fatalf("buildCreatorStatusView() text = %q, want enabled blocklist status", view.text)
	}
	if !strings.Contains(view.text, "<b>4</b>") {
		t.Fatalf("buildCreatorStatusView() text = %q, want banned user count", view.text)
	}
	if view.opts.Markup == nil || len(view.opts.Markup.InlineKeyboard) == 0 {
		t.Fatalf("buildCreatorStatusView() markup = %+v, want creator status menu", view.opts.Markup)
	}
	found := false
	for _, row := range view.opts.Markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == creatorBlocklistToggleCallback() {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("buildCreatorStatusView() markup = %+v, want blocklist toggle callback", view.opts.Markup.InlineKeyboard)
	}
}

func TestBuildCreatorManagedGroupsView(t *testing.T) {
	t.Parallel()

	view := buildCreatorManagedGroupsView("en", core.Creator{TwitchLogin: "creator"}, []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}}, "")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorManagedGroupsView() = %+v, want non-empty text and markup", view)
	}
}

func TestBuildCreatorGroupSettingsView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupSettingsView("en", core.Creator{TwitchLogin: "creator"}, core.ManagedGroup{ChatID: 1, GroupName: "VIP", Policy: core.GroupPolicyObserveWarn}, creatorMenuCallback(), "notice")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupSettingsView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "notice") || !strings.Contains(view.text, "Ignore, but warn") {
		t.Fatalf("buildCreatorGroupSettingsView() text = %q, want notice and current policy", view.text)
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != "5258318620722733379" {
		t.Fatalf("buildCreatorGroupSettingsView() change policy icon = %q, want %q", got, "5258318620722733379")
	}
}

func TestBuildCreatorGroupPolicyPickerView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupPolicyPickerView("en", core.Creator{TwitchLogin: "creator"}, core.ManagedGroup{ChatID: 1, GroupName: "VIP", Policy: core.GroupPolicyObserve})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupPolicyPickerView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "Ignore and track") {
		t.Fatalf("buildCreatorGroupPolicyPickerView() text = %q, want current policy line", view.text)
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != "5253959125838090076" {
		t.Fatalf("buildCreatorGroupPolicyPickerView() ignore icon = %q, want %q", got, "5253959125838090076")
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].IconCustomEmojiID; got != "5253959125838090076" {
		t.Fatalf("buildCreatorGroupPolicyPickerView() warn icon = %q, want %q", got, "5253959125838090076")
	}
}

func TestBuildCreatorGroupPolicyConfirmView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupPolicyConfirmView("en", core.Creator{TwitchLogin: "creator"}, core.ManagedGroup{ChatID: 1, GroupName: "VIP", Policy: core.GroupPolicyObserve}, core.GroupPolicyGraceWeek)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupPolicyConfirmView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "Current policy") || !strings.Contains(view.text, "Grace 7 days") {
		t.Fatalf("buildCreatorGroupPolicyConfirmView() text = %q, want current and new policy", view.text)
	}
}

func TestBuildCreatorGroupUnregisterConfirmView(t *testing.T) {
	t.Parallel()

	view := buildCreatorGroupUnregisterConfirmView("en", core.Creator{TwitchLogin: "creator"}, core.ManagedGroup{ChatID: 1, GroupName: "VIP"}, creatorMenuCallback())
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupUnregisterConfirmView() = %+v, want non-empty text and markup", view)
	}
}
