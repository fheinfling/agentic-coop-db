# ADR 0006 — Hoster-agnostic cloud profile

**Status:** accepted

## Decision

The original spec mentioned a "Hetzner profile". We renamed it to `cloud`
and made it work on any provider with DNS + ports 80/443. The only
provider-specific bits are:

- The DNS A record (you create it yourself).
- The restic backend URL in `deploy/.env` (S3 / B2 / SFTP / local).

Caddy auto-TLS via Let's Encrypt is the same on every provider.

## Why

We want the README to lead with one quickstart, not a per-provider matrix.
A user reading the cloud guide should not have to find their provider in a
list — they bring their own host, point a DNS record at it, and run
`make up-cloud`.

## docs/deploy-cloud.md

Worked examples for Hetzner Cloud, DigitalOcean, AWS Lightsail, and bare
metal. They differ only in the host bootstrap; the rest of the doc is
identical.

## What was given up

- We do not get hoster-specific niceties (Hetzner Cloud Volumes, AWS EBS
  snapshots) for free. Those are intentionally out of scope for v1; bring
  your own backup destination via restic.
