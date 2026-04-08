"""Unit tests for backoff() and with_retry()."""

from __future__ import annotations

from unittest.mock import MagicMock

import pytest

from aicoopdb.errors import AuthError, NetworkError, RateLimited, ServerError, ValidationError
from aicoopdb.retry import backoff, with_retry


# ---- backoff() ---------------------------------------------------------------

def test_backoff_attempt0_in_range() -> None:
    # attempt=0: upper = min(300, 1.0 * 2**0) = 1.0 → result in [0, 1.0)
    for _ in range(50):
        d = backoff(0)
        assert 0.0 <= d < 1.0, f"backoff(0) = {d} out of range"


def test_backoff_caps_at_cap() -> None:
    # With a large attempt number the cap (300 s) must never be exceeded.
    for attempt in range(10, 20):
        d = backoff(attempt, cap=300.0)
        assert d <= 300.0, f"backoff({attempt}) = {d} exceeds cap"


def test_backoff_custom_cap() -> None:
    for _ in range(50):
        d = backoff(10, base=1.0, cap=5.0)
        assert 0.0 <= d <= 5.0, f"backoff with cap=5 gave {d}"


# ---- with_retry() ------------------------------------------------------------

def test_with_retry_success_on_first_call() -> None:
    calls = 0

    def fn() -> str:
        nonlocal calls
        calls += 1
        return "ok"

    result = with_retry(fn, sleep=MagicMock())
    assert result == "ok"
    assert calls == 1


def test_with_retry_retries_network_error() -> None:
    sleep = MagicMock()
    calls = 0

    def fn() -> None:
        nonlocal calls
        calls += 1
        raise NetworkError("timeout")

    with pytest.raises(NetworkError):
        with_retry(fn, max_attempts=3, sleep=sleep)

    assert calls == 3
    assert sleep.call_count == 3


def test_with_retry_retries_server_error() -> None:
    sleep = MagicMock()
    calls = 0

    def fn() -> None:
        nonlocal calls
        calls += 1
        raise ServerError("internal", status=500)

    with pytest.raises(ServerError):
        with_retry(fn, max_attempts=2, sleep=sleep)

    assert calls == 2


def test_with_retry_respects_rate_limited_retry_after() -> None:
    sleep = MagicMock()

    def fn() -> None:
        raise RateLimited("slow down", retry_after=5.0, status=429)

    with pytest.raises(RateLimited):
        with_retry(fn, max_attempts=2, sleep=sleep)

    # Every sleep call should have used at least the retry_after value.
    for call in sleep.call_args_list:
        delay = call.args[0]
        assert delay >= 5.0, f"sleep delay {delay} < retry_after 5.0"


def test_with_retry_no_retry_auth_error() -> None:
    sleep = MagicMock()
    calls = 0

    def fn() -> None:
        nonlocal calls
        calls += 1
        raise AuthError("bad key", status=401)

    with pytest.raises(AuthError):
        with_retry(fn, max_attempts=5, sleep=sleep)

    assert calls == 1, "AuthError must not be retried"
    sleep.assert_not_called()


def test_with_retry_no_retry_validation_error() -> None:
    sleep = MagicMock()
    calls = 0

    def fn() -> None:
        nonlocal calls
        calls += 1
        raise ValidationError("bad sql", status=400)

    with pytest.raises(ValidationError):
        with_retry(fn, max_attempts=5, sleep=sleep)

    assert calls == 1, "ValidationError must not be retried"
    sleep.assert_not_called()


def test_with_retry_succeeds_on_second_attempt() -> None:
    sleep = MagicMock()
    calls = 0

    def fn() -> str:
        nonlocal calls
        calls += 1
        if calls == 1:
            raise NetworkError("flaky")
        return "recovered"

    result = with_retry(fn, max_attempts=3, sleep=sleep)
    assert result == "recovered"
    assert calls == 2
    assert sleep.call_count == 1
