# PVC Right-Sizing

> **Public docs:** Canonical user-facing page is
> [`docs-site/features/pvc-rightsizing.md`](../docs-site/features/pvc-rightsizing.md).
> Do not copy this file into the MkDocs site (`generate-docs.sh` no longer overwrites it).

## Overview

PVC (PersistentVolumeClaim) right-sizing analyzes storage capacity vs. actual
usage and classifies PVCs to help reduce storage costs and prevent outages.

## Data Source

The koku-metrics-operator already collects PVC metrics and writes them as
`cm-openshift-storage-usage-YYYYMM.csv` files in the upload tarball (includes optional
`vm_name` when the mounting pod is a virt-launcher). No operator
changes are required for data collection.

> **File routing (implemented):** The storage CSV is in the manifest `files`
> array (cost pipeline). The Koku listener was updated to also route it to the
> ROS Kafka topic — see `koku/masu/external/kafka_msg_handler.py` (the
> `_ros_extra_patterns` tuple). See the [snapshot staleness design doc](features-f-snapshot-staleness.md#two-strategies-both-documented) for the
> architectural rationale behind this "Strategy A" approach.

**CSV columns used:**
- `interval_start`, `interval_end` — time window
- `namespace`, `persistentvolumeclaim`, `persistentvolume`, `storageclass`
- `persistentvolumeclaim_capacity_bytes` — provisioned capacity
- `persistentvolumeclaim_usage_byte_seconds` — usage × seconds (converted to bytes)
- `volume_request_storage_byte_seconds` — request × seconds
- `pod` — mounting pod (API `mounted_by`)
- `vm_name` — KubeVirt VM when virt-launcher (API `vm_name`)

## Pipeline

1. **Detection**: `DetermineCSVType()` identifies `"storage"` in the filename
2. **Parsing**: `ingestion.ParsePVCRows()` reads CSV into `PVCRow` structs
3. **Digestion**: `ComputePVCDigests()` aggregates hourly rows into daily min/max/avg
4. **Upsert**: `UpsertPVCDigests()` writes to `daily_pvc_digests` table
5. **Recommendation**: `RecommendPVCs()` loads digests within the configured term window and classifies
6. **Persistence**: `WritePVCRecommendations()` upserts to `pvc_recommendation_sets`

> **Configurable terms:** PVC uses the TermProvider trait with defaults of
> 7d (short), 30d (medium), 90d (long). The maximum allowed window is 365 days
> (storage growth patterns are slow-moving). Terms can be customized per-tenant
> via the Settings API or locked by administrators via environment variables.

## Classification

| Type | Condition | Action |
|------|-----------|--------|
| **Oversized** | max usage / capacity < 20% (sustained 3+ days) | Recommends 2× max usage (min 1 GiB) |
| **Near-full** | max usage / capacity > 85% | Recommends expansion to 2× current usage |
| **Orphaned** | zero usage for 3+ days | Recommends deletion |
| **Healthy** | usage between 20-85% | No action needed |

### Minimum data requirements

- **3 days** minimum for orphaned/oversized classification (avoids false positives on new PVCs)
- **7 days** minimum for growth trend projection

## Growth Trend Projection

Linear regression on daily average usage (bytes/day slope). When growth is
positive and capacity is finite, `days_to_full` projects when the PVC will
reach capacity at the current growth rate.

PVCs with `days_to_full < 30` receive a near-full notification even if current
usage is below 85%.

## Operational Notes

The API response includes a `resize_note` field with important context:

- **Oversized**: "Kubernetes does not support in-place PVC shrinking. Reducing
  this PVC requires creating a smaller volume, migrating data, and deleting the
  original."
- **Orphaned**: "This PVC has zero usage. If the data is no longer needed,
  deleting the PVC will reclaim the backing storage volume."

**Why PVC right-sizing saves money:** With dynamic provisioning (EBS, Azure Disk,
Ceph RBD via CSI), the backing volume is exactly the size of the PVC request.
You pay for provisioned capacity, not used capacity. Oversized PVCs mean paying
for storage you don't use.

## Dollar savings estimates

When `KOKU_MASU_URL` is configured and `ROS_SAVINGS_ESTIMATES_ENABLED=true` (default),
ROS computes `estimated_monthly_savings` at ingestion (API: structured `{value, units}`) using
storage rates from Masu `effective_rates`:

| Classification | Formula |
|----------------|---------|
| **Oversized** | `(current_gib − recommended_gib) × storage_gb_request_per_month` (falls back to `storage_gb_usage_per_month` when the request rate is zero) |
| **Orphaned** | `current_gib × storage_gb_request_per_month` — full monthly cost recoverable by deletion |

Near-full and healthy rows do not receive positive savings. Requires migration **000070**.
When Masu is unavailable or savings are disabled, savings are omitted/zero and notification
code **25** (`NotifNoCostData`) is appended.

Full plugin matrix and troubleshooting: [architecture/cost-integration.md](architecture/cost-integration.md).

## Fleet savings rollup

PVC savings contribute to the cross-plugin fleet summary:

`GET /api/cost-management/v1/recommendations/openshift/savings-summary`

Optional query params include `engine` (default `cost`) and `term` (default `medium`). The response
`by_plugin.pvc` object totals estimated monthly savings across all PVC recommendations for the
selected engine and term — use it for Optimizations overview cards alongside container, node, and
other plugins.

## Realizing PVC savings (migration path)

Most CSI drivers and cloud block storage backends **do not support in-place PVC
shrinking**. When ROS classifies a PVC as oversized, `estimated_monthly_savings`
reflects the monthly cost difference between the current provisioned size
and the recommended size — but realizing that savings requires a manual migration:

1. Create a new PVC at the recommended (smaller) size.
2. Copy or migrate application data to the new volume (for example via a temporary
   Job, Velero, or application-specific tooling).
3. Update the workload to mount the new PVC.
4. Delete the old PVC to release the backing volume and stop paying for unused capacity.

The API `resize_note` on oversized PVCs repeats this guidance. Near-full and
orphaned PVC recommendations follow different actions (expand or delete). See
[Negative savings](architecture/cost-integration.md#negative-savings) when a
near-full recommendation implies expansion (negative savings = additional monthly cost).

## Notification Codes

| Code | Severity | Message |
|------|----------|---------|
| 20 | WARNING | PVC has zero usage across all intervals |
| 25 | INFO | No cost data available — savings estimate not computed |
| 29 | INFO | PVC capacity significantly exceeds sustained usage — consider shrinking |
| 30 | WARNING | PVC usage approaching capacity — consider expanding or investigate growth |

See [notification-codes.md](architecture/notification-codes.md) for triggers, emitters, and the full system catalog.

## API

List: `GET /api/cost-management/v1/recommendations/openshift/pvcs`

Detail: `GET /api/cost-management/v1/recommendations/openshift/pvcs/detail`

Bracket syntax is preferred; flat ROS aliases are also accepted. See
[API query parameters](operations/api-query-parameters.md).

## Settings API

Per-organization PVC classification thresholds:

- `GET /api/cost-management/v1/recommendations/openshift/settings/pvc`
- `PUT /api/cost-management/v1/recommendations/openshift/settings/pvc`
- `DELETE /api/cost-management/v1/recommendations/openshift/settings/pvc` (reset to defaults)

| Field | Purpose |
|-------|---------|
| `oversized_threshold` | Max usage / capacity below this fraction → **oversized** (default `0.20`) |
| `near_full_threshold` | Usage / capacity above this fraction → **near_full** (default `0.85`) |
| `min_trend_days` | Minimum days of usage data before computing a growth slope |
| `days_to_full_alert` | Fire a near-full alert when projected days-to-full falls below this value (default `30`) |

The response includes `locked_fields`: threshold names that cannot be changed via PUT
because an administrator set the matching `ROS_PVC_*` environment variable on the
deployment. PUT returns `403 Forbidden` when the body contains a locked field.

Term windows (`short`, `medium`, `long`) are configured separately at
`GET|PUT|DELETE .../settings/terms?recommendation_type=pvc`. See
[Configurability](architecture/configurability.md) for env var names and defaults.

## CSV export

The list endpoint supports flattened export for spreadsheets and integrations:

```
GET /api/cost-management/v1/recommendations/openshift/pvcs?format=csv
```

`Accept: text/csv` is also supported. Export returns all matching PVC recommendations
(up to `limit`, max 1000 per request) with CSV columns aligned to the JSON list fields
(`cluster_uuid`, `namespace`, `persistentvolumeclaim`, `recommendation_type`, `term`,
`usage_ratio`, `estimated_monthly_savings`, `idle_since`, `idle_duration_days`,
`days_to_full`, `growth_bytes_per_day`, and others). The same filters as the JSON list
apply (`filter[cluster]`, `filter[project]`, `filter[term]`, etc.).

### List query parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `filter[cluster]` | UUID | Filter by cluster (`cluster`, `cluster_uuid`) |
| `filter[project]` | string | Filter by namespace (`namespace`, `project`) |
| `filter[recommendation_type]` | enum | `oversized`, `near_full`, `orphaned`, `healthy` |
| `filter[term]` | enum | `short`, `medium`, `long` (default `medium`) |
| `filter[storageclass]` | string | Filter by StorageClass name |
| `filter[tag:<key>]` | string | Tag filter (when `ROS_TAGS_ENABLED=true`) |
| `order_by` | string | `usage_ratio`, `estimated_monthly_savings`, `pvc_name`, `capacity_bytes` |
| `order_how` | string | `asc` or `desc` (default `desc`) |
| `limit` | int | 1–100 (default 20) |
| `offset` | int | Pagination offset |
| `format` | string | `csv` for CSV export (`Accept: text/csv` also supported) |

Each list row includes `term` for the selected window. Orphaned rows may include
`idle_since` and `idle_duration_days`.

### Detail query parameters

Required: `cluster_uuid` (or `filter[cluster]`), `namespace` (or `filter[project]`),
`persistentvolumeclaim` (or `pvc_name`).

Response includes `terms` (`short`, `medium`, `long`), `historical_usage` (daily
digests), `mounted_by`, and optional `vm_name`. Term rows include `days_to_full` and
`growth_bytes_per_day` when growth projection applies.

### List response (excerpt)

```json
{
  "meta": { "count": 3, "limit": 20, "offset": 0, "currency": "USD" },
  "data": [
    {
      "cluster_uuid": "aaaaaaaa-...",
      "namespace": "production",
      "persistentvolumeclaim": "old-logs",
      "mounted_by": "virt-launcher-old-logs-x9y8z",
      "vm_name": "fedora-vm",
      "storageclass": "gp3",
      "capacity_bytes": 107374182400,
      "usage_ratio": 0.0,
      "recommendation_type": "orphaned",
      "idle_since": "2026-05-01",
      "idle_duration_days": 14,
      "estimated_monthly_savings": { "value": "12.50", "units": "USD" },
      "term": "medium",
      "resize_note": "This PVC has zero usage..."
    }
  ]
}
```

## Database Tables

### `daily_pvc_digests` (partitioned by `bucket_date`)

Daily aggregated PVC metrics. Unique on `(cluster_uuid, namespace, pvc, date)`.

### `pvc_recommendation_sets`

Per-term PVC recommendations. Unique on `(org_id, cluster_uuid, namespace, pvc, term)`.
Overwritten on each ingestion cycle.

## Key Files

| File | Purpose |
|------|---------|
| `internal/ingestion/pvc.go` | CSV parsing, digest computation, upsert |
| `internal/engine/pvc_recommend.go` | Classification, growth trend, DB write |
| `internal/engine/pvc_savings.go` | Dollar savings from Masu storage rates |
| `internal/api/handlers_pvc.go` | List API handler |
| `internal/api/handlers_pvc_detail.go` | Detail handler (multi-term + historical usage) |
| `migrations/000047_create_pvc_tables.up.sql` | Schema |
| `migrations/000048_add_pvc_notification_codes.up.sql` | Notification seed |
| `migrations/000070_add_node_pvc_savings_columns.up.sql` | `estimated_monthly_savings_usd` column |
| `migrations/000113_fix_pvc_recommendation_term_unique.up.sql` | Per-term unique key |
| `migrations/000114_add_pvc_last_seen_pod.up.sql` | `last_seen_pod` / `mounted_by` |

## Manual QE Verification

```bash
# 1. Generate OCP data with storage metrics (nise includes storage by default)
# 2. Ingest data
# 3. Query PVC recommendations:
IDENTITY=$(echo -n '...' | base64 -w0)
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/pvcs' \
  | python3 -m json.tool

# Filter by type:
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/pvcs?filter[recommendation_type]=oversized'

# Detail (all terms + historical_usage):
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/pvcs/detail?cluster_uuid=UUID&namespace=NS&persistentvolumeclaim=NAME'

# Check digest data directly:
psql -c "SELECT * FROM daily_pvc_digests LIMIT 10;"
psql -c "SELECT * FROM pvc_recommendation_sets ORDER BY usage_ratio DESC LIMIT 10;"
```
