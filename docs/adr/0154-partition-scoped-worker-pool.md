# ADR-0154: Use partition-scoped worker pool with ordering preserved per partition

## Status

Accepted

Amended (2026-11-04) — The per-partition **mutex** delivered exclusion, not order ([#756](https://github.com/pgarciaq/ros-ocp-backend/issues/756) / V15-06): workers dequeued from a shared FIFO channel *before* competing for the mutex, and Go mutexes make no FIFO wake-order guarantee, so a higher offset could execute and commit while a lower one was still waiting. Replaced by per-partition **ordered queues** (see the amendment below).

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

**Amendment (#756):** The mutex delivered the Consequences' *parallelism* half but not its *ordering* half — mutual exclusion is not FIFO. `consumeMessagesParallelUntilCancelled` now routes each partition's messages to an ordered queue (`partitionQueueSize`-buffered channel) served by a dedicated goroutine: handler execution and commits within a partition follow offset order **by construction** (the handler tail, the retry path, and panic-recovery commit-on-skip all run in the partition's goroutine). Cross-partition parallelism is bounded by a semaphore sized to the DB-connection budget — `ROS_KAFKA_WORKERS` now caps concurrent partition handlers rather than naming a fixed worker pool (each in-flight handler still holds one DB connection, per the `ManifestDownloadWorkers × KafkaWorkers ≤ DBMaxConns − 2` constraint). The global commit mutex in `CommitMessage` is removed: per-partition commits are serial by construction, and cross-partition commits rely on librdkafka's documented thread-safety ("the calling application may call any of the API functions from any of its own threads at any time" — librdkafka INTRODUCTION.md). **Rebalance policy (unchanged from the prior design):** no rebalance callback — messages already fetched for a revoked partition are processed to completion (at-least-once), which is safe because ingestion upserts are idempotent replacement writes; a replayed report converges to the same row. Regressions: `internal/kafka/consumer_partition_order_test.go` (offset-order stress trace, blocked-lower-offset, cross-partition concurrency, cap, drain, panic-continuation, and the mock-cluster broker-watermark proof).

## References

- [internal/kafka/consumer.go](internal/kafka/consumer.go)
