# ADR-0045: Use daily digest tables, not raw metrics in PostgreSQL

## Status

Accepted

## Context

Storing all hourly intervals in PG would be enormous; S3 retains source CSVs.

## Decision

Pre-aggregate into daily digest rows. Raw data stays in S3.

## Alternatives Considered

### Store all hourly intervals in PostgreSQL
Retaining every operator hourly sample in PG enables arbitrary re-aggregation but expands storage ~100× versus daily digests (96 rows/day × containers × clusters × retention months), blowing cost-onprem PVC budgets and slowing list queries that touch metric tables.

### External time-series database (Prometheus, Influx)
Offloading raw metrics to a dedicated TSDB would scale writes, but cost-onprem already runs PostgreSQL + Kafka + Koku with no TSDB operator; adding another datastore doubles operational burden for a team sized for a single relational backend.

### On-the-fly aggregation from S3 CSVs at recommendation time
Re-reading archived CSVs from MinIO/S3 per ingest cycle avoids PG storage but adds seconds of latency per cluster (network + parse) and duplicates work already done once during streaming ingest in `pipeline_stream.go`.

## Consequences

Small PG footprint. Trade-off: can't re-derive sub-daily stats from PG alone.

## References

- [migrations/000025+](migrations/000025+)
- [internal/ingestion/digest.go](internal/ingestion/digest.go)
