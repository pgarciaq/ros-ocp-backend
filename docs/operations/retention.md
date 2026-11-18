# Retention Policy

This document describes how ROS-OCP-Backend manages data lifecycle and cleanup.

## Overview

The retention system runs as a background goroutine (`StartRetentionTicker`) that executes a full sweep immediately on startup and then every **24 hours**. It cleans five categories of data:

1. **Partitioned digest tables** — monthly partitions older than `ROS_RETENTION_MONTHS`
2. **History/quality tables** — monthly partitions older than `ROS_HISTORY_RETENTION_DAYS`
3. **Abandoned clusters** — rows of clusters whose last report is older than `ROS_CLUSTER_LIVENESS_HOURS`, marked `stale = true` (reversible; the next report clears it)
4. **Stale recommendations** — rows marked stale longer ago than `ROS_STALE_CLEANUP_DAYS`, deleted after a delete-time liveness re-check
5. **Snapshot inventory** — raw snapshot rows older than `ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS`

> **Note:** The raw usage sample tables (`container_usage_samples`, `namespace_usage_samples`)
> and the `ROS_SAMPLE_RETENTION_DAYS` setting were removed in migration 000172. Writes were
> disabled in Phase 1 (#258) and the tables dropped in Phase 2. Detail API plots read daily
> digests only (ADR-0292).

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `ROS_RETENTION_MONTHS` | 6 | Months to retain digest partitions |
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Days to retain recommendation history and quality data |
| `ROS_CLUSTER_LIVENESS_HOURS` | 336 | Hours without a report before a cluster's recommendations are marked stale (14 days) |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Days after the mark before stale recommendations are deleted |
| `ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS` | 48 | Hours to retain raw snapshot inventory rows |

`ROS_CLUSTER_LIVENESS_HOURS` and `ROS_MAX_LOOKBACK_DAYS` are independent: the first decides when
a silent cluster is hidden, the second only bounds the engine's digest lookback window. Do not
raise one to fix the other.

## Tables Swept

### Main digest tables (monthly partitions, retained by `ROS_RETENTION_MONTHS`)

| Table | Content |
|-------|---------|
| `daily_container_digests` | Daily aggregated container metrics |
| `daily_namespace_digests` | Daily aggregated namespace metrics |
| `daily_node_digests` | Daily aggregated node utilization |
| `gpu_container_digests` | Daily GPU profiling metrics |

### History tables (monthly partitions, retained by `ROS_HISTORY_RETENTION_DAYS`)

| Table | Content |
|-------|---------|
| `recommendation_history` | Point-in-time recommendation snapshots |
| `recommendation_quality` | Stability and adoption tracking |

### Non-partitioned tables (date-based DELETE)

| Table | Column | Retention |
|-------|--------|-----------|
| `historical_namespace_recommendation_sets` | `created_at` | `ROS_RETENTION_MONTHS` |
| `node_recommendations` | `updated_at` | `ROS_RETENTION_MONTHS` |
| `namespace_recommendation_sets` | `updated_at` | `ROS_RETENTION_MONTHS` (backstop; the stale purge below usually fires first) |
| `pvc_recommendation_sets` | `updated_at` | `ROS_RETENTION_MONTHS` |
| `hcp_sleep_evidence` | `bucket_date` | `ROS_RETENTION_MONTHS` |

Rows deleted from recommendation tables invalidate the per-org fleet summary cache for affected tenants.

## Partition Naming Convention

Partitions are named `<parent_table>_YYYYMM` (e.g., `daily_container_digests_202601`). The retention sweep compares the YYYYMM suffix against the cutoff date and drops partitions that fall entirely before it.

## Abandoned Cluster Marking

A cluster that stops reporting never runs its own processing cycle, so nothing inside that
cycle can mark its rows stale. The sweep is the only actor that notices, and it runs before
the purge so a long-dead cluster is marked and reclaimed within the same sweep.

| Step | Key | Effect |
|------|-----|--------|
| Mark | every `clusters` row for `(org_id, cluster_uuid)` has `last_reported_at` older than `ROS_CLUSTER_LIVENESS_HOURS` | `stale = true`, `staled_at = now()` on `recommendation_sets` and `namespace_recommendation_sets`; the org's key catalogs and caches refresh so list counts match the rows returned |
| Reversal | the cluster reports again | the write path's `ON CONFLICT DO UPDATE` clears `stale` and `staled_at`; nothing is lost |

Marking never touches `updated_at` — that column stays the report time, because a container
that merely missed a cycle must not start claiming `last_reported: today`. A cluster registered
but never reported (`last_reported_at IS NULL`) is not marked: it is unknown, not abandoned, and
marking it would make a fresh registration invisible.

## Stale Recommendation Cleanup

Rows with `stale = true` whose mark (`staled_at`) is older than `ROS_STALE_CLEANUP_DAYS` are
permanently deleted from both `recommendation_sets` and `namespace_recommendation_sets`. A row
marked before `staled_at` existed keeps its original deadline:
`COALESCE(staled_at, updated_at)`.

The delete re-verifies liveness per row with a `NOT EXISTS` probe on `clusters`: a cluster that
is in fact reporting survives a mistaken mark, while a cluster whose `clusters` row is gone (the
Sources-destroy path) leaves an empty probe and is reclaimed. So with defaults a decommissioned
cluster disappears from default lists and fleet counts at day 14, stays visible to
`filter[stale]=only`, and is deleted at day 44.

## Plugin Integration

When recommendation plugins are registered (production binaries import `internal/plugins`), each plugin sweeps its own tables via the `RetentionProvider` trait. In test/CLI binaries without plugin imports, the fallback `retainedTables` slice covers all known partitioned tables. The overlap is harmless (DROP IF EXISTS).

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `rosocp_retention_partitions_dropped_total` | Counter | Cumulative partitions/rows dropped (also counts purged stale recommendation rows) |
| `rosocp_retention_rows_marked_stale_total` | Counter | Recommendation rows marked stale because their cluster stopped reporting |

Both counters are incremented for the work that committed even when a later table in the same
sweep fails.

## Operational Notes

- Retention runs are non-blocking; errors are logged but do not crash the process
- Each table is swept independently — one failure does not prevent others from being cleaned
- Partition drops are instantaneous (DDL, not row-by-row deletes)
- The 24-hour ticker is not configurable; restart the process to force an immediate sweep
- The mark pass has no index support by design: it must consider every non-stale row to find
  the ones whose cluster went silent, so it is a sequential scan once per day against a hash
  join of `clusters`. The purge, by contrast, drives off
  `idx_ros_rs_org_cluster_stale_updated_at` with an index condition on `stale = true`.
