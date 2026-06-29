# ADR-0303: Library Extraction of the Native Engine (librobne)

## Status

Proposed

## Phase

Future (cross-repo: ros-ocp-backend + robne-operator)

## Context

The ros-ocp-backend native engine ("robne") contains pure recommendation
algorithms for nine entity types: container, node, VM, GPU MIG, GPU
time-slicing, PVC, namespace quota, cluster quota, and snapshot. The planned
robne-operator ([Local Mode](../features/local-mode.md)) needs the same
algorithms embedded in an on-cluster Go operator.

Currently the engine code lives in `internal/engine/` with a mix of pure
computation and DB-coupled orchestration. The pure computation functions take
typed inputs (digest rows, configuration structs, threshold settings) and return
typed outputs (recommendation structs, notification codes) with no database
access, no I/O, and no allocation beyond return values. The orchestration layer
(`recommend_all.go`, the Produce/Enrich/Optimize plugin pipeline) handles batch
reading from PostgreSQL, concurrency, error handling, and write-back.

### Problem

Without extraction, the robne-operator must either:

1. Copy-paste the engine code (divergence risk, double maintenance), or
2. Import ros-ocp-backend as a dependency (pulls in Kafka, GORM, Echo, and the
   entire ingestion pipeline — unacceptable for an operator binary).

### Prior art

The Local Mode design document already identifies a shared `ros-ocp-engine` Go
module as the code-sharing mechanism between ros-ocp-backend and robne-operator.
This ADR specifies the concrete extraction boundary, per-entity readiness, and
the work required to reach it.

## Decision

Extract the pure computation core into a standalone Go module ("librobne") with
zero external dependencies beyond Go stdlib. Use the "Cut 1" approach: extract
stateless, pure functions only. The library exposes per-entity function families
(not a unified interface) because each entity has fundamentally different
input/output shapes.

ros-ocp-backend becomes a thin orchestration layer on top of librobne (DB
queries, Kafka consumption, API serving, plugin pipeline).

### Why per-entity functions, not a unified Recommender interface

Each entity type has fundamentally different input shapes:

- **Container:** Time-series percentiles with decay weighting, OOM history
- **Node:** Allocatable vs capacity ratios, pod scheduling, imbalance detection
- **VM:** Instance catalog matching, guest agent metrics, downsize hysteresis
- **GPU MIG:** Multi-metric tree (SM, tensor, DRAM), MIG profile selection
- **GPU time-slicing:** Node-level GPU group analysis, majority rule
- **PVC:** Growth projection via decay-weighted regression, trend extrapolation
- **Quota / Cluster Quota:** Container recommendation aggregation, headroom bands
- **Namespace:** Per-container rollup with term/engine grouping
- **Snapshot:** Priority-ordered classification rules, label-based grouping

A common `Recommender` interface would require either a `map[string]any` input
(losing type safety) or a massive union type (leaky abstraction). Per-entity
functions preserve compile-time type checking and allow each entity's API to
evolve independently.

### Why not Cut 2 (orchestration with injected DB interfaces)

A deeper cut would add `DigestProvider` / `RecommendationEmitter` interfaces
around the DB layer, allowing the library to manage its own read/compute/write
loop. This is less useful because:

- The robne-operator needs a completely different orchestration model (15s
  Prometheus queries vs 6-12h batch from PostgreSQL digest tables).
- The streaming/batching pattern in `recommend_all.go` is designed for batch
  processing, not real-time reconciliation loops.
- The operator has its own reconciliation loop (controller-runtime), error
  handling, concurrency model, and graceful shutdown semantics.
- Config loading differs fundamentally (CRD spec vs env vars + DB queries).

Cut 2 would force both consumers into a shared orchestration abstraction that
fits neither well. Pure functions let each consumer build its own orchestration.

### Extraction readiness by entity

| Tier | Entities | Current state | Work needed |
|------|----------|---------------|-------------|
| Ready now | Container, Node, VM, GPU MIG, GPU Time-slicing | Exported pure functions with typed I/O | None |
| Needs export | Quota, Cluster Quota, PVC | Pure inner logic exists but functions are private | Make private compute functions public (rename) |
| Needs refactoring | Namespace, Snapshot | DB access interleaved with computation | Extract intermediate data-transfer structs |

### Library interface (per-entity functions)

```go
// container.go
func RecommendCPUAndMemory(rows []DigestRow, cpu CPUConfig, mem MemoryConfig) (CPURec, MemoryRec, ContainerExplanationFactors)

// node.go
func RecommendNodes(digests []NodeDigestRow, cfg NodeRecConfig, thresholds NodeThresholdSettings, terms []TermConfig) []NodeRec

// vm.go
func RecommendVM(digests []VMDigest, cfg VMRecConfig, term TermWindow, engine string, instanceTypes []InstanceType, prefs *VMPreferenceContext, clusterDigests []VMDigest, nodeMemMap map[string]float64) (*VMRecommendation, error)

// gpu.go
func RecommendGPU(digests []GPUDigestRow, settings GPUThresholdSettings, idleCfg GPUIdleConfig) *GPURec

// gpu_timeslicing.go
func ComputeNodeTimeslicing(group NodeGPUGroup, gpuRate *float32, now time.Time, settings GPUThresholdSettings) *TimeslicingRec

// pvc.go
func RecommendPVC(digests []PVCDigestRow, term TermConfig, settings PVCThresholdSettings) PVCRec

// quota.go
func RecommendQuota(snap NamespaceQuotaSnapshot, agg ContainerQuotaAggregate, cfg QuotaRecConfig) QuotaRec
func RecommendClusterQuota(snap ClusterQuotaSnapshot, agg NamespaceQuotaClusterAggregate, cfg QuotaRecConfig) ClusterQuotaRec

// snapshot.go
func ClassifySnapshot(row SnapshotInventoryRow, pvcGroupIndex SnapshotGroupIndex, settings SnapshotSettings) SnapshotRec

// namespace.go
func RecommendNamespace(containerDigests []DigestRow, cpuCfg CPUConfig, memCfg MemoryConfig) NamespaceRec
```

