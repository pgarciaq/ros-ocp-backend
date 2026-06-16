# ADR-0297: Persist node GPU time-slicing recommendations at ingest

## Status

Accepted

## Phase

13 (explainability prerequisite)

## Context

Node-level GPU time-slicing recommendations identify underutilized physical GPUs
that can be shared across containers via `nvidia.com/gpu.replicas` (NVIDIA
device-plugin time-slicing). The native engine computes these recommendations in
[`ComputeNodeTimeslicingRecWithSettings()`](../../internal/engine/gpu_timeslicing.go)
from `gpu_container_digests` and per-container `GPURec` values produced by
[`RecommendGPUWithSettings()`](../../internal/engine/gpu_recommender.go).

Today the full node-level recommendation — replica count, confidence, candidate
and impacted container lists, notification code 36, and dollar savings — is
**computed at API read time**, not written during ingestion:

- [`GetNodeRecommendations()`](../../internal/api/handlers_node_recs.go) paginates
  `(cluster, node, gpu_model)` triples from digests, re-queries GPU recs, and
  runs the time-slicing engine per triple.
- [`enrichWithGPU()`](../../internal/api/gpu_enrichment.go) runs the same engine
  on every container list/detail request to attach `time_slicing_node`,
  `time_slicing_replicas`, and `estimated_monthly_timeslicing_savings` to the
  `gpu.{term}` block.

[ADR-0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md) accepted this
compute-at-read design because candidate sets and cost-model rates change between
ingests. That trade-off made sense when time-slicing was API-only and savings were
excluded from fleet summary ([ADR-0071](0071-exclude-gpu-from-savings-summary.md)).

ADR-0115's compute-at-read choice was a **pragmatic shortcut** for the initial
implementation: fewer tables, fewer writes, and no history or explanation
machinery to build. The presumed freshness benefit does not hold in practice.
Time-slicing recommendations are derived entirely from **accumulated historical
digests** (days to weeks of DCGM metrics per container), not from real-time
cluster state. A newly scheduled container has zero digest history and cannot
influence the recommendation until enough data accumulates across subsequent
ingest cycles. The underlying inputs are therefore bounded by the ingest cycle
regardless of whether the recommendation is computed at ingest or at API read —
recomputing on every request does not surface fresher signal, it only repeats
the same historical aggregation. Persistence is now required to support
explanation factors (ADR-0296), history tracking, reduced API latency, and
consistency with every other recommendation type (PVC, quota, VM, node CPU/memory)
that already follows compute-at-ingest.

Two additional pressures reinforce this shift:

1. **Explanation factors** — [ADR-0296](0296-recommendation-explanation-factors-typed-columns.md)
   requires write-time capture of driving metrics. Node GPU time-slicing cannot
   receive `expl_*` columns until recommendations are stored at ingest; recompute-at-read
   would reproduce explanations that may diverge from the stored recommendation after
   threshold or term changes.
2. **Operational cost** — Every list and container-detail request re-scans digests
   and re-runs classification + time-slicing for affected clusters. At fleet scale
   this duplicates work already performed during ingestion (`StoreGPUClassifications`).

**What is persisted today (GPU plugin):**

| Layer | Table | Persisted fields | Time-slicing? |
|-------|-------|------------------|---------------|
| Digests | `gpu_container_digests` | DCGM aggregates per container × day | Raw inputs only |
| Container | `recommendation_sets` | `gpu_classification`, idle state, `estimated_gpu_savings_cents` (MIG/idle) | **No** — no `time_slicing_*`, no notification 36 |
| Node CPU/memory | `node_recommendations` | Utilization, consolidation, sizing | **No** — different recommendation type |
| VM GPU | `vm_recommendations` | `recommended_time_slice_count`, `gpu_timeslice_*` | **Yes** — VM path already persists |

The docs reference `node_recommendations` with `recommendation_type = gpu_time_slicing`,
but that table holds only node CPU/memory utilization rows
([`PersistNodeRecommendations()`](../../internal/engine/recommend_nodes.go)). GPU
time-slicing uses a separate API response type (`NodeGPURecommendation`) with no
backing recommendation table.

## Decision

**This ADR supersedes the time-slicing portion of
[ADR-0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md).** MIG and idle
GPU savings remain persisted at ingest as before; node GPU time-slicing moves from
compute-at-read to compute-at-ingest. The freshness rationale that justified
read-time computation no longer applies (see Context).

Persist node GPU time-slicing recommendations to a dedicated table
**`node_gpu_timeslicing_recommendations`** during cluster ingestion, following the
same patterns as PVC, quota, and VM recommendation types:

1. **Compute at ingest** — After `StoreGPUClassifications`, group containers by
   `(cluster, node, gpu_model, term)`, run `ComputeNodeTimeslicingRecWithSettings`,
   and upsert via `PersistNodeGPUTimeSlicingRecs`.
2. **Read from table** — `GET .../gpu/timeslicing` and container GPU enrichment
   read persisted rows; digest recompute is a fallback only during backfill.
