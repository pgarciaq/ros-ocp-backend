# ADR-0188: List query splits identity on keys table, re-filters on recommendation_sets

## Status

Accepted

## Context

Native container list uses keyset pagination ([ADR-0066](0066-keyset-after-cursor-pagination.md)) over a lightweight identity table ([ADR-0052](0052-org-container-keys-denormalized-index.md), [ADR-0053](0053-split-list-query-keys-and-detail.md)). The keys table PK is `(org_id, namespace, workload, container_name)` and collapses `workload_type` and `cluster_uuid`—dimensions present on `recommendation_sets`.

Detail-only filters must not pollute the keys seek path.

## Decision

- Pagination cursor navigates `org_container_keys` for O(1) page seeks
- After fetching a page of identity tuples, re-apply all filters on `recommendation_sets` (full dimensionality including `workload_type` and `cluster_uuid`)
- Detail-only filters (`stale`, `has_gpu`, `idle_state`, `term`, date ranges) apply **only** to `recommendation_sets`, never to keys

## Alternatives Considered

### Paginate on recommendation_sets directly

Slower for large orgs without identity-only index structure.

### Add all dimensions to keys table

Bloats the table and complicates invalidation on every dimension change.

### Materialized view joining keys and detail

Maintenance overhead and stale-read complexity during ingest.

## Consequences

- Keys table may over-fetch—keys match but detail fails re-filter, yielding slightly smaller pages than requested.
- Workload_type collisions in keys are rare in practice (same namespace + workload + container with different types).
- Pre-computed counts avoid live `COUNT(DISTINCT …)` ([ADR-0189](0189-precomputed-org-recommendation-stats.md)).

## Related Decisions

- [ADR-0052](0052-org-container-keys-denormalized-index.md): Denormalized keys index.
- [ADR-0053](0053-split-list-query-keys-and-detail.md): Split list query architecture.
- [ADR-0189](0189-precomputed-org-recommendation-stats.md): Pre-computed list counts.

## References

- [internal/model/native_list_keys.go](../../internal/model/native_list_keys.go)
- [internal/model/org_container_keys.go](../../internal/model/org_container_keys.go)
