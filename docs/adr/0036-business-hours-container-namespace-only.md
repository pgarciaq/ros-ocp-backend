# ADR-0036: Scope business hours to container+namespace only

## Status

Accepted

## Context

Infrastructure (nodes) and storage (PVC) don't follow office-hour patterns.

## Decision

Only container and namespace plugins support business-hours schedules.

## Consequences

No misleading BH recommendations for infrastructure. Simpler for ops-focused plugins.

## References

- [docs/features-business-hours.md](docs/features-business-hours.md)