3. **History** — Append snapshots to `node_gpu_timeslicing_recommendation_history`
   on each upsert (mirroring `quota_recommendation_history` /
   `vm_recommendation_history`).
4. **Savings** — Persist `estimated_savings_cents` at ingest using the same cost
   rates available during ingestion; refresh via the existing savings-recalc path
   when cost models change (same as container and node CPU/memory savings).
5. **Container cross-reference** — Denormalize `time_slicing_node` and
   `time_slicing_replicas` onto `recommendation_sets` for SQL-filterable list
   queries (optional columns, populated when a container is a time-slicing candidate).
6. **Container lists** — Store `candidate_containers` and `impacted_containers` as
   `JSONB` on the parent row. Each entry is a structured object (namespace, workload,
   container, sm_active_avg, classification); lists are always read/written as a complete
   unit. Cross-container lookups use `recommendation_sets.time_slicing_node`, not
   search inside the JSONB arrays. Type safety is enforced in Go; a GIN index can be
   added later if needed.

Do **not** reuse `node_recommendations` — that table is keyed by `(node, term, engine)`
for dual cost/performance CPU/memory rows and has a different lifecycle.

## Alternatives Considered

### Continue compute-at-read (status quo)

Keeps recommendations aligned with live digest state on every request. Rejected:
blocks ADR-0296 explanation columns, repeats expensive digest scans on every API
call, and prevents history tracking. VM GPU time-slicing already chose persistence
(`vm_recommendations.recommended_time_slice_count`).

### Store in `node_recommendations` with `recommendation_type`

Single node table for all node-level recs. Rejected: `node_recommendations` uses
`(org_id, cluster_uuid, node, term, engine)` PK with cost/performance dual rows;
GPU time-slicing has no engine split, is keyed by `(node, gpu_model, term)`, and
carries container membership lists. Overloading the table would require nullable
columns and complicate existing node utilization queries.

### Child table for candidate/impacted container lists

Normalized `node_gpu_timeslicing_candidates` rows with one row per container.
Rejected: container lists are structured objects (5 fields each) always read/written
as a complete unit; "find all recommendations for container Y" is handled by the
`time_slicing_node` cross-reference on `recommendation_sets`, not by searching
inside the lists. A child table adds JOIN overhead on every single-row fetch for
2–8 items with no query benefit.

### JSONB columns for candidate/impacted container lists (chosen)

Store `candidate_containers` and `impacted_containers` as `JSONB` on the parent row.
Accepted despite [ADR-0048](0048-relational-columns-not-jsonb-blobs.md) general
guidance: these lists are never queried individually, type safety is enforced in Go,
and JSONB avoids JOINs for the dominant access pattern (fetch one recommendation row).
A GIN index can be added later if search-inside-list becomes a requirement.

### Persist recommendation metadata only; compute savings at read time

Hybrid of ADR-0115. Rejected: savings recalc infrastructure already exists for
other types; persisting cents at ingest enables consistent fleet views and history
while recalc refreshes rates.

## Consequences

### Positive

- **Explanation factors (future)** — ADR-0296 Phase 2c can add `expl_*` columns to
  `node_gpu_timeslicing_recommendations` and capture replica math inputs at write time.
- **API exposure** — List endpoint reads indexed rows instead of re-running the engine;
  pagination, sorting, and RBAC filters move to SQL (consistent with PVC and quota lists).
- **History tracking** — Operators and UI can show how replica recommendations and
  candidate sets evolved per node × GPU model × term.
- **Container detail** — `time_slicing_node` / `time_slicing_replicas` served from
  `recommendation_sets` without per-request digest aggregation.
- **Consistency with VM path** — VM GPU time-slicing already persists slice count and
  rationale; node container workloads follow the same model.

### Negative

- **Staleness between ingests** — Recommendations reflect last ingest until the next
  cluster payload; pod rescheduling may lag by one ingest cycle. Mitigation: ingest
  frequency (operator upload cycle) and optional threshold-triggered reship.
- **New table + migration** — Two tables (live + history), retention sweep registration,
  and plugin `RetentionTables()` update.
- **Supersedes ADR-0115 time-slicing portion** — MIG/idle persistence unchanged;
  time-slicing moves from read-time to ingest-time. Fleet summary may still exclude
  GPU time-slicing until product decides otherwise ([ADR-0071](0071-exclude-gpu-from-savings-summary.md)).
- **Backfill** — Existing deployments need a one-shot recompute from digests to populate
  the new table (same pattern as ADR-0296 backfill).

## References

- [ADR-0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md) — prior read-time decision (time-slicing portion superseded)
- [ADR-0296](0296-recommendation-explanation-factors-typed-columns.md) — explanation prerequisite
- [docs/plans/gpu-time-slicing-persistence.md](../plans/gpu-time-slicing-persistence.md) — implementation plan
- [internal/engine/gpu_timeslicing.go](../../internal/engine/gpu_timeslicing.go)
- [internal/api/handlers_node_recs.go](../../internal/api/handlers_node_recs.go)
- [internal/api/gpu_enrichment.go](../../internal/api/gpu_enrichment.go)
