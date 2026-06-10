# ADR-0117: Include infrastructure + supplementary + distributed costs in savings

## Status

Accepted

## Context

CPU/memory rates alone miss OCP-on-cloud correlated infrastructure costs.

## Decision

Aggregate all three cost types from effective_rates into savings calculation.

## Consequences

Accurate savings reflecting true cost. Depends on Koku exposing all cost types.

## References

- [internal/engine/savings.go](internal/engine/savings.go)
