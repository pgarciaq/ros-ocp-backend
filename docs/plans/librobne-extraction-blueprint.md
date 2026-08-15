# librobne extraction plan

**Status:** Draft for approval (2026-08-15) — **do not extract until this document is approved**  
**Tracking:** [GitHub #94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)  
**Branch:** `pgarciaq-rosocp-superpowers-phase17`  
**Baseline SHA:** [`841639f3`](https://github.com/pgarciaq/ros-ocp-backend/commit/841639f365079038fe60c5bb6127f9f08834eecf) (first commit on phase17)  
**Supersedes:** [Cut-1 blueprint](../archive/librobne-extraction-blueprint-cut1-2026-08.md) (rejected)  
**Will amend after approval:** [ADR-0303](../adr/0303-library-extraction-librobne.md)

librobne is a **statically linked Go engine**, not a network service and not a
plugin ABI. Consumers call it in-process. No HTTP, gRPC, Wasm, or `.so`.

---

## 0. Approval gate

Read this whole document. Approve (or amend) before any code moves.

After approval, work is **aggressive**: in-tree cleanup, then nested module, then
ros-ocp-backend imports librobne. Operator ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138))
and CLI ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)) consume the
library later; they are not in the first extract.

**Do not start** module creation, type converters, or import rewiring until
approval + baseline numbers are recorded (§8).

---

## 1. Why extract (and why the previous plan was wrong)

Three products need the **same** recommendation engine:

| Consumer | I/O | Needs engine? |
|----------|-----|----------------|
| **ros-ocp-backend** (this repo) | Kafka, S3, Echo, Masu HTTP, central PostgreSQL | Yes |
| **robne-operator** Local/Hybrid ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) | PromQL, CRD, local PostgreSQL, embedded API | Yes |
| **robne CLI** ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)) | NISE CSV / Prom JSON / optional PG COPY | Yes |

Importing all of ros-ocp-backend into an operator or CLI pulls Kafka, Echo,
Clowder, AWS SDK, Unleash, GORM. Copy-pasting `internal/engine/` into three
repos will drift.

**Rejected (Cut 1):** extract only inner `RecommendCPUAndMemory`-style functions;
leave grouping, term×engine loops, savings, and digest types behind “adapters”
(`model.X → librobne.X`). That duplicates the hot loop three times and copies
~280-byte `DigestRow`s (200k containers × 14 days ≈ 2.8M rows ≈ **750 MiB** extra
if copied once). That is the Kruize marshalling tax without a network.

**Rejected (old Cut 2 / Cut 3):** a shared `DigestProvider` / plugin registry that
pretends Prometheus, Kafka CSV, and NISE files are one I/O loop. Those sources
do not share a schema or a clock.

**This plan:** extract the **engine runner + canonical digest types + savings on a
deposited RateCard**. Each consumer fills `[]KeyedDigest` however it wants, then
calls the same runner. Persistence is an **emit callback**, not SQL inside
compute.

Drop the names Cut 1 / Cut 2 / Cut 3 / M1b. They mixed “who does I/O” with “who
owns the hot loop.”

---

## 2. Target architecture

```
Consumers (product I/O — not in librobne core)
┌─────────────────────┐  ┌─────────────────────┐  ┌──────────────┐
│ ros-ocp-backend     │  │ robne-operator      │  │ robne CLI    │
│ Kafka, Echo, Masu,  │  │ PromQL, CRD,        │  │ files, flags │
│ product plugins     │  │ local PG, API       │  │ optional PG  │
└──────────┬──────────┘  └──────────┬──────────┘  └──────┬───────┘
           │ scan / parse into librobne types            │
           │ emit callback writes recs                   │
           ▼                        ▼                    ▼
┌───────────────────────────────────────────────────────────────┐
│ librobne (in-process, one binary after static link)           │
│                                                               │
│  types     DigestRow, KeyedDigest, ContainerRec, RateCard, …  │
│  digest    ComputeDigest / weighted percentiles from samples  │
│  engine    group → window → terms×engines → idle → notif →    │
│            category → replica → emit(batch)                   │
│  savings   Apply*(recs, RateCard) — no HTTP                   │
│  compute   inner RecommendCPUAndMemory, MIG, PVC, … (tests)   │
│                                                               │
│  optional import (own deps, not required by operator Local):  │
│    csv/       NISE / operator CSV → samples → DigestRow       │
│    pgdigest/  central schema: SELECT/upsert via pgx           │
└───────────────────────────────────────────────────────────────┘
```