### Proposed module layout

```
librobne/
├── container.go        # RecommendCPUAndMemory + types
├── node.go             # RecommendNodes + types
├── vm.go               # RecommendVM + types (largest file)
├── gpu.go              # RecommendGPU + types
├── gpu_timeslicing.go  # ComputeNodeTimeslicing + types
├── pvc.go              # RecommendPVC + types
├── quota.go            # RecommendQuota + RecommendClusterQuota + types
├── snapshot.go         # ClassifySnapshot + types
├── namespace.go        # RecommendNamespace + types
├── percentile.go       # Shared: weighted percentile with decay
├── decay.go            # Shared: exponential decay weight function
├── margin.go           # Shared: adaptive margin arithmetic (scaled int)
├── trend.go            # Shared: linear trend / WLS regression
├── idle.go             # Shared: idle classification helpers
└── notifications.go    # Shared: notification bitmask codes
```

All shared utilities (`percentile.go`, `decay.go`, `margin.go`, `trend.go`,
`idle.go`, `notifications.go`) are internal to the module — exported only if
consumers genuinely need them. The primary public API is the per-entity
`Recommend*` / `Classify*` / `Compute*` functions.

### Performance impact

None. Go compiles everything into a single binary — the module boundary adds
zero runtime overhead. All functions are pure (no I/O, no allocation beyond
return values), so call frequency does not affect per-call cost.

### Work items before extraction

| Work item | Effort | Entities affected |
|-----------|--------|-------------------|
| Make 3 private compute functions public | Trivial (rename) | Quota, Cluster Quota, PVC |
| Extract `SnapshotInventoryRow` as standalone struct | Small | Snapshot |
| Extract namespace aggregation logic from DB query loop | Medium | Namespace |
| Move `model.DailyVMDigest` and `model.VMRecommendation` into library types | Small | VM |
| Audit `GPUIdleConfig` loading for self-containment | Trivial | GPU MIG |

## Consequences

### Positive

- **Code sharing without coupling.** robne-operator imports librobne as a Go
  dependency; any Go application can compute resource recommendations without
  importing ros-ocp-backend's infrastructure.
- **Forces cleaner separation.** Extracting pure functions makes the boundary
  between computation and orchestration explicit in the ros-ocp-backend codebase.
- **Independent versioning and testing.** The engine algorithms can be versioned,
  released, and tested independently from the API/ingestion layers. Algorithm
  changes are validated in isolation before integration.
- **Enables third-party consumption.** CLI tools, CI pipelines, or custom
  operators could import librobne to compute recommendations from their own data
  sources.

### Negative

- **Two repos to maintain.** librobne lives in a separate repository (or Go
  module within a monorepo). Dependency updates, CI, and releases add overhead.
- **Breaking changes propagate.** Type changes in librobne affect both
  ros-ocp-backend and robne-operator. Mitigated by semantic versioning and the
  small, stable surface area of each per-entity function.
- **Initial extraction effort.** The namespace and snapshot entities require
  non-trivial refactoring to decouple DB access from computation logic.

### Neutral

- No behavioral change in ros-ocp-backend. After extraction, the same functions
  are called with the same inputs and produce the same outputs. The module
  boundary is invisible to users.
- No API changes. The extraction is purely internal to the Go codebase.
- No migration required. librobne contains no database code.

## Alternatives Considered

### Keep engine code in ros-ocp-backend, vendor into robne-operator

Copy the relevant `.go` files into the operator repository. Avoids the overhead
of a separate module but creates divergence risk. Even with automated sync
scripts, the two copies inevitably drift as each repo applies local fixes.
Rejected because the maintenance burden grows with each entity type added.

### Extract the full plugin pipeline (Cut 2 + Cut 3)

Extract not only the pure functions but also the Produce/Enrich/Optimize
orchestration and the plugin registry. This would let robne-operator reuse the
entire pipeline with swapped data providers. Rejected because the orchestration
model differs fundamentally between batch (ros-ocp-backend) and real-time
(robne-operator). The abstraction cost outweighs the code reuse benefit.

### Unified Recommender interface with type assertions

Define `type Recommender interface { Recommend(input any) (any, error) }` and
use type assertions at call sites. Rejected because it sacrifices compile-time
type safety — the primary advantage of Go's type system — for superficial
uniformity. Each entity's input/output shapes are stable and well-defined;
per-entity functions are the idiomatic Go approach.

## References

- [Local Mode feature doc](../features/local-mode.md) — robne-operator design
- [ADR-0001](0001-native-engine-over-kruize.md) — Native engine adoption
- [ADR-0099](0099-compile-time-in-process-plugins.md) — Plugin architecture
- [ADR-0277](0277-local-hybrid-on-cluster-engine-deferred-central-only-v1.md) — Deferred on-cluster engine (v1)
- [librobne scalability analysis](../../docs-site/planned-features/librobne-scalability.md) — 200K container analysis
