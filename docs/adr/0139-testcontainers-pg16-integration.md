# ADR-0139: Use testcontainers PostgreSQL 16 + golang-migrate for integration tests

## Status

Accepted

## Context

SQLite mocks can't test partition behavior, upserts, or PG-specific SQL.

## Decision

Testcontainers spins real PG16; golang-migrate applies all migrations.

## Consequences

Real database behavior. Slower than mocks. Catches PG-specific bugs.

## References

- [internal/testutil/](internal/testutil/)
