# ADR-0200: Kafka consumer session tuning for slow CSV processing

## Status

Accepted

## Context

CSV processing can take 30–120 seconds for large files. Default Kafka session timeout (10s) causes constant rebalances. The consumer must stay in the group while processing without blocking partition assignment for the entire cluster.

## Decision

Consumer configuration in `internal/kafka/consumer.go`:

| Setting | Value | Rationale |
|---------|-------|-----------|
| `session.timeout.ms` | 120000 | Tolerate ~90s processing |
| `heartbeat.interval.ms` | 30000 | Keep session alive during work |
| `allow.auto.create.topics` | false | Prevent accidental topic creation |

Parallel workers use per-partition mutex ([ADR-0154](0154-partition-scoped-worker-pool.md)) with job channel buffer `workers×2`.

## Consequences

- Slow rebalance detection (up to 2 minutes to detect dead consumer) — acceptable trade-off for stable processing.
- Parallel workers bounded by channel buffer — backpressure when all slots full.

## Alternatives Considered

### Default 10s timeout

Constant rebalances during CSV processing. Rejected.

### Pause/resume consumer

Complex state management across worker pool. Rejected.

### Separate download/process stages

Over-engineering for current pipeline. Rejected.

## Related Decisions

- [ADR-0089](0089-manual-kafka-commit-after-success.md): Manual commit after successful processing.
- [ADR-0154](0154-partition-scoped-worker-pool.md): Partition-scoped parallel workers.

## References

- [internal/kafka/consumer.go](../../internal/kafka/consumer.go)
