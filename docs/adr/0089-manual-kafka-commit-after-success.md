# ADR-0089: Use manual Kafka commit after successful processing

## Status

Accepted

## Context

Auto-commit advances offsets before DB writes complete, losing messages on crash.

## Decision

Disable auto-commit; explicit CommitMessage only after successful processing.

## Consequences

At-least-once delivery guaranteed. Must handle duplicate messages idempotently.

## References

- [internal/kafka/consumer.go](internal/kafka/consumer.go)
