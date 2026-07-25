# ADR-0323: Unified category classification across all resource types

## Status

Accepted — supersedes [ADR-0307](0307-recommendation-categories.md) for the scope of `idle`/`zombie`/`abandoned` inclusion and node classification.

## Context

ADR-0307 introduced `category` (undersized/oversized/optimized) on containers and namespaces but explicitly excluded idle and abandoned states, keeping `idle_state` as a separate orthogonal dimension. This created two problems:

1. **Two-dimensional filtering complexity.** Clients must combine `filter[idle_state]` and `filter[category]` to answer "show me all problematic workloads." For VMs, `category` already includes activity states (abandoned, idle) since #354, so the API is inconsistent across resource types.

2. **Node classification via booleans.** Nodes use `is_underutilized`, `is_overcommitted`, and `idle_state` as three independent dimensions, creating a matrix of 8 possible states. No single field answers "what should I do about this node?"

Industry survey of competitor tools confirms a single primary classification per resource is the standard:

| Tool | Model |
|------|-------|
| AWS Compute Optimizer | Single enum: Under-provisioned, Over-provisioned, Optimized, None |
| Azure Advisor | Single: Right-size or shutdown, Idle |
| GCP Recommender | Single: Idle VM, Right-size VM |
| Kubecost | Single: Right-sized, Over-provisioned, Under-provisioned, Idle |
| CAST AI | Single primary action per workload |
| Turbonomic (IBM) | Single recommended action: Resize Up, Resize Down, Suspend |
| Densify | Single: Resize, Upsize, Downsize, Terminate, Right-sized |

No major tool surfaces two independent classification dimensions simultaneously on the same resource.

## Decision

### 1. `category` is the single badge source of truth for all resource types

Every resource type exposes a `category` string field as the primary classification. The UI renders a single badge from this field. Supplementary detail fields (`idle_state`, `category_cpu`, `category_memory`, utilization percentages) remain available for drill-down views but are not the primary badge driver.

### 2. Activity states take priority over sizing states

When a resource is idle or abandoned, the sizing recommendation is secondary — the primary action is "deal with the inactive resource." Priority ordering ensures the most actionable category wins:

**Containers and namespaces:**
`zombie > idle > undersized > oversized > optimized`

