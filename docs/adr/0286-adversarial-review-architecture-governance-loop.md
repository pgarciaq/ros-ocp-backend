# ADR-0286: Adversarial review as architecture governance loop

## Status

Accepted

## Phase

11–12

## Context

Traditional code review catches implementation bugs but misses systemic architectural gaps. After 11 phases of rapid development, a structured quality gate was needed before declaring the native engine production-ready.

Accumulated blind spots included cache invalidation gaps, SSRF edge cases, and missing OpenAPI components.

## Decision

Implement iterative adversarial reviews (v1.6 → v2.0 → v3.0 → v4.0 → v5.0) as a governance mechanism. Each review: systematic 8-dimension analysis → numbered findings → prioritized remediation → implementation → re-review. Findings drive ADR backfill (162 → 257+ ADRs) and hardening code (~3,900 LOC).

## Alternatives Considered

### External security audit only

Expensive, one-shot, misses architectural decisions.

### No formal review

Accumulated blind spots ship to production.

### Continuous automated scanning only

Misses architectural and design-level gaps.

## Consequences

- 85 findings identified and resolved across 5 review rounds.
- ADR coverage expanded from ~162 to 257+ records.
- Hardening code adds ~3% to codebase.
- Process formalized as reusable skill; diminishing returns reached at v5.0 (zero new findings).

## Related Decisions

- [ADR-0136](0136-operational-runbooks-adversarial-review.md): Runbooks + adversarial review.
- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory CI.
- [ADR-0285](0285-phase-branch-merge-order-migration-renumbering.md): Phase merge strategy.

## References

- [docs/operations/adversarial-review/](../../docs/operations/adversarial-review/)
- [.cursor/skills/adversarial-review/SKILL.md](../../.cursor/skills/adversarial-review/SKILL.md)
