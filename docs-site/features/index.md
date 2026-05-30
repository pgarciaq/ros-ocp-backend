# Features Overview

ROS-OCP Backend provides intelligent resource optimization recommendations for
OpenShift clusters. It analyzes historical usage from the koku-metrics-operator,
computes right-sizing and consolidation guidance, and optionally estimates monthly
dollar impact using Koku cost model rates.

## Feature matrix

| Feature | Plugins | Engines | Savings | Configurable |
|---------|---------|---------|---------|--------------|
| Container Right-Sizing | container | cost, performance | Yes | Yes |
| Namespace Quota Optimization | namespace | cost, performance | Planned | Yes |
| Node Consolidation | node | cost, performance | Yes | Yes |
| GPU MIG Profiling | gpu | single | No | Yes |
| GPU Time-Slicing | gpu | single | Yes | Yes |
| PVC Right-Sizing | pvc | single | Yes | Yes |
| ResourceQuota Right-Sizing | quota | single | Yes (tighten) | Yes |
| ClusterResourceQuota Right-Sizing | cluster-quota | single | Yes (tighten) | Yes |
| Snapshot Lifecycle | snapshot | single | Yes (cost) | Yes |
| Business Hours | container, namespace | cost, performance | Yes | Yes |
| Configurable Thresholds | all | all | N/A | Yes |
| Savings Estimations | container, node, pvc, snapshot | cost, performance | Core feature | N/A |
| Idle / Zombie Detection | container (GPU, PVC, node planned) | single | Yes (full waste) | Yes |
| Seasonality & Proactive Recs | seasonality-* (planned) | cost, performance | Planned | Planned |
| Virtual Machine Recommendations | vm (planned) | cost, performance | Planned | Planned |
| Java / JVM Optimization | java (planned) | cost, performance | Planned | Planned |

## All feature pages

| Page | Topic |
|------|-------|
| [container-recommendations.md](container-recommendations.md) | Container CPU/memory right-sizing |
| [namespace-recommendations.md](namespace-recommendations.md) | Namespace quota optimization |
| [quota-recommendations.md](quota-recommendations.md) | ResourceQuota right-sizing |
| [cluster-resource-quota.md](cluster-resource-quota.md) | ClusterResourceQuota right-sizing |
| [node-recommendations.md](node-recommendations.md) | Node consolidation |
| [gpu-mig.md](gpu-mig.md) | GPU MIG profiling |
| [gpu-time-slicing.md](gpu-time-slicing.md) | GPU time-slicing |
| [pvc-rightsizing.md](pvc-rightsizing.md) | PVC storage right-sizing |
| [snapshot-staleness.md](snapshot-staleness.md) | VolumeSnapshot lifecycle |
| [idle-detection.md](idle-detection.md) | Idle and zombie workloads |
| [business-hours.md](business-hours.md) | Business-hours weighted analysis |
| [configurable-thresholds.md](configurable-thresholds.md) | Per-tenant threshold tuning |
| [tag-filtering.md](tag-filtering.md) | Label-based filtering |
| [dual-engine.md](dual-engine.md) | Cost vs performance engines |
| [savings-estimations.md](savings-estimations.md) | Dollar savings estimates |
| [history-and-quality.md](history-and-quality.md) | History and quality metrics |
| [seasonality.md](seasonality.md) | Seasonality & proactive recommendations (**planned**) |
| [virtual-machines.md](virtual-machines.md) | OpenShift Virtualization VM right-sizing (**planned**) |
| [java-jvm.md](java-jvm.md) | Java/JVM heap, GC, and thread pool tuning (**planned**) |

## Capabilities

**[Container Right-Sizing](container-recommendations.md)** — The core feature.
Analyzes per-container CPU and memory usage to recommend Kubernetes requests and
limits, with idle/abandoned detection and OOM-aware memory bumps.

**[Namespace Quota Optimization](namespace-recommendations.md)** — Aggregates
container recommendations into namespace-level ResourceQuota guidance with growth
buffers and memory trend alerts.

**[Node Consolidation](node-recommendations.md)** — Identifies underutilized,
overcommitted, and stranded-resource nodes; recommends consolidation and target
node sizing with dual cost/performance engines.

**[GPU MIG Profiling](gpu-mig.md)** — Maps GPU utilization patterns to NVIDIA
MIG profiles (1g.5gb through 7g.40gb) for hardware-isolated sharing.

**[GPU Time-Slicing](gpu-time-slicing.md)** — Node-level software GPU sharing
recommendations for non-MIG GPUs when workloads are underutilized.

**[PVC Right-Sizing](pvc-rightsizing.md)** — Classifies PVCs as oversized,
near-full, orphaned, or healthy; projects growth trends and estimates storage savings.

