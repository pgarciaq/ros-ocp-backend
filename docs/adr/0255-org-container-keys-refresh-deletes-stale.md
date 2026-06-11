# ADR-0255: org_container_keys refresh deletes stale keys on ingest

## Status

Accepted

## Context

List pagination uses denormalized `org_container_keys` ([ADR-0052](0052-org-container-keys-denormalized-index.md), [ADR-0188](0188-list-query-keys-pagination-refilter-detail.md)). Keys must reflect containers with active (non-stale) recommendation sets for accurate cursors and counts ([ADR-0189](0189-precomputed-org-recommendation-stats.md)).

Retention purges stale recommendation rows on a schedule ([ADR-0203](0203-retention-side-effects-beyond-partition-drop.md)). If keys linger after workloads disappear, list pages show phantom identities until retention catches up.

## Decision

`RefreshOrgContainerKeys` on ingest:

1. **Upserts** keys for containers present in non-stale `recommendation_sets` for the org/cluster scope being refreshed.
2. **DELETEs** keys no longer present in that active set.

Pagination keys may disappear **before** corresponding detail rows are retention-purged — keys table is the leading edge of inventory; detail rows follow retention TTL.

## Alternatives Considered

### Upsert only, never delete until retention

Stale keys pollute list filters and inflate `meta.count`.

### Soft-delete key rows

Complicates keyset cursors; hard delete keeps index lean.

### Delete keys only on retention job

Hours-long window where list shows dead containers.

## Consequences

- List/detail brief inconsistency possible: key gone, detail row still queryable by UUID until purge.
- Ingest refresh cost scales with org container churn; batched where possible.
- Tag filters on keys ([ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md)) drop with key delete.

## Related Decisions

- [ADR-0188](0188-list-query-keys-pagination-refilter-detail.md): Keys vs detail split.
- [ADR-0203](0203-retention-side-effects-beyond-partition-drop.md): Retention side effects.
- [ADR-0052](0052-org-container-keys-denormalized-index.md): Denormalized keys index.

## References

- [internal/model/org_container_keys.go](../../internal/model/org_container_keys.go)
- [internal/processor/keys_refresh.go](../../internal/processor/keys_refresh.go)
