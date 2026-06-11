# ADR-0183: Separate estimated_waste_cents for idle workloads

## Status

Accepted

## Context

Idle and zombie workloads represent complete waste (100% of allocation cost), not partial over-provisioning. Rightsizing savings—a delta between current and recommended requests—is a different optimization type ([ADR-0175](0175-idle-api-surfaces-waste-not-savings.md)).

Persisting both concepts in one column would confuse aggregation and UI presentation.

## Decision

Idle and zombie rows persist `estimated_waste_cents` (100% of current allocation cost) distinct from rightsizing `estimated_savings_cents`.

- API hides savings and shows waste for idle rows
- Fleet aggregation separates the two totals
- `group_by[idle_state]` on fleet summary sums waste, not savings

Schema added in migration 000083.

## Alternatives Considered

### Single savings field for both optimization types

Confuses rightsizing opportunity with terminate-only waste.

### Set savings equal to waste for idle rows

Technically numeric but semantically misleading in savings-focused dashboards.

## Consequences

- Total recoverable opportunity = savings + waste (distinct optimizations, not double-counted).
- Schema carries both columns; queries must filter by idle state when summing.
- Waste uses same 730-hour extrapolation as savings ([ADR-0182](0182-monthly-savings-730-hours.md)).

## Related Decisions

- [ADR-0175](0175-idle-api-surfaces-waste-not-savings.md): API waste vs savings surface.
- [ADR-0182](0182-monthly-savings-730-hours.md): Monthly extrapolation constant.

## References

- [internal/engine/savings.go](../../internal/engine/savings.go)
- [migrations/000083_add_idle_state_columns.up.sql](../../migrations/000083_add_idle_state_columns.up.sql)
