# ADR-0040: Allow negative savings (cost to implement)

## Status

Accepted

## Context

Upsizing recommendations are valid outcomes that cost money rather than saving it.

## Decision

Don't clamp savings at zero; negative values indicate implementation cost.

## Alternatives Considered

### Clamp at zero
Hides cost-increasing (upsizing) recommendations from fleet totals and misleads operators who filter on `savings > 0` only.

### Separate "cost increase" field
UI and API consumers must merge two fields; fleet aggregation logic duplicates and drifts from per-entity savings math.

## Related Decisions

- [ADR-0071](0071-exclude-gpu-from-savings-summary.md) and [ADR-0072](0072-exclude-quota-from-fleet-savings.md): fleet totals exclude GPU and quota savings to avoid misleading aggregate negatives from non-additive categories.

## Consequences

Honest savings reporting. UI must handle negative display. Fleet totals can decrease.

## References

- [internal/engine/negative_savings_test.go](internal/engine/negative_savings_test.go)
- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
