# Notification codes

> **Last verified:** 2026-08-26

Every ROS recommendation can include **notification codes**: small integers that explain
*why* a row looks the way it does (low confidence, idle workload, orphaned PVC, and so on).
Use this page as a single lookup for **all notification codes** across containers, namespaces, nodes,
GPUs, PVCs, snapshots, and virtual machines.

**Maintainer reference** (emitters, constants, migrations): `docs/architecture/notification-codes.md` in the repository.

## How notifications appear in the API

Per [ADR-0293](adrs.md), notifications are emitted **per engine** (cost and performance independently). List endpoints deduplicate codes across engines into a slim `notification_codes` int array; full `notifications` maps and usage `plots` are **detail-only** (or per-engine on detail).

| Resource | List rows | Detail / per-engine |
|----------|-----------|---------------------|
| Containers, namespaces, PVCs, snapshots | `notification_codes` (int array). Code **80** is never on list. | `recommendation_engines.{cost,performance}.notifications` map keyed by code string; map entries include `type`, `message`, `code` (Kruize-compatible shape). Nested container-detail `gpu.{term}.business_hours.notifications` may include **80** (`GPU_BH_OFFICE_WINDOW`) when BH sizing is present — not merged into parent engines or list badges. `robne recommend` JSON `business_hours_gpu_recommendations` rows may include **80**; all-hours CLI GPU rows omit `notification_codes`. |
| Nodes | `notification_codes` on list rows (deduplicated across engines). Code **79** is never on list. | `recommendation_terms.<term>.recommendation_engines.{cost,performance}.notifications` map keyed by code string (`"11"`, `"13"`, …). Code **13** may include `suggested_direction`. Code **76** message includes MachineSet name when fleet consolidation applies. Nested **detail** `business_hours.notifications` may include **79** (`NODE_BH_NOT_PEAK_SAFE`) when BH sizing is present — not merged into parent engines. `robne recommend` JSON `business_hours_node_recommendations` rows may include **79** on `notification_codes`; all-hours CLI node rows omit the key. |
| GPU time-slicing | `notification_codes` (int array, typically **36**). Code **81** is never on list. | Nested **detail** `business_hours.notifications` on `GET .../gpu/timeslicing/{node}` may include **81** (`GPU_TS_BH_CLUSTER_WINDOW`) when BH replica sizing is present — not merged into list rows, history, or GPU summary. `robne recommend` JSON `business_hours_gpu_timeslicing_recommendations` rows may include **81**; all-hours CLI timeslicing rows omit `notification_codes`. |
| Virtual machines | `notifications` (JSON array) on list. Code **82** is never on list. | Same JSON **array** on detail. Nested **detail** `business_hours.notifications` is the Kruize **map** and may include **82** (`VM_BH_OFFICE_WINDOW`) when BH sizing is present — not merged into the parent array. `robne recommend` JSON `business_hours_vm_recommendations` rows may include **82** on `notification_codes`; all-hours CLI VM rows omit the key. |

Example (container list row):

```json
"notification_codes": [1, 3, 77]
```

Example (container detail — per engine):

```json
"notifications": {
  "1": { "type": "WARNING", "message": "Less than 4 days of data available for this workload", "code": 1 },
  "3": { "type": "CRITICAL", "message": "OOM kill events detected within the analysis window", "code": 3 }
}
```

**Severity** (for badges and alerts):

| API `type` | Meaning | Suggested UI |
|------------|---------|--------------|
| `CRITICAL` / `critical` | Immediate risk or failure signal | Error / danger badge |
| `WARNING` / `warning` | Review before acting; possible waste or bottleneck | Warning badge |
| `INFO` / `info` | Context or opportunity; often informational | Info badge |

**No cost data (code 25):** Hide dollar savings (`$0.00` is misleading). Show “—” and link to
[Cost integration](cost-integration.md). Fleet summary treats clusters with only code-25 recs as
having no actionable savings.

---

## Quick reference (all codes)

