# ADR-0131: Use housekeeper batched PK deletes (5000 rows) for source cleanup

## Status

Accepted

## Context

Single DELETE CASCADE on million-row tenants causes lock contention and timeout.

## Decision

Batch deletes in 5000-row chunks with brief sleep between batches.

## Consequences

Bounded lock time. Longer total cleanup. No table-level locks.

## References

- [internal/services/housekeeper/sourcesCleaner.go](internal/services/housekeeper/sourcesCleaner.go)
