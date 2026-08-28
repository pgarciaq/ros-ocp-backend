# ADR-0036: Scope business hours to container+namespace only

## Status

Amended (2026-08-29) — Namespace **HTTP list** omits nested `business_hours` ([#497](https://github.com/pgarciaq/ros-ocp-backend/issues/497)). Unfiltered list still uses `NamespaceDetailResponse` (ADR-0294 fat default) but strips the nest. Slim `filter[term]` / `filter[engine]` already omitted it via `toListDetailEngine`. Detail unchanged. Dual list DTO is not collapsed.

Amended (2026-08-24) — robne CLI JSON BH siblings (#487) **shipped**. Envelope **11** when node/gpu/vm plugins run with YAML `business_hours`. Sibling keys are **full CLI DTOs** (same shape as all-hours siblings). Product HTTP nests stay **thin detail-only**. YAML `business_hours` + `--plugins node|gpu|vm` is allowed. Dual-write node/GPU/VM BH **digests**; do not upsert BH recs onto tables without `schedule_type`.

Amended (2026-08-22) — VM product API (#486) dual-writes `daily_vm_digests` and nests a **thin** `business_hours` object on **GET .../vm/detail** only (namespace schedule; drop-or-full weighting). Product nests stay thin; CLI JSON siblings shipped later in #487.

Amended (2026-08-17) — GPU timeslicing product API (#491) nests `business_hours` on **GET .../gpu/timeslicing/{node}** only (cluster window; homogeneous node × model groups). GPU container-detail nested sizing remains #485. Node product API (#484) dual-writes `daily_node_digests` and nests `business_hours` on **node detail** only. (CLI JSON siblings later shipped in #487.)

## Context

Infrastructure (nodes) and storage (PVC) don't follow office-hour patterns the same way interactive containers do. v1 therefore limited business-hours schedules to container and namespace plugins.

Node sizing is still peak-oriented: overnight batch can be the real capacity constraint. Shipping node BH without a warning would invite operators to consolidate on business-hours numbers and page at 3am.

## Decision

**v1 (unchanged):** Container and namespace plugins support business-hours schedules (org → cluster → namespace inheritance).

**Amendment (#497):** Namespace **HTTP list** stays all-hours. Nested `business_hours` is **namespace detail only** (`GET .../namespaces/{id}`). Unfiltered list may still serialize `NamespaceDetailResponse` (ADR-0294) but must strip the nest. Slim projection already omitted it. Do not collapse the dual list DTO in this change.

**Amendment (#484):** Nodes get a **cluster-scoped** business-hours digest stream and nested detail sizing:

- Dual-write `daily_node_digests` (`all_hours` | `business_hours`) at ingest when org ⊕ cluster is enabled. Namespace-only enablement does **not** dual-write node BH (`ProducesNodeBusinessHoursDigests` / `ResolveCluster`).
- Node day build is not container `ComputeWeightedDigest`. Dual accumulators + `AddRowWeighted`: hour-bucket sums, P50/P95 across hours. Weight `<= 0` drops the row; `0 < w < 1` scales usage/request only (capacity/allocatable maxes unscaled). `hourIndex` is UTC hour. `hourly_node_digests` stays all-hours only.
- List stays all-hours. Nested `business_hours` is **node detail only**. The node plugin does **not** implement `APIEnricher`; the detail handler enriches and warns on error without failing the request.
- Catalog code **79** `NODE_BH_NOT_PEAK_SAFE` (WARNING) is emitted **only** on the nested object when sizing is present. Reason-only insufficient-data blocks do not get 79. No `peak_safe` boolean. Code 78 is not added to Definitions/DB.
- robne CLI: `WriteNodeDigests` / `ReadNodeDigests` default to `all_hours` for product ingest. CLI JSON BH siblings shipped in #487 (see amendment below).

**Amendment (#485):** GPU container digests get a **namespace-scoped** business-hours stream and nested detail sizing:

- Dual-write `gpu_container_digests` (`all_hours` | `business_hours`) at ingest when `ProducesBusinessHoursDigests()` (same as container — namespace-only enablement **does** produce GPU BH). Partition PK stays `(id, interval_start)`. `schedule_type` is added to the natural unique index. Persist rec tables stay all-hours.
- Weighting is not container `ComputeWeightedDigest` and not node usage-only scaling. Weight `<= 0` drops the sample; otherwise the **full** sample is included (min/max/avg unscaled).
- List, MIG list, and timeslicing **list** stay all-hours. Nested `business_hours` is **container detail `gpu.{term}` only**. The GPU plugin `APIEnricher` stays rates-only; the container detail handler attaches BH and warns on error without failing the request.
- Catalog code **80** `GPU_BH_OFFICE_WINDOW` (WARNING) is emitted **only** on the nested object when sizing is present. Reason-only insufficient-data blocks do not get 80. No `peak_safe` boolean. Do not reuse 79. Code 78 is not added to Definitions/DB.
- robne CLI: `WriteGPUContainerDigests` / `ReadGPUContainerDigests` default to `all_hours` for product ingest. CLI JSON BH siblings shipped in #487. No workload-type Settings opt-out.

**Amendment (#491):** GPU time-slicing gets nested business-hours replica sizing on **detail only**:

- Cluster gate: compute timeslicing BH only when `ProducesNodeBusinessHoursDigests()` (org ⊕ cluster). Namespace-only enablement does **not** produce timeslicing BH.
- Homogeneous cluster window: omit nested BH if **any** container in the **node × GPU model group** has `Resolve(ns) != ResolveCluster()`. No mixed-window math. No reduced candidate subset. No new GPU digest stream / no new `schedule_type` on persist tables.
- Detail route `GET .../gpu/timeslicing/{node}` returns all GPU models on that node (same row shape as the list) and nests `business_hours` there. List, history, GPU summary `timeslicing.count`, backfill, and container `time_slicing_node` / `time_slicing_replicas` stay all-hours.
- Persist unchanged: no `schedule_type` on `node_gpu_timeslicing_recommendations` or history. Recompute BH from BH digests at **read time**.
- Catalog code **81** `GPU_TS_BH_CLUSTER_WINDOW` (WARNING) is emitted **only** on the nested object when a replica recommendation is present. Do not reuse 79 or 80. Do not add 78. Nested payload: replicas / confidence / candidate·impacted counts — **no dollar savings**. Reason-only / omitted-because-heterogeneous: no 81. Heterogeneous → omit the nested object entirely. Insufficient BH days / `ComputeNodeTimeslicingRec` nil → reason-only without 81.
- robne CLI: product YAML gate for `--plugins gpu` is superseded by #487. No CLI timeslicing persist table.

**Amendment (#486):** VMs get a **namespace-scoped** business-hours digest stream and nested **detail** sizing:

- Dual-write `daily_vm_digests` (`all_hours` | `business_hours`) at ingest when `ProducesBusinessHoursDigests()` (same as container/GPU — namespace-only enablement **does** produce VM BH). Heap-table unique key includes `schedule_type`. Persist rec tables (`vm_recommendations`, history) stay all-hours. `hourly_vm_digests` stays all-hours.
- Weighting is **drop-or-full**, not container `ComputeWeightedDigest`. Weight `<= 0` drops the 15-minute sample; otherwise the **full** sample is included (percentiles unscaled). Default `off_hours_weight=0` makes this identical to true weighting; they diverge only if a fractional off-hours weight is configured (see [docs/features-business-hours.md](../features-business-hours.md#vm-business-hours-considerations)).
- List, history, CSV, and group-by stay all-hours. Nested `business_hours` is **GET .../vm/detail only**. The VM plugin does **not** implement `APIEnricher`; the detail handler enriches and warns on error without failing the request.
- **Thin nest vs full nest:** Persist still runs `RecommendVM` only on all-hours. Detail-read invokes `RecommendVM` on the BH digest stream (one extra recommend per GET, not a second nightly pipeline) and copies **only** vCPU/GiB + reason + code 82. A full nest (copy the entire VM rec: instance-type SKU, idle/abandoned/power-off, guest GPU, disk, I/O, network, parent notification array including 64, nested dollars) was rejected.
- Nested `notifications` is the **Kruize map** (same shape as node/GPU BH). Parent VM `notifications` stay a JSON **array**. Do not merge 82 into the parent array.
- Catalog code **82** `VM_BH_OFFICE_WINDOW` (WARNING) is emitted **only** on the nested object when sizing is present. Reason-only insufficient-data blocks do not get 82. Do not reuse 64/79/80/81. Do not add 78. Disabled schedule omits the object.
- Guest GPU devices dual-write onto the BH parent digest. Nested detail still **omits** GPU. Do not write `gpu_container_digests` for VMs. PVC attaches only to the **all_hours** parent.
- Cluster/namespace/org prune includes `daily_vm_digests`. VM ingest that cannot produce BH calls **only** `PruneClusterVMBusinessHoursDigests` (never the full cluster prune, which would delete container/node/GPU BH).
- robne CLI: `WriteVMDigests` / `ReadVMDigests` default to `all_hours` for product ingest. CLI JSON BH siblings shipped in #487 (`DailyVMDigestsWeighted` drop-or-full).

**Amendment (#487):** robne CLI emits **full** JSON siblings for node/GPU/timeslicing/VM when YAML `business_hours` is on and that plugin is enabled. Envelope **11** (stay **10** when only container/namespace BH siblings are present). Keys are arrays, never `null`; omit the key when the plugin is off. Dual-write BH **digests**; never upsert BH recs onto tables without `schedule_type`. Product HTTP nests remain thin (this ADR’s product amendments are unchanged). PVC/quota/snapshot stay out of CLI BH.

PVC remains not applicable (cumulative storage).

## Alternatives Considered

### Extend business hours to nodes and PVC in v1
Node consolidation and storage growth don't follow office-hour patterns; BH recommendations would mislead infrastructure teams with arbitrary scale-down windows.

### Cluster-wide business-hours schedule only
Too coarse for mixed container workloads—batch jobs and 24×7 services in the same cluster need per-namespace schedules, not one cluster default. Nodes still use org ⊕ cluster only because they are cluster-wide.

### `peak_safe` boolean on the nested object
A boolean invites clients to treat BH as an alternate 24/7 size. A WARNING notification (79) on the nested block is the honest signal.

## Consequences

GPU, VM, and PVC stay out of product BH except GPU container-detail nested sizing (#485), GPU timeslicing detail nested sizing (#491), and VM detail nested sizing (#486). **CLI JSON siblings (#487) are full DTOs** and are not the product nest. Node and VM list/API savings stay all-hours. Operators who open VM detail with a namespace schedule see a second vCPU/GiB perspective labeled as using the namespace office window (thin nest; overnight batch excluded). PVC remains excluded.

## References

- [docs/features-business-hours.md](../features-business-hours.md)
- [#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)
- [#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)
- [#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)
- [#486](https://github.com/pgarciaq/ros-ocp-backend/issues/486)
- [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)
- [#483](https://github.com/pgarciaq/ros-ocp-backend/issues/483)
