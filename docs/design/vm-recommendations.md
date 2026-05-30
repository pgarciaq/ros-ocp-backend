# OpenShift Virtualization Recommendations (Planned)

**Status:** Planned / Future Work  
**Last updated:** 2026-05-30  
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

**Goal:** Add a `vm` plugin (Phase 1 Produce) gated by `ROS_ENABLE_VM_RECS` that ingests VM usage digests, runs `recommendVM()` in Go, and exposes list/detail APIs aligned with container recommendations.

---

## Current state

### koku-metrics-operator (cost path)

The operator collects **~11 `cost:vm_*` queries** at **hourly** granularity for billing:

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

**Output:** `cm-openshift-vm-usage-<YYYYMM>.csv` in the operator upload tarball.

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
| `ROS_ENABLE_VM_RECS` | Planned master gate (default `false`) |

---

## Recommendation types

### 1. vCPU right-sizing

- **Input:** p95 CPU usage (millicores) over the active term window.
- **Algorithm:** Apply adaptive margin (minimum **15%**, maximum **50%**), convert to vCPUs, **ceil** to whole vCPUs (minimum 1).
- **Engines:** Cost engine uses p95; performance engine uses p99 (same pattern as containers).

### 2. Memory right-sizing

- **Input:** p95 memory usage (KiB).
- **Algorithm:** Minimum **20%** margin above p95; apply **guest OS floor** (Windows: **2 GiB**, Linux: **0.5 GiB**); **ceil** to whole GiB (minimum 1 GiB).
- **Note:** Without QEMU guest agent, in-guest OOM is not visible in Prometheus — recommendations cannot use OOM feedback (unlike containers).

### 3. Instance type matching

Map recommended vCPU + memory to the **smallest** `VirtualMachineClusterInstancetype` that satisfies both dimensions.

- **Catalog:** Built-in OpenShift Virt defaults (series below) + operator-collected custom types from cluster API list.
- **`VirtualMachinePreference` CRDs:** Optional hints for workload class (e.g. high-performance vs development) to bias series selection.

### 4. Idle / zombie VM detection

Flag VMs where **both**:

- CPU p95 **< 50 millicores**
- Memory p95 **< 512 MiB**

Emits `NotifVMIdle` (code 18). Distinct from rightsizing — full allocated waste, not marginal savings.

### 5. Disk size trending

- **Input:** Daily max `disk_allocated_bytes` (and growth on usage proxy where available).
- **Algorithm:** MAX allocated + **30-day** linear trend projection + **25%** headroom → round up to nearest **10 GiB**.
- **Limitation:** No in-guest “used %” without guest agent — only **allocated** capacity is observable.

### 6. Disk I/O profile (informational)

- **Input:** p95 read/write IOPS and throughput (new metrics).
- **Output:** Notification when total p95 IOPS exceeds **3000** — suggests reviewing storage class performance; **no automatic storage class name** in MVP (catalog varies per cluster).

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

**Operator addition (VM-E):** List `VirtualMachineClusterInstancetype` objects (and optionally preferences) into CSV or a sidecar manifest for catalog sync.

---

## Engine design

### Plugin registration

| Property | Value |
|----------|-------|
| Plugin name | `vm` |
| Phase | **1 — Produce** (ingest + recommend + persist) |
| Gate | `ROS_ENABLE_VM_RECS=true` |
| Pattern | Same as `container`: ingest CSV → `daily_vm_digests` → `recommendVM()` → `vm_recommendations` |

### Pipeline

