# ADR-0081: Use meta.currency + per-object currency propagation

## Status

Accepted

## Context

Hardcoded USD-only breaks after Koku multi-currency effective_rates.

## Decision

Response `meta.currency` field; per-object `units` in MoneyAmount.

## Consequences

Multi-currency ready. Currency determined by cost model, not ROS.

## References

- [internal/api/currency.go](internal/api/currency.go)
