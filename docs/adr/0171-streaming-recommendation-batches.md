# ADR-0171: Streaming recommendation batches for memory bounding

## Status

Accepted

## Context

The recommendation engine processes all containers for an org in a single invocation. At 50k+ containers, loading all historical data and computing recommendations simultaneously would exceed memory limits. GORM's `FindInBatches` loads all matching rows before calling the callback.

## Decision

`RecommendWorkloadsStreaming` in `internal/engine/recommend_all.go` first buffers all digest rows for a cluster via `loadDigestRows()` (in a transaction with the ingest statement timeout), then processes containers in batches of 500 (`streamBatchSize`). Each batch:

1. Reads container history from the in-memory digest buffer (ordered by container key + date).
2. Computes recommendations for containers in the batch.
3. Emits results via pgx batch queue (`maxPgxBatchQueue = 2000`).

The digest read was originally a streaming cursor consumed row-by-row. This was changed to a buffered read ([#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)) because long-running recommendation writes caused TCP backpressure that blocked cursor consumption, triggering `statement_timeout` failures on large clusters. Buffering all rows upfront and releasing the DB connection before processing eliminates this failure mode.

Peak memory is O(total_containers × history_days) for the digest buffer plus O(batch_size × terms × engines) for the write batches.

## Alternatives Considered

### Single-pass all containers

OOM at scale on large clusters.

### GORM FindInBatches

Loads entire result set into memory before invoking the callback, defeating the purpose.

### Per-container processing

Too many round trips—orders of magnitude slower on large fleets.

### External stream processor (Kafka Streams)

Over-engineering for a batch job tied to ingest completion.

## Consequences

- Write-side memory bounded by `streamBatchSize` regardless of cluster size.
- Read-side memory scales with cluster size (all digest rows buffered), trading memory for reliability — the streaming cursor was susceptible to `statement_timeout` when write backpressure stalled consumption ([#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)).
- Batch boundaries mean recommendations are computed independently per batch (no cross-container correlation needed for current algorithms).
- pgx batch queue provides write pipelining without unbounded buffering.
- Adds complexity: emit callback, batch cursor management, partial-failure handling within batches.

## Related Decisions

- [ADR-0289](0289-defer-org-metadata-refresh-end-of-reconcile.md): Defer org metadata refresh to end of streaming cycle.
- [ADR-0003](0003-read-once-compute-n-terms.md): Read-once SQL scan strategy.
- [ADR-0001](0001-native-engine-over-kruize.md): Native engine architecture.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go) — `streamBatchSize`, `maxPgxBatchQueue`, `RecommendWorkloadsStreaming`
- [cmd/bench/main.go](../../cmd/bench/main.go) — scale benchmark CLI for regression testing
