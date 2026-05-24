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
| Snapshot Lifecycle | snapshot | single | Yes (cost) | Yes |
| Business Hours | container, namespace | cost, performance | Yes | Yes |
| Configurable Thresholds | all | all | N/A | Yes |
| Savings Estimations | container, node, pvc, snapshot | cost, performance | Core feature | N/A |

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
