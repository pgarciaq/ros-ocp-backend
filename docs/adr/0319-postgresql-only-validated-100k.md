# ADR-0319: PostgreSQL-only architecture validated at 100K containers

## Status

Accepted

## Context

The native engine was designed with PostgreSQL as the sole data store ([ADR-0134](0134-postgresql-16-target.md)), using daily digest tables ([ADR-0045](0045-daily-digest-tables-not-raw-metrics.md)) and integer-first arithmetic ([ADR-0295](0295-integer-first-architecture.md)) to keep resource usage low.

The 100K comprehensive benchmark (July 2026) is the first validation at a scale matching the largest production SaaS tenants (~100K containers). For context, the [CNCF 2025 Annual Survey](https://www.cncf.io/reports/cncf-annual-survey-2025/) reports a median of ~370 containers per Kubernetes cluster across the industry; 100K containers represents approximately 270× this baseline, confirming that PostgreSQL handles workloads far exceeding typical production deployments. Results:

- **84,133 distinct containers** across 31 days of data
- **~2.6 million digests** created
- **Database size: 3.5 GB** after full ingestion and recommendation generation
- **Processing time: ~87 minutes** wall time on a single replica
- **Zero restarts**, stable memory usage throughout

Prior to this benchmark, the largest validated scale was 20K containers (2.2 GB DB). The 5× scale increase produced a proportional resource increase with no architectural bottlenecks.

## Decision

PostgreSQL 16 remains the sole data store. No additional caching layer (Redis/Memcached), time-series database (TimescaleDB), or analytical engine (Trino) is needed for the ROS processor at 100K scale.

The digest-based data model and integer arithmetic keep the database compact enough that PostgreSQL B-tree indexes and range-partitioned tables handle both ingestion throughput and API query latency.

## Alternatives Considered

### Add Redis for digest caching

Digests are written once per container-day and read during recommendation generation in the same transaction. A cache would add network round-trips without reducing DB load for this write-heavy, sequential-read pattern.

### Migrate to TimescaleDB for time-series operations

The digest model already pre-aggregates per container-day. TimescaleDB's continuous aggregates and chunk-based storage add complexity for a workload that doesn't need sub-day granularity or real-time aggregation.

### Use Trino for batch recommendation generation

Trino excels at analytical queries over Parquet on S3. The ROS processor's recommendations are computed inline during ingestion ([ADR-0259](0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md)), not as batch post-processing. Adding Trino would require a fundamentally different pipeline architecture.

## Consequences

- Database size scales roughly linearly: ~35 bytes per digest row, ~3.5 GB at 100K containers × 31 days.
- PostgreSQL `max_connections` and connection pool sizing become the primary scaling constraints ([ADR-0320](0320-db-pool-arithmetic-primary-scaling-constraint.md)).
- Operators need only PostgreSQL expertise — no additional database technologies to operate.
- If future requirements (>500K containers, sub-second API responses at 1M containers) exceed PostgreSQL's capacity, this decision should be revisited.

## Related Decisions

- [ADR-0045](0045-daily-digest-tables-not-raw-metrics.md): Daily digest tables.
- [ADR-0134](0134-postgresql-16-target.md): PostgreSQL 16 target.
- [ADR-0295](0295-integer-first-architecture.md): Integer-first architecture.
- [ADR-0259](0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md): Synchronous ingest-time engine.

## References

- [Scale Benchmark Report — 100K comprehensive](../../docs-site/operations/scale-benchmark-report.md)
