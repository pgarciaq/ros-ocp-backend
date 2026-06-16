# ADR-0296: Store recommendation explanation factors as typed columns

## Status

Accepted

## Phase

13 (explainability)

## Context

The native Go engine produces recommendations for containers (CPU/memory), namespaces,
nodes, PVCs, GPUs, VMs, namespace quotas, cluster resource quotas, and volume
snapshots. Each engine computes rich intermediate values — decay-weighted percentiles,
adaptive margins, OOM bumps, growth slopes, utilization ratios, classification
trees, snapshot age thresholds — but persists only the final recommendation numbers. Users see *what* was recommended without
understanding *why*.

This gap shows up in support tickets and UI reviews: a container with a 2× memory
request increase looks arbitrary unless the user can see that OOM events triggered
a logarithmic bump on top of a P95-based baseline. The engine already has every
factor in memory at write time; it simply discards them after upserting the
recommendation row.

Prior art in this codebase rejected JSONB blobs for recommendation payloads
([ADR-0048](0048-relational-columns-not-jsonb-blobs.md)) and clarified when
normalized columns vs JSONB are appropriate
([ADR-0281](0281-jsonb-vs-normalized-columns-when-each-appropriate.md)).
Explanation factors are query-facing metadata with stable semantics — they belong
in typed columns, not schemaless documents.

## Decision

Add **typed, nullable columns** with the **`expl_` prefix** to each recommendation
table (live and history) to capture the driving factors computed at recommendation
time. Explanation fields are **embedded directly in the engine's result structs**
(`CPURec`, `MemoryRec`, `NodeRec`, `GPURec`, etc.) and persisted from those structs
— not copied into a separate `Explanation` type. This prevents drift when algorithms
change because the same struct that produces the recommendation also carries its
driving factors.

Each `Recommend*` function populates explanation fields on the rec struct;
persistence maps rec struct fields → `expl_*` columns; API layers serialize these
as a nested `explanation` object when the client requests it. API uses
always-SELECT + conditional-serialize: GORM fetches all fields; the response
serializer includes explanation factors only when `?include=explanation` is
requested.

Scope covers all native-engine recommendation types:

| Resource | Table |
|----------|-------|
| Container CPU/memory | `recommendation_sets` |
| Container GPU (MIG/time-slicing signals) | `recommendation_sets` (where `has_gpu = true`) |
| Namespace CPU/memory | `namespace_recommendation_sets` |
| Node CPU/memory | `node_recommendations` |
| PVC/storage | `pvc_recommendation_sets` |
| Namespace quota | `quota_recommendation_sets` |
| Cluster resource quota | `cluster_quota_recommendation_sets` |
| VM | `vm_recommendations` |
| Snapshot | `snapshot_recommendation_sets` |
| Node GPU time-slicing | `node_gpu_timeslicing_recommendations` (Phase 0 prerequisite) |

History tables (`recommendation_history`, `vm_recommendation_history`,
`quota_recommendation_history`, and related) receive the same explanation columns.
When a recommendation is archived to history, its explanation is archived with it.

**Confidence:** The existing `confidence_level` column (`dataDays / windowDays`)
already exists on recommendation tables. The API includes it in the `explanation`
response from that column — no new `expl_confidence` column.

**Existing columns:** Where a factor is already stored (e.g. PVC `usage_ratio`,
`growth_bytes_per_day`), the API assembles the explanation from the existing column
rather than duplicating it as `expl_*`.

**GPU persistence:** Container GPU explanation factors are written at classification
time. The detail endpoint reads persisted columns; digest recompute is a fallback
only for NULL rows during the backfill window.

**Node GPU time-slicing:** Persisting node GPU time-slicing recommendations to
`node_gpu_timeslicing_recommendations` (moving compute-at-read to compute-at-ingest)
is a **prerequisite for Phase 2c**. Explanation columns for that type are added
after the table exists.

Explanation columns are:

- Written atomically with the recommendation row during ingestion
- Exposed on **detail** endpoints via the **`include` query parameter** (OpenAPI:
  comma-separated list; `explanation` only in v1; e.g. `?include=explanation`,
  future `?include=explanation,savings_detail`) — opt-in, not included by default
  — avoids bloating responses for consumers who do not need explainability data;
  not on slim list DTOs per [ADR-0294](0294-slim-list-contract.md))
- Integer-scaled where the engine already uses integer arithmetic ([ADR-0295](0295-integer-first-architecture.md))
- Nullable — older rows and partial failures leave explanation fields empty until
  backfill

## Alternatives Considered

### JSONB `explanation` blob per row

Flexible schema, one migration. Rejected: loses type safety in Go, prevents SQL
debugging (`SELECT … WHERE explanation->>'oom_bump_applied' = 'true'`), and
conflicts with [ADR-0048](0048-relational-columns-not-jsonb-blobs.md). The set of
explanation factors is stable and engine-defined — not user-extensible metadata.

### Recompute explanations at API read time from digests

