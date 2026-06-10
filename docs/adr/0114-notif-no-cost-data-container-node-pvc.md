# ADR-0114: Emit notification code 25 (NotifNoCostData) on container/node/PVC, not GPU

## Status

Accepted

## Context

GPU doesn't persist savings; emitting no-cost-data on GPU would be noisy and misleading.

## Decision

Code 25 emitted only for resource types that persist savings.

## Consequences

Clean notification semantics. GPU recommendations don't false-alert on missing rates.

## References

- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
