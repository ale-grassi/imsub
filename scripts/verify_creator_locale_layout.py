#!/usr/bin/env python3

from __future__ import annotations

import runpy
from pathlib import Path


if __name__ == "__main__":
    target = Path(__file__).with_name("verify_i18n_locale_layout.py")
    runpy.run_path(str(target), run_name="__main__")
