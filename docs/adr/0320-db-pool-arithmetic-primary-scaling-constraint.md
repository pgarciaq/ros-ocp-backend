# ADR-0320: DB connection pool arithmetic as primary scaling constraint

## Status

Accepted

## Context

The ROS processor uses a shared pgxpool ([ADR-0128](0128-unify-gorm-pgxpool-stdlib.md), [ADR-0240](0240-connection-pool-timeout-tuning-surface.md)) with `ROS_DB_MAX_CONNS` controlling the per-process pool size.

During parallel ingestion, each in-flight CSV file download holds a DB connection for the duration of its digest upsert transaction. The parallel ingestion pipeline uses `ManifestDownloadWorkers` concurrent downloads per Kafka message, and `KafkaWorkers` messages can be processed concurrently across partitions.

The worst-case concurrent connection demand is therefore:

```
ManifestDownloadWorkers × KafkaWorkers + 2 (reserved for recommendations and health checks)
```

With defaults `ManifestDownloadWorkers=2` and `KafkaWorkers=3`, this is `2 × 3 + 2 = 8`. The previous `defaultDBMaxConns=5` was insufficient, causing potential pool exhaustion under concurrent ingestion load.

In multi-replica deployments, total connections across all replicas = `replicas × ROS_DB_MAX_CONNS`. This must not exceed PostgreSQL's `max_connections` (default 100).

## Decision

Formalize the constraint as an invariant:

```
ManifestDownloadWorkers × KafkaWorkers <= DBMaxConns - 2
```

Document this in `config.go` (already present as a comment on `ManifestDownloadWorkers`) and enforce it with a startup warning log when violated.

Raise `defaultDBMaxConns` from 5 to 10 to satisfy the invariant with default worker settings and provide headroom for API queries ([ADR-0321](0321-raise-default-dbmaxconns-5-to-10.md)).

## Alternatives Considered

### Dynamic pool sizing based on worker configuration

Automatically setting `DBMaxConns = ManifestDownloadWorkers × KafkaWorkers + 4` removes the manual coordination but makes the pool size unpredictable and harder to capacity-plan against PostgreSQL `max_connections`.

### Reduce default workers to fit within pool size of 5

Setting `ManifestDownloadWorkers=1` and `KafkaWorkers=2` would fit in a pool of 5 but sacrifices ingestion throughput — particularly impactful at 100K+ scale.

### Per-operation connection pools (ingest vs API vs health)

Separate pools provide better isolation but increase total connection count and complicate configuration.

## Consequences

- Operators scaling replicas must verify `replicas × ROS_DB_MAX_CONNS <= max_connections - headroom`.
- The startup warning catches misconfigurations before they manifest as pool timeout errors under load.
- The constraint is documented in code (comment on `ManifestDownloadWorkers`), configuration docs, and this ADR — three places to maintain consistency.

## Related Decisions

- [ADR-0128](0128-unify-gorm-pgxpool-stdlib.md): Unified pgxpool.
- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): Connection pool tuning surface.
- [ADR-0154](0154-partition-scoped-worker-pool.md): Partition-scoped worker pool.
- [ADR-0321](0321-raise-default-dbmaxconns-5-to-10.md): Raise default DBMaxConns.

## References

- [internal/config/config.go](../../internal/config/config.go) (constraint comment on `ManifestDownloadWorkers`)
- [internal/services/parallel_ingest.go](../../internal/services/parallel_ingest.go)
