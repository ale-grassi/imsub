package flows

import (
	"strings"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
)

func TestBuildCreatorPromptView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorPromptView("en", "https://example.com/auth", false)
	if view.opts.ParseMode != telego.ModeHTML {
		t.Fatalf("ParseMode = %q, want %q", view.opts.ParseMode, telego.ModeHTML)
	}
	if view.opts.Markup == nil {
		t.Fatal("Markup = nil, want creator prompt buttons")
	}
	if !strings.Contains(view.text, "Twitch") && view.text == "" {
		t.Fatalf("text = %q, want non-empty prompt text", view.text)
	}
}

func TestBuildCreatorStatusViewNoGroups(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{Name: "streamer"}, core.Status{
		Auth:       core.CreatorAuthHealthy,
		LastSyncAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}, nil)
	if view.opts.ParseMode != telego.ModeHTML {
		t.Fatalf("ParseMode = %q, want %q", view.opts.ParseMode, telego.ModeHTML)
	}
	if !view.opts.DisablePreview {
		t.Fatal("DisablePreview = false, want true")
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].CallbackData; got == creatorManageGroupsCallback() {
		t.Fatalf("manage groups callback unexpectedly present in no-groups status view")
	}
	if !strings.Contains(view.text, "streamer") {
		t.Fatalf("text = %q, want creator name present", view.text)
	}
}

func TestBuildCreatorStatusViewWithSingleGroup(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorStatusView("en", "https://example.com/reconnect", core.Creator{Name: "streamer"}, core.Status{
		EventSub:           core.EventSubActive,
		Auth:               core.CreatorAuthReconnectRequired,
		AuthStatusAt:       time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC),
		LastSyncAt:         time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
		HasSubscriberCount: true,
		SubscriberCount:    42,
	}, []core.ManagedGroup{{GroupName: "VIP"}})
	if view.opts.Markup == nil {
		t.Fatal("Markup = nil, want creator status menu")
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].CallbackData; got != creatorManageGroupsCallback() {
		t.Fatalf("manage groups callback = %q, want %q", got, creatorManageGroupsCallback())
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != uiGroupButtonEmojiID() {
		t.Fatalf("manage group icon = %q, want %q", got, uiGroupButtonEmojiID())
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].Text; got != "Manage Group - VIP" {
		t.Fatalf("manage group text = %q, want %q", got, "Manage Group - VIP")
	}
	if got := view.opts.Markup.InlineKeyboard[2][0].CallbackData; got != resetOpenCallback(resetOriginCreator) {
		t.Fatalf("reset callback = %q, want %q", got, resetOpenCallback(resetOriginCreator))
	}
	if !strings.Contains(view.text, "streamer") || !strings.Contains(view.text, "VIP") {
		t.Fatalf("text = %q, want creator and group names present", view.text)
	}
}

func TestBuildCreatorStatusViewWithMultipleGroups(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorStatusView("en", "", core.Creator{Name: "streamer"}, core.Status{
		EventSub:           core.EventSubActive,
		Auth:               core.CreatorAuthHealthy,
		LastSyncAt:         time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
		HasSubscriberCount: true,
		SubscriberCount:    42,
	}, []core.ManagedGroup{{GroupName: "VIP One"}, {GroupName: "VIP Two"}})
	if view.opts.Markup == nil {
		t.Fatal("Markup = nil, want creator status menu")
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].CallbackData; got != creatorManageGroupsCallback() {
		t.Fatalf("manage groups callback = %q, want %q", got, creatorManageGroupsCallback())
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].IconCustomEmojiID; got != uiManageButtonEmojiID() {
		t.Fatalf("manage groups icon = %q, want %q", got, uiManageButtonEmojiID())
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].Text; got != "Manage groups" {
		t.Fatalf("manage groups text = %q, want %q", got, "Manage groups")
	}
}

func TestBuildCreatorManagedGroupsView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorManagedGroupsView("en", core.Creator{Name: "streamer"}, []core.ManagedGroup{
		{ChatID: -1001, GroupName: "VIP"},
		{ChatID: -1002, GroupName: "VIP"},
	}, "notice")
	if view.opts.Markup == nil {
		t.Fatal("Markup = nil, want group management buttons")
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].CallbackData; got != creatorGroupPickCallback(-1001) {
		t.Fatalf("first group callback = %q, want %q", got, creatorGroupPickCallback(-1001))
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != uiGroupButtonEmojiID() {
		t.Fatalf("first group icon = %q, want %q", got, uiGroupButtonEmojiID())
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].Text; !strings.Contains(got, "(-1002)") {
		t.Fatalf("second group label = %q, want duplicate suffix", got)
	}
	if got := view.opts.Markup.InlineKeyboard[2][0].CallbackData; got != creatorMenuCallback() {
		t.Fatalf("back callback = %q, want %q", got, creatorMenuCallback())
	}
}

func TestBuildCreatorGroupUnregisterConfirmView(t *testing.T) {
	t.Parallel()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

	view := buildCreatorGroupUnregisterConfirmView(
		"en",
		core.Creator{Name: "streamer"},
		core.ManagedGroup{ChatID: -1001, GroupName: "VIP"},
		creatorMenuCallback(),
	)
	if view.opts.Markup == nil {
		t.Fatal("Markup = nil, want confirm buttons")
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].CallbackData; got != creatorGroupExecuteCallback(-1001) {
		t.Fatalf("confirm callback = %q, want %q", got, creatorGroupExecuteCallback(-1001))
	}
	if got := view.opts.Markup.InlineKeyboard[0][0].IconCustomEmojiID; got != uiUnregisterEmojiID() {
		t.Fatalf("confirm icon = %q, want %q", got, uiUnregisterEmojiID())
	}
	if got := view.opts.Markup.InlineKeyboard[1][0].CallbackData; got != creatorMenuCallback() {
		t.Fatalf("back callback = %q, want %q", got, creatorMenuCallback())
	}
}

func uiGroupButtonEmojiID() string {
	return "5258513401784573443"
}

func uiManageButtonEmojiID() string {
	return "5258096772776991776"
}

func uiUnregisterEmojiID() string {
	return "5258084656674250503"
}
