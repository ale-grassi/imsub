package ui

import (
	"strings"
	"testing"
)

func TestRenderScreenOrdersSectionsAndActions(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{
			Emoji:      "⚙️",
			NoticeHTML: TrustedHTML("notice"),
			Title:      "Header",
			BodyHTML:   TrustedHTML("intro"),
		},
		Details: DetailsSection{
			Title: "Details",
			Items: []DetailItem{
				{Label: "Group:", ValueHTML: TrustedHTML("VIP")},
				{Label: "Current policy:", ValueHTML: TrustedHTML("Allow")},
			},
		},
		Requirements: RequirementsSection{
			Title: "Requirements",
			Items: []RequirementItem{
				{Label: "Manage Tags", State: RequirementStateReady, DetailHTML: TrustedHTML("Granted")},
				{Label: "Broadcaster scopes", State: RequirementStateBlocked, DetailHTML: TrustedHTML("Reconnect required")},
			},
		},
		Body: []BodySection{{Title: "Notes", TextHTML: TrustedHTML("body")}},
		Actions: []ActionGroup{{
			Items: []ActionItem{
				{Kind: ActionKindCallback, Label: "Open", Target: "cb:open", Available: true},
				{Kind: ActionKindCallback, Label: "Hidden", Target: "cb:hidden", Available: false, Reason: "blocked"},
			},
		}},
		Navigation:     []NavigationItem{{Label: "Back", Target: "cb:back"}},
		DisablePreview: true,
	})

	wantParts := []string{
		"notice",
		"⚙️ <b>Header</b>",
		"<b>Requirements</b>",
		"• ✅ <b>Manage Tags:</b> Granted",
		"• ⛔ <b>Broadcaster scopes:</b> Reconnect required",
		"<b>Details</b>",
		"<b>Group:</b> VIP",
		"<b>Current policy:</b> Allow",
		"<b>Notes</b>",
		"body",
	}
	for _, want := range wantParts {
		if !strings.Contains(rendered.Text, want) {
			t.Fatalf("RenderScreen() text = %q, want substring %q", rendered.Text, want)
		}
	}
	if !rendered.Opts.DisablePreview {
		t.Fatal("RenderScreen() DisablePreview = false, want true")
	}
	if rendered.Opts.Markup == nil || len(rendered.Opts.Markup.InlineKeyboard) != 2 {
		t.Fatalf("RenderScreen() markup = %+v, want 2 rows", rendered.Opts.Markup)
	}
	if got := rendered.Opts.Markup.InlineKeyboard[0][0].CallbackData; got != "cb:open" {
		t.Fatalf("RenderScreen() first callback = %q, want %q", got, "cb:open")
	}
	if got := rendered.Opts.Markup.InlineKeyboard[1][0].CallbackData; got != "cb:back" {
		t.Fatalf("RenderScreen() back callback = %q, want %q", got, "cb:back")
	}
}

func TestRenderScreenOmitsEmptySections(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Requirements: RequirementsSection{
			Title: "Requirements",
			Items: []RequirementItem{{State: RequirementStateAttention, DetailHTML: TrustedHTML("warning")}},
		},
		Details: DetailsSection{Title: "Details"},
	})

	if strings.Contains(rendered.Text, "<b>Details</b>") {
		t.Fatalf("RenderScreen() text = %q, did not expect empty details heading", rendered.Text)
	}
	if !strings.Contains(rendered.Text, "<b>Requirements</b>") {
		t.Fatalf("RenderScreen() text = %q, want non-empty requirements heading", rendered.Text)
	}
	if !strings.Contains(rendered.Text, "• ⚠️ warning") {
		t.Fatalf("RenderScreen() text = %q, want rendered requirement line", rendered.Text)
	}

	rendered = RenderScreen(Screen{
		Header:       HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Requirements: RequirementsSection{Title: "Requirements"},
	})
	if strings.Contains(rendered.Text, "<b>Requirements</b>") {
		t.Fatalf("RenderScreen() text = %q, did not expect empty requirements heading", rendered.Text)
	}
}

func TestRenderScreenGroupsActionItemsIntoOneRowPerActionGroup(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Actions: []ActionGroup{
			{Items: []ActionItem{
				{Kind: ActionKindCallback, Label: "One", Target: "cb:one", Available: true},
				{Kind: ActionKindCallback, Label: "Two", Target: "cb:two", Available: true},
			}},
			{Items: []ActionItem{
				{Kind: ActionKindCallback, Label: "Three", Target: "cb:three", Available: true},
			}},
		},
	})

	if rendered.Opts.Markup == nil || len(rendered.Opts.Markup.InlineKeyboard) != 2 {
		t.Fatalf("RenderScreen() markup = %+v, want 2 rows", rendered.Opts.Markup)
	}
	if got := len(rendered.Opts.Markup.InlineKeyboard[0]); got != 2 {
		t.Fatalf("RenderScreen() first row buttons = %d, want 2", got)
	}
	if got := rendered.Opts.Markup.InlineKeyboard[0][0].CallbackData; got != "cb:one" {
		t.Fatalf("RenderScreen() first grouped callback = %q, want %q", got, "cb:one")
	}
	if got := rendered.Opts.Markup.InlineKeyboard[0][1].CallbackData; got != "cb:two" {
		t.Fatalf("RenderScreen() second grouped callback = %q, want %q", got, "cb:two")
	}
}

