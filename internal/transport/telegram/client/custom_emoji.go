package client

import (
	"strings"

	"github.com/mymmrac/telego"
)

type customEmojiReplacement struct {
	standard string
	customID string
}

var htmlCustomEmojiReplacer = strings.NewReplacer(customEmojiHTMLReplacements()...)

var htmlCustomEmojiReplacementTable = []customEmojiReplacement{
	{
		standard: "⏳",
		customID: "5386367538735104399",
	},
	{
		standard: "⚠️",
		customID: "5274099962655816924",
	},
	{
		standard: "✅",
		customID: "5206607081334906820",
	},
	{
		standard: "🎙️",
		customID: "5294339927318739359",
	},
	{
		standard: "⬇️",
		customID: "5406745015365943482",
	},
	{
		standard: "❗️",
		customID: "5274099962655816924",
	},
	{
		standard: "📩",
		customID: "5253742260054409879",
	},
	{
		standard: "😊",
		customID: "5461117441612462242",
	},
	{
		standard: "🔗",
		customID: "5271604874419647061",
	},
	{
		standard: "🎉",
		customID: "5461151367559141950",
	},
	{
		standard: "🚫",
		customID: "5240241223632954241",
	},
	{
		standard: "❔",
		customID: "5452069934089641166",
	},
	{
		standard: "🗑️",
		customID: "5445267414562389170",
	},
	{
		standard: "⚙️",
		customID: "5341715473882955310",
	},
	{
		standard: "➡️",
		customID: "5416117059207572332",
	},
}

func customEmojiHTMLReplacements() []string {
	replacements := make([]string, 0, len(htmlCustomEmojiReplacementTable)*2)
	for _, replacement := range htmlCustomEmojiReplacementTable {
		if replacement.customID == "" {
			continue
		}
		replacements = append(
			replacements,
			replacement.standard,
			customEmojiTag(replacement.standard, replacement.customID),
		)
	}
	return replacements
}

func customEmojiTag(fallback, customEmojiID string) string {
	return `<tg-emoji emoji-id="` + customEmojiID + `">` + fallback + `</tg-emoji>`
}

func transformOutgoingText(text string, opts *MessageOptions) string {
	if text == "" || opts == nil || opts.ParseMode != telego.ModeHTML || opts.DisableCustomEmoji {
		return text
	}

	// Apply global replacements first.
	text = htmlCustomEmojiReplacer.Replace(text)

	return text
}
