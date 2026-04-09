"""Exponential backoff with full jitter.

Used by both the offline queue (queue.py) and the in-process retry helper
that wraps every transient HTTP request in client.py.
"""

from __future__ import annotations

import random
import time
from collections.abc import Callable
from typing import TypeVar

from agentcoopdb.errors import AgentCoopDBError, NetworkError, RateLimited, ServerError

T = TypeVar("T")


def backoff(attempt: int, base: float = 1.0, cap: float = 300.0) -> float:
    """Return the delay in seconds for the given retry attempt (0-indexed).

    Full jitter: uniform in [0, min(cap, base * 2**attempt)).
    """
    upper = min(cap, base * (2.0**attempt))
    return random.uniform(0.0, upper)


def with_retry(fn: Callable[[], T], *, max_attempts: int = 5, sleep: Callable[[float], None] = time.sleep) -> T:
    """Run fn() and retry on transient errors with backoff.

    Retried: NetworkError, ServerError, RateLimited.
    Not retried: ValidationError, AuthError, IdempotencyConflict.
    """
    last: BaseException | None = None
    for attempt in range(max_attempts):
        try:
            return fn()
        except RateLimited as e:
            sleep(max(e.retry_after, backoff(attempt)))
            last = e
        except (NetworkError, ServerError) as e:
            sleep(backoff(attempt))
            last = e
        except AgentCoopDBError:
            raise
    assert last is not None
    raise last