func TestRenderScreenEscapesPlainTextFields(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "⚙️", Title: `Tags <b>&</b> "quotes"`},
		Requirements: RequirementsSection{
			Title: "Reqs <i>&</i>",
			Items: []RequirementItem{{Label: "Scope <a>&</a>", State: RequirementStateReady, DetailHTML: TrustedHTML("<code>ok</code>")}},
		},
		Details: DetailsSection{
			Title: "Details <u>&</u>",
			Items: []DetailItem{{Label: "Account <s>&</s>:", ValueHTML: TrustedHTML("<code>safe</code>")}},
		},
	})

	for _, want := range []string{
		"<b>Tags &lt;b&gt;&amp;&lt;/b&gt; &#34;quotes&#34;</b>",
		"<b>Reqs &lt;i&gt;&amp;&lt;/i&gt;</b>",
		"• ✅ <b>Scope &lt;a&gt;&amp;&lt;/a&gt;:</b> <code>ok</code>",
		"<b>Details &lt;u&gt;&amp;&lt;/u&gt;</b>",
		"<b>Account &lt;s&gt;&amp;&lt;/s&gt;:</b> <code>safe</code>",
	} {
		if !strings.Contains(rendered.Text, want) {
			t.Errorf("RenderScreen() text = %q, want escaped substring %q", rendered.Text, want)
		}
	}
	for _, tainted := range []string{"<b>&</b>", "<i>&</i>", "<a>&</a>", "<u>&</u>", "<s>&</s>"} {
		if strings.Contains(rendered.Text, tainted) {
			t.Errorf("RenderScreen() text = %q, contains unescaped plain-text input %q", rendered.Text, tainted)
		}
	}
}

func TestRenderScreenActionButtonKinds(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Actions: []ActionGroup{
			{Items: []ActionItem{{Kind: ActionKindURL, Label: "Open", Target: "https://example.com", IconEmojiID: "123", Style: "primary", Available: true}}},
			{Items: []ActionItem{{Kind: ActionKindCopy, Label: "Copy", Target: "https://t.me/bot", Available: true}}},
			{Items: []ActionItem{{Kind: ActionKindCallback, Label: "Do", Target: "cb:do", IconEmojiID: "456", Style: "danger", Available: true}}},
			{Items: []ActionItem{{Kind: ActionKindCallback, Label: "", Target: "cb:nolabel", Available: true}}},
			{Items: []ActionItem{{Kind: ActionKindCallback, Label: "No target", Target: "  ", Available: true}}},
		},
	})

	markup := rendered.Opts.Markup
	if markup == nil || len(markup.InlineKeyboard) != 3 {
		t.Fatalf("RenderScreen() markup = %+v, want 3 rows (label-less and target-less buttons dropped)", markup)
	}
	urlButton := markup.InlineKeyboard[0][0]
	if urlButton.URL != "https://example.com" || urlButton.IconCustomEmojiID != "123" || urlButton.Style != "primary" {
		t.Errorf("RenderScreen() url button = %+v, want URL, icon 123, primary style", urlButton)
	}
	copyButton := markup.InlineKeyboard[1][0]
	if copyButton.CopyText == nil || copyButton.CopyText.Text != "https://t.me/bot" {
		t.Errorf("RenderScreen() copy button = %+v, want copy text %q", copyButton, "https://t.me/bot")
	}
	cbButton := markup.InlineKeyboard[2][0]
	if cbButton.CallbackData != "cb:do" || cbButton.IconCustomEmojiID != "456" || cbButton.Style != "danger" {
		t.Errorf("RenderScreen() callback button = %+v, want cb:do, icon 456, danger style", cbButton)
	}
}

func TestRenderScreenWithoutButtonsHasNilMarkup(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header:  HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Actions: []ActionGroup{{Items: []ActionItem{{Kind: ActionKindCallback, Label: "Hidden", Target: "cb:h", Available: false}}}},
	})
	if rendered.Opts.Markup != nil {
		t.Fatalf("RenderScreen() markup = %+v, want nil when no button is visible", rendered.Opts.Markup)
	}
}

func TestEscapeHTML(t *testing.T) {
	t.Parallel()

	if got, want := EscapeHTML(`<b>&"'`), HTML("&lt;b&gt;&amp;&#34;&#39;"); got != want {
		t.Fatalf("EscapeHTML() = %q, want %q", got, want)
	}
	if got, want := TrustedHTML("<b>kept</b>"), HTML("<b>kept</b>"); got != want {
		t.Fatalf("TrustedHTML() = %q, want %q", got, want)
	}
}

func TestRenderScreenHeaderRequiresEmojiAndTitle(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "✅", Title: "  ", BodyHTML: TrustedHTML("orphan body")},
		Body:   []BodySection{{TextHTML: TrustedHTML("real body")}},
	})
	if strings.Contains(rendered.Text, "orphan body") {
		t.Fatalf("RenderScreen() text = %q, header without title must render nothing", rendered.Text)
	}
	if rendered.Text != "real body" {
		t.Fatalf("RenderScreen() text = %q, want %q", rendered.Text, "real body")
	}
}

func TestRenderScreenPartialItems(t *testing.T) {
	t.Parallel()

	rendered := RenderScreen(Screen{
		Header: HeaderSection{Emoji: "ℹ️", Title: "Title"},
		Requirements: RequirementsSection{Items: []RequirementItem{
			{Label: "Label only", State: RequirementStateReady},
			{DetailHTML: TrustedHTML("detail only")},
		}},
		Details: DetailsSection{Items: []DetailItem{
			{Label: "Label only:"},
			{ValueHTML: TrustedHTML("value only")},
			{},
		}},
	})

	for _, want := range []string{
		"• ✅ Label only",
		"• detail only",
		"<b>Label only:</b>\nvalue only",
	} {
		if !strings.Contains(rendered.Text, want) {
			t.Errorf("RenderScreen() text = %q, want substring %q", rendered.Text, want)
		}
	}
}
