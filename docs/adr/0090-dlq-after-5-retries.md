# ADR-0090: Use DLQ after 5 transient retries with forensic headers

## Status

Accepted

## Context

Infinite retry blocks partitions; poison messages need forensics without stalling consumers.

## Decision

After 5 retries (tracked via `X-Retry-Count` header), produce to DLQ with forensic metadata.

## Alternatives Considered

### Infinite retry with exponential backoff
Retrying forever preserves every message eventually, but a single poison payload (malformed CSV, missing required column) blocks its Kafka partition indefinitely—stalling all clusters on that partition and hiding the failure from operators who only see consumer lag.

### Commit-and-drop after first failure
Skipping bad messages immediately unblocks partitions, but loses forensic context needed to diagnose operator CSV regressions; operators would see missing recommendations with no `report_file_status` row explaining why.

### Pause consumer until manual intervention
Halting the consumer on any failure prevents silent data loss, but one bad tenant message stops ingestion for all tenants on shared topics—a unacceptable blast radius for multi-tenant SaaS where DLQ isolation per message is preferred.

## Consequences

Partitions unblocked after max retries. Forensic data preserved. Requires DLQ monitoring.

## References

- [internal/services/kafka_retry.go](internal/services/kafka_retry.go)
- [docs/architecture/kafka-schema.md](docs/architecture/kafka-schema.md)
