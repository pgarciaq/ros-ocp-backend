# ADR-0136: Use operational runbooks + adversarial review as first-class docs

## Status

Accepted

## Context

Implicit tribal knowledge lost when team members rotate.

## Decision

Maintain runbooks for DLQ, migrations, SSRF, and publish adversarial audit findings.

## Consequences

Operational knowledge codified. New operators self-sufficient. Audit trail.

As of v5.0 (2026-06-11), all 85 findings across four review rounds are resolved or accepted with zero open items.

## References

- [docs/operations/runbooks.md](docs/operations/runbooks.md)
- [docs/audits/adversarial-review.md](docs/audits/adversarial-review.md)
