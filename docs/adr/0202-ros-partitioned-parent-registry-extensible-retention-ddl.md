# ADR-0202: ros_partitioned_parent_registry for extensible retention DDL

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

The retention sweep drops old monthly partitions ([ADR-0132](0132-retention-policies-per-table.md)). Hardcoding parent table names in the drop function breaks when plugins add new partitioned tables.

## Decision

`ros_partitioned_parent_registry` table stores parent table name patterns (exact match + LIKE patterns). The retention function queries this registry to discover all partitioned tables. Plugins register their partitioned tables via migrations.

## Consequences

- Adding a new partitioned table only requires a registry INSERT migration — no changes to retention logic.
- Registry survives schema migrations (never dropped).
- Test `TRUNCATE` isolation excludes this table.

## Alternatives Considered

### Hardcoded list

Breaks on plugin addition. Rejected.

### Information_schema introspection

Slow and brittle across PostgreSQL versions. Rejected.

### Convention-based naming

Fragile when naming diverges. Rejected.

## Related Decisions

- [ADR-0059](0059-auto-create-partitions-in-go.md): Partition creation at first write.
- [ADR-0132](0132-retention-policies-per-table.md): Retention TTL policies.

## References

- [migrations/000060_ros_partitioned_parent_registry.up.sql](../../migrations/000060_ros_partitioned_parent_registry.up.sql)
- [internal/engine/retention.go](../../internal/engine/retention.go)