**Primary API (shape; names may tighten in P3):**

```go
type KeyedDigest struct {
    Key WorkloadKey // namespace, workload, workload_type, container_name
    Row DigestRow   // canonical in-memory digest — scan into this, never convert
}

type EmitContainer func(batch []ContainerRec) error // caller reuses backing array

func RecommendWorkloads(rows []KeyedDigest, cfg EngineConfig, emit EmitContainer) error
```

That **is** today’s `RecommendWorkloadsStreaming` `processContainer` loop minus
`*pgxpool.Pool`. Inner `RecommendCPUAndMemory` stays exported for unit tests.
Production consumers must not reimplement the term×engine loop.

`[]DigestRow` / `[]KeyedDigest` are slice headers (no copy of the backing array).
Windows are subslices (`digests[lo:hi]`), as they are today.

---

## 3. Boundary: in vs out

### In librobne (required)

- Canonical **digest and recommendation value types** (`DigestRow`, `ContainerRec`,
  node/VM/GPU/PVC/quota/snapshot equivalents). **One type.** Consumers scan or
  parse **directly into these types**.
- Digest **aggregation from in-memory samples** (`ComputeDigest`, weighted
  percentiles, BH weights). This is how the native engine stays fast: recommend
  over daily rows, not raw hourly points.
- Engine **runner**: group-by-entity, window, terms × engines, idle, notifications,
  category, replica helpers, batch emit.
- **Savings math** on a deposited `RateCard` (empty / Tier A / Tier A+B — §4).
- Embedded read-only catalogs (`go:embed`: GPU YAML, VM instance types, …).
- Notification **integer codes** (not the HTTP catalog).
- Domain enablement bitmask (`EngineConfig.Enabled`), not Echo/CSV plugin traits.

### Optional librobne packages (separate import)

| Package | Deps | Who uses it |
|---------|------|-------------|
| `csv` | stdlib | CLI; anyone ingesting NISE/operator CSV |
| `pgdigest` | pgx | Central + CLI COPY/SELECT against **central** digest schema |

Operator Local Mode **does not** import `pgdigest`. Its on-disk summaries are a
different, smaller schema; it maps PromQL + local rows → `DigestRow` itself.

### Never in librobne

| Keep out | Why |
|----------|-----|
| Echo, Kafka, S3, Clowder, Unleash, AWS SDK | Product / SaaS |
| GORM + `internal/model` tags | Persistence ORM ≠ compute types |
| `costdata` HTTP client (`effective_rates`) | Fetch vs math; Koku is one mapper into `RateCard` |
| `CSVIngestor` / `APIProvider` / `RetentionProvider` | ros-ocp-backend **product plugins** |
| `*pgxpool.Pool` on `Recommend*` | Hides I/O inside compute |
| Unified `DigestProvider` for Prom+PG+CSV | Fits none; invites copies |
| HTTP / gRPC / Wasm / `.so` | Repeats Kruize / violates ADR-0099 |

### Product plugins stay in each binary

Today’s `internal/plugins/container` owns **CSV ingest, retention, term defaults**.
It does **not** run the recommender. `RecommendWorkloadsStreaming` does.

| Name | What it is | Where it lives |
|------|------------|----------------|
| **ros-ocp-backend plugins** | CSV types, Echo routes, retention, `ROS_ENABLED_PLUGINS` | `internal/plugins/*` |
| **librobne engines** | Compute domains (container, node, gpu, …) | librobne |
| **Local Mode CRD `spec.engine.plugins.*`** | Enable/disable **engines** | Maps to `EngineConfig.Enabled`, not to `CSVIngestor` |

Moving `internal/plugins/*` into librobne would drag Echo/Kafka into the operator
and CLI. Do not do it.

