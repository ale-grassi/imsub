package ui

import (
	"html"
	"strings"

	"imsub/internal/transport/telegram/client"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// HTML is pre-rendered Telegram HTML. Dynamic plain text must be escaped before
// it is converted to HTML.
type HTML string

// TrustedHTML marks a string as already-safe Telegram HTML.
func TrustedHTML(s string) HTML {
	return HTML(s)
}

// EscapeHTML escapes plain text for use in Telegram HTML fields.
func EscapeHTML(s string) HTML {
	return HTML(html.EscapeString(s))
}

// Screen renders a Telegram message using a fixed high-level structure.
type Screen struct {
	Header         HeaderSection
	Requirements   RequirementsSection
	Details        DetailsSection
	Body           []BodySection
	Actions        []ActionGroup
	Navigation     []NavigationItem
	DisablePreview bool
}

// HeaderSection is the emoji-and-title heading rendered at the top of a screen.
type HeaderSection struct {
	Emoji string
	Title string
	// NoticeHTML and BodyHTML are inserted as Telegram HTML. Callers must escape
	// dynamic values before building these fields.
	NoticeHTML HTML
	BodyHTML   HTML
}

// DetailsSection lists label/value detail lines under an optional title.
type DetailsSection struct {
	Title string
	Items []DetailItem
}

// RequirementsSection lists requirement bullet lines under an optional title.
type RequirementsSection struct {
	Title string
	Items []RequirementItem
}

// DetailItem is one label/value line in a DetailsSection.
type DetailItem struct {
	Label string
	// ValueHTML is inserted as Telegram HTML. Callers must escape dynamic values.
	ValueHTML HTML
}

// RequirementState describes how a requirement line is rendered.
type RequirementState string

// Requirement states map to the bullet prefix rendered for each item.
const (
	RequirementStateReady     RequirementState = "ready"
	RequirementStateBlocked   RequirementState = "blocked"
	RequirementStateAttention RequirementState = "attention"
)

// RequirementItem is one requirement line in a RequirementsSection.
type RequirementItem struct {
	Label string
	State RequirementState
	// DetailHTML is inserted as Telegram HTML. Callers must escape dynamic values.
	DetailHTML HTML
}

// BodySection is a free-form HTML block rendered under an optional title.
type BodySection struct {
	Title string
	// TextHTML is inserted as Telegram HTML. Callers must escape dynamic values.
	TextHTML HTML
}

// ActionKind selects the inline button type used for an ActionItem.
type ActionKind string

// Action kinds map to callback, URL, and copy-text inline buttons.
const (
	ActionKindCallback ActionKind = "callback"
	ActionKindURL      ActionKind = "url"
	ActionKindCopy     ActionKind = "copy"
)

// Button styles forwarded to Telegram inline-keyboard buttons via WithStyle.
const (
	StyleSuccess = "success"
	StyleDanger  = "danger"
)

// ActionItem is one inline keyboard button in an ActionGroup.
type ActionItem struct {
	Kind        ActionKind
	Label       string
	Target      string
	IconEmojiID string
	Style       string
	Available   bool
	Reason      string
}

// ActionGroup is one keyboard row of action buttons.
type ActionGroup struct {
	Items []ActionItem
}

// NavigationItem is a back-style navigation button rendered after actions.
type NavigationItem struct {
	Label  string
	Target string
}

// RenderedScreen is the Telegram-ready output of RenderScreen.
type RenderedScreen struct {
	Text string
	Opts client.MessageOptions
}

// RenderScreen renders a Screen to Telegram-ready text and message options.
func RenderScreen(screen Screen) RenderedScreen {
	parts := make([]string, 0, 6)
	if header := renderHeader(screen.Header); header != "" {
		parts = append(parts, header)
	}
	if section := renderRequirementsSection(screen.Requirements); section != "" {
		parts = append(parts, section)
	}
	if section := renderDetailsSection(screen.Details); section != "" {
		parts = append(parts, section)
	}
	for _, body := range screen.Body {
		if section := renderBodySection(body); section != "" {
			parts = append(parts, section)
		}
	}

	return RenderedScreen{
		Text: strings.Join(parts, "\n\n"),
		Opts: client.MessageOptions{
			Markup:         renderActionMarkup(screen.Actions, screen.Navigation),
			DisablePreview: screen.DisablePreview,
		},
	}
}

func renderHeader(header HeaderSection) string {
	emoji := strings.TrimSpace(header.Emoji)
	title := strings.TrimSpace(header.Title)
	if emoji == "" || title == "" {
		return ""
	}
	lines := collectLines(renderHeaderTitle(emoji, title), string(header.BodyHTML))
	if len(lines) == 0 {
		return ""
	}
	text := strings.Join(lines, "\n\n")
	if notice := strings.TrimSpace(string(header.NoticeHTML)); notice != "" {
		return notice + "\n\n" + text
	}
	return text
}

func renderHeaderTitle(emoji, title string) string {
	return emoji + " <b>" + html.EscapeString(title) + "</b>"
}

func renderSectionTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return "<b>" + html.EscapeString(title) + "</b>"
}

