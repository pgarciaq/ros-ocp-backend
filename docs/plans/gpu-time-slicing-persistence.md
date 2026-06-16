# GPU Time-Slicing Recommendation Persistence — Implementation Plan

## Overview

Move node GPU time-slicing recommendations from **compute-at-read** to
**compute-at-ingest**, storing results in `node_gpu_timeslicing_recommendations`
with history tracking. This implements [ADR-0297](../adr/0297-gpu-time-slicing-recommendation-persistence.md)
and unblocks ADR-0296 explanation columns for this recommendation type.

**Scope:** Node-level container GPU time-slicing (`GET .../gpu/timeslicing`). VM
GPU time-slicing is already persisted on `vm_recommendations` and is out of scope.

---

## Current State: What Is Computed vs Persisted

### Computed today (engine)

All logic lives in [`internal/engine/gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go):

| Function | Output |
|----------|--------|
| `partitionContainers()` | Splits node containers into **candidates** (underutilized, not MIG) vs **impacted** |
| `avgCandidateUtilization()` | Average SM, DRAM, FB fraction across candidates |
| `computeReplicas()` | `ceil(1/peak_util)` clamped to `[timeslicing_min_replicas, timeslicing_max_replicas]` |
| `computeTimeslicingConfidence()` | Candidate confidence × base penalty × impacted ratio |
| `computeTimeslicingSavings()` | Per-GPU and total-node savings from GPU monthly rate |
| `ComputeNodeTimeslicingRecWithSettings()` | Assembles `TimeslicingRec` + mutates candidate `GPURec` with cross-ref fields |

`TimeslicingRec` fields (all **ephemeral** today):

```go
NodeName, ClusterUUID, GPUModel, Term
RecommendedReplicas, SavingsPerGPU, TotalNodeSavings, Confidence
CandidateContainers[], ImpactedContainers[], NotificationCodes[]  // includes code 36
```

Candidate `GPURec` side effects (in-memory only unless API re-runs engine):

- `TimeSlicingNode`, `TimeSlicingReplicas`
- `EstimatedTimeslicingSavingsUSD`
- `NotificationCodes` += `NotifGPUTimeSharingCandidate` (36)

### Read-time invocation sites

| Site | File | Trigger |
|------|------|---------|
| Time-slicing list | [`handlers_node_recs.go`](../../internal/api/handlers_node_recs.go) | `ListNodeGPUTriplesPage` → `QueryGPURecommendations` → `ComputeNodeTimeslicingRecForOrg` per triple |
| Container enrichment | [`gpu_enrichment.go`](../../internal/api/gpu_enrichment.go) | Every container list/detail page with GPU containers |
| GPU summary count | [`handlers_gpu_summary.go`](../../internal/api/handlers_gpu_summary.go) | `CountNodeGPUTriples` (coverage only, not full rec) |

### Persisted today (GPU-related tables)

| Table | Written by | Relevant columns | Time-slicing? |
|-------|------------|----------------|---------------|
| `gpu_container_digests` | Ingest / digest flush | DCGM metrics, `node_name`, `gpu_model_name` | Inputs only |
| `recommendation_sets` | [`StoreGPUClassifications()`](../../internal/engine/gpu_query.go) | `has_gpu`, `gpu_model_name`, `gpu_classification`, `gpu_idle_*`, `estimated_gpu_savings_cents` | **No** MIG profile, no time-slicing cross-ref |
| `node_recommendations` | [`PersistNodeRecommendations()`](../../internal/engine/recommend_nodes.go) | CPU/memory utilization, consolidation | **Unrelated** type |
| `vm_recommendations` | [`PersistVMRecommendations()`](../../internal/engine/vm_db.go) | `recommended_time_slice_count`, `gpu_timeslice_*` | VM only |

### Gap summary

```
Ingest:  gpu_container_digests ──► StoreGPUClassifications ──► recommendation_sets (partial)
                                              │
                                              ✗ no node GPU TS persist
API:     gpu_container_digests ──► QueryGPURecommendations ──► ComputeNodeTimeslicingRec ──► response
                                              │
                                              ✗ repeated on every list/detail request
```

---

## Target Architecture

Follow patterns from **PVC** ([`WritePVCRecommendations()`](../../internal/engine/pvc_recommend.go)),
**quota** ([`AppendQuotaRecommendationHistory()`](../../internal/engine/quota_rec_history.go)),
and **VM** ([`PersistVMRecommendations()`](../../internal/engine/vm_db.go)):

```
Ingest (report_processor.go, GPU plugin enabled):
  MarkContainersWithGPU
  StoreGPUClassifications          → recommendation_sets (classification + MIG/idle savings)
  ComputeAndPersistNodeGPUTimeSlicing → node_gpu_timeslicing_recommendations
                                      → node_gpu_timeslicing_recommendation_history (append)
                                      → recommendation_sets (time_slicing_node, time_slicing_replicas denorm)

API:
  GET .../gpu/timeslicing          → SELECT from node_gpu_timeslicing_recommendations (+ JOIN candidates if normalized)
  Container list/detail            → read time_slicing_* from recommendation_sets; fallback to engine during backfill
```

---

## Database Schema

### Migration `000NNN_node_gpu_timeslicing_recommendations.up.sql`

**Live table** — one row per `(org_id, cluster_uuid, node_name, gpu_model, term)`:

```sql
CREATE TABLE IF NOT EXISTS node_gpu_timeslicing_recommendations (
    org_id                    TEXT NOT NULL,
    cluster_uuid              UUID NOT NULL,
    node_name                 TEXT NOT NULL,
    gpu_model                 TEXT NOT NULL DEFAULT '',
    term                      TEXT NOT NULL,

    recommended_replicas      INTEGER NOT NULL,
    confidence                REAL NOT NULL DEFAULT 0,
    confidence_level          REAL NOT NULL DEFAULT 0,  -- data_days/window_days; matches other rec types (migration 000133 pattern)

    candidate_count           INTEGER NOT NULL DEFAULT 0,
    impacted_count            INTEGER NOT NULL DEFAULT 0,

    notification_codes        SMALLINT[] NOT NULL DEFAULT '{}',

    estimated_savings_cents   BIGINT,                   -- total node savings; NULL when no cost data
    savings_per_gpu_cents     BIGINT,                   -- per-GPU share; NULL when no cost data

    last_seen_at              TIMESTAMPTZ,              -- from node freshness (max digest date on node)

    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (org_id, cluster_uuid, node_name, gpu_model, term)
);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_org_cluster
    ON node_gpu_timeslicing_recommendations (org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_list_sort
    ON node_gpu_timeslicing_recommendations (org_id, cluster_uuid, term, recommended_replicas);
```

**Candidate membership** (optional Phase 1b — add if list filters need container-level SQL):

```sql
CREATE TABLE IF NOT EXISTS node_gpu_timeslicing_candidates (
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    node_name       TEXT NOT NULL,
    gpu_model       TEXT NOT NULL,
    term            TEXT NOT NULL,
    namespace       TEXT NOT NULL,
    workload        TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    role            TEXT NOT NULL,  -- 'candidate' | 'impacted'
    sm_active_avg   REAL NOT NULL DEFAULT 0,
    classification  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_uuid, node_name, gpu_model, term, namespace, workload, container_name, role)
);
```

For v1, **JSONB is acceptable for candidate/impacted arrays on the parent row**
only if we defer normalized child table — prefer the child table if RBAC/tag
filters on candidate namespace are required at SQL layer.

**History table** — append-only, 90-day retention (match quota/VM):

```sql
CREATE TABLE IF NOT EXISTS node_gpu_timeslicing_recommendation_history (
    id                      BIGSERIAL PRIMARY KEY,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node_name               TEXT NOT NULL,
    gpu_model               TEXT NOT NULL,
    term                    TEXT NOT NULL,
    recommended_replicas    INTEGER NOT NULL,
    confidence              REAL NOT NULL,
    candidate_count         INTEGER NOT NULL,
    impacted_count          INTEGER NOT NULL,
    estimated_savings_cents BIGINT,
    recorded_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_node_gpu_ts_hist_lookup
    ON node_gpu_timeslicing_recommendation_history (org_id, cluster_uuid, node_name, gpu_model, term, recorded_at DESC);
```

**Container denormalization** (migration add-column on `recommendation_sets`):

```sql
ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS time_slicing_node TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS time_slicing_replicas INTEGER NOT NULL DEFAULT 0;
```

Use `0` replicas and empty node to mean "not a candidate" (consistent with
clearing GPU metadata in `MarkContainersWithGPU`).

### Go model

Add [`internal/model/node_gpu_timeslicing_recommendation.go`](../../internal/model/node_gpu_timeslicing_recommendation.go)
mirroring [`NodeGPURecommendation`](../../internal/model/node_recommendation.go) with DB tags.

---

## Engine Changes

### New: `PersistNodeGPUTimeSlicingRecs`

Location: `internal/engine/gpu_timeslicing_persist.go` (keeps `gpu_timeslicing.go` pure).

```go
func ComputeAndPersistNodeGPUTimeSlicingRecs(
    ctx context.Context,
    pool *pgxpool.Pool,
    orgID, clusterUUID string,
    terms []TermConfig,
    costData *costdata.ClusterCostData,
) error
```

Steps (mirror [`runNodeRecommendations`](../../internal/services/report_processor.go) + PVC upsert):

1. `QueryGPURecommendations` for cluster (reuse existing digest query).
2. `groupByNodeAndModel` (extract shared helper from `handlers_node_recs.go` into engine package).
3. For each group × term: `ComputeNodeTimeslicingRecWithSettings`.
4. Batch upsert live rows; delete stale `(node, gpu_model, term)` keys not in current result set.
5. `AppendNodeGPUTimeSlicingHistory` for each upserted row.
6. Update `recommendation_sets.time_slicing_*` for candidate containers; clear for non-candidates.
7. Observe `metrics.ObserveDB("persist_node_gpu_timeslicing_recs", ...)`.

**Savings:** Convert `computeTimeslicingSavings` output to `int64` cents via
[`money.USDToCents`](../../internal/money/money.go) per [ADR-0295](../adr/0295-integer-first-architecture.md).
Store NULL when `gpuRate` unavailable at ingest.

**Stale term cleanup:** Same `validTerms []string` pattern as
[`PersistNodeRecommendations`](../../internal/engine/recommend_nodes.go) and
[`WritePVCRecommendations`](../../internal/engine/pvc_recommend.go).

### Wire into ingest

In [`report_processor.go`](../../internal/services/report_processor.go), after
`StoreGPUClassifications` succeeds (GPU plugin block ~line 756):

```go
if err := engine.ComputeAndPersistNodeGPUTimeSlicingRecs(egCtx, pool, orgID, clusterUUID, gpuTerms, costData); err != nil {
    log.Warnf("native engine: persist node GPU time-slicing failed: %v", err)
}
```

Also invoke from [`threshold_recalculate.go`](../../internal/engine/threshold_recalculate.go)
GPU threshold path if time-slicing settings change (same as container recalc).

### Refactor: shared grouping helper

Move `groupByNodeAndModel` from [`handlers_node_recs.go`](../../internal/api/handlers_node_recs.go)
to `internal/engine/gpu_timeslicing_group.go` so ingest and API share one implementation.

---

## API Changes

### `GET .../gpu/timeslicing`

Replace digest recompute path with SQL read from `node_gpu_timeslicing_recommendations`:

1. Keep `CountNodeGPUTriples` / triple pagination **or** replace with direct table
   pagination once all actionable recs are persisted (simpler: paginate the rec table
   directly, matching PVC list pattern).
2. Map rows → `model.NodeGPURecommendation` (existing response shape unchanged).
3. Load candidate/impacted lists from child table or JSON column.
4. **Savings recalc at read:** Optional refresh from Masu rates when
   `estimated_savings_cents IS NULL` or cost cache stale (same fallback as container GPU).

OpenAPI: no breaking changes — response schema already documents `NodeGPURecommendation`.

### Container list/detail

Update [`gpu_enrichment.go`](../../internal/api/gpu_enrichment.go):

1. Read `time_slicing_node`, `time_slicing_replicas` from `recommendation_sets` when populated.
2. Fall back to live `ComputeNodeTimeslicingRecForOrg` only when columns are empty (backfill window).
3. Remove per-request full-cluster time-slicing pass once backfill completes.

### History endpoint (new, optional Phase 2)

`GET .../gpu/timeslicing/history?cluster=&node=&gpu_model=&term=`

Mirror [`ListQuotaRecommendationHistory`](../../internal/engine/quota_rec_history.go).
Not required for MVP if UI can defer history charts.

---

## History Tracking

| Event | Action |
|-------|--------|
| Ingest upsert | INSERT into `node_gpu_timeslicing_recommendation_history` |
| Recommendation removed (no longer qualifies) | DELETE live row; optional history row with `recommended_replicas = 0` or skip |
| Retention | Register both tables in GPU plugin `RetentionTables()`; prune history > 90 days |
| Source delete | Include in [`sourcesCleaner.go`](../../internal/services/housekeeper/sourcesCleaner.go) cluster cleanup |

---

## Testing Approach

### Unit tests

- `gpu_timeslicing_persist_test.go` — upsert, stale term deletion, savings cents conversion, candidate denorm onto `recommendation_sets`.
- Extend [`gpu_timeslicing_test.go`](../../internal/engine/gpu_timeslicing_test.go) — ensure `ComputeNodeTimeslicingRec` output maps 1:1 to persist struct.

### Integration tests

- `gpu_timeslicing_persist_integration_test.go` — seed `gpu_container_digests` + `recommendation_sets`, run persist, assert live + history rows.
- Update [`handlers_node_recs_integration_test.go`](../../internal/api/handlers_node_recs_integration_test.go) — list reads from table (no digest mock required for rec body).
- Update [`gpu_enrichment_test.go`](../../internal/api/gpu_enrichment_test.go) — container detail reads denormalized columns.

### Regression / contract

- [`openapi_contract_test.go`](../../internal/api/openapi_contract_test.go) — seed persisted row, assert list response shape.
- Run existing GPU E2E templates (`ocp_report_gpu_timeslicing.yml` in cost-onprem-chart).

### Backfill verification

One-shot command or masu management endpoint:

```
recompute-node-gpu-timeslicing --org-id=... [--cluster-uuid=...]
```

Compare persisted rows against current read-time output for sample clusters (diff report).

---

## Implementation Phases

| Phase | Work | Effort |
|-------|------|--------|
| **1 — Schema** | Migration (live + history + `recommendation_sets` columns), Go models, retention registration | **1 day** |
| **2 — Engine persist** | `ComputeAndPersistNodeGPUTimeSlicingRecs`, wire ingest + threshold recalc, shared grouping helper | **1.5 days** |
| **3 — API read path** | Refactor `handlers_node_recs.go` to SQL; simplify `gpu_enrichment.go` | **1 day** |
| **4 — History** | Append on upsert, prune job, optional history GET endpoint | **0.5 day** |
| **5 — Tests + backfill** | Unit/integration tests, backfill command, docs-site sync | **1 day** |
| **6 — Explanation (follow-on)** | ADR-0296 `expl_*` columns on live + history tables | **0.5 day** (after Phase 1–3) |

**Total: ~5–6 engineering days** (single developer, including review cycles).

---

## Effort Estimate Summary

| Item | Estimate |
|------|----------|
| Migration + models | 1 day |
| Engine + ingest wiring | 1.5 days |
| API refactor | 1 day |
| History + retention | 0.5 day |
| Tests + backfill + docs | 1 day |
| **Total MVP** | **~5 days** |
| Explanation columns (ADR-0296 Phase 2c) | +0.5 day |

---

## Files to Touch (expected)

| Area | Files |
|------|-------|
| Migration | `migrations/000NNN_node_gpu_timeslicing_recommendations.{up,down}.sql` |
| Engine | `internal/engine/gpu_timeslicing_persist.go`, `gpu_timeslicing_group.go`, `gpu_timeslicing.go` (remove side-effect mutation or keep for backfill fallback) |
| Ingest | `internal/services/report_processor.go`, `internal/engine/threshold_recalculate.go` |
| API | `internal/api/handlers_node_recs.go`, `internal/api/gpu_enrichment.go` |
| Model | `internal/model/node_gpu_timeslicing_recommendation.go` |
| Plugin | `internal/plugins/gpu/plugin.go` (retention tables) |
| Housekeeper | `internal/services/housekeeper/sourcesCleaner.go` |
| Tests | `internal/engine/gpu_timeslicing_persist_*_test.go`, `internal/api/handlers_node_recs_integration_test.go` |
| Docs | `docs/features/gpu-time-slicing.md` (fix incorrect `node_recommendations` table reference), `docs-site/features/gpu-time-slicing.md` |

---

## Related Documents

- [ADR-0297](../adr/0297-gpu-time-slicing-recommendation-persistence.md)
- [ADR-0115](../adr/0115-gpu-mig-idle-persist-timeslicing-read-time.md) (superseded for time-slicing)
- [ADR-0296](../adr/0296-recommendation-explanation-factors-typed-columns.md)
- [docs/plans/recommendation-explanations.md](recommendation-explanations.md) — Phase 0 prerequisite
- [docs/plans/gpu-timeslicing-implementation-plan.md](gpu-timeslicing-implementation-plan.md) — original feature plan (API done; persistence was deferred)
