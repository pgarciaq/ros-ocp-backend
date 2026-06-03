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

Prometheus gauges (`ros_recommendation_stability`, `ros_recommendation_adoption_rate`, `ros_recommendation_oom_rate`) are updated after each ingestion quality write; gauge stability/adoption values use a **0–100** scale. See [Monitoring](../monitoring.md).

**Not available for:** node and other non-container plugins. Confidence (`high` / `medium` / `low`) is on live recommendations and container history rows, not on `/quality` list rows.

### Future work

| Item | Status |
|------|--------|
| **`data_coverage_pct`** — share of expected digest days in the analysis window | Not implemented |
| **Stale recommendation archive on cleanup** — copy rows to `recommendation_history` before deleting stale `recommendation_sets` (today `ROS_STALE_CLEANUP_DAYS` deletes stale rows without archiving) | Not implemented |
| **Per-plugin quality** (node, PVC, VM, etc.) | Not implemented |

Internal design detail: [quality-metrics design](../../docs/design/quality-metrics.md).
