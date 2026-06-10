# ADR-0035: Use business-hours as nested block, not separate API rows

## Status

Accepted

## Context

Separate list resources for BH would double API surface and client complexity.

## Decision

BH recommendations nested as `business_hours` block inside container/namespace responses.

## Consequences

Single API call returns both perspectives. Slightly more complex response schema. No separate pagination.

## References

- [docs/features/business-hours.md](docs/features/business-hours.md)
