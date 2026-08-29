# ADR-0303: Library Extraction of the Native Engine (librobne)

## Status

Accepted

## Phase

P4+ (2026-08-15). Nested module `librobne/` ships container + namespace, snapshot, node, GPU, VM, PVC, and quota compute. Product wrappers still load PostgreSQL and persist. P5/P6 are other issues. Tracker: [GitHub #94](https://github.com/pgarciaq/ros-ocp-backend/issues/94). Plan: [librobne-extraction-blueprint.md](../plans/librobne-extraction-blueprint.md).

## Context

Three products need the **same** recommendation engine:

| Consumer | I/O |
|----------|-----|
| ros-ocp-backend | Kafka, S3, Echo, Masu HTTP, central PostgreSQL |
| robne-operator Local/Hybrid ([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) | PromQL, CRD, local PostgreSQL |
| robne CLI ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)) | NISE CSV / Prometheus JSON / optional PG |

Importing all of ros-ocp-backend pulls Kafka, GORM, Echo, Clowder, AWS SDK. Copy-pasting `internal/engine/` into three repos will drift.

The engine already does the expensive work in-process: load daily `DigestRow`s once, then recommend. The extract must not add convert loops (`model.X → libX`) or a network hop. A second copy of `[]DigestRow` at 200k containers × 14 days is on the order of **750 MiB**.

## Decision

Extract a **statically linked Go engine** (`librobne`), not a service and not a plugin ABI. No HTTP, gRPC, Wasm, or `.so`.

**What ships in #94 (P4):** nested module `github.com/redhatinsights/ros-ocp-backend/librobne` (`replace => ./librobne`) containing:

- Canonical **container** digest and recommendation types (`DigestRow`, `KeyedDigest`, `ContainerRec`). Consumers scan/parse **directly** into these types. **Zero converters.**
- Digest aggregation from in-memory samples (`ComputeDigest`). Exact sort + nearest-lower-rank percentiles. **Not t-digest.** Weighted path takes a `WeightFunc`; `internal/bhschedule` stays in the product.
- Engine **runner**: group → window → terms × engines → idle → notifications → category → replica → `emit(batch)`.
- **`Apply*`** on a deposited `RateCard` as a **separate** call after emit (same as today). Forgetting it is missing dollars, not wrong millicores. Empty card: **no** default `"USD"`. Money is integer micro-cents. **Observed** request hours on the card are milli-hours; **projection** hours are calendar hours on the `Apply*` argument (`HoursInMonth`), not a RateCard field and not derived inside librobne.

**What stays out of librobne core:** PostgreSQL/pgx, Echo, Kafka, GORM, S3, Clowder, CSV ingest, PromQL. Persistence is the emit callback. Optional `csv` / `pgdigest` are **P5**, not #94 DoD. One binary per product: the operator must not import `csv`.

**P4 was container-first.** Node, VM, GPU, PVC, quota, namespace, and snapshot moved in **P4+** once each `Recommend*` no longer took a `*pgxpool.Pool`.

