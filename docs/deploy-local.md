# Local development

```bash
git clone https://github.com/fheinfling/agentic-coop-db.git
cd agentic-coop-db
make up-local
```

This:

- generates `deploy/secrets/postgres_password.txt` if missing
- builds the api image from the local source
- starts postgres + api on `http://localhost:8080`
- runs migrations on first boot
- exposes postgres on `127.0.0.1:5432` (handy for `psql` / `pgcli`)

Mint an admin key:

```bash
./scripts/gen-key.sh default dbadmin
# prints: acd_dev_<id>_<secret>   <-- copy this once, it is shown only here
```

Then point any HTTP client at `http://localhost:8080`:

```bash
curl -H "Authorization: Bearer acd_dev_..." http://localhost:8080/v1/me
```

## Tearing down

```bash
make down
```

This removes the compose project, including the postgres volume — any data
in the local stack is gone. Use `docker compose -p agentcoopdb stop` if you want
to keep the volume.

## Common dev commands

```bash
make logs                 # tail the api + postgres logs
make test-unit            # fast unit tests
make test-integration     # testcontainers + cross-tenant + sql bypass
make lint                 # golangci-lint + ruff + mypy + lint-migrations
make build                # binaries -> bin/
```
