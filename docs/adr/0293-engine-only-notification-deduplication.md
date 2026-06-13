# ADR-0293: Engine-only notification emission in container/namespace detail JSON

## Status

Accepted

## Phase

Performance (A-2)

## Context

Container and namespace detail responses duplicated the same notification objects at
three JSON levels:

1. `recommendations.notifications` (top-level aggregate)
2. `recommendation_terms.<term>.notifications` (per-term aggregate)
3. `recommendation_engines.<engine>.notifications` (per-engine, authoritative)

Notifications are evaluated per engine during recommendation production. The
term-level and top-level copies were redundant aggregations added for Kruize UI
compatibility. They added several KB per container in list-sized payloads and
multiplied serialization cost on detail responses.

Performance audit A-2 identified this as P0 waste alongside the A-1 list DTO work.

## Decision

1. **Detail responses** (`BuildDetailResponse`, `BuildNamespaceDetailResponse`):
   emit `notifications` maps **only** on `recommendation_engines.<engine>`.
   Remove top-level and term-level notification maps.

2. **List responses** (`BuildListResponse`):
   replace the top-level `recommendations.notifications` map with a flat
   `recommendations.notification_codes` integer array (deduplicated across all
   terms/engines). The UI resolves badge text via
   `GET .../notification-codes`.

3. **OpenAPI**: split list item schema (`RecommendationListItem` /
   `ListRecommendations`) from detail schema (`Recommendations` /
   `DetailRecommendations`); document engine-only notifications on detail.

## Consequences

### Positive

- Smaller detail and list JSON payloads (fewer duplicated notification objects).
- Clearer semantics: notifications are scoped to the engine that produced them.
- List badges use lightweight codes; full messages remain on detail engines.

### Negative

- **Breaking change** for API consumers reading `recommendations.notifications`
  or `recommendation_terms.*.notifications` on detail responses.
- koku-ui must read engine-level notifications on detail and
  `notification_codes` on list rows.

### Neutral

- `GET .../notification-codes` catalog endpoint unchanged.
- Node, VM, PVC, quota, and other plugins retain their existing notification
  shapes (this ADR covers container/namespace Kruize-shaped responses only).

## Alternatives considered

| Alternative | Why rejected |
|-------------|--------------|
| Keep all three levels for backward compatibility | Defeats payload reduction goal; triplication is the problem |
| Top-level only, drop engine/term | Loses per-engine context; notifications differ between cost and performance |
| List: keep full notification map | Still heavy; codes + catalog is sufficient for badges |

## References

- [native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) — A-2
- [ADR-0065](0065-kruize-compatible-json-shape.md) — original nested JSON shape
- [ADR-0272](0272-detail-response-typed-struct-replaces-adhoc-json-maps.md) — DetailResponse struct
