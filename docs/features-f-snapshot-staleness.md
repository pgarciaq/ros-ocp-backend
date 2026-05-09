# Snapshot Staleness Detection — Design Document

**Status:** Design (not yet implemented)
**Dependencies:** Requires operator change (new CSV collector) + Koku listener routing update

## Problem Statement

VolumeSnapshots accumulate over time and are easy to forget about. Each
snapshot consumes storage in the underlying CSI provider (EBS, Azure Disk,
Ceph/ODF) and incurs ongoing cost. Customers have no visibility into which
snapshots are still useful and which are stale, orphaned, or redundant.

Detecting unused snapshots and surfacing them as recommendations enables
teams to reclaim storage costs without manual auditing.

## Data Source: New Operator CSV

The koku-metrics-operator would need a new collector that queries the
Kubernetes API for `VolumeSnapshot` objects and cross-references
`PersistentVolumeClaim.spec.dataSource` to determine restore activity.

### New file: `cm-openshift-snapshot-inventory-YYYYMM.csv`

| Column | Type | Source |
|--------|------|--------|
| `interval_start` | timestamp | Collection window start |
| `interval_end` | timestamp | Collection window end |
| `namespace` | string | VolumeSnapshot namespace |
| `snapshot_name` | string | VolumeSnapshot name |
| `source_pvc_name` | string | `.spec.source.persistentVolumeClaimName` |
| `volume_snapshot_class` | string | `.spec.volumeSnapshotClassName` |
| `storageclass` | string | Source PVC's StorageClass |
| `creation_timestamp` | timestamp | `.metadata.creationTimestamp` |
| `ready_to_use` | boolean | `.status.readyToUse` |
| `restore_size_bytes` | int64 | `.status.restoreSize` (converted to bytes) |
| `source_pvc_exists` | boolean | Whether the source PVC still exists in the namespace |
| `restored_pvc_count` | int | Count of PVCs in the namespace with `dataSource.name == snapshot_name` |
| `labels` | string | JSON-encoded map of snapshot labels (for backup tool detection) |

### Operator Implementation Notes

- Query `VolumeSnapshot` objects via the Kubernetes API (not Prometheus —
  snapshots are not exposed as metrics)
- Cross-reference `PersistentVolumeClaim.spec.dataSource` to count restores:
  list all PVCs in the namespace, check if `.spec.dataSource.kind == "VolumeSnapshot"`
  and `.spec.dataSource.name` matches
- Check source PVC existence by attempting a GET on the source PVC name
- Include `.metadata.labels` as JSON for backup tool detection in the backend
- Gracefully skip collection if the `VolumeSnapshot` CRD
  (`snapshot.storage.k8s.io`) is not installed on the cluster
- Estimated effort: 1 new Kubernetes API LIST path, ~250 lines of Go

### Collection Frequency

The snapshot inventory is collected **once per upload cycle** (default: every
6 hours, aligned with the existing packaging cadence). VolumeSnapshots are
relatively static objects (created once, rarely modified), so this cadence is
sufficient.

The collection consists of:
- One `LIST VolumeSnapshots` call (all namespaces or per-namespace)
- One `LIST PersistentVolumeClaims` call per namespace (for dataSource cross-reference)

These are lightweight point-in-time queries — no watches or informers required.

## Backend Classification Logic

| Classification | Rule | Notification |
|----------------|------|--------------|
| **Orphaned** | Source PVC no longer exists AND age > `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` (7) | "Source PVC was deleted; snapshot may no longer be needed" |
| **Never restored** | `restored_pvc_count == 0` AND age > `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` (30) AND not managed | "Snapshot has never been used to restore a volume" |
| **Redundant** | More than `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` (3) snapshots for same source PVC AND this one is not among the N most recent AND age > `ROS_SNAPSHOT_STALE_DAYS` (90) AND not managed | "Newer snapshot exists for the same PVC" |
| **Stale** | Age > `ROS_SNAPSHOT_STALE_DAYS` (90) AND never restored AND not managed | "Snapshot older than retention threshold with no known usage" |
| **Managed** | Labels indicate a backup tool manages this snapshot | "Snapshot is managed by [tool] — review retention policy for cost optimization" |
| **Active** | Age < `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` (7) OR `restored_pvc_count > 0` | No notification |

