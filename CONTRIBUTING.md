# Contributing to AIColDB

Thanks for considering a contribution. AIColDB is intentionally small, so the
fastest way to get a PR landed is to start with a `good-first-issue` from the
issue tracker or pick a `proposed`/`accepted` feature from
[`docs/features/`](docs/features/).

## Developer Certificate of Origin (DCO)

By contributing to this project, you certify that you have the right to submit
your contribution under the Apache-2.0 license. We use the
[Developer Certificate of Origin](https://developercertificate.org/) — every
commit must be signed off:

```
git commit -s -m "your message"
```

The `-s` flag appends a `Signed-off-by: Your Name <you@example.com>` trailer.
PRs without sign-offs will be flagged by CI.

## Code of conduct

This project follows the Contributor Covenant 2.1 — see
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

## Local dev setup

You need:

- Go 1.24+
- Docker (with the daemon enabled)
- Python 3.11+ (for the SDK and CLI)
- Make

```bash
git clone https://github.com/fheinfling/aicoldb.git
cd aicoldb
make build              # go build ./... + python -m build
make test-unit          # fast unit tests
make up-local           # bring up postgres + api on localhost:8080
make test-integration   # testcontainers + cross-tenant + sql bypass
make test-e2e           # python offline-queue test
make down
```

`scripts/dev-up.sh` is a wrapper that ensures the Docker daemon is running.

## What goes where

| Concern                       | Path                              |
|-------------------------------|-----------------------------------|
| HTTP transport                | `internal/httpapi/`               |
| API key auth + middleware     | `internal/auth/`                  |
| SQL validator + executor      | `internal/sql/`                   |
| RPC registry + dispatcher     | `internal/rpc/`                   |
| Tenant context (`SET LOCAL`)  | `internal/tenant/`                |
| pgvector helpers              | `internal/vector/`                |
| Audit log writer              | `internal/audit/`                 |
| Pool + tx helpers             | `internal/db/`                    |
| Config (envconfig)            | `internal/config/`                |
| slog/prom/optional OTEL       | `internal/observability/`         |
| ldflags-injected build info   | `internal/version/`               |

`internal/` is unimportable from outside this module. `cmd/server/main.go` is
the only place that wires the layers together.

## PR checklist

- [ ] All commits are signed off (`git commit -s`)
- [ ] `make test-unit` passes
- [ ] `make lint` passes (golangci-lint + ruff + mypy)
- [ ] If you added a tenant table, `scripts/lint-migrations` passes
- [ ] If you added a Go dependency, justified in `docs/adr/0000-dependencies.md`
- [ ] If you closed a feature file, moved it to `docs/features/shipped/` and
      linked from `CHANGELOG.md`
- [ ] If you added a new public API, there is at least one test that doubles as
      usage documentation
- [ ] No `// TODO` comments — write a `docs/features/NNNN-*.md` file instead

## Adding a feature idea

If you have an idea that does not fit v1:

1. Pick the next free number under `docs/features/` (e.g. `0016-…`).
2. Copy the frontmatter format from an existing file.
3. Add it to `docs/features/README.md`.
4. Optionally file a GitHub issue and link to the doc from there.

CI fails if a file in `docs/features/` is missing from the index.

## Security disclosure

Do **not** open a public issue for security vulnerabilities. See
[`SECURITY.md`](SECURITY.md) for the private reporting process.
