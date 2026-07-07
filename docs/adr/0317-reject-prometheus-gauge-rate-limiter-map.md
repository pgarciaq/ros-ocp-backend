# ADR-0317: Reject Prometheus gauge for rate limiter map size (PERF-10)

## Status

Rejected

## Context

The proposal was to expose the rate limiter's internal map size as a Prometheus gauge for observability (detecting memory growth from accumulated client keys).

## Decision

Won't Fix. Echo's `RateLimiterMemoryStore` has no exposed size accessor for its internal map. Implementing this would require either:

1. Replacing Echo's store with a custom implementation (high maintenance burden), or
2. Using `reflect` to access unexported fields (fragile across Echo upgrades).

Both approaches are disproportionate effort for an observability-only gain. The rate limiter map is bounded by unique client IPs × TTL window, which is self-limiting in practice.

## Consequences

No rate limiter size metric. Monitor via process-level memory metrics if needed.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PERF-10
- [#214](https://github.com/pgarciaq/ros-ocp-backend/issues/214)
