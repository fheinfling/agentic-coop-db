---
name: signed-releases
description: Cosign-signed releases + SBOM
status: accepted
owner: ""
priority: p1
created: 2026-04-08
updated: 2026-04-08
---

## Problem

Releases are pushed to ghcr.io and PyPI but not signed. Downstream
consumers cannot verify provenance.

## Proposed solution

Add a `release.yml` workflow step that:

1. Uses `cosign` (keyless via OIDC) to sign every published image tag.
2. Generates a CycloneDX SBOM with `syft` and uploads it as a release asset.
3. Publishes a `.intoto.jsonl` provenance attestation.

## Why deferred from v1

The release pipeline must work first. Signing is a follow-up.

## Acceptance criteria

- `cosign verify ghcr.io/fheinfling/agentic-coop-db-server:0.1.0` succeeds keylessly
- The release page lists the SBOM and the provenance file