| Code | Severity | Area | Summary | What to do |
|------|----------|------|---------|------------|
| 1 | WARNING | Container, Namespace | Short history / low confidence | Wait for more data or treat recommendation as tentative |
| 2 | WARNING | Container, Namespace | Stale metrics (cluster stopped reporting) | Fix operator upload; verify cluster connectivity |
| 3 | CRITICAL | Container | OOM kills in window | Increase memory request/limit or fix app memory leak |
| 4 | WARNING | Node (reserved) | PDB caveat for MachineSet | *Not emitted today* |
| 5 | INFO | Container | Idle workload (low % of requests) | Right-size down, scale to zero, or decommission |
| 6 | INFO | Container | Prior recommendation appears applied | Confirm change; no action if intentional |
| 7 | INFO | Container, Namespace | &lt; 24h of data | Wait before acting; rec may shift |
| 8 | WARNING | Container | Abandoned (zero usage 72h+) | Decommission or investigate zombie workload |
| 9 | WARNING | Container, Namespace | Memory trending up | Plan capacity; watch for OOM |
| 10 | INFO | GPU | GPU underutilized | Consider MIG, smaller profile, or remove GPU |
| 11 | INFO | Node | Node underutilized | Consider consolidation / fewer nodes |
| 12 | WARNING | Node | Node overcommitted (requests ≫ allocatable) | Reduce requests or add capacity |
| 13 | INFO | Node | CPU/memory imbalance on node | Consider different instance family / balance workloads |
| 14 | WARNING | Node (reserved) | Autoscaler at maxReplicas | *Not emitted today* |
| 15 | INFO | Node | Node idle or zombie | Review idle detection; consider consolidation or decommission |
| 16 | WARNING | Node (reserved) | Autoscaler flapping | *Not emitted today* |
| 17 | INFO | Node (reserved) | No autoscaler on variable load | *Not emitted today* |
| 18 | WARNING | VM | VM idle | Power off, delete, or resize down |
| 19 | WARNING | VM | VM oversized | Reduce vCPU/memory (may need restart) |
| 1 | WARNING | Container, Namespace, PVC | Low confidence — insufficient data for full term | Treat as early signal; confidence rises with more days |
| 20 | WARNING | PVC | PVC orphaned (zero usage) | Delete PVC if truly unused |
| 21 | WARNING | Container (reserved) | HPA at maxReplicas | *Not emitted today* |
| 22 | INFO | Container (reserved) | HPA-managed workload | *Not emitted today* |
| 23 | INFO | Node (reserved) | Instance type not in catalog | *Not emitted today* |
| 24 | INFO | Node (reserved) | Deprecated instance type | *Not emitted today* |
| 25 | INFO | Container, Node, PVC | No cost data | Configure cost model; savings N/A |
| 26 | INFO | GPU | GPU idle | Remove GPU request or reclaim GPU |
| 27 | INFO | GPU | GPU memory-bound | Larger MIG profile / more HBM |
| 28 | INFO | GPU | No profiling data | Install DCGM; classification limited |
| 29 | INFO | PVC | PVC oversized | Shrink via new PVC + migration (cannot shrink in place) |
| 30 | WARNING | PVC | PVC near full | Expand PVC or investigate growth |
| 31 | WARNING | Snapshot | Source PVC deleted | Delete orphan snapshot |
| 32 | INFO | Snapshot | Never restored | Review retention; candidate for deletion |
| 33 | INFO | Snapshot | Newer snapshot exists | Delete redundant older snapshot |
| 34 | INFO | Snapshot | Stale snapshot | Apply retention policy |
| 35 | INFO | Snapshot | Backup-tool managed | Review backup retention for cost |
| 36 | INFO | GPU, Node | GPU time-slicing candidate | Enable sharing / replicas on node — see [GPU time-slicing](../features/gpu-time-slicing.md) |
| 37 | INFO | VM | Disk growing (no guest capacity data) | Plan expansion; install qemu-guest-agent |
| 38 | INFO | VM | No guest agent | Install qemu-guest-agent for better sizing |
| 39 | WARNING | VM | High disk I/O | Faster storage class or storage-optimized type |
| 40 | WARNING | VM | Filesystem filling (&lt; 90 days) | Expand disk/filesystem soon |
| 41 | INFO | VM | Instance type recommended | Consider matching instance type CR |
| 42 | CRITICAL | VM | Filesystem &gt; 90% full | Expand immediately |
| 43 | CRITICAL | VM | VM abandoned | Delete or power off VM |
| 44 | INFO | VM | Guest agent interrupted | Stabilize agent; rec uses hypervisor metrics |
| 45 | INFO | VM | Insufficient VM metrics | Wait for ≥1 full day of samples |
| 46 | INFO | VM | Unknown guest OS | Install guest agent; Linux defaults used |
| 47 | INFO | VM | Windows update spike | P95 sizing accounts for spikes; optional ignore |
| 48 | WARNING | VM | Frequent restarts | Investigate crash loop / instability |
| 49 | INFO | VM | Downsize held (performance engine) | Usage not stable enough to downsize yet |
| 50 | WARNING | VM | VM GPU idle | Remove GPU assignment |
| 51 | WARNING | VM | VM GPU underutilized | Smaller MIG/vGPU or time-slicing |
| 52 | WARNING | VM | VM GPU memory saturated | Larger GPU or more frame buffer |
| 53 | WARNING | VM | VM GPU compute saturated | More powerful GPU |
| 54 | WARNING | VM | Mixed idle/active GPUs | Reduce GPU count on VM |
| 55 | WARNING | VM | Network-saturated workload | Consider **n1** network-optimized instance type |
| 56 | INFO | VM | vGPU profile recommended | Apply `recommended_vgpu_profile` on guest |
| 57 | WARNING | VM | GPU time-slicing unsafe (frame buffer) | Do not time-slice; resize GPU or reduce FB pressure |
| 58 | INFO | VM | Sequential I/O pattern | Consider storage optimized for throughput |
| 59 | INFO | VM | Random I/O pattern | Consider storage optimized for IOPS |
| 60 | WARNING | VM | Redundant VMs co-located on same node — consider adding anti-affinity rules | Set `metadata.is_redundant_placement`; spread HA peers with anti-affinity or topology spread |
| 61 | INFO | VM | Uneven VM distribution across nodes — consider topologySpreadConstraints | Rebalance profile/prefix groups across nodes (skew ratio) |
| 62 | INFO | VM | VM shares storage with other VMs — correlated workload group detected | Review coupled workloads; `metadata.has_shared_storage` (profile proxy until PVC on ROS CSV) |
| 63 | WARNING | VM | VM memory exceeds single NUMA node capacity — NUMA pinning not possible | Reduce memory or use hosts with larger NUMA nodes (`metadata.numa_oversized`) |
| 64 | INFO | VM | Periodically idle (power-off schedule) | Schedule power-off during inactive periods; see `is_power_off_candidate` |
| 65 | INFO | VM | Network-bound — SR-IOV may help | High throughput or packet drops on network-bound VM |
| 66 | INFO | VM | Network-bound — DPDK may help | High PPS with small packets on network-bound VM |
| 67 | INFO | VM | Sustained minimal disk I/O | Consider lower-cost storage tier |
| 68 | INFO | VM | Sustained random high IOPS | IOPS-optimized storage recommended |
| 69 | INFO | VM | Sustained sequential high throughput | Throughput-optimized storage recommended |
| 74 | WARNING | Node | Near pod scheduling limit | Limited headroom for additional pods — review before consolidation |
| 75 | INFO | Node (reserved) | Autoscaler at minReplicas | *Not emitted today* |
| 76 | INFO | Node | Fleet consolidation (MachineSet) | Review `node_count_reduction` and scale down MachineSet manually |
| 77 | INFO | Container, Namespace, Node, PVC | Sparse data (limited observation days) | Treat as early signal; accuracy improves with more days |
| 79 | WARNING | Node (detail `business_hours` or robne BH sibling) | Business-hours node sizing is not peak-safe | Do not treat BH cores/GiB as 24/7 capacity; overnight spikes are excluded |
| 80 | WARNING | GPU (container detail `gpu.{term}.business_hours` or robne BH sibling) | Business-hours GPU sizing uses the namespace office window | Overnight training and off-hours bursts are excluded; do not merge into list badges |
| 81 | WARNING | GPU (timeslicing detail `business_hours` or robne BH sibling) | Business-hours GPU time-slicing uses the cluster office window | Overnight training and off-hours bursts are excluded; do not merge into list badges |
| 82 | WARNING | VM (detail `business_hours` or robne BH sibling) | Business-hours VM sizing uses the namespace office window | Overnight batch and off-hours bursts are excluded; thin nest (vCPU/GiB only); do not merge into the parent VM notifications array |

