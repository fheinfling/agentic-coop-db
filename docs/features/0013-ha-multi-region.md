---
name: ha-multi-region
description: Multi-region replication and failover
status: proposed
owner: ""
priority: p3
created: 2026-04-08
updated: 2026-04-08
---

## Problem

v1 is single-node. A regional outage means downtime.

## Proposed solution

Build on Postgres logical replication (or, more likely, an external HA
operator like Patroni or CloudNativePG). The gateway gains read-replica
routing: writes go to the primary, reads can be routed to local replicas.

## Why deferred from v1

HA Postgres is a project-sized investment. Doing it badly is worse than
not doing it. v1 is honest about being single-node so users self-select.

## Acceptance criteria

- Documented HA reference architecture with at least one supported tool
- Read replica routing via a `prefer_replica` HTTP header
- RPO + RTO targets stated explicitly
