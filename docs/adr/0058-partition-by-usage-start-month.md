# ADR-0058: Partition usage/history/quality by usage_start / month

## Status

Accepted

## Context

Need efficient retention (DROP PARTITION) without scanning and deleting individual rows.

## Decision

Monthly RANGE partitions on `usage_start` or `bucket_date`.

## Consequences

O(1) retention via partition drop. Requires partition pre-creation. Standard PostgreSQL pattern.

## References

- [internal/engine/retention.go](internal/engine/retention.go)