```
ros-openshift-vm-usage-*.csv (operator)
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
| OOM / throttling feedback | Yes | **No** |
| Limit vs request | Both tuned | Focus on **requests** / domain resources (limits optional in API) |

### Terms

VMs are more stable than pods; term windows use **higher minimum data** thresholds:

| Term | Window | Min data points | Rationale |
|------|--------|-----------------|-----------|
| **Short** | 7 days | 3 | Quick signal; VMs change slowly |
| **Medium** | 30 days | 14 | Weekly patterns |
| **Long** | 90 days | 30 | Quarterly behavior |

At **hourly** sampling: 168 / 720 / 2160 samples per term — sufficient for percentile stability.

### Default thresholds

| Parameter | Default | Notes |
|-----------|---------|-------|
| CPU percentile (cost) | 0.95 | |
| CPU percentile (performance) | 0.99 | |
| CPU margin min / max | 15% / 50% | Adaptive margin between bounds |
| Memory margin min | 20% | |
| Downsize hysteresis ratio | **0.60** | `recommended / current` must be below this (or absolute drop rule) |
| Min vCPU change to recommend | **2** | Avoid noisy 1-vCPU churn |
| Min GiB change to recommend | **2** | |
| Idle CPU threshold | 50 millicores | p95 |
| Idle memory threshold | 512 MiB | p95 |
| Disk headroom | 25% | On projected size |
| Disk projection window | 30 days | Linear trend on daily max allocated |
| Disk round step | 10 GiB | |
| High IOPS hint (read+write p95) | 3000 | Informational |

Configurable via Settings API and env locks (see [Configuration](#configuration)).

---

## Metrics — reuse strategy

**MVP:** Reuse **hourly** `cost:vm_*` overlap where possible — operator already pays the query cost for Koku. Add **5 new `ros:vm_*` queries** for ROS-only signals.

### Full metrics table (12 ROS queries)

| # | ROS query | Prometheus source | Collected today? | Reuse `cost:vm_*`? | Notes |
|---|-----------|-------------------|------------------|-------------------|-------|
| 1 | `ros:vm_cpu_usage_cores` | `rate(kubevirt_vmi_cpu_usage_seconds_total[5m])` | Yes (usage) | **Yes** — `cost:vm_cpu_usage` | Same series; hourly OK for MVP |
| 2 | `ros:vm_cpu_request_cores` | `kubevirt_vm_resource_requests{resource='cpu'}` | Yes | **Yes** — `cost:vm_cpu_request_cores` | |
| 3 | `ros:vm_cpu_limit_cores` | `kubevirt_vm_resource_limits{resource='cpu'}` | Yes | **Yes** — `cost:vm_cpu_limit_cores` | |
| 4 | `ros:vm_memory_usage_bytes` | `kubevirt_vmi_memory_used_bytes` | Yes | **Yes** — `cost:vm_memory_usage_bytes` | |
| 5 | `ros:vm_memory_request_bytes` | `kubevirt_vm_resource_requests{resource='memory'}` | Yes | **Yes** — `cost:vm_memory_request_bytes` | |
| 6 | `ros:vm_memory_available_bytes` | `kubevirt_vmi_memory_available_bytes` | **No** | **No — NEW** | Headroom / balloon visibility |
| 7 | `ros:vm_disk_read_iops` | `rate(kubevirt_vmi_storage_iops_read_total[5m])` | **No** | **No — NEW** | IOPS profile |
| 8 | `ros:vm_disk_write_iops` | `rate(kubevirt_vmi_storage_iops_write_total[5m])` | **No** | **No — NEW** | |
| 9 | `ros:vm_disk_read_bytes_per_sec` | `rate(kubevirt_vmi_storage_read_traffic_bytes_total[5m])` | **No** | **No — NEW** | Throughput |
| 10 | `ros:vm_disk_write_bytes_per_sec` | `rate(kubevirt_vmi_storage_write_traffic_bytes_total[5m])` | **No** | **No — NEW** | |
| 11 | `ros:vm_disk_allocated_bytes` | `kubevirt_vm_disk_allocated_size_bytes` | Yes | **Yes** — `cost:vm_disk_allocated_size_bytes` | |
| 12 | `ros:vm_info` | `kubevirt_vmi_info{phase='running'}` | Yes | **Yes** — `cost:vm_info` | Join filter on running VMIs |

**Not in MVP ROS CSV:** `cost:vm_labels`, socket/thread CPU breakdown — cost-only unless needed for filtering.

**Post-MVP:** Unify collection at **15-minute** ROS granularity; Koku aggregates to hourly during ingestion (eliminates duplicate scrape). See REQ-8b.1 future optimization in [requirements.md §12b](../architecture/requirements.md#req-8b1-operator--vm-ros-prometheus-queries-high--not-implemented).

### Detection filter

All VM queries **must** join `kubevirt_vmi_info{phase='running'}` on `(name, namespace)` — do not rely on pod labels alone.

---

## Configuration

### Master gate

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_ENABLE_VM_RECS` | `false` | Enables VM plugin registration and processing |

### Settings API

Same three-tier precedence as other plugins: **compiled defaults → tenant DB → env locks**.

| Endpoint | Purpose |
|----------|---------|
| `GET .../settings/ros/thresholds/?recommendation_type=vm` | Read merged VM thresholds |
| `PUT .../settings/ros/thresholds/?recommendation_type=vm` | Tenant overrides |
| `DELETE .../settings/ros/thresholds/?recommendation_type=vm` | Clear tenant overrides |

