# ADR-0082: Use internal POST /recalculate-savings async 202

## Status

Accepted

## Context

Synchronous full re-ingest on cost model change would block the caller.

## Decision

Async endpoint returns 202; recalculation runs in background.

## Consequences

Non-blocking. Eventual consistency on savings. Client can poll for completion.

## References

- [internal/api/handlers_savings_recalculate.go](internal/api/handlers_savings_recalculate.go)
