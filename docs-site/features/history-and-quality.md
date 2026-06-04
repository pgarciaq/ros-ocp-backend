# Recommendation History & Quality

Container-scoped features for tracking recommendation changes over time and measuring recommendation effectiveness.

## History {#history}

Recommendation history is a **fleet-wide** API. There is no per-container ID sub-resource.

```
GET /api/cost-management/v1/recommendations/openshift/history
```

Filter to a single container (or cluster, project, workload) with query parameters:

```
GET /api/cost-management/v1/recommendations/openshift/history?filter[container]=<name>&filter[cluster]=<cluster_alias>
```

Each row is one container + `term` + `engine` snapshot at `recorded_at`. Data is retained for `ROS_HISTORY_RETENTION_DAYS` (default 90).

**Not available for:** node, PVC, VM, namespace, GPU, or quota plugins — container recommendations only.

### Scope limits (by design)

These are intentional boundaries, not missing implementations:

| Area | Behavior |
|------|----------|
| **Fleet history API** | Container recommendations only — no node, PVC, GPU, or VM fleet `GET .../history` endpoints |
| **PVC history** | Usage time-series on PVC detail — not recommendation snapshot history |
| **Quota / cluster quota** | `history[]` is embedded in **detail** responses (`/quota/detail`, `/cluster-quota/detail`), not a separate fleet history API |

### Future work

| Item | Status |
|------|--------|
| **GPU time-slicing / MIG recommendation history** | Not implemented — GPU recommendations have list/detail APIs but no `recommendation_history` writer |
| **PVC recommendation snapshot history** | By design, not implemented — PVC detail exposes usage time-series only; see [PVC plugin](../plugin-reference/pvc.md#history) |

## Quality

Quality metrics measure stability, adoption, and OOM signals after recommendations are issued. **Container-only.**

```
GET /api/cost-management/v1/recommendations/openshift/quality
```

| Field | Meaning | Scale |
|-------|---------|-------|
| `stability_pct` | How much the new recommendation changed vs the prior cycle | **0.0–1.0** (1.0 = unchanged) |
| `adoption_detected` | Current requests match the prior recommendation within 5% | boolean |
| `oom_events_after_rec` | OOM events in the **current ingestion batch** (not cumulative since the rec was issued; repeated non-zero values across batches indicate ongoing pressure) | integer |
| `recommendation_age_hours` | Hours since the prior recommendation | integer |
| `measured_at` | UTC date bucket for the row | timestamp |

Default `filter[engine]` is **cost** when omitted.

Prometheus gauges (`ros_recommendation_stability`, `ros_recommendation_adoption_rate`, `ros_recommendation_oom_rate`) are updated after each ingestion quality write; gauge stability/adoption values use a **0–100** scale (API quality fields use **0.0–1.0**). See [Monitoring](../monitoring.md).

## Retention and cleanup

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Drop `recommendation_history` and `recommendation_quality` partitions older than this |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Delete `recommendation_sets` rows marked `stale = true` older than this (not archived first) |
| `ROS_STALENESS_THRESHOLD_HOURS` | 48 | Hours without cluster report before marking recommendations stale |

The ros-processor retention ticker runs every 24 hours. See [Configuration — Retention](../configuration.md#retention-and-data-lifecycle).

**Not available for:** node and other non-container plugins.

### Confidence on recommendations and history

`confidence_level` is a **float from 0.0 to 1.0** (1.0 = highest confidence) on live container recommendations and on container history rows. It is **not** exposed on `/quality` list rows.

When `confidence_level` is below `low_confidence_threshold` (configurable via container settings; default **0.5**) and digest data exists (`data_days > 0`), notification code **1** (`NotifLowConfidence`) is emitted. See [Notification codes — Containers](../architecture/notification-codes.md#containers).

### Pipeline resilience

History and quality writes are **non-fatal**: if the analytics pipeline fails (database error, timeout), container recommendations are still persisted successfully.

- **Containers:** Failed `WriteRecommendationHistory` or `WriteRecommendationQuality` calls log an error, set `pipelineDegraded`, and processing continues — recommendations are not blocked by analytics failures.
- **Namespaces:** Transient database errors return a Kafka retry so history is written on redelivery; permanent errors skip history but keep recommendations available.

This design ensures recommendations are never blocked by analytics failures.

### Future work

| Item | Status |
|------|--------|
| **`data_coverage_pct`** — share of expected digest days in the analysis window | Not implemented |
| **Stale recommendation archive on cleanup** — copy rows to `recommendation_history` before deleting stale `recommendation_sets` (today `ROS_STALE_CLEANUP_DAYS` deletes stale rows without archiving) | Not implemented |
| **Per-plugin quality** (node, PVC, VM, etc.) | Not implemented |

Internal design detail: [quality-metrics design](../../docs/design/quality-metrics.md).
