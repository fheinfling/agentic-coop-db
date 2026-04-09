"""`agentic-coop-db key` — manage API keys."""

from __future__ import annotations

import json

import typer

from agentcoopdb import connect
from agentcoopdb.cli.config import CLIConfig
from agentcoopdb.cli.config import load as load_config

key_app = typer.Typer(no_args_is_help=True)


@key_app.command("create")
def create(
    pg_role: str = typer.Option(..., "--role", help="Postgres role to attach (e.g. dbuser, dbadmin, custom_role)"),
    workspace: str = typer.Option("", "--workspace", help="Override the workspace id (defaults to caller's)"),
    name: str = typer.Option("", "--name", help="Human label for the key"),
) -> None:
    """Mint a new API key. The caller must be a dbadmin key."""
    cfg = _cfg()
    db = connect(cfg.base_url, api_key=cfg.api_key)
    body = {"pg_role": pg_role, "name": name}
    if workspace:
        body["workspace_id"] = workspace
    res = db._post("/v1/auth/keys", body)  # noqa: SLF001 — intentional internal use
    typer.echo(json.dumps(res, indent=2))
    typer.echo("\nstore the token above NOW — it is shown exactly once")


@key_app.command("rotate")
def rotate() -> None:
    """Rotate the calling key. The old key remains active for the configured overlap window."""
    cfg = _cfg()
    db = connect(cfg.base_url, api_key=cfg.api_key)
    res = db.rotate_key()
    typer.echo(json.dumps(res, indent=2))
    typer.echo("\nstore the token above NOW — it is shown exactly once")


def _cfg() -> CLIConfig:
    cfg = load_config()
    if cfg is None:
        typer.echo("no config found — run `agentic-coop-db init` first", err=True)
        raise typer.Exit(code=2)
    return cfg
