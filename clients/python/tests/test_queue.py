"""Unit tests for the offline retry queue."""

from __future__ import annotations

from pathlib import Path

import pytest

from aicoldb.errors import NetworkError, ValidationError
from aicoldb.queue import Queue, QueueItem


@pytest.fixture()
def q(tmp_path: Path) -> Queue:
    return Queue(tmp_path / "queue.db", max_size=10)


def test_enqueue_increments_depth(q: Queue) -> None:
    assert q.depth() == 0
    q.enqueue("INSERT INTO t VALUES ($1)", [1], "k1")
    assert q.depth() == 1


def test_max_size_raises_queue_full(q: Queue) -> None:
    from aicoldb.errors import QueueFullError

    for i in range(10):
        q.enqueue("SELECT 1", [], f"k{i}")
    with pytest.raises(QueueFullError):
        q.enqueue("SELECT 1", [], "k11")


def test_flush_acks_on_success(q: Queue) -> None:
    q.enqueue("SELECT 1", [], "k1")
    q.enqueue("SELECT 2", [], "k2")

    sent_calls: list[QueueItem] = []

    def send(item: QueueItem) -> None:
        sent_calls.append(item)

    sent, failed = q.flush(send)
    assert sent == 2
    assert failed == 0
    assert q.depth() == 0


def test_flush_backs_off_on_network_error(q: Queue) -> None:
    q.enqueue("SELECT 1", [], "k1")
    q.enqueue("SELECT 2", [], "k2")

    def send(item: QueueItem) -> None:
        raise NetworkError("offline")

    sent, failed = q.flush(send)
    assert sent == 0
    assert failed == 1
    # Item still pending (with bumped attempt count + delayed next_attempt_at).
    assert q.depth() == 2


def test_permanent_failure_moves_to_dead_letter(q: Queue) -> None:
    q.enqueue("INSERT INTO bad VALUES ($1)", [1], "k1")

    def send(item: QueueItem) -> None:
        raise ValidationError("schema does not exist")

    sent, failed = q.flush(send)
    assert sent == 0
    assert failed == 1
    assert q.depth() == 0
    assert q.dead_letter_count() == 1
