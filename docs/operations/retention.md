# Retention Policy

This document describes how ROS-OCP-Backend manages data lifecycle and cleanup.

## Overview

The retention system runs as a background goroutine (`StartRetentionTicker`) that executes a full sweep immediately on startup and then every **24 hours**. It cleans five categories of data:

1. **Partitioned digest tables** — monthly partitions older than `ROS_RETENTION_MONTHS`
2. **Partitioned sample tables** — monthly partitions older than `ROS_SAMPLE_RETENTION_DAYS` (separate, shorter window)
3. **History/quality tables** — monthly partitions older than `ROS_HISTORY_RETENTION_DAYS`
4. **Stale recommendations** — recommendation_sets marked `stale = true` older than `ROS_STALE_CLEANUP_DAYS`
5. **Snapshot inventory** — raw snapshot rows older than `ROS_SNAPSHOT_INVENTORY_RETENTION_H`

### Two-tier retention (digests vs samples)

Recommendations read **daily digests** (`daily_container_digests`, `daily_namespace_digests`) for percentile sizing and detail **plots** (p50/p95/p99/max per time bucket). Raw **usage samples** (`container_usage_samples`, `namespace_usage_samples`) are retained on a shorter schedule because they dominate disk usage and are no longer required for plot assembly after E-2.

| Tier | Tables | Control | Default | Purpose |
|------|--------|---------|---------|---------|
| Digests | `daily_*_digests`, `gpu_container_digests`, … | `ROS_RETENTION_MONTHS` | 6 months | Recommendation engine + digest-based plots |
| Samples | `container_usage_samples`, `namespace_usage_samples` | `ROS_SAMPLE_RETENTION_DAYS` | 45 days | Optional drill-down / legacy tooling; swept independently |

Align `ROS_SAMPLE_RETENTION_DAYS` with the longest plot window you need from raw samples. Detail API plots use digests and remain available while digest partitions exist.

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `ROS_RETENTION_MONTHS` | 6 | Months to retain digest partitions |
| `ROS_SAMPLE_RETENTION_DAYS` | 45 | Days to retain raw usage sample partitions |
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Days to retain recommendation history and quality data |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Days before stale recommendations are deleted |
| `ROS_SNAPSHOT_INVENTORY_RETENTION_H` | 48 | Hours to retain raw snapshot inventory rows |

## Tables Swept

### Main digest tables (monthly partitions, retained by `ROS_RETENTION_MONTHS`)

| Table | Content |
|-------|---------|
| `daily_container_digests` | Daily aggregated container metrics |
| `daily_namespace_digests` | Daily aggregated namespace metrics |
| `daily_node_digests` | Daily aggregated node utilization |
| `gpu_container_digests` | Daily GPU profiling metrics |

### Sample tables (monthly partitions, retained by `ROS_SAMPLE_RETENTION_DAYS`)

| Table | Content |
|-------|---------|
| `container_usage_samples` | Raw per-interval CPU/memory samples |
| `namespace_usage_samples` | Raw namespace-level samples |

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
| `namespace_recommendation_sets` | `updated_at` | `ROS_RETENTION_MONTHS` |
| `pvc_recommendation_sets` | `updated_at` | `ROS_RETENTION_MONTHS` |

Rows deleted from recommendation tables invalidate the per-org fleet summary cache for affected tenants.

## Partition Naming Convention

Partitions are named `<parent_table>_YYYYMM` (e.g., `daily_container_digests_202601`). The retention sweep compares the YYYYMM suffix against the cutoff date and drops partitions that fall entirely before it.

## Stale Recommendation Cleanup

Recommendations in `recommendation_sets` with `stale = true` AND `updated_at` older than `ROS_STALE_CLEANUP_DAYS` are permanently deleted. This prevents indefinite accumulation of recommendations for decommissioned workloads.

## Plugin Integration

When recommendation plugins are registered (production binaries import `internal/plugins`), each plugin sweeps its own tables via the `RetentionProvider` trait. In test/CLI binaries without plugin imports, the fallback `retainedTables` slice covers all known partitioned tables. The overlap is harmless (DROP IF EXISTS).

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `rosocp_retention_partitions_dropped_total` | Counter | Cumulative partitions/rows dropped |

## Operational Notes

- Retention runs are non-blocking; errors are logged but do not crash the process
- Each table is swept independently — one failure does not prevent others from being cleaned
- Partition drops are instantaneous (DDL, not row-by-row deletes)
- The 24-hour ticker is not configurable; restart the process to force an immediate sweep
