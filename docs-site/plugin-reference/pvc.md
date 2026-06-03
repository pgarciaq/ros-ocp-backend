# pvc

Package: [`internal/plugins/pvc`](../../internal/plugins/pvc/)

**PVC right-sizing** — analyzes PersistentVolumeClaim capacity vs. usage over time, recommends resized capacity, and flags orphaned volumes for deletion.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `pvc` |
| Phase | 1 (Produce) |
| Priority | 30 |
| CSV types | `storage` (storage usage CSV from koku-metrics-operator) |
| Retention tables | `daily_pvc_digests`, `pvc_recommendation_sets` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — parses `storage` CSV type |
| APIProvider | Yes — list and detail |
| RetentionProvider | Yes |
| TermProvider | Yes — short/medium/long (max 365 days) |

## What it does

1. Ingest PVC capacity and usage samples into `daily_pvc_digests`.
2. Classify each PVC: **oversized**, **near_full**, **orphaned**, or **healthy**.
3. Project growth with weighted least squares (WLS) for near-full volumes.
4. Write `pvc_recommendation_sets` with recommended capacity, notifications, and savings.

See [PVC right-sizing](../features/pvc-rightsizing.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/pvcs
GET /api/cost-management/v1/recommendations/openshift/pvcs/detail
  ?cluster_uuid={uuid}&namespace={ns}&persistentvolumeclaim={name}
```

List filters include `filter[cluster]`, `filter[project]`, `filter[storageclass]`, and `filter[recommendation_type]` (`oversized`, `near_full`, `orphaned`, `healthy`).

Handlers: [`internal/plugins/pvc/routes.go`](../../internal/plugins/pvc/routes.go), [`GetPVCRecommendations`](../../internal/api/handlers_pvc.go).

## Key features

### Recommendation types

| Type | Condition (defaults) | Action |
|------|----------------------|--------|
| **oversized** | Max usage &lt; 20% of capacity (3+ days) | Resize down (~2× max usage, min 1 GiB) |
| **near_full** | Usage &gt; 85% or growth trend | Expand capacity |
| **orphaned** | Zero usage 3+ days | Delete candidate |
| **healthy** | 20–85% utilization | No change |

### Terms

| Term | Window | Min data |
|------|--------|----------|
| `short` | 7 days | 3 days |
| `medium` | 30 days | 14 days |
| `long` | 90 days | 30 days |

Storage patterns are slow-moving; max window is **365 days** (unlike CPU/memory plugins).

### Storage class awareness

Recommendations respect `storageclass` from CSV rows; list filters can scope by class.

## History

PVC detail exposes **usage time-series** data (historical capacity and usage observations), not recommendation snapshots over time. There is no `recommendation_history` equivalent for PVCs. This aligns with the PVC recommendation model which is based on current capacity vs. observed peak usage rather than evolving multi-engine recommendations.

## Notification codes

PVC rows may emit sizing and risk codes; code **25** (`NotifNoCostData`) when Masu storage rates are unavailable.

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=pvc`.

See [Notification codes](../architecture/notification-codes.md).

## Savings

Per-PVC `estimated_monthly_savings` (structured `value` + `units`), computed at ingestion:

| Type | Formula |
|------|---------|
| **Rightsizing (oversized)** | `(current_capacity − recommended_capacity) × storage_rate_per_gib_month` |
| **Orphaned (deletion)** | `current_capacity × storage_rate_per_gib_month` (full monthly cost recoverable) |

When no cost data is available, code **25** is set and savings show `$0` or are omitted.

PVC savings roll into fleet totals: `GET .../savings-summary` → `by_plugin.pvc`.

See [Savings estimations](../features/savings-estimations.md).

## Settings

Per-organization utilization thresholds and term overrides:

```
GET /api/cost-management/v1/recommendations/openshift/settings/pvc
PUT /api/cost-management/v1/recommendations/openshift/settings/pvc
DELETE /api/cost-management/v1/recommendations/openshift/settings/pvc
```

Env locks: `ROS_PVC_*`. See [Configurability](../architecture/configurability.md) (PVC section).

## Architecture

- [PVC right-sizing (feature)](../features/pvc-rightsizing.md)
- [Cost integration](../architecture/cost-integration.md)
- Internal design: [`docs/features/pvc-rightsizing.md`](../../docs/features/pvc-rightsizing.md)
