"""HTTP client for the AIColDB gateway.

This module is the only place that talks to the network. It is intentionally
small: the gateway endpoint is `{sql, params}` so the wrapper does almost
nothing beyond JSON encode/decode and error mapping.
"""

from __future__ import annotations

import json
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Any, Iterator, Sequence

import requests

from aicoldb.errors import (
    AIColDBError,
    AuthError,
    IdempotencyConflict,
    NetworkError,
    RateLimited,
    ServerError,
    ValidationError,
)
from aicoldb.retry import with_retry


@dataclass
class ExecuteResult:
    """Server response for POST /v1/sql/execute."""

    command: str
    columns: list[str]
    rows: list[list[Any]]
    rows_affected: int
    duration_ms: int


def connect(base_url: str, *, api_key: str, timeout: float = 30.0, verify_tls: bool = True) -> "AIColDBClient":
    """Create a client bound to base_url + api_key.

    >>> db = connect("https://db.example.com", api_key="aic_live_...")
    """
    return AIColDBClient(base_url=base_url, api_key=api_key, timeout=timeout, verify_tls=verify_tls)


class AIColDBClient:
    """The thin client that wraps every server endpoint."""

    def __init__(self, base_url: str, api_key: str, timeout: float, verify_tls: bool) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.verify_tls = verify_tls
        self._session = requests.Session()
        self._session.headers.update(
            {
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
                "User-Agent": "aicoldb-python/0.1.0",
            }
        )

    # ---- public surface --------------------------------------------------

    def execute(
        self,
        sql: str,
        params: Sequence[Any] | None = None,
        *,
        idempotency_key: str | None = None,
    ) -> ExecuteResult:
        """Forward a SQL statement to the gateway and return the result.

        Use $N placeholders and pass user values in `params`. Never build
        SQL strings with f-strings or `+`; the gateway counts placeholders
        and rejects mismatches at parse time.
        """
        body = {"sql": sql, "params": list(params or [])}
        headers: dict[str, str] = {}
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        data = self._post("/v1/sql/execute", body, headers)
        return ExecuteResult(
            command=str(data.get("command", "")),
            columns=list(data.get("columns") or []),
            rows=list(data.get("rows") or []),
            rows_affected=int(data.get("rows_affected", 0)),
            duration_ms=int(data.get("duration_ms", 0)),
        )

    def select(self, sql: str, params: Sequence[Any] | None = None) -> list[dict[str, Any]]:
        """Convenience: run a SELECT and return rows as a list of dicts.

        Asserts that the executed command tag is SELECT — protects against
        accidentally calling .select on a write.
        """
        result = self.execute(sql, params)
        if result.command != "SELECT":
            raise ValidationError(
                f"select() expects a SELECT statement, got {result.command}",
                status=400,
            )
        return [dict(zip(result.columns, row, strict=False)) for row in result.rows]

    @contextmanager
    def transaction(self) -> Iterator["TransactionBuilder"]:
        """Build a single-statement CTE-wrapped transaction.

        For multi-statement transactions, register an RPC and call it via
        rpc_call() — the server runs each RPC inside its own transaction.
        """
        builder = TransactionBuilder()
        yield builder
        builder.commit(self)

    def rpc_call(self, name: str, args: dict[str, Any], *, idempotency_key: str | None = None) -> dict[str, Any]:
        """Call a registered RPC by name."""
        headers: dict[str, str] = {}
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        return self._post("/v1/rpc/call", {"procedure": name, "args": args}, headers)

    def vector_upsert(self, table: str, rows: list[dict[str, Any]]) -> int:
        """Insert or update embedding rows in `table`.

        Each row must include `id`, `metadata`, and `vector` (list[float]).
        """
        # We compose a parameterised INSERT ... ON CONFLICT and let the
        # server enforce auth and RLS.
        if not rows:
            return 0
        cols = ("id", "metadata", "embedding")
        values_clauses = []
        params: list[Any] = []
        for i, row in enumerate(rows):
            base = i * 3
            values_clauses.append(f"(${base + 1}, ${base + 2}::jsonb, ${base + 3}::vector)")
            params.extend([row["id"], json.dumps(row.get("metadata", {})), _format_vector(row["vector"])])
        sql = (
            f"INSERT INTO {_safe_ident(table)} ({', '.join(cols)}) VALUES "
            + ", ".join(values_clauses)
            + " ON CONFLICT (id) DO UPDATE SET metadata = EXCLUDED.metadata, embedding = EXCLUDED.embedding"
        )
        result = self.execute(sql, params)
        return result.rows_affected

    def vector_search(self, table: str, query: list[float], *, k: int = 5) -> list[dict[str, Any]]:
        """Top-k cosine similarity search."""
        sql = (
            f"SELECT id, metadata, embedding <=> $1::vector AS distance "
            f"FROM {_safe_ident(table)} ORDER BY embedding <=> $1::vector LIMIT $2"
        )
        return self.select(sql, [_format_vector(query), k])

    def rotate_key(self) -> dict[str, Any]:
        """Rotate the calling key. Returns {new_key_id, token, notice}."""
        return self._post("/v1/auth/keys/rotate", {})

    def health(self) -> dict[str, Any]:
        """GET /healthz."""
        return self._get("/healthz")

    def me(self) -> dict[str, Any]:
        """GET /v1/me — { workspace, role, server }."""
        return self._get("/v1/me")

    # ---- internals -------------------------------------------------------

    def _get(self, path: str) -> dict[str, Any]:
        def fn() -> dict[str, Any]:
            try:
                r = self._session.get(self.base_url + path, timeout=self.timeout, verify=self.verify_tls)
            except requests.RequestException as e:
                raise NetworkError(str(e)) from e
            return self._handle(r)

        return with_retry(fn)

    def _post(self, path: str, body: dict[str, Any], headers: dict[str, str] | None = None) -> dict[str, Any]:
        def fn() -> dict[str, Any]:
            try:
                r = self._session.post(
                    self.base_url + path,
                    data=json.dumps(body).encode("utf-8"),
                    headers=headers or {},
                    timeout=self.timeout,
                    verify=self.verify_tls,
                )
            except requests.RequestException as e:
                raise NetworkError(str(e)) from e
            return self._handle(r)

        return with_retry(fn)

    @staticmethod
    def _handle(r: requests.Response) -> dict[str, Any]:
        if r.status_code < 300:
            if not r.content:
                return {}
            try:
                data = r.json()
                return data if isinstance(data, dict) else {"data": data}
            except json.JSONDecodeError as e:
                raise ServerError(f"invalid JSON in response: {e}") from e

        problem: dict[str, Any] = {}
        try:
            problem = r.json()
        except (ValueError, json.JSONDecodeError):
            problem = {"title": "non_json_error", "detail": r.text}
        title = str(problem.get("title", ""))
        detail = str(problem.get("detail", title))

        if r.status_code in (401, 403):
            raise AuthError(detail, status=r.status_code, problem=problem)
        if r.status_code == 400:
            raise ValidationError(detail, status=r.status_code, problem=problem)
        if r.status_code == 409 and title == "idempotency_conflict":
            raise IdempotencyConflict(detail, status=r.status_code, problem=problem)
        if r.status_code == 429:
            retry_after = float(r.headers.get("Retry-After", "1") or 1.0)
            raise RateLimited(detail, status=r.status_code, problem=problem, retry_after=retry_after)
        if r.status_code >= 500:
            raise ServerError(detail, status=r.status_code, problem=problem)
        raise AIColDBError(detail, status=r.status_code, problem=problem)


