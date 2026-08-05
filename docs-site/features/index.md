# Features Overview

> **Last verified:** 2026-08-05

ROS-OCP Backend provides intelligent resource optimization recommendations for
OpenShift clusters. It analyzes historical usage from the koku-metrics-operator,
computes right-sizing and consolidation guidance, and optionally estimates monthly
dollar impact using Koku cost model rates.

## Feature matrix

| Feature | Plugins | Engines | Savings | Configurable |
|---------|---------|---------|---------|--------------|
| Container Right-Sizing | container | cost, performance | Yes | Yes |
| Usage Percentile-Band Plots | container (shipped); vm, namespace, node, jvm (planned) | — | No | No (detail-only; `p50`/`p95`/`p99`/`max` from digests) |
| Namespace Quota Optimization | namespace | cost, performance | No (by design; use container-level savings) | Yes |
| Node Consolidation | node | cost, performance | Yes | Yes |
| MachineSet Aggregation (Tier 1) | node | cost, performance | Yes (aggregated fleet savings) | Yes |
| GPU MIG Profiling | gpu | single | Yes (container detail) | Yes |
| GPU Time-Slicing | gpu | single | Yes | Yes |
| PVC Right-Sizing | pvc | single | Yes | Yes |
| ResourceQuota Right-Sizing | quota | single | Yes (tighten) | Yes |
| ClusterResourceQuota Right-Sizing | cluster-quota | single | Yes (tighten) | Yes |
| Snapshot Lifecycle | snapshot | single | Yes (cost model rates; single engine) | Yes |
| Business Hours | container, namespace | cost, performance | No (sizing only; savings use all_hours) | Yes |
| Configurable Thresholds | all | container, namespace, node (dual); gpu, pvc (single); quota, snapshot, vm (separate settings) | N/A | Yes |
| Savings Estimations | container, node, pvc, snapshot, vm; quota (tighten), cluster-quota (tighten); GPU MIG/time-slicing/idle on container detail (excluded from fleet summary) | cost, performance (container, node, vm only); N/A for pvc, snapshot, quota, GPU | Core feature | N/A |
| Idle / Zombie Detection | container, GPU, namespace, node (PVC: orphaned only) | single | Yes: container & GPU (full waste), PVC (orphaned cost); No: namespace, node (uses consolidation savings) | Yes |
| Virtual Machine Recommendations | vm | cost, performance | Complete | Yes |

## All feature pages

| Page | Topic |
|------|-------|
| [container-recommendations.md](container-recommendations.md) | Container CPU/memory right-sizing |
| [namespace-recommendations.md](namespace-recommendations.md) | Namespace quota optimization |
| [quota-recommendations.md](quota-recommendations.md) | ResourceQuota right-sizing |
| [cluster-resource-quota.md](cluster-resource-quota.md) | ClusterResourceQuota right-sizing |
| [node-recommendations.md](node-recommendations.md) | Node consolidation |
| [../plugin-reference/node.md](../plugin-reference/node.md#machineset-aggregation-api-get-machinesets) | MachineSet fleet aggregation (`GET .../machinesets`, Tier 1 shipped) |
| [gpu-mig.md](gpu-mig.md) | GPU MIG profiling |
| [gpu-time-slicing.md](gpu-time-slicing.md) | GPU time-slicing |
| [pvc-rightsizing.md](pvc-rightsizing.md) | PVC storage right-sizing |
| [snapshot-staleness.md](snapshot-staleness.md) | VolumeSnapshot lifecycle |
| [idle-detection.md](idle-detection.md) | Idle and zombie workloads |
| [business-hours.md](business-hours.md) | Business-hours weighted analysis |
| [configurable-thresholds.md](configurable-thresholds.md) | Per-tenant threshold tuning |
| [tag-filtering.md](tag-filtering.md) | Label-based filtering |
| [percentile-band-plots.md](percentile-band-plots.md) | Usage percentile-band plots (replaced boxplots) |
| [visual-insights.md](visual-insights.md) | Charts, gauges, and heatmaps on recommendation detail pages (**shipped**; list sparklines follow-up) |
| [dual-engine.md](dual-engine.md) | Cost vs performance engines |
| [savings-estimations.md](savings-estimations.md) | Dollar savings estimates |
| [history-and-quality.md](history-and-quality.md) | History and quality metrics |
| [virtual-machines.md](virtual-machines.md) | OpenShift Virtualization VM right-sizing |

## Capabilities

**[Container Right-Sizing](container-recommendations.md)** — The core feature.
Analyzes per-container CPU and memory usage to recommend Kubernetes requests and
limits, with idle/abandoned detection and OOM-aware memory bumps.

**[Namespace Quota Optimization](namespace-recommendations.md)** — Aggregates
container recommendations into namespace-level ResourceQuota guidance with growth
buffers and memory trend alerts.

**[Node Consolidation](node-recommendations.md)** — Identifies underutilized,
overcommitted, and stranded-resource nodes; recommends consolidation and target
node sizing with dual cost/performance engines. **`GET .../machinesets`** (Tier 1 shipped)
aggregates node recommendations by MachineSet for fleet-level savings.

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

**[Virtual Machine Recommendations](virtual-machines.md)** — Right-size
KubeVirt guests: whole vCPU and GiB recommendations, instance type matching (u1, cx1,
m1, gn1 when GPU metrics exist), idle and abandoned VM detection, disk growth projection,
I/O profiling, crash-loop detection, GPU passthrough/vGPU/MIG on guests, graduated
confidence with guest-agent adaptivity. Enabled by default; disable with
`ROS_DISABLED_PLUGINS=vm`.
Technical design: [`docs/design/vm-recommendations.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/vm-recommendations.md).

**[Visual Insights](visual-insights.md)** — Charts, gauges, and heatmaps on recommendation
detail pages (Tier 1–3). Master toggle `ROS_VISUAL_INSIGHTS_ENABLED` (default on). List-view
sparklines remain an optional follow-up (`ROS_SPARKLINES_ENABLED=false` by default).

## Planned capabilities

**Phase 14 (in progress):** recommendation explanation factors (`?include=explanation`
on detail APIs) and GPU time-slicing persistence at ingest. See
[What's New — Phase 14](../whats-new.md#in-progress-phase-14).

Other upcoming features (MachineSet Tier 2, seasonality, JVM, HPA/VPA, network, local mode)
are documented separately and are **not yet available**:

**[Features (planned)](../planned-features/index.md)** — product direction, API sketches,
and integration notes for future releases.

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
