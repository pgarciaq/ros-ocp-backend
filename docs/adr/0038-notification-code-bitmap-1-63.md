# ADR-0038: Use notification code bitmap (1–63) for deduplication

## Status

Accepted

## Context

Need O(1) merge of notification codes in hot path (many containers per cluster).

## Decision

Use SMALLINT bitmap operations for dedup; codes numbered 1–63.

## Consequences

Fast merge. Limited to 63 distinct codes. Sufficient for foreseeable taxonomy.

## References

- [internal/engine/notifications_bitmap.go](internal/engine/notifications_bitmap.go)
