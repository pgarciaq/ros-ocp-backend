# ADR-0034: Normalize vm_gpu_devices JSONB to child table

## Status

Accepted

## Context

JSONB arrays prevent SQL filtering and notification logic on per-GPU telemetry.

## Decision

Normalize GPU device data from JSONB into relational child table.

## Consequences

Enables SQL-level GPU filtering on VMs. More tables. Standard relational patterns.

## References

- [docs/architecture/database-conventions.md](docs/architecture/database-conventions.md)
