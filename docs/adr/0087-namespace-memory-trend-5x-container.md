# ADR-0087: Use namespace memory trend threshold 5× container (500 KiB/day)

## Status

Accepted

## Context

Aggregate noise scales with pod count; container-identical thresholds false-positive.

## Decision

Namespace memory trend threshold at 500 KiB/day (5× the container 100 KiB/day).

## Consequences

Fewer false trend alerts at namespace level. Different tuning per scope.

## References

- [docs/archive/requirements.md](docs/archive/requirements.md)
