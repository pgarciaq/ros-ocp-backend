# ADR-0321: Raise default DBMaxConns from 5 to 10

## Status

Accepted

## Context

The `defaultDBMaxConns` constant in `config.go` was set to 5. With default worker settings (`ManifestDownloadWorkers=2`, `KafkaWorkers=3`), the pool constraint ([ADR-0320](0320-db-pool-arithmetic-primary-scaling-constraint.md)) requires:

```
ManifestDownloadWorkers × KafkaWorkers + 2 = 2 × 3 + 2 = 8
```

A pool of 5 connections cannot satisfy a worst-case demand of 8, leading to potential `context deadline exceeded` errors during concurrent ingestion. This was identified during the 100K benchmark analysis.

The documentation (`configuration.md`) already stated the default as 10, but the code had 5 — an inconsistency that masked the bug.

## Decision

Raise `defaultDBMaxConns` from 5 to 10 in `internal/config/config.go`.

The value 10 provides:
- Headroom for the default worker configuration (8 needed, 10 available)
- Room for 2 concurrent API queries or health checks alongside full ingestion load
- A safe single-replica default that leaves 90 connections available for other PostgreSQL clients (out of default `max_connections=100`)

Add a startup validation warning when the pool constraint is violated, regardless of whether the default or an explicit value is used.

## Alternatives Considered

### Raise to 8 (exact minimum)

Leaves zero headroom for API queries during peak ingestion. Connection acquire timeouts would still occur under mixed API + ingest load.

### Raise to 20

Unnecessarily aggressive for a single-replica default. With 3 replicas, this would consume 60 of PostgreSQL's default 100 connections, leaving little room for admin tools or monitoring.

### Keep at 5 and lower worker defaults

Would degrade ingestion performance to avoid the configuration issue. Penalizes throughput to mask a defaults bug.

## Consequences

- Single-replica deployments use 10 connections by default instead of 5.
- Multi-replica deployments with default settings use `replicas × 10` connections.
- Operators who previously set `ROS_DB_MAX_CONNS=5` explicitly will see the startup warning and should raise the value.
- The `config_db_pool_test.go` test must be updated to expect the new default.

## Related Decisions

- [ADR-0320](0320-db-pool-arithmetic-primary-scaling-constraint.md): DB pool arithmetic constraint.
- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): Connection pool tuning surface.

## References

- [internal/config/config.go](../../internal/config/config.go)
- [internal/config/config_db_pool_test.go](../../internal/config/config_db_pool_test.go)
