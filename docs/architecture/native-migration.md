# Legacy-to-Native Engine Migration Guide

## Overview

ROS-OCP-Backend supports two recommendation engines:

- **Kruize** (legacy): External Java service, writes to `workload_metrics` + `recommendation_sets` via JSONB
- **Native** (current): Built-in Go engine, writes to `daily_*_digests` + `recommendation_sets` via relational columns

The native engine is the **default** for all new deployments. It covers containers, namespaces, nodes, GPUs, PVCs, ResourceQuotas, ClusterResourceQuotas, VolumeSnapshots, and OpenShift Virtualization VMs. This guide covers transitioning an existing Kruize-based deployment to native.

## Native plugins (current)

When `ROS_ENABLED_PLUGINS` is empty, every native plugin is enabled except `kruize` (mutually exclusive). Plugins are sorted at runtime by [phase, priority, and name](plugin-phases.md)—not by the order in the env var.

| Plugin | Phase | Priority | Primary outputs |
|--------|-------|----------|-----------------|
| **container** | Produce | 10 | CPU/memory recs; inline idle/zombie; OOM bump; cost + performance engines |
| **gpu** | Produce | 20 | MIG profiles, time-slicing, classification; `gpu_catalog.yaml` model metadata |
| **node** | Produce | 30 | Node consolidation and sizing; cost + performance engines |
| **pvc** | Produce | 30 | PVC right-sizing and growth projection |
| **quota** | Produce | 35 | ResourceQuota tighten/raise/optimal |
| **cluster-quota** | Produce | 36 | ClusterResourceQuota vs namespace quota aggregates |
| **vm** | Produce | 40 | VM vCPU/GiB, disk, I/O, instance types, idle/abandoned, guest GPU (enabled by default; disable with `ROS_DISABLED_PLUGINS=vm`) |
| **snapshot** | Produce | 40 | VolumeSnapshot staleness and cost |
| **namespace** | Produce | 90 | Namespace quota targets; aggregates namespace idle after container/GPU |

**Not separate plugins:** idle/zombie detection runs inside container (and GPU) produce paths with shared settings at `GET/PUT .../settings/idle-detection`. **Business hours** is a platform feature (`ROS_BUSINESS_HOURS_ENABLED`) that maintains dual metric streams for container and namespace analysis.

**Planned (not registered today):** java, golang, python, nodejs, hpa, vpa, binpacking, machineset, seasonality — see [Plugin Execution Phases](plugin-phases.md).

### Example allowlists

```bash
# Default (native only — all plugins above except kruize):
# ROS_ENABLED_PLUGINS=   (empty)

# Explicit minimal set (order does not matter):
ROS_ENABLED_PLUGINS=container,namespace,node,gpu,pvc,quota,cluster-quota,snapshot,vm

# Legacy Kruize only:
ROS_ENABLED_PLUGINS=kruize

# Invalid — fatal at startup (mutually exclusive):
ROS_ENABLED_PLUGINS=kruize,container,namespace,node,gpu,pvc,snapshot
```

Disable individual domains without an allowlist:

```bash
ROS_DISABLED_PLUGINS=vm,namespace
```

## Platform capabilities (native only)

| Capability | Kruize | Native |
|------------|--------|--------|
| Notification codes | Limited set | **54+ codes** — [notification-codes.md](notification-codes.md) |
| Settings API (per-tenant) | No | **Yes** — thresholds, terms, idle, VM, quota, snapshot, business hours |
| Three-tier config | No | Admin `ROS_*` locks → Settings API → compiled defaults — [configurability.md](configurability.md) |
| Custom term windows | Fixed short/medium/long | **1–90 days** per plugin via `TermProvider` + `ROS_TERMS_*` |
| Recommendation history | Limited | **Yes** — container history + quality endpoints |
| Dollar savings | No | **Yes** — container, node, PVC, GPU, snapshot, quota tighten; fleet `savings-summary` |
| Adaptive CPU margins | No | **Yes** — variability-driven margins on container and VM CPU |
| OOM detection & memory bump | Partial | **Yes** — logarithmic bump from OOM events in window |
| GPU hardware catalog | N/A | **`internal/engine/gpu_catalog.yaml`** — VRAM, MIG profiles, model matching |
| VM instance type catalog | N/A | Built-in u1/cx1/m1 (+ gn1 when GPU metrics); cluster CRs via `cluster_instance_types.json` |
| OpenShift Virtualization | No (never supported) | **vm** plugin — native-only; CPU, memory, disk, I/O, idle, abandoned, crash loop, GPU on guest. No Kruize VM path existed to migrate. |
| ResourceQuota / CRQ | No | **quota**, **cluster-quota** plugins |
| Tag-based list filters | No | **Yes** — `filter[tag:key]` on container recommendations |
| Box plots | Pre-computed by Kruize | On-the-fly from usage samples |

