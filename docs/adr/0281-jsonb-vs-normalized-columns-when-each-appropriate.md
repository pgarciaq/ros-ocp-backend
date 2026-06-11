# ADR-0281: JSONB vs normalized columns — when each is appropriate

## Status

Accepted

## Phase

7–8

## Context

Schema design tension: JSONB columns (flexible, schemaless) vs normalized columns (indexable, type-safe). Different use cases warrant different approaches. Early prototypes used JSONB blobs for entire recommendation payloads ([ADR-0048](0048-relational-columns-not-jsonb-blobs.md) rejected that).

## Decision

Use JSONB for: tags/labels (variable keys), metadata blobs (operator-reported fields not queried), plugin-specific extension data. Use normalized columns for: anything in WHERE/ORDER BY clauses, any aggregation target, any field with validation rules. GPU child tables ([ADR-0034](0034-normalize-vm-gpu-devices-child-table.md)) use normalized columns despite nested structure.

## Alternatives Considered

### All JSONB

Loses query optimization and type safety.

### All normalized

Excessive migrations for ephemeral metadata.

### EAV (entity-attribute-value) pattern

Worst of both worlds — complex queries, no type safety.

## Consequences

- Tag filtering requires GIN indexes on JSONB ([ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md)).
- New filterable dimensions require migrations (cannot just add to JSONB).
- JSONB fields are opaque to query optimizer for sorting.

## Related Decisions

- [ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md): resolved_tags JSONB on keys.
- [ADR-0048](0048-relational-columns-not-jsonb-blobs.md): Relational columns not JSONB blobs.
- [ADR-0034](0034-normalize-vm-gpu-devices-child-table.md): VM GPU child table.

## References

- [internal/model/org_container_keys.go](../../internal/model/org_container_keys.go)
- [docs/architecture/schema-design.md](../../docs/architecture/schema-design.md)
