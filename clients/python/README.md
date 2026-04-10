# agentcoopdb (Python client)

A thin Python client for [Agentic Coop DB](https://github.com/fheinfling/agentic-coop-db), the
auth gateway for shared PostgreSQL.

```python
from agentcoopdb import connect

db = connect("https://db.example.com", api_key="acd_live_...")
db.execute(
    "INSERT INTO notes(id, body) VALUES ($1, $2)",
    ["b9c3...", "hi"],
)
rows = db.select("SELECT * FROM notes WHERE owner = $1", ["alice"])
```

## Install

```bash
pip install agentic-coop-db
```

Or install directly from GitHub (the Python client lives in a subdirectory):

```bash
pip install "agentic-coop-db @ git+https://github.com/fheinfling/agentic-coop-db.git#subdirectory=clients/python"
```

## CLI

```bash
agentic-coop-db init                  # interactive onboarding wizard
agentic-coop-db me
agentic-coop-db sql "SELECT 1"
agentic-coop-db queue flush
agentic-coop-db doctor
```

See the [main repo](https://github.com/fheinfling/agentic-coop-db) for the full docs.