**VMs (unchanged from #354):**
`abandoned > power_off_candidate > idle > oversized > undersized > optimized`

**Nodes:**
`idle > overcommitted > stranded_cpu > stranded_memory > underutilized > optimized`

### 3. Category values per resource type

| Resource | Category values | Notes |
|----------|----------------|-------|
| Container | `zombie`, `idle`, `undersized`, `oversized`, `optimized` | `zombie` ≈ VM `abandoned` |
| Namespace | `zombie`, `idle`, `undersized`, `oversized`, `optimized` | Same as container |
| VM | `abandoned`, `power_off_candidate`, `idle`, `oversized`, `undersized`, `optimized` | Established in #354 |
| Node | `idle`, `overcommitted`, `stranded_cpu`, `stranded_memory`, `underutilized`, `optimized` | Replaces booleans |

### 4. Container/namespace: remove engine-internal booleans

`ContainerRec.IsIdle` and `ContainerRec.IsAbandoned` are engine-internal fields derived from `IdleState`. They have a known inconsistency (`recommend_all.go` hardcodes `IsAbandoned = false`; `savings_recalculate.go` derives it correctly). Both are removed in favor of using `IdleState` directly via an `IsIdleOrZombie()` helper method.

### 5. Node: collapse booleans into `category`

`is_underutilized`, `is_overcommitted`, and `stranded_resource` are replaced by a single `category` column. The DB migration backfills using the priority ordering, then drops the boolean columns.

### 6. API filter unification

| Before | After |
|--------|-------|
| Container: `filter[idle_state]` + `filter[category]` | `filter[category]` (accepts all 5 values) |
| Namespace: `filter[idle_state]` + `filter[category]` | `filter[category]` (accepts all 5 values) |
| Node: `filter[is_underutilized]` + `filter[is_overcommitted]` + `filter[idle_state]` + `filter[stranded_resource]` | `filter[category]` (accepts all 6 values) |

### 7. Naming: `zombie` vs `abandoned`

Containers use `zombie` (established Kubernetes concept for long-inactive processes). VMs use `abandoned` (infrastructure term for decommission candidates). Both mean "inactive for an extended period, strong candidate for removal." The naming difference is intentional — each aligns with its domain's conventions.

## Alternatives Considered

### Keep `idle_state` and `category` as orthogonal dimensions

This is what ADR-0307 decided. Rejected because:
- Forces two-dimensional filtering on clients
- Inconsistent with the VM pattern (which already includes activity in `category`)
- No competitor uses two independent classification dimensions simultaneously

### Undo the VM unification — move VMs to the richer two-dimensional model

Rejected. The VM single-`category` model is proven and aligns with industry practice. The correct direction is to extend the pattern to other resource types, not revert it.

### Single `category` enum for all resource types (shared values)

Rejected. Each resource type has domain-specific states (VMs: `power_off_candidate`; nodes: `overcommitted`, `stranded_cpu`) that don't apply to other types. A shared enum would either be too broad (unused values) or too narrow (missing states).

### Move nodes to a richer multi-dimensional model instead of collapsing

Rejected. The industry overwhelmingly uses single primary classification. The detail (utilization percentages, overcommit ratios) remains available in the response for drill-down — it just doesn't drive the primary badge.

## Consequences

### Positive

- Unified `filter[category]` across all resource types — no more combining multiple filter parameters.
- Single badge source of truth makes UI rendering consistent (VM, container, namespace, and node all use the same pattern).
- Eliminates the `IsIdle`/`IsAbandoned` boolean inconsistency in the container engine.
- Node classification simplified from 8-state boolean matrix to 6 distinct categories.
- Aligns with industry standard (single primary classification per resource).

### Negative

- Breaking API change: `filter[idle_state]`, `filter[is_underutilized]`, `filter[is_overcommitted]` are removed. Acceptable because we are currently the only API consumer.
- Container/namespace `category` can no longer be NULL — idle/zombie states that previously had NULL category now have an explicit value. Migration backfills existing rows.
- Node `stranded_resource` detail (which resource is stranded) is encoded in the category value rather than a separate field. Clients that need the raw resource name can derive it from the category string.

### Neutral

- `idle_state`, `category_cpu`, `category_memory` remain as supplementary detail columns in the database. They are not removed — only demoted from primary badge driver.
- PVC, GPU, quota, and snapshot classifications are unchanged by this ADR. They already have domain-specific classification fields that work correctly.

## Related Decisions

- [ADR-0012](0012-three-state-idle-zombie-active.md): Three-state idle/zombie/active classification (unchanged).
- [ADR-0172](0172-dual-path-idle-classification.md): Dual-path idle classification (unchanged).
- [ADR-0177](0177-node-idle-separate-from-container.md): Node idle classification separate from container (unchanged).
- [ADR-0253](0253-pvc-four-way-classification-healthy-orphaned.md): PVC classification (unchanged).
- [ADR-0307](0307-recommendation-categories.md): Original category introduction (superseded by this ADR for idle/node scope).

## References

- [#354](https://github.com/pgarciaq/ros-ocp-backend/issues/354): VM category unification (completed)
- [#358](https://github.com/pgarciaq/ros-ocp-backend/issues/358): Umbrella issue for unified classification
- [#360](https://github.com/pgarciaq/ros-ocp-backend/issues/360): Container phase
- [#361](https://github.com/pgarciaq/ros-ocp-backend/issues/361): Namespace phase
- [#362](https://github.com/pgarciaq/ros-ocp-backend/issues/362): Node phase
- [internal/engine/core/types.go](../../internal/engine/core/types.go) — ContainerRec struct
- [internal/engine/node/types.go](../../internal/engine/node/types.go) — Node Rec struct
- [internal/api/handlers.go](../../internal/api/handlers.go) — Container filter handling
- [internal/api/handlers_node_utilization.go](../../internal/api/handlers_node_utilization.go) — Node filter handling
