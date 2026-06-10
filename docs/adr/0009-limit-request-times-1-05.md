# ADR-0009: Use limit = request × 1.05 for containers

## Status

Accepted

## Context

Equal limit/request leaves no burst headroom; large gaps waste quota.

## Decision

Set limit at 5% above request, consistent with legacy Kruize UX.

## Consequences

Minimal burst headroom. Consistent with user expectations from Kruize migration.

## References

- [internal/engine/types.go](internal/engine/types.go)