### Redundant Detection Details

The redundant classification avoids flagging intentional retention windows
(e.g., 7 daily snapshots kept by a cron job). A snapshot is only flagged
redundant when ALL of these are true:

1. There are more than `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` snapshots for the
   same `source_pvc_name` within the same namespace
2. This snapshot is NOT among the N most recent (where N = threshold)
3. This snapshot is older than `ROS_SNAPSHOT_STALE_DAYS`
4. This snapshot is not managed by a known backup tool

This means a team keeping 7 recent daily snapshots gets zero noise (all are
within the staleness window), while a team with 50 accumulating snapshots
spanning 2 years gets redundancy alerts on the old ones beyond their retention
count.

### Managed Snapshot Detection

Rather than excluding backup-managed snapshots, we classify them as
**"managed"** and include them in results with appropriate context. This
ensures full visibility while avoiding false "stale" alerts.

Detection is based on well-known labels:

| Tool | Label prefix |
|------|-------------|
| Velero | `velero.io/backup-name` |
| Kasten K10 | `k10.kasten.io/` |
| OpenShift Backup | `backup.openshift.io/` |
| Trilio | `triliovault.trilio.io/` |
| Stash/KubeStash | `stash.appscode.com/` |

The backend checks if any label key on the snapshot matches these prefixes.
If so, it extracts the tool name and includes it in the notification message.

This approach:
- Shows everything (nothing hidden from the user)
- Calculates `estimated_monthly_cost_usd` for managed snapshots too (useful
  for understanding the total cost of backup retention policies)
