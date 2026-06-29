# Extract native recommendation engine into librobne Go module

## Summary

Extract the pure computation core of the ros-ocp-backend native engine into a
standalone Go module ("librobne") with zero external dependencies beyond Go
stdlib. This enables the planned robne-operator (Local Mode) to import the same
recommendation algorithms as a library dependency without pulling in
ros-ocp-backend's infrastructure (Kafka, GORM, Echo, ingestion pipeline).

After extraction, ros-ocp-backend becomes a thin orchestration layer on top of
librobne — the computation logic is identical, just importable from a separate
module.

**ADR:** [0303-library-extraction-librobne](../adr/0303-library-extraction-librobne.md)
**Scalability analysis:** [librobne-scalability](../../docs-site/planned-features/librobne-scalability.md)

## Work Items

### Phase 1: Prepare extraction targets (ros-ocp-backend)

- [ ] **Make 3 private compute functions public** (trivial — rename only)
  - Quota: export inner recommendation function
  - Cluster Quota: export inner recommendation function
  - PVC: export inner recommendation function
  - Entities affected: Quota, Cluster Quota, PVC

- [ ] **Extract `SnapshotInventoryRow` as standalone struct** (small)
  - Currently coupled to the model package with DB tags
  - Create a pure data struct in the engine package with no GORM/DB annotations
  - Map between the two at the orchestration boundary
  - Entities affected: Snapshot

- [ ] **Extract namespace aggregation logic from DB query loop** (medium)
  - Currently the namespace recommendation interleaves DB reads with
    per-container aggregation in the same function
  - Split into: (1) DB query that returns `[]DigestRow`, (2) pure
    `RecommendNamespace()` function that computes the recommendation
  - Entities affected: Namespace

- [ ] **Move `model.DailyVMDigest` and `model.VMRecommendation` into library types** (small)
  - Create library-side equivalents without GORM tags
  - Add conversion functions at the ros-ocp-backend boundary
  - Entities affected: VM

- [ ] **Audit `GPUIdleConfig` loading for self-containment** (trivial)
  - Verify that the GPU idle configuration struct can be passed as a plain
    value without requiring Viper or DB access
  - Entities affected: GPU MIG

### Phase 2: Create librobne module

- [ ] **Initialize Go module** (`github.com/<org>/librobne`)
  - `go.mod` with Go stdlib dependencies only
  - CI pipeline: `go test ./...`, `go vet ./...`, `staticcheck ./...`

- [ ] **Copy pure computation files**
  - Per-entity files: `container.go`, `node.go`, `vm.go`, `gpu.go`,
    `gpu_timeslicing.go`, `pvc.go`, `quota.go`, `snapshot.go`, `namespace.go`
  - Shared utilities: `percentile.go`, `decay.go`, `margin.go`, `trend.go`,
    `idle.go`, `notifications.go`
  - All input/output types (config structs, digest row types, recommendation
    result types)

- [ ] **Copy and adapt tests**
  - Unit tests for each per-entity function
  - Golden-file / snapshot tests for regression detection
  - Benchmark tests for the hot path (container recommendation at scale)

- [ ] **Write module documentation**
  - `README.md` with usage examples
  - GoDoc comments on all exported functions and types
  - Architecture decision reference to ADR-0303

### Phase 3: Migrate ros-ocp-backend to import librobne

- [ ] **Replace inline engine calls with librobne imports**
  - Update `internal/engine/` to call `librobne.RecommendCPUAndMemory()` etc.
  - Remove duplicated computation code from ros-ocp-backend
  - Retain orchestration code (batch reading, plugin pipeline, write-back)

- [ ] **Run full test suite** — verify no behavioral change
  - All existing unit tests must pass without modification
  - Integration tests with testcontainers PostgreSQL
  - Manual smoke test: ingest OCP data, verify identical recommendations

- [ ] **Update go.mod** to depend on librobne module

## Per-Entity Extraction Status

| Entity | Current state | Extraction tier | Blocking work |
|--------|---------------|-----------------|---------------|
| Container | Exported pure functions | Ready now | None |
| Node | Exported pure functions | Ready now | None |
| VM | Exported pure functions | Ready now | Move types to library |
| GPU MIG | Exported pure functions | Ready now | Audit config loading |
| GPU Time-slicing | Exported pure functions | Ready now | None |
| PVC | Private compute function | Needs export | Rename to public |
| Quota | Private compute function | Needs export | Rename to public |
| Cluster Quota | Private compute function | Needs export | Rename to public |
| Namespace | DB-interleaved logic | Needs refactoring | Extract aggregation |
| Snapshot | DB-interleaved logic | Needs refactoring | Extract inventory struct |

## Definition of Done

1. **librobne module exists** as a standalone Go module with zero dependencies
   beyond Go stdlib.
2. **All 9 entity types** have exported per-entity functions with typed I/O.
3. **ros-ocp-backend imports librobne** — no computation code remains in
   `internal/engine/` (only orchestration).
4. **No behavioral change** — identical recommendations for identical inputs,
   verified by:
   - All existing unit tests passing
   - Integration test suite passing
   - Side-by-side comparison of recommendations before/after on test data
5. **CI green** on both repos (librobne standalone and ros-ocp-backend with
   librobne dependency).
6. **Documentation updated:**
   - librobne `README.md` with usage examples
   - ros-ocp-backend `CHANGELOG.md` entry
   - ADR-0303 status changed from "Proposed" to "Accepted"

## Non-Goals

- **robne-operator integration** is a separate effort. This issue covers only
  the library extraction and ros-ocp-backend migration.
- **Cut 2 orchestration interfaces** (`DigestProvider`, `RecommendationEmitter`)
  are explicitly excluded — see ADR-0303 for rationale.
- **API changes** — none. The extraction is purely internal.
- **Performance optimization** — the extraction must be behavior-preserving.
  Performance improvements to the extracted functions are separate work.