CSV parse + DB insert of **digest tables** stay split from recommendation
generation (already true at runtime: `ParseAndDigestCSV` then
`runManifestRecommendations`). The library makes that split the public contract:
ingest fills digests; `RecommendWorkloads` never sees a CSV or a pool.

---

## 4. RateCard (empty / Tier A / Tier A+B)

librobne **never fetches** rates. Any FinOps tool (Koku, OpenCost, a YAML price
list, CLI flags, operator) **deposits** a card, then calls savings.

Do not clone Koku’s HTTP JSON as the library API. Do not put `float64` money or
an IEEE decimal type on `RateCard`. Integer scales, converted at boundaries:

| Layer | Unit | `$0.007` / core-hour |
|-------|------|----------------------|
| **Masu `effective_rates` + RateCard** | **micro-cents** (`int64`, `$ × 100_000_000`) **per core-hour / GiB-hour** (rates) or as **totals** (namespace spend) | `700000` µ¢ / core-hour |
| **`Apply*`** (hot path) | same micro-cents, **per millicore-hour** (`÷ 1000`) — keep [`savings_int.go`](../../internal/engine/core/savings_int.go) | `700` µ¢ / millicore-hour |
| **DB + API** | **cents** + [`MoneyAmount`](../../internal/money/format.go) | `"5.21"`, `units` from `RateCard.Currency` |

