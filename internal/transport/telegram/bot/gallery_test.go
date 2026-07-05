package bot

import (
	"strings"
	"testing"
)

// telegramCallbackDataLimit is the Telegram Bot API hard limit for
// callback_data payloads, in bytes.
const telegramCallbackDataLimit = 64

func TestPreviewScenariosRenderAllLanguages(t *testing.T) {
	t.Parallel()

	scenarios := PreviewScenarios()
	if len(scenarios) == 0 {
		t.Fatal("PreviewScenarios() = empty, want scenarios")
	}

	seen := make(map[string]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		if scenario.ID == "" {
			t.Fatalf("scenario = %+v, want non-empty ID", scenario)
		}
		if _, ok := seen[scenario.ID]; ok {
			t.Fatalf("duplicate scenario ID %q", scenario.ID)
		}
		seen[scenario.ID] = struct{}{}

		for _, lang := range []string{"en", "it"} {
			view := scenario.Render(lang)
			assertRenderedPreview(t, scenario.ID, lang, view)
		}
	}
}

// assertRenderedPreview checks invariants that must hold for every message
// the bot can send: no fmt verb mismatches leaking from locale strings, no
// run-on blank lines, balanced HTML tags, and buttons Telegram will accept.
func assertRenderedPreview(t *testing.T, scenarioID, lang string, view PreviewView) {
	t.Helper()

	if view.Text == "" {
		t.Fatalf("scenario %q lang %q rendered empty text", scenarioID, lang)
	}
	if strings.Contains(view.Text, "%!") {
		t.Errorf("scenario %q lang %q text = %q, contains fmt error marker (locale verb/arg mismatch)", scenarioID, lang, view.Text)
	}
	if strings.Contains(view.Text, "\n\n\n") {
		t.Errorf("scenario %q lang %q text = %q, contains run-on blank lines", scenarioID, lang, view.Text)
	}
	for _, tag := range []string{"b", "i", "code", "a", "u", "s", "pre"} {
		open := strings.Count(view.Text, "<"+tag+">") + strings.Count(view.Text, "<"+tag+" ")
		closed := strings.Count(view.Text, "</"+tag+">")
		if open != closed {
			t.Errorf("scenario %q lang %q text = %q, unbalanced <%s> tags (%d open, %d closed)", scenarioID, lang, view.Text, tag, open, closed)
		}
	}

	for rowIdx, row := range view.Buttons {
		if len(row) == 0 {
			t.Errorf("scenario %q lang %q button row %d is empty", scenarioID, lang, rowIdx)
		}
		for _, button := range row {
			if strings.TrimSpace(button.Label) == "" {
				t.Errorf("scenario %q lang %q button %+v has empty label", scenarioID, lang, button)
			}
			if strings.TrimSpace(button.Target) == "" {
				t.Errorf("scenario %q lang %q button %q has empty target", scenarioID, lang, button.Label)
			}
			switch button.Kind {
			case "callback":
				if len(button.Target) > telegramCallbackDataLimit {
					t.Errorf("scenario %q lang %q button %q callback data %q is %d bytes, exceeds Telegram limit %d", scenarioID, lang, button.Label, button.Target, len(button.Target), telegramCallbackDataLimit)
				}
			case "url", "copy":
				if !strings.Contains(button.Target, "://") {
					t.Errorf("scenario %q lang %q button %q %s target = %q, want absolute URL", scenarioID, lang, button.Label, button.Kind, button.Target)
				}
			default:
				t.Errorf("scenario %q lang %q button %q has unknown kind %q", scenarioID, lang, button.Label, button.Kind)
			}
		}
	}
}
