---
name: object-storage
description: S3-compatible upload/download endpoint
status: proposed
owner: ""
priority: p3
created: 2026-04-08
updated: 2026-04-08
---

## Problem

Many app schemas need to store files alongside structured data
(images, PDFs, audio). Putting them in Postgres bytea columns is rarely
the right answer.

## Proposed solution

A `/v1/storage/<bucket>/<key>` endpoint that proxies to a configured
S3-compatible backend (MinIO, R2, B2, S3). Auth uses the same API key; the
gateway enforces a per-key quota and writes an audit row.

## Why deferred from v1

v1 is a SQL gateway. Object storage is a parallel feature surface that
deserves its own design pass.

## Acceptance criteria

- Pluggable backend (s3, minio, r2)
- Per-workspace quota enforced server-side
- Streaming upload + download (no buffering in the api process)