Masu wire contract: [#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461) (integer micro-cents, not `float()`, not dollar strings). Quantity on rates stays **core-hour / GiB-hour**; ROS owns the millicore split. Hours on the card are **milli-hours** (`hours × 1000`). Markup, if deposited, is **basis points**.

**Why not millidollars / microdollars on the card:** millidollars truncates `$0.0015`. Microdollars would be a third scale next to Masu micro-cents and `Apply*` micro-cents. One money scale from HTTP → RateCard → `savings_int.go`.

**Why keep `÷ 1000` only in `Apply*`:** ADR-0291. Integer cents as a *per-millicore-hour* rate round to zero. Round once at the edge: `MicroCentsToCents`.

**Why not IEEE decimal64 / shopspring in librobne:** ADR-0295. Hot path is `int64` multiply.

`currency` is ISO 4217 **display metadata**. All numbers on the card are already
in that currency. **Empty / nil card: do not default `Currency` to `"USD"`** —
no savings, no ISO code, existing “no cost data” notification. FX (user
preferred currency vs cost-model currency) stays in the product API layer
([ADR-0327](../adr/0327-api-time-currency-conversion-over-storage.md)), not in
librobne. Display fallback is `user preference → cost-model currency → omit`,
never invent USD inside compute.

```text
RateCard
  currency: "EUR"                 // cost-model unit; empty if no card
  hours_per_month: 744            // or librobne derives from calendar month
  markup_basis_points: 1000       // optional; 10% = 1000

  // Tier A — unit prices (CLI, operator, YAML, Koku mapper)
  // integer micro-cents per core-hour / GiB-hour / GiB-month / GPU-month
  cpu_microcents_per_core_hour
  mem_microcents_per_gib_hour
  gpu_microcents_per_gpu_hour          // optional
  storage_microcents_per_gib_month     // optional

  // Tier B — observed spend (Koku-shaped; same money scale)
  namespaces[name]:
    cost_model_cpu_microcents          // totals, not rates
    cost_model_mem_microcents
    infra_microcents
    distributed_microcents
    cpu_request_milli_hours, mem_request_milli_hours
    distribution: cpu | memory
```

| Card | Behavior |
|------|----------|
| **Empty / nil** | Skip savings; “no cost data” notification. Sizing recs still compute. `Currency` unset. |
| **Tier A only** | Unit prices × millicore/KiB deltas (idle = full current request). |
| **Tier A + B** | Same math as today: effective rate = spend / hours; infra+distributed by `distribution`. |

ros-ocp-backend’s `internal/costdata` HTTP client becomes **one mapper**
(`ClusterCostData` → `RateCard`). It does not belong in librobne. After #461
the mapper copies `int64` micro-cents and milli-hours. Until Masu ships, a
compat decoder may quantize legacy JSON floats **once** at the mapper — never
on `RateCard`, never in `Apply*`.

---

## 5. Digests are in librobne

Daily digests **are** the native engine. They are not “kept out.”

| In librobne | Out of compute core |
|-------------|---------------------|
| `DigestRow` / `KeyedDigest` layout | `*pgxpool.Pool`, SQL, GORM |
| Building a digest from samples | Kafka, S3 |
| Recommending from `[]KeyedDigest` | Operator PromQL (fills `DigestRow`) |
| Optional CSV → samples → `DigestRow` | Optional `INSERT` into `daily_container_digests` |

Operator Local Mode still uses `DigestRow`. Prometheus `quantile_over_time()` is
another way to **fill** the struct. Compact on-disk summaries (~70 bytes) are a
**storage** choice; during an engine cycle, RAM holds the fields compute needs.

Namespace and snapshot paths that still interleave `pool.Query` with compute must
become **load-then-compute** (same as containers / [#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263))
**before** the module split. That releases the connection during CPU work. It is
not a performance regression.

---

## 6. PostgreSQL optimizations this split must not lose

The official baseline **includes PostgreSQL**. A no-DB microbench is only a
canary for extra copies.

Must still be true after extraction (in ros-ocp-backend wrappers and/or
`librobne/pgdigest`):

1. Digests in PG, not raw samples / JSONB — recommend reads `daily_*_digests` (RANGE partitions).
2. **Read once, compute N terms in Go** — one digest query per cluster; no per-term SQL rescan.
3. Covering index `idx_daily_container_digests_recommend` on
   `(org_id, cluster_uuid, schedule_type, namespace, workload, workload_type, container_name, bucket_date)`.
4. **`loadDigestRows` then drop the connection** ([ADR-0171](../adr/0171-streaming-recommendation-batches.md), [#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)) — no cursor held across writes.
5. Ingest statement timeout 120s via `SET LOCAL` ([ADR-0092](../adr/0092-ingest-statement-timeout-120s.md)).
6. Chunked `pgx.Batch`, cap **2000** ([ADR-0093](../adr/0093-chunked-pgx-batches-500.md)).
7. Emit every **500** containers (`streamBatchSize`); history/quality/savings per batch.
8. Digest upserts: `INSERT … ON CONFLICT DO UPDATE` (COPY cannot express conflict + `RETURNING` for `org_container_keys`).
9. **One connection pool** ([ADR-0128](../adr/0128-unify-gorm-pgxpool-stdlib.md)) — do not split GORM + pgx pools again.
10. Go-created monthly partitions ([ADR-0059](../adr/0059-auto-create-partitions-in-go.md)); retention by partition drop.
11. `ROS_MAX_DIGEST_ROWS_PER_CLUSTER` — skip the cluster rather than truncate ([#290](https://github.com/pgarciaq/ros-ocp-backend/issues/290)).
12. `container_id` index seek on detail — no composite-key scan fallback for new rows.

**Forbidden on the recommend path (review reject):**

- Field-by-field `engine.DigestRow` → `librobne.DigestRow` convert loops
- `interface{}` / `map[string]any` per row
- JSON encode/decode in compute
- Allocating a new `[]DigestRow` to “adapt” windows (use subslices)
- Holding a PG connection across CPU work
- Row-by-row `INSERT` or unbounded pgx batches
- GORM `FindInBatches` for digest load

Go package boundary after static link is **zero** runtime cost. Extra **copies**
are not.

---

## 7. Proposed defaults (approve-or-amend)

| Topic | Default if you approve this doc as-is |
|-------|----------------------------------------|
| Module home | Nested module `librobne/` in this repo until the API is stable. Optional later split to `github.com/pgarciaq/librobne`. |
| Import path (bootstrap) | `github.com/pgarciaq/ros-ocp-backend/librobne` with a `replace` in the parent `go.mod` |
| Core `go.mod` deps | Stdlib + `gopkg.in/yaml.v3` only if catalogs need it; **no** pgx/Echo/GORM in core |
| Namespace + business hours | **One** runner; `schedule_type` is a field on rows. Two orchestrator entry points, not two extractions. |
| VM types | `vm.Digest` / `vm.Recommendation` (no `model.*`) |
| Currency | `RateCard.Currency` = cost-model ISO 4217 when a card exists; **unset** on empty card (not `"USD"`). Money is **micro-cents** on the card and in `Apply*` (per millicore-hour after `÷1000`); persist **cents**. FX is API-time (ADR-0327). |
| #94 / public site / ADR-0303 | Update **after** this plan is approved, as a docs pass in P0.5 |

---

## 8. Baseline procedure

### 8.1 What is frozen

| Item | Value |
|------|--------|
| Git SHA | `841639f365079038fe60c5bb6127f9f08834eecf` |
| First phase17 commit | `docs: Point live docs to phase17 and add branch bump checklist` |
| Engine-equivalent parent | `e7d2a4dbc2ddb05daffec7dc78599eb68ae92551` (phase16 tip; engine/SQL identical) |
| Artifact dir | `docs/performance/librobne-baseline-841639f3/` (create when recording) |
| PG | PostgreSQL **16** via testcontainers (`cmd/bench`) |

`841639f3` is docs-only vs `e7d2a4db`. Measuring either SHA for engine/PG is the
same. Record **`841639f3`** as the named baseline.

Worktree at current `phase17` HEAD is also engine-identical until P1 starts
(only later docs). **Still check out `841639f3` when recording** so the SHA in
the artifact matches the plan.

### 8.2 Prerequisites

```bash
cd /path/to/ros-ocp-backend
git fetch pgarciaq
git checkout 841639f3

# Docker or Podman — testcontainers needs a runtime
# Go version: see go.mod (1.26.x)

go install golang.org/x/perf/cmd/benchstat@latest
mkdir -p docs/performance/librobne-baseline-841639f3
```

Hardware: a developer machine is enough (same class as
[`docs/native-engine-performance.md`](../native-engine-performance.md)). A live
SNO cluster is **not** required to lock the library split. Optional later: UXSNO
for PromQL + real disks (operator), not this extract.

### 8.3 Contract bench — PostgreSQL full pipeline

Existing harness: [`cmd/bench/main.go`](../../cmd/bench/main.go). It starts PG 16,
seeds `daily_container_digests`, runs `RecommendAllWorkloads` (load+compute into
one slice), then `WriteRecommendationsAndRefreshOrg`, then list/detail.

**Caveat:** `RecommendAllWorkloads` is the test wrapper (all recs in memory).
Production is `RecommendWorkloadsStreaming` (emit every 500). Keep `cmd/bench`
for continuity with published 10k/100k numbers. After approval, P0.5 adds a
streaming variant to the same binary so write-path optimizations are measured
the way production runs. Until that exists, `Write (ms)` on this harness is still
the PG write contract.

```bash
# From repo root at 841639f3. 100k takes several minutes.
go run ./cmd/bench/ 1000,10000,100000 \
  | tee docs/performance/librobne-baseline-841639f3/cmd-bench.txt
```

Record the summary table (Seed / Recommend / Write / List p50 / List p99 / Detail /
Peak RSS). Historical published ballpark (laptop, worst-case testcontainers):

| Containers | Recommend (ms) | Write (ms) | Peak RSS (MB) |
|------------|----------------|------------|---------------|
| 10,000     | ~857           | ~1,617     | ~176          |
| 100,000    | ~10,485        | ~19,171    | ~1,790        |

**Re-measure.** Do not copy these numbers as the gate; they are orientation.
The file from `tee` is the gate.

Also store:

```bash
{
  echo "SHA=$(git rev-parse HEAD)"
  echo "date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go=$(go version)"
  echo "uname=$(uname -srm)"
  nproc
} > docs/performance/librobne-baseline-841639f3/environment.txt
```

### 8.4 Canary benches — no extra copies in compute/savings

Existing tests (PG used only where the test already opens a pool for thresholds):

```bash
go test -bench='BenchmarkSavingsCalculation_1000Containers$|BenchmarkNodeSavings_100Nodes$|BenchmarkDualRecommendation_Overhead$|BenchmarkThresholdResolution_SingleOrg$' \
  -benchmem -count=10 -timeout 30m \
  ./internal/engine/ \
  | tee docs/performance/librobne-baseline-841639f3/go-test-bench.txt
```

After approval, P0.5 adds `BenchmarkRecommendWorkloads_ComputeOnly` (synthetic
`[]KeyedDigest`, emit to `io.Discard`, **no pool**) at 1k / 10k / 100k × 14 days.
First run of that new bench is recorded on **pre-P1 HEAD** (engine still =
`841639f3`) and checked in beside the SHA files. That becomes the copy-regression
tripwire.

### 8.5 Compare after each extract phase

```bash
# After a phase, on the working tree:
go run ./cmd/bench/ 1000,10000,100000 \
  | tee /tmp/librobne-after-cmd-bench.txt

# Compare Recommend / Write / RSS by hand from the summary tables.
# For go test benches:
go test -bench='BenchmarkSavingsCalculation_1000Containers$|BenchmarkNodeSavings_100Nodes$|BenchmarkDualRecommendation_Overhead$|BenchmarkThresholdResolution_SingleOrg$' \
  -benchmem -count=10 -timeout 30m ./internal/engine/ \
  | tee /tmp/librobne-after-go-test-bench.txt

benchstat docs/performance/librobne-baseline-841639f3/go-test-bench.txt \
  /tmp/librobne-after-go-test-bench.txt
```

### 8.6 Gates (fail the PR / revert the phase)

Tune only if the frozen `841639f3` numbers show the harness itself is noisier;
do not loosen to hide copies.

| Signal | Gate vs `841639f3` |
|--------|---------------------|
| Compute-only canary (once added) | ≤ **2%** ns/op and allocs/op (`benchstat`) |
| `cmd/bench` **Recommend** (load+compute) | ≤ **5%** wall time at 10k and 100k |
| `cmd/bench` **Write** | ≤ **5%** wall time at 10k and 100k |
| `cmd/bench` **Peak RSS** | ≤ **10%** (a second `[]DigestRow` copy fails this) |
| List p50/p99 | No worse than noise; extract must not change list SQL |

If a phase fails: **fix or revert that phase**. Do not “optimize later.”

---

## 9. Implementation sequence (aggressive after approval)

Do **in ros-ocp-backend first**. Nested module before a second GitHub repo.

| Phase | What | Extract? |
|-------|------|----------|
| **P0** | This document approved | No |
| **P0.5** | Record §8 baseline at `841639f3`; add compute-only + streaming `cmd/bench` path; docs pass (#94, ADR-0303 amendment draft, short site note) | Harness only |
| **P1** | Canonical types in place: VM off `model.*`; GPU idle/thresholds as values; `RateCard` at engine boundary; pgx flush **out** of `core` types file | No new module |
| **P2** | Namespace + snapshot **load-then-compute** (container pattern) | No new module |
| **P3** | `RecommendWorkloads(rows, cfg, emit)` with **no pool**; wrappers: query → runner → persist. Plugins unchanged | No new module |
| **P4** | Nested `librobne/` module; **move** types+digest+runner+savings; `replace` in parent; **no converters** | Yes |
| **P5** | Optional `csv` + `pgdigest` when CLI/central need shared I/O | Optional packages |
| **P6** | CLI / operator import librobne (separate features #99 / #138) | Consumers |

P1–P3 must be behavior-preserving and gate-green. P4 is an import-path move of
already-clean packages. That is how the extract stays fast.

**Stop line:** nothing after P0 until explicit approval. After approval, P0.5 is
mandatory before P1.

---

## 10. Module layout (P4)

```
librobne/                    # nested module
├── go.mod
├── types/                   # DigestRow, recs, EngineConfig, RateCard
├── digest/                  # ComputeDigest, weighted samples (no SQL)
├── engine/                  # RecommendWorkloads + per-domain runners
├── savings/                 # Apply* (RateCard)
├── container/ node/ vm/ gpu/ pvc/ quota/ snapshot/
├── csv/                     # optional; own import
├── pgdigest/                # optional; pgx; central schema only
└── testdata/
```

ros-ocp-backend after P4: `internal/engine` is wrappers (load, settings from DB,
Masu → RateCard, emit → pgx batch, history/quality). Compute lives in librobne.

---

## 11. Per-entity work (in P1–P3, before P4)

| Entity | Blocking work |
|--------|----------------|
| Container | Runner already close; strip pool from loop; RateCard for savings |
| Node | Same; types already mostly engine-local |
| GPU MIG / time-slicing | Pass idle + threshold structs; delete library-side `LoadGPUIdleConfig` |
| VM | Replace `model.DailyVMDigest` / `model.VMRecommendation` **in place** (no dual types) |
| PVC | Export `ComputePVCRecommendation`; split from DB `RecommendPVCs` |
| Quota / cluster quota | Split DB runner; currency from RateCard / cfg, not `money.DefaultCurrency` inside compute |
| Namespace | Load-then-compute; one rollup for all-hours and business-hours |
| Snapshot | Export inventory row + group index; classify over slices |

No algorithm changes during the move. Golden tests: same digest fixture → same
rec fields (CPU/mem/GPU/…) before vs after each phase.

---

## 12. Testing

| Layer | What |
|-------|------|
| librobne unit | Existing pure tests move with code; `go test -short` without PostgreSQL |
| Compatibility | Golden rec JSON/fields per entity |
| ros-ocp-backend integration | Existing testcontainers tests stay; they validate load/persist/plugins |
| Performance | §8 gates on every P1–P4 PR that touches the runner or types |
| Behavior | Full `go test` / tox-equivalent CI green; no API contract change |

---

## 13. Definition of done (extract, not operator/CLI)

1. Nested `librobne` module exists; core has no Echo/Kafka/GORM; pgx only in optional `pgdigest`.
2. Engine runner + digest types + RateCard savings live in librobne.
3. ros-ocp-backend scans into librobne types and emits via existing chunked pgx batches — **zero convert loops**.
4. Product plugins still in ros-ocp-backend (ingest/API/retention).
5. No user-facing API change.
6. §8 gates pass vs `841639f3`.
7. ADR-0303 amended to this design (Accepted); #94 and a short public note updated.
8. CHANGELOG entry.

**Not** DoD for this extract: robne-operator wiring, robne CLI binary, separate
GitHub repo, third-party HTTP API.

---

## 14. Explicitly deferred

- Creating `librobne/` or moving packages (until approval + P0.5)
- robne-operator / Local Mode ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138))
- robne CLI ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99))
- Repo rebrand ([#421](https://github.com/pgarciaq/ros-ocp-backend/issues/421))
- Allocation micro-optimizations beyond “no regression”
- Shared I/O interface covering Prometheus and PostgreSQL
- Masu `effective_rates` integer micro-cents + milli-hours + markup basis points ([#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461)) — mapper in ros-ocp-backend; not a librobne extract blocker
- ROS `money.DefaultCurrency` / `user_currency` USD fallback when the cost model is not USD (product API; use cost-model currency instead)

---

## 15. References

- Rejected Cut-1 plan: [archive](../archive/librobne-extraction-blueprint-cut1-2026-08.md)
- [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94) — extract librobne
- [#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138) / [Local Mode](../../docs-site/planned-features/local-mode.md)
- [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99) / [robne CLI](../../docs-site/planned-features/robne-cli.md)
- [ADR-0303](../adr/0303-library-extraction-librobne.md) (to amend)
- [ADR-0001](../adr/0001-native-engine-over-kruize.md), [ADR-0099](../adr/0099-compile-time-in-process-plugins.md)
- [Native engine performance](../native-engine-performance.md), [`cmd/bench`](../../cmd/bench/main.go)
- [librobne scalability](../../docs-site/planned-features/librobne-scalability.md)
- [#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461) — Koku `effective_rates` integer micro-cents (not `float()`)
- [ADR-0291](../adr/0291-integer-micro-cents-savings-computation.md), [ADR-0295](../adr/0295-integer-first-architecture.md), [ADR-0327](../adr/0327-api-time-currency-conversion-over-storage.md)
