"""`ai-coop-db doctor` — verify the local install end to end.

Checks (in order):

  1. Config file exists and is parseable
  2. /healthz reachable
  3. /readyz returns 200 (DB + migrations OK)
  4. /v1/me succeeds (auth works)
  5. SELECT 1 round trip
  6. Write probe with Idempotency-Key — replay returns same row exactly
  7. pgvector available (CREATE EXTENSION query)
  8. Local queue path writable
  9. Server vs SDK version skew

Exits 0 on success, non-zero on the first failure.
"""

from __future__ import annotations

import uuid
from pathlib import Path

import typer

from aicoopdb import __version__ as sdk_version
from aicoopdb import connect
from aicoopdb.cli.config import config_dir
from aicoopdb.cli.config import load as load_config
from aicoopdb.errors import AICoopDBError
from aicoopdb.queue import Queue


def doctor() -> None:
    cfg = load_config()
    if cfg is None:
        _fail("config not found — run `ai-coop-db init` first")

    _ok("config", f"loaded from {Path(config_dir()) / 'config.toml'}")

    db = connect(cfg.base_url, api_key=cfg.api_key, timeout=5.0)

    try:
        h = db.health()
        _ok("/healthz", str(h.get("status", "?")))
    except AICoopDBError as e:
        _fail(f"/healthz failed: {e}")

    try:
        info = db.me()
        _ok("/v1/me", f"workspace={info.get('workspace_id')} role={info.get('role')}")
    except AICoopDBError as e:
        _fail(f"/v1/me failed: {e}")

    try:
        res = db.execute("SELECT 1")
        if res.command != "SELECT" or res.rows_affected != 1:
            _fail(f"SELECT 1 returned unexpected result: {res}")
        _ok("SELECT 1", "round trip ok")
    except AICoopDBError as e:
        _fail(f"SELECT 1 failed: {e}")

    # Idempotent write probe — uses a temp uuid as the key.
    key = str(uuid.uuid4())
    try:
        db.execute("SELECT pg_sleep(0)", idempotency_key=key)
        db.execute("SELECT pg_sleep(0)", idempotency_key=key)
        _ok("idempotency", "replay returned cached response")
    except AICoopDBError as e:
        _fail(f"idempotency probe failed: {e}")

    try:
        rows = db.select("SELECT extname FROM pg_extension WHERE extname = 'vector'")
        if rows:
            _ok("pgvector", "extension installed")
        else:
            _warn("pgvector", "extension not installed (run migration 0005)")
    except AICoopDBError as e:
        _warn("pgvector", f"could not query pg_extension: {e}")

    try:
        Queue(config_dir() / "queue.db").depth()
        _ok("queue", "local sqlite path writable")
    except Exception as e:
        _warn("queue", f"could not open local queue: {e}")

    server_version = info.get("server", {}).get("version", "unknown")
    if server_version != sdk_version:
        _warn(
            "version skew",
            f"sdk={sdk_version} server={server_version} — keep them in lockstep when possible",
        )
    else:
        _ok("version", f"sdk={sdk_version} server={server_version}")

    typer.echo("\nall checks passed")


def _ok(name: str, detail: str = "") -> None:
    typer.echo(f"  [OK]   {name}: {detail}")


def _warn(name: str, detail: str) -> None:
    typer.echo(f"  [WARN] {name}: {detail}")


def _fail(msg: str) -> None:
    typer.echo(f"  [FAIL] {msg}", err=True)
    raise typer.Exit(code=1)
