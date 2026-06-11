# ADR-0236: Large-table index strategy — manual CONCURRENTLY pre-step for production

## Status

Accepted

## Context

Migrations live in a centralized numbered directory ([ADR-0063](0063-centralized-migrations-with-plugin-headers.md)). `golang-migrate` wraps each migration in a transaction. PostgreSQL forbids `CREATE INDEX CONCURRENTLY` inside a transaction block.

Large partitioned tables (`recommendation_sets`, digest tables, `org_container_keys`) cannot take blocking `CREATE INDEX` in production without unacceptable ingest/API downtime. [ADR-0137](0137-migration-lint-concurrently-template.md) introduced lint and templates but did not codify the operator pre-step workflow.

## Decision

For indexes on known large tables:

1. **Production:** Operators manually apply `CREATE INDEX CONCURRENTLY` (documented SQL in `migrations/README.md`) **before** running `migrate up` with the matching numbered migration that creates the same index non-concurrently (no-op if index exists) or references the pre-created index.
2. **CI / dev:** Migrations run without pre-step on small test datasets; blocking index creation is acceptable.
3. **Lint:** `scripts/lint-migrations.sh` warns when new migrations add non-concurrent indexes on tables in the large-table allowlist.

Migrations must remain idempotent where possible (`IF NOT EXISTS`) so pre-applied concurrent indexes do not fail migrate.

## Alternatives Considered

### Split migrate tool (non-transactional migrations)

 golang-migrate lacks first-class support; custom runner adds operational complexity.

### pg_partman-managed indexes only

Does not cover all ROS application indexes on partitioned parents.

### Always CONCURRENTLY via manual ops only, no migrate file

Drift between environments; migrate remains source of truth for schema shape.

## Consequences

- Runbooks must list pre-step order for production deploys.
- Authors of new indexes on large tables must update README pre-step section and lint allowlist.
- Missing pre-step in production causes long table locks during migrate — operational risk, not CI failure.

## Related Decisions

- [ADR-0063](0063-centralized-migrations-with-plugin-headers.md): Centralized migrations directory.
- [ADR-0137](0137-migration-lint-concurrently-template.md): Migration lint and CONCURRENTLY template.
- [ADR-0058](0058-partition-by-usage-start-month.md): Partitioned table layout.

## References

- [migrations/README.md](../../migrations/README.md)
- [scripts/lint-migrations.sh](../../scripts/lint-migrations.sh)
