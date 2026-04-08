---
name: aicoldb-lint
description: Lint rule that flags db.execute(f"...{x}...") patterns
status: accepted
owner: ""
priority: p1
created: 2026-04-08
updated: 2026-04-08
---

## Problem

The Python SDK pushes parameterisation, but a lazy caller can still write
`db.execute(f"INSERT INTO notes VALUES ({user_input})")` and bypass the
intent. The gateway will accept whatever string the caller produces.

## Proposed solution

A tiny ast-based linter shipped under `clients/python/aicoldb/lint.py`
that walks Python source and flags:

- `db.execute(f"...")` with non-empty interpolation parts
- `db.execute("..." + x)` for any x
- `db.select(...)` with the same patterns

Wired into the project's `.pre-commit-hooks.yaml` so other open-source
consumers can adopt it. Also exposed as `aicoldb lint <path>` for ad-hoc
runs.

## Why deferred from v1

The validator already enforces `$N` placeholders at runtime. The linter is
a quality gate, not a security boundary.

## Acceptance criteria

- `aicoldb lint clients/python/tests/` exits 0
- A purposely-bad fixture file fails with a clear error message
- Available as a `pre-commit` hook
