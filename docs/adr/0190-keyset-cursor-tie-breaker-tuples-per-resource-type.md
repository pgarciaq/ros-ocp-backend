# ADR-0190: Keyset cursor tie-breaker tuples per resource type

## Status

Accepted

## Context

Each list endpoint (container, namespace, PVC, node, GPU MIG, GPU timeslicing, quota, VM, machineset, snapshot) needs a pagination cursor. Different resources have different natural orderings and identity columns. A single universal cursor cannot encode resource-specific tie-breaker tuples.

## Decision

Each resource type defines a cursor struct with resource-specific tie-breaker columns (identity tuple + sort value). Seek uses SQL tuple comparison with `IS NOT DISTINCT FROM` for nullable sort columns. The cursor is base64-encoded JSON (opaque to clients, not documented in OpenAPI). When an `after` cursor is present, offset is cleared.

MachineSet uses offset pagination (not keyset) due to aggregation query structure.

### Implementation notes

- Cursor structs and seek predicates live in `internal/api/cursor.go` and `internal/api/pagination_keyset.go`.
- Nullable sort columns must use `IS NOT DISTINCT FROM` rather than `=` so NULL sort keys paginate correctly.
- MachineSet is the explicit exception: Tier-1 aggregation queries do not support stable keyset seeks.

## Consequences

- No universal cursor — each resource handler must define its own tie-breaker tuple.
- Nullable sort columns require `IS NOT DISTINCT FROM` (not `=`).
- MachineSet is the exception to keyset pagination.
- Cursor format is opaque to clients (base64 JSON, not documented in OpenAPI).

## Alternatives Considered

### Universal cursor

A single cursor schema cannot encode resource-specific identity columns (container UUID vs node name vs PVC name). Rejected.

### Offset-only pagination

Simple for clients but performance degrades at depth; overlapping pages when data mutates between fetches. Rejected except for MachineSet aggregation.

### Encrypted cursor

Hides cursor contents from tampering but adds key management and rotation overhead without a demonstrated threat model. Rejected.

## Related Decisions

- [ADR-0066](0066-keyset-after-cursor-pagination.md): Keyset pagination concept and offset cap.
- [ADR-0066](0066-keyset-after-cursor-pagination.md): Base64 JSON cursor encoding (keyset pagination).
- [ADR-0188](0188-list-query-keys-pagination-refilter-detail.md): Keys/detail split for list queries.

## References

- [internal/api/cursor.go](../../internal/api/cursor.go)
- [internal/api/pagination_keyset.go](../../internal/api/pagination_keyset.go)
