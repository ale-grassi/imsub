// Command imsub-message-gallery renders Telegram bot message previews as HTML or Markdown.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"imsub/internal/platform/i18n"
)

var (
	errMissingChatID = errors.New("missing chat-id")
	errInvalidChatID = errors.New("invalid chat-id")
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
	formatFlag := fs.String("format", string(formatHTML), "output format: html, md, or telegram")
	groupFlag := fs.String("group", "", "optional exact scenario group filter")
	scenarioFlag := fs.String("scenario", "", "optional exact scenario ID filter")
	chatIDFlag := fs.String("chat-id", "", "telegram chat/user ID for telegram output")
	noHeaderFlag := fs.Bool("no-header", false, "send original card text without gallery header in telegram output")

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

	page, err := buildPage(langs, galleryFilters{
		Group:    *groupFlag,
		Scenario: *scenarioFlag,
	})
	if err != nil {
		return err
	}

	if format == formatTelegram {
		chatID, err := parseChatID(*chatIDFlag)
		if err != nil {
			return err
		}
		result, err := sendTelegramGallery(page, chatID, *noHeaderFlag)
		if err != nil {
			return err
		}
		fmt.Printf("sent %d gallery cards to %d (%s)\n", result.Sent, result.ChatID, strings.Join(langs, ", "))
		return nil
	}
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

func parseChatID(flagValue string) (int64, error) {
	if strings.TrimSpace(flagValue) == "" {
		return 0, fmt.Errorf("%w for %q format", errMissingChatID, formatTelegram)
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(flagValue), 10, 64)
	if err != nil || chatID == 0 {
		return 0, fmt.Errorf("%w %q", errInvalidChatID, flagValue)
	}
	return chatID, nil
}
