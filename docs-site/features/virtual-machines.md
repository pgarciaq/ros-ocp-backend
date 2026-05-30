# Virtual Machine Recommendations

!!! warning "Status: Planned / Future Work"
    This feature is **not yet implemented**. The description below is the intended
    product direction for a future ROS-OCP release. Container, node, PVC, quota, and
    GPU recommendations remain available today.

!!! info "Quick Facts (planned)"
    **Scope:** OpenShift Virtualization (KubeVirt) virtual machines  
    **Plugin:** `vm` (Produce phase)  
    **Analysis windows:** 7 / 30 / 90 days (configurable)  
    **Gate:** `ROS_ENABLE_VM_RECS` (off by default until release)  
    **Units:** Whole vCPUs and whole GiB (not millicores)

---

## What it does

**Virtual Machine Recommendations** right-size OpenShift Virtualization workloads the
same way ROS already optimizes containers — but tuned for how VMs actually run on KubeVirt.

For each virtual machine, ROS will analyze historical CPU, memory, and disk signals
and recommend:

- Optimal **vCPU count** (whole cores, not millicores)
- Optimal **memory** (whole GiB, with guest-OS-aware floors)
- Best-fit **instance type** from the OpenShift Virtualization catalog (for example `m1.large`)
- **Idle** VMs that consume cluster resources with almost no utilization
- **Disk growth** and **I/O profile** guidance for storage planning

Example messages you can expect in the Optimizations UI:

> *"Your VM uses 2 of its 8 allocated cores; consider **m1.large** (4 vCPU)."*  
> *"Memory recommendation: **8 GiB** (currently 32 GiB allocated)."*  
> *"VM `legacy-app-02` has been idle for 45 days — CPU and memory usage are near zero."*  
> *"At current growth rate (2.1 GiB/day), disk will be full in 45 days — expand by 100 GiB now."*  
> *"High IOPS detected (p95: 4,200 IOPS); consider a faster storage class than `standard`."*

---

## The problem — why VMs are different from containers

### Lift-and-shift over-provisioning

VMs migrated from VMware, Hyper-V, or physical hardware often land on OpenShift with
**the same sizing as the old world**:

> *"We gave it **16 vCPUs** and **64 GiB** because that's what the physical server had."*

Industry experience shows a large share of VMs are **idle or substantially oversized** —
often **more waste at the VM layer** than at the pod layer, because VMs change less
frequently and carry legacy assumptions.

### Resize is expensive — recommendations must be confident

| Aspect | Containers | Virtual machines |
|--------|------------|------------------|
| Typical change | Patch requests/limits; rolling restart | Change domain resources or instance type |
| Downtime | Often minimal with rolling updates | **Usually full VM restart** or disruptive live migration |
| Granularity | 250m CPU, 512 MiB | **Whole vCPUs**, **whole GiB** |
| Operator trust | Frequent small tweaks acceptable | Fewer, higher-confidence actions |

Unlike containers, shrinking a VM by one vCPU still implies a **maintenance window** for many teams. ROS will be **conservative on downsizing** and more willing to recommend **upsize** when usage shows starvation risk.

### Full guest vs cgroup view

VMs run a **full guest operating system** with its own scheduler, page cache, and
background services. Container metrics reflect cgroup limits on a single process
group; VM metrics reflect the **virt-launcher** pod and hypervisor view of the guest.

That means:

- Memory "usage" may not expose in-guest OOM without a guest agent
- CPU usage can look low while the guest is actually **waiting on disk or network**
- Disk recommendations start from **allocated volume size** unless guest tools are present

### Cost visibility without optimization

Koku already reports **what VMs cost** through the metrics operator and cost models.
This feature closes the loop: **what you should run instead**, mapped to OpenShift's
native **VirtualMachineClusterInstancetype** catalog.

---

## Recommendation types

### vCPU optimization

Analyzes sustained CPU usage (high percentiles over your chosen window), applies a
safety margin, and rounds **up** to whole vCPUs so the guest always has integer cores.

**Example:**

> *"VM `erp-backend` in namespace `finance`: allocated **8 vCPUs**, p95 usage **1.7 cores**
> over 30 days. Recommend **4 vCPUs** via **m1.large** — saves **4 vCPUs** of cluster
> capacity with 20% headroom above p95."*

**Why it matters:** Cluster schedulers reserve full vCPU requests; oversized VM CPU
blocks other workloads from scheduling even when the guest is idle.

**Customer view:** Current vs recommended vCPU, optional monthly savings when cost
rates are configured.

### Memory optimization

Uses percentile-based sizing with minimum headroom above peak usage. Applies sensible
**floors** by guest OS family (higher baseline for Windows guests) before rounding to whole GiB.

**Example:**

