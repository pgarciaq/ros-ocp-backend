# ADR-0311: Reject pooling gzip.Writer in Echo middleware (PROF-1)

## Status

Rejected

## Context

Live profiling (2026-07-05) showed `gzip.Writer` allocations in the Echo compression middleware. The proposal was to add a custom `sync.Pool`-based middleware to eliminate these allocations.

## Decision

Won't Fix. Echo v4.15.2 already pools `gzip.Writer` via `sync.Pool` internally (~98% reuse). The residual 1.9% allocations come from Go's GC clearing the pool between request bursts — inherent to `sync.Pool` design. A channel-based pool would save ~7MB/30s (0.24MB/s) but requires maintaining a custom middleware for <2% gain.

## Consequences

Accept the ~2% residual allocation overhead from `sync.Pool` GC clearing. No custom middleware to maintain.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PROF-1
- [Profiling session 2026-07-05](../performance/profiling-2026-07-05.md)
