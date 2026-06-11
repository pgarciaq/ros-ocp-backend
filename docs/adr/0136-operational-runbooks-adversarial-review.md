# ADR-0136: Use operational runbooks + adversarial review as architecture governance

## Status

Accepted

## Context

Implicit tribal knowledge lost when team members rotate. Traditional code review catches implementation bugs but misses systemic architectural gaps — cache invalidation holes, SSRF edge cases, missing OpenAPI components, and cross-cutting security assumptions.

## Decision

Maintain runbooks for DLQ, migrations, SSRF, and publish adversarial audit findings as first-class operational documentation.

### Adversarial review governance loop (incorporates former ADR-0286)

After 11 phases of rapid native-engine development, implement **iterative adversarial reviews** (v1.6 → v2.0 → v3.0 → v4.0 → v5.0) as a structured quality gate before declaring production-ready:

1. **Systematic analysis** — eight dimensions (security, API contract, data model, ingest, cost, ops, test coverage, documentation).
2. **Numbered findings** — each finding gets priority, owner, and remediation path.
3. **Implementation + re-review** — fix, then re-run; stop when diminishing returns (v5.0 reached zero new findings).

Results across five rounds:

- **85 findings** identified and resolved.
- **ADR backfill** — coverage expanded from ~162 to 257+ records driven by review gaps.
- **Hardening code** — ~3,900 LOC of security and contract fixes (~3% of codebase).
- **Reusable process** — formalized in `.cursor/skills/adversarial-review/` for future phases.

This complements (does not replace) code review: reviewers hunt implementation bugs; adversarial review hunts architectural blind spots.

## Consequences

Operational knowledge codified. New operators self-sufficient. Audit trail. Architectural gaps caught before production rather than in customer incidents.

As of v5.0 (2026-06-11), all 85 findings across five review rounds are resolved or accepted with zero open items.

## CI Governance Workflows

Adversarial review findings #53, #54, and #60 motivated advisory CI workflows that catch drift proactively:

| Workflow | Purpose |
|----------|---------|
| [`.github/workflows/openapi-changelog-check.yml`](../../.github/workflows/openapi-changelog-check.yml) | Advisory: warns when API-affecting paths (see [`.github/openapi-paths.txt`](../../.github/openapi-paths.txt)) change without `openapi.json` or `CHANGELOG.md` updates |
| [`.github/workflows/adr-reminder.yml`](../../.github/workflows/adr-reminder.yml) | Advisory: posts reminder on PRs touching architectural paths (see [`.github/architectural-paths.txt`](../../.github/architectural-paths.txt)) |
| [`.github/workflows/govulncheck.yml`](../../.github/workflows/govulncheck.yml) | Runs `govulncheck` on PRs and weekly schedule (pinned `@v1.1.4`) |

These complement the adversarial review process by catching documentation and dependency drift before merge.

## Alternatives Considered (adversarial review)

### External security audit only

Expensive, one-shot, misses architectural decisions and internal context.

### No formal review

Accumulated blind spots ship to production.

### Continuous automated scanning only

Misses architectural and design-level gaps (contract drift, multi-tenant assumptions, ingest ordering).

## Related Decisions

- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory CI.
- [ADR-0285](0285-phase-branch-merge-order-migration-renumbering.md): Phase merge strategy.

## References

- [docs/operations/runbooks.md](../operations/runbooks.md)
- [docs/audits/adversarial-review.md](../audits/adversarial-review.md)
- [docs/operations/adversarial-review/](../operations/adversarial-review/)
- [.cursor/skills/adversarial-review/SKILL.md](../../.cursor/skills/adversarial-review/SKILL.md)
