# ADR-0269: testcontainers over docker-compose for test isolation

## Status

Accepted

## Phase

1

## Context

Integration tests need real PostgreSQL with migrations, partitions, and index behavior. Options: shared docker-compose service (fast startup, shared mutable state) or per-process testcontainers (isolated, slower startup).

Test pollution from shared DB caused flaky CI failures in early development. SQLite mocks cannot test partition behavior, upserts, or PostgreSQL-specific SQL.

## Decision

testcontainers with one PostgreSQL 16 container per test process. Mutex serialization prevents parallel test connection exhaustion. TRUNCATE between tests for isolation. No shared state between test runs. golang-migrate applies all migrations to the container (incorporates former ADR-0139).

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
- Catches PostgreSQL-specific bugs that mocks miss.

## Implementation Details

### Single shared container per test process

`SetupTestDB` in `internal/testutil/testdb.go` starts one testcontainers PostgreSQL 16 instance per test process (`sharedTestDBOnce`). This avoids Docker exhaustion and deadlocks when many integration tests would each spawn their own container.

### Mutex serialization

`sharedTestDBMu` serializes integration tests against the shared pool so parallel test runs do not read or write each other's fixture data.

### TRUNCATE CASCADE isolation

Between tests, `truncatePublicTables` runs `TRUNCATE ... CASCADE` on all public tables **except**:

- `schema_migrations`
- `notification_code_definitions`
- `ros_partitioned_parent_registry`

This preserves migration state and static catalog data while resetting tenant-scoped fixture data.

### Connection pool cap

`SetForceTestPool()` caps the shared pool at **16 connections** (`sharedTestDBMaxConns`) to prevent connection exhaustion when many tests run sequentially against one container.

## Related Decisions

- [ADR-0142](0142-csv-contract-test-operator-columns.md): CSV contract test.
- [ADR-0249](0249-advisory-openapi-changelog-ci-non-blocking.md): Advisory CI.

## References

- [internal/testutil/testdb.go](../../internal/testutil/testdb.go)
- [internal/testutil/postgres.go](../../internal/testutil/postgres.go)
- [internal/testutil/migrate.go](../../internal/testutil/migrate.go)