> *"Peak memory usage is **8.2 GiB** over 90 days (p95). Current allocation: **32 GiB**.
> Recommend **16 GiB** (p95 + 20% margin, rounded to whole GiB)."*

**Why it matters:** Memory is reserved for the VM's request; 32 GiB allocated for an
8 GiB workload is pure stranded RAM on the node.

**Customer view:** *"You allocated 32 GiB; usage supports 16 GiB."*

### Instance type suggestions

When your cluster defines **VirtualMachineClusterInstancetype** resources, ROS maps
recommended vCPU and memory to the **smallest cost-effective type** that still fits the workload profile.

**Example:**

> *"Your VM shows CPU-heavy pattern (**75%** CPU utilization vs **20%** memory pressure).
> Recommend **cx1.large** instead of **m1.2xlarge**."*

VMs defined with raw CPU/memory in the VM spec (no instance type) still receive vCPU
and memory guidance; instance type fields may be omitted in the API.

### Idle VM detection

Flags VMs whose CPU and memory usage stay below very low thresholds for the analysis
window — candidates for power-off, archival, or deletion review.

**Example:**

> *"VM `legacy-app-02` has been idle (**< 50 millicores** CPU p95, **< 512 MiB** memory p95)
> for **45 days**. Consider decommissioning."*

**Why it matters:** Idle VMs often still incur **full instance-type or node reservation**
cost. This is **full waste** of provisioned resources, not incremental rightsizing savings.

### Disk growth trending

Tracks provisioned disk size over time, projects growth forward (default **30-day**
linear trend), adds headroom, and rounds to practical sizes (for example **10 GiB** steps).

**Example:**

> *"At current growth rate (**2.1 GiB/day**), disk will reach allocated capacity in
> **45 days**. Recommend expanding by **100 GiB** now to avoid emergency resize."*

!!! note "Allocated vs used"
    Without a QEMU guest agent, ROS sees **allocated** disk capacity from the platform,
    not in-guest filesystem free space. Recommendations focus on **allocation trends**
    and capacity planning, not filesystem utilization percentages.

**Why it matters:** PVC-style surprises apply to VM disks; lead time beats paging at 95% full.

### Disk I/O profile (informational)

For storage-heavy VMs, ROS summarizes read/write **IOPS** and throughput peaks.

**Example:**

> *"High IOPS workload: read+write p95 **4,200 IOPS**. Storage class `standard` may be
> insufficient — consider **`fast`** or SSD-backed classes."*

**Why it matters:** Database and analytics VMs are often **I/O bound** while CPU looks healthy.

Specific storage class names are **not** auto-selected in the first release — classes
vary by cluster (Ceph, cloud disks, NFS, etc.). ROS provides **actionable hints**, not blind StorageClass changes.

---

## Instance type intelligence

### What instance types are

OpenShift Virtualization provides **VirtualMachineClusterInstancetype** (and preference)
CRDs — a cluster-wide catalog of **approved VM sizes**, similar in spirit to cloud
instance families (but running on your metal or cloud workers).

Each instance type specifies:

- **Guest vCPU** count (whole cores)
- **Guest memory** (GiB)
- Optional **series** metadata (compute-optimized, general purpose, GPU, etc.)

Using instance types enforces **standard sizes** across teams, simplifies quota math,
and makes recommendations portable in the UI ("move from m1.2xlarge to m1.large").

### Series breakdown

| Series | Profile | Best for | Example workload |
|--------|---------|----------|------------------|
| **cx1** | Compute-optimized (high CPU:memory ratio) | CPU-bound batch, encoders, compilers | Java build farm, video transcode |
| **m1** | General purpose (balanced) | Most enterprise apps | ERP, web app servers, middleware |
| **u1** | Micro / utility | Low footprint services, dev sandboxes, jump boxes | Small Linux utility VM |
| **gn1** | GPU-attached | ML inference, rendering, CUDA workloads | GPU inference guest |
| **o1** | Overcommitted / burstable | Dev/test, bursty non-production | CI dev environments |
| **n1** | Network-optimized | High packet rates, proxies, load generators | Network lab VMs (when available) |

Sizes typically run from **small** through **8xlarge** within each series.

### How ROS maps usage to a series

1. Compute recommended **vCPU** and **GiB** from usage percentiles + margins.
2. Derive **CPU:memory utilization ratio** and variance (bursty vs steady).
3. Apply **idle** and **GPU** flags from metrics and labels.
4. Honor **VirtualMachinePreference** hints when present (development vs high-performance).
5. Select the **smallest** instance type in the chosen series that satisfies recommended vCPU and memory.

**Walkthrough example:**

