# ADR-0041: Use savings on all_hours row only; BH affects sizing not dollars

## Status

Accepted

## Context

Counting BH and calendar savings separately would double-count in fleet totals.

## Decision

Persist savings only on `all_hours` schedule type; BH affects sizing for UX but not fleet dollars.

## Consequences

No double-counting. BH savings visible in detail view only. Fleet totals remain accurate.

## References

- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
