---
name: realtime-subscriptions
description: LISTEN/NOTIFY-backed websocket subscriptions
status: proposed
owner: ""
priority: p3
created: 2026-04-08
updated: 2026-04-08
---

## Problem

Users coming from Supabase ask for "realtime" — push notifications when a
row changes.

## Proposed solution

Expose a websocket endpoint at `/v1/subscribe` that subscribes to a
Postgres `LISTEN` channel filtered by `app.workspace_id`. Triggers on
tenant tables emit notifications via `NOTIFY` to the channel; the server
filters and forwards to the relevant subscribers.

## Why deferred from v1

Realtime is an entirely separate transport surface (websockets, fan-out,
backpressure). It would dominate the codebase and is explicitly listed as
a non-goal in the README. v1 ships without it; this doc is here so users
have a place to land when they ask.

## Acceptance criteria

- Subscribers see only events from their own workspace
- Backpressure: slow consumers get disconnected, not buffered indefinitely
- A `aicoldb subscribe <table>` CLI demo
