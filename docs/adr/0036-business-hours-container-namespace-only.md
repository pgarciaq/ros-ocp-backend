# ADR-0036: Scope business hours to container+namespace only

## Status

Amended (2026-08-17) — node product API (#484) now dual-writes `daily_node_digests` and nests `business_hours` on **node detail** only. GPU, VM, PVC, and CLI JSON siblings remain out of scope.

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

GPU, VM, and PVC stay out of product BH (#485/#486). PVC remains not applicable (cumulative storage).

## Alternatives Considered

### Extend business hours to nodes and PVC in v1
Node consolidation and storage growth don't follow office-hour patterns; BH recommendations would mislead infrastructure teams with arbitrary scale-down windows.

### Cluster-wide business-hours schedule only
Too coarse for mixed container workloads—batch jobs and 24×7 services in the same cluster need per-namespace schedules, not one cluster default. Nodes still use org ⊕ cluster only because they are cluster-wide.

### `peak_safe` boolean on the nested object
A boolean invites clients to treat BH as an alternate 24/7 size. A WARNING notification (79) on the nested block is the honest signal.

## Consequences

Container/namespace BH unchanged. Node list/API savings stay all-hours. Operators who open node detail with a cluster schedule see a second sizing perspective labeled not peak-safe. GPU/VM still excluded.

## References

- [docs/features-business-hours.md](docs/features-business-hours.md)
- [#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)
- [#483](https://github.com/pgarciaq/ros-ocp-backend/issues/483)
