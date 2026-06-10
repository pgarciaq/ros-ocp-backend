# ADR-0154: Use partition-scoped worker pool with ordering preserved per partition

## Status

Accepted

## Context

Unbounded parallel handlers reorder commits within a partition, violating Kafka semantics.

## Decision

Per-partition mutex ensures one in-flight handler per partition; N workers across partitions.

## Consequences

Parallelism across partitions. Ordering within partition. Configurable worker count.

## References

- [internal/kafka/consumer.go](internal/kafka/consumer.go)
