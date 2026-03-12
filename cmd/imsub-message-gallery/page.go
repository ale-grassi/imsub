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
	formatTelegram outputFormat = "telegram"
)

var (
	errUnsupportedFormatFlag = errors.New("unsupported format flag")
	errUnsupportedLangFlag   = errors.New("unsupported language flag")
	errUnsupportedFormat     = errors.New("unsupported format")
	errUnknownGroupFilter    = errors.New("unknown group filter")
	errUnknownScenarioFilter = errors.New("unknown scenario filter")
)

type galleryFilters struct {
	Group    string
	Scenario string
}

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
	Language       string
	RawText        string
	Buttons        [][]bot.PreviewButton
	HasButtons     bool
	DisablePreview bool
}

func parseFormat(flagValue string) (outputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "", string(formatHTML):
		return formatHTML, nil
	case string(formatMarkdown):
		return formatMarkdown, nil
	case string(formatTelegram):
		return formatTelegram, nil
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

func buildPage(langs []string, filters galleryFilters) (galleryPage, error) {
	scenarios := bot.PreviewScenarios()
	sections := make([]gallerySection, 0, 8)
	sectionIdx := map[string]int{}
	groupFilter := strings.TrimSpace(filters.Group)
	scenarioFilter := strings.TrimSpace(filters.Scenario)
	groupMatched := groupFilter == ""
	scenarioMatched := scenarioFilter == ""

	for _, scenario := range scenarios {
		if groupFilter != "" && scenario.Group != groupFilter {
			continue
		}
		if scenarioFilter != "" && scenario.ID != scenarioFilter {
			continue
		}
		groupMatched = true
		scenarioMatched = true
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
				Language:       strings.ToUpper(lang),
				RawText:        view.Text,
				Buttons:        view.Buttons,
				HasButtons:     len(view.Buttons) > 0,
				DisablePreview: view.DisablePreview,
			})
		}
		sections[idx].Scenarios = append(sections[idx].Scenarios, item)
	}

	if !groupMatched {
		return galleryPage{}, fmt.Errorf("%w: %q", errUnknownGroupFilter, filters.Group)
	}
	if !scenarioMatched {
		return galleryPage{}, fmt.Errorf("%w: %q", errUnknownScenarioFilter, filters.Scenario)
	}
	if groupFilter != "" && scenarioFilter != "" {
		found := false
		for _, section := range sections {
			for _, scenario := range section.Scenarios {
				if scenario.ID == scenarioFilter && section.Name == groupFilter {
					found = true
					break
				}
			}
		}
		if !found {
			return galleryPage{}, fmt.Errorf("%w: %q not in group %q", errUnknownScenarioFilter, filters.Scenario, filters.Group)
		}
	}

	return galleryPage{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Languages:   langs,
		Sections:    sections,
	}, nil
}

func renderPage(page galleryPage, format outputFormat) ([]byte, error) {
	switch format {
	case formatHTML:
		return renderHTML(page)
	case formatMarkdown:
		return renderMarkdown(page)
	case formatTelegram:
		return nil, fmt.Errorf("%w: %q", errUnsupportedFormat, format)
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedFormat, format)
	}
}
