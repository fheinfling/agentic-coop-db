"""agentcoopdb — top-level CLI.

Subcommands:

    agentic-coop-db init       interactive onboarding wizard (start the stack, mint a key)
    agentic-coop-db sql        run a one-shot SQL statement
    agentic-coop-db me         show the calling key's workspace + role
    agentic-coop-db key        manage API keys (create, rotate)
    agentic-coop-db queue      inspect / flush the local offline retry queue
    agentic-coop-db doctor     verify that everything is wired correctly
"""

from __future__ import annotations

import json
from typing import Any

import typer

from agentcoopdb import connect
from agentcoopdb.cli import config as cli_config
from agentcoopdb.cli.doctor import doctor as doctor_cmd
from agentcoopdb.cli.init import init as init_cmd
from agentcoopdb.cli.key import key_app
from agentcoopdb.cli.queue import queue_app

app = typer.Typer(
    name="agentcoopdb",
    help="Agentic Coop DB CLI — auth gateway client for shared PostgreSQL.",
    no_args_is_help=True,
)

app.add_typer(key_app, name="key", help="Manage API keys.")
app.add_typer(queue_app, name="queue", help="Inspect / flush the local offline retry queue.")
app.command(name="init", help="Interactive onboarding wizard.")(init_cmd)
app.command(name="doctor", help="Verify config, network, auth, db, and migrations.")(doctor_cmd)


@app.command()
def me() -> None:
    """Print { workspace, role, server } for the configured key."""
    cfg = _require_config()
    db = connect(cfg.base_url, api_key=cfg.api_key)
    typer.echo(json.dumps(db.me(), indent=2))


@app.command()
def sql(
    statement: str = typer.Argument(..., help="The SQL statement (use $1, $2, ... for params)"),
    param: list[str] = typer.Option([], "--param", "-p", help="Repeatable; positional values for $1, $2, ..."),
) -> None:
    """Run a one-shot SQL statement against the configured server."""
    cfg = _require_config()
    db = connect(cfg.base_url, api_key=cfg.api_key)
    parsed_params: list[Any] = list(param)
    res = db.execute(statement, parsed_params)
    typer.echo(
        json.dumps(
            {
                "command": res.command,
                "columns": res.columns,
                "rows": res.rows,
                "rows_affected": res.rows_affected,
                "duration_ms": res.duration_ms,
            },
            indent=2,
        )
    )


def _require_config() -> cli_config.CLIConfig:
    cfg = cli_config.load()
    if cfg is None:
        typer.echo("no config found — run `agentic-coop-db init` first", err=True)
        raise typer.Exit(code=2)
    return cfg


if __name__ == "__main__":
    app()
