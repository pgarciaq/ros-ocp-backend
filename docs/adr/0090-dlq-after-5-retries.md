# ADR-0090: Use DLQ after 5 transient retries with forensic headers

## Status

Accepted

## Context

Infinite retry blocks partitions; poison messages need forensics without stalling consumers.

## Decision

After 5 retries (tracked via `X-Retry-Count` header), produce to DLQ with forensic metadata.

## Consequences

Partitions unblocked after max retries. Forensic data preserved. Requires DLQ monitoring.

## References

- [internal/services/kafka_retry.go](internal/services/kafka_retry.go)
- [docs/architecture/kafka-schema.md](docs/architecture/kafka-schema.md)
