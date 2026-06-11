# ADR-0144: Keep domain tests colocated; add wiring tests per plugin extraction

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Big-bang test moves obscure regression ownership.

## Decision

Tests stay next to implementation; plugin extraction adds targeted wiring tests.

## Consequences

Clear ownership. Easy to find related tests. Some duplication acceptable.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
