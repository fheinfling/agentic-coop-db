"""aicoldb — Python client for the AIColDB auth gateway.

This package is intentionally tiny. The public surface is:

    from aicoldb import connect
    db = connect("https://db.example.com", api_key="aic_live_...")
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

from aicoldb.client import AIColDBClient, connect
from aicoldb.errors import (
    AIColDBError,
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
    "AIColDBClient",
    "AIColDBError",
    "AuthError",
    "ValidationError",
    "IdempotencyConflict",
    "RateLimited",
    "ServerError",
    "NetworkError",
    "QueueFullError",
]

__version__ = "0.1.0"
