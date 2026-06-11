# ADR-0166: Per-file report_file_status tracking with manifest completeness gating

## Status

Accepted

## Context

Kafka messages arrive per-file, but recommendation engines must run only after **all** files for a manifest are ingested. Without per-file tracking, engines could run on partial data and produce incorrect recommendations. At-least-once Kafka semantics mean files may be re-delivered.

## Decision

Introduce a `report_file_status` table (migration `000140`) that records each file's processing state per manifest.

- `ReportFileTracker` in `internal/services/report_file_tracker.go` idempotently registers files (upsert on `(manifest_id, filename)`).
- `runManifestRecommendations()` in `manifest_recommendations.go` defers engine execution until `IsManifestIngestionComplete` returns true (all expected files processed).
- For publisher-supplied manifest IDs, completeness is determined by manifest metadata (expected file count).
- For synthesized IDs ([ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md)), completeness uses the quiet-period debouncer before calling the same gating logic.

Tracker DB errors fail open: log a warning and do not block ingestion.

## Alternatives Considered

### Run engines on every file

Simplest path—no tracking state—but produces incorrect recommendations when files arrive over seconds or minutes for the same manifest.

### Transactional outbox pattern

Reliable exactly-once semantics, but too complex for the reliability gain given idempotent upserts and Kafka retry semantics already in place.

### Timer-only approach

A fixed delay after the first file cannot distinguish "slow delivery" from "all files received"; operators would see either premature engines or unnecessary latency on every manifest.

## Consequences

- Guarantees engines see complete data for real manifest IDs.
- Adds one DB write per file (low overhead—idempotent upsert).
- Fail-open on tracker DB errors (logs warning, does not block ingestion).
- Requires manifest metadata to declare expected file count for publisher-supplied IDs.
- Legacy Kruize CSV path does not use `report_file_status` ([ADR-0163](0163-deprecate-kruize-plugin.md)).

## Related Decisions

- [ADR-0088](0088-kafka-s3-pipeline-both-modes.md): Kafka ingest pipeline and manifest-scoped file tracking.
- [ADR-0165](0165-defer-recommendations-for-synthesized-manifests.md): Quiet-period debouncer for synthesized manifest IDs.
- [ADR-0050](0050-uuid-v5-deterministic-recommendation-ids.md): UUID v5 synthesis pattern for deterministic IDs.

## References

- [internal/services/report_file_tracker.go](../../internal/services/report_file_tracker.go)
- [internal/services/manifest_recommendations.go](../../internal/services/manifest_recommendations.go)
- [internal/model/report_file_status.go](../../internal/model/report_file_status.go)
- [migrations/000140_report_file_status.up.sql](../../migrations/000140_report_file_status.up.sql)
- [docs/operations/runbooks.md](../operations/runbooks.md) — Partial manifest ingestion runbook
