"""`agentic-coop-db init` — interactive onboarding wizard.

Goal: get a brand-new user from zero to a working CRUD round trip in
under five minutes without reading any docs beyond the README.

Decision tree:

  1. Detect docker. If missing, link to install instructions.
  2. Ask which profile they want: local / pi-lite / cloud / "I have a server".
  3. For self-hosted profiles: run `make up-<profile>`, wait for /readyz,
     mint an admin API key via scripts/gen-key.sh, capture it once.
  4. For "existing server": prompt for URL + key, call /v1/me to validate.
  5. Write ~/.agentic-coop-db/config.toml.
  6. Run `agentic-coop-db doctor` and print a Python + curl snippet.
"""

from __future__ import annotations

import shutil
import subprocess
import sys
import time
from pathlib import Path

import typer

from agentcoopdb import connect
from agentcoopdb.cli import config as cli_config
from agentcoopdb.errors import AgentCoopDBError


def init() -> None:
    """Run the wizard."""
    typer.echo(_BANNER)

    if not _have("docker"):
        typer.echo(
            "Docker is not on PATH. Install it from https://docs.docker.com/engine/install/ "
            "and re-run `agentic-coop-db init`."
        )
        raise typer.Exit(code=2)

    profile = typer.prompt(
        "Which profile? [local / pi-lite / cloud / existing]",
        default="local",
    ).strip().lower()

    if profile == "existing":
        base_url = typer.prompt("Server base URL (e.g. https://db.example.com)").strip()
        api_key = typer.prompt("API key (will not be echoed)", hide_input=True).strip()
        _validate_and_save(base_url, api_key)
        return

    repo_root = _find_repo_root()
    if repo_root is None:
        typer.echo(
            "Could not find an Agentic Coop DB checkout in the current or parent directories. "
            "Either run this from inside the repo, or pick `existing` and point at a remote server."
        )
        raise typer.Exit(code=2)

    target = {
        "local": "up-local",
        "pi-lite": "up-pi",
        "cloud": "up-cloud",
    }.get(profile)
    if target is None:
        typer.echo(f"unknown profile: {profile}")
        raise typer.Exit(code=2)

    typer.echo(f"\n→ make {target}")
    res = subprocess.run(["make", target], cwd=repo_root)
    if res.returncode != 0:
        typer.echo("`make` failed — fix the error above and re-run `agentic-coop-db init`")
        raise typer.Exit(code=res.returncode)

    base_url = "http://localhost:8080"
    typer.echo(f"\n→ waiting for {base_url}/readyz ...")
    if not _wait_ready(base_url, timeout=120):
        typer.echo("the server did not become ready within 120s — check `make logs`")
        raise typer.Exit(code=1)

    typer.echo("→ minting an admin API key via scripts/gen-key.sh ...")
    key_proc = subprocess.run(
        ["./scripts/gen-key.sh", "default", "dbadmin"],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    if key_proc.returncode != 0:
        typer.echo(key_proc.stderr)
        raise typer.Exit(code=key_proc.returncode)
    api_key = _extract_token(key_proc.stdout)
    if not api_key:
        typer.echo("could not parse the minted key from gen-key.sh output")
        typer.echo(key_proc.stdout)
        raise typer.Exit(code=1)

    _validate_and_save(base_url, api_key)


# ---- helpers -----------------------------------------------------------------

def _validate_and_save(base_url: str, api_key: str) -> None:
    db = connect(base_url, api_key=api_key, timeout=10.0)
    try:
        info = db.me()
    except AgentCoopDBError as e:
        typer.echo(f"could not authenticate against {base_url}: {e}")
        raise typer.Exit(code=1) from e

    cfg = cli_config.CLIConfig(base_url=base_url, api_key=api_key)
    path = cfg.write()
    typer.echo(f"\nconfig saved to {path}")
    typer.echo(f"workspace: {info.get('workspace_id', '?')}")
    typer.echo(f"role:      {info.get('role', '?')}")
    typer.echo(f"server:    {info.get('server', {}).get('version', '?')}")
    typer.echo("\nyou are ready. try this:\n")
    typer.echo(f"    curl -H 'Authorization: Bearer {api_key}' {base_url}/v1/me")
    typer.echo("    agentic-coop-db sql 'SELECT 1'")
    typer.echo("\nor in Python:\n")
    typer.echo("    from agentcoopdb import connect")
    typer.echo(f"    db = connect('{base_url}', api_key='acd_...')")
    typer.echo("    db.execute('SELECT 1')")


def _have(cmd: str) -> bool:
    return shutil.which(cmd) is not None


def _find_repo_root() -> Path | None:
    here = Path.cwd().resolve()
    for candidate in [here, *here.parents]:
        if (candidate / "Makefile").exists() and (candidate / "deploy").is_dir():
            return candidate
    return None


def _wait_ready(base_url: str, timeout: int) -> bool:
    import requests

    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            r = requests.get(base_url + "/readyz", timeout=2.0)
            if r.status_code == 200:
                return True
        except requests.RequestException:
            pass
        time.sleep(1)
    return False


def _extract_token(stdout: str) -> str | None:
    for line in stdout.splitlines():
        line = line.strip()
        if line.startswith("acd_"):
            return line
    return None


_BANNER = """\
agentic-coop-db init — interactive onboarding wizard

This wizard will:
  1. Verify Docker is up
  2. Start the stack with your chosen profile
  3. Mint an admin API key
  4. Save your config (run `agentic-coop-db doctor` to see the path)
  5. Print a Python + curl snippet you can copy/paste

Press Ctrl-C at any time to abort.
"""

if __name__ == "__main__":
    init()
    sys.exit(0)
