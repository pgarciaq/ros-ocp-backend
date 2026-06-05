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
| Retention tables | `daily_pvc_digests` (partition sweep via `RetentionProvider`) |

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

**Business hours:** not applicable. Business-hours weighting applies to container and namespace recommendations only.

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/pvcs
GET /api/cost-management/v1/recommendations/openshift/pvcs/detail
  ?cluster_uuid={uuid}&namespace={ns}&persistentvolumeclaim={name}
```

List filters include `filter[cluster]`, `filter[project]`, `filter[storageclass]`,
`filter[recommendation_type]` (`oversized`, `near_full`, `orphaned`, `healthy`),
`filter[term]` (`short`, `medium`, `long`; aliases `short_term`, `medium_term`,
`long_term`), and `filter[tag:<key>]` (when
`ROS_TAGS_ENABLED=true`).

### List query parameters

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Cluster UUID |
| `filter[project]` | Namespace |
| `filter[storageclass]` | StorageClass name |
| `filter[recommendation_type]` | `oversized`, `near_full`, `orphaned`, `healthy` |
| `filter[term]` | `short`, `medium`, `long` (default `medium`); `short_term`, `medium_term`, `long_term` aliases |
| `filter[tag:<key>]` | Tag value filter on namespace scope |
| `format` | `csv` for flattened export (`Accept: text/csv` also supported) |

### List response fields

| Field | When present | Description |
|-------|----------------|-------------|
| `mounted_by` | Storage CSV included a `pod` column | Most recently observed mounting pod (persisted as `last_seen_pod`) |
| `vm_name` | `virt-launcher-*` pod and operator `vm_name` in CSV | Authoritative KubeVirt VM name for VM disks |
| `days_to_full` | Near-full or oversized with positive growth slope | Projected days until capacity at current growth rate |
| `growth_bytes_per_day` | Same as `days_to_full` | WLS trend slope in bytes/day |
| `idle_since` | `recommendation_type=orphaned` | First date with zero usage (`YYYY-MM-DD`) |
| `idle_duration_days` | Orphaned | Days since `idle_since` at the last classification run |
| `estimated_monthly_savings` | Oversized/orphaned when cost rates exist | Structured `{value, units}` monthly savings |

Growth and idle fields are omitted when not applicable (for example **healthy** PVCs without
a growth projection, or non-orphaned rows for idle fields).

Handlers: [`internal/plugins/pvc/plugin.go`](../../internal/plugins/pvc/plugin.go) (`RegisterRoutes`), [`GetPVCRecommendations`](../../internal/api/handlers_pvc.go), [`GetPVCRecommendationDetail`](../../internal/api/handlers_pvc_detail.go).

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

| Code | Trigger |
|------|---------|
| 20 | Orphaned (no pod mount / zero usage detected) |
| 25 | No cost data available for savings |
| 29 | Oversized (capacity exceeds usage beyond threshold) |
| 30 | Near full (projected to exhaust within alert window) |

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=pvc`.

See [Notification codes](../architecture/notification-codes.md) for severity, messages, and the full catalog.

## Savings

Per-PVC `estimated_monthly_savings` (structured `value` + `units`), computed at ingestion:

| Type | Formula |
|------|---------|
| **Rightsizing (oversized)** | `(current_capacity − recommended_capacity) × storage_rate_per_gib_month` |
| **Orphaned (deletion)** | `current_capacity × storage_rate_per_gib_month` (full monthly cost recoverable) |

When no cost data is available, code **25** is set and savings show `$0` or are omitted.

`estimated_monthly_savings.value` can be **negative** when current capacity is already below the recommended target (upsize required). Display as additional monthly cost, not as a savings opportunity.

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
- Internal design: [`docs/features-f27-pvc-rightsizing.md`](../../docs/features-f27-pvc-rightsizing.md)
