#!/usr/bin/env python3

from __future__ import annotations

import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LOCALES = [
    ROOT / "internal/platform/i18n/locale/en.toml",
    ROOT / "internal/platform/i18n/locale/it.toml",
]


def humanize(key: str) -> str:
    return key.replace("_", " ")


def description_for(key: str) -> str:
    if key.startswith("btn_"):
        return f"Button label for {humanize(key[4:])}."
    if key.startswith("cb_"):
        return f"Callback acknowledgement text for {humanize(key[3:])}."
    if key.startswith("cmd_"):
        return f"Help or informational text for {humanize(key[4:])}."
    if key.startswith("err_"):
        return f"Error message for {humanize(key[4:])}."
    if key.startswith("web_oauth_"):
        return f"OAuth web flow text for {humanize(key[10:])}."
    if key.startswith("creator_"):
        return f"Creator flow text for {humanize(key[8:])}."
    if key.startswith("group_"):
        return f"Group setup or group management text for {humanize(key[6:])}."
    if key.startswith("linked_status_"):
        return f"Viewer linked-status text for {humanize(key[14:])}."
    if key.startswith("reset_"):
        return f"Reset flow text for {humanize(key[6:])}."
    if key.startswith("sub_"):
        return f"Subscription lifecycle text for {humanize(key[4:])}."
    if key.startswith("consent_"):
        return f"Consent flow text for {humanize(key[8:])}."
    if key.startswith("export_"):
        return f"Data export flow text for {humanize(key[7:])}."
    if key.startswith("viewer_"):
        return f"Viewer flow text for {humanize(key[7:])}."
    if key.startswith("link_"):
        return f"Linking flow text for {humanize(key[5:])}."
    if key.startswith("cleanup_"):
        return f"Cleanup or follow-up text for {humanize(key[8:])}."
    if key.startswith("user_"):
        return f"Generic user-facing text for {humanize(key[5:])}."
    return f"Localized text for {humanize(key)}."


HEADER_RE = re.compile(r"^\[([^\]]+)\]\s*$")


def process(path: Path) -> None:
    lines = path.read_text(encoding="utf-8").splitlines()
    out: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        out.append(line)
        match = HEADER_RE.match(line.strip())
        if not match:
            i += 1
            continue

        key = match.group(1)
        j = i + 1
        has_description = False
        while j < len(lines):
            stripped = lines[j].strip()
            if HEADER_RE.match(stripped):
                break
            if stripped.startswith("description ="):
                has_description = True
                break
            if stripped.startswith("other =") or stripped.startswith("zero =") or stripped.startswith("one =") or stripped.startswith("two =") or stripped.startswith("few =") or stripped.startswith("many ="):
                break
            j += 1

        if not has_description:
            out.append(f'description = "{description_for(key)}"')
        i += 1

    path.write_text("\n".join(out) + "\n", encoding="utf-8")


def main() -> int:
    for path in LOCALES:
        process(path)
    print("descriptions updated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
