# snapshot

> **Last verified:** 2026-09-20

Package: [`internal/plugins/snapshot`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/snapshot)

**Snapshot staleness** — detects VolumeSnapshots that are orphaned, unused, redundant, stale, or backup-managed, and surfaces cleanup opportunities from operator inventory CSVs.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `snapshot` |
| Phase | 1 (Produce) |
| Priority | 40 |
| CSV types | `snapshot` (snapshot inventory CSV: `ocp_snapshot_inventory.csv`, `ros-openshift-snapshot-inventory-*.csv`, `cm-openshift-snapshot-inventory-*.csv`) |
| Retention tables | (none — inventory reconciled per ingest) |
| Generation gate | Yes — skipped unless `snapshot` enabled (#591), processor and recalc central |

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
GET /api/cost-management/v1/recommendations/openshift/snapshots/age-distribution
GET /api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type
GET|PUT|DELETE /api/cost-management/v1/recommendations/openshift/settings/snapshot
```

Snapshot recommendation quality metrics (adoption-only via disappearance):

```
GET /api/cost-management/v1/recommendations/openshift/quality/snapshots
```

See [Recommendation History & Quality](../features/history-and-quality.md#quality).

The age-distribution and cost-by-type aggregates are Visual Insights endpoints
(gated by `ROS_VISUAL_INSIGHTS_ENABLED`, default `true`; with the gate off the
routes are unregistered and fall through to the detail catch-all → **400**
`bad recommendation_id`, verified live). They are org-scoped aggregates over
`snapshot_recommendation_sets` — no `filter[*]`, `limit`/`offset`, or `order_by`
parameters. Detail: [Age distribution](#age-distribution-histogram) and
[Cost by type](#cost-by-type) below; UI context in
[Snapshot staleness](../features/snapshot-staleness.md) and
[Visual insights](../features/visual-insights.md#snapshots).

List and summary filters include `filter[cluster]`, `filter[project]`, and
`filter[recommendation_type]` (`orphaned`, `never_restored`, `redundant`, `stale`,
`managed`, `active`). On the summary endpoint, `filter[recommendation_type]` restricts
which snapshots are aggregated into each group (counts and reclaimable totals reflect
only matching rows).

**Tag filtering is not supported** on snapshot list or summary endpoints (`filter[tag:*]` is ignored).
Use container, namespace, or PVC routes for label-based filtering.

Handlers: [`GetSnapshotRecommendations`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot.go),
[`GetSnapshotSummary`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot_summary.go),
[`GetSnapshotAgeDistribution`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot_age_distribution.go),
[`GetSnapshotCostByType`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_snapshot_cost_by_type.go).
Routes: [`internal/plugins/snapshot/plugin.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/snapshot/plugin.go)
(`RegisterRoutes` — age-distribution and cost-by-type registered only when
`config.VisualInsightsEnabled()`). Gate default:
[`internal/config/config.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/config/config.go)
(`viper.SetDefault("ROS_VISUAL_INSIGHTS_ENABLED", true)`).
OpenAPI (`openapi.json`) lists both paths with a generic object schema — field
shapes below are second-sourced from the handler structs.

### List pagination and export

- **Keyset pagination:** `?after=<meta.next_cursor>` with `meta.has_next` (default sort `age_days` DESC). `offset` remains supported for backward compatibility. See [API pagination](../pagination.md).
- **CSV export:** `?format=csv` or `Accept: text/csv` — columns include `classification`, `estimated_monthly_cost_value` / `estimated_monthly_cost_units`, `created_at`, `last_reported`, and notification codes.
- **Sort:** `order_by` (`age_days`, `restore_size_bytes`, `estimated_monthly_cost`, `snapshot_name`, `namespace`, `recommendation_type`) and `order_how` (`asc` / `desc`).

### Age distribution (histogram)

```
GET /api/cost-management/v1/recommendations/openshift/snapshots/age-distribution
```

Histogram of snapshot counts grouped by age buckets, computed from `age_days`
in `snapshot_recommendation_sets` for the caller's org. Backs the Visual
Insights age-distribution bar chart.

| Parameter | Type | Description |
|-----------|------|-------------|
| `bucket_boundaries` | string | Optional comma-separated positive integers in strictly ascending order (max 20). Default `7,30,90` → buckets `<7 days`, `7-30 days`, `30-90 days`, `90+ days`. Bucket `i` covers `age_days < boundary[i]` semantics with labels `<N days` / `A-B days` / `N+ days` and `min_days` / `max_days` (`max_days: null` on the unbounded last bucket). |

No `filter[*]`, `limit`/`offset`, or `order_by` — the response always covers the
whole org with `len(boundaries) + 1` buckets. `openapi.json` lists the path with
a generic object schema; the field shape below is second-sourced from
`SnapshotAgeDistributionResponse` in `handlers_snapshot_age_distribution.go`.

Response shape: `{"buckets": [{"label": string, "min_days": int, "max_days": int|null, "count": int}], "total": int}`.
`total` is the sum of bucket counts. Responses carry `Cache-Control: no-store`.

Live example (default boundaries, org `3340851`, 2026-09-14 — 84 snapshots):

```json
{
  "buckets": [
    {"label": "<7 days", "min_days": 0, "max_days": 6, "count": 0},
    {"label": "7-30 days", "min_days": 7, "max_days": 29, "count": 1},
    {"label": "30-90 days", "min_days": 30, "max_days": 89, "count": 14},
    {"label": "90+ days", "min_days": 90, "max_days": null, "count": 69}
  ],
  "total": 84
}
```

Custom boundaries (`?bucket_boundaries=14,60`, same org/data — 84 snapshots):

```json
{
  "buckets": [
    {"label": "<14 days", "min_days": 0, "max_days": 13, "count": 0},
    {"label": "14-60 days", "min_days": 14, "max_days": 59, "count": 7},
    {"label": "60+ days", "min_days": 60, "max_days": null, "count": 77}
  ],
  "total": 84
}
```

| Status | When | Example body |
|--------|------|--------------|
| **200** | Success (including empty org — all `count: 0`, default 4 buckets) | see above |
| **400** | Bad `bucket_boundaries` — recorded live: `?bucket_boundaries=abc` → `{"status":"error","message":"bucket_boundaries must be comma-separated positive integers"}`; `?bucket_boundaries=30,7,90` → `{"status":"error","message":"bucket_boundaries must be in strictly ascending order"}`. Code-verified siblings: non-positive values (`must be positive integers`), duplicates (same ascending-order error), >20 values (`must not exceed 20 values`) | `{"status":"error","message":"..."}` |
| **401** | Missing/invalid `x-rh-identity` — recorded live | `{"message":"Unable to unmarshal X-Rh-Identity into struct"}` |
| **400** | `ROS_VISUAL_INSIGHTS_ENABLED=false` (route unregistered — falls through to the detail catch-all, ADR-0168; recorded live) or `snapshot` plugin disabled (no guard covers these VI-only paths — same fall-through by inspection) | `{"status":"error","message":"bad recommendation_id"}` |
| **503** | Code path only (not triggered live): DB pool unavailable or query/scan failure → `unable to fetch/read snapshot age distribution` | `{"status":"error","message":"unable to fetch snapshot age distribution"}` |

Gating: registered only when `config.VisualInsightsEnabled()` — default `true`
(`ROS_VISUAL_INSIGHTS_ENABLED`). See [Visual insights — Snapshots](../features/visual-insights.md#snapshots).

### Cost by type

```
GET /api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type
```

Snapshot holding cost grouped by `recommendation_type` for the caller's org:
`SUM(estimated_cost_cents)` plus row `count` per type, ordered by
`total_cost_cents` DESC. Backs the Visual Insights cost-by-type donut chart.
No query parameters. `openapi.json` lists the path with a generic object
schema; the field shape below is second-sourced from
`SnapshotCostByTypeItem`/`SnapshotCostByTypeResponse` in
`handlers_snapshot_cost_by_type.go`.

Response shape: `{"data": [{"recommendation_type": string, "total_cost_cents": int, "count": int}]}`.
Empty orgs return `{"data": []}` (never `null`). Responses carry
`Cache-Control: no-store`. The query runs under a heavy-statement timeout;
a timeout surfaces as **503** (code path, not triggered live).

Live example (org `3340851`, 2026-09-14 — 5 types, 84 snapshots):

```json
{
  "data": [
    {"recommendation_type": "redundant", "total_cost_cents": 7535, "count": 34},
    {"recommendation_type": "stale", "total_cost_cents": 4895, "count": 20},
    {"recommendation_type": "active", "total_cost_cents": 3575, "count": 13},
    {"recommendation_type": "never_restored", "total_cost_cents": 2475, "count": 8},
    {"recommendation_type": "managed", "total_cost_cents": 2465, "count": 9}
  ]
}
```

| Status | When | Example body |
|--------|------|--------------|
| **200** | Success (including empty org → `{"data": []}`) | see above |
| **401** | Missing/invalid `x-rh-identity` — recorded live | `{"message":"Unable to unmarshal X-Rh-Identity into struct"}` |
| **400** | `ROS_VISUAL_INSIGHTS_ENABLED=false` (route unregistered — falls through to the detail catch-all, ADR-0168; recorded live) or `snapshot` plugin disabled (no guard covers these VI-only paths — same fall-through by inspection) | `{"status":"error","message":"bad recommendation_id"}` |
| **503** | Code path only (not triggered live): DB pool unavailable, query failure, or heavy-statement timeout → `unable to fetch snapshot cost by type` | `{"status":"error","message":"unable to fetch snapshot cost by type"}` |

Gating: same Visual Insights gate as age-distribution (default on). See
[Visual insights — Snapshots](../features/visual-insights.md#snapshots).

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
