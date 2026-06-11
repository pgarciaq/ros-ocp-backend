# ADR-0089: Use manual Kafka commit after successful processing

## Status

Accepted

## Context

Auto-commit advances offsets before DB writes complete, losing messages on crash.

## Decision

Disable auto-commit; explicit CommitMessage only after successful processing.

## Alternatives Considered

### Kafka auto-commit (enable.auto.commit=true)
Auto-commit advances consumer offsets on a timer regardless of DB write status; a processor crash after partial ingest but before commit confirmation loses messages permanently (at-most-once), which is unacceptable for billing-adjacent recommendation data.

### Transactional outbox pattern
Writing Kafka offsets and DB rows in one atomic outbox transaction gives exactly-once semantics, but requires a separate outbox table, relay process, and dedup logic—operational overhead disproportionate when at-least-once plus idempotent upserts already suffice in `consumer.go`.

### Idempotent consumer only (no manual commit, rely on dedup table)
Tracking processed `(topic, partition, offset)` in a dedup table allows auto-commit, but duplicates every message key check on replay and adds another hot table; manual commit after successful pipeline completion is simpler and matches confluent best practice for data pipelines.

## Consequences

At-least-once delivery guaranteed. Must handle duplicate messages idempotently.

## References

- [internal/kafka/consumer.go](internal/kafka/consumer.go)
