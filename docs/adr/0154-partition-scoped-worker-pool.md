# ADR-0154: Use partition-scoped worker pool with ordering preserved per partition

## Status

Accepted

## Context

Unbounded parallel handlers reorder commits within a partition, violating Kafka semantics.

## Decision

Per-partition mutex ensures one in-flight handler per partition; N workers across partitions.

## Alternatives Considered

### Unbounded goroutine per message
Spawning a goroutine for every consumed message maximizes throughput, but burst manifests (post-outage backlog) create thousands of concurrent handlers—each holding open DB transactions and digest maps—OOM'ing the processor pod under load tests.

### Single consumer thread (serial processing)
One goroutine eliminates ordering concerns entirely, but underutilizes multi-core nodes; ingest lag scaled linearly with partition count when tested on 8-core ros-processor deployments.

### Separate consumer group per partition
Assigning one consumer instance per partition preserves ordering without mutexes, but Kafka rebalance storms on pod restarts redistribute dozens of partitions simultaneously, causing duplicate processing windows and consumer lag spikes during rolling updates in `consumer.go`.

## Consequences

Parallelism across partitions. Ordering within partition. Configurable worker count.

## References

- [internal/kafka/consumer.go](internal/kafka/consumer.go)
