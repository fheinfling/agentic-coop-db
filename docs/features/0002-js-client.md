---
name: js-client
description: TypeScript SDK for browser and node
status: proposed
owner: ""
priority: p1
created: 2026-04-08
updated: 2026-04-08
---

## Problem

The README quickstart shows a raw `fetch()` example for JavaScript, which
works but is verbose. A typed SDK with built-in error mapping and retry
would massively reduce the friction for the most common consumer.

## Proposed solution

A `clients/js` package published as `@aicoldb/client` on npm. ESM only,
zero deps, ships TypeScript types out of the box.

```ts
import { connect } from "@aicoldb/client";
const db = connect("https://db.example.com", { apiKey: "aic_..." });
await db.execute("INSERT INTO notes(id, body) VALUES ($1, $2)", [id, "hi"]);
const rows = await db.select("SELECT * FROM notes WHERE owner = $1", ["alice"]);
```

## Why deferred from v1

Same reason as the Go SDK — the v1 release ships the gateway and the
Python client first. JS is the next priority.

## Acceptance criteria

- Published to npm as `@aicoldb/client`
- TypeScript strict mode passes
- Works in node 20+ and modern browsers (no polyfills required)
- README quickstart matches the curl example
