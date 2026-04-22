package bot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"imsub/internal/platform/i18n"
)

// i18n keys sent only in private (DM) chats. DM-only commands must appear bare
// so Telegram auto-links them; group-only commands must be wrapped in <code>…</code>.
var i18nPrivateKeys = map[string]struct{}{
	"cmd_help":                                     {},
	"cmd_help_both":                                {},
	"cmd_help_creator":                             {},
	"cmd_help_viewer":                              {},
	"cmd_info_html":                                {},
	"creator_register_info":                        {},
	"creator_reconnect_info":                       {},
	"creator_reconnect_mismatch":                   {},
	"err_creator_link":                             {},
	"err_viewer_generic":                           {},
	"link_prompt_html":                             {},
	"linked_status_no_subs_body_html_no_account":   {},
	"linked_status_with_subs_body_html_no_account": {},
	"linked_status_with_subs_no_groups_body_html_no_account": {},
	"sub_end_partial": {},
}

// i18n keys sent only in group chats. Every slash-command must be wrapped in
// <code>…</code> so Telegram does not auto-link it inside the group message.
var i18nGroupKeys = map[string]struct{}{
	"group_not_creator":                  {},
	"group_setup_permissions_html":       {},
	"group_permissions_blocked_html":     {},
	"group_policy_existing_members_html": {},
	"group_already_linked":               {},
	"group_different_linked":             {},
	"group_taken_by_other":               {},
}

// i18nCommandRegex matches every slash-command known to the rule. It is
// derived from dmSlashCommands and groupSlashCommands so the command sets
// stay the single source of truth.
var (
	i18nCommandRegex   = regexp.MustCompile(buildSlashCommandPattern(dmSlashCommands, groupSlashCommands))
	i18nCodeBlockRegex = regexp.MustCompile(`<code>[^<]*</code>`)
)

func buildSlashCommandPattern(sets ...map[string]struct{}) string {
	var names []string
	for _, set := range sets {
		for cmd := range set {
			names = append(names, strings.TrimPrefix(cmd, "/"))
		}
	}
	sort.Strings(names)
	return `/(?:` + strings.Join(names, "|") + `)\b`
}

// TestI18NCommandWrappingRule enforces the DM-vs-group slash-command wrapping
// rule defined in i18n_authoring.go across every supported locale. Any
// message key that mentions a slash-command but has not been classified into
// i18nPrivateKeys or i18nGroupKeys is flagged, so new messages must declare
// their chat scope before the test will pass.
//
// If you see this test fail, read the package comment in i18n_authoring.go
// for the canonical rule and its rationale.
func TestI18NCommandWrappingRule(t *testing.T) {
	t.Parallel()

	langs := []string{"en", "it"}

	allKeys, err := i18n.AllKeys()
	if err != nil {
		t.Fatalf("i18n.AllKeys() error = %v", err)
	}
	if len(allKeys) == 0 {
		t.Fatalf("i18n.AllKeys() returned no keys; catalog failed to load")
	}

	keysWithCommands := collectI18NKeysWithCommands(allKeys, langs)
	for _, key := range keysWithCommands {
		_, isPrivate := i18nPrivateKeys[key]
		_, isGroup := i18nGroupKeys[key]
		switch {
		case isPrivate && isGroup:
			t.Errorf("i18n key %q is classified as both DM and group; pick one", key)
		case !isPrivate && !isGroup:
			t.Errorf("i18n key %q references slash-commands but is not classified; add it to i18nPrivateKeys or i18nGroupKeys in i18n_command_rule_test.go", key)
		}
	}

	for _, lang := range langs {
		for key := range i18nPrivateKeys {
			for _, reason := range checkDMWrappingRule(i18n.Translate(lang, key)) {
				t.Errorf("i18n[%s][%s] %s", lang, key, reason)
			}
		}
		for key := range i18nGroupKeys {
			for _, reason := range checkGroupWrappingRule(i18n.Translate(lang, key)) {
				t.Errorf("i18n[%s][%s] %s", lang, key, reason)
			}
		}
	}
}

