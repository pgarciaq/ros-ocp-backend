# Snapshot staleness detection

VolumeSnapshots accumulate on OpenShift clusters and consume backing storage
(EBS, Azure Disk, Ceph/ODF, etc.). ROS classifies each snapshot reported by the
cost-management metrics operator and surfaces actionable recommendations through
the REST API.

There is **no koku-ui view yet**; integrate via API or automation. Dollar savings
for snapshots are estimated from configurable cost-per-GiB rates (not full CUR
integration in v1).

## What it does

On each ingestion cycle, ROS ingests `ocp_snapshot_inventory.csv` from the
operator tarball, classifies snapshots, upserts `snapshot_recommendation_sets`,
and removes rows for snapshots no longer present in the latest inventory.

Classifications:

| Type | Meaning |
|------|---------|
| `orphaned` | Source PVC deleted and age exceeds orphan threshold |
| `never_restored` | Never used to restore a volume; age exceeds threshold |
| `redundant` | More than N snapshots per PVC; this one is outside the N newest |
| `stale` | Old, never restored, not managed by a backup tool |
| `managed` | Labels indicate Velero/OADP or similar backup tooling |
| `active` | Recent or restored — informational only |

Notification codes **31–35** map to these classes (see
[notification codes](../../docs/architecture/notification-codes.md)).

## Data flow

```text
OpenShift API (VolumeSnapshot + PVC dataSource)
        ↓
koku-metrics-operator → ocp_snapshot_inventory.csv
        ↓
Koku listener → ROS processor → classify → snapshot_recommendation_sets
        ↓
GET /recommendations/openshift/snapshots
```

Operator collection is documented in the design doc:
[`docs/features-f-snapshot-staleness.md`](../../docs/features-f-snapshot-staleness.md).

## API

**List:** `GET /api/cost-management/v1/recommendations/openshift/snapshots`

Query parameters:

| Parameter | Description |
|-----------|-------------|
| `limit`, `offset` | Pagination (default limit 20, max 100) |
| `filter[cluster]` | Cluster UUID |
| `filter[project]` | Namespace |
| `filter[recommendation_type]` | One of the classifications above |

### Namespace summary

**Summary:** `GET /api/cost-management/v1/recommendations/openshift/snapshots/summary`

Aggregates `snapshot_recommendation_sets` by namespace and cluster (default) or by
cluster only (`group_by=cluster`). Use this endpoint to **prioritize cleanup work**
— sort by `reclaimable_monthly_holding_cost_usd` (default, descending) or
`reclaimable_restore_size_gib` to find namespaces with the highest recoverable
holding cost or storage.

Reclaimable totals include only `orphaned`, `stale`, `never_restored`, and
`redundant` snapshots. `active` and `managed` snapshots are counted in
`snapshot_count` and `counts_by_type` but excluded from reclaimable byte and cost
sums (managed snapshots are backup-tool-owned and should not be auto-deleted).

| Parameter | Description |
|-----------|-------------|
| `group_by` | `project` (namespace + cluster, default) or `cluster` |
| `filter[cluster]` | Cluster UUID |
| `filter[project]` | Namespace (exact or wildcard `*` → ILIKE) |
| `order_by` | `reclaimable_monthly_holding_cost_usd` (default), `reclaimable_restore_size_gib`, `actionable_snapshot_count`, `snapshot_count` |
| `order_how` | `desc` (default) or `asc` |
| `limit`, `offset` | Pagination (default limit 10, max 100) |

Example response (one namespace group):

```json
{
  "meta": { "count": 3, "limit": 10, "offset": 0, "currency": "USD" },
  "data": [
    {
      "namespace": "payments",
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
      "snapshot_count": 12,
      "actionable_snapshot_count": 8,
      "counts_by_type": {
        "orphaned": 2,
        "stale": 4,
        "never_restored": 1,
        "redundant": 1,
        "managed": 2,
        "active": 2
      },
      "total_restore_size_bytes": 128849018880,
      "total_restore_size_gib": 120.0,
      "reclaimable_restore_size_bytes": 96636764160,
      "reclaimable_restore_size_gib": 90.0,
      "total_monthly_holding_cost_usd": 6.0,
      "reclaimable_monthly_holding_cost_usd": 4.5,
      "age_days": { "min": 14, "max": 180 }
    }
  ],
  "links": { "first": "...", "next": null, "previous": null, "last": "..." }
}
```

**Settings:** `GET|PUT|DELETE /api/cost-management/v1/recommendations/openshift/settings/snapshot`

Tenant overrides for orphan age, never-restored days, stale days, redundant
threshold, and cost per GiB/month. Fields locked by deployment env vars appear
in `locked_fields`. **DELETE** removes tenant overrides and resets to deployment
defaults.

OpenAPI: [openapi.md](../openapi.md).

## Configuration (environment)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | 7 | Orphan classification |
| `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | 30 | Never-restored classification |
| `ROS_SNAPSHOT_STALE_DAYS` | 90 | Stale / redundant age gate |
| `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | 3 | Max snapshots per PVC before redundant |
| `ROS_SNAPSHOT_STALE_GRACE_HOURS` | 48 | Skip classification until inventory seen long enough |
| `ROS_SNAPSHOT_COST_PER_GIB_MONTH_USD` | 0.05 | Monthly cost estimate per GiB |

Snapshot settings can be locked with `ROS_SETTINGS_LOCKED_SNAPSHOT=true`.

## Distinction from container/namespace staleness

Container and namespace recommendations use **cluster reporting freshness**
(`ROS_STALENESS_THRESHOLD_HOURS`, default **48** hours) and `filter[stale]` on
container and namespace list APIs. That is unrelated to VolumeSnapshot age
classification.

See [stale detection](../../docs/operations/stale-detection.md) for
recommendation staleness.

## Limitations (v1)

- No UI in koku-ui
- Cost estimates use a flat GiB/month rate, not provider-specific CUR lines
- Requires VolumeSnapshot CRDs on the cluster; operator skips collection if absent
