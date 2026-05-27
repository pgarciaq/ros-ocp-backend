# Plugin Execution Phases

## Overview

Plugins execute in ordered phases. All plugins in a phase complete before the next phase begins.

Implementation: [`internal/plugin/phases.go`](../../internal/plugin/phases.go), [`ExecuteInPhases`](../../internal/plugin/phases.go), and phase-sorted [`Enabled()`](../../internal/plugin/registry.go).

## Phase 1: Produce

Generate recommendations from raw metrics and digests. These plugins are independent
and can theoretically run in parallel.

| Plugin | Description |
|--------|-------------|
| container | CPU/memory rightsizing and inline idle/zombie classification ([`ClassifyIdleState`](../../internal/engine/idle_classification.go)) |
| namespace | Namespace-level resource recommendations; post-processes namespace idle when all containers are idle/zombie |
| node | Node CPU/memory sizing recommendations |
| gpu | GPU time-slicing and MIG recommendations |
| pvc | PVC storage rightsizing |
| snapshot | Staleness detection for recommendation freshness |
| vm (future) | OpenShift Virtualization VM rightsizing |
| instance-type (future) | Cloud instance type optimization for nodes |

## Phase 2: Enrich

Read Phase 1 outputs and annotate, classify, or enhance them. These plugins depend
on Phase 1 being complete but are independent of each other.

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
Phase 1: [container→gpu→node/pvc→snapshot→namespace] → all complete (by Priority)
         ↓ barrier
Phase 2: [java, golang, hpa, vpa, ...] → all complete
         ↓ barrier
Phase 3: [binpacking, machineset] → all complete
```

Within a phase, plugins run in **priority order** (lower `Priority()` first), then by
name for ties. See [Priority](#priority) below.

[`Enabled()`](../../internal/plugin/registry.go) and [`ByTrait`](../../internal/plugin/registry.go) return plugins in phase order. [`ExecuteInPhases`](../../internal/plugin/phases.go) invokes a callback per plugin with barriers between phases. Retention sweeps and ingest-hook dispatch use this ordering today; recommendation pipelines will adopt the same model as Phase 2/3 plugins land.

## Priority

Each plugin implements [`Priority() int`](../../internal/plugin/plugin.go). Lower values run first within the same phase. Embed [`BasePlugin`](../../internal/plugin/phases.go) for the default priority `50`.

| Plugin | Phase | Priority | Rationale |
|--------|-------|----------|-----------|
| container | 1 | 10 | Writes `idle_state` on `recommendation_sets`; others depend on it |
| kruize | 1 | 10 | Legacy container path (mutually exclusive with native plugins) |
| gpu | 1 | 20 | Enriches GPU metadata and `gpu_idle_state` after container rows exist |
| node | 1 | 30 | Independent |
| pvc | 1 | 30 | Independent |
| snapshot | 1 | 40 | Reads recommendation freshness |
| namespace | 1 | 90 | Aggregates namespace idle after all container/GPU rows exist |
| example | 1 | 50 | Template default |

`ROS_ENABLED_PLUGINS` list order does **not** affect execution order.

## Adding a New Plugin

1. Create package under `internal/plugins/<name>/`
2. Implement the Plugin interface including `Phase() int`
3. Register with `plugin.Register("<name>", &MyPlugin{})`
4. Phase 1: embed `plugin.BasePlugin` (default)
5. Phase 2: return `plugin.PhaseEnrich` from `Phase()`
6. Phase 3: return `plugin.PhaseOptimize` from `Phase()`

Per-resource idle detection is **not** a separate plugin: Phase 1 `container` calls
[`ClassifyIdleState`](../../internal/engine/idle_classification.go) inline; Phase 1
`namespace` runs [`AggregateNamespaceIdleState`](../../internal/engine/idle_classification.go)
after writing namespace rows (`container` priority 10, `namespace` priority 90).

Users enable via `ROS_ENABLED_PLUGINS=container,namespace,...`.
Order in the env var does NOT matter — the registry sorts by phase and priority automatically.
