# ADR-0249: Advisory OpenAPI changelog CI is non-blocking

## Status

Accepted

## Context

[ADR-0136](0136-operational-runbooks-adversarial-review.md) and adversarial review finding #53 introduced `.github/workflows/openapi-changelog-check.yml`. The workflow detects PRs that touch API-affecting Go paths without updating `openapi.json` or `CHANGELOG.md`.

Blocking CI on every internal refactor produces false positives: handler moves, RBAC-only changes, and test-only edits may touch globed paths without user-visible API impact.

## Decision

The OpenAPI changelog check is **advisory only**:

- Posts PR comments when API-affecting files change without corresponding `openapi.json` / `CHANGELOG.md` updates.
- Does **NOT** block merge (no required check gate).

Reviewers must **actively verify** the advisory comment — dismiss when internal-only, update OpenAPI/CHANGELOG when behavior or schema changes.

Path globs live in `.github/openapi-paths.txt`; intentionally broad to err toward reminder over silence.

## Alternatives Considered

### Required blocking check

Slows development on false positives; teams bypass with empty CHANGELOG noise.

### No automation

Regressions slip without reviewer memory ([ADR-0074](0074-manual-openapi-contract-tests.md)).

### Block only on openapi.json drift via codegen

No codegen; manual OpenAPI remains source ([ADR-0074](0074-manual-openapi-contract-tests.md)).

## Consequences

- Merge is possible with stale OpenAPI — contract tests ([ADR-0141](0141-openapi-contract-tests-all-plugins.md)) remain the hard gate in CI test job.
- Authors should not ignore advisory comments without explicit review note.
- ADR reminder workflow ([ADR-0136](0136-operational-runbooks-adversarial-review.md)) complements but does not replace OpenAPI discipline.

## Related Decisions

- [ADR-0136](0136-operational-runbooks-adversarial-review.md): Operational runbooks and governance.
- [ADR-0248](0248-v1-only-api-no-v2-migration-policy.md): v1-only API policy.
- [ADR-0141](0141-openapi-contract-tests-all-plugins.md): Contract tests.

## References

- [.github/workflows/openapi-changelog-check.yml](../../.github/workflows/openapi-changelog-check.yml)
- [.github/openapi-paths.txt](../../.github/openapi-paths.txt)
