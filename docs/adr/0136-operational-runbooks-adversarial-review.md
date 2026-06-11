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

## CI Governance Workflows

Adversarial review findings #53, #54, and #60 motivated advisory CI workflows that catch drift proactively:

| Workflow | Purpose |
|----------|---------|
| [`.github/workflows/openapi-changelog-check.yml`](../../.github/workflows/openapi-changelog-check.yml) | Advisory: warns when API-affecting paths (see [`.github/openapi-paths.txt`](../../.github/openapi-paths.txt)) change without `openapi.json` or `CHANGELOG.md` updates |
| [`.github/workflows/adr-reminder.yml`](../../.github/workflows/adr-reminder.yml) | Advisory: posts reminder on PRs touching architectural paths (see [`.github/architectural-paths.txt`](../../.github/architectural-paths.txt)) |
| [`.github/workflows/govulncheck.yml`](../../.github/workflows/govulncheck.yml) | Runs `govulncheck` on PRs and weekly schedule (pinned `@v1.1.4`) |

These complement the adversarial review process by catching documentation and dependency drift before merge.

## References

- [docs/operations/runbooks.md](../operations/runbooks.md)
- [docs/audits/adversarial-review.md](../audits/adversarial-review.md)
