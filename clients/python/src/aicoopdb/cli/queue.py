"""`ai-coop-db queue` — inspect / flush the local offline retry queue."""

from __future__ import annotations

from pathlib import Path

import typer

from aicoopdb import connect
from aicoopdb.cli.config import config_dir
from aicoopdb.cli.config import load as load_config
from aicoopdb.queue import Queue, QueueItem

queue_app = typer.Typer(no_args_is_help=True)


def queue_path() -> Path:
    return config_dir() / "queue.db"


@queue_app.command("status")
def status() -> None:
    """Show the current depth + dead-letter count."""
    q = Queue(queue_path())
    typer.echo(f"pending:     {q.depth()}")
    typer.echo(f"dead-letter: {q.dead_letter_count()}")
    typer.echo(f"db:          {q.path}")


@queue_app.command("flush")
def flush() -> None:
    """Drain the queue against the configured server."""
    cfg = load_config()
    if cfg is None:
        typer.echo("no config found — run `ai-coop-db init` first", err=True)
        raise typer.Exit(code=2)
    db = connect(cfg.base_url, api_key=cfg.api_key)
    q = Queue(queue_path())

    def send(item: QueueItem) -> None:
        db.execute(item.sql, item.params, idempotency_key=item.idempotency_key)

    sent, failed = q.flush(send)
    typer.echo(f"sent={sent} failed={failed} remaining={q.depth()}")


@queue_app.command("clear-dead")
def clear_dead() -> None:
    """Truncate the dead-letter table after manual review."""
    import sqlite3

    path = queue_path()
    if not path.exists():
        typer.echo("no queue db")
        return
    with sqlite3.connect(path) as c:
        c.execute("DELETE FROM dead")
    typer.echo("dead letters cleared")
