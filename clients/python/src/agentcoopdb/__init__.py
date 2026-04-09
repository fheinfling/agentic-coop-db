"""agentcoopdb — Python client for the Agentic Coop DB auth gateway.

This package is intentionally tiny. The public surface is:

    from agentcoopdb import connect
    db = connect("https://db.example.com", api_key="acd_live_...")
    db.execute(sql, params)
    db.select(sql, params)
    db.transaction()
    db.vector_upsert(...)
    db.vector_search(...)
    db.rotate_key()
    db.health()
    db.me()

Plus error classes (errors.py) and the offline retry queue (queue.py).
"""

from agentcoopdb.client import AgentCoopDBClient, connect
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

__all__ = [
    "connect",
    "AgentCoopDBClient",
    "AgentCoopDBError",
    "AuthError",
    "ValidationError",
    "IdempotencyConflict",
    "RateLimited",
    "ServerError",
    "NetworkError",
    "QueueFullError",
]

__version__ = "0.1.0"
