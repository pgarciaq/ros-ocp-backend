# ADR-0038: Use notification code bitmap (1–63) for deduplication

## Status

Accepted

## Context

Need O(1) merge of notification codes in hot path (many containers per cluster).

## Decision

Use SMALLINT bitmap operations for dedup; codes numbered 1–63 (6-bit range fits PostgreSQL `SMALLINT[]` and enables efficient array overlap queries).

Codes are **persisted as PostgreSQL `SMALLINT[]`**, not JSONB (incorporates former ADR-0039). The array type enables indexable `@>` containment and `&&` overlap filtering on list queries.

## Alternatives Considered

### JSONB array of codes
Slow aggregation queries when computing fleet-wide notification counts; GIN indexing less efficient than native array overlap for numeric codes.

### Separate notifications table
JOIN overhead on every container list query; multi-code containers explode row count (one row per code) and complicate pagination.

### Comma-separated TEXT
Cannot use PostgreSQL `&&` overlap operator; requires string parsing or `LIKE` patterns that defeat indexing.

### Separate row per notification code
Row explosion for containers with many codes; pagination and savings rollups double-count entities.

## Consequences

Fast merge. Indexable array containment. Compact storage. GIN index support. Limited to 63 distinct codes in the original bitmap design (see status update below for extensions). Not as flexible as JSONB for per-code metadata.

## References

- [internal/engine/notifications_bitmap.go](../../internal/engine/notifications_bitmap.go)
- [docs/architecture/database-conventions.md](../architecture/database-conventions.md)

## Status Update (2026-06)

Notification codes are no longer a contiguous 1–N range. Codes 36 (GPU time-sharing, [ADR-0198](0198-gpu-time-slicing-notification-code-36-savings-formula.md)), 64 (VM power schedule), 67–69 (VM storage tiering), 74 (GPU MIG downsizing), and 76 (node fleet consolidation, [ADR-0194](0194-node-consolidation-precedence-pod-scheduling-gate.md)) extend beyond the original 1–35 range. Plugin filters must handle non-contiguous code sets.
