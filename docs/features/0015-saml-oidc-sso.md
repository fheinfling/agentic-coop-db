---
name: saml-oidc-sso
description: SSO via SAML / OIDC for the admin endpoints
status: proposed
owner: ""
priority: p3
created: 2026-04-08
updated: 2026-04-08
---

## Problem

API keys are great for service-to-service auth, but for human operators
in an enterprise context, SSO (SAML / OIDC) is often a hard requirement.

## Proposed solution

Add an `/auth/oidc/login` flow that exchanges an OIDC id token for a
short-lived API key bound to the operator's email. Same for SAML.

## Why deferred from v1

API keys cover the v1 use case. SSO adds significant compliance and
maintenance surface (SP metadata, certificate rotation, IdP-specific
quirks). Best done after we have real enterprise users asking for it.

## Acceptance criteria

- An operator can log in via their IdP and receive a key minted on the fly
- The minted key has a short TTL (max 24h) and is logged in `audit_logs`
- Compatible with at least one IdP (Okta or Authentik)
