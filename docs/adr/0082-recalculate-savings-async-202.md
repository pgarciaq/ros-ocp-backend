# ADR-0082: Use internal POST /recalculate-savings async 202

## Status

Accepted

## Context

Synchronous full re-ingest on cost model change would block the caller.

## Decision

Async endpoint returns 202; recalculation runs in background.

## Alternatives Considered

### Synchronous recalculate on POST
Full-org savings recompute exceeds HTTP timeout at scale (large tenants with millions of digest rows).

### Fire-and-forget with no completion signal
Callers cannot tell when savings are fresh; Masu cost-model webhooks would trigger redundant recalcs without idempotency cues.

## Consequences

Non-blocking. Eventual consistency on savings. Client can poll `/internal/recalculate-savings/status` or watch `rosocp_savings_recalc_*` Prometheus metrics for completion and failure rates.

## References

- [internal/api/handlers_savings_recalculate.go](internal/api/handlers_savings_recalculate.go)