// collectI18NKeysWithCommands returns every key in keys whose translation
// in any of langs contains a slash-command recognized by the rule. It
// deliberately scans the full catalog (not just the classified subset) so
// that unclassified keys are surfaced to TestI18NCommandWrappingRule.
func collectI18NKeysWithCommands(keys, langs []string) []string {
	seen := map[string]struct{}{}
	for _, key := range keys {
		for _, lang := range langs {
			if i18nCommandRegex.MatchString(i18n.Translate(lang, key)) {
				seen[key] = struct{}{}
				break
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func checkDMWrappingRule(text string) []string {
	var out []string
	for _, cmd := range bareCommandsIn(text) {
		if _, ok := groupSlashCommands[cmd]; ok {
			out = append(out, "is a DM message but contains bare group command "+cmd+"; wrap it in <code>"+cmd+"</code>")
		}
	}
	for _, cmd := range wrappedCommandsIn(text) {
		if _, ok := dmSlashCommands[cmd]; ok {
			out = append(out, "is a DM message but wraps DM command "+cmd+" in <code>; leave it bare so Telegram auto-links it")
		}
	}
	return out
}

func checkGroupWrappingRule(text string) []string {
	bare := bareCommandsIn(text)
	if len(bare) == 0 {
		return nil
	}
	return []string{"is a group message but contains bare commands " + joinSorted(bare) + "; wrap each in <code>…</code>"}
}

// bareCommandsIn returns every slash-command that appears outside of a
// <code>…</code> block in text.
func bareCommandsIn(text string) []string {
	stripped := i18nCodeBlockRegex.ReplaceAllString(text, " ")
	return dedupeStrings(i18nCommandRegex.FindAllString(stripped, -1))
}

// wrappedCommandsIn returns every slash-command that appears inside a
// <code>…</code> block in text.
func wrappedCommandsIn(text string) []string {
	blocks := i18nCodeBlockRegex.FindAllString(text, -1)
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, i18nCommandRegex.FindAllString(block, -1)...)
	}
	return dedupeStrings(out)
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func joinSorted(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	sorted := make([]string, len(ss))
	copy(sorted, ss)
	sort.Strings(sorted)
	return "[" + strings.Join(sorted, " ") + "]"
}

// TestCheckDMWrappingRule verifies the DM rule checker with synthetic inputs.
func TestCheckDMWrappingRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{name: "bare group command fails", text: "Use /linkgroup to continue.", wantHit: true},
		{name: "wrapped dm command fails", text: "Use <code>/start</code> to begin.", wantHit: true},
		{name: "correctly formatted passes", text: "Use /start or <code>/linkgroup</code>."},
		{name: "no commands passes", text: "Nothing relevant here."},
		{name: "bare dm command passes", text: "Open /start in the bot."},
		{name: "wrapped group command passes", text: "Run <code>/linkgroup</code>."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := len(checkDMWrappingRule(tc.text)) > 0; got != tc.wantHit {
				t.Errorf("checkDMWrappingRule(%q) violation=%v, want %v", tc.text, got, tc.wantHit)
			}
		})
	}
}

// TestDMSlashCommandsCoversPrivateOnlyHandlers keeps the command set declared
// in i18n_authoring.go in sync with the privateOnly HandleMessage
// registrations in bot.go. Any command gated on privateOnly that isn't
// declared as a DM command would slip past the i18n wrapping rule, so we
// parse bot.go's AST and assert every such registration is represented in
// dmSlashCommands.
func TestDMSlashCommandsCoversPrivateOnlyHandlers(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bot.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse bot.go: %v", err)
	}

	registered := collectPrivateOnlyCommands(file)
	if len(registered) == 0 {
		t.Fatalf("expected to find privateOnly command registrations in bot.go, found none (did the handler wiring change?)")
	}

	for _, cmd := range registered {
		if _, ok := dmSlashCommands[cmd]; !ok {
			t.Errorf("bot.go registers %q as privateOnly but it is missing from dmSlashCommands in i18n_authoring.go", cmd)
		}
	}
}

// collectPrivateOnlyCommands walks file looking for calls shaped like
// tghandler.And(tghandler.CommandEqual("X"), ..., privateOnly) and returns
// each "/X" command found. It intentionally ignores argument order so the
// test stays robust to minor refactors of the handler registration.
func collectPrivateOnlyCommands(file *ast.File) []string {
	seen := map[string]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "And" {
			return true
		}
		if !callArgsContainIdent(call.Args, "privateOnly") {
			return true
		}
		for _, cmd := range commandEqualLiteralsIn(call.Args) {
			seen["/"+cmd] = struct{}{}
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for cmd := range seen {
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}

func callArgsContainIdent(args []ast.Expr, name string) bool {
	for _, arg := range args {
		if ident, ok := arg.(*ast.Ident); ok && ident.Name == name {
			return true
		}
	}
	return false
}

func commandEqualLiteralsIn(args []ast.Expr) []string {
	var out []string
	for _, arg := range args {
		inner, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "CommandEqual" || len(inner.Args) != 1 {
			continue
		}
		lit, ok := inner.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		cmd, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// TestCheckGroupWrappingRule verifies the group rule checker with synthetic
// inputs.
func TestCheckGroupWrappingRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{name: "bare dm command fails", text: "Open /start in the bot.", wantHit: true},
		{name: "bare group command fails", text: "Run /linkgroup again.", wantHit: true},
		{name: "correctly formatted passes", text: "Run <code>/start</code> after <code>/linkgroup</code>."},
		{name: "no commands passes", text: "Nothing relevant here."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := len(checkGroupWrappingRule(tc.text)) > 0; got != tc.wantHit {
				t.Errorf("checkGroupWrappingRule(%q) violation=%v, want %v", tc.text, got, tc.wantHit)
			}
		})
	}
}
