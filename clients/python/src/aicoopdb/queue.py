"""SQLite-backed durable retry queue for offline writes.

When the gateway is unreachable, calling .execute() on a write raises
NetworkError unless the caller has wrapped the client with `enable_queue()`.
With the queue enabled, writes are persisted to ~/.ai-coop-db/queue.db and
replayed on the next successful flush.

Reads are NEVER queued — they would silently return stale data. The caller
gets a NetworkError so they can decide their own UX.

The queue is intentionally tiny:
  * one table `pending` (id, sql, params, idempotency_key, attempts, last_error, created_at)
  * one table `dead`    (same shape, written when attempts >= max_attempts)
  * FIFO replay with exponential backoff

The queue is single-process: there is no advisory locking. If you need
multi-process replay, use a single sidecar process to run `ai-coop-db queue flush`.
"""

from __future__ import annotations

import json
import sqlite3
import time
from collections.abc import Iterator, Sequence
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from aicoopdb.errors import AICoopDBError, NetworkError, QueueFullError
from aicoopdb.retry import backoff

_SCHEMA = """
CREATE TABLE IF NOT EXISTS pending (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sql             TEXT NOT NULL,
    params          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at REAL NOT NULL,
    created_at      REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS pending_next_idx ON pending(next_attempt_at);

CREATE TABLE IF NOT EXISTS dead (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sql             TEXT NOT NULL,
    params          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    attempts        INTEGER NOT NULL,
    last_error      TEXT,
    moved_at        REAL NOT NULL
);
"""


@dataclass
class QueueItem:
    id: int
    sql: str
    params: list[Any]
    idempotency_key: str
    attempts: int
    last_error: str | None
    next_attempt_at: float


class Queue:
    """Durable retry queue backed by SQLite.

    Thread-safe within a single process (sqlite3 connection per call).
    """

    MAX_ATTEMPTS = 12
    DEFAULT_MAX_SIZE = 10_000

    def __init__(self, path: Path | str, *, max_size: int = DEFAULT_MAX_SIZE) -> None:
        self.path = Path(path)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.max_size = max_size
        with self._conn() as c:
            c.executescript(_SCHEMA)

    @contextmanager
    def _conn(self) -> Iterator[sqlite3.Connection]:
        c = sqlite3.connect(self.path, isolation_level=None, timeout=10.0)
        try:
            c.execute("PRAGMA journal_mode = WAL")
            c.execute("PRAGMA synchronous = NORMAL")
            yield c
        finally:
            c.close()

    def enqueue(self, sql: str, params: Sequence[Any], idempotency_key: str) -> int:
        with self._conn() as c:
            row = c.execute("SELECT count(*) FROM pending").fetchone()
            if row and row[0] >= self.max_size:
                raise QueueFullError(f"local queue is full ({self.max_size} pending writes)")
            cur = c.execute(
                "INSERT INTO pending (sql, params, idempotency_key, next_attempt_at, created_at) "
                "VALUES (?, ?, ?, ?, ?)",
                (sql, json.dumps(list(params)), idempotency_key, time.time(), time.time()),
            )
            return int(cur.lastrowid or 0)

    def depth(self) -> int:
        with self._conn() as c:
            row = c.execute("SELECT count(*) FROM pending").fetchone()
            return int(row[0] if row else 0)

    def dead_letter_count(self) -> int:
        with self._conn() as c:
            row = c.execute("SELECT count(*) FROM dead").fetchone()
            return int(row[0] if row else 0)

    def next_due(self, limit: int = 32) -> list[QueueItem]:
        now = time.time()
        with self._conn() as c:
            rows = c.execute(
                "SELECT id, sql, params, idempotency_key, attempts, last_error, next_attempt_at "
                "FROM pending WHERE next_attempt_at <= ? ORDER BY id LIMIT ?",
                (now, limit),
            ).fetchall()
        return [
            QueueItem(
                id=int(r[0]),
                sql=str(r[1]),
                params=list(json.loads(r[2])),
                idempotency_key=str(r[3]),
                attempts=int(r[4]),
                last_error=r[5],
                next_attempt_at=float(r[6]),
            )
            for r in rows
        ]

    def ack(self, item_id: int) -> None:
        with self._conn() as c:
            c.execute("DELETE FROM pending WHERE id = ?", (item_id,))

    def fail(self, item: QueueItem, err: BaseException) -> None:
        attempts = item.attempts + 1
        if attempts >= self.MAX_ATTEMPTS:
            with self._conn() as c:
                c.execute(
                    "INSERT INTO dead (sql, params, idempotency_key, attempts, last_error, moved_at) "
                    "VALUES (?, ?, ?, ?, ?, ?)",
                    (item.sql, json.dumps(item.params), item.idempotency_key, attempts, str(err), time.time()),
                )
                c.execute("DELETE FROM pending WHERE id = ?", (item.id,))
            return
        next_at = time.time() + backoff(attempts, base=2.0, cap=300.0)
        with self._conn() as c:
            c.execute(
                "UPDATE pending SET attempts = ?, last_error = ?, next_attempt_at = ? WHERE id = ?",
                (attempts, str(err), next_at, item.id),
            )

    def flush(self, send: Sender) -> tuple[int, int]:
        """Drain the queue using `send`. Returns (sent, failed)."""
        sent, failed = 0, 0
        while True:
            batch = self.next_due(limit=64)
            if not batch:
                return sent, failed
            for item in batch:
                try:
                    send(item)
                    self.ack(item.id)
                    sent += 1
                except NetworkError as e:
                    self.fail(item, e)
                    failed += 1
                    return sent, failed  # back off on the first network failure
                except AICoopDBError as e:
                    # Permanent failure (4xx) — move to dead letter immediately.
                    with self._conn() as c:
                        c.execute(
                            "INSERT INTO dead (sql, params, idempotency_key, attempts, last_error, moved_at) "
                            "VALUES (?, ?, ?, ?, ?, ?)",
                            (
                                item.sql,
                                json.dumps(item.params),
                                item.idempotency_key,
                                item.attempts + 1,
                                str(e),
                                time.time(),
                            ),
                        )
                        c.execute("DELETE FROM pending WHERE id = ?", (item.id,))
                    failed += 1


# Sender is the callable signature flush() expects: takes a QueueItem and
# either succeeds (the item is acked) or raises.
class Sender:
    def __call__(self, item: QueueItem) -> None: ...  # pragma: no cover - protocol