class TransactionBuilder:
    """Collects a single CTE-wrapped multi-write call."""

    def __init__(self) -> None:
        self._statements: list[tuple[str, list[Any]]] = []

    def execute(self, sql: str, params: Sequence[Any] | None = None) -> None:
        self._statements.append((sql, list(params or [])))

    def commit(self, client: AIColDBClient) -> None:
        if not self._statements:
            return
        if len(self._statements) == 1:
            sql, params = self._statements[0]
            client.execute(sql, params)
            return
        # Multi-statement: build a WITH chain. The last statement is the one
        # whose result is returned.
        ctes = []
        params_all: list[Any] = []
        idx = 0
        for i, (sql, params) in enumerate(self._statements[:-1]):
            renumbered, idx = _renumber(sql, idx)
            ctes.append(f"_aicoldb_w{i} AS ({renumbered})")
            params_all.extend(params)
        last_sql, last_params = self._statements[-1]
        renumbered_last, _ = _renumber(last_sql, idx)
        params_all.extend(last_params)
        full = "WITH " + ", ".join(ctes) + " " + renumbered_last
        client.execute(full, params_all)


def _renumber(sql: str, start: int) -> tuple[str, int]:
    """Renumber $N placeholders so they don't collide across CTEs."""
    out = []
    i = 0
    next_idx = start
    placeholders: dict[int, int] = {}
    while i < len(sql):
        c = sql[i]
        if c == "$" and i + 1 < len(sql) and sql[i + 1].isdigit():
            j = i + 1
            while j < len(sql) and sql[j].isdigit():
                j += 1
            n = int(sql[i + 1 : j])
            if n not in placeholders:
                next_idx += 1
                placeholders[n] = next_idx
            out.append(f"${placeholders[n]}")
            i = j
            continue
        out.append(c)
        i += 1
    return "".join(out), next_idx


def _format_vector(v: Sequence[float]) -> str:
    return "[" + ",".join(repr(float(x)) for x in v) + "]"


def _safe_ident(s: str) -> str:
    if not s or len(s) > 63 or not all(c.islower() or c.isdigit() or c == "_" for c in s):
        raise ValidationError(f"unsafe identifier: {s!r}")
    return s
