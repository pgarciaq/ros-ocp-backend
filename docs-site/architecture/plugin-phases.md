# Plugin Execution Phases

> **Last verified:** 2026-08-05

## Overview

Plugins execute in ordered phases. All plugins in a phase complete before the next phase begins.

Implementation: [`internal/plugin/phases.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go), [`ExecuteInPhases`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go), and phase-sorted [`Enabled()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/registry.go).

## Phase constants

| Value | Constant | Name | Purpose |
|-------|----------|------|---------|
| 1 | `PhaseProduce` | Produce | Generate recommendations from raw metrics and digests |
| 2 | `PhaseEnrich` | Enrich | Annotate, classify, or enhance Phase 1 outputs |
| 3 | `PhaseOptimize` | Optimize | Cross-entity aggregation requiring a global fleet view |

Embed [`BasePlugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go) for Phase 1 and priority `50` unless a plugin needs a different phase or run order.

## Phase 1: Produce

Generate recommendations from raw metrics and digests. These plugins are independent
and can theoretically run in parallel.

| Plugin | Description |
|--------|-------------|
| container | CPU/memory rightsizing and inline idle/zombie classification ([`ClassifyIdleState`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/idle_classification.go)) |
| namespace | Namespace-level resource recommendations; post-processes namespace idle when all containers are idle/zombie |
| node | Node CPU/memory sizing recommendations |
| gpu | GPU time-slicing and MIG recommendations |
| pvc | PVC storage rightsizing |
| quota | ResourceQuota right-sizing vs configured hard/used limits and container rec aggregates ([quota-recommendations.md](../features/quota-recommendations.md)) |
| cluster-quota | OpenShift ClusterResourceQuota right-sizing from `openshift_clusterresourcequota_*` metrics ([cluster-resource-quota.md](../features/cluster-resource-quota.md)) |
| snapshot | VolumeSnapshot staleness and recoverable cost |
| vm | OpenShift Virtualization VM rightsizing (CPU, memory, disk, I/O, instance types, idle/abandoned, guest GPU) |
| kruize | Legacy Kruize engine (mutually exclusive with native plugins) |
| instance-type (future) | Cloud instance type optimization for worker nodes |

## Phase 2: Enrich

Read Phase 1 outputs and annotate, classify, or enhance them. These plugins depend
on Phase 1 being complete but are independent of each other.

| Plugin | Depends on | Description |
|--------|------------|-------------|
| java (future) | container (memory rec) | JVM heap/GC recommendations |
| golang (future) | container (CPU+memory rec) | GOMAXPROCS, GOMEMLIMIT tuning |
| python (future) | container (CPU rec) | Worker process count (gunicorn/uvicorn) |
| nodejs (future) | container (memory rec) | --max-old-space-size, cluster workers |
| hpa (future) | container (recs + patterns) | HPA min/max/target autoscaler config |
| vpa (future) | container | VPA policy recommendations |

## Phase 3: Optimize

Cross-entity aggregation requiring a global view of all recommendations.

| Plugin | Depends on | Description |
|--------|-------------|------------|
| binpacking (future) | container, node | Optimal pod placement to reduce fragmentation |
| machineset (future) | binpacking, instance-type | Fleet-level node pool right-sizing |

## Execution model

```
Phase 1: [container → gpu → node → pvc → quota → cluster-quota → snapshot → vm → namespace]  (by Priority, then Name)
         ↓ barrier
Phase 2: [java, golang, hpa, vpa, ...]  (future)
         ↓ barrier
Phase 3: [binpacking, machineset]  (future)
```

[`Enabled()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/registry.go) and [`ByTrait`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/registry.go) return plugins in this order. [`ExecuteInPhases`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go) invokes a callback per plugin with barriers between phases. Retention sweeps and ingest-hook dispatch use this ordering today; recommendation pipelines will adopt the same model as Phase 2/3 plugins land.

## Sorting rules

When the registry builds the enabled plugin list (or runs [`ExecuteInPhases`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go)), it sorts with [`sortPluginsByPhase`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go):

1. **`Phase()` ascending** — all Phase 1 plugins finish before any Phase 2 plugin starts.
2. **`Priority()` ascending** — within the same phase, lower priority runs first.
3. **`Name()` ascending** — when phase and priority match, alphabetical order (deterministic but arbitrary).

`ROS_ENABLED_PLUGINS` list order does **not** affect execution order. Registration order (`init()` side-effect import order) also does **not** affect execution order.

Invalid phase values (`< 1` or `> 3`) are treated as Phase 1 (Produce).

## Priority

Each plugin implements [`Priority() int`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/plugin.go). **Lower values run first within the same phase.** Embed [`BasePlugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go) for the default priority `50`.

Choose priority when one plugin must read or update rows another plugin writes in the same phase (for example namespace idle aggregation after container and GPU rows exist).

### Current plugins (Phase and Priority)

All production plugins today use Phase 1 via embedded [`BasePlugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/phases.go) (none override `Phase()`). The `kruize` plugin is never enabled alongside native plugins.

