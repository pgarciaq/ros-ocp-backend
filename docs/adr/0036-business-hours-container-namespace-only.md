# ADR-0036: Scope business hours to container+namespace only

## Status

Accepted

## Context

Infrastructure (nodes) and storage (PVC) don't follow office-hour patterns.

## Decision

Only container and namespace plugins support business-hours schedules.

## Alternatives Considered

### Extend business hours to nodes and PVC
Node consolidation and storage growth don't follow office-hour patterns; BH recommendations would mislead infrastructure teams with arbitrary scale-down windows.

### Cluster-wide business-hours schedule
Too coarse for mixed workloads—batch jobs and 24×7 services in the same cluster need per-namespace schedules, not one cluster default.

## Consequences

No misleading BH recommendations for infrastructure. Simpler for ops-focused plugins.

## References

- [docs/features-business-hours.md](docs/features-business-hours.md)
