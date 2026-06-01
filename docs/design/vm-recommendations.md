# OpenShift Virtualization Recommendations

**Status:** Implemented (phase11) — backend plugin, engine, API, and settings  
**Last updated:** 2026-06-01  
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

### Recommendation engine (native only)

VM recommendations are **always** produced by the built-in Go engine ([`recommendVM()`](../../internal/engine/vm_recommender.go)). **Kruize does not support VMs** and never will—there is no legacy Kruize VM path to gate or migrate.

| Term | Meaning for VMs |
|------|-----------------|
| **Dual engine** | Native **cost** vs **performance** sizing for the same VM (`vmEngineCost` / `vmEnginePerformance`); select with `filter[engine]=cost\|performance`. Not Kruize vs native. |
| **Coexistence** | On default native deployments, the `vm` plugin runs alongside container/namespace/node plugins. Legacy `ROS_ENABLED_PLUGINS=kruize` disables all native plugins (including `vm`); VM recs require native mode. |

---

## Implementation status

| Component | Status | Code |
|-----------|--------|------|
| VM CSV ingestion → `daily_vm_digests` | ✅ | [`internal/ingestion/vm_csv.go`](../../internal/ingestion/vm_csv.go), [`internal/plugins/vm/plugin.go`](../../internal/plugins/vm/plugin.go) |
| `recommendVM()` engine | ✅ | [`internal/engine/vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Instance type catalog + smallest-fit | ✅ | [`internal/engine/vm_instance_catalog.go`](../../internal/engine/vm_instance_catalog.go) |
| Hypervisor disk trending (Strategy B) | ✅ | `vmDiskProjectionHypervisor()` in [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Guest-agent disk projection (Strategy A) | ✅ | `vmDiskProjectionGuestAgent()` in [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| Notifications 18, 19, 37–63 | ✅ | [`internal/engine/vm_notifications.go`](../../internal/engine/vm_notifications.go), [`vm_gpu.go`](../../internal/engine/vm_gpu.go), [`vm_placement.go`](../../internal/engine/vm_placement.go) |
| Placement / shared storage / NUMA flags (60–63) | ✅ | [`vm_placement.go`](../../internal/engine/vm_placement.go), [`vm_pvc_correlation.go`](../../internal/engine/vm_pvc_correlation.go), [`vm_numa_check.go`](../../internal/engine/vm_numa_check.go) |
| GPU classification + `gn1` matching + notifications 50–53 | ✅ | [`internal/engine/vm_gpu.go`](../../internal/engine/vm_gpu.go), API filters `has_gpu` / `gpu_classification` |
| Abandoned detection (zero usage) | ✅ | [`internal/engine/vm_detect_abandoned.go`](../../internal/engine/vm_detect_abandoned.go) |
| List/detail API | ✅ | [`internal/api/handlers_vm_recs.go`](../../internal/api/handlers_vm_recs.go) |
| Settings + terms API | ✅ | [`internal/api/handlers_vm_settings.go`](../../internal/api/handlers_vm_settings.go), [`internal/engine/vm_settings.go`](../../internal/engine/vm_settings.go) |
| Operator Strategy 3 dual-CSV | ✅ | koku-metrics-operator (`ros-openshift-vm-usage`, `cm-openshift-vm-usage`, `ros-openshift-vm-gpu-device`) |
| OpenAPI spec (VM list/detail/history/settings) | ✅ | [`openapi.json`](../../openapi.json) |
| Savings ($) in API | ⬜ | — |
| `current_instance_type` catalog match | ✅ | [`RecognizeInstanceTypeExact()`](../../internal/engine/vm_instance_catalog.go) in [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |
| CPU adaptive margin (CV-based) | ✅ | [`ComputeAdaptiveMarginFromCV()`](../../internal/engine/vm_adaptive_margin.go), `ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED` |
| vGPU MIG / time-slicing recommendations | ✅ | [`vm_gpu.go`](../../internal/engine/vm_gpu.go), [`RecommendVMTimeSlicing()`](../../internal/engine/vm_gpu_timeslicing.go), [`OptimalMIGProfile()`](../../internal/engine/vm_mig_optimal.go) |
| Multi-GPU per-device analysis (notification **54**) | ✅ | `gpu_devices` JSONB on digests, [`analyzeVMGPU()`](../../internal/engine/vm_gpu.go) |
| Disk projection window from settings | ✅ | `vmComputeDiskExpandGiB()` uses `DiskProjectionWindowDays` |
| VM recommendation history API | ✅ | `vm_recommendation_history` table, `GET .../vms/{vm_name}/history` |
| koku-ui VM page | ⬜ | koku-ui |
| Per-mountpoint disk recs | ⬜ | — |
| VirtualMachinePreference CRD | ✅ | Operator `cluster_instance_types.json`; series override in [`vm_cluster_preferences.go`](../../internal/engine/vm_cluster_preferences.go) |
| Recommendation history | ✅ | Append-only `vm_recommendation_history`; `ROS_VM_REC_HISTORY_RETENTION_DAYS` (default 90) |

---

## Collection strategy: Strategy 3 (unified 15-min, dual-CSV)

The operator collects VM metrics once at **15-minute** resolution and emits two CSV streams:

| Output | Cadence | Consumer | Purpose |
|--------|---------|----------|---------|
| `ros-openshift-vm-usage-*.csv` | **15 min** | ROS-OCP | Percentiles, I/O, filesystem (guest agent) |
| `cm-openshift-vm-usage-*.csv` | **Hourly** (aggregated) | Koku | Cost reporting (unchanged) |

**ROS CSV columns** (canonical header in [`vm_csv.go`](../../internal/ingestion/vm_csv.go)): `interval_start`, `interval_end`, `vm_name`, `namespace`, `node_name`, `guest_os`, CPU/memory request/limit/usage, `disk_allocated_bytes`, optional `filesystem_used_bytes` / `filesystem_capacity_bytes`, disk IOPS and throughput.

---

## Guest agent: graduated confidence

Confidence reflects **guest-agent stability on the latest day**, not merely whether agent columns ever appeared. Each daily digest stores `sample_count` (total 15-minute intervals) and `agent_sample_count` (intervals with non-null `memory_available_kib`).

| Condition | `confidence` | Memory sizing | Disk strategy |
|-----------|--------------|---------------|---------------|
| Latest day ≥ 80% agent samples (`agent_sample_count / sample_count ≥ 0.80`) and ≥ 20 samples that day | `high` | Working set from `mem_available_*` (request − available p95) | Strategy A when ≥ 2 days have filesystem metrics; else Strategy B |
| Agent data exists in the window but latest day &lt; 80% stable | `moderate` | Hypervisor `mem_usage_*` p95/p99 | Strategy B |
| No agent samples in the window | `moderate` | Hypervisor usage | Strategy B |
| Latest day &lt; 20 total samples (&lt; one full day) | `low` | Hypervisor usage | None (no disk projection) |

**Minimum agent samples for percentiles:** `mem_available_p50_kib` and `mem_available_p95_kib` are computed only when `agent_sample_count ≥ 20` on that day. Fewer agent samples still count toward the stability ratio but do not produce agent percentiles (sizing falls back to hypervisor metrics internally without changing the confidence label).

**Transitions (no artificial multi-day penalty):**

- New VM with agent from boot → first full day (≥ 20 samples, ≥ 80% agent) → `high` immediately.
- Agent installed mid-day → if that day reaches ≥ 80% agent coverage, next day is `high`; otherwise `moderate` until stable.
- Agent removed or flapping (&lt; 80% on latest day) → `moderate` with notification **44** (`VM_GUEST_AGENT_INTERRUPTED`).
- Never had agent → `moderate` with notification **38** (`VM_NO_GUEST_AGENT`), not **44**.

**Disk projection:** Strategy A (filesystem linear growth) requires **≥ 2 days** with both `filesystem_used_max_bytes` and `filesystem_capacity_bytes`. With fewer filesystem days, Strategy B (hypervisor `disk_allocated_max_bytes` slope) is used. The two strategies are never mixed in one regression.

API fields: `metadata.guest_agent_detected` (latest day ≥ 80% agent), `metadata.confidence` (`high` | `moderate` | `low` on filters).

Implementation: [`DetermineVMConfidence()`](../../internal/engine/vm_recommender.go), digest field `agent_sample_count` in [`vm_digest_builder.go`](../../internal/ingestion/vm_digest_builder.go).

---

## Recommendation types

### vCPU and memory right-sizing

- **CPU:** p95 (cost) or p99 (performance) millicores → variability-driven margin (15–50% from daily P95 CV when `ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED=true`, else fixed `cpu_margin_min` / `cpu_margin_max` for performance) → ceil to whole vCPUs (min 1).
- **Memory:** p95 + margin (min 20%) → **memory floors** (`memory_floors.linux_gib` default 1, `windows_gib` default 2) → ceil to whole GiB (min 1).
- **Windows kernel reserve:** For Windows guests, subtract `memory_floors.windows_kernel_reserve_gib` (default **1.5** GiB) from observed memory usage before sizing (hypervisor `mem_usage_*` and guest-agent working set). Floors still apply after subtraction.
- **Downsize hysteresis:** Recommend downsize only if `recommended/current < 0.60` **and** drop ≥ `min_vcpu_change` (2) vCPU **and** ≥ `min_gib_change` (2) GiB.
- **Performance downsize stability:** Performance engine additionally requires each of the last `stability.downsize_stability_days` days (default **3**) to have per-day **P95** usage below the downsize threshold; otherwise hold current size and emit notification **49**.

### Instance type matching

Built-in catalog in [`vm_instance_catalog.go`](../../internal/engine/vm_instance_catalog.go) (OpenShift Virtualization defaults). **GPU types (`gn1.*`) are selectable** when VM GPU metrics indicate an attached device.

| Series | API `series` value | Sizes (examples) |
|--------|-------------------|------------------|
| **u1** | `general-purpose` | `u1.nano` … `u1.8xlarge` |
| **cx1** | `compute-optimized` | `cx1.medium` … `cx1.8xlarge` |
| **m1** | `memory-optimized` | `m1.large` … `m1.4xlarge` |
| **n1** | `network-optimized` | `n1.medium` … `n1.2xlarge` — active when `network.enable_network_series` is true (default) |
| **gn1** | `gpu` | `gn1.xlarge` … `gn1.16xlarge` — GPU memory capacity per size |

## GPU sharing mechanisms by workload type

The GPU recommendation system uses **different sharing mechanisms and catalogs** depending on workload type. Do not assume container GPU behavior applies to VMs (or vice versa).

| Workload | GPU sharing mechanism | Catalog(s) | Code path |
|----------|----------------------|------------|-----------|
| **Containers** (bare metal / virtualized pods) | MIG (`nvidia.com/mig-*`) + node time-slicing (`nvidia.com/gpu.replicas`) | [`gpu_catalog.yaml`](../../internal/engine/gpu_catalog.yaml) only | [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go), [`gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go) |
| **OpenShift Virtualization VMs** | MIG + vGPU profiles (`grid_*` device names) + guest time-slicing | `gpu_catalog.yaml` + [`vgpu_profiles.yaml`](../../internal/engine/vgpu_profiles.yaml) | [`vm_gpu.go`](../../internal/engine/vm_gpu.go), [`vm_gpu_timeslicing.go`](../../internal/engine/vm_gpu_timeslicing.go), [`vm_mig_optimal.go`](../../internal/engine/vm_mig_optimal.go) |
| **OpenShift AI** (inference / training Pods and Jobs) | Same as containers | `gpu_catalog.yaml` only | Same as containers (`gpu` plugin) |

