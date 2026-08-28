# ADR-0294: Slim list contract for container and namespace recommendations

## Status

Accepted

## Phase

Performance (S4, H-4)

## Context

Container list responses already use a slim DTO (`BuildListResponse`) that omits
plots, `duration_in_hours`, `business_hours`, and per-engine notification maps
while preserving the fields the optimizations table reads: `current`, selected
term/engine `variation`, top-level `notification_codes`, and `monitoring_end_time`.

Namespace list responses still assembled full `NamespaceDetailResponse` rows for
every list item, including empty plot slots and full term/engine trees the
projects table never renders. Badge and summary widgets also issued separate
`limit=1` fetches whose single rows triggered GPU and business-hours enrichment
even though only `meta.count` was consumed.

Performance audit items S4 (slim list contract), H-4 (list field projection),
H-5 (cursor pagination in UI), and H-6 (cross-component count cache) depend on a
consistent list-vs-detail split.

## Decision

1. **List contract (S4):** List endpoints return slim DTOs; detail endpoints
   return full `DetailResponse` / `NamespaceDetailResponse` with plots and
   engine-level notifications.

   | Endpoint family | List DTO | Detail DTO |
   |-----------------|----------|------------|
   | Container | `ListResponse` (`BuildListResponse`) | `DetailResponse` |
   | Namespace | `NamespaceListResponse` (`BuildNamespaceListResponse`) | `NamespaceDetailResponse` |

2. **Default projection:** When `term` and `engine` query params are omitted,
   list items include **`short_term` + `cost` only** (same default as container
   since ADR-0293). Callers that need other windows or engines pass
   `term=medium_term&engine=performance` (or `filter[term]` / `filter[engine]`).

3. **Count-only optimization:** Skip GPU, business-hours, and currency
   enrichment when `limit <= 1` on list handlers (CSV export still enriches).

4. **UI alignment:** koku-ui passes `term` and `engine` explicitly on list calls,
   prefers `after=<meta.next_cursor>` for forward pagination when available, and
   shares `meta.count` across badge/summary/table via a Redux count cache keyed
   by filter parameters (excluding pagination/sort).

## Consequences

### Positive

- Smaller namespace list JSON payloads and faster list handler CPU.
- Count-only requests avoid expensive enrichment plugins.
- Explicit term/engine params improve HTTP cache key stability and future UI
  term/engine selectors.
- Single count source reduces duplicate `limit=1` API calls.

### Negative / trade-offs

- List responses no longer include plots or full term trees unless requested via
  query params — consumers that assumed detail-shaped list rows must use the
  detail endpoint or pass broader `term`/`engine` filters. Unfiltered fat list
  rows still omit nested `business_hours` ([#497](https://github.com/pgarciaq/ros-ocp-backend/issues/497)).
- Cursor pagination does not support arbitrary page jumps; UI keeps offset for
  backward navigation and page-number display.

## References

- [Container recommendations feature doc](../features/container-recommendations.md)
- [Namespace recommendations feature doc](../features/namespace-recommendations.md)
- ADR-0293 (engine-only notifications, container slim list)
- Performance audit `docs/performance/native-engine-audit-2026-06.md` (S4, H-4, H-5, H-6)