**[ResourceQuota Recommendations](quota-recommendations.md)** — Compares namespace
ResourceQuota hard and used limits against container recommendation sums; advises
tighten, raise, or optimal with risk levels and optional dollar savings on tighten.

**[ClusterResourceQuota Recommendations](cluster-resource-quota.md)** — OpenShift
team/tenant quotas across namespace selectors; compares CRQ hard/used to aggregated
namespace quota recommendations with the same classification model.

**[Snapshot Staleness](snapshot-staleness.md)** — Flags orphaned, stale,
redundant, and never-restored VolumeSnapshots; estimates recoverable monthly cost.

**[Business Hours](business-hours.md)** — Computes separate all-hours and
business-hours recommendation streams when clusters run interactive workloads on
a schedule.

**[Configurable Thresholds](configurable-thresholds.md)** — Per-tenant tuning of
sizing and classification parameters via the Settings API, with admin env-var locks.

**[Dual Engine (Cost vs Performance)](dual-engine.md)** — Two recommendation
perspectives for the same workload: cost-minimizing vs headroom-maximizing.

**[Savings Estimations](savings-estimations.md)** — Dollar estimates from Koku
`effective_rates`; fleet summaries and per-recommendation fields.

**[History & Quality](history-and-quality.md)** — Time-series of past
recommendations and quality metrics (stability, adoption, OOM events).

**[Tag Filtering](tag-filtering.md)** — Filter container recommendations by
OpenShift labels synced from Koku (`filter[tag:key]=value`).

**[Idle / Zombie Detection](idle-detection.md)** — Classify workloads with little or
no usage (zombie vs idle), estimate full monthly waste, and filter with
`filter[idle_state]=zombie,idle` — distinct from rightsizing savings.

**[Seasonality & Proactive Recommendations](seasonality.md)** — *Planned.* Detect
recurring peaks in daily usage history, forecast upcoming demand with
[Augurs](https://github.com/grafana/augurs), and warn operators days before
predictable spikes (month-end batch, weekly surges, holiday traffic). See the
internal design: [`docs/design/seasonality-plugin.md`](../../../docs/design/seasonality-plugin.md).

**[Virtual Machine Recommendations](virtual-machines.md)** — *Planned.* Right-size
KubeVirt guests: whole vCPU and GiB recommendations, instance type mapping (cx1, m1,
u1, …), idle VM detection, disk growth and I/O hints. Gated behind `ROS_ENABLE_VM_RECS`.
Technical design: [`docs/design/vm-recommendations.md`](../../../docs/design/vm-recommendations.md).

**[Java & JVM Optimization](java-jvm.md)** — *Planned.* JVM-specific tuning on top of container
recommendations: MaxRAMPercentage, garbage collector selection, Quarkus/Spring thread pools,
and container limits that account for non-heap memory (metaspace, thread stacks). Addresses
OOMKills where the heap was not full. Gated behind `ROS_ENABLE_JVM_RECS`.
Technical design: [`docs/design/java-recommendations.md`](../../../docs/design/java-recommendations.md).

## Planned recommendation types

Not yet implemented; phase and priority slots are reserved in
[Plugin Execution Phases](../architecture/plugin-phases.md).

| Type | Phase | Description |
|------|-------|-------------|
| Seasonality (detector / forecast / proactive) | 1–3 | Learned periodicity → proactive quota/node/container guidance |
| VM (OpenShift Virtualization) | 1 | vCPU, memory, disk rightsizing for KubeVirt guests |
| Instance type | 1 | Cloud instance optimization for worker nodes |
| Java / JVM | 2 | Heap, GC, thread pool tuning — see [java-jvm.md](java-jvm.md) |
| Go runtime | 2 | GOMAXPROCS, GOMEMLIMIT |
| Python / Node.js | 2 | Worker and heap advisories |
| HPA / VPA | 2 | Autoscaler min/max/target and policy recommendations |
| Binpacking / MachineSet | 3 | Fleet placement and node pool sizing |

!!! tip "Getting Started"
    Integrate with the REST API and UI using the
    **[Frontend Integration Guide](../ui-integration-guide.md)**. It covers
    authentication, list/detail response shapes, pagination, and settings endpoints.

## Related documentation

| Document | Scope |
|----------|-------|
| [Recommendation Engines](../architecture/recommendation-engines.md) | Thresholds, percentiles, term windows |
| [Configurability Reference](../architecture/configurability.md) | All `ROS_*` env vars and precedence |
| [Cost Integration](../architecture/cost-integration.md) | Savings formulas and fleet summary |
