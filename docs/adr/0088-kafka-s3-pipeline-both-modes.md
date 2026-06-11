# ADR-0088: Use Kafka + S3 pipeline for on-prem and SaaS (no custom /ingest)

## Status

Accepted

## Context

Cost-onprem already ships Kafka via AMQ Streams; custom HTTP ingest would duplicate infrastructure.

## Decision

Same Kafka consumer + S3 fetch path for both deployment modes.

## Alternatives Considered

### HTTP `/ingest` upload API for on-prem
A direct multipart upload endpoint would let operators push CSV tarballs without Kafka, but duplicates manifest validation, per-file tracking, and DLQ handling already implemented for the Kafka path—and cost-onprem already ships AMQ Streams, making Kafka infrastructure a sunk cost.

### Separate on-prem code path (PostgreSQL listener)
Mirroring Koku's on-prem listener pattern inside ROS would avoid Kafka dependency, but forks ingestion logic: every bug fix and security hardening (SSRF, DLQ, statement timeout) would need dual implementation and testing.

### SaaS-only Kafka with on-prem batch cron
Running Kafka consumer only in SaaS and a nightly cron for on-prem would reduce on-prem infrastructure, but breaks near-real-time recommendations expected after operator upload cycles (default 6h); stale data windows are unacceptable for cost optimization use cases.

## Consequences

Single code path. On-prem requires Kafka+S3 infrastructure. No HTTP upload API.

## Manifest completeness gating (ADR-0166)

Recommendation engines run only after manifest ingestion is complete. Two paths converge on `runManifestRecommendations()`:

### Real manifest IDs (publisher-supplied)

`IsManifestIngestionComplete` checks `report_file_status` against manifest metadata expected file count. Engines defer until all files reach `done` status. See [ADR-0166](0166-report-file-status-manifest-gating.md).

### Synthesized manifest IDs (`synth-*` prefix)

When Kafka messages omit `metadata.manifest_id`, UUID v5 synthesis ([ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md)) produces deterministic `synth-*` IDs. Completeness uses the quiet-period debouncer ([ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md)) before invoking the same gating and engine path.

## References

- [docs/archive/requirements.md](../archive/requirements.md)
- [internal/services/report_processor.go](../../internal/services/report_processor.go)
- [internal/services/manifest_recommendations.go](../../internal/services/manifest_recommendations.go)
- [internal/services/report_file_tracker.go](../../internal/services/report_file_tracker.go)
