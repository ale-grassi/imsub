package bot

import (
	"strings"
	"testing"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
)

func TestCreatorGroupLineEscapesHTML(t *testing.T) {
	t.Parallel()

	line := CreatorGroupLines("en", []core.ManagedGroup{{GroupName: `group "x"`}})
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
	if !strings.Contains(view.text, "Synced banned users") {
		t.Fatalf("buildCreatorStatusView() text = %q, want synced banned users line", view.text)
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

func TestBuildCreatorStatusViewWithGraceEnabled(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{
		TwitchLogin:          "creator",
		SubscriptionEndGrace: core.SubscriptionEndGrace48h,
	}, core.Status{}, []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}})
	if !strings.Contains(view.text, "48 hours") {
		t.Fatalf("buildCreatorStatusView() text = %q, want grace period line", view.text)
	}
	found := false
	for _, row := range view.opts.Markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == creatorGraceOpenCallback() {
				found = true
				if button.Style != "success" {
					t.Fatalf("grace button style = %q, want success", button.Style)
				}
				if button.IconCustomEmojiID != "5258318620722733379" {
					t.Fatalf("grace button icon = %q, want %q", button.IconCustomEmojiID, "5258318620722733379")
				}
			}
		}
	}
	if !found {
		t.Fatalf("buildCreatorStatusView() markup = %+v, want grace button", view.opts.Markup.InlineKeyboard)
	}
}

func TestBuildCreatorGracePickerView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGracePickerView("en", core.Creator{TwitchLogin: "creator", SubscriptionEndGrace: core.SubscriptionEndGrace24h})
	if !strings.Contains(view.text, "24 hours") {
		t.Fatalf("buildCreatorGracePickerView() text = %q, want current grace", view.text)
	}
	if view.opts.Markup == nil || len(view.opts.Markup.InlineKeyboard) != 5 {
		t.Fatalf("buildCreatorGracePickerView() markup = %+v, want 5 rows", view.opts.Markup)
	}
	if got := view.opts.Markup.InlineKeyboard[2][0].CallbackData; got != creatorGraceExecuteCallback(core.SubscriptionEndGrace48h) {
		t.Fatalf("buildCreatorGracePickerView() 48h callback = %q, want %q", got, creatorGraceExecuteCallback(core.SubscriptionEndGrace48h))
	}
}

func TestBuildCreatorManagedGroupsView(t *testing.T) {
	t.Parallel()

	view := buildCreatorManagedGroupsView("en", []core.ManagedGroup{{ChatID: 1, GroupName: "VIP"}}, "")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorManagedGroupsView() = %+v, want non-empty text and markup", view)
	}
}

func TestBuildCreatorGroupSettingsView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupSettingsView("en", core.ManagedGroup{ChatID: 1, GroupName: "VIP", Language: "it", Policy: core.GroupPolicyObserveWarn}, creatorMenuCallback(), "notice")
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupSettingsView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "notice") || !strings.Contains(view.text, "Allow, but warn") || !strings.Contains(view.text, "Italiano") {
		t.Fatalf("buildCreatorGroupSettingsView() text = %q, want notice and current policy/language", view.text)
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != "5258318620722733379" {
		t.Fatalf("buildCreatorGroupSettingsView() change policy icon = %q, want %q", got, "5258318620722733379")
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].CallbackData; got != creatorGroupLanguageOpenCallback(1) {
		t.Fatalf("buildCreatorGroupSettingsView() language callback = %q, want %q", got, creatorGroupLanguageOpenCallback(1))
	}
}

func TestBuildCreatorGroupLanguagePickerView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupLanguagePickerView("en", core.ManagedGroup{ChatID: 1, GroupName: "VIP", Language: "it"})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupLanguagePickerView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "Current language") || !strings.Contains(view.text, "Italiano") {
		t.Fatalf("buildCreatorGroupLanguagePickerView() text = %q, want current language", view.text)
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].CallbackData; got != creatorGroupLanguageExecuteCallback(1, "en") {
		t.Fatalf("buildCreatorGroupLanguagePickerView() callback = %q, want %q", got, creatorGroupLanguageExecuteCallback(1, "en"))
	}
}

func TestBuildCreatorGroupPolicyPickerView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure failed: %v", err)
	}

	view := buildCreatorGroupPolicyPickerView("en", core.ManagedGroup{ChatID: 1, GroupName: "VIP", Policy: core.GroupPolicyObserve})
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupPolicyPickerView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "Allow") {
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

	view := buildCreatorGroupPolicyConfirmView("en", core.ManagedGroup{ChatID: 1, GroupName: "VIP", Policy: core.GroupPolicyObserve}, core.GroupPolicyGraceWeek)
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupPolicyConfirmView() = %+v, want non-empty text and markup", view)
	}
	if !strings.Contains(view.text, "Current policy") || !strings.Contains(view.text, "Allow 7 days, then remove") {
		t.Fatalf("buildCreatorGroupPolicyConfirmView() text = %q, want current and new policy", view.text)
	}
}

func TestBuildCreatorGroupUnregisterConfirmView(t *testing.T) {
	t.Parallel()

	view := buildCreatorGroupUnregisterConfirmView("en", core.ManagedGroup{ChatID: 1, GroupName: "VIP"}, creatorMenuCallback())
	if view.text == "" || view.opts.Markup == nil {
		t.Fatalf("buildCreatorGroupUnregisterConfirmView() = %+v, want non-empty text and markup", view)
	}
}
