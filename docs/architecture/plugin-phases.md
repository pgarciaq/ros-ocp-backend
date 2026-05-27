# Plugin Execution Phases

## Overview

Plugins execute in ordered phases. All plugins in a phase complete before the next phase begins.

Implementation: [`internal/plugin/phases.go`](../../internal/plugin/phases.go), [`ExecuteInPhases`](../../internal/plugin/phases.go), and phase-sorted [`Enabled()`](../../internal/plugin/registry.go).

## Phase 1: Produce

Generate recommendations from raw metrics and digests. These plugins are independent
and can theoretically run in parallel.

| Plugin | Description |
|--------|-------------|
| container | CPU/memory rightsizing from container usage digests |
| namespace | Namespace-level resource recommendations |
| node | Node CPU/memory sizing recommendations |
| gpu | GPU time-slicing and MIG recommendations |
| pvc | PVC storage rightsizing |
| snapshot | Staleness detection for recommendation freshness |
| vm (future) | OpenShift Virtualization VM rightsizing |
| instance-type (future) | Cloud instance type optimization for nodes |

## Phase 2: Enrich

Read Phase 1 outputs and annotate, classify, or enhance them. These plugins depend
on Phase 1 being complete but are independent of each other.

| Plugin | Description | Depends on |
|--------|-------------|------------|
| idledetection (future) | Classify containers as idle/zombie | container |
| java (future) | JVM heap/GC recommendations | container (memory rec) |
| golang (future) | GOMAXPROCS, GOMEMLIMIT tuning | container (CPU+memory rec) |
| python (future) | Worker process count (gunicorn/uvicorn) | container (CPU rec) |
| nodejs (future) | --max-old-space-size, cluster workers | container (memory rec) |
| hpa (future) | HPA min/max/target autoscaler config | container (recs + patterns) |
| vpa (future) | VPA policy recommendations | container |

## Phase 3: Optimize

Cross-entity aggregation requiring a global view of all recommendations.

| Plugin | Description | Depends on |
|--------|-------------|------------|
| binpacking (future) | Optimal pod placement to reduce fragmentation | container, node |
| machineset (future) | Fleet-level node pool right-sizing | binpacking, instance-type |

## Execution Model

```
Phase 1: [container, namespace, node, gpu, pvc, snapshot] → all complete
         ↓ barrier
Phase 2: [idledetection, java, golang, ...] → all complete
         ↓ barrier
Phase 3: [binpacking, machineset] → all complete
```

[`Enabled()`](../../internal/plugin/registry.go) and [`ByTrait`](../../internal/plugin/registry.go) return plugins in phase order. [`ExecuteInPhases`](../../internal/plugin/phases.go) invokes a callback per plugin with barriers between phases. Retention sweeps and ingest-hook dispatch use this ordering today; recommendation pipelines will adopt the same model as Phase 2/3 plugins land.

## Adding a New Plugin

1. Create package under `internal/plugins/<name>/`
2. Implement the Plugin interface including `Phase() int`
3. Register with `plugin.Register("<name>", &MyPlugin{})`
4. Phase 1: embed `plugin.BasePlugin` (default)
5. Phase 2: return `plugin.PhaseEnrich` from `Phase()`
6. Phase 3: return `plugin.PhaseOptimize` from `Phase()`

Users enable via `ROS_ENABLED_PLUGINS=container,namespace,...,idledetection`.
Order in the env var does NOT matter — the registry sorts by phase automatically.
