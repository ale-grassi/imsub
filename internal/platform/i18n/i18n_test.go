package i18n

import "testing"

func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "en"},
		{in: "it_IT", want: "it"},
		{in: "it-IT", want: "it"},
		{in: "en-US", want: "en"},
		{in: "fr", want: "en"},
		{in: "???", want: "en"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeLanguage(tc.in); got != tc.want {
				t.Fatalf("NormalizeLanguage(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateMessageCatalogs(t *testing.T) {
	t.Parallel()

	valid := map[string]map[string]string{
		"en": {"a": "A", "b": "B"},
		"it": {"a": "A", "b": "B"},
	}
	if err := ValidateMessageCatalogs(valid, "en"); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	missingBase := map[string]map[string]string{
		"it": {"a": "A"},
	}
	if err := ValidateMessageCatalogs(missingBase, "en"); err == nil {
		t.Fatal("expected missing base language error")
	}

	missingKey := map[string]map[string]string{
		"en": {"a": "A", "b": "B"},
		"it": {"a": "A"},
	}
	if err := ValidateMessageCatalogs(missingKey, "en"); err == nil {
		t.Fatal("expected missing key error")
	}

	extraKey := map[string]map[string]string{
		"en": {"a": "A"},
		"it": {"a": "A", "b": "B"},
	}
	if err := ValidateMessageCatalogs(extraKey, "en"); err == nil {
		t.Fatal("expected extra key error")
	}
}

func TestLoadCatalogs(t *testing.T) {
	t.Parallel()

	catalogs, err := loadCatalogs()
	if err != nil {
		t.Fatalf("loadCatalogs failed: %v", err)
	}
	if _, ok := catalogs["en"]; !ok {
		t.Fatal("expected en catalog")
	}
	if _, ok := catalogs["it"]; !ok {
		t.Fatal("expected it catalog")
	}
	if catalogs["en"]["btn_refresh"] == "" {
		t.Fatal("expected non-empty btn_refresh in en")
	}
}

func TestTranslationLookup(t *testing.T) {
	t.Parallel()

	if err := Ensure(); err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	if got := Tr("it", "btn_refresh"); got == "btn_refresh" {
		t.Fatal("expected translated message for known key")
	}
	if got := Tr("it", "nonexistent_key_for_test"); got != "nonexistent_key_for_test" {
		t.Fatalf("expected key fallback, got %q", got)
	}
}
