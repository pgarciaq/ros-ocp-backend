# ADR-0088: Use Kafka + S3 pipeline for on-prem and SaaS (no custom /ingest)

## Status

Accepted

## Context

Cost-onprem already ships Kafka via AMQ Streams; custom HTTP ingest would duplicate infrastructure.

## Decision

Same Kafka consumer + S3 fetch path for both deployment modes.

## Consequences

Single code path. On-prem requires Kafka+S3 infrastructure. No HTTP upload API.

## References

- [docs/architecture/requirements.md](docs/architecture/requirements.md)
- [internal/services/report_processor.go](internal/services/report_processor.go)
