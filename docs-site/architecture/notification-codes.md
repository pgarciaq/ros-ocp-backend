# Notification codes

Every ROS recommendation can include **notification codes**: small integers that explain
*why* a row looks the way it does (low confidence, idle workload, orphaned PVC, and so on).
Use this page as a single lookup for **all 59 codes** across containers, namespaces, nodes,
GPUs, PVCs, snapshots, and virtual machines.

**Maintainer reference** (emitters, constants, migrations): `docs/architecture/notification-codes.md` in the repository.

## How notifications appear in the API

| Resource | List / detail fields | Notes |
|----------|----------------------|-------|
| Containers, namespaces, nodes, PVCs, snapshots | `notification_codes` (array) and `notifications` (map keyed by code string) | Map entries include `type`, `message`, `code` (Kruize-compatible shape) |
| Virtual machines | `notifications` (JSON array) | `type` is lowercase: `info`, `warning`, `critical` |

Example (container):

```json
"notification_codes": [1, 3],
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
| 2 | WARNING | Container | Stale metrics (cluster stopped reporting) | Fix operator upload; verify cluster connectivity |
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
| 15 | INFO | Node (reserved) | Autoscaler at minReplicas | *Not emitted today* |
| 16 | WARNING | Node (reserved) | Autoscaler flapping | *Not emitted today* |
| 17 | INFO | Node (reserved) | No autoscaler on variable load | *Not emitted today* |
| 18 | WARNING | VM | VM idle | Power off, delete, or resize down |
| 19 | WARNING | VM | VM oversized | Reduce vCPU/memory (may need restart) |
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

---

## By feature

### Containers and idle detection

Codes **1–9**, **21–22** (reserved), **25**. See [Container recommendations](../features/container-recommendations.md)
and [Idle / zombie detection](../features/idle-detection.md).

- Prefer `idle_state` (`active` / `idle` / `zombie`) over inferring from codes **5** and **8** alone.
- Code **2** (stale) means the cluster is not sending fresh ROS metrics — not the same as idle.
- Code **3** (OOM) should drive memory increases before cost-only downsizing.

### Namespaces

Codes **1**, **7**, **9** only. See [Namespace recommendations](../features/namespace-recommendations.md).
Namespace-level memory trend uses a higher slope threshold than containers.

### Nodes

Codes **11–13**, **25**, plus reserved **4**, **14–17**, **23–24**. See [Node consolidation](../features/node-recommendations.md).

### GPU (containers) and time-slicing

Codes **10**, **26–28**, **36**. See [GPU MIG](../features/gpu-mig.md) and [GPU time-slicing](../features/gpu-time-slicing.md).

### PVCs

Codes **20**, **25**, **29**, **30**. See [PVC right-sizing](../features/pvc-rightsizing.md).
Always show `resize_note` for oversized PVCs — Kubernetes cannot shrink volumes in place.

### Snapshots

Codes **31–35**. See [Snapshot staleness](../features/snapshot-staleness.md).

### Virtual machines

Codes **18–19**, **37–59**. Full VM notification table also appears in
[Virtual machine recommendations](../features/virtual-machines.md#notifications).
Abandoned VMs use **43** only (not **18**).

### Quotas

**ResourceQuota** and **ClusterResourceQuota** plugins do not emit notification codes today.

---

## UI integration

For badge mapping, fleet tiles, and filter hints, see [UI integration guide](../ui-integration-guide.md)
(Section 16 — notification codes). After backend changes, flush API cache and hard-refresh the browser.

---

## Catalog endpoint

`GET /api/cost-management/v1/recommendations/openshift/notification-codes` (when enabled) returns
rows from `notification_code_definitions`. Until then, use this page or query the database table
for machine-readable `name`, `severity`, and `description` fields.
