package main

import (
	"bytes"
	"fmt"
	"strings"

	"imsub/internal/transport/telegram/bot"
)

func renderMarkdown(page galleryPage) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "# ImSub Telegram Message Gallery\n\n")
	fmt.Fprintf(&buf, "Generated at `%s`.\n\n", page.GeneratedAt)
	fmt.Fprintf(&buf, "Languages: `%s`\n", strings.Join(page.Languages, "`, `"))

	for _, section := range page.Sections {
		fmt.Fprintf(&buf, "\n## %s\n", section.Name)
		for _, scenario := range section.Scenarios {
			fmt.Fprintf(&buf, "\n### %s\n", scenario.Title)
			fmt.Fprintf(&buf, "- ID: `%s`\n", scenario.ID)
			if scenario.Notes != "" {
				fmt.Fprintf(&buf, "- Notes: %s\n", scenario.Notes)
			}
			for _, card := range scenario.Cards {
				if len(page.Languages) > 1 {
					fmt.Fprintf(&buf, "\n#### %s\n", card.Language)
				}
				buf.WriteString("\n```text\n")
				buf.WriteString(card.RawText)
				if !strings.HasSuffix(card.RawText, "\n") {
					buf.WriteByte('\n')
				}
				buf.WriteString("```\n")
				if card.HasButtons {
					fmt.Fprintf(&buf, "Buttons:\n")
					for _, row := range card.Buttons {
						fmt.Fprintf(&buf, "- %s\n", formatMarkdownButtonRow(row))
					}
				} else {
					buf.WriteString("Buttons: none\n")
				}
			}
		}
	}

	return buf.Bytes(), nil
}

func formatMarkdownButtonRow(row []bot.PreviewButton) string {
	labels := make([]string, 0, len(row))
	for _, button := range row {
		labels = append(labels, "["+button.Label+"]")
	}
	return strings.Join(labels, " ")
}
