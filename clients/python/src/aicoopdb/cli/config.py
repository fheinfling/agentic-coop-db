"""Local user config for the CLI.

The CLI persists a tiny TOML file at ~/.ai-coop-db/config.toml so that
subsequent commands don't need --url / --api-key flags. The wizard
(`ai-coop-db init`) writes this file; `ai-coop-db me` and friends read it.
"""

from __future__ import annotations

import contextlib
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import platformdirs

CONFIG_FILENAME = "config.toml"


def config_dir() -> Path:
    return Path(platformdirs.user_config_dir("aicoopdb", appauthor=False, ensure_exists=True))


def config_path() -> Path:
    return config_dir() / CONFIG_FILENAME


@dataclass
class CLIConfig:
    base_url: str
    api_key: str

    def write(self) -> Path:
        path = config_path()
        path.write_text(
            f'base_url = "{self.base_url}"\napi_key  = "{self.api_key}"\n',
            encoding="utf-8",
        )
        # Best-effort chmod — on Windows this raises and we don't care.
        with contextlib.suppress(OSError):
            os.chmod(path, 0o600)
        return path


def load() -> CLIConfig | None:
    path = config_path()
    if not path.exists():
        return None
    text = path.read_text(encoding="utf-8")
    data: dict[str, Any] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            continue
        k, v = line.split("=", 1)
        k = k.strip()
        v = v.strip().strip('"')
        data[k] = v
    if "base_url" not in data or "api_key" not in data:
        return None
    return CLIConfig(base_url=data["base_url"], api_key=data["api_key"])
