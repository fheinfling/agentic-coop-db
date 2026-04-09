---
name: cloud-provider-examples
description: Per-provider walkthroughs (Hetzner, DO, AWS, bare metal)
status: proposed
owner: ""
priority: p2
created: 2026-04-08
updated: 2026-04-08
---

## Problem

`docs/deploy-cloud.md` mentions multiple providers, but the worked examples
are short. New users want a copy/paste path for their specific provider.

## Proposed solution

One `docs/deploy/<provider>.md` per provider with:

- Provider account setup
- Server provisioning (CLI commands or web UI screenshots)
- DNS configuration
- First-time `make up-cloud` walkthrough
- Cost estimate

Providers to cover:

- Hetzner Cloud (ARM `cax11`)
- DigitalOcean Droplet
- AWS Lightsail
- Vultr
- bare metal (Pi 5)

## Why deferred from v1

Documentation breadth — not blocking on functionality.

## Acceptance criteria

- Each guide reproduces a working `https://db.<domain>/healthz` from a
  brand-new account
- All guides cross-link to the central `docs/deploy-cloud.md`
