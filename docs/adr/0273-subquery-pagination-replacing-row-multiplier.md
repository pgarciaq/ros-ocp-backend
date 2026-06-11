# ADR-0273: Subquery pagination replacing row-multiplier approach

## Status

Accepted

## Phase

6–7

## Context

Each container has up to six recommendation rows (3 terms × 2 engines). Early pagination multiplied `limit` by 6 to fetch correct page sizes. This broke when filters eliminated some term/engine combinations — pages returned fewer containers than requested.

Client-side assembly was considered but shifts complexity to frontend.

## Decision

Replace row-multiplier with subquery pagination: inner query fetches container identities (page), outer query joins full recommendation rows. Correct page sizes regardless of filter combinations.

## Alternatives Considered

### Client-side page assembly

Shifts complexity to frontend; inconsistent across clients.

### Fixed 6-row assumption

Breaks with filters or future term count changes.

## Consequences

- More complex SQL (subquery + join).
- Correct pagination for all filter scenarios.
- Later evolved into keyset pagination ([ADR-0188](0188-list-query-keyset-pagination-design.md)) in phases 8–9.

## Related Decisions

- [ADR-0188](0188-list-query-keyset-pagination-design.md): Keyset pagination design.
- [ADR-0066](0066-keyset-after-cursor-pagination.md): Keyset after cursor.
- [ADR-0250](0250-pagination-meta-contract.md): Pagination meta contract.

## References

- [internal/model/list_query.go](../../internal/model/list_query.go)
- [internal/api/handlers_list.go](../../internal/api/handlers_list.go)
