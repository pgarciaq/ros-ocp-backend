# ADR-0091: Use incremental digest flush during streaming CSV parse

## Status

Accepted

## Context

Holding full cluster-day digest map until EOF causes OOM on large clusters.

## Decision

Flush digest groups every `ROS_INGEST_FLUSH_BATCH_SIZE` (default 1000) during streaming parse.

## Consequences

Bounded memory. Multiple DB round-trips per file. Acceptable latency trade-off.

## References

- [internal/ingestion/pipeline_stream.go](internal/ingestion/pipeline_stream.go)
