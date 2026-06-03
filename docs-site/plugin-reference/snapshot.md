# snapshot

Package: [`internal/plugins/snapshot`](../../internal/plugins/snapshot/)

**Snapshot staleness** — detects VolumeSnapshots that are orphaned, unused, redundant, stale, or backup-managed, and surfaces cleanup opportunities from operator inventory CSVs.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `snapshot` |
| Phase | 1 (Produce) |
| Priority | 40 |
| CSV types | `snapshot-inventory` (`ocp_snapshot_inventory.csv`) |
| Retention tables | (none — inventory reconciled per ingest) |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — ingests snapshot inventory, classifies, upserts `snapshot_recommendation_sets` |
| APIProvider | Yes — list, namespace/cluster summary, settings |
| TermProvider | No — threshold-based age rules, not short/medium/long windows |

## What it does

On each ingestion cycle, ROS ingests `ocp_snapshot_inventory.csv`, classifies each snapshot (orphaned → managed → redundant → stale → never_restored → active), and removes rows no longer present in the latest inventory. Classifications drive notification codes and reclaimable cost estimates.

## Key settings

| Setting | API field | Env var (default) | Purpose |
|---------|-----------|-------------------|---------|
| Inventory freshness | `inventory_fresh_hours` | `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS` (6) | Hours of recent `snapshot_inventory` rows used for classify/reconcile |
| Orphan age | `orphan_age_days` | `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` (7) | PVC-less snapshot threshold |
| Never restored | `never_restored_days` | `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` (30) | Unused snapshot threshold |
| Stale age | `stale_days` | `ROS_SNAPSHOT_STALE_DAYS` (90) | General staleness gate |
| Redundant cap | `redundant_threshold` | `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` (3) | Max snapshots per PVC before older ones are redundant |
| Cost rate | `cost_per_gib_month_usd` | `ROS_SNAPSHOT_COST_PER_GIB_MONTH` (0.05) | Monthly holding cost per GiB (v1 estimate) |

**Enablement:** Include `snapshot` in `ROS_ENABLED_PLUGINS` (or leave the allowlist empty for all native plugins). Disabled plugins return **404** on snapshot routes. Per-tenant overrides: `GET|PUT|DELETE /recommendations/openshift/settings/snapshot`. See [Configurability — Snapshot](../architecture/configurability.md#snapshot).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/snapshots
GET /api/cost-management/v1/recommendations/openshift/snapshots/summary
GET|PUT|DELETE /api/cost-management/v1/recommendations/openshift/settings/snapshot
```

List filters include `filter[cluster]`, `filter[project]`, and `filter[recommendation_type]` (`orphaned`, `never_restored`, `redundant`, `stale`, `managed`, `active`).

Handlers: [`GetSnapshotRecommendations`](../../internal/api/handlers_snapshot.go), [`GetSnapshotSummary`](../../internal/api/handlers_snapshot.go).

## Notification codes

Filter the catalog: `GET /recommendations/openshift/notification-codes?filter[plugin]=snapshot`.

| Code | Name | When |
|------|------|------|
| **31** | `NotifSnapshotOrphaned` | Source PVC deleted, age > orphan threshold |
| **32** | `NotifSnapshotNeverUsed` | Age > never-restored threshold, never restored |
| **33** | `NotifSnapshotRedundant` | Older snapshot when newer ones exist for same PVC |
| **34** | `NotifSnapshotStale` | Age > stale threshold, never restored |
| **35** | `NotifSnapshotManaged` | Backup-tool annotation (Velero/OADP, etc.) |

`active` classifications emit no snapshot notification code.

See [Notification codes — Snapshots](../architecture/notification-codes.md#snapshots).

## Savings

Snapshot savings use a flat **$0.05/GiB/month** approximation (`cost_per_gib_month_usd`, default aligned with `ROS_SNAPSHOT_COST_PER_GIB_MONTH`). Reclaimable totals appear on list rows and the namespace/cluster **summary** endpoint. Enhanced billing-derived costs are planned in [COST-7523](https://redhat.atlassian.net/browse/COST-7523).

When `ROS_SAVINGS_ESTIMATES_ENABLED=false`, dollar fields are omitted (recommendations still classify).

Snapshot totals are included in fleet `GET /recommendations/openshift/savings-summary` when the plugin is enabled and cost data exists.

## Architecture

- [Snapshot staleness (feature)](../features/snapshot-staleness.md)
- [Configurability — Snapshot](../architecture/configurability.md#snapshot)
- Design reference: [`docs/features-f-snapshot-staleness.md`](../../docs/features-f-snapshot-staleness.md)
