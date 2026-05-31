# OpenShift Virtualization Recommendations

**Status:** Implemented (phase11) — backend plugin, engine, API, and settings  
**Last updated:** 2026-05-31  
**Public overview:** [Virtual Machine Recommendations (docs-site)](../../docs-site/features/virtual-machines.md)

**Related requirements:** [requirements.md §12b (Phase 8b)](../architecture/requirements.md#12b-phase-8b-vm-recommendations-weeks-1218)  
**Related analysis:** [performance-analysis.md §30](../architecture/performance-analysis.md#30-openshift-virtualization-vm-recommendations)  
**Test plan:** [vm-test-plan.md](vm-test-plan.md)

---

## Overview

KubeVirt virtual machines on OpenShift Virtualization need right-sizing like containers, but with different characteristics:

| Dimension | Containers | VMs |
|-----------|--------------|-----|
| Resource units | Millicores, KiB (continuous) | Whole vCPUs, whole GiB (discrete) |
| Resize impact | Pod restart / rolling update | **VM restart** or disruptive live migration |
| Workload profile | Often bursty, ephemeral | Usually long-running, more stable |
| Overprovisioning | 2–10× common | **5–20× common** (lift-and-shift sizing) |

**Data path:** The metrics operator emits `ros-openshift-vm-usage-*.csv` at **15-minute** resolution. The `vm` plugin (Produce phase, **priority 40**) ingests rows, builds `daily_vm_digests`, runs `recommendVM()` in Go, and persists `vm_recommendations`. Koku continues to consume hourly `cm-openshift-vm-usage-*.csv` for cost only.

**Gate:** `ROS_ENABLE_VM_RECS` (default **`true`**). If no VM CSV is present, the plugin no-ops silently.

---

## Implementation status

| Component | Status | Code |
|-----------|--------|------|
| VM CSV ingestion → `daily_vm_digests` | ✅ | [`internal/ingestion/vm_csv.go`](../../internal/ingestion/vm_csv.go), [`internal/plugins/vm/plugin.go`](../../internal/plugins/vm/plugin.go) |
| `recommendVM()` engine | ✅ | [`internal/engine/vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Instance type catalog + smallest-fit | ✅ | [`internal/engine/vm_instance_catalog.go`](../../internal/engine/vm_instance_catalog.go) |
| Hypervisor disk trending (Strategy B) | ✅ | `vmDiskProjectionHypervisor()` in [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Guest-agent disk projection (Strategy A) | ✅ | `vmDiskProjectionGuestAgent()` in [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Notifications 18, 19, 37–43 | ✅ | [`internal/engine/vm_notifications.go`](../../internal/engine/vm_notifications.go) |
| Abandoned detection (zero usage) | ✅ | [`internal/engine/vm_detect_abandoned.go`](../../internal/engine/vm_detect_abandoned.go) |
| List/detail API | ✅ | [`internal/api/handlers_vm_recs.go`](../../internal/api/handlers_vm_recs.go) |
| Settings + terms API | ✅ | [`internal/api/handlers_vm_settings.go`](../../internal/api/handlers_vm_settings.go), [`internal/engine/vm_settings.go`](../../internal/engine/vm_settings.go) |
| Operator Strategy 3 dual-CSV | ⬜ (operator) | koku-metrics-operator |
| Savings ($) in API | ⬜ | — |
| `current_instance_type` from operator | ⬜ | TODO in [`vm_recommender.go`](../../internal/engine/vm_recommender.go#L142) |
| koku-ui VM page | ⬜ | koku-ui |
| Per-mountpoint disk recs | ⬜ | — |
| VirtualMachinePreference CRD | ✅ | Operator `cluster_instance_types.json`; series override in [`vm_cluster_preferences.go`](../../internal/engine/vm_cluster_preferences.go) |
| Recommendation history | ⬜ | Only latest row per VM/term/engine |

---

## Collection strategy: Strategy 3 (unified 15-min, dual-CSV)

The operator collects VM metrics once at **15-minute** resolution and emits two CSV streams:

| Output | Cadence | Consumer | Purpose |
|--------|---------|----------|---------|
| `ros-openshift-vm-usage-*.csv` | **15 min** | ROS-OCP | Percentiles, I/O, filesystem (guest agent) |
| `cm-openshift-vm-usage-*.csv` | **Hourly** (aggregated) | Koku | Cost reporting (unchanged) |

**ROS CSV columns** (canonical header in [`vm_csv.go`](../../internal/ingestion/vm_csv.go)): `interval_start`, `interval_end`, `vm_name`, `namespace`, `node_name`, `guest_os`, CPU/memory request/limit/usage, `disk_allocated_bytes`, optional `filesystem_used_bytes` / `filesystem_capacity_bytes`, disk IOPS and throughput.

---

## Guest agent: adaptive per-VM

| Mode | Detection | Engine behavior | API `confidence` |
|------|-----------|-----------------|------------------|
| **Enhanced** | Guest-agent columns populated in digests | Filesystem projection, working-set signals, code 40/42 | `"high"` |
| **Hypervisor-only** | Guest-agent columns null | Wider margins; Strategy B disk trending; code 38, 37 | `"moderate"` |

Guest-agent **disk strategy takes precedence** when any digest has non-null `filesystem_used_bytes` and `filesystem_capacity_bytes`; hypervisor allocation trending is not used for that VM.

API fields: `metadata.guest_agent_detected`, `metadata.confidence` (`high` | `moderate` | `low` accepted on filters).

---

## Recommendation types

### vCPU and memory right-sizing

- **CPU:** p95 (cost) or p99 (performance) millicores → adaptive margin (15–50%) → ceil to whole vCPUs (min 1).
- **Memory:** p95 + margin (min 20%) → **memory floors** (`memory_floors.linux_gib` default 1, `windows_gib` default 2) → ceil to whole GiB (min 1).
- **Downsize hysteresis:** Recommend downsize only if `recommended/current < 0.60` **or** drop ≥ `min_vcpu_change` (2) vCPU **or** ≥ `min_gib_change` (2) GiB.

### Instance type matching

Built-in catalog in [`vm_instance_catalog.go`](../../internal/engine/vm_instance_catalog.go) (OpenShift Virtualization defaults). **GPU types (`gn1.*`) are defined but excluded** from matching until GPU metrics exist.

| Series | API `series` value | Sizes (examples) |
|--------|-------------------|------------------|
| **u1** | `general-purpose` | `u1.nano` … `u1.8xlarge` |
| **cx1** | `compute-optimized` | `cx1.medium` … `cx1.8xlarge` |
| **m1** | `memory-optimized` | `m1.large` … `m1.4xlarge` |

**Algorithm:**

1. Compute recommended vCPU and GiB from usage + margins.
2. Classify preferred series via `vmClassifySeries()` (CPU:memory ratio; idle → general-purpose).
3. If the VM has a `VirtualMachine.spec.preference` mapping in `cluster_instance_types.json`, override the series using the preference’s class label (**preference wins over ratio**). See [VirtualMachinePreference integration](#virtualmachinepreference-integration).
4. If `instance_type_matching` is enabled (`ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING`, default **true**), call `MatchInstanceType()` — smallest type in preferred series that fits vCPU and memory (MiB-aware for `u1.nano`).
5. Fall back to general-purpose series if no match in preferred series.
6. Emit notification **41** when a type is recommended.

Cluster `VirtualMachineClusterInstancetype` and `VirtualMachineClusterPreference` CRs are exported in `cluster_instance_types.json` by the metrics operator and persisted for matching.

### VirtualMachinePreference integration

OpenShift Virtualization lets admins attach a **cluster preference** to a VM (`spec.preference.name`). Preferences carry a **class** label (`instancetype.kubevirt.io/class` on the preference CR) that indicates intended series (for example memory-intensive for database workloads).

The operator extends `cluster_instance_types.json`:

```json
{
  "preferences": [{"name": "database", "class": "memory-intensive"}],
  "vm_preferences": {"production/db-server-01": "database"}
}
```

ROS loads this catalog on ingest ([`UpsertClusterInstanceTypes`](../../internal/engine/vm_cluster_instance_types.go)) and applies overrides in [`RecommendVM()`](../../internal/engine/vm_recommender.go) via [`VMPreferenceContext.SeriesForVM()`](../../internal/engine/vm_cluster_preferences.go).

**Precedence:** `VirtualMachinePreference` class → ratio-based `vmClassifySeries()` → general-purpose fallback inside `MatchInstanceType()`.

**Class → series mapping** ([`NormalizePreferenceClass()`](../../internal/engine/vm_cluster_preferences.go)):

| KubeVirt class label | ROS `series` value |
|----------------------|-------------------|
| `general-purpose` | `general-purpose` |
| `compute-intensive`, `compute` | `compute-optimized` |
| `memory-intensive`, `memory` | `memory-optimized` |
| unknown / empty | ratio classification (no override) |

Detail API adds `metadata.preference_name` and `metadata.preference_class` when configured. The instance-types API reports `preferences.configured` and counts.

### Idle detection (OS-aware)

Both CPU and memory p95 must be below thresholds:

| Guest OS | CPU p95 | Memory p95 |
|----------|---------|------------|
| **Linux** (default) | < 50 millicores | < 512 MiB |
| **Windows** | < 200 millicores | < 3072 MiB |

Emits notification **18** (`warning`). OS from `guest_os` in CSV.

### Abandoned detection (zero usage)

Stricter than idle: **every** daily digest in the term window must have `cpu_usage_max_mc = 0` and `mem_usage_max_kib = 0`, and the window must include at least `abandoned_min_days` digests (default **3**, ≈72 hours at daily granularity).

| Classification | Condition | Notification | Recommended sizing |
|----------------|-----------|--------------|-------------------|
| **Idle** | CPU and memory **p95** below OS thresholds (non-zero usage allowed) | **18** `warning` | 1 vCPU + memory floor |
| **Abandoned** | All days: CPU max = 0 **and** memory max = 0 | **43** `critical` | **0** vCPU, **0** GiB (deallocate) |

**Precedence:** abandoned supersedes idle — a VM never has both `is_abandoned` and `is_idle`, and notification **18** is omitted when **43** applies.

Algorithm: [`DetectVMAbandoned()`](../../internal/engine/vm_detect_abandoned.go) runs in [`RecommendVM()`](../../internal/engine/vm_recommender.go) before idle classification.

Configure via `thresholds.abandoned_min_days` (Settings API) or `ROS_VM_ABANDONED_MIN_DAYS` (env lock).

### Disk projection

**Strategy A (guest agent)** — when filesystem metrics exist on digests:

- Linear growth from earliest→latest daily `filesystem_used_max_bytes` vs capacity.
- Sets `disk_projection.days_until_full` when growth > 0.
- Notification **40** when `days_until_full < 90`.
- Notification **42** (`critical`) when latest used/capacity > 90%.

**Strategy B (hypervisor-only)** — OLS slope on daily `disk_allocated_max_bytes`:

- Requires ≥ 2 days with positive allocation samples.
- Slope must be ≥ `disk.min_growth_mib_per_day` (default **100** MiB/day).
- Sets `disk_projection.growth_gib_per_day` and `recommended_expand_gib` (30-day projection × headroom, rounded to `round_step_gib`).
- **`days_until_full` is always `null`** — allocated bytes do not represent in-guest free space.
- Notification **37** when growth is significant.

Guest-agent path **wins** whenever filesystem columns are present; Strategy B is not combined for the same VM.

### Disk I/O profile

p95 read/write IOPS and throughput from digests. When read+write p95 exceeds `io.high_iops_threshold` (default 3000), sets `io_profile.hint` and notification **39**.

---

## Notifications

All notifications are JSON objects in the `notifications` array:

```json
{"code": 18, "type": "warning", "message": "VM is idle: CPU and memory usage are consistently below thresholds"}
```

| Code | Name | Type | Trigger |
|------|------|------|---------|
| **18** | `NotifVMIdle` | `warning` | `metadata.is_idle` |
| **19** | `NotifVMOversized` | `warning` | `metadata.is_oversized` (meaningful downsize) |
| **37** | `NotifVMDiskGrowingNoCapacity` | `info` | Hypervisor allocation growth ≥ min threshold; no guest agent |
| **38** | `NotifVMNoGuestAgent` | `info` | `guest_agent_detected == false` |
| **39** | `NotifVMHighIO` | `warning` | Total p95 IOPS > high IOPS threshold |
| **40** | `NotifVMDiskFillingGuest` | `warning` | Guest agent: `days_until_full < 90` |
| **41** | `NotifVMInstanceTypeRec` | `info` | `recommended.instance_type` set |
| **42** | `NotifVMDiskCritical` | `critical` | Guest agent: filesystem > 90% used |
| **43** | `NotifVMAbandoned` | `critical` | `metadata.is_abandoned` — zero CPU and memory max for N days |

Implementation: [`vm_notifications.go`](../../internal/engine/vm_notifications.go). Codes 18/19 are shared constants in [`notifications.go`](../../internal/engine/notifications.go).

---

## Plugin and pipeline

| Property | Value |
|----------|-------|
| Plugin name | `vm` |
| Phase | **1 — Produce** |
| Priority | **40** |
| CSV type | `PayloadTypeVM` / `ros-openshift-vm-usage` |
| Retention tables | `daily_vm_digests`, `vm_recommendations` |

```
ros-openshift-vm-usage-*.csv
        │
        ▼
  ParseVMCSVRows() → BuildDailyVMDigests() → Upsert daily_vm_digests
        │
        ▼
  RunVMRecommendations() / recommendVM()
        │
        ▼
  vm_recommendations + notifications
        │
        ▼
  GET /recommendations/openshift/vm
  GET /recommendations/openshift/vm/detail
```

Default term windows ([`plugin.go`](../../internal/plugins/vm/plugin.go)): **7 / 15 / 30** days lookback with **3 / 7 / 15** min data days (`short_term`, `medium_term`, `long_term`). Max plugin window: **90** days.

---

## Configuration

### Three-tier precedence

Compiled defaults → tenant Settings API → environment variable locks (field paths in `locked_fields`).

### Settings API (implemented paths)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/cost-management/v1/recommendations/openshift/settings/vm` | Thresholds, memory floors, disk, I/O, instance type matching |
| PUT | `/api/cost-management/v1/recommendations/openshift/settings/vm` | Partial update of allowed blocks |
| GET | `/api/cost-management/v1/recommendations/openshift/settings/vm/terms` | Term windows |
| PUT | `/api/cost-management/v1/recommendations/openshift/settings/vm/terms` | Replace term windows (1–3 terms) |

**GET settings response shape** ([`VMSettingsResponse`](../../internal/engine/vm_settings.go)):

```json
{
  "enabled": true,
  "thresholds": {
    "cpu_percentile_cost": 0.95,
    "cpu_percentile_perf": 0.99,
    "cpu_margin_min": 0.15,
    "cpu_margin_max": 0.50,
    "mem_margin_min": 0.20,
    "downsize_hysteresis_ratio": 0.60,
    "min_vcpu_change": 2,
    "min_gib_change": 2,
    "idle_cpu_mc": 50,
    "idle_memory_mib": 512,
    "idle_cpu_mc_windows": 200,
    "idle_memory_mib_windows": 3072,
    "abandoned_min_days": 3
  },
  "memory_floors": { "linux_gib": 1, "windows_gib": 2 },
  "disk": {
    "projection_window_days": 30,
    "headroom_pct": 0.25,
    "round_step_gib": 10,
    "min_growth_mib_per_day": 100
  },
  "io": { "high_iops_threshold": 3000 },
  "instance_type_matching": true,
  "locked_fields": []
}
```

### Environment variables

| Variable | Default | Settings API field |
|----------|---------|-------------------|
| `ROS_ENABLE_VM_RECS` | `true` | No (deployment gate) |
| `ROS_VM_CPU_PERCENTILE_COST` | `0.95` | `thresholds.cpu_percentile_cost` |
| `ROS_VM_CPU_PERCENTILE_PERF` | `0.99` | `thresholds.cpu_percentile_perf` |
| `ROS_VM_CPU_MARGIN_MIN` | `0.15` | `thresholds.cpu_margin_min` |
| `ROS_VM_CPU_MARGIN_MAX` | `0.50` | `thresholds.cpu_margin_max` |
| `ROS_VM_MEM_MARGIN_MIN` | `0.20` | `thresholds.mem_margin_min` |
| `ROS_VM_DOWNSIZE_HYSTERESIS_RATIO` | `0.60` | `thresholds.downsize_hysteresis_ratio` |
| `ROS_VM_MIN_VCPU_CHANGE` | `2` | `thresholds.min_vcpu_change` |
| `ROS_VM_MIN_GIB_CHANGE` | `2` | `thresholds.min_gib_change` |
| `ROS_VM_IDLE_CPU_MC` | `50` | `thresholds.idle_cpu_mc` |
| `ROS_VM_IDLE_MEMORY_MIB` | `512` | `thresholds.idle_memory_mib` |
| `ROS_VM_IDLE_CPU_MC_WINDOWS` | `200` | `thresholds.idle_cpu_mc_windows` |
| `ROS_VM_IDLE_MEMORY_MIB_WINDOWS` | `3072` | `thresholds.idle_memory_mib_windows` |
| `ROS_VM_ABANDONED_MIN_DAYS` | `3` | `thresholds.abandoned_min_days` |
| `ROS_VM_LINUX_MEMORY_FLOOR_GIB` | `1` | `memory_floors.linux_gib` |
| `ROS_VM_WINDOWS_MEMORY_FLOOR_GIB` | `2` | `memory_floors.windows_gib` |
| `ROS_VM_DISK_PROJECTION_DAYS` | `30` | `disk.projection_window_days` |
| `ROS_VM_DISK_HEADROOM_PCT` | `0.25` | `disk.headroom_pct` |
| `ROS_VM_DISK_ROUND_STEP_GIB` | `10` | `disk.round_step_gib` |
| `ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY` | `100` | `disk.min_growth_mib_per_day` |
| `ROS_VM_HIGH_IOPS_THRESHOLD` | `3000` | `io.high_iops_threshold` |
| `ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING` | `true` | `instance_type_matching` |

Source: [`internal/config/config.go`](../../internal/config/config.go), [`internal/engine/vm_config.go`](../../internal/engine/vm_config.go).

---

## REST API reference

Base prefix: `/api/cost-management/v1`. Requires `x-rh-identity` and cost-management entitlement. Routes return **404** when `ROS_ENABLE_VM_RECS=false` or the `vm` plugin is not in `enabledPlugins`.

### List recommendations

`GET /recommendations/openshift/vm`

**Query parameters**

| Parameter | Description |
|-----------|-------------|
| `limit` | 1–100 (default 10) |
| `offset` | Pagination offset |
| `order_by` | `vm_name`, `namespace`, `current_vcpu`, `current_memory_gib`, `guest_os`, `recommended_vcpu`, `recommended_memory_gib`, `is_idle`, `is_abandoned`, `is_oversized`, `confidence`, `last_recommended_at` |
| `order_how` | `asc` or `desc` |
| `filter[cluster]` | Cluster UUID (RBAC-scoped) |
| `filter[namespace]` | Namespace |
| `filter[vm_name]` | VM name |
| `filter[term]` | `short_term`, `medium_term`, `long_term` |
| `filter[engine]` | `cost` or `performance` |
| `filter[confidence]` | `high`, `moderate`, or `low` |
| `filter[is_idle]` | `true` / `false` |
| `filter[is_abandoned]` | `true` / `false` |
| `filter[is_oversized]` | `true` / `false` |
| `filter[guest_agent_detected]` | `true` / `false` |

**Example**

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/vm?limit=10&filter[is_idle]=true'
```

**Response (abbreviated)**

```json
{
  "meta": { "count": 1, "limit": 10, "offset": 0 },
  "links": { "first": "...", "last": "...", "next": null, "previous": null },
  "data": [
    {
      "vm_name": "legacy-app-02",
      "namespace": "finance",
      "cluster_uuid": "02059694-68ab-4d58-8809-de1e91f1d0e5",
      "guest_os": "linux",
      "current": { "vcpu": 8, "memory_gib": 32, "disk_gib": 500, "instance_type": null },
      "recommended": { "vcpu": 4, "memory_gib": 16, "disk_gib": 600, "instance_type": "u1.xlarge", "series": "general-purpose" },
      "metadata": {
        "guest_agent_detected": false,
        "confidence": "moderate",
        "term": "medium_term",
        "engine": "cost",
        "is_idle": false,
        "is_abandoned": false,
        "is_oversized": true
      },
      "io_profile": { "read_iops_p95": 1200, "write_iops_p95": 800, "hint": null },
      "disk_projection": { "days_until_full": null, "growth_gib_per_day": 2.1, "recommended_expand_gib": 100 },
      "notifications": [
        { "code": 19, "type": "warning", "message": "VM is oversized: recommended resources are significantly below current allocation" },
        { "code": 38, "type": "info", "message": "QEMU guest agent not installed: recommendations based on hypervisor metrics only (moderate confidence)" }
      ],
      "last_recommended_at": "2026-05-30T12:00:00Z"
    }
  ]
}
```

### Detail (includes daily digests)

`GET /recommendations/openshift/vm/detail`

**Required query parameters:** `cluster_uuid` (or `filter[cluster]`), `vm_name`, `namespace`  
**Optional:** `term` (default `medium_term`), `engine` (default `cost`)

```bash
curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/vm/detail?cluster_uuid=CLUSTER&vm_name=erp-backend&namespace=finance&term=medium_term&engine=cost'
```

Adds `daily_digests[]` with per-day percentile fields for charts.

---

## Not yet implemented (future work)

| Item | Notes |
|------|-------|
| **Savings estimation** | No dollar fields in API; Koku VM cost rates not wired |
| **GPU / MIG VMs** | `gn1` catalog reference only; no GPU metrics or recommendations |
| **koku-ui** | No dedicated VM optimizations view |
| **`current_instance_type`** | Column exists; not populated from operator/`kubevirt_vmi_info` yet |
| **Per-mountpoint disk** | Single filesystem aggregate; no `/var` vs `/` split |
| **Recommendation history** | Upsert keeps latest row only |
| **OpenAPI spec** | VM paths not in [`openapi.json`](../../openapi.json) yet |

---

## Database

Migrations: [`000089_vm_recommendations.up.sql`](../../migrations/000089_vm_recommendations.up.sql), notification seeds in `000090`/`000091`.

- `daily_vm_digests` — partitioned by `bucket_date`; percentile and I/O columns
- `vm_recommendations` — current/recommended sizing, flags, instance type, disk projection, notifications JSONB

---

## Testing

See [vm-test-plan.md](vm-test-plan.md). Summary:

| Layer | Planned | Implemented (approx.) |
|-------|---------|------------------------|
| Unit (ros-ocp-backend) | ~30 | **~58** test functions |
| E2E (cost-onprem-chart) | ~15 | **13** |
| IQE | ~12 | **26** |
| Nise templates | — | `ocp_report_vm.yml` (chart + IQE) |

```bash
cd ros-ocp-backend
go test ./internal/engine/... ./internal/ingestion/... ./internal/api/... -run 'VM|Vm|vm' -count=1
```

---

## References

- [vm-test-plan.md](vm-test-plan.md)
- [configurability.md](../architecture/configurability.md) — global `ROS_*` precedence
- [plugin-phases.md](../architecture/plugin-phases.md) — `vm` priority 40
- IQE: [`test_ros_vm_recommendations.py`](../../../iqe-cost-management-plugin/iqe_cost_management/tests/rest_api/v1/test_ros_vm_recommendations.py)
- E2E: [`test_vm_recommendations.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_recommendations.py)
