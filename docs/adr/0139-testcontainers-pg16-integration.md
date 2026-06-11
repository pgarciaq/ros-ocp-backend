# ADR-0139: Use testcontainers PostgreSQL 16 + golang-migrate for integration tests

## Status

Accepted

## Context

SQLite mocks can't test partition behavior, upserts, or PG-specific SQL.

## Decision

Testcontainers spins real PG16; golang-migrate applies all migrations.

## Consequences

Real database behavior. Slower than mocks. Catches PG-specific bugs.

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

## References

- [internal/testutil/testdb.go](../../internal/testutil/testdb.go)
