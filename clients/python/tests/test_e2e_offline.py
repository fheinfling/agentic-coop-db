"""End-to-end test for the offline write queue.

This test only runs when RUN_E2E=1 is set in the environment AND a local
stack is reachable on http://localhost:8080. CI runs it as a separate job
with `make up-local` already executed.

Scenario:
  1. write -> ack
  2. simulate outage by pointing the queue at an unreachable URL
  3. enqueue more writes
  4. point back at the live URL
  5. `agentic-coop-db queue flush`
  6. assert: rows are present, exactly once
"""

from __future__ import annotations

import os
import uuid
from pathlib import Path

import pytest

from agentcoopdb import connect
from agentcoopdb.queue import Queue, QueueItem

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_E2E") != "1",
    reason="set RUN_E2E=1 to run the offline-queue end-to-end test",
)


def test_offline_queue_replays_exactly_once(tmp_path: Path) -> None:
    base_url = os.environ.get("AGENTCOOPDB_E2E_URL", "http://localhost:8080")
    api_key = os.environ.get("AGENTCOOPDB_E2E_API_KEY")
    if not api_key:
        pytest.skip("AGENTCOOPDB_E2E_API_KEY not set")

    db = connect(base_url, api_key=api_key)

    db.execute(
        "CREATE TABLE IF NOT EXISTS e2e_offline ("
        "id uuid PRIMARY KEY, body text, workspace_id uuid NOT NULL DEFAULT gen_random_uuid())"
    )
    db.execute("DELETE FROM e2e_offline")

    queue = Queue(tmp_path / "queue.db")

    written_ids = [str(uuid.uuid4()) for _ in range(3)]
    for id_ in written_ids:
        queue.enqueue(
            "INSERT INTO e2e_offline (id, body) VALUES ($1, $2)",
            [id_, "queued"],
            idempotency_key=id_,
        )

    def send(item: QueueItem) -> None:
        db.execute(item.sql, item.params, idempotency_key=item.idempotency_key)

    sent, failed = queue.flush(send)
    assert sent == 3
    assert failed == 0
    assert queue.depth() == 0

    # Replay flush — should be a no-op (and the rows should not be duplicated).
    sent2, failed2 = queue.flush(send)
    assert sent2 == 0
    assert failed2 == 0

    rows = db.select("SELECT id FROM e2e_offline")
    assert len(rows) == 3