---

## By feature

### Containers and idle detection

Codes **1–9**, **21–22** (reserved), **25**, **77**. See [Container recommendations](../features/container-recommendations.md)
and [Idle / zombie detection](../features/idle-detection.md).

- Prefer `idle_state` (`active` / `idle` / `zombie`) over inferring from codes **5** and **8** alone.
- Code **2** (stale) means the cluster is not sending fresh ROS metrics — not the same as idle.
- Code **3** (OOM) should drive memory increases before cost-only downsizing.

### Namespaces

Codes **1**, **2**, **7**, **9**, **77** (namespace sizing plugin). See [Namespace recommendations](../features/namespace-recommendations.md).

| Code | Name | Message |
|------|------|---------|
| 1 | `LOW_CONFIDENCE` | Less than 4 days of data available for this workload |
| 2 | `STALE_DATA` | No new metrics data received for more than 48 hours |
| 7 | `NEW_WORKLOAD` | Less than 24 hours of data — recommendation may be unstable |
| 9 | `MEMORY_TRENDING_UP` | Memory usage trend suggests capacity risk within 30 days |
| 77 | `SPARSE_DATA` | Recommendation based on limited data; accuracy improves with more observation time |

Namespace-level memory trend uses a higher slope threshold than containers (500 KiB/day vs 100 KiB/day).
ResourceQuota codes **70–72** are documented under the [quota plugin](../plugin-reference/quota.md), not here.

