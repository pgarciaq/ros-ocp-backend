# ADR-0248: v1-only API namespace with no v2 migration policy

## Status

Accepted

## Context

All ROS HTTP endpoints live under `/api/cost-management/v1/` per Koku platform convention ([ADR-0065](0065-kruize-compatible-json-shape.md)). Consumers include HCCM UI, on-prem shell, and IQE contract tests ([ADR-0141](0141-openapi-contract-tests-all-plugins.md)).

No formal API versioning policy existed: when to bump version, how long to deprecate fields, or whether v2 namespace is planned.

## Decision

**v1-only namespace** with no documented v2 migration path at this time.

Breaking change prevention relies on:

- OpenAPI contract tests on every plugin endpoint ([ADR-0141](0141-openapi-contract-tests-all-plugins.md))
- Advisory CI changelog checks ([ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md))
- Manual OpenAPI maintenance ([ADR-0074](0074-manual-openapi-contract-tests.md))

Compatibility rules:

- **Additive JSON fields** are always non-breaking.
- **Enum extensions** (new allowed values) are non-breaking.
- **Removed fields, renamed params, or semantic changes** require explicit reviewer approval and CHANGELOG entry; no automated version bump.

A future v2 namespace would require a new ADR with deprecation window criteria — not assumed today.

## Alternatives Considered

### Parallel v2 with sunset timeline

No consumer demand; doubles maintenance for OpenAPI and handlers.

### Header-based versioning

Inconsistent with Koku `/v1/` path convention.

### Code-first OpenAPI generation

Rejected in [ADR-0074](0074-manual-openapi-contract-tests.md).

## Consequences

- API authors must treat OpenAPI diff as the compatibility contract.
- Long-lived clients depend on additive-only discipline.
- Breaking changes are social/process gated, not mechanically versioned.

## Related Decisions

- [ADR-0074](0074-manual-openapi-contract-tests.md): Manual OpenAPI and contract tests.
- [ADR-0141](0141-openapi-contract-tests-all-plugins.md): Contract tests all plugins.
- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory OpenAPI changelog CI.

## References

- [openapi.json](../../openapi.json)
- [docs/operations/api-query-parameters.md](../operations/api-query-parameters.md)
