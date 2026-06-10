# Recommendation Quality Metrics — Design

## Overview

Quality metrics track how stable, adopted, and reliable recommendations are over time. Currently **container-only**.

## Metrics

| Metric | Formula | Source |
|--------|---------|--------|
| `stability_pct` | `max(0, 1.0 − \|cpuVariation\|/100×0.5 − \|memVariation\|/100×0.5)` — stored as **0.0–1.0** (1.0 = no change) | Pre-overwrite snapshot from `recommendation_sets` (`ReadOldRecommendations` / `ReadClusterOldRecommendations`), compared to the new recommendation in the same ingestion cycle |
| `adoption_detected` | `true` if current CPU/memory requests match the **previous** recommendation within **5%** tolerance | `DetectAdoption` in [`internal/engine/quality.go`](../../internal/engine/quality.go) |
| `oom_events_after_rec` | OOM count from the **current ingest batch** (`oom_count_sum` on digests for containers in that batch) | Ingestion window for the cluster — **not** cumulative since the recommendation was issued |
| `recommendation_age_hours` | Truncated hours since prior `recommendation_sets.updated_at` | Prior row read before overwrite |

### OOM metric semantics

`oom_events_after_rec` reflects OOM events observed in the **current processing batch** only. It does not count all OOM events since the recommendation was first issued.

This is still useful: repeated non-zero values across consecutive ingestion batches indicate persistent memory pressure after recommendations were published.

**Future enhancement:** cumulative OOM tracking anchored to `first_recommended_at` (or `recommendation_applied_at`) would provide a stronger quality signal for under-sized memory recommendations.

## Confidence (recommendations only)

`confidence_level` is a **float from 0.0 to 1.0** (1.0 = highest confidence) on **individual recommendations**, not on `/quality` API rows.

| Where | Field |
|-------|--------|
| Live recommendations | `recommendation_sets.confidence_level` |
| Historical snapshots | `recommendation_history.confidence_level` |

Confidence is computed during ingestion from digest coverage vs the term window:

| Plugin | Storage | Formula |
|--------|---------|---------|
| Container / namespace | `recommendation_sets.confidence_level` | `min(data_days / window_days, 1.0)` via `computeConfidence` |
| PVC | `pvc_recommendation_sets` (no separate column; computed at ingest) | `min(data_days / MinDataDays, 1.0)` |
| Node | `node_recommendations.confidence_level` | `min(data_days / window_days, 1.0)` — same as container |
| GPU MIG / time-slicing | Not persisted; computed at API read | Tiered observation days + burst penalty (MIG); candidate-confidence blend (time-slicing). Exposed as `confidence` and `confidence_level`. |

When `confidence_level` is below `low_confidence_threshold` (container/node settings; default **0.5**) and `data_days > 0`, notification code **1** (`NotifLowConfidence` / `LOW_CONFIDENCE`) is emitted.

The `/quality` endpoint aggregates post-hoc signals per container cycle: `stability_pct`, `adoption_detected`, `oom_events_after_rec`, `recommendation_age_hours`. It does **not** expose a confidence field.

## Storage

- `recommendation_quality` table (partitioned monthly by `measured_at`)
- Written by `WriteRecommendationQuality` after each ingestion cycle
- Retained for `ROS_HISTORY_RETENTION_DAYS` (default 90)

## Pipeline behavior

By default (`ROS_INGEST_STRICT_ANALYTICS=false`), recommendations are persisted first and analytics gaps are surfaced via metrics and API flags. Set `ROS_INGEST_STRICT_ANALYTICS=true` to require history/quality writes before recommendations (transient failure retries the Kafka message).

### Container path (streaming batches)

Orchestrated by `WriteContainerRecBatch` in [`internal/engine/analytics_pipeline.go`](../../internal/engine/analytics_pipeline.go) from [`internal/services/report_processor.go`](../../internal/services/report_processor.go).

