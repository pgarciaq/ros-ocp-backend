# ADR-0117: Include infrastructure + supplementary + distributed costs in savings

## Status

Accepted

## Context

CPU/memory rates alone miss OCP-on-cloud correlated infrastructure costs.

## Decision

Aggregate all three cost types from effective_rates into savings calculation. Total savings = sum across infrastructure + supplementary + markup (distributed overhead included where exposed by Masu).

## Alternatives Considered

### CPU/memory rates only
Understates savings for GPU- and storage-heavy workloads; OCP-on-cloud tenants miss infrastructure and markup components that dominate bill.

### Separate savings per cost type in API
UI cannot show a single actionable number; operators must mentally sum three fields on every recommendation card.

## Consequences

Accurate savings reflecting true cost. Depends on Koku exposing all cost types.

## References

- [internal/engine/savings.go](internal/engine/savings.go)
