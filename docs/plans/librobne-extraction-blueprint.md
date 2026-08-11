# librobne extraction blueprint

**Status:** Draft for review (2026-08-12)  
**Tracking:** [GitHub #94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)  
**ADR:** [0303-library-extraction-librobne](../adr/0303-library-extraction-librobne.md)  
**Branch:** `pgarciaq-rosocp-superpowers-phase17` (planning + implementation)

This document is the **implementation blueprint** for [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94).
It supersedes the flat file list in the issue where the current codebase has evolved.
**Do not start module creation, code moves, or import rewiring until this blueprint is
approved.**

---

## 1. Goal

Extract the **pure recommendation computation core** of the native engine into a
standalone Go module (`librobne`) that:

- Has **stdlib-only** dependencies (no pgx, Echo, Kafka, Viper, GORM, AWS, etc.)
- Is **statically linked** into consumers (ros-ocp-backend, future robne-operator Local Mode)
- Exposes a **Go library contract** (typed functions and structs), not a network service

After extraction, `ros-ocp-backend` keeps orchestration: DB reads/writes, plugin pipeline,
Kafka ingestion, API handlers, savings/cost integration, persistence, retention.

---

## 2. Architecture constraints (non-negotiable)

From [#94 Architecture Constraints](https://github.com/pgarciaq/ros-ocp-backend/issues/94):

| Allowed | Not in scope (now) |
|---------|-------------------|
| Go package import / static link | HTTP API server in librobne |
| Exported functions + value types | gRPC API in librobne |
| Deterministic pure compute | Dynamic/binary plugin loading |
| Unit tests without PostgreSQL | Third-party extension APIs (future only) |

**“Public API” means the Go library surface**, not REST or gRPC. Consumers call
`librobne.Recommend…()` directly in-process.

---

## 3. Review of [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94) vs current code

Issue #94 (Opus 4.6, June 2026) remains directionally correct. This section records
what still matches `internal/engine/` on `phase17` and what needs updating in the issue
text when implementation starts.

### Still accurate

- **Cut 1 only:** pure functions in librobne; orchestration stays in ros-ocp-backend
  (ADR-0303 rationale unchanged).
- **Per-entity function families** instead of a unified `Recommender` interface.
- **Nine entity families:** container, node, VM, GPU MIG, GPU time-slicing, PVC,
  namespace quota, cluster quota, snapshot.
- **Namespace + snapshot** still need the most refactoring (DB interleaved today).
- **Behavior-preserving extraction** — no algorithm changes during the move.
- **Non-goals:** no robne-operator wiring in #94, no Cut 2 `DigestProvider` /
  `RecommendationEmitter`, no user-facing API changes.

### Outdated or incomplete in #94

| #94 claim | Current `phase17` reality |
|-----------|---------------------------|
| “Make 3 private compute functions public” for quota/PVC | **Quota:** `ComputeQuotaRecommendation` and `ComputeClusterQuotaRecommendation` are already exported in `internal/engine/quota/`. **PVC:** inner `computePVCRecommendation` is still **private** (rename/export still required). |
| Flat `librobne/*.go` layout | Engine already lives in subpackages: `container/`, `node/`, `vm/`, `gpu/`, `pvc/`, `quota/`, `snapshot/`, shared `core/`. Blueprint uses a **package map**, not a single flat directory. |
| VM “exported pure functions, move types only” | `RecommendVM` is exported but takes/returns **`model.DailyVMDigest` / `model.VMRecommendation`** (GORM-tagged tenant models). Library types + boundary converters are **required**, not optional polish. |
| GPU MIG “audit config loading” marked trivial | `LoadGPUIdleConfig` is a **package-level function variable** that reads org settings from PostgreSQL (`internal/engine/gpu/settings.go`). Pure `RecommendGPUWithSettings` exists, but production path still wires DB-backed idle config — decouple before extract. |
| “Zero allocation beyond return values” (ADR) | Aspirational; not enforced today. Extraction must **not regress** allocations; optimizing is out of scope for #94. |

### Code layout today (extraction source)

```
internal/engine/
├── core/           # DigestRow, configs, decay, margin, trend, idle helpers, explanation types
├── container/      # RecommendCPU*, replica optimization, notifications (compute)
├── node/           # RecommendNodes (compute)
├── vm/             # RecommendVM, MIG/time-slicing, placement (compute + heavy model.* coupling)
├── gpu/            # RecommendGPU*, timeslicing compute, embedded catalog helpers
├── pvc/            # computePVCRecommendation (private), RecommendPVCs (DB orchestration)
├── quota/          # Compute*Recommendation (public), Recommend* (DB orchestration)
├── snapshot/       # classifySnapshot* (private), ClassifySnapshots (DB orchestration)
├── recommend_all.go, recommend_namespace.go, …  # orchestration at engine root
└── *savings*, *persist*, *history*               # NOT librobne — cost/DB layers
```

---

## 4. Boundary: inside librobne vs stays in ros-ocp-backend

### Inside librobne (stdlib only)

- Weighted percentiles, decay, adaptive margin, trend/WLS regression
- Per-entity **recommendation math** and classification rules
- Notification **code selection** for recommendations (integer codes, not API catalog)
- Explanation factor structs attached to recommendations
- Embedded **read-only data** required for compute (e.g. GPU catalog YAML, vGPU profiles,
  VM instance catalog) via `go:embed` inside librobne
- Pure functions that accept **already-loaded** config structs (`CPUConfig`, `GPUThresholdSettings`,
  `GPUIdleConfig`, `QuotaRecConfig`, `SnapshotSettings`, etc.)

### Stays in ros-ocp-backend (orchestration / I/O)

- `pgxpool` queries, migrations, batch flush (`core.FlushRecommendationBatch` uses pgx)
- Plugin registry, `recommend_all.go` streaming, poller scheduling
- `internal/costdata`, savings micro-cent math, currency, Masu rate fetch
- `internal/model` persistence types; **adapters** convert model ↔ librobne at boundaries
- Settings resolution from env + DB (`LoadGPUIdleConfig`, threshold recalc hashes, etc.)
- Kafka, S3, Echo handlers, RBAC, metrics registration
- Quality/history persistence, retention sweeps, analytics hooks

### Explicit exclusion: savings and money

Container/node/PVC/VM **savings dollar amounts** depend on `costdata.NamespaceCosts` and
`internal/money`. Those stay in ros-ocp-backend. librobne outputs **usage/request
recommendations** (and notification codes); savings are applied in a later orchestration
step (unchanged behavior).

---

## 5. Proposed module layout

**Module path (proposed):** `github.com/pgarciaq/librobne`  
Separate repository preferred for independent versioning (ADR-0303); alternative:
`ros-ocp-backend/librobne/` submodule during bootstrap — **decision required before M2**.

```
librobne/
├── go.mod                    # stdlib only
├── doc.go                    # package librobne — re-exports or thin facades
├── core/                     # from internal/engine/core (minus pgx batch helpers)
│   ├── digest.go             # DigestRow, TermConfig, configs
│   ├── decay.go, margin.go, trend.go, idle.go
│   ├── explanation.go
│   └── notifications.go      # code constants / bit helpers only
├── container/
├── node/
├── vm/                       # library VMDigest + VMRec types (no model.*)
├── gpu/                      # embed gpu_catalog.yaml, vgpu_profiles.yaml
├── pvc/
├── quota/
├── snapshot/
└── testdata/                 # golden fixtures shared across packages
```

**Import rule for ros-ocp-backend during migration:** only `internal/engine/*` adapter
files and thin wrappers import `librobne`; plugins and API handlers continue to call
engine wrappers until cutover is complete.

---

## 6. Per-entity readiness (updated)

| Entity | Pure compute entry point(s) | Tier | Blocking work before extract |
|--------|----------------------------|------|------------------------------|
| Container | `container.RecommendCPUAndMemory`, `RecommendCPU`, `RecommendMemory`, replica helpers | **Ready** | Move `core` types; keep savings out |
| Node | `node.RecommendNodes` | **Ready** | Move `node.DigestRow` types |
| GPU MIG | `gpu.RecommendGPUWithSettings` | **Ready*** | Pass `GPUIdleConfig` + `GPUThresholdSettings` as values; remove default DB load from library |
| GPU time-slicing | `gpu` timeslicing compute functions | **Ready** | Same settings decoupling |
| VM | `vm.RecommendVM` | **Refactor** | Replace `model.DailyVMDigest` / `model.VMRecommendation` with library types + converters |
| PVC | `pvc.computePVCRecommendation` | **Export** | Rename to exported `ComputePVCRecommendation` (or `RecommendPVC`); split from `RecommendPVCs` DB runner |
| Quota | `quota.ComputeQuotaRecommendation` | **Ready** | Split DB runner; drop `money.DefaultCurrency` from library rec or pass currency in |
| Cluster quota | `quota.ComputeClusterQuotaRecommendation` | **Ready** | Same as quota |
| Namespace | `recommendNamespaceStream` (partial) | **Refactor** | Extract DB loop in `recommend_namespace.go`; pure rollup over `[]core.DigestRow` per namespace × term × engine |
| Snapshot | `classifySnapshot`, `classifySnapshotWithExplanation` | **Refactor** | Export `SnapshotInventoryRow`; pure classify over `[]SnapshotInventoryRow` + group index |

\* GPU “ready” assumes callers supply idle/threshold structs; library must not call
`LoadGPUIdleConfig`.

---

## 7. Library contract principles

1. **Inputs are values** — configs and digest rows passed in; no `context.Context` + pool
   in librobne public functions (orchestrators may use context upstream).
2. **Outputs are values** — recommendation structs with notification codes and explanation
   factors; no DB tags on library types.
3. **No global mutable config** — replace function variables like `LoadGPUIdleConfig` with
   explicit parameters at the librobne boundary; ros-ocp-backend resolves settings then calls in.
4. **Stable per-entity APIs** — semver on librobne; breaking type changes require major bump.
5. **Same numerics** — integer millicores/KiB paths preserved; no float regressions in hot paths.

Representative signatures (target state; names may adjust during export refactors):

```go
// librobne/container
func RecommendCPUAndMemory(rows []core.DigestRow, cpu core.CPUConfig, mem core.MemoryConfig) (
    core.CPURec, core.MemoryRec, core.ContainerExplanationFactors)

// librobne/pvc
func RecommendPVC(digests []pvc.DigestRow, term core.TermConfig, settings pvc.ThresholdSettings,
    notif core.NotificationThresholds) pvc.Rec

// librobne/snapshot
func ClassifySnapshot(row snapshot.InventoryRow, group snapshot.PVCGroupIndex,
    settings snapshot.Settings) snapshot.Rec
```

Full signature list should mirror ADR-0303 but aligned to actual subpackages after Phase 1 prep.

---

## 8. Milestone slices (implementation order)

| Milestone | Deliverable | Repo | Depends on |
|-----------|-------------|------|------------|
| **M0 — Blueprint** | This document approved | ros-ocp-backend | — |
| **M1 — Prep in place** | Export PVC compute; snapshot inventory type; namespace pure func; VM library types; GPU idle/threshold injection only | ros-ocp-backend | M0 |
| **M2 — librobne module** | `go.mod`, CI, copy/move compute packages, tests, README | librobne (new) | M1 |
| **M3 — Consumer import** | `replace` or pseudo-version; wrappers call librobne; delete duplicated compute | ros-ocp-backend | M2 |
| **M4 — Validation** | Full test suite, side-by-side rec comparison on fixture cluster, ADR-0303 → Accepted | both | M3 |

**Stop line:** After M0 (this doc), wait for explicit approval before M1.

Issue #94 “Phase 1 / 2 / 3” maps to **M1 / M2 / M3** above.

---

## 9. Testing strategy

| Layer | What |
|-------|------|
| **librobne unit** | All existing pure tests move with code; `go test -short ./...` without PostgreSQL |
| **Compatibility** | Golden tests: same inputs → same CPU/mem/GPU/… outputs before/after move (subtest per entity) |
| **ros-ocp-backend integration** | Existing testcontainers tests unchanged; they validate orchestration + persistence |
| **Regression gate** | Optional script: run engine on frozen digest fixtures in both layouts, diff JSON (M3) |

Benchmarks (`container` hot path at 200K scale) live in librobne; see
[librobne-scalability](../../docs-site/planned-features/librobne-scalability.md).

---

## 10. CI and release (M2+)

- **librobne:** `go test ./...`, `go vet ./...`, `staticcheck ./...` on each PR
- **ros-ocp-backend:** pin librobne via tagged release or `replace` during development
- **No Docker/Kafka/DB** required for librobne CI

---

## 11. Open decisions (need your input before M1)

1. **Repository home:** new `github.com/pgarciaq/librobne` repo vs subdirectory in
   `ros-ocp-backend` until stable?
2. **Module import path:** `github.com/pgarciaq/librobne` vs future Red Hat org path?
3. **Namespace scope:** extract only all-hours path first, or include business-hours
   `recommendNamespaceStream` in the same library package?
4. **VM type naming:** `vm.Digest` / `vm.Recommendation` in librobne vs prefixed `VMDigestRow`?
5. **Currency on quota recs:** pass `currency string` into compute vs strip from library output?

---

## 12. Explicitly deferred (not until after blueprint approval)

The following were listed as “next PR” in planning but **must not start** until you
approve this blueprint:

- **2.2** — Create `librobne/` module skeleton (`go.mod`, CI)
- **2.3** — Move percentile/decay/margin (or any compute slice) into librobne
- **2.4** — Wire ros-ocp-backend to import librobne

Also deferred: robne-operator / Local Mode integration ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)),
repo rebrand ([#421](https://github.com/pgarciaq/ros-ocp-backend/issues/421)).

---

## 13. Definition of done (unchanged from #94)

1. librobne exists, stdlib-only, statically linked
2. All nine entity families have exported typed compute entry points
3. ros-ocp-backend imports librobne; `internal/engine/` retains orchestration only
4. No behavioral change (unit + integration + spot comparison)
5. CI green on both modules
6. Docs: librobne README, CHANGELOG entry, ADR-0303 status → Accepted

---

## 14. References

- [#94 — Extract native recommendation engine into librobne](https://github.com/pgarciaq/ros-ocp-backend/issues/94)
- [ADR-0303](../adr/0303-library-extraction-librobne.md)
- [librobne scalability (Local Mode)](../../docs-site/planned-features/librobne-scalability.md)
- [Local Mode planned feature](../features/local-mode.md)