| Signal | Interpretation |
|--------|----------------|
| CPU p95 = 75% of request, memory p95 = 20% of request | CPU-heavy → bias **cx1** |
| Both CPU and memory moderate, steady | Default **m1** |
| Very low utilization 30+ days | **u1** or idle notification |
| GPU metrics or instance type already gn1 | Stay in **gn1** family |
| High short-term variance, non-prod labels | **o1** candidate |

**Customer message:**

> *"Workload profile: CPU-heavy. Recommend **cx1.large** (4 vCPU, 8 GiB) instead of
> **m1.2xlarge** (8 vCPU, 32 GiB)."*

### Custom instance types

Clusters may define **custom** `VirtualMachineClusterInstancetype` objects. ROS will
incorporate them into the catalog the same way as built-in series — recommendations
always reference types that **exist in your cluster**.

---

## Conservative approach — why we are careful with VMs

VM resize implies **restart risk** for many applications. ROS biases toward **operator trust**:

| Direction | Behavior | Rationale |
|-----------|----------|-----------|
| **Downsize** | Only when usage is clearly low **and** change is meaningful: recommend downsize only if recommended/current **< 0.60** **or** drop ≥ **2 vCPU** **or** ≥ **2 GiB** | Avoid "save one core" churn that still needs a maintenance window |
| **Upsize** | Standard safety margins; performance engine uses higher percentiles | Under-provisioned VMs fail loudly after restart |
| **Idle** | Informational; triggers cleanup workflows, not auto power-off | Decommission is a business decision |
| **Storage class** | Informational I/O hints only in MVP | Wrong storage class change can break databases |

**Why:** Platform teams need to justify a change window to application owners. Noisy
recommendations get ignored; a smaller set of high-confidence recs gets adopted.

---

## Time windows — why VMs use longer terms than containers

| Resource | Typical ROS terms | Default windows | Min data points (planned) |
|----------|-------------------|-----------------|---------------------------|
| **Containers** | short / medium / long | 1 / 7 / 15 days | 1 / 3 / 7 days |
| **Virtual machines** | short / medium / long | **7 / 30 / 90 days** | **3 / 14 / 30 days** |

**Why longer for VMs:**

- VMs are **long-running**; a single busy day should not drive a downsize.
- Weekly and monthly business cycles appear in **30–90 day** windows.
- Restart cost means recommendations should reflect **sustained** behavior, not pod-style noise.

**Customer impact:** Short-term spikes still appear in metrics, but ROS waits until
the pattern is stable across the configured window before recommending a smaller size.

---

## Metrics reuse — no duplicate collection burden for basics

ROS is designed to **reuse** metrics the koku-metrics-operator already collects for
**cost reporting** wherever possible:

| Signal | Already in cost pipeline? | ROS adds |
|--------|---------------------------|----------|
| CPU usage / request / limit | Yes (`cost:vm_*`) | Same series at ROS cadence |
| Memory usage / request | Yes | Optional available-bytes for headroom |
| Disk allocated size | Yes | Growth trend |
| VM info (OS, instance type) | Yes | Catalog matching |
| Disk read/write IOPS | No | **New** queries for I/O profile |
| Disk throughput | No | **New** queries for I/O profile |

**Customer-friendly summary:** You do **not** need a second monitoring stack for
basic VM rightsizing — hourly VM usage CSVs already flow to Koku. ROS adds a focused
set of **disk I/O** metrics for storage-heavy guidance.

Additional IOPS/throughput collection is only required if you want **storage performance**
recommendations; CPU/memory rightsizing works with existing cost metrics.

---

## How it works (high level)

```mermaid
flowchart LR
  subgraph Cluster["OpenShift cluster"]
    O[Metrics operator]
    KV[KubeVirt VMs]
    IT[Instance type catalog]
    KV --> O
    IT --> O
  end
  O -->|VM usage CSV| R[ROS-OCP Backend]
  R --> D[Daily VM digests]
  D --> E[Recommendation engine]
  E --> API[REST API]
  API --> UI[Cost Management UI]
```

1. **Collect** — Metrics operator gathers KubeVirt Prometheus metrics (CPU, memory, disk; I/O when enabled).
2. **Summarize** — ROS builds **daily digests** per VM (percentiles and peaks), same philosophy as containers.
3. **Analyze** — Over **7-, 30-, or 90-day** windows, percentile-based sizing with VM-specific downsize rules.
4. **Map** — Match recommended resources to **instance types** where a catalog exists.
5. **Deliver** — List and detail APIs expose current vs recommended values and notification codes.

---

## Configuration

VM recommendations use the **same three-tier configuration model** as containers,
nodes, and PVCs:

| Tier | Source | Example |
|------|--------|---------|
| 1 | Compiled defaults | 95th percentile CPU, 20% memory margin |
| 2 | Tenant Settings API | Adjust idle thresholds, downsize ratio |
| 3 | Environment locks | Platform admin enforces minimum change sizes |

