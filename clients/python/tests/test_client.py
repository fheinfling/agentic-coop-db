"""Unit tests for AgentCoopDBClient using a mock requests session."""

from __future__ import annotations

import json
from typing import Any
from unittest.mock import MagicMock

import pytest

from agentcoopdb.client import AgentCoopDBClient, _format_vector, _renumber
from agentcoopdb.errors import (
    AuthError,
    IdempotencyConflict,
    NetworkError,
    RateLimited,
    ServerError,
    ValidationError,
)


def make_client() -> tuple[AgentCoopDBClient, MagicMock]:
    c = AgentCoopDBClient("http://localhost:8080", api_key="acd_dev_x_y", timeout=1.0, verify_tls=True)
    session = MagicMock()
    c._session = session
    return c, session


def fake_response(status: int, body: Any) -> MagicMock:
    r = MagicMock()
    r.status_code = status
    r.json.return_value = body
    r.text = json.dumps(body)
    r.content = json.dumps(body).encode("utf-8")
    r.headers = {}
    return r


def test_execute_returns_typed_result() -> None:
    c, s = make_client()
    s.post.return_value = fake_response(
        200,
        {"command": "SELECT", "columns": ["id"], "rows": [[1]], "rows_affected": 1, "duration_ms": 3},
    )
    res = c.execute("SELECT id FROM notes WHERE id = $1", [1])
    assert res.command == "SELECT"
    assert res.columns == ["id"]
    assert res.rows == [[1]]
    assert res.rows_affected == 1


def test_select_asserts_command_tag() -> None:
    c, s = make_client()
    s.post.return_value = fake_response(
        200,
        {"command": "INSERT", "columns": [], "rows": [], "rows_affected": 1, "duration_ms": 1},
    )
    with pytest.raises(ValidationError):
        c.select("INSERT INTO notes(id) VALUES ($1)", [1])


@pytest.mark.parametrize(
    "status,title,exc",
    [
        (401, "invalid_api_key", AuthError),
        (403, "permission_denied", AuthError),
        (400, "params_mismatch", ValidationError),
        (409, "idempotency_conflict", IdempotencyConflict),
        (429, "rate_limited", RateLimited),
        (500, "internal", ServerError),
    ],
)
def test_error_taxonomy(status: int, title: str, exc: type[BaseException]) -> None:
    c, s = make_client()
    s.post.return_value = fake_response(status, {"title": title, "status": status, "detail": "x"})
    with pytest.raises(exc):
        c.execute("SELECT 1")


def test_network_error_is_retried_then_raised() -> None:
    import requests

    c, s = make_client()
    s.post.side_effect = requests.ConnectionError("boom")
    with pytest.raises(NetworkError):
        c.execute("SELECT 1")


def test_renumber_renumbers_placeholders() -> None:
    out, idx = _renumber("SELECT $1, $2, $1", 0)
    assert out == "SELECT $1, $2, $1"
    assert idx == 2

    out2, idx2 = _renumber("UPDATE t SET v = $1 WHERE id = $2", idx)
    assert out2 == "UPDATE t SET v = $3 WHERE id = $4"
    assert idx2 == 4


def test_format_vector_emits_pgvector_literal() -> None:
    assert _format_vector([1.0, 2.5, 3.0]) == "[1.0,2.5,3.0]"