**Module path** stays under `github.com/redhatinsights/ros-ocp-backend/librobne` until repo rebrand (#421). Do not use `github.com/pgarciaq/...` as the nested import path.

**Performance gate** vs frozen SHA [`841639f3`](https://github.com/pgarciaq/ros-ocp-backend/commit/841639f365079038fe60c5bb6127f9f08834eecf): compute-only ≤2% ns/op and allocs/op; `cmd/bench` Recommend/Write ≤5% at 10k; Peak RSS ≤10% at 10k. Failed gate **reverts the phase**. 100k OOM on a small laptop is not a fail.

## Why not the previous “Cut 1”

Extracting only inner `RecommendCPUAndMemory`-style functions and adapting `model.X → librobne.X` duplicates the hot loop three times and copies digest rows. That is the Kruize marshalling tax without HTTP.

## Why not a unified `DigestProvider`

Prometheus (15s scrape → PromQL `quantile_over_time` into daily `DigestRow`s), Kafka CSV, and NISE files do not share a schema or a clock. Two clocks: scrape vs recommend. librobne recommends over daily rows. Local Mode must not sort 5760 raw samples in the runner.

## Why not millidollars on RateCard / default USD

One money scale from Masu HTTP → RateCard → `Apply*` (micro-cents, per millicore-hour after `÷1000`). Empty card must not invent `"USD"` (FX is API-time, ADR-0327).

## Consequences

### Positive

- Operator and CLI import the engine without Kafka/GORM/Echo.
- Canonical types: scan once, no copy on the hot path.
- Nested module first: one repo, `replace`, no premature split.

### Negative

- P4 did not move all nine entities. P4+ completed those moves; P5/P6 remain other issues.
- `Apply*` remains a product wrapper responsibility. Tests must keep “nil savings until Apply*”.
- Nested `replace` until a possible later `github.com/pgarciaq/librobne` split.

### Neutral

- No user-facing API change. No IQE plugin change planned for the extract.
- After static link, the module boundary adds **zero** RSS if we do not copy.

## Alternatives considered

### Keep engine in ros-ocp-backend, vendor into the operator

Rejected: copies drift.

### HTTP / gRPC / Wasm / `.so`

Rejected: marshalling tax and/or operational surface the operator and CLI do not need.

### Cut 1 (inner functions + converters)

Rejected: see above.

### `DigestProvider` / plugin registry for Prom + Kafka + NISE

Rejected: see above.

### t-digest at Local Mode 15s scrape

Rejected: two clocks; PromQL fills `DigestRow`. TimescaleDB/t-digest was already rejected for the native engine.

### PostgreSQL inside librobne

Rejected: load-then-compute stays in the product (#263 / ADR-0171). Core has no pgx.

## Work sequence (do not skip P0.5)

| Phase | What |
|-------|------|
| P0.5 | Baseline at `841639f3`; this ADR; streaming `cmd/bench`; compute-only canary |
| P1a | In-tree type/pool hygiene, bit-identical |
| P1b | RateCard at container savings; golden ±1 cent |
| P3 | `RecommendWorkloads(rows, cfg, emit)` with no pool |
| P4 | Nested module; **move** container types + digest + runner + `Apply*` |
| P4b | Namespace/snapshot load-then-compute (formerly P2) |
| P4+ | Move remaining entities into `librobne/` (done 2026-08-15) |
| P5 / P6 | Optional I/O packages (`csv`/`pgdigest`); CLI / operator |

Do **not** create `librobne/` before P4. Do **not** start P1a until P0.5 artifacts exist under `docs/performance/librobne-baseline-841639f3/`.

## References

- [librobne extraction blueprint](../plans/librobne-extraction-blueprint.md) — canonical plan
- [Cut-1 blueprint (rejected)](../archive/librobne-extraction-blueprint-cut1-2026-08.md)
- [ADR-0001](0001-native-engine-over-kruize.md) — native engine over Kruize
- [ADR-0099](0099-compile-time-in-process-plugins.md) — in-process plugins
- [ADR-0171](0171-streaming-recommendation-batches.md) — streaming batches / load-then-compute (#263)
- [ADR-0291](0291-integer-micro-cents-savings-computation.md) — integer micro-cents
- [ADR-0327](0327-api-time-currency-conversion-over-storage.md) — FX at API time
- [Local Mode](../features/local-mode.md)
- [librobne scalability (planned)](../../docs-site/planned-features/librobne-scalability.md)
- How to call it (not this ADR): [Integrating librobne](../../docs-site/architecture/librobne.md) — public site https://pgarciaq.github.io/ros-ocp-backend/architecture/librobne/ ; HTML API browse https://pgarciaq.github.io/ros-ocp-backend/pkg/
- Baseline artifacts: `docs/performance/librobne-baseline-841639f3/`
