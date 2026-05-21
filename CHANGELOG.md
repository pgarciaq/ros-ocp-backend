# Changelog

All notable API and behavioral changes to ROS-OCP-Backend are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Per-phase Prometheus histograms for pipeline observability (`rosocp_pipeline_phase_duration_seconds`)
- `rosocp_recommendations_written_total` counter with per-type labels
- `rosocp_ingestion_errors_total` counter with stage labels
- Structured logging with `org_id`, `cluster_uuid`, and `request_id` fields
- `request_id` header propagation (Echo RequestID middleware)
- Operational runbook (`docs/operations/runbooks.md`)
- Streaming recommendation pipeline (batch-of-500, O(batch) memory)
- Node recommendation term-based windowing (short/medium/long terms)

### Changed
- Recommendation pipeline refactored to streaming architecture (reduced peak memory)
- All engine/ingestion packages now use centralized `internal/logging` package
- API handlers use consistent `hlog` pattern with org_id + request_id context
- GPU filters pushed to SQL for correct pagination (denormalized columns in recommendation_sets)

### Fixed
- GPU pagination returning incomplete pages when filters applied post-query (#496)
- `rosocp_partition__missing_error_total` metric name double-underscore (#329)
- Missing ingestion error counter (#330)
- Engine math edge cases: negative savings, zero-division in margin (#256, #262, #263)
- `workload_type` missing from PKs causing silent data collisions (#346, #349)

## [Native Engine v1.0] — 2026-05-11

Initial release of the native Go recommendation engine replacing Kruize.

### Added
- Container CPU/memory recommendations with decay weighting and adaptive margin
- GPU recommendations (classification, MIG profile selection, time-slicing)
- Node utilization recommendations (underutilized, overcommitted, stranded)
- Namespace aggregate recommendations with box plots
- PVC right-sizing (oversized, near-full, orphaned, growth trend)
- Snapshot staleness detection
- Custom term configuration API (`GET/PUT/DELETE .../settings/terms`)
- Recommendation history and quality tracking
- Fleet summary endpoint
- Idle/abandoned workload detection with full savings estimation
- Adoption detection (15% tolerance matching)
- Stale data detection and lifecycle
- Estimated monthly savings via Koku cost integration
- CSV export for history and quality data
- Plugin architecture for engine selection
- RBAC enforcement on all endpoints