- Gives actionable context ("you have 200 GiB of Velero snapshots costing
  $10/month — is your 90-day retention policy still appropriate?")
- Avoids noisy false positives on properly-managed snapshots

### Empty `source_pvc_name` Handling

VolumeSnapshots created from pre-provisioned VolumeSnapshotContent may have
no source PVC. When `source_pvc_name` is empty:

- **Skip:** Orphaned (can't check if source PVC exists) and Redundant (can't
  group by source PVC)
- **Still apply:** Stale, Never-restored, Managed, Active

These snapshots still appear in the API with `source_pvc_name: ""` and can
still receive stale/never-restored notifications. They are not hidden.

### Classification Priority

When multiple classifications apply, use this precedence (highest first):

1. Orphaned (source PVC deleted — strongest signal, even for managed snapshots)
2. Managed (backup tool detected — supersedes stale/redundant/never-restored)
3. Redundant (newer snapshot supersedes)
4. Stale (age threshold exceeded)
5. Never restored (informational)
6. Active (no action)

### Configuration

All classification thresholds are user-configurable via API. If the
corresponding environment variable is set, the value becomes **read-only**
(the API returns `403 Forbidden` on PUT attempts). This allows operators to
lock down thresholds in managed deployments while letting self-managed users
tune them.

| Setting | Env Variable (locks value) | Default | API key | Description |
|---------|---------------------------|---------|---------|-------------|
| Orphan age | `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | 7 | `orphan_age_days` | Min age before orphaned; also "active" ceiling |
| Never-restored age | `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | 30 | `never_restored_days` | Min age for "never restored" |
| Staleness threshold | `ROS_SNAPSHOT_STALE_DAYS` | 90 | `stale_days` | Age threshold for staleness and redundant filter |
| Redundant threshold | `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | 3 | `redundant_threshold` | Max snapshots to keep per PVC before flagging |
| Inventory retention | `ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS` | 48 | (not exposed) | Raw inventory row retention (operational) |

### Settings API

```
GET  /api/cost-management/v1/recommendations/openshift/settings/snapshot
PUT  /api/cost-management/v1/recommendations/openshift/settings/snapshot
```

**GET response:**
```json
{
  "orphan_age_days": 7,
  "never_restored_days": 30,
  "stale_days": 90,
  "redundant_threshold": 3,
  "cost_per_gib_month_usd": 0.05,
  "locked_fields": ["stale_days"]
}
```

`locked_fields` lists any settings whose value was set via environment variable
and cannot be changed via API.

**PUT request** (partial updates allowed):
```json
{
  "orphan_age_days": 14,
  "cost_per_gib_month_usd": 0.02
}
```

Returns `403` with an error message if any requested field is in `locked_fields`.

### Resolution Order

1. If the env variable is set → use that value (locked, API returns it as read-only)
2. If the user has set a value via API → use that (stored in `snapshot_settings`)
3. Otherwise → use the compiled-in default

## Cost Rate Configuration

Snapshot cost estimation is based on `restore_size_bytes` multiplied by a
configurable per-GiB monthly rate. The rate is **user-configurable** — not
hardcoded — because actual costs vary significantly by provider and storage
tier:

- AWS EBS snapshots: ~$0.05/GiB/month (incremental after first)
- Azure Managed Disk: ~$0.05/GiB/month
- Ceph/ODF (on-prem): Near-zero for COW snapshots with low churn; user
  should set based on their actual pool economics

### Per-StorageClass Cost Rates (v2 — Not in Initial Implementation)

Different StorageClasses have different snapshot costs (e.g., gp3 vs io2 on
AWS, SSD vs HDD). The initial implementation uses a single org-wide rate.

**How we can distinguish StorageClasses in the future:**

The CSV already includes both `storageclass` (from the source PVC's
`.spec.storageClassName`) and `volume_snapshot_class` (from the snapshot's
`.spec.volumeSnapshotClassName`). These are first-class Kubernetes fields —
no user labeling required. The operator resolves them directly:

- `storageclass`: `PVC.spec.storageClassName` (e.g., `gp3`, `ceph-rbd-ssd`)
- `volume_snapshot_class`: `VolumeSnapshot.spec.volumeSnapshotClassName`
  (e.g., `csi-aws-vsc`, `ocs-storagecluster-rbdplugin-snapclass`)

For v2, the cost rate API could accept per-`volume_snapshot_class` rates:

```json
{
  "default_cost_per_gib_month_usd": 0.05,
  "overrides": {
    "csi-aws-vsc-io2": 0.10,
    "ocs-storagecluster-rbdplugin-snapclass": 0.001
  }
}
```

This eliminates the need for user-applied labels. The `volume_snapshot_class`
is the correct abstraction because snapshot cost is determined by the CSI
driver (which the VolumeSnapshotClass encapsulates), not by the PVC's
StorageClass alone.

### Default Rate

`ROS_SNAPSHOT_COST_PER_GIB_MONTH` env var, default: `0.05`

The cost rate is managed through the unified settings API (see "Settings API"
above). When set via env var, it appears in `locked_fields` and cannot be
changed via API. The setting is stored per-org in `snapshot_settings`.

### Cost Formula

```
estimated_monthly_cost_usd = (restore_size_bytes / 1073741824) * cost_per_gib_month_usd
```

The API response includes a note that this is a ceiling estimate for
providers with incremental snapshot behavior.

## Database Tables

### `snapshot_inventory`

Raw inventory data ingested from the operator CSV. One row per snapshot per
ingestion cycle.

**Retention policy:** Keep only 48 hours of inventory data (8 captures at
6-hour cadence). Classification only needs the latest ingestion; older rows
are purged during the retention sweep. This prevents unbounded growth while
providing buffer for late delivery and re-processing.

> **Important:** This retention applies ONLY to raw staging rows. The
> `snapshot_recommendation_sets` table (which the API queries) persists
> indefinitely — one row per snapshot, updated via UPSERT on each ingestion.
> Recommendations disappear only when the snapshot is no longer reported in
> the inventory (i.e., it was deleted from the cluster).

```sql
CREATE TABLE snapshot_inventory (
    id                  BIGSERIAL,
    ingested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    snapshot_name       TEXT NOT NULL,
    source_pvc_name     TEXT NOT NULL DEFAULT '',
    volume_snapshot_class TEXT NOT NULL DEFAULT '',
    storageclass        TEXT NOT NULL DEFAULT '',
    creation_timestamp  TIMESTAMPTZ NOT NULL,
    ready_to_use        BOOLEAN NOT NULL DEFAULT false,
    restore_size_bytes  BIGINT NOT NULL DEFAULT 0,
    source_pvc_exists   BOOLEAN NOT NULL DEFAULT true,
    restored_pvc_count  INT NOT NULL DEFAULT 0,
    labels              JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (id)
);

CREATE INDEX idx_snapshot_inventory_lookup
    ON snapshot_inventory (org_id, cluster_uuid, namespace, snapshot_name);
CREATE INDEX idx_snapshot_inventory_ingested
    ON snapshot_inventory (ingested_at);
```

Retention sweep (runs with the existing retention ticker):

```sql
DELETE FROM snapshot_inventory WHERE ingested_at < NOW() - INTERVAL '48 hours';
```

### `snapshot_recommendation_sets`

Classified snapshot recommendations. One row per snapshot per cluster.

**Removal policy:** Recommendations are removed via active reconciliation
during each ingestion cycle. After classifying the latest inventory for a
cluster, any recommendation whose snapshot is no longer present in the
inventory is deleted:

```sql
DELETE FROM snapshot_recommendation_sets
WHERE org_id = :org_id AND cluster_uuid = :cluster_uuid
AND (namespace, snapshot_name) NOT IN (
    SELECT DISTINCT namespace, snapshot_name
    FROM snapshot_inventory
    WHERE org_id = :org_id AND cluster_uuid = :cluster_uuid
    AND ingested_at >= NOW() - INTERVAL '6 hours'
);
```

**User-facing timeline:** User deletes snapshot → next operator upload
(≤6 hours) omits it → next ingestion reconciles → recommendation removed
from API.

**Edge cases:**
- Operator temporarily down: no ingestion = no reconciliation = recommendations
  persist (correct — don't delete on silence)
- Cluster stops reporting entirely: recommendations stay until the cluster is
  deregistered or a stale-cluster sweep fires (parallels F55 container staleness)

```sql
CREATE TABLE snapshot_recommendation_sets (
    id                  BIGSERIAL PRIMARY KEY,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    snapshot_name       TEXT NOT NULL,
    source_pvc_name     TEXT NOT NULL DEFAULT '',
    volume_snapshot_class TEXT NOT NULL DEFAULT '',
    storageclass        TEXT NOT NULL DEFAULT '',
    creation_timestamp  TIMESTAMPTZ NOT NULL,
    restore_size_bytes  BIGINT NOT NULL DEFAULT 0,
    age_days            INT NOT NULL DEFAULT 0,
    source_pvc_exists   BOOLEAN NOT NULL DEFAULT true,
    restored_pvc_count  INT NOT NULL DEFAULT 0,
    managed_by          TEXT NOT NULL DEFAULT '',
    recommendation_type TEXT NOT NULL DEFAULT '',
    estimated_monthly_cost_usd REAL,
    notification_codes  SMALLINT[] NOT NULL DEFAULT '{}',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, cluster_uuid, namespace, snapshot_name)
);
```

### `snapshot_settings` (per-org)

Stores user-configurable thresholds and cost rate. All columns have defaults
matching the compiled-in values. A row is created on first PUT.

```sql
CREATE TABLE snapshot_settings (
    org_id                  TEXT PRIMARY KEY,
    orphan_age_days         INT NOT NULL DEFAULT 7,
    never_restored_days     INT NOT NULL DEFAULT 30,
    stale_days              INT NOT NULL DEFAULT 90,
    redundant_threshold     INT NOT NULL DEFAULT 3,
    cost_per_gib_month_usd  REAL NOT NULL DEFAULT 0.05,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

The settings API reads from this table, falling back to defaults when no row
exists. Environment variables override any stored value and mark the field as
locked (read-only via API).

## API Endpoints

### List Snapshot Recommendations

`GET /api/cost-management/v1/recommendations/openshift/snapshots`

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `cluster_uuid` | UUID | Filter by cluster |
| `namespace` | string | Filter by namespace |
| `recommendation_type` | enum | `orphaned`, `never_restored`, `redundant`, `stale`, `managed`, `active` |
| `limit` | int | Results per page (1-100, default 20) |
| `offset` | int | Pagination offset |

#### Response Shape

```json
{
  "meta": { "count": 2, "limit": 20, "offset": 0 },
  "data": [
    {
      "cluster_uuid": "aaaaaaaa-...",
      "namespace": "production",
      "snapshot_name": "db-backup-2025-12-01",
      "source_pvc_name": "postgres-data",
      "volume_snapshot_class": "csi-aws-vsc",
      "storageclass": "gp3",
      "creation_timestamp": "2025-12-01T03:00:00Z",
      "restore_size_bytes": 53687091200,
      "age_days": 158,
      "source_pvc_exists": true,
      "restored_pvc_count": 0,
      "managed_by": "",
      "recommendation_type": "stale",
      "estimated_monthly_cost_usd": 2.50,
      "notifications": {
        "34": {
          "type": "INFO",
          "message": "Snapshot older than retention threshold with no known usage",
          "code": 34
        }
      }
    },
    {
      "cluster_uuid": "aaaaaaaa-...",
      "namespace": "production",
      "snapshot_name": "velero-daily-2026-04-15",
      "source_pvc_name": "postgres-data",
      "volume_snapshot_class": "csi-aws-vsc",
      "storageclass": "gp3",
      "creation_timestamp": "2026-04-15T02:00:00Z",
      "restore_size_bytes": 53687091200,
      "age_days": 23,
      "source_pvc_exists": true,
      "restored_pvc_count": 0,
      "managed_by": "Velero",
      "recommendation_type": "managed",
      "estimated_monthly_cost_usd": 2.50,
      "notifications": {
        "35": {
          "type": "INFO",
          "message": "Snapshot is managed by Velero — review retention policy for cost optimization",
          "code": 35
        }
      }
    }
  ]
}
```

### Get/Set Snapshot Settings

See "Settings API" in the Configuration section above. The unified endpoint
covers both classification thresholds and cost rate.

## Notification Codes (Proposed)

Current allocation: codes 1-30 are in use (see `internal/engine/notifications.go`).
Snapshot codes continue the sequence:

| Code | Severity | Classification | Message |
|------|----------|----------------|---------|
| 31 | WARNING | Orphaned | Source PVC was deleted; snapshot may no longer be needed |
| 32 | INFO | Never restored | Snapshot has never been used to restore a volume |
| 33 | INFO | Redundant | Newer snapshot exists for the same PVC |
| 34 | INFO | Stale | Snapshot older than retention threshold with no known usage |
| 35 | INFO | Managed | Snapshot is managed by [tool] — review retention policy for cost optimization |

No collision with existing codes. Next available after snapshots: **36**.

## File Routing Architecture

The Koku listener (`kafka_msg_handler.py`) dispatches files from the tarball
based on the manifest:
- `manifest.files` → Koku/masu cost pipeline (not routed to ROS)
- `manifest.resource_optimization_files` → S3 upload → Kafka `hccm.ros.events` → ros-ocp-backend

### Two Strategies (Both Documented)

| Strategy | When to use | Change location | Operator release? |
|----------|-------------|-----------------|-------------------|
| **A: Listener-side routing** | Existing CSVs already in `manifest.files` (e.g. storage-usage) | `kafka_msg_handler.py` in Koku | No |
| **B: Manifest-side (operator)** | New CSVs designed for ROS from day one (e.g. snapshot-inventory) | Operator's manifest builder | Yes |

**Strategy A** is the short-term fix for F27 PVC right-sizing: the storage CSV
already exists in `manifest.files` and we want it to also reach ros-ocp-backend
without waiting for an operator release. Implemented in
`koku/masu/external/kafka_msg_handler.py`.

**Strategy B** is the long-term correct approach for new files like snapshot
inventory: the operator places them directly in `resource_optimization_files`,
so the existing listener pipeline routes them automatically.

### Alternatives Considered and Rejected

| Alternative | Reason rejected |
|-------------|----------------|
| Send ALL tarball files to ros-ocp-backend | Floods ROS with irrelevant CSVs (node labels, pod labels, etc.) |
| Internal masu API endpoint for PVC data | Adds runtime coupling, auth complexity, and latency |
| Duplicate file in both manifest arrays | Requires operator release for an already-existing file |

### Strategy A: Koku Listener Change (Implemented for F27)

In `koku/masu/external/kafka_msg_handler.py`, after building `ros_reports` from
`manifest.resource_optimization_files`, also include matching cost-pipeline
files:

```python
_ros_extra_patterns = ("storage-usage",)
ros_reports.extend(
    (f, payload_path.with_name(f))
    for f in manifest_files
    if any(pat in f for pat in _ros_extra_patterns) and f in payload_files
)
```

This makes F27 functional immediately for all existing clusters. The pattern
tuple can be extended for future files if needed (though Strategy B is
preferred for new files).

### Strategy B: Operator Manifest Placement (For Snapshots)

The operator places `cm-openshift-snapshot-inventory-*.csv` in the
`resource_optimization_files` array of `manifest.json`. Since this is a new
file requiring an operator change anyway, we design it correctly from day one.

## Pipeline Flow

```
Operator (new VolumeSnapshot collector, runs once per upload cycle)
    |
    v
cm-openshift-snapshot-inventory-YYYYMM.csv
    (in resource_optimization_files array of manifest.json)
    |
    v
Koku listener: uploads to S3, produces Kafka message on hccm.ros.events
    |
    v
ros-ocp-backend: DetermineCSVType() detects "snapshot"
    |
    v
ParseSnapshotRows() -> UpsertSnapshotInventory()
    |
    v
ClassifySnapshots() -> apply cost rate -> WriteSnapshotRecommendations()
    |
    v
GET /recommendations/openshift/snapshots
```

## Implementation Phases

### Phase 1: Nise (fake data generator)

Add snapshot inventory generation to nise so the backend can be developed
and tested without a real cluster or operator changes.

**Changes to `nise/generators/ocp/ocp_generator.py`:**

- Add `OCP_SNAPSHOT_INVENTORY` report type constant
- Add `OCP_SNAPSHOT_INVENTORY_COLUMNS` tuple matching the CSV schema
- Add to `COST_OCP_REPORT_TYPE_TO_COLS` mapping
- Implement `_gen_snapshot_inventory()` that produces rows with:
  - Realistic snapshot names (e.g., `db-backup-YYYY-MM-DD`, `velero-daily-YYYY-MM-DD`)
  - Mix of classifications: some orphaned (source PVC deleted), some with
    Velero/K10 labels, some old-and-never-restored, some active
  - Varying `restore_size_bytes` (1-100 GiB range)
  - `creation_timestamp` spanning 1-180 days ago
  - `restored_pvc_count` mostly 0, some with 1-2

**New static YAML support** (optional, for `--static-report-file`):

```yaml
snapshots:
  - snapshot_name: db-daily-backup
    source_pvc_name: postgres-data
    storageclass: gp3
    volume_snapshot_class: csi-aws-vsc
    restore_size_gb: 50
    creation_days_ago: 120
    source_pvc_exists: true
    restored_pvc_count: 0
    labels:
      velero.io/backup-name: daily-backup-schedule
  - snapshot_name: orphaned-snapshot
    source_pvc_name: deleted-app-data
    storageclass: gp3
    volume_snapshot_class: csi-aws-vsc
    restore_size_gb: 20
    creation_days_ago: 45
    source_pvc_exists: false
    restored_pvc_count: 0
    labels: {}
```

**Output file:** `cm-openshift-snapshot-inventory-YYYYMM.csv`

This enables E2E testing of the full pipeline (nise → tarball → ingest →
classify → API) without waiting for the operator implementation.

### Phase 2: Operator

- Add `VolumeSnapshot` collector to koku-metrics-operator
- Gracefully skip if `snapshot.storage.k8s.io` CRD is not installed
- Write `cm-openshift-snapshot-inventory-YYYYMM.csv`
- Include in manifest `resource_optimization_files` array (ensures automatic
  routing to ros-ocp-backend via the existing Koku listener → S3 → Kafka path)
- Collect once per upload cycle (default every 6 hours)

### Phase 3: Backend

- Add `PayloadTypeSnapshot` to `DetermineCSVType()`
- Create migrations for `snapshot_inventory`, `snapshot_recommendation_sets`,
  and `snapshot_settings`
- Implement CSV parser, classification engine (including managed detection),
  cost calculation, API handler
- Add notification codes 31-35
- Add unified settings API (GET/PUT with env-var locking)
- Follow the same pattern as PVC right-sizing (F27)

### Phase 4: Testing

- Unit tests for classification logic (all 6 types)
- Unit tests for managed-snapshot label detection
- Integration test with testcontainers (full pipeline)
- IQE test: generate data with nise, upload tarball, verify API returns
  classified snapshots with correct types and cost estimates

### Phase 5: UI (optional)

- New tab or section in the ROS recommendations view
- Show snapshot name, age, classification, estimated cost, managed-by tool
- Settings page for snapshot cost rate

## Limitations and Considerations

1. **No restore history**: Kubernetes does not track historical restores.
   `restored_pvc_count` only reflects *currently existing* PVCs with
   `dataSource` pointing to the snapshot. If a PVC was restored and later
   deleted, we won't know about it.

2. **Incremental snapshots**: On AWS EBS, only the first snapshot is full;
   subsequent snapshots are incremental. The cost estimate based on
   `restoreSize` is a ceiling, not exact. The API response should note this.

3. **Ceph/ODF COW semantics**: Ceph RBD snapshots are Copy-on-Write and may
   consume near-zero additional space if the source volume hasn't been
   written to. On-prem users should set the cost rate to reflect their actual
   pool economics (possibly `0.00` or a very low value).

4. **Cross-namespace restores**: A snapshot can technically be restored in a
   different namespace (via VolumeSnapshotContent). Our `restored_pvc_count`
   only checks the same namespace. This is a known limitation.

5. **VolumeSnapshotContent**: We focus on namespace-scoped `VolumeSnapshot`
   objects, not cluster-scoped `VolumeSnapshotContent`. The latter would
   require cluster-admin permissions.

6. **CRD availability**: Not all clusters have the `snapshot.storage.k8s.io`
   CRDs installed. The operator must detect this and skip collection
   gracefully rather than erroring.

7. **Backup tool label evolution**: New backup tools or label conventions may
   emerge. The managed-detection logic should be configurable or at least
   easy to extend (a simple list of label prefixes).

8. **Per-StorageClass cost granularity**: v1 uses a single org-wide cost rate.
   Clusters with mixed storage tiers (e.g., gp3 + io2) will get approximate
   cost estimates. The `volume_snapshot_class` field is already collected and
   can be used for per-class rates in v2 without additional operator changes.

9. **Koku cost model gap — per-StorageClass rates**: The OpenShift cost model
   in Koku only supports a single `storage_gb_usage_per_month` /
   `storage_gb_request_per_month` rate across all StorageClasses. The only way
   to differentiate is via tag-based rates (user labels PVCs with a tag key
   like `storageclass` and defines tag rates per value). This is a known gap
   in the Koku cost model system — it has no native concept of per-StorageClass
   pricing. For snapshots, we avoid this gap entirely by collecting
   `volume_snapshot_class` as a first-class field and supporting per-class cost
   rates in the v2 API (no tag labeling needed). A similar approach could
   eventually be adopted for PVC cost models in Koku itself (using
   `PVC.spec.storageClassName` directly instead of requiring user-applied tags).

10. **E2E test infrastructure**: Integration testing on real clusters requires
   a CSI driver with snapshot support (the `snapshot.storage.k8s.io` CRDs and
   a VolumeSnapshotClass must exist). Not all test environments provide this.
   Backend unit/integration tests use nise-generated data and testcontainers,
   so they work anywhere. Operator E2E tests specifically need a cluster with
   CSI snapshot capability (e.g., AWS EBS CSI, ODF/Ceph CSI, or hostpath CSI
   provisioner for CI).