| Plugin | Phase | Priority | Rationale |
|--------|-------|----------|-----------|
| container | 1 | 10 | Writes `idle_state` on `recommendation_sets`; downstream plugins depend on container rows |
| kruize | 1 | 10 | Legacy container path (mutually exclusive with native plugins) |
| gpu | 1 | 20 | Ingest hooks and recommendations after container rows exist; sets `gpu_idle_state` |
| node | 1 | 30 | Independent; ingest hook after container CSV |
| pvc | 1 | 30 | Independent; owns storage CSV ingest |
| quota | 1 | 35 | After PVC ingest; compares quota hard/used limits to namespace usage and container rec aggregates |
| cluster-quota | 1 | 36 | After namespace `quota`; ingests CRQ CSV; compares CRQ hard/used to namespace quota rec aggregates |
| snapshot | 1 | 40 | VolumeSnapshot staleness after core recommendations exist |
| vm | 1 | 40 | VM digests and recommendations; same priority as snapshot (name tie-break: snapshot before vm) |
| example (`_example`) | 1 | 50 | Template default (`BasePlugin`); always disabled in production |
| namespace | 1 | 90 | Aggregates namespace idle after container/GPU (and related) rows exist |

When `node` and `pvc` are both enabled (priority 30), **`node` runs before `pvc`** because names tie-break alphabetically.

### Example 1: Phase beats Priority

Suppose two plugins are enabled:

| Plugin | Phase | Priority |
|--------|-------|----------|
| early-producer | 1 | 50 |
| enricher | 2 | 20 |

**`early-producer` runs before `enricher`.** Phase 1 completes entirely before Phase 2 starts, even though `enricher` has a lower priority number (20 &lt; 50). Priority only orders plugins **within** the same phase.

### Example 2: Priority within Phase 1

Suppose two Phase 1 plugins are enabled:

| Plugin | Phase | Priority |
|--------|-------|----------|
| container | 1 | 10 |
| namespace | 1 | 90 |

**`container` runs before `namespace`.** Both are Phase 1; priority 10 sorts before 90. This ensures container (and GPU hook) work finishes before namespace idle aggregation.

### Example 3: Same phase and priority — sort by Name

Suppose two Phase 1 plugins share priority 30:

| Plugin | Phase | Priority |
|--------|-------|----------|
| pvc | 1 | 30 |
| node | 1 | 30 |

**`node` runs before `pvc`.** Phase and priority are equal, so the registry sorts by `Name()` ascending (`"node"` &lt; `"pvc"`). The order is stable across runs but is a naming convention, not a semantic dependency—if ordering matters, assign distinct priorities.

## Adding a New Plugin

1. Create package under `internal/plugins/<name>/`
2. Implement the [`Plugin`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugin/plugin.go) interface: `Name()`, `Enabled()`, and optionally override `Phase()` / `Priority()`
3. Register with `plugin.Register(&MyPlugin{})` in `init()`
4. Add a blank import to [`internal/plugins/plugins.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/plugins.go)
5. **Phase 1:** embed `plugin.BasePlugin` (default phase and priority 50)
6. **Phase 2:** implement `Phase() int { return plugin.PhaseEnrich }` and set `Priority()` relative to other Enrich plugins
7. **Phase 3:** implement `Phase() int { return plugin.PhaseOptimize }`

Per-resource idle detection is **not** a separate plugin: Phase 1 `container` calls
[`ClassifyIdleState`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/idle_classification.go) inline; Phase 1
`namespace` runs [`AggregateNamespaceIdleState`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/idle_classification.go)
after writing namespace rows (`container` priority 10, `namespace` priority 90).

Users enable via `ROS_ENABLED_PLUGINS=container,namespace,...`.
Order in the env var does NOT matter — the registry sorts by phase, priority, and name automatically.

## Future plugins (summary)

Plugins marked **(future)** above are not registered in production builds today. They are
documented here so phase and priority slots stay stable when implementations land.

| Plugin | Phase | Priority (planned) | Domain |
|--------|-------|-------------------|--------|
| instance-type | 1 | (TBD) | Cloud instance type optimization for worker nodes |
| java | 2 | (TBD) | JVM heap / GC |
| golang | 2 | (TBD) | GOMAXPROCS, GOMEMLIMIT |
| python | 2 | (TBD) | Gunicorn/uWSGI workers |
| nodejs | 2 | (TBD) | Node.js heap / cluster workers |
| hpa | 2 | (TBD) | Horizontal Pod Autoscaler min/max/target |
| vpa | 2 | (TBD) | Vertical Pod Autoscaler policy |
| binpacking | 3 | (TBD) | Fleet pod placement |
| machineset | 3 | (TBD) | Node pool right-sizing |

See [performance-analysis.md §23](../archive/performance-analysis.md#23-additional-recommendation-types-industry-gap-analysis)
for industry gap context on HPA, runtime tuning, and ephemeral storage. ResourceQuota and ClusterResourceQuota recommendations are implemented — see
[quota-recommendations.md](../features/quota-recommendations.md) and
[cluster-resource-quota.md](../features/cluster-resource-quota.md).
