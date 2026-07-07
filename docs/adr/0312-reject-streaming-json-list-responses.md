# ADR-0312: Reject streaming JSON for list responses (PROF-4)

## Status

Rejected

## Context

Live profiling showed that list handler responses are fully materialized in memory before serialization. The proposal was to stream JSON directly to the HTTP response to reduce peak memory.

## Decision

Won't Fix. The handler pipeline requires full materialization for three reasons:

1. Enrichment passes (savings calculation, projection filtering) need the complete result set.
2. JSON response format requires `meta.count` before the `data` array.
3. Paginated results are bounded (10–100 items per page) and small after PROF-2 (manual scanning) and PROF-3 (pre-allocation).

CSV export already streams via `io.Pipe`, which is the appropriate path for large unbounded exports.

## Consequences

List API responses continue to buffer in memory. Peak memory per request is bounded by page size × row size (~100 × 2KB = 200KB), which is acceptable.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PROF-4
- [Profiling session 2026-07-05](../performance/profiling-2026-07-05.md)
