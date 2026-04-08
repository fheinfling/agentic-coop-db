# aicoldb (Python client)

A thin Python client for [AIColDB](https://github.com/fheinfling/aicoldb), the
auth gateway for shared PostgreSQL.

```python
from aicoldb import connect

db = connect("https://db.example.com", api_key="aic_live_...")
db.execute(
    "INSERT INTO notes(id, body) VALUES ($1, $2)",
    ["b9c3...", "hi"],
)
rows = db.select("SELECT * FROM notes WHERE owner = $1", ["alice"])
```

## Install

```bash
pip install aicoldb
```

## CLI

```bash
aicoldb init                  # interactive onboarding wizard
aicoldb me
aicoldb sql "SELECT 1"
aicoldb queue flush
aicoldb doctor
```

See the [main repo](https://github.com/fheinfling/aicoldb) for the full docs.
