# ADR-0126: Use forward-only fallback when reship fails after max retries

## Status

Accepted

## Context

Blocking BH settings forever on historical gaps is worse than partial data.

## Decision

After max reship retries, fall forward with partial BH data.

## Consequences

BH settings always eventually apply. Historical gaps possible. Notification flags gap.

## References

- [internal/reship/service.go](internal/reship/service.go)
