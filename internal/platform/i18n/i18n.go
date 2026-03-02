package i18n

import (
	"embed"
	"errors"
	"fmt"
	"path"
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
	// ErrNilService indicates an operation was attempted on a nil Service.
	ErrNilService = errors.New("i18n: nil service")

	defaultService = NewService()
)

// Service provides translation lookups backed by embedded locale catalogs.
type Service struct {
	bundlePtr atomic.Pointer[goi18n.Bundle]
	initOnce  sync.Once
	initErr   error
}

// NewService creates a new i18n service instance.
func NewService() *Service {
	return &Service{}
}

// Ensure initializes the default i18n service bundle.
func Ensure() error {
	return defaultService.Ensure()
}

// Ensure initializes the service i18n bundle. It is safe to call multiple times.
func (s *Service) Ensure() error {
	if s == nil {
		return ErrNilService
	}
	s.initOnce.Do(func() {
		bundle, err := loadBundle()
		if err != nil {
			s.initErr = err
			return
		}
		s.bundlePtr.Store(bundle)
	})
	return s.initErr
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

// Translate translates the given key for the specified language.
func Translate(lang, key string) string {
	return defaultService.Translate(lang, key)
}

// Translate translates the given key for the specified language.
func (s *Service) Translate(lang, key string) string {
	if s == nil {
		return key
	}
	bundle := s.bundlePtr.Load()
	if bundle == nil {
		return key
	}
	localizer := goi18n.NewLocalizer(bundle, NormalizeLanguage(lang), DefaultLanguage)
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{
		MessageID:      key,
		DefaultMessage: &goi18n.Message{ID: key, Other: key},
	})
	if err != nil {
		return key
	}
	return msg
}

// Tr translates the given key for the specified language.
//
// Deprecated: use Translate.
func Tr(lang, key string) string {
	return Translate(lang, key)
}

// Tr translates the given key for the specified language.
//
// Deprecated: use (*Service).Translate.
func (s *Service) Tr(lang, key string) string {
	return s.Translate(lang, key)
}

// loadCatalogs reads all embedded locale/*.toml files and returns them
// as a map[lang]map[key]value.
func loadCatalogs() (map[string]map[string]string, error) {
	entries, err := localeFS.ReadDir("locale")
	if err != nil {
		return nil, fmt.Errorf("i18n: reading locale dir: %w", err)
	}

	catalogs := make(map[string]map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".toml")
		data, err := localeFS.ReadFile(path.Join("locale", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("i18n: reading %s: %w", e.Name(), err)
		}
		var raw map[string]struct {
			Other string `toml:"other"`
		}
		if _, err := toml.Decode(string(data), &raw); err != nil {
			return nil, fmt.Errorf("i18n: parsing %s: %w", e.Name(), err)
		}
		dict := make(map[string]string, len(raw))
		for k, msg := range raw {
			dict[k] = msg.Other
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
// as the base language.
func ValidateMessageCatalogs(all map[string]map[string]string, baseLang string) error {
	base, ok := all[baseLang]
	if !ok {
		return fmt.Errorf("i18n: missing base language %q", baseLang)
	}

	baseKeys := make(map[string]struct{}, len(base))
	for k := range base {
		baseKeys[k] = struct{}{}
	}

	for lang, dict := range all {
		for key := range baseKeys {
			if _, exists := dict[key]; !exists {
				return fmt.Errorf("i18n: missing key %q in language %q", key, lang)
			}
		}
		for key := range dict {
			if _, exists := baseKeys[key]; !exists {
				return fmt.Errorf("i18n: extra key %q in language %q", key, lang)
			}
		}
	}

	for lang := range supportedLanguages {
		if _, exists := all[lang]; !exists {
			return fmt.Errorf("i18n: supported language %q has no dictionary", lang)
		}
	}
	return nil
}
