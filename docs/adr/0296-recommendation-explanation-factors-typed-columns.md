# ADR-0296: Store recommendation explanation factors as typed columns

## Status

Accepted

## Phase

13 (explainability)

## Context

The native Go engine produces recommendations for containers (CPU/memory), namespaces,
nodes, PVCs, GPUs, VMs, namespace quotas, and cluster resource quotas. Each engine
computes rich intermediate values — decay-weighted percentiles, adaptive margins,
OOM bumps, growth slopes, utilization ratios, classification trees — but persists
only the final recommendation numbers. Users see *what* was recommended without
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

Add **typed, nullable columns** to each recommendation table to capture the
driving factors computed at recommendation time. Each `Recommend*` function returns
an explanation struct alongside the recommendation; persistence and API layers
serialize these as a nested `explanation` object in detail responses.

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

Node GPU time-slicing list responses remain API-computed today; explanation columns
for node-level GPU time-slicing are deferred to a follow-up once those values are
persisted (see implementation plan).

Explanation columns are:

- Written atomically with the recommendation row during ingestion
- Exposed on **detail** endpoints (not slim list DTOs per [ADR-0294](0294-slim-list-contract.md))
- Integer-scaled where the engine already uses integer arithmetic ([ADR-0295](0295-integer-first-architecture.md))
- Nullable — older rows and partial failures leave explanation fields empty

## Alternatives Considered

### JSONB `explanation` blob per row

Flexible schema, one migration. Rejected: loses type safety in Go, prevents SQL
debugging (`SELECT … WHERE explanation->>'oom_bump_applied' = 'true'`), and
conflicts with [ADR-0048](0048-relational-columns-not-jsonb-blobs.md). The set of
explanation factors is stable and engine-defined — not user-extensible metadata.

### Recompute explanations at API read time from digests

Avoids schema changes. Rejected: duplicates engine logic in the API layer, adds
latency to detail endpoints, and produces explanations that may diverge from the
values that actually drove the persisted recommendation (threshold changes,
term window drift, stale digest pruning).

### Separate `recommendation_explanations` side table (EAV)

Normalized 1:1 extension table per resource type. Rejected: doubles join cost on
detail queries, splits the write path, and adds indirection without benefit when
column count per table is bounded (~10–20 factors).

### Emit explanations only in logs / internal debug endpoints

Useful for engineers, invisible to users. Rejected as the sole approach — the
primary goal is user-facing explainability in the UI.

## Rationale

- **Type safety:** Go structs map 1:1 to columns; compile-time checks on read/write.
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

## Consequences

### Positive

- Users and support can trace recommendations to observable metrics.
- UI can render a generic "Why this recommendation?" panel from a consistent
  `explanation` JSON shape without per-type custom logic in the frontend.
- Regression tests can assert explanation factors, not just final numbers.

### Negative

- **Column proliferation:** ~10–20 new nullable columns per recommendation table
  (8 tables → one migration file, manageable).
- **Engine API change:** Every `Recommend*` function gains an explanation return
  value; write paths must map explanation → columns.
- **Migration coordination:** Single numbered migration (`000145`) adds all
  columns; down migration drops them.
- **API surface growth:** Detail responses grow; list endpoints stay slim
  ([ADR-0294](0294-slim-list-contract.md)).
- **History tables:** `recommendation_history`, `vm_recommendation_history`, and
  quota history tables do **not** get explanation columns in v1 — history captures
  recommendation snapshots, not audit-grade factor traces.

## Related Decisions

| ADR | Relationship |
|-----|--------------|
| [ADR-0048](0048-relational-columns-not-jsonb-blobs.md) | Typed columns over JSONB blobs |
| [ADR-0281](0281-jsonb-vs-normalized-columns-when-each-appropriate.md) | When JSONB vs columns |
| [ADR-0294](0294-slim-list-contract.md) | Explanation on detail only |
| [ADR-0295](0295-integer-first-architecture.md) | Integer scales for factors |
| [ADR-0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md) | Percentile choices documented in factors |
| [ADR-0007](0007-adaptive-margin-p95-p50-over-mean.md) | Adaptive margin as explanation factor |
| [ADR-0010](0010-logarithmic-oom-bump-capped-1-60.md) | OOM bump as explanation factor |

## References

- [Implementation plan](../plans/recommendation-explanations.md)
- [Recommendation engine reference](../architecture/recommendation-engines.md)
- [`internal/engine/recommend_cpu_and_memory.go`](../../internal/engine/recommend_cpu_and_memory.go)
- [`internal/engine/recommend_all.go`](../../internal/engine/recommend_all.go)
- [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go)
