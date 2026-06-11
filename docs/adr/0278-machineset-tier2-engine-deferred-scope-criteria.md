# ADR-0278: MachineSet Tier-2 engine deferred — scope criteria documented

## Status

Accepted

## Phase

11–12

## Context

[ADR-0170](0170-machineset-tier1-aggregation-over-node-recommendations.md) ships Tier-1 MachineSet API (query-time aggregation over node recommendations). Tier-2 would add a dedicated engine computing MachineSet-level recommendations with cross-node correlation.

Customer demand for Tier-2 is unvalidated; cross-node scheduling model is unddesigned.

## Decision

Defer Tier-2 until: (1) customer demand validated, (2) cross-node scheduling model designed, (3) migration for `machineset_recommendations` table justified. Tier-1 provides immediate value. API contract designed to be forward-compatible with Tier-2 backend change.

## Alternatives Considered

### Ship Tier-2 now

Months of work for unvalidated demand.

### Never ship Tier-2

Limits operator automation for fleet sizing at scale.

## Consequences

- No dedicated MachineSet engine in v1.
- Query-time aggregation has performance ceiling (~100 nodes/MachineSet acceptable).
- Tier-2 implementation will not change API contract.

## Related Decisions

- [ADR-0170](0170-machineset-tier1-aggregation-over-node-recommendations.md): MachineSet Tier-1 aggregation.
- [ADR-0194](0194-machineset-consolidation-query-design.md): MachineSet consolidation query.
- [ADR-0260](0260-per-container-recommendation-granularity-operator-csv-grain.md): Per-container granularity.

## References

- [internal/api/handlers_machineset.go](../../internal/api/handlers_machineset.go)
- [docs/features/machineset-recommendations.md](../../docs/features/machineset-recommendations.md)
