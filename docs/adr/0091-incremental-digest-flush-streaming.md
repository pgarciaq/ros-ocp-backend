# ADR-0091: Use incremental digest flush during streaming CSV parse

## Status

Accepted

## Context

Holding full cluster-day digest map until EOF causes OOM on large clusters.

## Decision

Flush digest groups every `ROS_INGEST_FLUSH_BATCH_SIZE` (default 1000) during streaming parse.

## Alternatives Considered

### Buffer entire cluster-day in memory before flush
The original approach accumulated all digest groups until EOF; clusters with 10k+ containers per day exceeded processor memory limits (2–4 GiB) during concurrent partition processing, triggering OOM kills mid-ingest and leaving partial data committed.

### Spool digests to disk (temp files)
Writing intermediate digest state to PVC would cap RAM usage, but adds I/O latency and cleanup complexity on ephemeral processor pods; batch DB upserts every 1000 groups achieve similar memory bounds with simpler failure recovery.

### One transaction per entire CSV file
A single large transaction minimizes commit overhead but holds row locks for minutes on digest tables, blocks concurrent reads, and rolls back all progress if the final row fails validation—unacceptable for multi-hour ingest files.

## Consequences

Bounded memory. Multiple DB round-trips per file. Acceptable latency trade-off.

## References

- [internal/ingestion/pipeline_stream.go](internal/ingestion/pipeline_stream.go)