**Shared vs VM-only catalogs**

- **`gpu_catalog.yaml`** — MIG profile definitions (A100, A30, H100, …). Used by **both** container MIG recommendations and VM `recommended_gpu_profile` / `OptimalMIGProfile()`.
- **`vgpu_profiles.yaml`** — NVIDIA GRID **C-series** vGPU profiles (A100D, A30, T4). **VM-only**; container GPU recommendations never read this file.

**Time-slicing outputs differ by workload**

| Workload | Time-slice output | vGPU profile name |
|----------|-------------------|-------------------|
| Containers | Integer replica count on the **node** (`recommended_replicas` → device-plugin `nvidia.com/gpu.replicas`) | Not applicable |
| VMs | Integer `recommended_time_slice_count` on the **guest** + optional `recommended_vgpu_profile` | Only on VM list/detail API; **never** on container GPU endpoints |

Container time-slicing is documented in [GPU time-slicing (public)](../../docs-site/features/gpu-time-slicing.md). Container classification thresholds: [gpu-classification.md](../architecture/gpu-classification.md).

**Profile families today**

- **C-series** (compute / CUDA) — what `vgpu_profiles.yaml` implements. Appropriate for FinOps optimization of training, inference, and general GPU compute on guests.
- **Q-series** (graphics / VDI) — **not** in the catalog. CAD, visualization, and remote-desktop workloads would need Q-series profiles and workload-type detection (see [Future: Q-series support](#future-q-series-support) below).

**API field:** `recommended_vgpu_profile` appears only on **VM** recommendation responses (notification **56**). Container GPU APIs expose MIG profile names and node replica counts only.

### GPU analysis

When `ros-openshift-vm-usage` includes GPU columns (DCGM metrics from virt-launcher pods), ROS aggregates daily GPU utilization and classifies workloads in [`analyzeVMGPU()`](../../internal/engine/vm_gpu.go):

| Classification | Typical action | Notification |
|----------------|----------------|--------------|
| `idle` | `remove_gpu` | **50** |
| `underutilized` | `use_mig_profile`, `enable_time_slicing`, or legacy `consider_vgpu_or_mig` | **51** |
| `memory_saturated` | `larger_gpu` | **52** |
| `compute_saturated` | `more_powerful_gpu` | **53** |
| `well_utilized` | `no_change` | — |

Thresholds: `ROS_VM_GPU_IDLE_THRESHOLD` (default 0.05), `ROS_VM_GPU_UNDERUTIL_THRESHOLD` (default 0.30), `ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD` (default 0.85). Frame-buffer saturation uses 90% of catalog GPU memory when `GPUFBSaturationMiB` is unset.

**Time-slicing (non-MIG):** [`RecommendVMTimeSlicing()`](../../internal/engine/vm_gpu_timeslicing.go) mirrors container [`computeReplicas()`](../../internal/engine/gpu_timeslicing.go): peak of SM, DRAM, and frame-buffer fractions drives `recommended_time_slice_count`, with FB safety (default 80% → no time-slice), DRAM penalty (halves max replicas above 50% DRAM), MIG-capable GPUs prefer MIG over time-slice, and confidence from observation days (`high` ≥ 7 days). Optional vGPU profiles come from [`vgpu_profiles.yaml`](../../internal/engine/vgpu_profiles.yaml) (`recommended_vgpu_profile`). Persisted fields: `gpu_timeslice_confidence`, `gpu_timeslice_rationale`, `recommended_vgpu_profile`. Tunables via `GET/PUT /settings/vm` → `gpu` block or env: `ROS_VM_GPU_TIMESLICE_*`.

**Algorithm:**

1. Compute recommended vCPU and GiB from usage + margins.
2. Run GPU analysis when `has_gpu` is true on daily digests.
3. Classify preferred series via `vmClassifySeries()` (CPU:memory ratio; idle → general-purpose; **network-saturated + balanced ratio → `network-optimized`**; **GPU VMs → `gpu` series**).
4. If the VM has a `VirtualMachine.spec.preference` mapping in `cluster_instance_types.json`, override the series using the preference’s class label (**preference wins over ratio**). See [VirtualMachinePreference integration](#virtualmachinepreference-integration).
5. If `instance_type_matching` is enabled (`ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING`, default **true**), call `MatchInstanceType()` — smallest type that fits vCPU, memory, and (when applicable) GPU count + GPU memory.
6. Fall back to general-purpose series if no match in preferred series (non-GPU VMs never receive `gn1.*`).
7. Emit notification **41** when a type is recommended.

### Network-optimized classification (n1)

When the operator includes KubeVirt network metrics on `ros-openshift-vm-usage-*.csv` (`net_*_per_sec` columns), ROS aggregates daily:

- `net_throughput_p95_bps` — P95 of (rx + tx) bytes/sec across 15-minute samples
- `net_pps_p95` — P95 of (rx + tx) packets/sec
- `net_drop_ratio_max_bp` — max drop ratio (drops / total packets × 10 000) for the day

[`vmClassifySeriesNetwork()`](../../internal/engine/vm_network_classification.go) returns true when **either**:

1. **Throughput:** `net_throughput_p95_bps` ≥ threshold (default **62 500 000** B/s ≈ 500 Mbps) on ≥ `network_sustained_days` (default **7**) days in the term window, **or**
2. **PPS + drops:** `net_pps_p95` ≥ **100 000** and `net_drop_ratio_max_bp` > **10** (0.1%) on ≥ **7** sustained days.

`vmClassifySeries()` then maps to `network-optimized` only when network classification is true **and** recommended vCPU:GiB ratio is balanced (0.5–2.0, same band as compute/memory). `MatchInstanceType()` selects **n1.*** when the series is `network-optimized` and `ROS_VM_ENABLE_NETWORK_SERIES` (Settings API `network.enable_network_series`, default **true**) is enabled. Recommendations set `is_network_bound=true` and may emit notification **55**.

Tunables: `GET/PUT /settings/vm` → `network` block or env `ROS_VM_NETWORK_*`.

Cluster `VirtualMachineClusterInstancetype` and `VirtualMachineClusterPreference` CRs are exported in `cluster_instance_types.json` by the metrics operator and persisted for matching.

### Per-GPU device metrics (`vm_gpu_device_digests`)

Multi-GPU VMs store **one row per physical GPU per daily digest** in `vm_gpu_device_digests` (migration `000100`). Columns mirror the device CSV: `gpu_uuid`, `gpu_model`, utilization and frame-buffer stats (basis points / MiB), and optional MIG metadata (`mig_profile`, `max_slices`). Parent digest rows in `daily_vm_digests` keep aggregate GPU fields; normalized device rows power notification **54** (partial multi-GPU idle) and `gpu_devices` in the detail API.

Ingestion: [`ParseVMGPUDeviceCSVRows()`](../../internal/ingestion/vm_gpu_device_csv.go) → [`MergeVMGPUDeviceRowsIntoDigests()`](../../internal/ingestion/vm_gpu_device_csv.go) → [`UpsertVMGPUDevices()`](../../internal/ingestion/vm_gpu_device_db.go). API reads attach devices via [`AttachGPUDevicesToDigests()`](../../internal/engine/vm_gpu_device_db.go).

### `ros-openshift-vm-gpu-device` CSV (operator → ROS)

The metrics operator emits **`ros-openshift-vm-gpu-device-*.csv`** (payload type `vm-gpu` / `ocp_ros_vm_gpu_device`) at the same 15-minute cadence as VM usage. Each row is one GPU on one VMI for one interval (14 columns: `interval_start`, `namespace`, `vm_name`, `gpu_uuid`, `gpu_model`, utilization and frame-buffer averages/maxima, SM/tensor/DRAM active averages, `mig_profile`, `max_slices`).

Koku routes these filenames to ROS only (not cost): `ros-openshift-vm-gpu-device` and `ocp_ros_vm_gpu_device` substrings in Masu `ROS_EXTRA_PATTERNS` / `is_ros_vm_filename()`. Cost remains on hourly `cm-openshift-vm-usage-*.csv`.

The VM plugin detects device CSVs by header (`gpu_uuid` present, no `cpu_usage_mc`) and calls [`IngestVMGPUDeviceCSV()`](../../internal/ingestion/vm_gpu_device_db.go) instead of the usage digest path.

### GPU ↔ VMI name correlation (operator)

DCGM metrics are collected on **virt-launcher pods**, not VMIs directly. The operator maps pod → VMI name before writing VM or device CSV rows:

1. **Primary:** `kube_pod_labels` / label `vm.kubevirt.io/name` via Prometheus ([`fetchPodVMINameMap()`](../../../koku-metrics-operator/internal/collector/vm_pod_vmi_labels.go), [`vmiNameForGPURow()`](../../../koku-metrics-operator/internal/collector/vm_gpu_collector.go)).
2. **Fallback:** Parse `virt-launcher-<vmi-name>-<hash>` when kube-state-metrics labels are unavailable.

The same correlation applies to aggregated VM GPU columns and per-device CSV rows.

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

**Unknown OS:** Empty `guest_os` uses Linux thresholds and emits notification **46** (`info`).

**Windows update spikes:** When P99−P95 spread exceeds **50%** (CPU) or **30%** (memory) across the window, emit notification **47** (`info`) — typical of Windows Update bursts; performance sizing already uses P99.

### Crash loop detection

Operator query `ros:vm_restart_count` counts `Running` phase transitions per 15-minute interval (`restart_count` CSV column). Daily digests sum `restart_count_sum`. When the term-window sum ≥ `stability.crash_loop_restart_threshold` (default **3**), emit notification **48** (`warning`).

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

Strategy A runs only when **≥ 2 days** have filesystem metrics; otherwise Strategy B is used. The two strategies are never combined for the same VM.

### Disk I/O profile

p95 read/write IOPS and throughput from digests. When read+write p95 exceeds `io.high_iops_threshold` (default 3000), sets `io_profile.hint` and notification **39**.

**Sequential vs random classification** ([`ClassifyIOPattern`](../../internal/engine/vm_io_classification.go)): peak combined BPS ÷ peak combined IOPS yields average I/O size. Below `io.min_iops_for_classification` (default 100) → `io_pattern` **`low-io`**. At or above `io.sequential_threshold_bytes` (default 64 KiB) → **`sequential`** (notification **58**). Below `io.random_threshold_bytes` (default 16 KiB) → **`random`** (notification **59**). Between those thresholds → **`mixed`**. Exposed as `io_profile.pattern` in list and detail APIs; stored in `vm_recommendations.io_pattern`.

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
| **38** | `NotifVMNoGuestAgent` | `info` | Never had stable guest agent (no notification **44**) |
| **44** | `NotifVMGuestAgentInterrupted` | `info` | Agent data in window but latest day &lt; 80% stable |
| **45** | `NotifVMInsufficientData` | `info` | `confidence == low` (&lt; one full day of samples) |
| **39** | `NotifVMHighIO` | `warning` | Total p95 IOPS > high IOPS threshold |
| **40** | `NotifVMDiskFillingGuest` | `warning` | Guest agent: `days_until_full < 90` |
| **41** | `NotifVMInstanceTypeRec` | `info` | `recommended.instance_type` set |
| **42** | `NotifVMDiskCritical` | `critical` | Guest agent: filesystem > 90% used |
| **43** | `NotifVMAbandoned` | `critical` | `metadata.is_abandoned` — zero CPU and memory max for N days |
| **46** | `NotifVMUnknownOS` | `info` | Empty `guest_os` — Linux defaults |
| **47** | `NotifVMWindowsUpdateSpike` | `info` | Windows P99≫P95 CPU or memory spread |
| **48** | `NotifVMCrashLoop` | `warning` | `restart_count_sum` ≥ threshold in window |
| **49** | `NotifVMDownsizeHeld` | `info` | Performance engine: unstable downsize (N-day P95 check) |
| **50** | `NotifVMGPUIdle` | `warning` | GPU idle — remove GPU assignment |
| **51** | `NotifVMGPUUnderutilized` | `info` | GPU underutilized — smaller MIG or vGPU/MIG |
| **52** | `NotifVMGPUMemorySaturated` | `warning` | GPU memory saturated — larger GPU |
| **53** | `NotifVMGPUComputeSaturated` | `warning` | GPU compute saturated — more powerful GPU |
| **54** | `NotifVMGPUMixedIdle` | `warning` | Some GPUs idle while others are active — reduce GPU count |
| **55** | `NotifVMNetworkSaturated` | `warning` | Network-saturated workload — n1 network-optimized instance type |
| **56** | `NotifVMVGPUProfileRecommended` | `info` | vGPU profile recommended — see `recommended_vgpu_profile` |
| **57** | `NotifVMGPUTimeSliceUnsafeFB` | `warning` | Time-slicing unsafe — frame-buffer usage above safety threshold |
| **58** | `NotifVMIOSequential` | `info` | `io_pattern == sequential` — throughput-oriented storage |
| **59** | `NotifVMIORandom` | `info` | `io_pattern == random` — IOPS-oriented storage |

Implementation: [`vm_notifications.go`](../../internal/engine/vm_notifications.go). Codes 18/19 are shared constants in [`notifications.go`](../../internal/engine/notifications.go).

**Full catalog (all plugins, codes 1–59):** [`docs/architecture/notification-codes.md`](../architecture/notification-codes.md) (developer; VM codes **55**–**59**),
[`docs-site/architecture/notification-codes.md`](../../docs-site/architecture/notification-codes.md) (operator).

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
| DELETE | `/api/cost-management/v1/recommendations/openshift/settings/vm` | Reset VM settings to compiled defaults (`204`) |
| GET | `/api/cost-management/v1/recommendations/openshift/settings/vm/terms` | Term windows |
| PUT | `/api/cost-management/v1/recommendations/openshift/settings/vm/terms` | Replace term windows (1–3 terms) |
| DELETE | `/api/cost-management/v1/recommendations/openshift/settings/vm/terms` | Reset VM term windows to plugin defaults (`204`) |

Term windows can also be read/written via the generic endpoint
`GET/PUT/DELETE .../settings/terms?recommendation_type=vm` (same lock rules: type-specific `vm` lock,
then generic `terms` lock — see [configurability.md](../architecture/configurability.md#generic-terms-lock-alignment)).

**Global settings lock:** When `ROS_SETTINGS_LOCKED=true` and `ROS_SETTINGS_LOCKED_VM` is true (default),
VM settings GET responses include `settings_locked: true` and `locked_fields: ["*"]`; PUT/DELETE return
`403`. Opt out with `ROS_SETTINGS_LOCKED_VM=false` while keeping other features frozen. Admin `ROS_VM_*`
env vars still override compiled defaults on read. See
[Global Settings Lock](../architecture/configurability.md#global-settings-lock).

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
  "memory_floors": { "linux_gib": 1, "windows_gib": 2, "windows_kernel_reserve_gib": 1.5 },
  "stability": { "downsize_stability_days": 3, "crash_loop_restart_threshold": 3 },
  "disk": {
    "projection_window_days": 30,
    "headroom_pct": 0.25,
    "round_step_gib": 10,
    "min_growth_mib_per_day": 100
  },
  "io": { "high_iops_threshold": 3000 },
  "network": {
    "throughput_threshold_bps": 62500000,
    "pps_threshold": 100000,
    "drop_ratio_bp": 10,
    "sustained_days": 7,
    "enable_network_series": true
  },
  "gpu": {
    "idle_threshold_bp": 500,
    "underutil_threshold_bp": 3000,
    "fb_saturation_mib": 0,
    "compute_saturation_threshold_bp": 8500,
    "gpu_timeslice_min_replicas": 2,
    "gpu_timeslice_max_replicas": 16,
    "gpu_timeslice_fb_safety_threshold_bp": 8000,
    "gpu_timeslice_dram_penalty_threshold_bp": 5000
  },
  "instance_type_matching": true,
  "locked_fields": [],
  "settings_locked": false
}
```

When the platform locks VM settings, `settings_locked` is `true` and `locked_fields` is `["*"]`.

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
| `ROS_VM_WINDOWS_KERNEL_RESERVE_GIB` | `1.5` | `memory_floors.windows_kernel_reserve_gib` |
| `ROS_VM_DOWNSIZE_STABILITY_DAYS` | `3` | `stability.downsize_stability_days` |
| `ROS_VM_CRASH_LOOP_RESTART_THRESHOLD` | `3` | `stability.crash_loop_restart_threshold` |
| `ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS` | `62500000` | `network.throughput_threshold_bps` |
| `ROS_VM_NETWORK_PPS_THRESHOLD` | `100000` | `network.pps_threshold` |
| `ROS_VM_NETWORK_DROP_RATIO_BP` | `10` | `network.drop_ratio_bp` |
| `ROS_VM_NETWORK_SUSTAINED_DAYS` | `7` | `network.sustained_days` |
| `ROS_VM_ENABLE_NETWORK_SERIES` | `true` | `network.enable_network_series` |
| `ROS_VM_GPU_IDLE_THRESHOLD` | `0.05` | `gpu.idle_threshold_bp` |
| `ROS_VM_GPU_UNDERUTIL_THRESHOLD` | `0.30` | `gpu.underutil_threshold_bp` |
| `ROS_VM_GPU_FB_SATURATION_MIB` | `0` | `gpu.fb_saturation_mib` |
| `ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD` | `0.85` | `gpu.compute_saturation_threshold_bp` |
| `ROS_VM_GPU_TIMESLICE_MIN_REPLICAS` | `2` | `gpu.gpu_timeslice_min_replicas` |
| `ROS_VM_GPU_TIMESLICE_MAX_REPLICAS` | `16` | `gpu.gpu_timeslice_max_replicas` |
| `ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP` | `8000` | `gpu.gpu_timeslice_fb_safety_threshold_bp` |
| `ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP` | `5000` | `gpu.gpu_timeslice_dram_penalty_threshold_bp` |

Source: [`internal/config/config.go`](../../internal/config/config.go), [`internal/engine/vm_config.go`](../../internal/engine/vm_config.go). Full table: [configurability.md](../architecture/configurability.md).

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
| `filter[is_network_bound]` | `true` / `false` |
| `filter[guest_agent_detected]` | `true` / `false` |
| `filter[has_gpu]` | `true` / `false` |
| `filter[gpu_classification]` | Comma-separated GPU classes (`idle`, `underutilized`, `well_utilized`, `saturated`) |
| `filter[guest_os]` | Guest OS substring match (case-insensitive; comma-separated OR) |

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
        "is_oversized": true,
        "is_network_bound": false
      },
      "io_profile": { "read_iops_p95": 1200, "write_iops_p95": 800, "hint": null, "pattern": "mixed" },
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

## Placement, correlated workloads, and NUMA (codes 60–63)

Implemented in phase11 with pragmatic signals until the operator exposes app labels, per-VM PVC names, and per-node NUMA topology on `ros-openshift-vm-usage-*.csv`.

| Code | Flag | Detection today | Operator enhancement |
|------|------|-----------------|----------------------|
| **60** | `is_redundant_placement` | Same namespace + matching vCPU/memory/disk profile on one node | `app` (or workload) label on VM CSV |
| **61** | — | Uneven node spread for that profile (skew ratio, default 3:1) | Same as **60** |
| **62** | `has_shared_storage` | Correlated peers in namespace with matching profile | `persistentvolumeclaim_name` per VM (already on `cm-openshift-vm-usage`) |
| **63** | `numa_oversized` | Recommended memory > `ROS_VM_NUMA_NODE_MEMORY_GIB` (default 64 GiB) | Per-node NUMA memory from node metrics |

Settings: `GET/PUT /settings/vm` → `placement` block (`enable_placement_checks`, `placement_skew_ratio`, `enable_shared_pvc_correlation`, `numa_node_memory_gib`).

---

## Future enhancements

| Item | Notes |
|------|-------|
| **Network flow correlation** | Requires OVN flow logs or eBPF telemetry between VMs |
| **Full NUMA optimization** | Requires LLC miss rate / perf counters per NUMA node |
| **Smart co-location** | Requires network flow data to recommend affinity |
| **Network QoS (SR-IOV, DPDK)** | SR-IOV and DPDK-specific sizing not modeled |
| **Storage tiering (hot/cold)** | No hot/cold volume class recommendations |
| **Power management / consolidation** | No suspend or cluster consolidation guidance |
| **Live migration recommendations** | No awareness of migration in progress |
| **Savings estimation ($)** | No dollar fields; requires Koku cost rate integration |
| **Per-mountpoint disk** | Single filesystem aggregate; no `/var` vs `/` split |
| **koku-ui** | No dedicated VM optimizations view |
| **Network metrics dependency** | **n1** requires KubeVirt `net_*` on `ros-openshift-vm-usage-*.csv` |

**GPU limitations (implemented with constraints):** VM time-slicing is guest-level guidance (`recommended_time_slice_count`, `recommended_vgpu_profile`, notifications **56**–**57**), not node-level `nvidia.com/gpu.replicas` orchestration like container [GPU time-slicing](../../docs-site/features/gpu-time-slicing.md). DCGM Exporter required on cluster for GPU metrics. Per-device utilization and notification **54** require `ros-openshift-vm-gpu-device` ingestion (or GPU columns on usage CSV with distinct `gpu_uuid` values).

### Future: Q-series support

Today VM vGPU recommendations use **C-series** profiles only (`vgpu_profiles.yaml`). **Q-series** profiles would be required for guests running graphics-heavy or VDI-style workloads (CAD, 3D visualization, remote desktop) where frame-buffer and display encoding matter more than CUDA throughput.

Planned approach (not implemented):

1. Add a `profile_family` field to `vgpu_profiles.yaml` (`compute` vs `graphics`).
2. Extend catalog entries for Q-series NVIDIA types (e.g. `grid_*` Q variants per Virtual GPU Software User Guide).
3. Select family using a **workload-type signal**, for example:
   - Existing guest already assigned a Q-series profile
   - VM or namespace labels indicating VDI / graphics
   - Tenant preference via Settings API (`/settings/vm` → `gpu` block)

Until that exists, FinOps guidance assumes compute/CUDA utilization (DCGM SM, tensor, DRAM) — correct for most OpenShift AI and batch GPU VMs, not for pure graphics guests.

---

## Database

Migrations: [`000089_vm_recommendations.up.sql`](../../migrations/000089_vm_recommendations.up.sql), GPU fields `000097`, device table `000100`, notification seeds in `000090`/`000091`.

- `daily_vm_digests` — daily percentile and I/O columns; aggregate GPU fields; `has_gpu`
- `vm_gpu_device_digests` — per-GPU rows keyed by `vm_digest_id` + `gpu_uuid`
- `vm_recommendations` — current/recommended sizing, flags, instance type, disk projection, notifications JSONB
- `vm_recommendation_history` — append-only history; retention via `ROS_VM_REC_HISTORY_RETENTION_DAYS`

---

## Testing

See [vm-test-plan.md](vm-test-plan.md). Summary:

| Layer | Planned | Implemented (approx.) |
|-------|---------|------------------------|
| Unit (ros-ocp-backend) | ~30 | **~70** test functions |
| E2E (cost-onprem-chart) | ~15 | **~44** |
| IQE | ~12 | **~59** |
| Nise templates | — | `ocp_report_vm.yml`, `ocp_report_vm_gpu.yml`, `ocp_report_vm_mvp_promotions.yml` |

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
