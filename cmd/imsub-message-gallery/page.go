package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"imsub/internal/transport/telegram/bot"
)

type outputFormat string

const (
	formatHTML     outputFormat = "html"
	formatMarkdown outputFormat = "md"
)

var (
	errUnsupportedFormatFlag = errors.New("unsupported format flag")
	errUnsupportedLangFlag   = errors.New("unsupported language flag")
	errUnsupportedFormat     = errors.New("unsupported format")
)

type galleryPage struct {
	GeneratedAt string
	Languages   []string
	Sections    []gallerySection
}

type gallerySection struct {
	Name      string
	Scenarios []galleryScenario
}

type galleryScenario struct {
	ID    string
	Title string
	Notes string
	Cards []galleryCard
}

type galleryCard struct {
	Language   string
	RawText    string
	Buttons    [][]bot.PreviewButton
	HasButtons bool
}

func parseFormat(flagValue string) (outputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "", string(formatHTML):
		return formatHTML, nil
	case string(formatMarkdown):
		return formatMarkdown, nil
	default:
		return "", fmt.Errorf("%w: %q", errUnsupportedFormatFlag, flagValue)
	}
}

func selectedLanguages(flagValue string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "", "en":
		return []string{"en"}, nil
	case "all":
		return []string{"en", "it"}, nil
	case "it":
		return []string{"it"}, nil
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedLangFlag, flagValue)
	}
}

func buildPage(langs []string) galleryPage {
	scenarios := bot.PreviewScenarios()
	sections := make([]gallerySection, 0, 8)
	sectionIdx := map[string]int{}

	for _, scenario := range scenarios {
		idx, ok := sectionIdx[scenario.Group]
		if !ok {
			sectionIdx[scenario.Group] = len(sections)
			sections = append(sections, gallerySection{Name: scenario.Group})
			idx = len(sections) - 1
		}

		item := galleryScenario{
			ID:    scenario.ID,
			Title: scenario.Title,
			Notes: scenario.Notes,
			Cards: make([]galleryCard, 0, len(langs)),
		}
		for _, lang := range langs {
			view := scenario.Render(lang)
			item.Cards = append(item.Cards, galleryCard{
				Language:   strings.ToUpper(lang),
				RawText:    view.Text,
				Buttons:    view.Buttons,
				HasButtons: len(view.Buttons) > 0,
			})
		}
		sections[idx].Scenarios = append(sections[idx].Scenarios, item)
	}

	return galleryPage{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Languages:   langs,
		Sections:    sections,
	}
}

func renderPage(page galleryPage, format outputFormat) ([]byte, error) {
	switch format {
	case formatHTML:
		return renderHTML(page)
	case formatMarkdown:
		return renderMarkdown(page)
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedFormat, format)
	}
}
