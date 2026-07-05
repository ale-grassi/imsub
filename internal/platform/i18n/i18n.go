package i18n

import (
	"embed"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BurntSushi/toml"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// DefaultLanguage is the fallback language code used by localization helpers.
const DefaultLanguage = "en"

var supportedLanguages = map[string]struct{}{
	"en": {},
	"it": {},
}

//go:embed locale/*.toml
var localeFS embed.FS

var (
	errMissingBaseLang = errors.New("i18n: missing base language")
	errMissingKey      = errors.New("i18n: missing key in language")
	errExtraKey        = errors.New("i18n: extra key in language")
	errMissingDict     = errors.New("i18n: supported language has no dictionary")
	errEmptyMessage    = errors.New("i18n: message has no localized content")

	defaultBundlePtr atomic.Pointer[goi18n.Bundle]
	defaultInitOnce  sync.Once
	errDefaultInit   error
)

// Ensure initializes the default i18n service bundle.
func Ensure() error {
	defaultInitOnce.Do(func() {
		bundle, err := loadBundle()
		if err != nil {
			errDefaultInit = err
			return
		}
		defaultBundlePtr.Store(bundle)
	})
	return errDefaultInit
}

// NormalizeLanguage returns the two-letter language code for the given input,
// falling back to [DefaultLanguage] for unsupported or unparseable values.
func NormalizeLanguage(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return DefaultLanguage
	}
	trimmed = strings.ReplaceAll(trimmed, "_", "-")
	tag, err := language.Parse(trimmed)
	if err != nil {
		return DefaultLanguage
	}
	base, _ := tag.Base()
	lang := strings.ToLower(base.String())
	if _, ok := supportedLanguages[lang]; ok {
		return lang
	}
	return DefaultLanguage
}

// Translate translates the given key for the specified language using the
// default "other" form and no template data.
func Translate(lang, key string) string {
	return Localize(lang, &goi18n.LocalizeConfig{
		MessageID:      key,
		DefaultMessage: &goi18n.Message{ID: key, Other: key},
	})
}

// Localize resolves a message using the full go-i18n LocalizeConfig so callers
// can opt into plural forms and template data when needed.
func Localize(lang string, cfg *goi18n.LocalizeConfig) string {
	bundle := defaultBundlePtr.Load()
	if bundle == nil {
		return fallbackLocalizedText(cfg)
	}
	localizer := goi18n.NewLocalizer(bundle, NormalizeLanguage(lang), DefaultLanguage)
	msg, err := localizer.Localize(cfg)
	if err != nil {
		return fallbackLocalizedText(cfg)
	}
	return msg
}

// AllKeys returns a sorted list of every message key present in the base
// language catalog. It is intended for tests and tooling that need to
// enumerate translations (for example, to verify that an authoring rule
// covers the entire catalog, not just an allow-listed subset).
func AllKeys() ([]string, error) {
	catalogs, err := loadCatalogs()
	if err != nil {
		return nil, err
	}
	base, ok := catalogs[DefaultLanguage]
	if !ok {
		return nil, fmt.Errorf("%w %q", errMissingBaseLang, DefaultLanguage)
	}
	out := make([]string, 0, len(base))
	for k := range base {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// loadCatalogs reads all embedded locale/*.toml files and returns them
// as a map[lang]map[key]message.
func loadCatalogs() (map[string]map[string]goi18n.Message, error) {
	entries, err := localeFS.ReadDir("locale")
	if err != nil {
		return nil, fmt.Errorf("i18n: reading locale dir: %w", err)
	}

	catalogs := make(map[string]map[string]goi18n.Message, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".toml")
		data, err := localeFS.ReadFile(path.Join("locale", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("i18n: reading %s: %w", e.Name(), err)
		}
		var raw map[string]goi18n.Message
		if _, err := toml.Decode(string(data), &raw); err != nil {
			return nil, fmt.Errorf("i18n: parsing %s: %w", e.Name(), err)
		}
		dict := make(map[string]goi18n.Message, len(raw))
		for k, msg := range raw {
			msg.ID = k
			dict[k] = msg
		}
		catalogs[lang] = dict
	}
	return catalogs, nil
}

func loadBundle() (*goi18n.Bundle, error) {
	catalogs, err := loadCatalogs()
	if err != nil {
		return nil, err
	}
	if err := ValidateMessageCatalogs(catalogs, DefaultLanguage); err != nil {
		return nil, err
	}

	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	entries, err := localeFS.ReadDir("locale")
	if err != nil {
		return nil, fmt.Errorf("i18n: reading locale dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		if _, err := b.LoadMessageFileFS(localeFS, path.Join("locale", e.Name())); err != nil {
			return nil, fmt.Errorf("i18n: loading %q: %w", e.Name(), err)
		}
	}
	return b, nil
}

// ValidateMessageCatalogs checks that all catalogs have the same set of keys
// as the base language and that each key has at least one localized form.
func ValidateMessageCatalogs(all map[string]map[string]goi18n.Message, baseLang string) error {
	base, ok := all[baseLang]
	if !ok {
		return fmt.Errorf("%w %q", errMissingBaseLang, baseLang)
	}

	baseKeys := make(map[string]struct{}, len(base))
	for k := range base {
		baseKeys[k] = struct{}{}
	}

	for lang, dict := range all {
		for key := range baseKeys {
			msg, exists := dict[key]
			if !exists {
				return fmt.Errorf("%w: key %q in lang %q", errMissingKey, key, lang)
			}
			if !hasLocalizedContent(msg) {
				return fmt.Errorf("%w: key %q in lang %q", errEmptyMessage, key, lang)
			}
		}
		for key := range dict {
			if _, exists := baseKeys[key]; !exists {
				return fmt.Errorf("%w: key %q in lang %q", errExtraKey, key, lang)
			}
		}
	}

	for lang := range supportedLanguages {
		if _, exists := all[lang]; !exists {
			return fmt.Errorf("%w: %q", errMissingDict, lang)
		}
	}
	return nil
}

func fallbackLocalizedText(cfg *goi18n.LocalizeConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.DefaultMessage != nil && cfg.DefaultMessage.Other != "" {
		return cfg.DefaultMessage.Other
	}
	return cfg.MessageID
}

func hasLocalizedContent(msg goi18n.Message) bool {
	return msg.Other != "" ||
		msg.Zero != "" ||
		msg.One != "" ||
		msg.Two != "" ||
		msg.Few != "" ||
		msg.Many != ""
}
