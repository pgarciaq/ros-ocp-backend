# ADR-0274: Remove rh_accounts join — direct org_id filtering on recommendation tables

## Status

Accepted

## Phase

8–9

## Context

Legacy schema used `rh_accounts` table as join target for org_id lookups. Every list/detail query joined through this table even though `org_id` was denormalized onto recommendation tables ([ADR-0051](0051-org-id-on-every-detail-lookup.md)).

The join added query planner overhead and prevented index-only scans on hot paths.

## Decision

Remove `rh_accounts` join from all list/detail queries. Filter directly by `org_id` column on `recommendation_sets` and related tables. Reduces query complexity and enables index-only scans.

## Alternatives Considered

### Keep join for consistency

Performance penalty on every request.

### Materialized view bridging org lookups

Maintenance overhead without benefit.

## Consequences

- Queries simpler and faster.
- `rh_accounts` table retained for cluster metadata but not on hot path.
- One-time migration ensured `org_id` populated on all rows.

## Related Decisions

- [ADR-0051](0051-org-id-on-every-detail-lookup.md): org_id on every detail lookup.
- [ADR-0188](0188-list-query-keyset-pagination-design.md): List query design.
- [ADR-0052](0052-org-container-keys-denormalized-index.md): org_container_keys index.

## References

- [internal/model/list_query.go](../../internal/model/list_query.go)
- [migrations/000058_denormalize_org_id.up.sql](../../migrations/000058_denormalize_org_id.up.sql)
