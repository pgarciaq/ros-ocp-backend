# ADR-0229: Container savings derive effective rates from namespace aggregates (not raw rates)

## Status

Accepted

## Context

Koku provides both raw configured rates and aggregated cost/hour values per namespace. Savings computation needs $/core-hour and $/GiB-hour rates that reflect markup and distribution, not list prices alone.

## Decision

Effective rates are derived:

- CPU: `CostModelCPUCost / CPURequestHours`
- Memory: analogous ratio on memory cost and request hours

Infrastructure and distributed costs are apportioned by `distribution_type` (cpu vs memory weighting). Uses namespace-level aggregated data from Masu, not the raw `ConfiguredRates` map from the cost model.

## Alternatives Considered

### Use raw configured rates

Does not account for markup and platform/worker distribution.

### Fetch rates per container

Too granular; massive API call volume.

## Consequences

- Savings accuracy depends on Koku summarization being current.
- Engineers debugging savings may inspect wrong field (configured vs effective).
- Distribution type changes in Koku alter ROS savings without ROS code changes.

## Related Decisions

- [ADR-0182](0182-monthly-savings-730-hours.md): 730h monthly extrapolation.
- [ADR-0228](0228-effective-rates-cache-key-org-cluster-only.md): Cache key design.
- [ADR-0111](0111-rates-from-koku-masu.md): Rates sourced from Masu.

## References

- [internal/cost/effective_rates.go](../../internal/cost/effective_rates.go)
- [internal/engine/savings_container.go](../../internal/engine/savings_container.go)