**Degraded mode (default):** recommendations written first; history/quality failures log structured errors, increment `rosocp_analytics_incomplete_total`, set `clusters.analytics_incomplete`, and expose `analytics_incomplete` on container list/detail responses.

**Strict mode:** history and quality written before recommendations; failures abort the batch (no offset commit).

When `ReadClusterOldRecommendations` fails, quality metrics are skipped for that cycle (no prior row to compare) and the pipeline is also marked degraded (degraded mode only).

### Namespace path (single CSV / message)

Namespace history runs after `WriteNamespaceRecommendations`. Behavior depends on error class (`isTransientKafkaProcessingError` in [`internal/services/kafka_processing_errors.go`](../../internal/services/kafka_processing_errors.go)):

- **Transient** errors (connection loss, timeouts, deadlocks): return error → Kafka offset not committed → message redelivered → history eventually written
- **Permanent** errors (constraint violations, bad data): log error, set analytics degraded, commit offset — recommendations remain available

Orchestration: [`WriteNamespaceRecommendationHistories`](../../internal/engine/analytics_hooks.go) in the namespace ingest path in `report_processor.go`.

## Prometheus gauges

After each successful `WriteRecommendationQuality` batch, the processor emits cluster-level gauges (labels `org_id`, `cluster_id` = `cluster_uuid`):

| Gauge | Aggregation |
|-------|-------------|
| `ros_recommendation_stability` | Mean `stability_pct` × 100 (0–100 scale on the gauge) |
| `ros_recommendation_adoption_rate` | Percent of rows in the batch with `adoption_detected` |
| `ros_recommendation_oom_rate` | Mean `oom_events_after_rec` per quality row in the batch |

The REST `/quality` API returns `stability_pct` as **0.0–1.0**, not 0–100.

## Stability weighting

The stability formula uses a fixed **50/50 CPU/memory weighting** (`ComputeStabilityPct` in [`internal/engine/quality.go`](../../internal/engine/quality.go)). This is not currently configurable via settings or environment variables. Making stability weights configurable is a future enhancement.

## Query performance

Migration `000131_recommendation_quality_list_index` adds `idx_recommendation_quality_org_engine_measured` on `(org_id, engine, measured_at DESC)` to support the default list query: org scope, engine filter, `measured_at` range, and `ORDER BY measured_at DESC`. Monthly partition pruning on `measured_at` remains the primary retention mechanism.

## Future work

| Item | Status |
|------|--------|
| **`data_coverage_pct`** — explicit fraction of the expected analysis window covered by digest data | Not implemented |
| **Per-plugin quality** (node, PVC, VM, namespace, GPU, quota) | Not implemented; see table below |
| **Configurable stability weights** | Not implemented |
| **Cumulative OOM-after-rec** | Not implemented; see OOM metric semantics above |

### `data_coverage_pct` (planned)

Would expose what percentage of the expected data window (`min_data_days` from term config) is covered by actual metrics in digests.

Example: if `min_data_days=14` and 10 distinct days of digest data exist in the window, coverage ≈ 71%.

Today this signal is communicated indirectly via per-recommendation `confidence_level` and notification code 1 (`LOW_CONFIDENCE`) when insufficient data is available.

**Future:** expose `data_coverage_pct` as an explicit field on `recommendation_sets` / `recommendation_history` and optionally on `recommendation_quality` rows.

### Per-Plugin Quality (not implemented)

| Plugin | Potential signals |
|--------|-------------------|
| Node | Capacity stability over time; consolidation adoption (did cluster shrink?) |
| PVC | Resize/delete adoption (did capacity change?); orphan confirmation |
| VM | Downsize adoption; power-off compliance; idle→terminated transition |
| Namespace | Target stability (do namespace CPU/mem targets stay consistent?) |
| GPU | Utilization improvement after recommendation; MIG profile adoption |
| Quota | Quota adjustment adoption (did admin lower the quota?) |

These require new writer functions and per-plugin quality formulas. Deferred to future phases.
