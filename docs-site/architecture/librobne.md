# Integrating librobne

> **Last verified:** 2026-08-29

librobne is the in-process, statically linked recommendation engine shared by
ros-ocp-backend, the [robne CLI](../features/robne-cli.md), and the planned
robne-operator. Algorithms and HTTP contracts live on other pages; **this page
is the library contract.**

Public docs site (this page):
[https://pgarciaq.github.io/ros-ocp-backend/architecture/librobne/](https://pgarciaq.github.io/ros-ocp-backend/architecture/librobne/).
HTML API browse (doc2go, librobne only — not pkgsite, not `internal/`):
[https://pgarciaq.github.io/ros-ocp-backend/pkg/](https://pgarciaq.github.io/ros-ocp-backend/pkg/).
The GitHub **import path is not** `pgarciaq` — see [Import path vs this fork](#import-path-vs-this-fork).

In-tree package map: [`librobne/README.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/README.md)
(not indexed by MkDocs search until this page). Why it was extracted:
[ADR-0303](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0303-library-extraction-librobne.md).
Thresholds and percentiles: [Recommendation Engines](recommendation-engines.md).
Local-mode scale estimates (planned operator): [Local Mode scale estimates](../planned-features/librobne-scalability.md).

---

## Import path vs this fork

| What | Value |
|------|--------|
| Go module | `github.com/redhatinsights/ros-ocp-backend/librobne` |
| Nested replace in this repo | `replace => ./librobne` in the parent `go.mod` |
| Clone this fork | `https://github.com/pgarciaq/ros-ocp-backend` |
| Docs site | `https://pgarciaq.github.io/ros-ocp-backend/` |

Until [rebrand #421](https://github.com/pgarciaq/ros-ocp-backend/issues/421),
consumers import the **redhatinsights** module path. After editing `librobne/`,
run `go mod vendor` in the parent so `vendor/` stays in sync.

A consumer outside this repo that tracks the fork:

```go
import "github.com/redhatinsights/ros-ocp-backend/librobne/engine"
```

```text
replace github.com/redhatinsights/ros-ocp-backend/librobne => github.com/pgarciaq/ros-ocp-backend/librobne v0.0.0-<commit>
```

Do not import `github.com/pgarciaq/ros-ocp-backend/librobne/...` as the module
path. Nested-module `pkg.go.dev` publishing waits on [#430](https://github.com/pgarciaq/ros-ocp-backend/issues/430) /
[#500](https://github.com/pgarciaq/ros-ocp-backend/issues/500). Until then, function-level
HTML is the [doc2go tree](https://pgarciaq.github.io/ros-ocp-backend/pkg/) on this
Pages site (`make docs-build` writes `_site/pkg/`).

Core packages have **no** pgx, Echo, Kafka, or GORM. Optional `csv`, `pgrec`,
and `pgdigest` may import pgx.

---

## Call shape (zero converters)

1. Scan or parse **directly** into canonical types (`types.DigestRow`,
   `types.KeyedDigest`, entity-specific digest rows). Do not copy
   `internal/model` → librobne on the hot path.
2. Call `Recommend*` / `Compute*` / `Classify*` with **no pool**.
3. Persist in the **emit** callback (or collect the returned slice).
4. Call `Apply*` **after** emit when dollar estimates are needed. Forgetting
   it is missing dollars, not wrong millicores. An empty `RateCard` does
   **not** invent `"USD"`. Money is integer micro-cents. Projection hours are
   calendar hours on the `Apply*` argument (`HoursInMonth`), not a RateCard field.

Container runner:

```go
cfg := engine.DefaultEngineConfig(orgID, clusterUUID, now)
err := engine.RecommendWorkloads(ctx, rows, cfg, func(batch []engine.ContainerRec) error {
    // persist or buffer; batch backing array is reused — copy if retaining
    return nil
})
container.ApplySavingsEstimates(recs, rateCard, hoursInMonth)
```

`rows` must be ordered by container key then `BucketDate` (same as the digest
SELECT). `engine` aliases canonical types so the runner is one import.

In-memory samples (not daily SQL rows): `digest.ComputeDigest` /
`digest.ComputeWeightedDigest` (exact sort, nearest-lower-rank). Not t-digest.
The weighted path takes a `WeightFunc`; do not import `bhschedule` from `csv`
— pass a callback from the product or CLI.

---

## Import allow / deny

| Package | Backend processor | robne CLI | robne-operator |
|---------|-------------------|-----------|----------------|
| Core (`types`, `engine`, `digest`, `container`, entity `Recommend*`) | Yes | Yes | Yes |
| `bhschedule` (window evaluation only) | Prefer `internal/bhschedule` | Yes | Evaluation only; no SQL |
| `csv` | Yes (container `ForEachRow` / `ParseRows`; other-entity ingest parsers still duplicated) | Yes | **Never** |
| `pgrec` | Yes | Yes | **Never** |
| `pgdigest` | Yes (recommend-path `Read*` / writers) | Yes | **Never** |

SQL, cache, prune, and pending-marker stubs for business-hours **schedules**
stay in `internal/bhschedule`. `librobne/bhschedule` evaluates day-of-week,
local wall clock, overnight spans, and off-hours weight.

Processor container ROS parse is `librobne/csv.ForEachRow` (ingest stays the
product wrapper: grouping, BH weights, GPU/node accumulators, incremental
flush). CLI `ParseRows` collects from the same loop. Namespace / PVC / VM /
snapshot / cluster-quota ingest parsers are still duplicated until a later
cut.

All-hours container recommend SELECT is `pgdigest.Read` /
`ReadContainerDigests` (wrapper `loadDigestRows`). Business-hours list/detail
variants still have product helpers; folding those onto `pgdigest.Read*` is
[#476](https://github.com/pgarciaq/ros-ocp-backend/issues/476). Timeout, row cap,
and Prometheus stay in the processor wrapper.

---

## Three consumers

### ros-ocp-backend

Product wrappers in
[`internal/engine`](https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/internal/engine)
load PostgreSQL, call librobne, persist recs, and serve HTTP. Example:
[`recommend_all.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/recommend_all.go)
(`loadDigestRows` → `engine.RecommendWorkloads`). Kafka, Echo, Masu, and GORM
stay in the product.

### robne CLI

[`cmd/robne`](https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/cmd/robne)
parses files with `librobne/csv` (or reads this CLI’s digest tables with
`pgdigest.Read*`), calls the same `Recommend*` functions, and writes stdout or
CLI-owned PostgreSQL. User manual: [robne CLI](../features/robne-cli.md).

### robne-operator (planned, #138)

No separate operator repo yet. The contract is:

1. Turn PromQL / CRD samples into **daily** canonical digest rows (two clocks:
   scrape vs recommend — do not sort thousands of raw samples in the runner).
2. Call `Recommend*` with no pool.
3. Persist in **your** emit/store. Do not import `csv`, `pgrec`, or `pgdigest`.
4. Scale notes: [Local Mode scale estimates](../planned-features/librobne-scalability.md).

---

## Package index

Entry points only. Plugin math stays on [Recommendation Engines](recommendation-engines.md).
`go doc` in this tree: `go doc -C librobne <package>`.
HTML: [https://pgarciaq.github.io/ros-ocp-backend/pkg/](https://pgarciaq.github.io/ros-ocp-backend/pkg/)
(per-package pages such as
[engine](https://pgarciaq.github.io/ros-ocp-backend/pkg/engine/)).

| Package | Entry points |
|---------|----------------|
| `types` | `DigestRow`, `KeyedDigest`, `ContainerRec`, `RateCard`, idle, notifications |
| `digest` | `ComputeDigest`, `ComputeWeightedDigest` |
| `engine` | `RecommendWorkloads`, `DefaultEngineConfig` |
| `container` | `RecommendCPU`, `RecommendMemory`, `RecommendCPUAndMemory`, `ApplySavingsEstimates`, `ComputeRecommendedReplicas` |
| `savings` | Re-export of `ApplySavingsEstimates` (not a convert loop) |
| `namespace` | `RecommendNamespaces` |
| `node` | `RecommendNodes` |
| `gpu` | `RecommendGPU` / `RecommendGPUWithSettings`, `ComputeNodeTimeslicingRec`, embedded catalogs |
| `vm` | `RecommendVM`, `RecommendVMTimeSlicing` |
| `pvc` | `RecommendPVCs`, `ComputePVCRecommendation` |
| `quota` | `RecommendQuotas`, `ComputeQuotaRecommendation`, `RecommendClusterQuotas` |
| `snapshot` | `ClassifySnapshotInventory` |
| `fixedpoint` | Basis-point helpers |
| `bhschedule` | Window evaluation (not SQL) |
| `csv` | ROS CSV parse + in-memory daily aggregation (CLI; operator must not import) |
| `pgrec` | Rec upsert (CLI + processor; operator must not import) |
| `pgdigest` | Digest INSERT + `Read*` (CLI + processor; operator must not import) |

Tests: `go test -C librobne ./...` (see [Contributing](../contributing.md#running-tests)).
