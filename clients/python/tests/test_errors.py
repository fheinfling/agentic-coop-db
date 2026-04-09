"""Unit tests for the agentcoopdb error hierarchy."""

from __future__ import annotations

import pytest

from agentcoopdb.errors import (
    AgentCoopDBError,
    AuthError,
    IdempotencyConflict,
    NetworkError,
    QueueFullError,
    RateLimited,
    ServerError,
    ValidationError,
)


def test_root_attributes() -> None:
    err = AgentCoopDBError("something failed", status=500, problem={"title": "internal"})
    assert str(err) == "something failed"
    assert err.status == 500
    assert err.problem == {"title": "internal"}


def test_title_from_problem() -> None:
    err = AgentCoopDBError("x", problem={"title": "unique_violation"})
    assert err.title == "unique_violation"


def test_title_fallback_to_class_name() -> None:
    err = ValidationError("bad sql")
    assert err.title == "ValidationError"


def test_sqlstate() -> None:
    err = AgentCoopDBError("x", problem={"sqlstate": "23505"})
    assert err.sqlstate == "23505"


def test_sqlstate_missing() -> None:
    err = AgentCoopDBError("x")
    assert err.sqlstate == ""


def test_rate_limited_retry_after() -> None:
    err = RateLimited("slow down", retry_after=7.5, status=429)
    assert err.retry_after == 7.5
    assert err.status == 429


def test_rate_limited_default_retry_after() -> None:
    err = RateLimited("slow down")
    assert err.retry_after == 1.0


def test_inheritance() -> None:
    subclasses = [
        AuthError("a"),
        ValidationError("v"),
        IdempotencyConflict("i"),
        RateLimited("r"),
        ServerError("s"),
        NetworkError("n"),
        QueueFullError("q"),
    ]
    for e in subclasses:
        assert isinstance(e, AgentCoopDBError), f"{type(e).__name__} is not an AgentCoopDBError"


def test_catch_by_base() -> None:
    with pytest.raises(AgentCoopDBError):
        raise ServerError("boom", status=500)


def test_network_error_no_status() -> None:
    err = NetworkError("connection refused")
    assert err.status is None
