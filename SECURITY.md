# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | yes       |
| < 0.1   | no        |

Once 1.0 ships, the previous minor will continue to receive security fixes
for 6 months.

## Reporting a vulnerability

**Please do not open a public issue.** Use [GitHub Security Advisories](https://github.com/fheinfling/agentic-coop-db/security/advisories/new)
or email **security@franzheinfling.com** (PGP key in `docs/security.md`).

Include:

- A description of the issue and the impact
- Steps to reproduce (or a proof of concept)
- The version and deployment profile (`local`, `pi-lite`, `cloud`, `swarm`)
- Whether this affects the gateway, the SDK, or a deployment artifact

## Response timeline

| Stage              | Target time     |
|--------------------|-----------------|
| Acknowledgement    | 48 hours        |
| Initial assessment | 7 days          |
| Critical fix + CVE | 7 days from confirmed report |
| Patch release      | with the fix    |
| Public disclosure  | after the patch is released and downstreams have had a reasonable update window |

We follow [coordinated disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure)
and will credit reporters in the advisory unless they ask not to be named.

## Threat model

Documented in [`docs/security.md`](docs/security.md).
