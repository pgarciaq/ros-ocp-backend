# ADR-0036: Scope business hours to container+namespace only

## Status

Amended (2026-08-17) — GPU product API (#485) dual-writes `gpu_container_digests` and nests `business_hours` on **container detail** `gpu.{term}` only. Node product API (#484) dual-writes `daily_node_digests` and nests `business_hours` on **node detail** only. VM and CLI JSON siblings remain out of scope.

## Context

Infrastructure (nodes) and storage (PVC) don't follow office-hour patterns the same way interactive containers do. v1 therefore limited business-hours schedules to container and namespace plugins.

Node sizing is still peak-oriented: overnight batch can be the real capacity constraint. Shipping node BH without a warning would invite operators to consolidate on business-hours numbers and page at 3am.

## Decision

**v1 (unchanged):** Container and namespace plugins support business-hours schedules (org → cluster → namespace inheritance).

**Amendment (#484):** Nodes get a **cluster-scoped** business-hours digest stream and nested detail sizing:

- Dual-write `daily_node_digests` (`all_hours` | `business_hours`) at ingest when org ⊕ cluster is enabled. Namespace-only enablement does **not** dual-write node BH (`ProducesNodeBusinessHoursDigests` / `ResolveCluster`).
- Node day build is not container `ComputeWeightedDigest`. Dual accumulators + `AddRowWeighted`: hour-bucket sums, P50/P95 across hours. Weight `<= 0` drops the row; `0 < w < 1` scales usage/request only (capacity/allocatable maxes unscaled). `hourIndex` is UTC hour. `hourly_node_digests` stays all-hours only.
- List stays all-hours. Nested `business_hours` is **node detail only**. The node plugin does **not** implement `APIEnricher`; the detail handler enriches and warns on error without failing the request.
- Catalog code **79** `NODE_BH_NOT_PEAK_SAFE` (WARNING) is emitted **only** on the nested object when sizing is present. Reason-only insufficient-data blocks do not get 79. No `peak_safe` boolean. Code 78 is not added to Definitions/DB.
- robne CLI: `WriteNodeDigests` / `ReadNodeDigests` default to `all_hours`. YAML `business_hours` with explicit `--plugins node` (or gpu/vm) remains a hard error. No CLI JSON BH siblings (#487).

**Amendment (#485):** GPU container digests get a **namespace-scoped** business-hours stream and nested detail sizing:

- Dual-write `gpu_container_digests` (`all_hours` | `business_hours`) at ingest when `ProducesBusinessHoursDigests()` (same as container — namespace-only enablement **does** produce GPU BH). Partition PK stays `(id, interval_start)`. `schedule_type` is added to the natural unique index. Persist rec tables stay all-hours.
- Weighting is not container `ComputeWeightedDigest` and not node usage-only scaling. Weight `<= 0` drops the sample; otherwise the **full** sample is included (min/max/avg unscaled).
- List, MIG list, and timeslicing stay all-hours. Nested `business_hours` is **container detail `gpu.{term}` only**. The GPU plugin `APIEnricher` stays rates-only; the container detail handler attaches BH and warns on error without failing the request.
- Catalog code **80** `GPU_BH_OFFICE_WINDOW` (WARNING) is emitted **only** on the nested object when sizing is present. Reason-only insufficient-data blocks do not get 80. No `peak_safe` boolean. Do not reuse 79. Code 78 is not added to Definitions/DB. Timeslicing BH is a follow-up ([#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)).
- robne CLI: `WriteGPUContainerDigests` / `ReadGPUContainerDigests` default to `all_hours`. YAML `business_hours` with explicit `--plugins gpu` remains a hard error. No CLI JSON BH siblings (#487). No workload-type Settings opt-out.

VM and PVC stay out of product BH (#486). PVC remains not applicable (cumulative storage).

## Alternatives Considered

### Extend business hours to nodes and PVC in v1
Node consolidation and storage growth don't follow office-hour patterns; BH recommendations would mislead infrastructure teams with arbitrary scale-down windows.

### Cluster-wide business-hours schedule only
Too coarse for mixed container workloads—batch jobs and 24×7 services in the same cluster need per-namespace schedules, not one cluster default. Nodes still use org ⊕ cluster only because they are cluster-wide.

### `peak_safe` boolean on the nested object
A boolean invites clients to treat BH as an alternate 24/7 size. A WARNING notification (79) on the nested block is the honest signal.

## Consequences

GPU, VM, and PVC stay out of product BH except GPU container-detail nested sizing (#485). Node list/API savings stay all-hours. Operators who open container detail with a namespace schedule see a second GPU sizing perspective labeled as using the office window. Timeslicing BH is [#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491). VM still excluded (#486).

## References

- [docs/features-business-hours.md](docs/features-business-hours.md)
- [#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)
- [#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)
- [#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)
- [#483](https://github.com/pgarciaq/ros-ocp-backend/issues/483)
