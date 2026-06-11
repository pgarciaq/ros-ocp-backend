# ADR-0171: Streaming recommendation batches for memory bounding

## Status

Accepted

## Context

The recommendation engine processes all containers for an org in a single invocation. At 50k+ containers, loading all historical data and computing recommendations simultaneously would exceed memory limits. GORM's `FindInBatches` loads all matching rows before calling the callback.

## Decision

`RecommendWorkloadsStreaming` in `internal/engine/recommend_all.go` processes containers in batches of 500 (`streamBatchSize`). Each batch:

1. Loads container history from a row-by-row digest scan (ORDER BY container key).
2. Computes recommendations for containers in the batch.
3. Emits results via pgx batch queue (`maxPgxBatchQueue = 500`).

Peak memory is O(batch_size × history_days × terms × engines) rather than O(total_containers × history_days).

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

- Peak memory bounded regardless of cluster size.
- Batch boundaries mean recommendations are computed independently per batch (no cross-container correlation needed for current algorithms).
- pgx batch queue provides write pipelining without unbounded buffering.
- Adds complexity: emit callback, batch cursor management, partial-failure handling within batches.

## Related Decisions

- [ADR-0003](0003-read-once-compute-n-terms.md): Read-once SQL scan strategy.
- [ADR-0001](0001-native-engine-over-kruize.md): Native engine architecture.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go) — `streamBatchSize`, `maxPgxBatchQueue`, `RecommendWorkloadsStreaming`
- [cmd/bench/main.go](../../cmd/bench/main.go) — scale benchmark CLI for regression testing
