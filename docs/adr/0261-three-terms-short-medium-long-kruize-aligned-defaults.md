# ADR-0261: Three terms (short/medium/long) with Kruize-aligned defaults (1d/7d/15d)

## Status

Accepted

## Phase

1–3

## Context

Kruize offered `short_term` (1d), `medium_term` (7d), and `long_term` (15d) recommendation windows. The UI was built around these three named terms. The native engine needed to maintain this contract while allowing future customization.

FinOps best practice suggests 7/30/90 day windows, but changing defaults would break existing UI expectations.

## Decision

Fixed three term slots (`term_ord` 0/1/2 in schema). Default windows: 1d/7d/15d matching Kruize behavior and existing UI contract. Windows configurable per-org via `org_recommendation_terms` table and `/settings/terms` API (phase 6).

PVC and other plugins may override windows via `TermProvider` trait ([ADR-0108](0108-term-provider-per-plugin.md)) but cannot add a fourth term.

## Alternatives Considered

### Arbitrary N terms

Schema complexity and UI contract break.

### 7/30/90 FinOps standard

Breaks existing UI; available via PVC plugin TermProvider override.

### Single term only

Insufficient for cost/performance dual-engine UX.

## Consequences

- Cannot add a fourth term without schema/API breaking change.
- UI contract preserved across Kruize → native migration.
- Per-org window customization available since phase 6.
- 1/7/15 chosen for Kruize parity, not first-principles FinOps analysis.

## Related Decisions

- [ADR-0108](0108-term-provider-per-plugin.md): TermProvider per plugin.
- [ADR-0069](0069-filter-term-normalized.md): filter[term] normalization.
- [ADR-0004](0004-dual-cost-performance-engine-rows.md): Dual engine rows.
- [ADR-0270](0270-on-demand-api-time-recommendations-deferred.md): Realtime recs deferred.

## References

- [internal/model/org_recommendation_terms.go](../../internal/model/org_recommendation_terms.go)
- [internal/api/handlers_terms_settings.go](../../internal/api/handlers_terms_settings.go)