**Thresholds:**  
`GET/PUT/DELETE .../settings/ros/thresholds/?recommendation_type=vm`

**Analysis windows (terms):**  
`GET/PUT/DELETE .../settings/ros/terms/?recommendation_type=vm`

| Parameter (planned) | Default | Purpose |
|---------------------|---------|---------|
| CPU percentile (cost) | 0.95 | Sustained CPU for rightsizing |
| CPU percentile (performance) | 0.99 | Headroom for critical VMs |
| Downsize hysteresis ratio | 0.60 | Must be below 60% of current to downsize |
| Min vCPU change | 2 | Minimum cores delta to recommend |
| Min GiB change | 2 | Minimum memory delta to recommend |
| Idle CPU threshold | 50 millicores | p95 below → idle candidate |
| Idle memory threshold | 512 MiB | p95 below → idle candidate |
| Disk projection window | 30 days | Linear growth horizon |
| High IOPS hint | 3000 | Informational read+write p95 |

Master gate: **`ROS_ENABLE_VM_RECS`** (off until release).

See [Configurable thresholds](configurable-thresholds.md) for precedence rules.

---

## API (planned)

| Endpoint | Purpose |
|----------|---------|
| `GET .../recommendations/openshift/virtual-machines/` | List VM recommendations |
| `GET .../recommendations/openshift/virtual-machines/:id` | Detail for one VM |

**Filters:** `filter[vm_name]`, `filter[namespace]`, `filter[cluster]`, `filter[recommendation_status]`

### Example list item (abbreviated)

```json
{
  "vm_name": "erp-backend",
  "namespace": "finance",
  "cluster_id": "prod-east",
  "guest_os_name": "RHEL 9",
  "current": {
    "vcpu": 8,
    "memory_gib": 32,
    "instance_type": "m1.2xlarge",
    "disk_gib": 500
  },
  "recommended": {
    "vcpu": 4,
    "memory_gib": 16,
    "instance_type": "m1.large",
    "disk_gib": 600
  },
  "notifications": [
    {"code": 19, "type": "INFO", "message": "VM is oversized based on 30-day usage."}
  ],
  "term": "medium_term",
  "engine": "cost",
  "disk_projection": {
    "days_until_full": 45,
    "growth_gib_per_day": 2.1,
    "recommended_expand_gib": 100
  },
  "io_profile": {
    "read_iops_p95": 2100,
    "write_iops_p95": 2100,
    "hint": "Consider faster storage class"
  }
}
```

Full shapes will align with the [UI Integration Guide](../ui-integration-guide.md).

---

## Prerequisites

| Requirement | Why |
|-------------|-----|
| **OpenShift Virtualization** installed | KubeVirt metrics and VM CRs |
| **VMs running** with active VMIs | Metrics join on `phase='running'` |
| **Metrics operator** uploading VM usage CSV | Data path into ROS |
| **Instance types configured** (optional) | Enables `m1.large`-style recommendations; raw sizing still works without them |
| **Guest agent** (optional) | Improves memory/disk insight; not required for MVP |

---

## Comparison to container recommendations

| Topic | Containers | Virtual machines |
|-------|------------|------------------|
| Units | millicores, Mi/Gi | vCPUs, GiB |
| Typical action | Patch deployment resources | Resize VM / change instance type |
| Downtime | Often rolling | Usually full restart for size change |
| Idle detection | Available today | Planned (VM-specific thresholds) |
| Instance types | N/A | cx1, m1, u1, gn1, o1, n1, … |
| OOM feedback | Yes | Limited without guest agent |

Container recommendations are **unchanged** when VM recommendations ship.

---

## Related documentation

| Document | Audience |
|----------|----------|
| [Internal design: VM recommendations](../../../docs/design/vm-recommendations.md) | Engineering — metrics, algorithms, phases |
| [Seasonality](seasonality.md) | Planned proactive peaks for long-running VMs |
| [Container right-sizing](container-recommendations.md) | Available today |
| [Configurable thresholds](configurable-thresholds.md) | Settings API precedence |
| [Dual engine](dual-engine.md) | Cost vs performance profiles |

---

## Coming soon checklist

When this feature ships, expect:

- [ ] VM usage CSV from the metrics operator (including disk I/O metrics)
- [ ] VM recommendations in the Optimizations API and UI
- [ ] Settings and terms for `recommendation_type=vm`
- [ ] Idle and oversized VM badges (notification codes 18–19)
- [ ] Optional savings estimates tied to VM cost model rates in Koku

Until then, use **container**, **node**, and **PVC** recommendations for workload
optimization; use **OpenShift cost reports** for VM spend visibility.
