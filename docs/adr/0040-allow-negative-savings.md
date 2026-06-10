# ADR-0040: Allow negative savings (cost to implement)

## Status

Accepted

## Context

Upsizing recommendations are valid outcomes that cost money rather than saving it.

## Decision

Don't clamp savings at zero; negative values indicate implementation cost.

## Consequences

Honest savings reporting. UI must handle negative display. Fleet totals can decrease.

## References

- [internal/engine/negative_savings_test.go](internal/engine/negative_savings_test.go)
- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
