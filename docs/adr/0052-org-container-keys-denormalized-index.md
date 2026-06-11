# ADR-0052: Use org_container_keys denormalized index for list pagination

## Status

Accepted

## Context

Scanning full recommendation_sets (200k+ rows) for pagination was too slow.

## Decision

Maintain denormalized `org_container_keys` table optimized for list/filter/sort.

## Alternatives Considered

### Covering indexes on recommendation_sets
A wide covering index on `recommendation_sets` could avoid heap fetches, but the table carries full recommendation payloads (CPU/memory limits, notification arrays, cost fields)—index entries exceed PostgreSQL page size limits and bloat autovacuum on every ingest upsert.

### Materialized views refreshed on ingest
A `REFRESH MATERIALIZED VIEW` after each manifest would precompute list projections, but refresh locks block concurrent API reads and still requires full recompute when a single container updates; incremental maintenance is as complex as the dedicated keys table.

### Elasticsearch for list/search
Elasticsearch excels at faceted search, but on-prem deployments would need another stateful cluster, duplicate denormalized state from PostgreSQL, and operate outside the existing GORM/pgx transaction boundaries used in `native_list_keys.go`.

## Consequences

Fast pagination. Requires sync on ingest. Extra storage for denormalized data.

## References

- [internal/model/native_list_keys.go](internal/model/native_list_keys.go)
- [internal/model/org_container_keys.go](internal/model/org_container_keys.go)
