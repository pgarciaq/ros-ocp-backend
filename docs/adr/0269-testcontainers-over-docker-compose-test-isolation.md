# ADR-0269: testcontainers over docker-compose for test isolation

## Status

Accepted

## Phase

1

## Context

Integration tests need real PostgreSQL with migrations, partitions, and index behavior. Options: shared docker-compose service (fast startup, shared mutable state) or per-process testcontainers (isolated, slower startup).

Test pollution from shared DB caused flaky CI failures in early development.

## Decision

testcontainers with one PostgreSQL 16 container per test process. Mutex serialization prevents parallel test connection exhaustion. TRUNCATE between tests for isolation. No shared state between test runs.

## Alternatives Considered

### docker-compose shared DB

State pollution between tests; flaky CI.

### sqlmock only

Misses real SQL behavior (partitions, indexes, constraints).

### KEEPDB pattern (like Koku Django tests)

Requires careful cleanup; harder in Go test binaries.

## Consequences

- ~5s startup per test binary (container creation).
- Full isolation — no test pollution.
- CI does not need pre-running database services.
- Slightly slower than shared DB but more reliable.

## Related Decisions

- [ADR-0139](0139-testcontainers-pg16-integration.md): testcontainers PostgreSQL 16.
- [ADR-0142](0142-test-infrastructure-integration-patterns.md): Test infrastructure patterns.
- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory CI.

## References

- [internal/testutil/postgres.go](../../internal/testutil/postgres.go)
- [internal/testutil/migrate.go](../../internal/testutil/migrate.go)
