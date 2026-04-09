"""Typed error hierarchy for the Python client.

Every public client method raises one of these — never a bare requests.HTTPError
or json.JSONDecodeError. The taxonomy is:

    AgentCoopDBError                # root
        AuthError               # 401 / 403 — bad/missing/expired key
        ValidationError         # 400 — server-side SQL or arg validation
        IdempotencyConflict     # 409 — same key, different body
        RateLimited             # 429 — too fast
        ServerError             # 5xx — anything the server itself reports
        NetworkError            # transport-level (timeout, DNS, connect)
        QueueFullError          # local SQLite queue is full

The HTTP status code and the server-supplied RFC7807 problem document are
attached to every Server-side error so the caller can introspect them.
"""

from __future__ import annotations

from typing import Any


class AgentCoopDBError(Exception):
    """Root for every error this package raises."""

    def __init__(self, message: str, *, status: int | None = None, problem: dict[str, Any] | None = None) -> None:
        super().__init__(message)
        self.status = status
        self.problem = problem or {}

    @property
    def title(self) -> str:
        return str(self.problem.get("title", "")) or self.__class__.__name__

    @property
    def sqlstate(self) -> str:
        return str(self.problem.get("sqlstate", ""))


class AuthError(AgentCoopDBError):
    """401 / 403 — the API key is missing, invalid, expired, or unauthorised for the operation."""


class ValidationError(AgentCoopDBError):
    """400 — the request was malformed or the SQL failed validator checks."""


class IdempotencyConflict(AgentCoopDBError):
    """409 — Idempotency-Key reused with a different request body."""


class RateLimited(AgentCoopDBError):
    """429 — too many requests; honour the Retry-After header."""

    def __init__(self, message: str, *, retry_after: float = 1.0, **kwargs: Any) -> None:
        super().__init__(message, **kwargs)
        self.retry_after = retry_after


class ServerError(AgentCoopDBError):
    """5xx — the server reported an internal error."""


class NetworkError(AgentCoopDBError):
    """Transport-level failure: DNS, connect, TLS, read timeout."""


class QueueFullError(AgentCoopDBError):
    """The local SQLite retry queue is at its configured maximum size."""
