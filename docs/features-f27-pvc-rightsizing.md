# F27: PVC Right-Sizing

## Overview

PVC (PersistentVolumeClaim) right-sizing analyzes storage capacity vs. actual
usage and classifies PVCs to help reduce storage costs and prevent outages.

## Data Source

The koku-metrics-operator already collects PVC metrics and writes them as
`cm-openshift-storage-usage-YYYYMM.csv` files in the upload tarball. No operator
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

## Pipeline

1. **Detection**: `DetermineCSVType()` identifies `"storage"` in the filename
2. **Parsing**: `ingestion.ParsePVCRows()` reads CSV into `PVCRow` structs
3. **Digestion**: `ComputePVCDigests()` aggregates hourly rows into daily min/max/avg
4. **Upsert**: `UpsertPVCDigests()` writes to `daily_pvc_digests` table
5. **Recommendation**: `RecommendPVCs()` loads 90 days of digests and classifies
6. **Persistence**: `WritePVCRecommendations()` upserts to `pvc_recommendation_sets`

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

## Notification Codes

| Code | Severity | Message |
|------|----------|---------|
| 20 | WARNING | PVC has zero usage across all intervals |
| 29 | INFO | PVC capacity significantly exceeds sustained usage — consider shrinking |
| 30 | WARNING | PVC usage approaching capacity — consider expanding or investigate growth |

## API

`GET /api/cost-management/v1/recommendations/openshift/pvcs`

### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `cluster_uuid` | UUID | Filter by cluster |
| `namespace` | string | Filter by namespace |
| `recommendation_type` | enum | `oversized`, `near_full`, `orphaned`, `healthy` |
| `limit` | int | Results per page (1-100, default 20) |
| `offset` | int | Pagination offset |

### Response

```json
{
  "meta": { "count": 3, "limit": 20, "offset": 0 },
  "data": [
    {
      "cluster_uuid": "aaaaaaaa-...",
      "namespace": "production",
      "persistentvolumeclaim": "old-logs",
      "persistentvolume": "pv-123",
      "storageclass": "gp3",
      "capacity_bytes": 107374182400,
      "usage_bytes_max": 0,
      "usage_ratio": 0.0,
      "recommendation_type": "orphaned",
      "days_to_full": null,
      "growth_bytes_per_day": 0,
      "notifications": {
        "20": { "type": "WARNING", "message": "PVC has zero usage...", "code": 20 }
      },
      "data_days": 14,
      "resize_note": "This PVC has zero usage. If the data is no longer needed, deleting the PVC will reclaim the backing storage volume."
    }
  ]
}
```

## Database Tables

### `daily_pvc_digests` (partitioned by `bucket_date`)

Daily aggregated PVC metrics. Unique on `(cluster_uuid, namespace, pvc, date)`.

### `pvc_recommendation_sets`

Current PVC recommendations. Unique on `(org_id, cluster_uuid, namespace, pvc)`.
Overwritten on each ingestion cycle.

## Key Files

| File | Purpose |
|------|---------|
| `internal/ingestion/pvc.go` | CSV parsing, digest computation, upsert |
| `internal/engine/pvc_recommend.go` | Classification, growth trend, DB write |
| `internal/api/handlers_pvc.go` | API handler |
| `migrations/000047_create_pvc_tables.up.sql` | Schema |
| `migrations/000048_add_pvc_notification_codes.up.sql` | Notification seed |

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
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/pvcs?recommendation_type=oversized'

# Check digest data directly:
psql -c "SELECT * FROM daily_pvc_digests LIMIT 10;"
psql -c "SELECT * FROM pvc_recommendation_sets ORDER BY usage_ratio DESC LIMIT 10;"
```
