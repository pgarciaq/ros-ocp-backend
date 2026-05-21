# Stale Detection Algorithm

This document describes how ROS-OCP-Backend identifies stale recommendations.

## Definition

A recommendation is **stale** when its underlying data has not been refreshed within the configured staleness threshold. This indicates the source cluster has stopped reporting metrics (operator disabled, cluster decommissioned, network issues, etc.).

## Algorithm

### Detection (at recommendation write time)

During each ingestion cycle, when `RecommendWorkloadsStreaming` writes recommendation_sets, it also updates the `updated_at` timestamp. The `stale` column is set based on:

```
stale = (now() - last_digest_date) > staleness_threshold
```

Where:
- `last_digest_date` = the most recent `interval_start` in `daily_container_digests` for that container
- `staleness_threshold` = `ROS_STALENESS_THRESHOLD_HOURS` (default: **72 hours** / 3 days)

### API Filter

The `GET /recommendations/openshift` list endpoint supports a `?stale=` query parameter:
- `?stale=true` — only stale recommendations
- `?stale=false` — only fresh recommendations
- (omitted) — all recommendations

### Notification

When a recommendation is marked stale, notification code `STALE_DATA` is appended to the recommendation's `notification_codes` array, surfaced in the API detail response.

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `ROS_STALENESS_THRESHOLD_HOURS` | 72 | Hours without new data before marking stale |
| `ROS_STALE_ARCHIVE_DAYS` | 30 | Days before stale recs are permanently deleted (see [retention.md](retention.md)) |

## Lifecycle

```
Fresh data arriving → updated_at refreshed → stale = false
        ↓ (no data for >72h)
stale = true, NotifStaleData appended
        ↓ (no data for >30 days)
Deleted by retention sweep
```

## Edge Cases

- **Delayed uploads**: A cluster that uploads with a multi-day delay will NOT be marked stale as long as the upload arrives within the threshold. The check uses `updated_at` (when the recommendation was last written), not the CSV timestamp.
- **Re-activation**: If a stale cluster resumes reporting, the next ingestion cycle clears the `stale` flag and removes `NotifStaleData` from notifications.
- **Idle vs stale**: Idle workloads (CPU < 10mc, memory < 10 MiB) are NOT stale — they are actively reporting but have negligible usage. These get `NotifIdle` instead.