## Migration steps

### 1. Enable native engine

```bash
# Native engine is unconditionally active — no flag needed.
# To select specific plugins, use ROS_ENABLED_PLUGINS (empty = all native plugins).
# ROS_USE_NATIVE_ENGINE has been removed (see ADR-0157).
```

See the [Configuration Reference](../operations/configuration.md) for `ROS_ENABLED_PLUGINS`, `ROS_DISABLED_PLUGINS`, and per-plugin kill switches.

### 2. Data separation

The two engines write to separate tables and shapes:

| Data | Kruize | Native |
|------|--------|--------|
| Raw metrics | `workload_metrics` (JSONB) | `container_usage_samples`, `daily_*_digests` per domain |
| Recommendations | `recommendation_sets` (JSONB `recommendations`) | `recommendation_sets` (relational columns, `engine = 'native'`) |
| GPU | Not supported | `gpu_container_digests`, GPU fields on recommendations |
| Node | Not supported | `daily_node_digests`, `node_recommendations` |
| Namespace | Partial via Kruize JSON | `daily_namespace_digests`, namespace rows |
| PVC / snapshot | Not supported | `daily_pvc_digests`, snapshot recommendation tables |
| Quota / CRQ | Not supported | `quota_recommendations`, `cluster_quota_recommendations` |
| VM | Not supported (never existed in Kruize) | `daily_vm_digests`, `vm_recommendations` — always native |
| History / quality | Limited | `recommendation_history`, quality metrics (container-led) |

### 3. Cleanup after transition

Once native engine is confirmed working:

1. **Kruize-era tables** age out via retention (`RunRetentionSweep`) as partitions expire
2. **Background cleanup** — migration `000058` deletes legacy `workload_metrics` before CASCADE constraints apply
3. **`recommendation_sets`** — rows with `engine = 'kruize'` coexist safely; native writes `engine = 'native'`

### 4. Verification checklist

After enabling native plugins:

- [ ] `GET /recommendations/openshift` returns relational fields (not opaque JSONB blobs)
- [ ] `GET /recommendations/openshift/{id}` returns native `DetailResponse` shape with `notification_codes`
- [ ] Container: cost and performance engines via `filter[engine]=cost|performance`
- [ ] Container: idle/zombie via `filter[idle_state]`; savings non-zero when `KOKU_MASU_URL` is set
- [ ] GPU: `gpu_classification`, MIG and time-slicing fields on list responses
- [ ] Node: nested cost/performance node recommendations
- [ ] PVC, snapshot, quota, cluster-quota: domain list endpoints return rows (when metrics exist)
- [ ] VM: `GET .../recommendations/openshift/vm` when vm plugin enabled (not in `ROS_DISABLED_PLUGINS`) and VM CSV is ingested
- [ ] History: `GET .../history` records new entries
- [ ] Quality: `GET .../quality` shows stability/adoption metrics
- [ ] Settings: `GET .../settings/capabilities` lists enabled plugins and locked fields
- [ ] Fleet: `GET .../savings-summary` aggregates cross-plugin savings (when cost integration enabled)

### 5. Rollback

```bash
ROS_ENABLED_PLUGINS=kruize
```

Native-era data remains in the database but is not served. Tables age out via retention. No data loss in either direction.

## Stale data handling

When switching engines, recommendations from the disabled engine stop receiving updates. They will:

1. Be marked `stale = true` after `ROS_STALENESS_THRESHOLD_HOURS` (default 48h)
2. Be deleted after `ROS_STALE_CLEANUP_DAYS` (default 30 days)

No manual cleanup is required.

## API response differences

| Aspect | Kruize | Native |
|--------|--------|--------|
| `monitoring_start_time` / `monitoring_end_time` | From Kruize result JSON | Computed from digest analysis window |
| Notification codes | Limited set | Full set — see [notification-codes.md](notification-codes.md) |
| GPU recommendations | Not available | Classification, MIG, time-slicing, savings |
| VM recommendations | Not available | Full VM plugin API (Preview Beta) |
| Quota / CRQ | Not available | Tighten/raise/optimal with risk levels |
| Box plots | Pre-computed by Kruize | Computed on-the-fly from samples |
| Term support | Fixed (short/medium/long) | Configurable per tenant; admin env locks |
| Savings | Not available | Koku `effective_rates` + fleet summary |
| Idle / tags | Not available | Idle state, tag filters, idle-detection settings |

## Related documentation

- [Plugin Execution Phases](plugin-phases.md) — execution order and future plugins
- [Configurability Reference](configurability.md) — env vars, Settings API, locks
- [Recommendation Engines](recommendation-engines.md) — percentiles, thresholds, terms
- [Features overview](../../docs-site/features/index.md) — per-domain feature pages