func renderDetailsSection(section DetailsSection) string {
	return renderLabelValueSection(section.Title, mapDetailItems(section.Items))
}

func renderRequirementsSection(section RequirementsSection) string {
	lines := make([]string, 0, len(section.Items)+1)
	for _, item := range section.Items {
		line := strings.TrimSpace(renderRequirementItem(item))
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	if title := renderSectionTitle(section.Title); title != "" {
		lines = append([]string{title}, lines...)
	}
	return strings.Join(lines, "\n")
}

func renderRequirementItem(item RequirementItem) string {
	label := strings.TrimSpace(item.Label)
	detail := strings.TrimSpace(string(item.DetailHTML))
	if label == "" && detail == "" {
		return ""
	}
	prefix := "•"
	switch item.State {
	case RequirementStateReady:
		prefix = "• ✅"
	case RequirementStateAttention:
		prefix = "• ⚠️"
	case RequirementStateBlocked:
		prefix = "• ⛔"
	}
	if label == "" {
		return prefix + " " + detail
	}
	label = html.EscapeString(label)
	if detail == "" {
		return prefix + " " + label
	}
	return prefix + " <b>" + label + ":</b> " + detail
}

func renderBodySection(section BodySection) string {
	lines := collectLines(renderSectionTitle(section.Title), string(section.TextHTML))
	return strings.Join(lines, "\n\n")
}

func renderLabelValueSection(title string, items []labelValueItem) string {
	lines := make([]string, 0, len(items)+1)
	for _, item := range items {
		if line := renderLabelValueItem(item); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if title = renderSectionTitle(title); title != "" {
		lines = append([]string{title}, lines...)
	}
	return strings.Join(lines, "\n")
}

type labelValueItem struct {
	label string
	value string
}

func mapDetailItems(items []DetailItem) []labelValueItem {
	out := make([]labelValueItem, 0, len(items))
	for _, item := range items {
		out = append(out, labelValueItem{label: item.Label, value: string(item.ValueHTML)})
	}
	return out
}

func renderLabelValueItem(item labelValueItem) string {
	label := strings.TrimSpace(item.label)
	value := strings.TrimSpace(item.value)
	switch {
	case label == "" && value == "":
		return ""
	case label == "":
		return value
	case value == "":
		return "<b>" + html.EscapeString(label) + "</b>"
	default:
		return "<b>" + html.EscapeString(label) + "</b> " + value
	}
}

func renderActionMarkup(groups []ActionGroup, navigation []NavigationItem) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0)
	for _, group := range groups {
		row := make([]telego.InlineKeyboardButton, 0, len(group.Items))
		for _, item := range group.Items {
			if !item.Available {
				continue
			}
			if button, ok := renderActionButton(item); ok {
				row = append(row, button)
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	for _, item := range navigation {
		label := strings.TrimSpace(item.Label)
		target := strings.TrimSpace(item.Target)
		if label == "" || target == "" {
			continue
		}
		rows = append(rows, tu.InlineKeyboardRow(BackButton(label, target)))
	}
	if len(rows) == 0 {
		return nil
	}
	return tu.InlineKeyboard(rows...)
}

func renderActionButton(item ActionItem) (telego.InlineKeyboardButton, bool) {
	label := strings.TrimSpace(item.Label)
	target := strings.TrimSpace(item.Target)
	if label == "" || target == "" {
		return telego.InlineKeyboardButton{}, false
	}
	var button telego.InlineKeyboardButton
	switch item.Kind {
	case ActionKindURL:
		button = IconURLButton(label, target, item.IconEmojiID)
	case ActionKindCopy:
		button = CopyTextButton(label, target)
	case ActionKindCallback:
		fallthrough
	default:
		button = IconCallbackButton(label, target, item.IconEmojiID)
	}
	if style := strings.TrimSpace(item.Style); style != "" {
		button = button.WithStyle(style)
	}
	return button, true
}

func collectLines(lines ...string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
