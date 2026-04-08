---
name: pgvector-benchmarks
description: Reproducible IVFFlat / HNSW benchmarks
status: proposed
owner: ""
priority: p2
created: 2026-04-08
updated: 2026-04-08
---

## Problem

The pgvector docs claim certain throughputs and latencies for IVFFlat and
HNSW indexes. Users ask "what should I see on my hardware?" and we don't
have a reproducible answer.

## Proposed solution

`scripts/bench/pgvector.sh`:

- Loads N synthetic embeddings of M dimensions
- Builds IVFFlat (lists=L) and HNSW indexes
- Times top-k cosine queries
- Emits CSV that `tools/plot.py` turns into a chart

Run on the Pi 5 reference hardware and on a 4 vCPU / 8 GB cloud VM.
Publish results in `docs/benchmarks/pgvector.md`.

## Why deferred from v1

Benchmarks don't ship the gateway. They are a credibility artefact.

## Acceptance criteria

- Reproducible from a clean checkout
- Results published for at least two reference machines
- Includes a "what does this mean for me?" section
