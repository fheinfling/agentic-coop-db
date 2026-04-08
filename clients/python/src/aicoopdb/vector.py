"""Vector helpers for the Python client.

These mirror the Go-side internal/vector helpers and are exposed on the
client as `db.vector_upsert` / `db.vector_search`. The functions here are
re-exported so callers can also import them directly when they want to
build their own retry / batching wrappers.
"""

from __future__ import annotations

from collections.abc import Sequence


def format_vector(v: Sequence[float]) -> str:
    """Format a sequence of floats as a pgvector literal: [1.0,2.0,3.0]."""
    return "[" + ",".join(repr(float(x)) for x in v) + "]"
