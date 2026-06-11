# ADR-0038: Use notification code bitmap (1–63) for deduplication

## Status

Accepted

## Context

Need O(1) merge of notification codes in hot path (many containers per cluster).

## Decision

Use SMALLINT bitmap operations for dedup; codes numbered 1–63 (6-bit range fits PostgreSQL `SMALLINT[]` and enables efficient array overlap queries).

## Alternatives Considered

### JSONB array of codes
Slow aggregation queries when computing fleet-wide notification counts; GIN indexing less efficient than native array overlap for numeric codes.

### Separate notifications table
JOIN overhead on every container list query; multi-code containers explode row count (one row per code) and complicate pagination.

## Consequences

Fast merge. Limited to 63 distinct codes. Sufficient for foreseeable taxonomy.

## References

- [internal/engine/notifications_bitmap.go](internal/engine/notifications_bitmap.go)
