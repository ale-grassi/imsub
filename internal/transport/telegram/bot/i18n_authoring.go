package bot

// i18n authoring rule: DM-vs-group slash-command wrapping.
//
// Telegram auto-linkifies bare /commands in HTML-parsed messages so users can
// tap them to invoke. Commands wrapped in <code>…</code> are NOT auto-linked;
// they render as literal, tap-to-copy text. Whether a command should be
// auto-linked depends on which chat the message is sent in and which chat
// the command is supposed to be invoked from.
//
// Rule:
//
//  1. DM (private) messages: DM-scope commands appear BARE so the user can tap
//     to invoke; group-scope commands MUST be wrapped in <code>…</code> because
//     tapping them in a DM tries to invoke them in the DM where they don't
//     work.
//  2. Group messages: ALL slash-commands MUST be wrapped in <code>…</code>.
//     Group messages often describe what the admin should do (in DM or in the
//     group) — wrapping prevents accidental taps and makes the instruction
//     render as literal text.
//
// This convention is enforced at test time by TestI18NCommandWrappingRule in
// i18n_command_rule_test.go, which reads every catalog, classifies keys into
// DM vs group via i18nPrivateKeys/i18nGroupKeys, and checks each <code> span
// against the command sets below.
//
// When you add a new slash-command to the bot:
//   - Add it to dmSlashCommands or groupSlashCommands here.
//   - Update i18nPrivateKeys/i18nGroupKeys for any new message keys that
//     reference it.
//
// When you add a new i18n message that references any slash-command:
//   - Classify its key in i18nPrivateKeys (DM-only) or i18nGroupKeys
//     (group-only) in i18n_command_rule_test.go.
//   - Wrap slash-commands per the rule above; the test will tell you when
//     you didn't.

// dmSlashCommands lists the slash-commands that are handled only in private
// chats (see handler registrations in bot.go). These are safe to leave bare
// in DM messages so Telegram auto-links them.
var dmSlashCommands = map[string]struct{}{
	"/start":   {},
	"/creator": {},
	"/reset":   {},
	"/info":    {},
	"/help":    {},
}

// groupSlashCommands lists the slash-commands that are handled only in group
// chats. These MUST be wrapped in <code>…</code> everywhere so that tapping
// them in a DM does not invoke them in the wrong chat.
var groupSlashCommands = map[string]struct{}{
	"/linkgroup":   {},
	"/unlinkgroup": {},
}