**Terms API:** `GET/PUT/DELETE .../settings/ros/terms/?recommendation_type=vm` — same pattern as containers (7/30/90 defaults above).

Env-locked fields follow existing `ROS_*` lock semantics documented in [configurability.md](../architecture/configurability.md).

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

### Response shape (conceptual)

Per VM (aligned with requirements § VM Recommendation Response Format):

- **Identity:** `vm_name`, `namespace`, `cluster_id`, `guest_os_name`, `vm_instance_type` (current)
- **Current / recommended:** vCPU, memory_gib, disk_gib
- **Instance type:** `recommended_instance_type` (e.g. `m1.large`), `series`, optional `preference`
- **Flags:** `idle`, `oversized`, `abandoned` (future)
- **Terms:** which term drove the primary recommendation
- **IOPS profile:** read/write p95 IOPS and throughput (informational)
- **Notifications:** codes 18/19 + informational storage messages

---

## Implementation priority

| Phase | Work | Effort | Owner |
|-------|------|--------|-------|
| **VM-A** | Operator: 5 new `ros:vm_*` queries + CSV columns + manifest entry | **Low** | koku-metrics-operator |
| **VM-B** | ros-ocp-backend: migrations (`daily_vm_digests`, `vm_recommendations`), CSV parser, digest upsert | **Low–moderate** | ros-ocp-backend |
| **VM-C** | `recommendVM()`, notifications 18/19, wire into batch orchestration | **Moderate** | ros-ocp-backend |
| **VM-D** | API list/detail + settings/terms for `recommendation_type=vm` | **Low** | ros-ocp-backend + Koku API proxy if needed |
| **VM-E** | Instance type catalog + series classification | **Low** (after VM-C) | operator + ros-ocp-backend |

**VM-A** can start immediately in parallel with other work. **VM-B/C** need VM-A for end-to-end validation (mock CSVs suffice for unit tests).

---

## Dependencies

| Dependency | Notes |
|------------|-------|
| koku-metrics-operator VM-A | New metrics in `ros-openshift-vm-usage-*.csv` |
| Koku cost pipeline | **No change required** for MVP ROS path (separate CSV) |
| Instance type catalog | Operator list of `VirtualMachineClusterInstancetype` + hardcoded default catalog in Go |
| Feature flag | `ROS_ENABLE_VM_RECS` in ros-ocp-backend deployment |
| UI | New Optimizations views (out of scope for backend doc; tracked in requirements UI matrix) |

---

## Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| No in-guest disk **used** % | Disk recs based on allocation + trend only | Set expectations in API; optional guest agent future |
| No guest OOM in Prometheus | Memory upsizing less reactive | Higher default memory margin; performance engine p99 |
| Instance types optional | Many VMs use raw domain resources | vCPU/GiB recs still valuable; nullable instance type field |
| Balloon driver not enabled | `memory_available` misleading | Document metric prerequisite; fall back to usage-only |
| VM restart churn | Operator ignores noisy recs | Strong downsize hysteresis (0.60, min 2 vCPU/GiB delta) |
| Duplicate operator queries | ~7 overlapping cost + ros scrapes | Post-MVP unified 15-min collection |
| Windows idle false negatives | Higher idle memory baseline | Uniform 512 MiB threshold for MVP; OS-aware thresholds later |

---

## Database objects (target)

See [requirements.md §18](../architecture/requirements.md) for canonical DDL sketches:

- `daily_vm_digests` — partitioned by `bucket_date`; BIGINT metrics (`_mc`, `_kib`, IOPS columns)
- `vm_recommendations` — current vs recommended resources, flags, instance type, IOPS JSON

No raw hourly VM metrics in PostgreSQL — same digest-only model as containers.

---

## Testing (planned)

From [test-plan.md](../architecture/test-plan.md#phase-8b-vm-right-sizing):

- VM CSV → `daily_vm_digests` population
- Whole vCPU / whole GiB rounding
- Windows memory floor ≥ 2 GiB
- Downsize hysteresis gates
- Instance type smallest-fit selection
- API list/detail contract tests

---

## References

- [requirements.md §12b — Phase 8b](../architecture/requirements.md#12b-phase-8b-vm-recommendations-weeks-1218)
- [performance-analysis.md §30](../architecture/performance-analysis.md#30-openshift-virtualization-vm-recommendations)
- [known-issues.md](../known-issues.md) — VM notification codes without plugin
- koku-metrics-operator: `internal/collector/queries.go` (`cost:vm_*` queries)
- ros-ocp-backend: `internal/engine/notifications.go` (`NotifVMIdle`, `NotifVMOversized`)
