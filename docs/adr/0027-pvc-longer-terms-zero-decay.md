# ADR-0027: Use longer PVC terms (7/30/90d) with zero decay

## Status

Accepted

## Context

Storage growth is slow and linear; container-like 1/7/15d windows with decay are too short.

## Decision

PVC plugin uses 7/30/90 day windows with no exponential decay.

## Consequences

Captures storage trends over longer horizons. No recency bias for storage (appropriate given linear growth).

## References

- [internal/plugins/pvc/plugin.go](internal/plugins/pvc/plugin.go)
