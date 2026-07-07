# ADR-0316: Defer rate limiter mutex sharding (PERF-09)

## Status

Deferred

## Context

Echo's built-in `RateLimiterMemoryStore` uses a single `sync.Mutex` for all rate limiter lookups. Under high concurrency, this could become a contention point.

## Decision

Deferred. Monitor only. The current deployment sees well below the threshold where mutex contention becomes measurable (~500 sustained req/s). Sharding the rate limiter store would require replacing Echo's built-in implementation with a custom sharded store.

## Trigger for Revisit

Implement sharded rate limiting if p99 request latency degrades and profiling shows `sync.Mutex` contention in the rate limiter above 500 sustained req/s.

## Consequences

Accept current single-mutex design. No custom rate limiter store to maintain.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PERF-09
- [#210](https://github.com/pgarciaq/ros-ocp-backend/issues/210)
