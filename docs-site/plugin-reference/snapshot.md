# snapshot

> **Last verified:** 2026-08-06

Package: [`internal/plugins/snapshot`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/snapshot)

**Snapshot staleness** — detects VolumeSnapshots that are orphaned, unused, redundant, stale, or backup-managed, and surfaces cleanup opportunities from operator inventory CSVs.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `snapshot` |
| Phase | 1 (Produce) |
| Priority | 40 |
| CSV types | `snapshot-inventory` (`ocp_snapshot_inventory.csv`, `ros-openshift-snapshot-inventory-*.csv`, `cm-openshift-snapshot-inventory-*.csv`) |
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

List and summary filters include `filter[cluster]`, `filter[project]`, and
`filter[recommendation_type]` (`orphaned`, `never_restored`, `redundant`, `stale`,
`managed`, `active`). On the summary endpoint, `filter[recommendation_type]` restricts
which snapshots are aggregated into each group (counts and reclaimable totals reflect
only matching rows).

**Tag filtering is not supported** on snapshot list or summary endpoints (`filter[tag:*]` is ignored).
Use container, namespace, or PVC routes for label-based filtering.

Handlers: [`GetSnapshotRecommendations`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot.go),
[`GetSnapshotSummary`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot_summary.go).

### List pagination and export

- **Keyset pagination:** `?after=<meta.next_cursor>` with `meta.has_next` (default sort `age_days` DESC). `offset` remains supported for backward compatibility. See [API pagination](../pagination.md).
- **CSV export:** `?format=csv` or `Accept: text/csv` — columns include `classification`, `estimated_monthly_cost_value` / `estimated_monthly_cost_units`, `created_at`, `last_reported`, and notification codes.
- **Sort:** `order_by` (`age_days`, `restore_size_bytes`, `estimated_monthly_cost`, `snapshot_name`, `namespace`, `recommendation_type`) and `order_how` (`asc` / `desc`).

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

`estimated_monthly_cost` is a [`MoneyAmount`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/money/format.go) (`{"value": "12.50", "units": "USD"}`), persisted as `estimated_cost_cents` (BIGINT) in `snapshot_recommendation_sets`. The rate comes from resolved `cost_per_gib_month_usd` (Settings API, env, or compiled default). When `ROS_SAVINGS_ESTIMATES_ENABLED=false`, Masu effective-rates lookup is skipped during ingestion; dollar fields still use the static or per-org configured rate.

Snapshot totals are included in fleet `GET /recommendations/openshift/savings-summary` when the plugin is enabled and cost data exists. Snapshot's fleet contribution is **term-independent** — all snapshot recommendations are summed regardless of the `term` query parameter.

## Architecture

- [Snapshot staleness (feature)](../features/snapshot-staleness.md)
- [Configurability — Snapshot](../architecture/configurability.md#snapshot)
- Design reference: [`docs/features-f-snapshot-staleness.md`](../features/snapshot-staleness.md)
