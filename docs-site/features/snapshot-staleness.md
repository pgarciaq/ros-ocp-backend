# Snapshot staleness detection

VolumeSnapshots accumulate on OpenShift clusters and consume backing storage
(EBS, Azure Disk, Ceph/ODF, etc.). ROS classifies each snapshot reported by the
cost-management metrics operator and surfaces actionable recommendations through
the REST API.

There is **no koku-ui view yet**; integrate via API or automation. Recoverable
holding cost in v1 is a **placeholder estimate** from a configurable $/GiB/month
rate (`cost_per_gib_month_usd`); see [Future work](#future-work) for provider-accurate
costing via [COST-7523](https://redhat.atlassian.net/browse/COST-7523).

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
threshold, inventory freshness window, and cost per GiB/month. Fields locked by
deployment env vars appear in `locked_fields`. **DELETE** removes tenant overrides
and resets to deployment defaults.

See [Configurability — Snapshot](../architecture/configurability.md#snapshot) for field
reference, env vars, and precedence. OpenAPI: [openapi.md](../openapi.md).

## Configuration (environment)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | 7 | Orphan classification |
| `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | 30 | Never-restored classification |
| `ROS_SNAPSHOT_STALE_DAYS` | 90 | Stale / redundant age gate |
| `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | 3 | Max snapshots per PVC before redundant |
| `ROS_SNAPSHOT_STALE_GRACE_HOURS` | 48 | Skip classification until inventory seen long enough |
| `ROS_SNAPSHOT_COST_PER_GIB_MONTH_USD` | 0.05 | Monthly cost estimate per GiB |
| `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS` | 6 | Inventory freshness window for classification (API: `inventory_fresh_hours`) |

Snapshot settings can be locked with `ROS_SETTINGS_LOCKED_SNAPSHOT=true`.

## Distinction from container/namespace staleness

Container and namespace recommendations use **cluster reporting freshness**
(`ROS_STALENESS_THRESHOLD_HOURS`, default **48** hours) and `filter[stale]` on
container and namespace list APIs. That is unrelated to VolumeSnapshot age
classification.

See [stale detection](../../docs/operations/stale-detection.md) for
recommendation staleness.

## Limitations (v1)

### Snapshot size estimation (design)

- **Source:** `restore_size_bytes` from VolumeSnapshot `.status.restoreSize` (full logical volume size).
- **Non-goal — CSI consumed bytes:** We do not collect provider-specific CSI metrics for actual snapshot consumption on backend storage. There is no standard Kubernetes metric for this; the CSI spec does not mandate it; in-cluster Prometheus rarely exposes provider metrics (Ceph RBD, CloudWatch, GCP, etc.); per-driver maintenance is high; and [COST-7523](https://redhat.atlassian.net/browse/COST-7523) will supply real consumed bytes from billing/CUR where available.
- **Trade-off:** `restoreSize` overestimates holding cost for incremental/COW snapshots on most providers — acceptable for v1 prioritization.
- **Resolution:** COST-7523 effective cost uses actual billing data; on-prem/ODF without CUR may warrant a separate Ceph-specific effort later.

- No UI in koku-ui
- **Detection and classification only** — ROS does not restore volumes, delete
  snapshots, run safe-delete workflows, or integrate with backup operators (Velero,
  OADP, Kasten, etc.). Operators and admins act on API output manually or via their
  own automation.
- **Cost estimates are approximate** — `estimated_monthly_cost_usd` uses
  `restore_size_bytes × cost_per_gib_month_usd`. The per-org Settings API field and
  env default are **placeholders** for QE/Ops tuning, not provider billing truth.
  When savings estimates are enabled, ROS may fall back to Koku
  `effective_rates` `storage_gb_usage_per_month` (PVC usage proxy), which is still
  not snapshot-specific CUR or block-storage snapshot line items.
- **Incremental snapshots** — on providers such as AWS EBS, `restore_size_bytes`
  is a ceiling estimate, not billed incremental snapshot size.
  [COST-7523](https://redhat.atlassian.net/browse/COST-7523)'s effective cost endpoint
  will use actual billing data rather than logical volume size.
- Requires VolumeSnapshot CRDs on the cluster; operator skips collection if absent

## Future work

### Accurate snapshot cost (COST-7523)

Production-quality recoverable cost should come from **actual storage economics**,
not a flat GiB/month knob:

| Source (target) | Role |
|-----------------|------|
| Koku cloud billing (AWS CUR, Azure exports, GCP BigQuery, etc.) | Provider-specific snapshot or volume backup charges where available |
| Koku OCP cost model | User-defined storage rates (e.g. `storage_gb_usage_per_month`, future snapshot metric) |

Upstream work is tracked in **[COST-7523](https://redhat.atlassian.net/browse/COST-7523)**.
That epic adds a Koku **effective cost internal endpoint**; ROS will consume it (same
pattern as today's Masu `effective_rates` fetch) to replace placeholder
`cost_per_gib_month_usd` defaults with cluster- and class-aware rates when data exists.

Until COST-7523 ships, keep using `/settings/snapshot` and `ROS_SNAPSHOT_COST_PER_GIB_MONTH_USD`
for demos and on-prem pools without CUR. See
[Cost integration — Snapshot cost](../../docs/architecture/cost-integration.md#snapshot-cost-dynamic-default-from-effective-rates)
and [features-f-snapshot-staleness.md](../../docs/features-f-snapshot-staleness.md).

### Per-StorageClass cost overrides (v2)

v1 uses a single org-wide `cost_per_gib_month_usd`. Per-`volume_snapshot_class` (or
StorageClass) rate overrides in snapshot settings v2 are tracked in
**[COST-7563](https://redhat.atlassian.net/browse/COST-7563)**. See
[features-f-snapshot-staleness.md](../../docs/features-f-snapshot-staleness.md#per-storageclass-cost-rates-v2--not-in-initial-implementation).

### Restore and cleanup automation (explicitly out of v1)

Planned follow-ons, **not** in staleness v1 scope:

- Automated restore-and-verify (prove a snapshot can rebuild a PVC before deletion)
- Safe-delete workflows (pre-checks, dry-run, approval gates)
- Backup operator integration (Velero/OADP retention alignment, coordinated prune)

v1 **`managed`** classification only flags backup-tool-owned snapshots for human
retention review; it does not trigger operator actions.
