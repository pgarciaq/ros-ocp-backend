# ADR-0018: Prefer operator node_allocatable over 0.93× request fallback

## Status

Accepted

## Context

Operator exposes true allocatable; estimating from capacity is inaccurate.

## Decision

Use `node_allocatable_*` from operator CSV when available; fall back to 93% of capacity.

## Consequences

More accurate sizing when operator data present. Graceful degradation without it.

## References

- [internal/ingestion/node_digest.go](internal/ingestion/node_digest.go)
