package bot

import "testing"

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
			if view.Text == "" {
				t.Fatalf("scenario %q lang %q rendered empty text", scenario.ID, lang)
			}
		}
	}
}
