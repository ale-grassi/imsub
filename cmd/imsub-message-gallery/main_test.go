package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesGallery(t *testing.T) {
	t.Parallel()

	outPath := filepath.Join(t.TempDir(), "gallery.html")
	if err := run([]string{"--out", outPath, "--lang", "en"}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	//nolint:gosec // Test reads a temp file path created in this test.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", outPath, err)
	}
	body := string(data)
	if !strings.Contains(body, "ImSub Telegram Message Gallery") {
		t.Fatalf("gallery output missing title: %q", body)
	}
	if !strings.Contains(body, "viewer-onboarding") {
		t.Fatalf("gallery output missing expected scenario ID: %q", body)
	}
	if !strings.Contains(body, "page_wrap") || !strings.Contains(body, "message default clearfix") || !strings.Contains(body, "bot_buttons_table") {
		t.Fatalf("gallery output missing telegram export markers: %q", body)
	}
}

func TestRunWritesMarkdownGallery(t *testing.T) {
	t.Parallel()

	outPath := filepath.Join(t.TempDir(), "gallery.md")
	if err := run([]string{"--out", outPath, "--format", "md", "--lang", "all"}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	//nolint:gosec // Test reads a temp file path created in this test.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", outPath, err)
	}
	body := string(data)
	if !strings.Contains(body, "# ImSub Telegram Message Gallery") {
		t.Fatalf("markdown output missing title: %q", body)
	}
	if !strings.Contains(body, "### /start onboarding prompt") {
		t.Fatalf("markdown output missing expected scenario title: %q", body)
	}
	if !strings.Contains(body, "```text") {
		t.Fatalf("markdown output missing text fence: %q", body)
	}
	if !strings.Contains(body, "Buttons:\n- ") {
		t.Fatalf("markdown output missing button summary: %q", body)
	}
	if strings.Contains(body, "[icon]") {
		t.Fatalf("markdown output still contains icon placeholder text: %q", body)
	}
}

func TestRunTelegramRequiresChatID(t *testing.T) {
	t.Parallel()

	err := run([]string{"--format", "telegram"})
	if err == nil {
		t.Fatal("run() error = nil, want missing chat-id error")
	}
	if !strings.Contains(err.Error(), "missing chat-id") {
		t.Fatalf("run() error = %v, want missing chat-id message", err)
	}
}

func TestBuildPageFiltersByGroupAndScenario(t *testing.T) {
	t.Parallel()

	page, err := buildPage([]string{"en"}, galleryFilters{
		Group:    "Viewer",
		Scenario: "viewer-onboarding",
	})
	if err != nil {
		t.Fatalf("buildPage() error = %v", err)
	}
	if len(page.Sections) != 1 {
		t.Fatalf("len(page.Sections) = %d, want 1", len(page.Sections))
	}
	if page.Sections[0].Name != "Viewer" {
		t.Fatalf("page.Sections[0].Name = %q, want %q", page.Sections[0].Name, "Viewer")
	}
	if len(page.Sections[0].Scenarios) != 1 {
		t.Fatalf("len(page.Sections[0].Scenarios) = %d, want 1", len(page.Sections[0].Scenarios))
	}
	if page.Sections[0].Scenarios[0].ID != "viewer-onboarding" {
		t.Fatalf("page.Sections[0].Scenarios[0].ID = %q, want %q", page.Sections[0].Scenarios[0].ID, "viewer-onboarding")
	}
}

func TestBuildPageUnknownFilters(t *testing.T) {
	t.Parallel()

	_, err := buildPage([]string{"en"}, galleryFilters{Group: "Missing"})
	if !errors.Is(err, errUnknownGroupFilter) {
		t.Fatalf("buildPage() error = %v, want errUnknownGroupFilter", err)
	}

	_, err = buildPage([]string{"en"}, galleryFilters{Scenario: "missing-scenario"})
	if !errors.Is(err, errUnknownScenarioFilter) {
		t.Fatalf("buildPage() error = %v, want errUnknownScenarioFilter", err)
	}
}
