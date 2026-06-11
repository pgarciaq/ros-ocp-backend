# ADR-0237: Runtime partition pre-creation (current + next month)

## Status

Accepted

## Context

Usage and history tables partition by month ([ADR-0058](0058-partition-by-usage-start-month.md)). [ADR-0059](0059-auto-create-partitions-in-go.md) creates partitions on first write. [ADR-0202](0202-ros-partitioned-parent-registry-extensible-retention-ddl.md) extended the registry for retention DDL.

Boot-time failures or clock skew near month boundaries can cause ingest to hit "partition does not exist" before the first write path runs partition creation.

## Decision

At process startup, `EnsureIngestPartitionsAtStartup` creates partitions for **current month and next month** for all ingest-parent tables registered in the partition registry.

Per-batch ingest continues to call `Ensure*Partition` on demand if a partition is still missing (e.g., leap-month edge cases or new parent registration).

Partition pre-creation failures are logged **non-fatal** at startup. If both pre-create and on-demand creation fail, ingest fails later with PostgreSQL "partition does not exist".

## Alternatives Considered

### First-write only (ADR-0059 alone)

Month-boundary race when worker starts on last day of month and ingest targets next month immediately.

### Pre-create 12 months ahead

Wastes empty partitions; retention drops handle old months separately.

### Fatal startup on partition failure

Blocks API mode when only processor needs partitions; rejected for deployment flexibility.

## Consequences

- Processor and housekeeper startups should run partition pre-create; API-only mode may skip or no-op safely.
- Operators monitoring logs should alert on repeated partition ensure warnings before ingest errors spike.
- Registry additions require startup hook registration for new partitioned parents.

## Related Decisions

- [ADR-0059](0059-auto-create-partitions-in-go.md): Auto-create partitions at first write.
- [ADR-0202](0202-ros-partitioned-parent-registry-extensible-retention-ddl.md): Partitioned parent registry.
- [ADR-0132](0132-retention-policies-per-table.md): Retention policies per table.

## References

- [internal/partition/ensure.go](../../internal/partition/ensure.go)
- [internal/partition/registry.go](../../internal/partition/registry.go)