Avoids schema changes. Rejected as the primary approach: duplicates engine logic
in the API layer, adds latency to detail endpoints, and produces explanations
that may diverge from the values that actually drove the persisted recommendation
(threshold changes, term window drift, stale digest pruning). GPU detail currently
uses this pattern; this ADR replaces it with write-time persistence plus a
temporary NULL fallback during backfill only.

### Separate `recommendation_explanations` side table (EAV)

Normalized 1:1 extension table per resource type. Rejected: doubles join cost on
detail queries, splits the write path, and adds indirection without benefit when
column count per table is bounded (~10–20 factors).

### Separate `Explanation` struct copied at write time

Engine returns `(Rec, Explanation)` and copies values into a parallel struct.
Rejected: the copy step drifts from the engine when algorithms change. Embedding
explanation fields on the rec struct keeps a single source of truth.

### Emit explanations only in logs / internal debug endpoints

Useful for engineers, invisible to users. Rejected as the sole approach — the
primary goal is user-facing explainability in the UI.

### Always include explanation on detail responses

Simpler client code. Rejected: detail responses grow significantly; most consumers
(list views, automation, savings aggregations) do not need explainability data.
Opt-in via `?include=explanation` keeps the default payload slim.

## Rationale

- **Type safety:** Go rec structs map 1:1 to columns; compile-time checks on read/write.
- **No drift:** Embedding factors on rec structs guarantees explanation fields stay
  in sync with the algorithm that produces recommendations.
- **Debuggability:** Support engineers can `SELECT` explanation columns alongside
  recommendation values without parsing JSON.
- **Indexability:** Future filters (e.g. "show recs where OOM bump applied") use
  standard B-tree indexes, not GIN on JSONB paths.
- **Consistency:** Aligns with the project's anti-JSONB philosophy for
  recommendation data ([ADR-0048](0048-relational-columns-not-jsonb-blobs.md),
  [ADR-0281](0281-jsonb-vs-normalized-columns-when-each-appropriate.md)).
- **Stable schema:** Explanation factors change infrequently (new engine
  algorithm → new columns in a migration every ~6 months is acceptable).
- **Write-time capture:** Guarantees the explanation matches the recommendation
  that was actually stored.
- **Opt-in API:** Consumers request explainability only when needed.

## Consequences

### Positive

- Users and support can trace recommendations to observable metrics.
- UI can render a generic "Why this recommendation?" panel from a consistent
  `explanation` JSON shape without per-type custom logic in the frontend.
- Regression tests can assert explanation factors, not just final numbers.
- History snapshots preserve explanation factors for trend analysis.
- GPU detail endpoints stop recomputing from digests after backfill.

### Negative

- **Column proliferation:** ~10–20 new nullable columns per recommendation table
  (8 live tables + history tables → one migration file, manageable).
- **Engine struct change:** Rec structs gain explanation fields; write paths map
  fields → `expl_*` columns.
- **Migration coordination:** Single numbered migration (`000145`) adds all
  columns to live and history tables; down migration drops them.
- **Backfill required:** Existing rows have NULL explanations until a one-shot
  backfill pass (Phase 2.5) re-runs the engine and writes the **full**
  recommendation (values and explanation columns). The algorithm has not changed,
  so values will match (or differ negligibly due to time drift). Supports
  `--concurrency N` with work partitioned by `cluster_uuid`. Triggered via
  management endpoint or CLI command.
- **Phase 0 prerequisite:** Node GPU time-slicing must be persisted to a new
  table before its explanation columns can be added.
- **API parameter:** Detail handlers must parse the `include` query parameter
  (comma-separated list) and conditionally assemble the nested object when
  `explanation` is requested.

## Related Decisions

| ADR | Relationship |
|-----|--------------|
| [ADR-0048](0048-relational-columns-not-jsonb-blobs.md) | Typed columns over JSONB blobs |
| [ADR-0281](0281-jsonb-vs-normalized-columns-when-each-appropriate.md) | When JSONB vs columns |
| [ADR-0294](0294-slim-list-contract.md) | Explanation on detail only; opt-in via query param |
| [ADR-0295](0295-integer-first-architecture.md) | Integer scales for factors |
| [ADR-0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md) | Percentile choices documented in factors |
| [ADR-0007](0007-adaptive-margin-p95-p50-over-mean.md) | Adaptive margin as explanation factor |
| [ADR-0010](0010-logarithmic-oom-bump-capped-1-60.md) | OOM bump as explanation factor |
| [ADR-0060](0060-separate-recommendation-history.md) | History tables now include explanation columns |

## References

- [Implementation plan](../plans/recommendation-explanations.md)
- [Recommendation engine reference](../architecture/recommendation-engines.md)
- [`internal/engine/recommend_cpu_and_memory.go`](../../internal/engine/recommend_cpu_and_memory.go)
- [`internal/engine/recommend_all.go`](../../internal/engine/recommend_all.go)
- [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go)
- [`internal/engine/snapshot_classify.go`](../../internal/engine/snapshot_classify.go)
