# OpenShift Virtualization Recommendations (Planned)

**Status:** Planned / Future Work  
**Last updated:** 2026-05-31  
**Public overview:** [Virtual Machine Recommendations (docs-site)](../../docs-site/features/virtual-machines.md)

**Related requirements:** [requirements.md §12b (Phase 8b)](../architecture/requirements.md#12b-phase-8b-vm-recommendations-weeks-1218)  
**Related analysis:** [performance-analysis.md §30](../architecture/performance-analysis.md#30-openshift-virtualization-vm-recommendations)

---

## Overview

KubeVirt virtual machines on OpenShift Virtualization need right-sizing like containers, but with different characteristics:

| Dimension | Containers | VMs |
|-----------|--------------|-----|
| Resource units | Millicores, KiB (continuous) | Whole vCPUs, whole GiB (discrete) |
| Resize impact | Pod restart / rolling update | **VM restart** or live migration (vCPU/memory change still disruptive) |
| Workload profile | Often bursty, ephemeral | Usually long-running, more stable |
| Overprovisioning | 2–10× common | **5–20× common** (lift-and-shift sizing) |

**Today:** VM metrics flow **operator → Koku** for cost reporting only. **ROS has no VM pipeline** — `aggregator.go` accepts only standard workload types; VM CSVs are not ingested.

**Goal:** Add a `vm` plugin (Phase 1 Produce, **priority 40**) gated by `ROS_ENABLE_VM_RECS` (default **enabled** with auto-detection) that ingests VM usage digests at **15-minute** resolution, runs `recommendVM()` in Go, and exposes list/detail APIs aligned with container recommendations.

---

## Collection strategy: Strategy 3 (unified 15-min, dual-CSV)

The operator collects **all VM metrics once** at **15-minute** resolution. To avoid duplicate Prometheus queries and to keep Koku ingestion stable:

| Output | Cadence | Consumer | Purpose |
|--------|---------|----------|---------|
| `ros-openshift-vm-usage-*.csv` | **15 min** | ROS-OCP | Percentiles, I/O profile, filesystem (when guest agent present) |
| `cm-openshift-vm-usage-*.csv` | **Hourly** (aggregated from same scrape) | Koku | Cost reporting (unchanged contract) |

**Benefits:**

- Single scrape pass per VM metric — no overlapping `cost:vm_*` + `ros:vm_*` query storms
- ROS gets higher resolution for stable percentile estimates
- Koku continues to receive hourly CSVs without pipeline changes

**Impact at scale (R2):** At **1,000 VMs**, Strategy 3 adds ~**1 MB per upload** (compressed) over a 6-hour cycle versus hourly-only collection. Operator PVC usage ~**720 MB** at 30 retained reports. Negligible relative to cluster and network capacity.

---

## Current state

### koku-metrics-operator (cost path → dual output)

The operator today collects **~11 `cost:vm_*` queries** at **hourly** granularity for billing. Under Strategy 3, the same underlying series are scraped at **15 min**; Koku-bound CSVs are downsampled to hourly aggregates.

| Query | Prometheus series (summary) | Purpose |
|-------|-----------------------------|---------|
| `cost:vm_cpu_usage` | `rate(kubevirt_vmi_cpu_usage_seconds_total[5m])` | CPU utilization |
| `cost:vm_cpu_request_cores` | `kubevirt_vm_resource_requests{resource='cpu'}` | CPU allocation |
| `cost:vm_cpu_limit_cores` | `kubevirt_vm_resource_limits{resource='cpu'}` | CPU limit |
| `cost:vm_cpu_request_sockets` | sockets unit on CPU requests | Topology (cost only) |
| `cost:vm_cpu_request_threads` | threads unit on CPU requests | Topology (cost only) |
| `cost:vm_memory_usage_bytes` | `kubevirt_vmi_memory_used_bytes` | Memory utilization |
| `cost:vm_memory_request_bytes` | `kubevirt_vm_resource_requests{resource='memory'}` | Memory allocation |
| `cost:vm_memory_limit_bytes` | `kubevirt_vm_resource_limits{resource='memory'}` | Memory limit |
| `cost:vm_disk_allocated_size_bytes` | `kubevirt_vm_disk_allocated_size_bytes` | Disk provisioned size |
| `cost:vm_info` | `kubevirt_vmi_info{phase='running'}` | OS, instance type, guest OS |
| `cost:vm_labels` | `kubevirt_vm_labels` | Labels for cost allocation |

**Outputs:** `cm-openshift-vm-usage-<YYYYMM>.csv` (hourly, Koku) and `ros-openshift-vm-usage-*.csv` (15 min, ROS) in the operator upload tarball.

### Koku backend (cost + reporting)

- Line items: `openshift_vm_usage_line_items` (Trino/Parquet SaaS; self-hosted PostgreSQL on-prem)
- UI summary: `reporting_ocp_vm_summary_p` / `OCPVirtualMachineSummaryP`
- Cost model metrics: `vm_cost_per_month`, `vm_core_cost_per_hour`, etc.
- REST: `reports/openshift/resources/virtual-machines/`

VMs are identified in Koku via the pod label `vm.kubevirt.io/name` (`vm_kubevirt_io_name` in JSON).

### ros-ocp-backend (gaps)

| Item | Status |
|------|--------|
| VM CSV ingestion | **Not implemented** |
| `daily_vm_digests` | Schema specified in requirements §18; table not wired |
| `recommendVM()` | **Not implemented** |
| VM API endpoints | **Not implemented** |
| Notification codes **18** (`NotifVMIdle`), **19** (`NotifVMOversized`) | Defined in `internal/engine/notifications.go`; **no plugin emits them** |
| `ROS_ENABLE_VM_RECS` | Master gate (default **`true`**; auto-no-op if no VM CSV) |

---

## Guest agent: adaptive per-VM

Recommendation quality adapts **per VM** based on whether QEMU guest agent metrics are present in the CSV (non-null columns):

| Mode | Detection | Engine behavior | API `confidence` |
|------|-----------|-----------------|------------------|
| **Enhanced** | Guest agent columns populated | Working-set memory, per-mountpoint filesystem used %, swap detection | `"high"` |
| **Hypervisor-only** | Guest agent columns null | Hypervisor metrics only; wider safety margins | `"moderate"` |

**API fields:**

- `guest_agent_detected` — `true` when guest agent metrics were present for this VM in the analysis window
- `confidence` — `"high"` (guest agent) or `"moderate"` (hypervisor-only)

Without guest agent, in-guest OOM and filesystem pressure are not visible in Prometheus — the engine compensates with higher memory margins (same pattern as containers without OOM feedback).

---

## Recommendation types

### 1. vCPU right-sizing

- **Input:** p95 CPU usage (millicores) over the active term window.
- **Algorithm:** Apply adaptive margin (minimum **15%**, maximum **50%**), convert to vCPUs, **ceil** to whole vCPUs (minimum 1).
- **Engines:** Cost engine uses p95; performance engine uses p99 (same pattern as containers).

### 2. Memory right-sizing

- **Input:** p95 memory usage (KiB); with guest agent, prefer working-set signals where available.
- **Algorithm:** Minimum **20%** margin above p95; apply **guest OS floor** (Windows: **2 GiB**, Linux: **0.5 GiB**); **ceil** to whole GiB (minimum 1 GiB).

### 3. Instance type matching

Map recommended vCPU + memory to the **smallest** `VirtualMachineClusterInstancetype` that satisfies both dimensions.

- **Catalog:** Built-in OpenShift Virt defaults (series below) + operator-collected custom types (**VM-E, phase11 scope**).
- **`VirtualMachinePreference` CRDs:** Optional hints for workload class (e.g. high-performance vs development) to bias series selection.

### 4. Idle / zombie VM detection (OS-aware)

Flag VMs where **both** CPU and memory p95 stay below OS-specific idle thresholds:

| Guest OS family | CPU p95 threshold | Memory p95 threshold |
|-----------------|-------------------|----------------------|
| **Linux** (default) | **< 50 millicores** | **< 512 MiB** |
| **Windows** | **< 200 millicores** | **< 3072 MiB** (3 GiB) |

**OS detection:** `os` label on `kubevirt_vmi_info` (available when the VM spec declares OS, even without guest agent).

Emits `NotifVMIdle` (code 18). Distinct from rightsizing — full allocated waste, not marginal savings.

### 5. Disk size trending

- **Hypervisor-only:** Daily max `disk_allocated_bytes` + **30-day** linear trend + **25%** headroom → round to **10 GiB**.
- **With guest agent:** Per-mountpoint recommendations from filesystem used/capacity (queries 13–14); projection uses in-guest utilization where available.

### 6. Disk I/O profile (day one)

- **Input:** p95 read/write IOPS and throughput (queries 7–10).
- **Output:** Notification when total p95 IOPS exceeds **3000** — suggests reviewing storage class performance; **no automatic storage class name** (catalog varies per cluster).

---

## Instance type recommendation (detail)

OpenShift Virtualization ships **VirtualMachineClusterInstancetype** CRDs. Common series (sizes from `small` through `8xlarge`):

| Series | Typical profile | Selection heuristic |
|--------|-----------------|---------------------|
| **cx1** | CPU-heavy | High CPU/memory ratio, sustained CPU |
| **m1** | Balanced general purpose | Default when no strong skew |
| **u1** | Utility / low cost | Idle or very low utilization |
| **gn1** | GPU | GPU-attached workloads |
| **o1** | Bursty | High variance, short peaks |
| **n1** | Network-optimized | Future: network-heavy VMs |

**Algorithm:**

1. Compute recommended vCPU count and GiB from usage + margins.
2. Enumerate instance types from catalog (built-in defaults + cluster CRs collected by operator).
3. Choose the **smallest** type where `spec.cpu.guest >= rec_vcpu` and `spec.memory.guest >= rec_gib`.
4. Classify workload → series using CPU/memory ratio, idle flag, and optional preference labels.
5. If VM uses **raw** `spec.template.spec.domain.resources` (no instance type), still emit vCPU/GiB recs; instance type field may be null.

**Operator addition (VM-E, phase11):** List `VirtualMachineClusterInstancetype` objects (and optionally preferences) into CSV or a sidecar manifest for catalog sync.

---

## Engine design

### Plugin registration

| Property | Value |
|----------|-------|
| Plugin name | `vm` |
| Phase | **1 — Produce** (ingest + recommend + persist) |
| Priority | **40** (after `pvc` 30, `quota` 35, `cluster-quota` 36) |
| Gate | `ROS_ENABLE_VM_RECS` (default **`true`**) |
| Auto-detection | If no `ros-openshift-vm-usage-*.csv` arrives, plugin **no-ops silently** (same pattern as GPU plugins) |
| Pattern | Same as `container`: ingest CSV → `daily_vm_digests` → `recommendVM()` → `vm_recommendations` |

### Pipeline

```
ros-openshift-vm-usage-*.csv (operator, 15-min)
        │
        ▼
  ParseVMRows() ──► ComputeVMDigests() ──► Upsert daily_vm_digests
        │
        ▼
  recommendVM()  (batch read digests, all terms in memory)
        │
        ▼
  vm_recommendations + notifications (18/19, disk/IOPS info codes)
        │
        ▼
  GET /recommendations/openshift/virtual-machines[/:id]
```

### Key differences from containers

| Behavior | Containers | VMs |
|----------|--------------|-----|
| Output granularity | `250m`, `512Mi` | Whole vCPUs, whole GiB |
| Downsize hysteresis | Moderate | **Strong:** recommend downsize only if `rec/current < 0.60` **OR** drop ≥ **2** vCPU **or** ≥ **2** GiB |
| Upsize | Standard margins | Same — favor headroom (restart cost of under-provisioning) |
| OOM / throttling feedback | Yes | **No** (hypervisor-only); guest agent improves memory/disk signals |
| Limit vs request | Both tuned | Focus on **requests** / domain resources (limits optional in API) |

### Terms

VMs are more stable than pods; term windows use **higher minimum data** thresholds:

| Term | Window | Min data points | Rationale |
|------|--------|-----------------|-----------|
| **Short** | 7 days | 3 | Quick signal; VMs change slowly |
| **Medium** | 30 days | 14 | Weekly patterns |
| **Long** | 90 days | 30 | Quarterly behavior |

At **15-minute** sampling: 672 / 2880 / 8640 samples per term — sufficient for percentile stability.

### Default thresholds

| Parameter | Default | Notes |
|-----------|---------|-------|
| CPU percentile (cost) | 0.95 | |
| CPU percentile (performance) | 0.99 | |
| CPU margin min / max | 15% / 50% | Adaptive margin between bounds |
| Memory margin min | 20% | Wider when `confidence=moderate` |
| Downsize hysteresis ratio | **0.60** | `recommended / current` must be below this (or absolute drop rule) |
| Min vCPU change to recommend | **2** | Avoid noisy 1-vCPU churn |
| Min GiB change to recommend | **2** | |
| Idle CPU threshold (Linux) | 50 millicores | p95 |
| Idle memory threshold (Linux) | 512 MiB | p95 |
| Idle CPU threshold (Windows) | 200 millicores | p95 |
| Idle memory threshold (Windows) | 3072 MiB | p95 |
| Disk headroom | 25% | On projected size |
| Disk projection window | 30 days | Linear trend on daily max allocated |
| Disk round step | 10 GiB | |
| High IOPS hint (read+write p95) | 3000 | Informational |

Configurable via Settings API and env locks (see [Configuration](#configuration)).

---

## Metrics — Strategy 3 (14 ROS queries at 15-min)

All VM metrics are collected at **15-minute** resolution. The operator emits a **dual-CSV**: 15-min for ROS, hourly aggregate for Koku. **14 `ros:vm_*` queries** cover rightsizing, I/O, filesystem (guest agent), and metadata.

### Full metrics table

| # | ROS query | Prometheus source | In Koku hourly CSV? | Notes |
|---|-----------|-------------------|---------------------|-------|
| 1 | `ros:vm_cpu_usage_cores` | `rate(kubevirt_vmi_cpu_usage_seconds_total[5m])` | Yes (aggregated) | Same series as `cost:vm_cpu_usage` |
| 2 | `ros:vm_cpu_request_cores` | `kubevirt_vm_resource_requests{resource='cpu'}` | Yes | |
| 3 | `ros:vm_cpu_limit_cores` | `kubevirt_vm_resource_limits{resource='cpu'}` | Yes | |
| 4 | `ros:vm_memory_usage_bytes` | `kubevirt_vmi_memory_used_bytes` | Yes | Hypervisor view |
| 5 | `ros:vm_memory_request_bytes` | `kubevirt_vm_resource_requests{resource='memory'}` | Yes | |
| 6 | `ros:vm_memory_available_bytes` | `kubevirt_vmi_memory_available_bytes` | No | Headroom / balloon visibility |
| 7 | `ros:vm_disk_read_iops` | `rate(kubevirt_vmi_storage_iops_read_total[5m])` | No | **Day one** — IOPS profile |
| 8 | `ros:vm_disk_write_iops` | `rate(kubevirt_vmi_storage_iops_write_total[5m])` | No | **Day one** |
| 9 | `ros:vm_disk_read_bytes_per_sec` | `rate(kubevirt_vmi_storage_read_traffic_bytes_total[5m])` | No | **Day one** — throughput |
| 10 | `ros:vm_disk_write_bytes_per_sec` | `rate(kubevirt_vmi_storage_write_traffic_bytes_total[5m])` | No | **Day one** |
| 11 | `ros:vm_disk_allocated_bytes` | `kubevirt_vm_disk_allocated_size_bytes` | Yes | Allocation trending |
| 12 | `ros:vm_info` | `kubevirt_vmi_info{phase='running'}` | Yes | OS label, instance type, join key |
| 13 | `ros:vm_filesystem_used_bytes` | `kubevirt_vmi_filesystem_used_bytes` | No | **Guest agent** — per mountpoint |
| 14 | `ros:vm_filesystem_capacity_bytes` | `kubevirt_vmi_filesystem_capacity_bytes` | No | **Guest agent** — per mountpoint |

**Not in ROS CSV:** `cost:vm_labels`, socket/thread CPU breakdown — cost-only unless needed for filtering.

### Detection filter

All VM queries **must** join `kubevirt_vmi_info{phase='running'}` on `(name, namespace)` — do not rely on pod labels alone.

---

## Configuration

### Master gate and auto-detection

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLE_VM_RECS` | **`true`** | Registers VM plugin; set `false` to disable entirely |

When enabled but **no VM CSV** is present in uploads, the plugin **no-ops silently** (no errors, no empty recommendations) — same pattern as GPU recommendation plugins.

### Three-tier precedence

Same model as containers, nodes, and PVCs: **compiled defaults → tenant Settings API → env locks**.

| Endpoint | Purpose |
|----------|---------|
| `GET/PUT/DELETE .../settings/ros/thresholds/?recommendation_type=vm` | Threshold overrides (percentiles, margins, idle, disk, IOPS) |
| `GET/PUT/DELETE .../settings/ros/terms/?recommendation_type=vm` | Term windows and min data points |

Env-locked fields follow existing `ROS_*` lock semantics documented in [configurability.md](../architecture/configurability.md).

### Environment variables (full list)

| Variable | Default | Configurable via Settings API |
|----------|---------|------------------------------|
| `ROS_ENABLE_VM_RECS` | `true` | No (deployment gate) |
| `ROS_VM_CPU_PERCENTILE` | `0.95` | Yes |
| `ROS_VM_CPU_PERCENTILE_PERFORMANCE` | `0.99` | Yes |
| `ROS_VM_CPU_MARGIN_MIN` | `0.15` | Yes |
| `ROS_VM_CPU_MARGIN_MAX` | `0.50` | Yes |
| `ROS_VM_MEMORY_MARGIN_MIN` | `0.20` | Yes |
| `ROS_VM_DOWNSIZE_HYSTERESIS` | `0.60` | Yes |
| `ROS_VM_MIN_VCPU_CHANGE` | `2` | Yes |
| `ROS_VM_MIN_GIB_CHANGE` | `2` | Yes |
| `ROS_VM_IDLE_CPU_MC` | `50` | Yes |
| `ROS_VM_IDLE_MEMORY_MIB` | `512` | Yes |
| `ROS_VM_IDLE_CPU_MC_WINDOWS` | `200` | Yes |
| `ROS_VM_IDLE_MEMORY_MIB_WINDOWS` | `3072` | Yes |
| `ROS_VM_DISK_HEADROOM` | `0.25` | Yes |
| `ROS_VM_DISK_PROJECTION_DAYS` | `30` | Yes |
| `ROS_VM_DISK_ROUND_GIB` | `10` | Yes |
| `ROS_VM_HIGH_IOPS_HINT` | `3000` | Yes |

---

## API

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/cost-management/v1/recommendations/openshift/virtual-machines/` | Paginated VM recommendation list |
| GET | `/api/cost-management/v1/recommendations/openshift/virtual-machines/:id` | Detail for one VM |

### Filters

- `filter[vm_name]`
- `filter[namespace]`
- `filter[cluster]` (cluster UUID)
- `filter[recommendation_status]` — e.g. active, idle, oversized
- `filter[confidence]` — `high`, `moderate`

### Response shape (conceptual)

Per VM (aligned with requirements § VM Recommendation Response Format):

- **Identity:** `vm_name`, `namespace`, `cluster_id`, `guest_os_name`, `vm_instance_type` (current)
- **Current / recommended:** vCPU, memory_gib, disk_gib (and per-mountpoint disk when guest agent present)
- **Instance type:** `recommended_instance_type` (e.g. `m1.large`), `series`, optional `preference`
- **Quality:** `guest_agent_detected`, `confidence` (`"high"` | `"moderate"`)
- **Flags:** `idle`, `oversized`, `abandoned` (future)
- **Terms:** which term drove the primary recommendation
- **IOPS profile:** read/write p95 IOPS and throughput (informational)
- **Notifications:** codes 18/19 + informational storage messages

---

## Implementation priority

| Phase | Work | Effort | Owner |
|-------|------|--------|-------|
| **VM-A** | Operator: Strategy 3 — 14 `ros:vm_*` at 15-min + dual-CSV + manifest entry | **Moderate** | koku-metrics-operator |
| **VM-B** | ros-ocp-backend: migrations (`daily_vm_digests`, `vm_recommendations`), CSV parser, digest upsert | **Low–moderate** | ros-ocp-backend |
| **VM-C** | `recommendVM()`, guest agent branches, OS idle, notifications 18/19 | **Moderate** | ros-ocp-backend |
| **VM-D** | API list/detail + settings/terms for `recommendation_type=vm` | **Low** | ros-ocp-backend + Koku API proxy if needed |
| **VM-E** | Instance type catalog + series classification (**phase11**) | **Low** (parallel with VM-C) | operator + ros-ocp-backend |

**VM-A** and **VM-E** can start in parallel. **VM-B/C** need VM-A for end-to-end validation (mock CSVs suffice for unit tests).

---

## Dependencies

| Dependency | Notes |
|------------|-------|
| koku-metrics-operator VM-A | Strategy 3 dual-CSV + 14 queries at 15-min |
| Koku cost pipeline | **No change** — continues ingesting hourly `cm-openshift-vm-usage-*.csv` |
| Instance type catalog | VM-E: operator list of `VirtualMachineClusterInstancetype` + default catalog in Go |
| Feature gate | `ROS_ENABLE_VM_RECS` (default on; auto-no-op without data) |
| UI | Optimizations VM views; namespace flag for on-prem (`cost-management.koku-ui-ros.namespace`) |

---

## Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| No in-guest disk **used** % (hypervisor-only) | Allocation-only disk recs | Guest agent → filesystem queries 13–14; `confidence=moderate` + wider margins |
| No guest OOM in Prometheus | Memory upsizing less reactive | Higher memory margin; performance engine p99; guest agent working-set when present |
| Instance types optional | Many VMs use raw domain resources | vCPU/GiB recs still valuable; nullable instance type field |
| Balloon driver not enabled | `memory_available` misleading | Document metric prerequisite; fall back to usage-only |
| VM restart churn | Operator ignores noisy recs | Strong downsize hysteresis (0.60, min 2 vCPU/GiB delta) |
| Windows idle false negatives (uniform threshold) | Missed idle VMs | **OS-aware idle thresholds** from day one via `os` label |
| Upload size growth | Operator PVC / bandwidth | ~1 MB/compressed upload per 1k VMs per 6h cycle — negligible |

---

## Database objects (target)

See [requirements.md §18](../architecture/requirements.md) for canonical DDL sketches:

- `daily_vm_digests` — partitioned by `bucket_date`; BIGINT metrics (`_mc`, `_kib`, IOPS columns); optional filesystem columns per mountpoint
- `vm_recommendations` — current vs recommended resources, flags, instance type, IOPS JSON, `guest_agent_detected`, `confidence`

No raw 15-min VM metrics in PostgreSQL — same digest-only model as containers.

---

## Testing (planned)

From [test-plan.md](../architecture/test-plan.md#phase-8b-vm-right-sizing):

- VM CSV → `daily_vm_digests` population (15-min rows aggregated to daily)
- Whole vCPU / whole GiB rounding
- Windows memory floor ≥ 2 GiB; Windows idle thresholds
- Downsize hysteresis gates
- Instance type smallest-fit selection
- Guest agent vs hypervisor-only code paths; `confidence` field
- API list/detail contract tests

---

## References

- [requirements.md §12b — Phase 8b](../architecture/requirements.md#12b-phase-8b-vm-recommendations-weeks-1218)
- [performance-analysis.md §30](../architecture/performance-analysis.md#30-openshift-virtualization-vm-recommendations)
- [known-issues.md](../known-issues.md) — VM notification codes without plugin
- [plugin-phases.md](../architecture/plugin-phases.md) — `vm` priority 40
- koku-metrics-operator: `internal/collector/queries.go` (`cost:vm_*` / `ros:vm_*` queries)
- ros-ocp-backend: `internal/engine/notifications.go` (`NotifVMIdle`, `NotifVMOversized`)
