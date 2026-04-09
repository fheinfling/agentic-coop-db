# Feature roadmap

Every deferred / future feature lives as a Markdown file in this directory.
**Not** in the issue tracker, **not** in `CHANGELOG.md`, **not** as inline
`// TODO` comments. The directory is the single source of truth for
"things we have considered and plan to do later".

## Rules

1. One file per feature, named `NNNN-short-slug.md` (zero-padded, sequential).
2. Each file has a frontmatter header with:
   - `status:` `proposed` | `accepted` | `in-progress` | `shipped` | `rejected`
   - `owner:` GitHub handle or empty
   - `priority:` `p0` | `p1` | `p2` | `p3`
   - `created:` `YYYY-MM-DD`
   - `updated:` `YYYY-MM-DD`
3. Body sections: **Problem**, **Proposed solution**, **Why deferred**,
   **Acceptance criteria**, **Open questions**, **Links**.
4. This `README.md` is the index. CI fails if a file in `docs/features/`
   is missing from the index.
5. When a feature ships, move it to `docs/features/shipped/` and link it
   from `CHANGELOG.md`. Don't delete it — the rationale stays as historical record.

## Adding a feature idea

Pick the next free number, copy an existing file's frontmatter, fill in
the sections, add a row to the table below, and (optionally) file a
GitHub issue that links to the doc.

## Index

| #    | Slug                       | Status   | Priority | Summary                                                          |
|------|----------------------------|----------|----------|------------------------------------------------------------------|
| 0001 | go-client                  | proposed | p2       | A Go SDK with the same surface as the Python client              |
| 0002 | js-client                  | proposed | p1       | Browser+node TypeScript SDK                                      |
| 0003 | agentcoopdb-shell-repl         | proposed | p2       | An interactive REPL bound to the configured workspace            |
| 0004 | cloud-provider-examples    | proposed | p2       | Per-provider deploy walkthroughs (Hetzner, DO, AWS, bare metal)  |
| 0005 | sqlstate-http-mapping      | accepted | p1       | Document and stabilise the SQLSTATE → HTTP status mapping        |
| 0006 | pgvector-benchmarks        | proposed | p2       | Reproducible IVFFlat / HNSW benchmark suite                      |
| 0007 | agentcoopdb-lint               | accepted | p1       | A pre-commit linter that flags `db.execute(f"...{x}...")` patterns |
| 0008 | signed-releases            | accepted | p1       | Cosign-signed releases + SBOM                                    |
| 0009 | distributed-rate-limiting  | proposed | p2       | Redis-backed token buckets for multi-replica deployments         |
| 0010 | pgbouncer-frontend         | proposed | p3       | Stick PgBouncer in front of postgres for higher concurrency      |
| 0011 | realtime-subscriptions     | proposed | p3       | LISTEN/NOTIFY-backed websocket subscriptions                     |
| 0012 | object-storage             | proposed | p3       | S3-compatible upload/download endpoint                           |
| 0013 | ha-multi-region            | proposed | p3       | Multi-region replication and failover                            |
| 0014 | admin-web-ui               | proposed | p2       | Read-only admin dashboard (audit log, key inventory)             |
| 0015 | saml-oidc-sso              | proposed | p3       | SSO via SAML / OIDC for the admin endpoints                      |
