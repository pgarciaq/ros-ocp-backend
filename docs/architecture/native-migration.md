# Legacy-to-Native Engine Migration Guide

## Overview

ROS-OCP-Backend supports two recommendation engines:

- **Kruize** (legacy): External Java service, writes to `workload_metrics` + `recommendation_sets` via JSONB
- **Native** (current): Built-in Go engine, writes to `daily_*_digests` + `recommendation_sets` via relational columns

The native engine is the default for all new deployments. This guide covers transitioning an existing Kruize-based deployment to native.

## Migration Steps

### 1. Enable Native Engine

The plugin architecture controls which engine is active:

```bash
# Default (native only):
ROS_ENABLED_PLUGINS=container,namespace,node,gpu,pvc,snapshot

# Legacy Kruize only:
ROS_ENABLED_PLUGINS=kruize

# Both (not recommended — writes duplicate data):
ROS_ENABLED_PLUGINS=kruize,container,namespace,node,gpu,pvc,snapshot
```

### 2. Data Separation

The two engines write to completely separate tables:

| Data | Kruize | Native |
|------|--------|--------|
| Raw metrics | `workload_metrics` (JSONB) | `container_usage_samples`, `daily_container_digests` |
| Recommendations | `recommendation_sets` (JSONB `recommendations` column) | `recommendation_sets` (relational columns) |
| GPU | Not supported | `gpu_container_digests` |
| Node | Not supported | `daily_node_digests`, `node_recommendations` |
| Namespace | `recommendation_sets` (partial) | `daily_namespace_digests` |

### 3. Cleanup After Transition

Once native engine is confirmed working:

1. **Kruize-era tables can be dropped** — the retention sweep (`RunRetentionSweep`) handles this automatically over time as partitions age out
2. **Background cleanup** — migration `000058` runs a background delete of Kruize-era `workload_metrics` rows before the CASCADE constraint activates
3. **`recommendation_sets` rows** — old Kruize-format rows (with `engine = 'kruize'`) coexist safely; the native engine writes `engine = 'native'`

### 4. Verification Checklist

After enabling native engine:

- [ ] `GET /recommendations/openshift` returns results with relational fields (not JSONB blobs)
- [ ] `GET /recommendations/openshift/{id}` returns `DetailResponse` shape
- [ ] GPU classifications appear in list responses (`gpu_classification` field)
- [ ] Savings estimates are non-zero (requires `KOKU_MASU_URL` configured)
- [ ] History endpoint (`GET .../history`) shows new entries being recorded
- [ ] Quality endpoint (`GET .../quality`) shows stability/adoption metrics

### 5. Rollback

To revert to Kruize:

```bash
ROS_ENABLED_PLUGINS=kruize
```

Native-era data remains in the database but is not served. The native tables will age out via retention. No data loss occurs in either direction.

## Stale Data Handling

When switching engines, recommendations from the disabled engine stop receiving updates. They will:

1. Be marked `stale = true` after `ROS_STALENESS_THRESHOLD_HOURS` (72h)
2. Be deleted after `ROS_STALE_ARCHIVE_DAYS` (30 days)

This is the intended lifecycle — no manual cleanup is needed.

## API Response Differences

| Aspect | Kruize | Native |
|--------|--------|--------|
| `monitoring_start_time` / `monitoring_end_time` | From Kruize result JSON | Computed from digest window |
| Notification codes | Limited set | Full set (20+ codes) |
| GPU recommendations | Not available | Full classification + MIG + time-slicing |
| Box plots | Pre-computed by Kruize | Computed on-the-fly from samples |
| Term support | Fixed (short/medium/long) | Configurable via settings API |
| Savings | Not available | Computed from Koku cost data |
