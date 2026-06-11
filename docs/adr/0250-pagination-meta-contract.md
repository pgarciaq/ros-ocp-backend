# ADR-0250: Pagination meta contract — count, limit, offset, has_next, next link

## Status

Accepted

## Context

List endpoints use keyset pagination ([ADR-0066](0066-keyset-after-cursor-pagination.md), [ADR-0190](0190-keyset-cursor-tie-breaker-tuples-per-resource-type.md)) with base64url JSON cursors encoded in the `after` parameter. UI and IQE expect consistent `meta` and `links` shapes across resource types.

Pre-computed totals ([ADR-0189](0189-precomputed-org-recommendation-stats.md)) feed `meta.count`. Keyset mode complicates offset semantics when `after` cursor is present.

## Decision

All list endpoints return:

| Field | Semantics |
|-------|-----------|
| `meta.count` | Total rows matching filters (may be approximate for expensive counts) |
| `meta.limit` | Page size requested/applied |
| `meta.offset` | Offset for offset-mode lists; **0 when `after` cursor present** |
| `meta.has_next` | Boolean — more pages exist |
| `links.next` | URL for next page, or `null` when exhausted |

`listKeysetMeta.applyOffset` zeroes `meta.offset` when keyset `after` cursor is active — clients must not mix offset and cursor pagination in one sequence.

**Caching:** Default fleet rollup list responses may use LRU cache ([ADR-0112](0112-bounded-lru-ttl-cost-cache.md), [ADR-0185](0185-fleet-savings-lru-cache-rbac-keys.md)). Responses with **`group_by` savings** parameters are **NOT** cached — only default rollup paths are cache-eligible.

## Alternatives Considered

### Omit offset in keyset mode entirely

Breaks clients expecting numeric field; zero is explicit sentinel.

### Cache group_by responses

RBAC and parameter cardinality explode cache key space.

### Offset-only pagination

Rejected for deep pages ([ADR-0066](0066-keyset-after-cursor-pagination.md)).

## Consequences

- OpenAPI schemas must document offset=0 keyset behavior.
- Contract tests assert `has_next`/`links.next` consistency per resource.
- Fleet cache invalidation rules apply only to non-group_by summaries.

## Related Decisions

- [ADR-0066](0066-keyset-after-cursor-pagination.md): Keyset pagination.
- [ADR-0189](0189-precomputed-org-recommendation-stats.md): Pre-computed counts.
- [ADR-0185](0185-fleet-savings-lru-cache-rbac-keys.md): Fleet cache RBAC keys.

## References

- [internal/api/pagination.go](../../internal/api/pagination.go)
- [internal/api/meta.go](../../internal/api/meta.go)
