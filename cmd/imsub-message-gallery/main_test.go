package main

import (
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