### Nodes

Emitted: **1**, **11**, **12**, **13**, **15**, **25**, **74**, **76**, **77**, **79** (79 on nested node-detail `business_hours` and on robne `business_hours_node_recommendations` rows). Reserved (not emitted today): **4**, **14**, **16**, **17**, **23**, **24**, **75**. See [Node consolidation](../features/node-recommendations.md).

### GPU (containers) and time-slicing

Codes **10**, **26–28**, **36**, **80**, **81** (80 on nested container-detail `gpu.{term}.business_hours` and robne `business_hours_gpu_recommendations`; 81 on nested timeslicing-detail `business_hours` and robne `business_hours_gpu_timeslicing_recommendations`). See [GPU MIG](../features/gpu-mig.md) and [GPU time-slicing](../features/gpu-time-slicing.md). Timeslicing **list** stays all-hours.

### PVCs

Codes **1**, **20**, **25**, **29**, **30**, **77**. Code **1** appears when `confidence_level` is below the low-confidence threshold (proportional to `data_days / window_days`). Code **77** appears when absolute `data_days` is at or below `sparse_data_threshold` (default 2). See [PVC plugin reference](../plugin-reference/pvc.md).
Always show `resize_note` for oversized PVCs — Kubernetes cannot shrink volumes in place.

### Snapshots

Codes **31–35**. See [Snapshot staleness](../features/snapshot-staleness.md).

### Virtual machines

Codes **18–19**, **37–69**, **82** (82 on nested VM-detail `business_hours` and robne `business_hours_vm_recommendations`). Full VM notification table also appears in
[Virtual machine recommendations](../features/virtual-machines.md#placement-and-numa).
Abandoned VMs use **43** only (not **18**). Power-off scheduling: **64** (`is_power_off_candidate`,
`power_off_idle_pct`) when mostly idle with occasional activity — not abandoned. Network QoS hints:
**65** (SR-IOV), **66** (DPDK) when `is_network_bound` — see [Network QoS hints](../features/virtual-machines.md#network-qos-hints-6566).
Storage tiering hints: **67**–**69** from multi-day disk I/O — see [Storage tiering hints](../features/virtual-machines.md#storage-tiering-hints-6769).
Placement flags:
`is_redundant_placement` (**60**), `has_shared_storage` (**62**), `numa_oversized` (**63**).

### Quotas

**ResourceQuota** and **ClusterResourceQuota** plugins do not emit notification codes today.

---

### SPARSE_DATA vs LOW_CONFIDENCE

These two notifications answer different questions and are orthogonal:

| | LOW_CONFIDENCE (code 1) | SPARSE_DATA (code 77) |
|---|---|---|
| Question | "How reliable is this recommendation *relative to the term's window*?" | "Is there enough *absolute* data to trust any recommendation?" |
| Trigger | `confidence_level < 0.5` (data covers less than half the term window) | `data_days <= sparse_data_threshold` (default 2) |
| Example: fires | Medium term (7-day window), 2 days of data → confidence = 0.29 | Any term, 1 day of data |
| Example: does NOT fire | Short term (1-day window), 1 day of data → confidence = 1.0 | Any term, 3+ days of data |
| Severity | WARNING | INFO |

A recommendation can be:

- **Neither**: 10 days of data in a 15-day window (plenty of data, good coverage)
- **SPARSE_DATA only**: 1 day in a 1-day window (full coverage but objectively sparse)
- **LOW_CONFIDENCE only**: 3 days in a 15-day window (not sparse, but poor coverage)
- **Both**: 1 day in a 7-day window (sparse AND low coverage)

Configurable via `sparse_data_threshold` on container and namespace Settings API; PVC and node use the same default (2 days).

---

## UI integration

For badge mapping, fleet tiles, and filter hints, see [UI integration guide](../ui-integration-guide.md)
(Section 16 — notification codes). After backend changes, flush API cache and hard-refresh the browser.

---

## Catalog endpoint

`GET /api/cost-management/v1/recommendations/openshift/notification-codes` (when enabled) returns
rows from `notification_code_definitions`. Until then, use this page or query the database table
for machine-readable `name`, `severity`, and `description` fields.
