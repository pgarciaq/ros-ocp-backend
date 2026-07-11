# ADR-0318: Horizontal scaling via Kafka consumer groups

## Status

Accepted

## Context

The 100K benchmark (July 2026) demonstrated that a single ROS processor replica handles ~84,000 containers in ~87 minutes wall time. However, for production tenants exceeding this or requiring lower latency, operators need confidence that adding replicas works without coordination overhead or data corruption.

The existing architecture already has several properties that support horizontal scaling:

1. **Kafka consumer groups** ([ADR-0154](0154-partition-scoped-worker-pool.md)): all replicas join the same `KAFKA_CONSUMER_GROUP_ID`, and Kafka distributes partitions across members.
2. **Message keying**: the Koku listener keys messages by `cluster_uuid`, so all data for a given cluster routes to the same partition (and therefore the same replica).
3. **Idempotent database operations**: digest upserts (`ON CONFLICT DO UPDATE`) and recommendation writes are safe to replay or run concurrently on different clusters.
4. **Per-partition mutexes**: internal parallel workers use mutexes per Kafka partition to maintain message ordering within a partition ([ADR-0154](0154-partition-scoped-worker-pool.md)).
5. **Manual offset commit** ([ADR-0089](0089-manual-kafka-commit-after-success.md)): offsets are committed only after successful processing, preventing data loss on replica failure.

The question is whether to formalize this as the scaling strategy or invest in a custom sharding/coordination layer.

## Decision

Horizontal scaling uses Kafka consumer group rebalancing — no custom coordination layer. Operators scale by increasing replicas with the same `KAFKA_CONSUMER_GROUP_ID` and adjusting `ROS_DB_MAX_CONNS` so that `replicas × max_conns` stays within PostgreSQL `max_connections`.

The Kafka topic must have at least as many partitions as desired replicas; excess replicas beyond the partition count sit idle.

## Alternatives Considered

### Custom sharding by org_id or cluster_uuid

Application-level routing adds complexity (consistent hashing, rebalance logic, state migration) for minimal benefit over Kafka's built-in partition assignment. Kafka already provides exactly the partition affinity needed.

### Shared-nothing with separate databases per replica

Eliminates DB contention but prevents cross-replica queries (e.g., fleet-summary API). The PostgreSQL connection pool is not the bottleneck at current scale.

### Stateless workers with external queue (Redis, SQS)

Adds an infrastructure dependency when Kafka already provides the work distribution semantics needed.

## Consequences

- Total DB connections across all replicas = `replicas × ROS_DB_MAX_CONNS`. Operators must verify this against PostgreSQL `max_connections` ([ADR-0320](0320-db-pool-arithmetic-primary-scaling-constraint.md)).
- Kafka `session.timeout.ms` (120s default) must exceed the longest single-message processing time to prevent unnecessary rebalances.
- Adding replicas beyond the topic partition count has no effect; operators should ensure `partitions >= replicas`.
- API replicas scale independently — they share the same database and have no Kafka coupling.

## Related Decisions

- [ADR-0154](0154-partition-scoped-worker-pool.md): Partition-scoped worker pool.
- [ADR-0089](0089-manual-kafka-commit-after-success.md): Manual Kafka commit.
- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): Connection pool tuning.
- [ADR-0320](0320-db-pool-arithmetic-primary-scaling-constraint.md): DB pool arithmetic constraint.

## References

- [Scale Benchmark Report — 100K comprehensive](../../docs-site/operations/scale-benchmark-report.md)
- [Performance and Scalability — Horizontal Scaling](../../docs-site/operations/performance-and-scalability.md)
- [internal/kafka/consumer.go](../../internal/kafka/consumer.go)
