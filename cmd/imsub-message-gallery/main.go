// Command imsub-message-gallery renders Telegram bot message previews as HTML or Markdown.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"imsub/internal/platform/i18n"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "imsub-message-gallery: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("imsub-message-gallery", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	outPath := fs.String("out", "/tmp/imsub-message-gallery.html", "output path")
	langFlag := fs.String("lang", "en", "language: en, it, or all")
	formatFlag := fs.String("format", string(formatHTML), "output format: html or md")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if err := i18n.Ensure(); err != nil {
		return fmt.Errorf("ensure i18n: %w", err)
	}

	langs, err := selectedLanguages(*langFlag)
	if err != nil {
		return err
	}
	format, err := parseFormat(*formatFlag)
	if err != nil {
		return err
	}

	page := buildPage(langs)
	output, err := renderPage(page, format)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(*outPath, output, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Println(*outPath)
	return nil
}
