# ADR-0170: MachineSet Tier-1 aggregation over node recommendations

## Status

Accepted

## Context

Operators expect MachineSet-level sizing recommendations (e.g., "scale this MachineSet from m5.xlarge to m5.2xlarge"). The native engine produces per-node recommendations. A dedicated MachineSet engine (Tier-2) is planned but not yet implemented.

## Decision

Ship a Tier-1 MachineSet list endpoint (`GET /recommendations/openshift/machinesets`) that aggregates node recommendations by MachineSet label at query time. No dedicated database table or migration required. Response includes member node count, combined resource totals, and the "worst-case" recommendation across member nodes.

## Alternatives Considered

### Wait for Tier-2 engine

Delays operator value by months; node-level data is already available.

### Materialized view

Adds migration complexity and invalidation logic for a v1 feature that may be replaced by Tier-2.

### Client-side aggregation

Pushes complexity to the frontend and duplicates RBAC-scoped filtering already done server-side.

## Consequences

- Immediate value without new migrations or engine complexity.
- Query-time aggregation cost (acceptable for typical MachineSet counts &lt;100 per cluster).
- When Tier-2 engine ships, this endpoint's implementation changes but the API contract stays the same.
- Engineers must not confuse this with the planned Tier-2 `machineset_recommendations` table.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native Go recommendation engine.
- [ADR-0061](0061-dual-engine-rows-for-nodes.md): Dual engine rows for node recommendations.

## References

- [internal/api/handlers_machinesets.go](../../internal/api/handlers_machinesets.go)
- [internal/api/handlers_machineset_pagination.go](../../internal/api/handlers_machineset_pagination.go)
