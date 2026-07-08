# ADR-0093: Use chunked pgx batches (max 500 queued)

## Status

Accepted — **Updated:** batch cap increased from 500 to 2000 in [#257](https://github.com/pgarciaq/ros-ocp-backend/issues/257). The chunking approach remains; only the threshold changed.

## Context

Single giant batch transactions consume excessive memory.

## Decision

Cap pgx batch queue at a fixed threshold; flush and continue. Originally 500, increased to 2000 after profiling showed the larger batch size reduces round-trips by ~4× with negligible memory impact (~1.4 MiB per batch at 44 params per row).

## Alternatives Considered

### Unbounded pgx batch for entire manifest
Queueing all upserts in one pgx batch minimizes round-trips, but large clusters (50k+ digest rows) allocate proportional send buffers in process memory—concurrent partition workers OOM'd the ros-processor pod at 2 GiB limit during soak tests.

### COPY-only bulk load path
PostgreSQL `COPY` is fastest for blind inserts, but digest upserts need `ON CONFLICT DO UPDATE` with `RETURNING` to sync `org_container_keys`; COPY cannot express conflict resolution without staging tables and a second merge pass.

### Row-by-row INSERT with prepared statements
Individual executes simplify error handling per row, but 100× round-trip latency on a 10k-row manifest exceeds the 120s `statement_timeout` configured in ADR-0092, blocking the Kafka consumer.

## Consequences

Bounded memory per batch. More round-trips. Acceptable for ingest throughput.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
