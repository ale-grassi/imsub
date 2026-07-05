#!/usr/bin/env python3

from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LOCALES = [
    ROOT / "internal/platform/i18n/locale/en.toml",
    ROOT / "internal/platform/i18n/locale/it.toml",
]

EXPECTED_COMMENTS = [
    "# Creator auth / onboarding prompts",
    "# Creator status menu",
    "# Managed groups menu",
    "# Group settings menu",
    "# Member tag sync menu",
    "# Group language menu",
    "# Group policy picker menu",
    "# Group policy confirm menu",
    "# Grace period menu",
    "# Unregister group menu",
    "# Creator notices / follow-up messages",
    "# Legacy creator templates kept for compatibility / migration",
]

EXPECTED_COMMENT_PAIRS = [
    ("# Prompt di collegamento / ricollegamento creator", "# Creator auth / onboarding prompts"),
    ("# Menu stato creator", "# Creator status menu"),
    ("# Menu gruppi gestiti", "# Managed groups menu"),
    ("# Menu impostazioni gruppo", "# Group settings menu"),
    ("# Menu sincronizzazione tag membri", "# Member tag sync menu"),
    ("# Menu lingua gruppo", "# Group language menu"),
    ("# Menu scelta policy gruppo", "# Group policy picker menu"),
    ("# Menu conferma policy gruppo", "# Group policy confirm menu"),
    ("# Menu periodo di tolleranza", "# Grace period menu"),
    ("# Menu scollegamento gruppo", "# Unregister group menu"),
    ("# Notice / messaggi di follow-up creator", "# Creator notices / follow-up messages"),
    ("# Template creator legacy mantenuti per compatibilita / migrazione", "# Legacy creator templates kept for compatibility / migration"),
]

EXPECTED_KEYS_IN_ORDER = [
    "creator_register_info",
    "creator_reconnect_info",
    "creator_registered_title",
    "creator_registered_no_group_title",
    "creator_dashboard_setup_status",
    "creator_auth_healthy",
    "creator_auth_reconnect_required",
    "creator_groups_none",
    "creator_last_sync_at",
    "creator_last_sync_disabled",
    "creator_reconnect_since",
    "creator_dashboard_current_data",
    "label_account",
    "creator_grace_enabled",
    "creator_grace_disabled",
    "creator_blocklist_enabled",
    "creator_blocklist_disabled",
    "creator_subscribers_pending",
    "creator_subscribers_ready",
    "creator_subscribers_cached",
    "creator_banned_users_cached",
    "creator_registered_body",
    "creator_registered_no_group_body",
    "creator_section_managed_groups",
    "creator_manage_groups_title",
    "creator_manage_groups_body",
    "creator_group_settings_title",
    "label_group",
    "label_current_policy",
    "label_current_language",
    "label_member_tags",
    "creator_group_member_tags_title",
    "creator_group_member_tags_need_tags_html",
    "label_current_setting",
    "creator_group_member_tags_state",
    "creator_group_member_tags_state_off",
    "creator_group_member_tags_body",
    "creator_group_language_title",
    "creator_group_language_body",
    "creator_group_policy_title",
    "creator_group_policy_body",
    "creator_group_policy_confirm_title",
    "label_new_policy",
    "creator_grace_title",
    "creator_grace_body",
    "creator_unregister_title",
    "creator_unregister_body",
    "creator_reconnect_mismatch",
    "creator_group_unregistered_html",
    "creator_group_policy_updated_html",
    "creator_group_language_updated_html",
    "creator_grace_updated",
    "creator_blocklist_on_notice",
    "creator_blocklist_off_notice",
    "creator_group_member_tags_enable_notice_html",
    "creator_group_member_tags_disable_notice_html",
    "creator_registered_links",
    "creator_manage_groups_html",
    "creator_group_settings_html",
    "creator_group_policy_picker_html",
    "creator_group_policy_confirm_html",
    "creator_group_language_picker_html",
    "creator_group_member_tags_confirm_html",
    "creator_registered_html",
    "creator_registered_no_group_html",
    "creator_unregister_confirm_html",
    "creator_grace_picker_html",
]

DESCRIPTION_EXEMPT_PREFIXES = (
    "btn_",
    "cb_",
    "err_",
)

DESCRIPTION_EXEMPT_KEYS = {
    "cmd_help",
    "cmd_help_both",
    "cmd_help_creator",
    "cmd_help_viewer",
    "user_generic_name",
}


def key_line_map(text: str) -> dict[str, int]:
    out: dict[str, int] = {}
    for idx, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            out[stripped[1:-1]] = idx
    return out


def description_presence_map(text: str) -> dict[str, bool]:
    out: dict[str, bool] = {}
    current_key: str | None = None
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if line.startswith("[") and line.endswith("]"):
            current_key = line[1:-1]
            out.setdefault(current_key, False)
            continue
        if current_key is None:
            continue
        if line.startswith("description ="):
            out[current_key] = True
    return out


def all_keys(text: str) -> list[str]:
    out: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            out.append(stripped[1:-1])
    return out


def verify(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    lines = text.splitlines()
    descriptions = description_presence_map(text)
    errors: list[str] = []

    comment_expectations = EXPECTED_COMMENTS
    if path.name == "it.toml":
        comment_expectations = [it for it, _ in EXPECTED_COMMENT_PAIRS]

    previous_comment_line = 0
    for comment in comment_expectations:
        try:
            line_no = next(i for i, line in enumerate(lines, start=1) if line == comment)
        except StopIteration:
            errors.append(f"{path.name}: missing comment {comment!r}")
            continue
        if line_no <= previous_comment_line:
            errors.append(f"{path.name}: comment out of order {comment!r}")
        previous_comment_line = line_no

    keys = key_line_map(text)
    previous_key_line = 0
    for key in EXPECTED_KEYS_IN_ORDER:
        line_no = keys.get(key)
        if line_no is None:
            errors.append(f"{path.name}: missing key [{key}]")
            continue
        if line_no <= previous_key_line:
            errors.append(f"{path.name}: key out of order [{key}]")
        previous_key_line = line_no

    for key in all_keys(text):
        if key.startswith(DESCRIPTION_EXEMPT_PREFIXES) or key in DESCRIPTION_EXEMPT_KEYS:
            continue
        if not descriptions.get(key, False):
            errors.append(f"{path.name}: missing description for [{key}]")

    return errors


def main() -> int:
    errors: list[str] = []
    for path in LOCALES:
        errors.extend(verify(path))
    if errors:
        for err in errors:
            print(err)
        return 1
    print("i18n locale layout OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
