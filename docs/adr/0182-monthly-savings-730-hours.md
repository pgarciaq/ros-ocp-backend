# ADR-0182: Monthly savings extrapolation uses 730 hours constant

## Status

Accepted

## Context

Savings must be expressed as monthly dollar values for dashboard comparison. Calendar months vary (28–31 days). A consistent extrapolation constant is needed across container, node, VM, and quota resource types ([ADR-0117](0117-savings-include-all-cost-types.md)).

## Decision

All savings multiply hourly usage deltas by `hoursPerMonth = 730` (≈30.4 days × 24 h).

Effective rates derive from namespace aggregates:

- `CostModelCPUCost / CPURequestHours` (and memory analogue)
- Infrastructure and distributed costs apportioned by distribution type (`cpu` vs `memory`)

Without Koku cost model data present, savings are zero ([ADR-0113](0113-nil-cost-provider-when-masu-unavailable.md)).

## Alternatives Considered

### Calendar-accurate months

Requires per-month computation and complicates year-over-year comparison.

### 720 hours (30 days exactly)

Slightly less accurate than 730 for typical billing periods.

## Consequences

- Savings are approximate (±3% vs an actual calendar month).
- Consistent extrapolation across all resource types and API surfaces.
- Rate derivation depends on Masu effective rates being current ([ADR-0111](0111-rates-from-koku-masu.md)).

## Related Decisions

- [ADR-0040](0040-allow-negative-savings.md): Negative savings allowed for rightsizing.
- [ADR-0117](0117-savings-include-all-cost-types.md): Cost types included in savings.

## References

- [internal/engine/savings.go](../../internal/engine/savings.go)
- [internal/engine/node_savings.go](../../internal/engine/node_savings.go)
- [internal/engine/vm_savings.go](../../internal/engine/vm_savings.go)
