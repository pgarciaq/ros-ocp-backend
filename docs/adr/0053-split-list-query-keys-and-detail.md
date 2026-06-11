# ADR-0053: Split list query: keys table for identity, detail table for rec state

## Status

Accepted

## Context

Single-table pagination dropped filters on re-join for fields like workload_type.

## Decision

Two-phase query: filter/paginate on keys table, then join detail for recommendation data.

## Alternatives Considered

### Single-table pagination with all columns
Paginating directly on `recommendation_sets` pulled every wide column into sort nodes; at 200k+ rows per org the planner oscillated between seq scans and expensive sort steps, and filters on `workload_type` dropped after re-join when pagination wrapped.

### Lateral joins (keys ⋈ LATERAL detail subquery)
A single query with `LATERAL` subselects looked elegant, but PostgreSQL 16 planner regressed on lateral plans against partitioned `recommendation_sets`, producing nested-loop costs 10× higher than the two-phase approach in `native_list_keys.go`.

### Wide denormalized view combining keys and detail
A database view merging both tables avoided two round-trips, but PostgreSQL cannot push `LIMIT`/`ORDER BY` through the view when detail columns participate in the sort key, forcing full materialization before pagination.

## Consequences

Correct filtering. Two queries per list call. Consistent pagination semantics.

## References

- [internal/model/native_list_keys.go](internal/model/native_list_keys.go)
