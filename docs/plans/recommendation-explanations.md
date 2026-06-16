# Implementation Plan: Recommendation Explanation Factors

**ADR:** [ADR-0296](../adr/0296-recommendation-explanation-factors-typed-columns.md)  
**Branch:** `pgarciaq-rosocp-superpowers-phase14`  
**Status:** Planned

## Goal

Persist the intermediate values that drive each native-engine recommendation as
typed database columns, written at recommendation time, and expose them via API
detail responses as a nested `explanation` object when requested.

## Recommendation Types in Scope

Verified against `docs/architecture/recommendation-engines.md` and `internal/engine/`:

| Type | Engine entry point | Persist function | DB table |
|------|-------------------|------------------|----------|
| Container CPU/memory | `RecommendCPUAndMemory` | `WriteRecommendations` | `recommendation_sets` |
| Container GPU | `RecommendGPUWithSettings` | `StoreGPUClassifications` | `recommendation_sets` |
| Namespace CPU/memory | `RecommendCPUAndMemory` (via `recommendNamespaces`) | `WriteNamespaceRecommendations` | `namespace_recommendation_sets` |
| Node CPU/memory | `RecommendNodes` | `PersistNodeRecommendations` | `node_recommendations` |
| PVC/storage | `computePVCRecommendation` | `WritePVCRecommendations` | `pvc_recommendation_sets` |
| Namespace quota | `computeQuotaRecommendation` | `WriteQuotaRecommendations` | `quota_recommendation_sets` |
| Cluster quota | `computeClusterQuotaRecommendation` | `WriteClusterQuotaRecommendations` | `cluster_quota_recommendation_sets` |
| VM | `RecommendVM` | `UpsertVMRecommendation` (`vm_db.go`) | `vm_recommendations` |
| Snapshot | `classifySnapshot` | `WriteSnapshotRecommendations` | `snapshot_recommendation_sets` |
| Node GPU time-slicing | `RecommendNodeGPUTimeSlicing` (planned) | `PersistNodeGPUTimeSlicingRecs` (planned) | `node_gpu_timeslicing_recommendations` (Phase 0) |

**History tables in scope:** `recommendation_history`, `vm_recommendation_history`,
`quota_recommendation_history`, and other quota/namespace history tables receive
the same explanation columns. When a recommendation is archived to history, its
explanation is archived with it.

---

## Cross-Cutting Design

### Struct embedding for safety

Explanation factors are **embedded directly in the engine's result structs**, not
populated manually into a separate `Explanation` struct at write time.

`CPURec`, `MemoryRec`, `NodeRec`, `GPURec`, and similar structs already carry
intermediate values computed during recommendation. Add `expl_*` fields to those
structs (or embed a small factor sub-struct on the rec struct) and persist them
directly from the same object that produced the recommendation. Do **not** copy
values into a parallel `Explanation` type — that pattern drifts when algorithms
change because engineers update the engine but forget the copy step.

```go
// Example — extend existing rec struct, not a separate Explanation type
type CPURec struct {
    CostRequestMC int64
    // ... existing recommendation outputs ...

    // Explanation factors — persisted as expl_* columns
    CostPercentileMC     int64
    PerfPercentileMC     int64
    UsageP95MC           int64
    AdaptiveMarginBP     int32
    // ...
}
```

The write path maps rec struct fields → `expl_*` columns with a single mapping
function per resource type. API serializers read the same columns back into the
nested `explanation` JSON object.

### Assemble from existing + new columns

Some explanation factors already exist as first-class columns (e.g. PVC
`usage_ratio`, `growth_bytes_per_day`; container `confidence_level`). The API
**assembles** the `explanation` response from both existing columns and new
`expl_*` columns — do not duplicate values in the database. When a factor is
already stored under its canonical column name, the serializer includes it in
`explanation` by reading that column, not by adding a redundant `expl_*` copy.

**Confidence:** The existing `confidence_level` column (`dataDays / windowDays`,
capped at 1.0) already exists on `recommendation_sets` and related tables. Include
it in the API `explanation` response from that column. Do **not** add
`expl_confidence` or any new confidence column.

### Column prefix

All new explanation columns use the **`expl_` prefix** to distinguish driving
factors from recommendation outputs (`rec_*`, `current_*`, etc.).

### API shape

Explanation is **opt-in** via the `include` query parameter on detail endpoints.
It is **not** included by default — this avoids bloating responses for consumers
who do not need explainability data.

Define `include` in the OpenAPI spec as a **comma-separated list** of optional
expansion tokens, even though `explanation` is the only supported value today.
This avoids a breaking change when additional expansions are added later (e.g.
`savings_detail`).

```
GET /recommendations/openshift/{uuid}?include=explanation
GET /recommendations/openshift/{uuid}?include=explanation,savings_detail   # future
```

When requested, detail handlers add a sibling `explanation` object (never on
slim list DTOs):

```json
{
  "recommendations": {
    "medium_term": {
      "cost": {
        "cpu_request_millicores": 250,
        "explanation": {
          "confidence_level": 0.857,
          "data_days": 7,
          "cpu_cost_percentile_millicores": 180,
          "cpu_adaptive_margin_basis_points": 11500,
          "cpu_usage_p95_millicores": 160,
          "oom_count_sum": 3,
          "oom_bump_applied": true
        }
      }
    }
  }
}
```

Namespace, node, PVC, quota, cluster-quota, VM, and snapshot handlers follow the same
opt-in pattern at their respective nesting level.

### Migration

Single migration **`migrations/000145_recommendation_explanation_columns.up.sql`**
(adds all columns to live **and history** tables, all nullable, no backfill in
migration). Down migration drops columns.

No `CONCURRENTLY` index needed — explanation columns are not filter/sort keys in v1.

### Integer scales

Follow [ADR-0295](../adr/0295-integer-first-architecture.md):

| Quantity | Storage | Notes |
|----------|---------|-------|
| CPU | `BIGINT` millicores | Same as existing rec columns |
| Memory | `BIGINT` KiB | Same as existing rec columns |
| Margins/ratios | `INTEGER` basis points | 10000 = 100%; matches `MarginScale` |
| GPU utilization | `INTEGER` basis points | Matches digest schema |
| Growth slope | `BIGINT` bytes/day | PVC already uses `growth_bytes_per_day BIGINT` |
| Percentiles (float legacy) | Prefer integers; use `REAL` only where engine already emits float32 utilizations (node CPU util p50/p95) |

---

## Per-Type Column Specification

### 1. Container CPU/memory (`recommendation_sets`)

**Engine:** `RecommendCPUAndMemory` in [`internal/engine/recommend_cpu_and_memory.go`](../../internal/engine/recommend_cpu_and_memory.go)  
**Write path:** `WriteRecommendations` in [`internal/engine/recommend_all.go`](../../internal/engine/recommend_all.go) (~L350 INSERT)  
**API assembly:** `assembleNativeResults` / detail handler in [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go)

Intermediate values currently computed then discarded in `multiCPUAndMemoryWeightedPercentiles` and margin/OOM application:

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_data_days` | `INTEGER` | Days in term window | `ContainerRec.DataDays` |
| `expl_decay_half_life_hours` | `REAL` | Term decay half-life | `TermConfig.DecayHalfLifeHours` |
| `expl_cpu_cost_pct_mc` | `BIGINT` | Decay-weighted CPU cost percentile (pre-margin) | `multiCPUAndMemoryWeightedPercentiles` vals[0] |
| `expl_cpu_perf_pct_mc` | `BIGINT` | Decay-weighted CPU perf percentile (pre-margin) | vals[1] |
| `expl_cpu_usage_p95_mc` | `BIGINT` | Weighted avg P95 for margin calc | vals[2] |
| `expl_cpu_usage_p50_mc` | `BIGINT` | Weighted avg P50 for margin calc | vals[3] |
| `expl_cpu_usage_mean_mc` | `BIGINT` | Weighted avg mean for margin calc | vals[4] |
| `expl_cpu_adaptive_margin_bp` | `INTEGER` | Applied CPU margin (basis points) | `ComputeAdaptiveMarginScaled` |
| `expl_cpu_trend_slope` | `REAL` | CPU usage trend slope | `extras.TrendSlope` |
| `expl_mem_cost_pct_kib` | `BIGINT` | Decay-weighted memory cost percentile | vals[5] |
| `expl_mem_perf_pct_kib` | `BIGINT` | Decay-weighted memory perf percentile | vals[6] |
| `expl_mem_usage_p95_kib` | `BIGINT` | Weighted avg mem P95 | vals[7] |
| `expl_mem_usage_p50_kib` | `BIGINT` | Weighted avg mem P50 | vals[8] |
| `expl_mem_usage_mean_kib` | `BIGINT` | Weighted avg mem mean | vals[9] |
| `expl_mem_adaptive_margin_bp` | `INTEGER` | Applied memory margin (basis points) | `ComputeAdaptiveMarginScaled` |
| `expl_mem_trend_slope` | `REAL` | Memory trend slope | `extras.MemTrendSlope` |
| `expl_oom_count_sum` | `BIGINT` | OOM events in window | `memCfg.OOMCountSum` |
| `expl_oom_bump_applied` | `BOOLEAN` | Whether OOM bump changed memory request | compare pre/post bump |
| `expl_cpu_floor_applied` | `BOOLEAN` | Whether 25 mCPU floor was applied | `applyFloor` |
| `expl_is_idle` | `BOOLEAN` | Idle classification for this term | `cpuRec.IsIdle` |

**Not a new column:** `confidence_level` — already on the row; include in API
`explanation` when `?include=explanation` is set.

**Engine change:** Extend `CPURec` / `MemoryRec` / `ContainerRec` with explanation
fields embedded directly. Populate in `processContainer` inside
`RecommendWorkloadsStreaming` (~L150–240 in `recommend_all.go`).

**API change:** Extend `EngineRecommendation` in `recommendation_set_native.go`
with optional `Explanation *ContainerExplanationAPI`, populated only when
`?include=explanation` is present.

---

### 2. Container GPU (`recommendation_sets`, `has_gpu = true`)

**Engine:** `RecommendGPUWithSettings` in [`internal/engine/gpu_recommender.go`](../../internal/engine/gpu_recommender.go)  
**Write path:** `StoreGPUClassifications` → `queueGPUClassificationUpdate` in [`internal/engine/gpu_query.go`](../../internal/engine/gpu_query.go)  
**API today:** GPU block enriched at read time in detail response (`GPURecommendation` in [`internal/model/detail_response.go`](../../internal/model/detail_response.go))

Today only classification, idle state, and savings are persisted; profile and
utilization averages are recomputed from digests on detail fetch. After this
feature, the GPU detail endpoint **reads from persisted columns only** — no more
live recomputation from digests. Old rows with NULL explanation columns use the
existing digest recompute as a **fallback only during the backfill window**; once
backfill completes, all rows have persisted factors.

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_gpu_sm_active_avg_bp` | `INTEGER` | Avg SM active (basis points) | `GPURec.SMActiveAvg` |
| `expl_gpu_tensor_active_avg_bp` | `INTEGER` | Avg tensor pipe active | `GPURec.TensorPipeActiveAvg` |
| `expl_gpu_dram_active_avg_bp` | `INTEGER` | Avg DRAM active | `GPURec.DRAMActiveAvg` |
| `expl_gpu_fb_usage_max_mib` | `INTEGER` | Max frame buffer usage MiB | `GPURec.FBUsageMaxMiB` |
| `expl_gpu_fb_p98_mib` | `INTEGER` | P98 FB for MIG selection | `percentileFB(digests, MIGFBPercentile)` |
| `expl_gpu_recommended_profile` | `TEXT` | MIG profile chosen | `GPURec.RecommendedGPUProfile` |
| `expl_gpu_current_profile` | `TEXT` | Current MIG profile | `GPURec.CurrentGPUProfile` |
| `expl_gpu_has_profiling_data` | `BOOLEAN` | Tier-1 vs Tier-2 classification | `GPURec.HasProfilingData` |
| `expl_gpu_memory_bound` | `BOOLEAN` | Memory-bound flag | `GPURec.MemoryBoundDetected` |

**Engine change:** Extend `GPURec` with explanation fields embedded directly.
Pass through `StoreGPUClassifications` batch UPDATE.

**API change:** Read persisted explanation columns when `?include=explanation`;
fall back to digest recompute only when columns are NULL (pre-backfill rows).

---

### 3. Namespace CPU/memory (`namespace_recommendation_sets`)

**Engine:** Same as container — `RecommendCPUAndMemory` via [`internal/engine/recommend_namespace.go`](../../internal/engine/recommend_namespace.go)  
**Write path:** `WriteNamespaceRecommendations` (same file, ~L200+)  
**API:** [`internal/model/namespace_recommendation_set_native.go`](../../internal/model/namespace_recommendation_set_native.go)

Use **identical explanation columns** as container (`expl_*` prefix), one set per
`(namespace, term, engine, schedule_type)` row. Namespace aggregates digest rows
across containers; explanation columns describe the aggregated window, not
individual containers.

**Business hours rows:** Same explanation columns on BH rows (`schedule_type =
'business_hours'`). Show explanation for BH rows if the row exists. The UI hides
the entire BH section when BH scheduling is not configured for the namespace — no
special backend handling required.

**Engine change:** Mirror container explanation capture in `recommendNamespaces` loop (~L146–180).

---

### 4. Node CPU/memory (`node_recommendations`)

**Engine:** `RecommendNodes` → `classifyNode` + `sizeNodeForEngine` in [`internal/engine/recommend_nodes.go`](../../internal/engine/recommend_nodes.go)  
**Write path:** `PersistNodeRecommendations` (~L1050 INSERT)

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_data_days` | `INTEGER` | Days in term window | `NodeRec.DataDays` |
| `expl_target_utilization_bp` | `INTEGER` | Engine target (8000 cost / 5500 perf) | `NodeEngineConfig.TargetUtilization` |
| `expl_current_cpu_mc` | `BIGINT` | Node allocatable/request baseline CPU | `nodeClassification.CurrentCPUMC` |
| `expl_current_mem_kib` | `BIGINT` | Node baseline memory | `nodeClassification.CurrentMemKiB` |
| `expl_max_cpu_usage_p95_mc` | `BIGINT` | Max daily P95 CPU usage in window | `class.maxCPUUsageP95MC` |
| `expl_max_mem_usage_p95_kib` | `BIGINT` | Max daily P95 mem usage | `class.maxMemUsageP95KiB` |
| `expl_pod_scheduling_headroom_bp` | `INTEGER` | Pod scheduling headroom (basis points) | `class.PodSchedulingHeadroom` |
| `expl_ema_imbalance_bp` | `INTEGER` | EMA-smoothed CPU/mem imbalance | stranded-resource calc |
| `expl_consolidation_applied` | `BOOLEAN` | Instance-type consolidation changed suggestion | `applyInstanceTypeConsolidation` |
| `expl_sizing_formula` | `TEXT` | `target_util` / `headroom_2x` / `idle` | engine branch taken |

Existing columns (`cpu_util_p50`, `trend_slope`, `stranded_resource`,
`suggested_instance_type`, `instance_type_reason`) remain; explanation columns
capture inputs to sizing that are not already stored.

**Engine change:** Extend `NodeRec` with explanation fields embedded directly,
populated in `RecommendNodes` after `sizeNodeForEngine`.

**API change:** Add `explanation` to `NodeUtilizationEngineRec` in [`internal/model/node_cpu_mem_recommendation.go`](../../internal/model/node_cpu_mem_recommendation.go) when `?include=explanation`.

---

### 5. PVC/storage (`pvc_recommendation_sets`)

**Engine:** `computePVCRecommendation` in [`internal/engine/pvc_recommend.go`](../../internal/engine/pvc_recommend.go)  
**Write path:** `WritePVCRecommendations`

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_data_days` | `INTEGER` | Days in window | `rec.DataDays` |
| `expl_oversized_threshold_bp` | `INTEGER` | Configured oversized threshold | `PVCThresholdSettings.OversizedThreshold` |
| `expl_near_full_threshold_bp` | `INTEGER` | Configured near-full threshold | `PVCThresholdSettings.NearFullThreshold` |
| `expl_recommended_size_multiplier` | `INTEGER` | Size multiplier applied (×100) | `PVCThresholdSettings.RecommendedSizeMultiplier` |
| `expl_min_recommended_gib` | `INTEGER` | Floor GiB applied | `PVCThresholdSettings.MinRecommendedGiB` |
| `expl_classification_reason` | `TEXT` | `orphaned` / `oversized` / `near_full` / `healthy` | switch branch in `computePVCRecommendation` |

**Not new columns:** `usage_ratio` and `growth_bytes_per_day` already exist on
the row. The API assembles them into `explanation` from those existing columns
(see Cross-Cutting Design — assemble from existing + new columns).

---

### 6. Namespace quota (`quota_recommendation_sets`)

**Engine:** `computeQuotaRecommendation` in [`internal/engine/recommend_quota.go`](../../internal/engine/recommend_quota.go)  
**Write path:** `WriteQuotaRecommendations`

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_headroom_bp` | `INTEGER` | Headroom basis points applied | `cfg.HeadroomBasisPoints` |
| `expl_container_cpu_sum_mc` | `BIGINT` | Sum of container rec CPU requests | `ContainerQuotaAggregate.CPURequestSumMC` |
| `expl_container_mem_sum_bytes` | `BIGINT` | Sum of container mem requests | `agg.MemoryRequestSumBytes` |
| `expl_signal_c_cpu_used_mc` | `BIGINT` | max(quota used, container sum) CPU | utilization input |
| `expl_max_utilization_bp` | `INTEGER` | Highest resource utilization BP | `maxQuotaUtilizationBP` |
| `expl_risk_level` | `TEXT` | Derived risk band | `classifyQuotaRisk` |
| `expl_recommendation_reason` | `TEXT` | `tighten` / `raise` / `optimal` branch | `classifyQuotaRecommendation` |

Hard/used/recommended values already exist as first-class columns; explanation
captures **aggregation inputs and classification logic**.

---

### 7. Cluster resource quota (`cluster_quota_recommendation_sets`)

**Engine:** `computeClusterQuotaRecommendation` in [`internal/engine/recommend_cluster_quota.go`](../../internal/engine/recommend_cluster_quota.go)  
**Write path:** `WriteClusterQuotaRecommendations`

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_headroom_bp` | `INTEGER` | Headroom basis points | `cfg.HeadroomBasisPoints` |
| `expl_ns_quota_cpu_sum_mc` | `BIGINT` | Sum of namespace quota recs CPU | `NamespaceQuotaClusterAggregate` |
| `expl_ns_quota_mem_sum_bytes` | `BIGINT` | Sum of namespace quota recs mem | same |
| `expl_base_cpu_mc` | `BIGINT` | max(CRQ used, NS agg) before headroom | `baseRecommended` |
| `expl_max_utilization_bp` | `INTEGER` | Highest utilization BP | `maxClusterQuotaUtilizationBP` |
| `expl_recommendation_reason` | `TEXT` | Classification branch | `classifyClusterQuotaRecommendation` |

---

### 8. VM (`vm_recommendations`)

**Engine:** `RecommendVM` in [`internal/engine/vm_recommender.go`](../../internal/engine/vm_recommender.go)  
**Write path:** `UpsertVMRecommendation` in [`internal/engine/vm_db.go`](../../internal/engine/vm_db.go)  
**API:** [`internal/api/handlers_vm_recs.go`](../../internal/api/handlers_vm_recs.go)

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_data_days` | `INTEGER` | Days in term window | `len(windowed)` |
| `expl_max_cpu_usage_mc` | `BIGINT` | P95 or P99 max CPU in window | `vmMaxCPUUsage` |
| `expl_max_mem_usage_kib` | `BIGINT` | Peak memory KiB | `vmMaxMemoryUsageKiB` |
| `expl_cpu_margin_bp` | `INTEGER` | Applied CPU margin | `vmResolveCPUMargin` |
| `expl_mem_margin_bp` | `INTEGER` | Applied memory margin | `cfg.MemMarginMin` scaled |
| `expl_raw_recommended_vcpu` | `INTEGER` | Pre-hysteresis vCPU | `rawRecommendedVCPU` |
| `expl_raw_recommended_mem_gib` | `INTEGER` | Pre-hysteresis memory GiB | `rawRecommendedMemGiB` |
| `expl_downsize_hysteresis_held` | `BOOLEAN` | Downsize blocked by hysteresis | `downsizeHeld` |
| `expl_guest_agent_used` | `BOOLEAN` | Sizing used guest-agent memory | `useAgentData` |
| `expl_idle_detected` | `BOOLEAN` | VM idle classification | `isIdle` |
| `expl_abandoned_detected` | `BOOLEAN` | VM abandoned | `isAbandoned` |
| `expl_power_off_candidate` | `BOOLEAN` | Power-off candidate | `isPowerOffCandidate` |
| `expl_sizing_branch` | `TEXT` | `abandoned` / `idle` / `active_downsize` / `active` | engine branch |

GPU sub-recommendation within VM (`RecommendVMTimeSlicing`, MIG profile) can
reuse VM GPU columns already on the row; add `expl_gpu_action` / `expl_gpu_rationale`
TEXT columns if not sufficiently covered by `gpu_timeslice_rationale`.

---

### 9. Node GPU time-slicing (`node_gpu_timeslicing_recommendations` — Phase 0)

**Prerequisite:** Node GPU time-slicing recommendations are currently computed at
API read time from digests. Phase 0 creates a `node_gpu_timeslicing_recommendations`
table and moves compute-at-read to compute-at-ingest. Explanation columns for this
type are added in Phase 2c after the table exists.

See [Phase 0: Prerequisites](#phase-0--prerequisites) below.

---

### 10. Snapshot (`snapshot_recommendation_sets`)

**Engine:** `classifySnapshot` in [`internal/engine/snapshot_classify.go`](../../internal/engine/snapshot_classify.go)  
**Write path:** `WriteSnapshotRecommendations` (same file)  
**API:** [`internal/api/handlers_snapshot.go`](../../internal/api/handlers_snapshot.go)

Snapshot recommendations are **simpler** than percentile/margin engines — they
are rule-based classification, not statistical sizing. Most driving factors already
exist as first-class columns: `age_days`, `source_pvc_exists`, `restored_pvc_count`,
`managed_by`, `recommendation_type`. The API assembles those existing columns into
`explanation` alongside three new columns that capture which threshold and rule
fired:

| Column | Type | Description | Source |
|--------|------|-------------|--------|
| `expl_threshold_used` | `INTEGER` | Threshold value that triggered classification (e.g. `orphan_age_days=7`) | `settings.OrphanAgeDays`, `settings.StaleAgeDays`, etc. |
| `expl_threshold_name` | `TEXT` | Which setting was decisive | `'orphan_age_days'` / `'stale_age_days'` / `'redundancy_max'` etc. |
| `expl_classification_rule` | `TEXT` | Human-readable rule that fired | e.g. `'source PVC deleted AND age > orphan threshold'` |

**Not new columns:** `age_days`, `source_pvc_exists`, `restored_pvc_count`,
`managed_by`, and `recommendation_type` — include in API `explanation` from
existing columns.

**Engine change:** Extend `SnapshotRec` with explanation fields embedded directly.
Populate in `classifySnapshot` when each classification branch fires (orphaned,
managed, redundant, stale, never_restored, active).

**API change:** Add `explanation` to snapshot detail response when
`?include=explanation` is set; assemble from existing columns + new `expl_*`.

---

## Implementation Phases

### Phase 0 — Prerequisites (1 PR, ~2–3 days)

Required before Phase 2c GPU work:

1. Create `node_gpu_timeslicing_recommendations` table (migration)
2. Move node GPU time-slicing compute-at-read logic to compute-at-ingest
   (`PersistNodeGPUTimeSlicingRecs` or equivalent)
3. Update list/detail handlers to read from the new table instead of live digest
   queries

Explanation columns for node GPU time-slicing are added in Phase 2c once this
table exists.

### Phase 1 — Schema and types (1 PR, ~1 day)

1. Add `migrations/000145_recommendation_explanation_columns.{up,down}.sql`
   (live tables **and** history tables: `recommendation_history`,
   `vm_recommendation_history`, `quota_recommendation_history`, etc.)
2. Extend engine rec structs (`CPURec`, `MemoryRec`, `NodeRec`, `GPURec`, etc.)
   with embedded explanation fields (zero value safe)
3. Add API mapping types for the nested `explanation` JSON shape
4. Unit tests: explanation field JSON tags match OpenAPI naming (snake_case)

### Phase 2 — Engine capture (2–3 PRs, ~7 days total)

Split by resource type to keep reviews manageable:

| PR | Files | Functions |
|----|-------|-----------|
| 2a Container + namespace | `recommend_cpu_and_memory.go`, `recommend_all.go`, `recommend_namespace.go` | Embed + populate explanation fields on rec structs |
| 2b Node + PVC | `recommend_nodes.go`, `pvc_recommend.go` | Same |
| 2c GPU + VM + node GPU TS | `gpu_recommender.go`, `gpu_query.go`, `vm_recommender.go`, `vm_db.go`, node GPU TS persist | Same (node GPU TS requires Phase 0) |
| 2d Quota + CRQ + snapshot | `recommend_quota.go`, `recommend_cluster_quota.go`, `snapshot_classify.go` | Same (snapshot is lightweight rule-based classification) |

Each PR updates the corresponding `Write*` / `Persist*` SQL to include new columns.
History archive paths (`WriteRecommendationHistory`, etc.) copy explanation
columns alongside recommendation values.

### Phase 2.5 — Backfill (1 PR, ~2 days)

After deploying migration + engine changes, run a one-shot backfill pass:

1. Iterate recommendation rows where `expl_data_days IS NULL` (or any sentinel
   explanation column)
2. Load corresponding digests / inventory for each container/node/PVC/snapshot/etc.
3. Re-run the **full** recommendation engine for that row
4. Write the **complete** recommendation row (values **and** explanation columns)
5. Trigger via a management endpoint or CLI command (e.g.
   `ros-ocp-backend backfill-explanations --org-id … --resource container`)
6. Support **`--concurrency N`** (or `--workers N`) and partition work by
   `cluster_uuid` for parallelism at scale

The algorithm has not changed, so recommendation values will be the same (or
negligibly different due to time drift). Recomputing the full recommendation is
simpler and safer than writing explanation columns alone — it eliminates any risk
of explanation/recommendation mismatch.

GPU rows backfilled here eliminate the need for digest recompute fallback on
detail fetch. Container/namespace/node/PVC/quota/VM/snapshot rows get explanations
without waiting for natural re-ingestion.

### Phase 3 — API exposure (1 PR, ~1 day)

1. Add `include` query parameter (comma-separated list; `explanation` only in v1)
   to detail handlers
2. Add `Explanation` field to API DTOs (`EngineRecommendation`, `GPURecommendation`,
   `NodeUtilizationEngineRec`, `PVCRecommendationResponse`, quota/VM/snapshot handlers)
3. Map DB columns (existing + new `expl_*`) → nested `explanation` in serializers
   when `include=explanation` is set; omit entirely otherwise
4. Include `confidence_level` from existing column in explanation response
5. Update OpenAPI spec (`docs/openapi/openapi.yaml` or generated spec)
6. Contract tests: detail response includes `explanation` only when requested and
   columns are populated
7. Implement `include` parameter whitelist validation (see Security Considerations)

### Query strategy: always SELECT, conditionally serialize

GORM always fetches all struct fields (including `expl_*` columns) in a single
query. No conditional query building is needed. The API serializer checks whether
`?include=explanation` is present and conditionally includes the `explanation`
object in the JSON response. This avoids maintaining two query paths and leverages
GORM's default behavior.

This means explanation columns are always read from disk with the row (they're in
the same page), but only serialized to JSON when requested. The disk I/O cost is
zero (same page fetch regardless) and the serialization cost is skipped by default.

### Phase 4 — Frontend guidance and user documentation (ships with or before Phase 5)

Deliverables:

1. **UI integration guide** — `docs-site/ui-integration-guide.md` section (existing plan):
   - Generic `<RecommendationExplanation>` component spec
   - Request detail with `?include=explanation`
   - Label map from factor keys → user-facing strings (i18n keys)
   - Hide section when explanation fields are null
   - Hide BH section when BH scheduling is not configured

2. **"Understanding Your Recommendations" page** — new `docs-site/architecture/understanding-recommendations.md`:
   - User-friendly explanation of how each recommendation type is computed
   - Why CPU uses P60 (cost) vs P98 (performance)
   - Why memory uses P95/Max + adaptive margin + OOM bump
   - What the confidence score means (data_days / window_days)
   - How to read explanation factors in the UI panel
   - Why a recommendation might be higher/lower than expected
   - Common scenarios: "Why is my memory recommendation 4x current?" with worked examples
   - References ADRs for deeper engineering context (links to `architecture/adrs.md`)
   - Targeted at platform engineers and cluster admins, not engine developers

3. **Operations documentation** — new section in `docs-site/operations/` (or existing page):
   - Backfill endpoint usage: authentication, invocation, monitoring
   - Admin-only access requirements
   - How to verify backfill completion (SQL query for NULL explanation rows)
   - GPU recompute fallback removal checklist

Phase 4 is documentation-only; Phase 5 implements the same guidance in `koku-ui`.

### Phase 5 — UI Implementation (2–3 PRs, ~5.5 days)

Implement explainability in the ROS optimizations breakdown area
(`apps/koku-ui-ros/`). The breakdown page today is container-focused: route
`/optimizations/details/breakdown?id={uuid}` loads a single recommendation detail
via Redux and renders configuration YAML plus optional CPU/memory utilization charts.

**Current UI architecture (baseline for Phase 5):**

| Concern | Location |
|---------|----------|
| Breakdown page shell | [`apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdown.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdown.tsx) |
| Configuration panel | [`optimizationsBreakdownConfiguration.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownConfiguration.tsx) |
| Utilization charts | [`optimizationsBreakdownUtilization.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownUtilization.tsx) + [`optimizationsBreakdownChart.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownChart.tsx) |
| Detail fetch (Redux thunk) | [`apps/koku-ui-ros/src/store/ros/rosActions.ts`](../../../koku-ui/apps/koku-ui-ros/src/store/ros/rosActions.ts) → `fetchRosReport` |
| Axios API call | [`apps/koku-ui-ros/src/api/ros/recommendations.ts`](../../../koku-ui/apps/koku-ui-ros/src/api/ros/recommendations.ts) `runRosReport` (GET `recommendations/openshift/{uuid}`) |
| API types | [`apps/koku-ui-ros/src/api/ros/recommendations.ts`](../../../koku-ui/apps/koku-ui-ros/src/api/ros/recommendations.ts) (`RecommendationEngine`, `RecommendationTerm`, etc.) |
| i18n messages | [`apps/koku-ui-ros/src/locales/messages.ts`](../../../koku-ui/apps/koku-ui-ros/src/locales/messages.ts) (+ generated `locales/data.json`, `locales/translations.json`) |
| Component library | PatternFly 6 (`@patternfly/react-core`, `@patternfly/react-charts/victory`) |

The breakdown page reads the recommendation UUID from the URL query (`id`), dispatches
`rosActions.fetchRosReport(RosPathsType.recommendation, RosType.ros, uuid)` in
`useMapToProps`, and selects cached data via `rosSelectors.selectRos`. Charts render
only when the box-plot feature toggle is enabled and `term.plots.plots_data` exists.

Split into manageable PRs:

#### PR 5a — API integration + "Why this recommendation?" panel (container type)

1. **Detail fetch:** Append `?include=explanation` to the breakdown detail GET.
   - Extend `runRosReport` in `recommendations.ts` to accept optional query params
     (e.g. `include=explanation`), or add a dedicated detail helper used only from
     breakdown fetch paths.
   - Update `fetchRosReport` / breakdown `useMapToProps` so the breakdown page always
     requests explanations (list endpoints unchanged).
   - Bust or shorten Redux cache for detail rows when `include` changes (detail fetch
     id should differ from list cache keys).
2. **Types:** Add `Explanation` interfaces to `recommendations.ts` — nested under
   `RecommendationEngine` at the same level as `config` (matches API shape under
   `recommendation_engines.{cost|performance}`).
3. **Component:** Create
   [`RecommendationExplanation.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/RecommendationExplanation.tsx)
   — PatternFly `ExpandableSection` (or `Panel` / `Card`) titled "Why this
   recommendation?"
   - Read `explanation` from the active term + optimization type
     (`getRecommendationTerm` + `recommendation_engines[optimizationType]`).
   - Render container CPU/memory factors as labeled key-value pairs using PatternFly
     `DescriptionList` / `DescriptionListGroup`.
   - Format values with existing helpers (`formatOptimization`, `formatPercentage`,
     `unitsLookupKey`) where applicable; basis points → percent display.
4. **Empty state:** When `explanation` is absent or all fields are null/undefined,
   show an inline message: "Explanation data will be available after next processing
   cycle" (i18n key). Do not render an empty expandable panel.
5. **Placement:** Mount `<RecommendationExplanation>` in
   `optimizationsBreakdown.tsx` inside each tab's content, above
   `OptimizationsBreakdownConfiguration` (or between configuration and utilization).
6. **i18n:** Add explanation panel title, empty-state text, and container factor labels
   to `messages.ts`; run locale generation script if required by CI.

#### PR 5b — Chart annotations

Extend [`optimizationsBreakdownChart.tsx`](../../../koku-ui/apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownChart.tsx)
(Victory / PatternFly charts):

1. **Base percentile reference line:** Horizontal dashed `ChartLine` at the cost or
   performance base percentile from `explanation` (e.g. `cpu_cost_percentile_millicores`
   for cost tab / CPU chart). Pass explanation-derived values from
   `OptimizationsBreakdownUtilization` into the chart as new props.
2. **OOM markers:** When `oom_count_sum > 0`, add vertical reference markers on the
   memory chart for days with OOM events (if plot data exposes OOM dates; otherwise
   annotate chart subtitle with OOM count + `oom_bump_applied` from explanation).
3. **Confidence badge:** PatternFly `Label` or `Badge` near the chart title showing
   low / medium / high derived from `confidence_level` thresholds (document thresholds
   in component or i18n descriptions).

#### PR 5c — Multi-type explanation rendering

Extend `<RecommendationExplanation>` (and reuse on future type-specific breakdown pages
as they land):

| Recommendation type | Key explanation fields to surface |
|---------------------|-----------------------------------|
| Container CPU/memory | Percentiles, adaptive margins, OOM, idle, data days |
| Node | Utilization targets, headroom, sizing formula, consolidation |
| PVC | Thresholds, classification reason, growth rate (from existing + expl columns) |
| Quota / CRQ | Headroom, aggregation inputs, risk level, recommendation reason |
| VM | Margins, hysteresis, guest agent, sizing branch, idle/abandoned flags |
| GPU | Utilization averages, MIG profile, memory-bound flag |
| Snapshot | Threshold name/value, classification rule (+ existing age/PVC columns) |

Implementation notes:

- Add a `recommendationType` prop (or infer from detail response metadata) and type-specific
  label maps in `messages.ts` (prefix keys e.g. `explanationContainer*`, `explanationNode*`, …).
- Use PatternFly `DescriptionList` for structured display; group CPU vs memory vs metadata
  sections for container type.
- Hide BH explanation subsection when BH scheduling is not configured (UI-only; same rule
  as Phase 4 doc).

**koku-ui files touched (summary):**

| File | PR |
|------|-----|
| `apps/koku-ui-ros/src/api/ros/recommendations.ts` | 5a (types + `include` param) |
| `apps/koku-ui-ros/src/api/ros/recommendations.test.ts` | 5a (assert `?include=explanation` on detail GET) |
| `apps/koku-ui-ros/src/store/ros/rosActions.ts` | 5a (pass include to API) |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdown.tsx` | 5a (mount panel) |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/RecommendationExplanation.tsx` | 5a, 5c (new) |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/RecommendationExplanation.test.tsx` | 5a (new) |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownUtilization.tsx` | 5b |
| `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/optimizationsBreakdownChart.tsx` | 5b |
| `apps/koku-ui-ros/src/locales/messages.ts` | 5a, 5c |
| `apps/koku-ui-ros/locales/data.json`, `translations.json` | 5a, 5c (regenerated) |

**Frontend testing (Phase 5):**

- Jest + React Testing Library (existing pattern in
  `apps/koku-ui-ros/src/routes/components/selectWrapper/`).
- `RecommendationExplanation.test.tsx`: panel hidden when explanation empty; factors
  rendered when fixture present; empty-state message when all null.
- `recommendations.test.ts`: detail helper calls
  `recommendations/openshift/{uuid}?include=explanation`.
- Optional chart unit test: reference line y-value matches explanation percentile prop.

**Dependency:** Phase 5a requires Phase 3 API (`?include=explanation`) deployed or
available in dev. Phase 5b/5c can follow incrementally; empty states degrade gracefully
when backend columns are NULL (pre-backfill).

---

## Recommended Execution Order

The plan spans two repositories (`ros-ocp-backend` and `koku-ui`). The recommended
order delivers end-to-end explainability for the most common case (container
CPU/memory) as early as possible, then iterates:

**Critical path (container end-to-end):**

Phase 1 → Phase 2a → Phase 3 → Phase 5a

This gives users a working "Why this recommendation?" panel for container
CPU/memory recommendations in ~6 days.

**Parallel / follow-up work:**

| Order | Phase | Depends on | Effort |
|-------|-------|------------|--------|
| 1 | Phase 1 (migration + types) | — | 1 day |
| 2 | Phase 2a (container + namespace) | Phase 1 | 2 days |
| 3 | Phase 3 (API opt-in) | Phase 2a | 1 day |
| 4 | Phase 5a (UI panel, container) | Phase 3 | 2 days |
| 5 | Phase 2b (node + PVC) | Phase 1 | 1.5 days |
| 6 | Phase 0 (GPU TS table) | — | 2–3 days |
| 7 | Phase 2c (GPU + VM + TS) | Phase 0 + Phase 1 | 2 days |
| 8 | Phase 2d (quota + CRQ + snapshot) | Phase 1 | 1.5 days |
| 9 | Phase 5b (chart annotations) | Phase 5a | 1.5 days |
| 10 | Phase 5c (multi-type rendering) | Phase 2b–2d + Phase 5a | 2 days |
| 11 | Phase 2.5 (backfill) | Phase 2a–2d | 2 days |
| 12 | GPU recompute removal (cleanup) | Phase 2.5 complete for GPU rows | 0.5 day |
| **Total** | | | **~19–20 days** |

Phase 0 and Phase 2b–2d can run in parallel with the critical path once Phase 1
ships. Phase 5b/5c follow incrementally after Phase 5a.

---

## Testing Approach

### Unit tests (engine)

- Extend existing engine tests (`recommend_cpu_test.go`, `pvc_recommend_test.go`,
  `recommend_nodes_test.go`, `vm_recommender_test.go`, `recommend_quota_test.go`)
  to assert explanation fields on rec structs match known inputs
- Table-driven tests for classification branches (PVC orphaned vs oversized,
  quota tighten vs optimal, VM hysteresis held)

### Integration tests (DB round-trip)

- Insert recommendation via `Write*` functions; SELECT explanation columns;
  assert non-null for synthetic digest fixtures
- Verify history archive copies explanation columns
- Pattern: follow `recommend_all_test.go` / `WritePVCRecommendations_BatchUpsert`

### API contract tests

- Extend `handlers_*_integration_test.go` detail fixtures
- Assert `explanation` present when `?include=explanation` and omitted otherwise
- Assert `explanation` omits from list responses (ADR-0294 compliance)

### Migration test

- `tests/migrations/` or manual: `migrate up 000145`, verify `\d recommendation_sets`
  and `\d recommendation_history` show new columns, `migrate down` drops cleanly

### Backfill tests

- Unit test backfill logic: engine re-run writes full recommendation row including
  explanation columns; recommendation values match (or are negligibly different
  due to time drift)
- Integration test: NULL explanation row → backfill → columns populated,
  recommendation values consistent with full recompute
- Concurrency test: `--concurrency N` partitions by `cluster_uuid` without
  duplicate work or data races

### Frontend tests (`koku-ui`)

- **Unit tests:** `RecommendationExplanation.test.tsx` — panel show/hide based on
  explanation data; labeled factors for container CPU/memory fixtures; empty-state
  copy when explanation absent or all-null.
- **API unit test:** Extend `recommendations.test.ts` — detail GET includes
  `?include=explanation` when breakdown fetch requests explanations.
- **Optional:** Chart annotation test in `optimizationsBreakdownChart.test.tsx` —
  reference line y-coordinate matches passed percentile value.
- **Manual:** On breakdown page (`/optimizations/details/breakdown?id=…`), verify
  expandable panel, chart reference line, confidence badge, and graceful empty state
  before backfill completes.

---

## Rollout

1. Deploy migration (additive, nullable — zero downtime; includes history tables)
2. Deploy engine changes — new rows get explanations; old rows stay NULL
3. Deploy API with `?include=explanation` — UI opts in when ready
4. Run backfill (Phase 2.5) — recompute full recommendations (values + explanations)
   for existing rows
5. **GPU recompute removal (cleanup PR):** After backfill completes for GPU rows,
   verify with:
   ```sql
   SELECT COUNT(*) FROM recommendation_sets WHERE has_gpu AND expl_gpu_sm_active_avg_bp IS NULL;
   ```
   When the count is zero, submit a cleanup PR that removes the digest recompute
   fallback in `internal/api/gpu_enrichment.go`. This is a separate PR — do not
   mix with feature work.

New rows receive explanations automatically on ingestion. History snapshots
include explanation factors from the moment Phase 2 ships.

---

## Security Considerations

### Backfill endpoint: admin-only access

The backfill management endpoint (Phase 2.5) triggers expensive recomputation
across all recommendation rows. It MUST:

- Require admin authentication (not accessible to regular org users)
- Be rate-limited or one-shot (prevent repeated triggers as a compute DoS vector)
- Live on the internal admin API (same as other management endpoints), not the
  public user-facing API
- Log invocations with caller identity for audit

### `?include=` parameter: whitelist validation

The `include` query parameter parser MUST whitelist known values and silently
ignore unknown ones:

```go
allowed := map[string]bool{"explanation": true}
for _, v := range strings.Split(includeParam, ",") {
    if allowed[strings.TrimSpace(v)] { ... }
}
```

This prevents injection of arbitrary values. Unknown includes are ignored, not
rejected — forward-compatible for when new includable sections are added.

### Algorithm transparency: accepted risk

Explanation factors reveal internal algorithm details (percentile choices, margin
multipliers, OOM bump logic, threshold values). This is intentional — the project
is open-source and users benefit from transparency. No information beyond what is
already available in the source code is exposed.

---

## Performance and Scale

### Engine computation overhead: none

Explanation factors are values already computed internally during recommendation
generation (percentiles, margins, peaks, OOM counts). We are persisting values
that were previously discarded. Zero additional computation at recommendation time.

### API response size: opt-in only

The `?include=explanation` parameter is opt-in. Default responses are unchanged.
No performance impact on existing consumers.

### Migration speed: instant

`ADD COLUMN ... NULL` on PostgreSQL is metadata-only (instant), even on large
partitioned tables. No table rewrite required.

### Row width increase: acceptable

Each recommendation table gains ~6–10 nullable typed columns (float64, int, short
strings), adding ~80–120 bytes per row when populated. This is far more compact
than JSONB alternatives and negligible relative to the existing row width.

### History table growth

History tables already have a retention policy that limits how long rows are kept.
Explanation columns add ~10% more storage per history row — a rounding error
relative to the row itself.

Future optimizations (not in scope for this plan, but noted for reference):
- **PostgreSQL table partitioning by age** — partition history by month, drop old
  partitions efficiently
- **Compression** — PostgreSQL TOAST already compresses large values; for further
  gains, consider TimescaleDB compression or pg_partman with tablespace tiering
- **Archival** — move history older than N months to a read-only historian/cold
  storage if query patterns warrant it

None of these are needed today given the current data volumes and retention policy.

### Backfill compute cost: one-time, bounded

Backfill is a one-time operation. Per the native engine benchmark
(https://pgarciaq.github.io/ros-ocp-backend/operations/benchmark-report/),
full recommendation recomputation completes in ~35 minutes for the current dataset.
Adding explanation factor capture to that recomputation adds negligible overhead
(it's the same computation, just persisting more output columns).

The backfill endpoint recomputes recommendations with explanation factors enabled,
replacing existing recommendation rows. This is safe because the algorithm is
deterministic — recomputation produces identical recommendations plus the new
explanation columns.

### Digest data availability: not a concern

Backfill recomputes recommendations from the existing digest data in the database.
Since we retain digests for the same duration as recommendations, all current
recommendation rows have their source digests available. No data loss scenario
exists for the backfill.

### Query performance: no impact

Explanation columns are never used in WHERE clauses, JOIN conditions, or ORDER BY.
GORM always SELECTs all struct fields (including `expl_*` columns) in one query;
the serializer omits the `explanation` JSON object unless `?include=explanation`
is set. No index pressure, no query plan changes for existing queries, and no
second query path to maintain.

---

## Resolved Decisions

| # | Question | Decision |
|---|----------|----------|
| 1 | Column prefix convention | **`expl_` prefix** for all new explanation columns |
| 2 | PVC duplicate columns | **No duplicates** — API assembles `usage_ratio` and `growth_bytes_per_day` from existing columns; only add `expl_*` for values not already stored |
| 3 | Node GPU time-slicing | **Prerequisite for Phase 2c** — create `node_gpu_timeslicing_recommendations` table in Phase 0; explanation columns added after table exists |
| 4 | Business hours | Show explanation for BH rows if the row exists; **UI hides BH section** when BH scheduling is not configured — no special backend handling |
| 5 | Confidence column | Use existing **`confidence_level`** column — do not add `expl_confidence` |
| 6 | Struct safety | **Embed explanation fields in engine rec structs** — do not copy to a separate `Explanation` type |
| 7 | GPU read path | **Persist at classification; stop read-time recompute** — digest fallback only for NULL rows during backfill window |
| 8 | API inclusion | **`include` query param (comma-separated list) opt-in** — `explanation` only in v1; not included by default on detail responses |
| 9 | History tables | **Include explanation columns** on history tables; archive explanation with recommendation |
| 10 | Backfill | **One-shot backfill pass** (Phase 2.5) recomputes full recommendations (values + explanations); supports `--concurrency N` partitioned by `cluster_uuid`; triggered via management endpoint or CLI |

---

## File Checklist

| Area | Files to touch |
|------|----------------|
| Migration | `migrations/000145_recommendation_explanation_columns.{up,down}.sql` (live + history tables) |
| Phase 0 | New migration for `node_gpu_timeslicing_recommendations`, node GPU TS persist + handler |
| Engine types | `internal/engine/types.go` (extend `CPURec`, `MemoryRec`, `NodeRec`, `GPURec`, etc.) |
| Engine logic | `recommend_cpu_and_memory.go`, `recommend_all.go`, `recommend_namespace.go`, `recommend_nodes.go`, `pvc_recommend.go`, `gpu_recommender.go`, `gpu_query.go`, `vm_recommender.go`, `vm_db.go`, `recommend_quota.go`, `recommend_cluster_quota.go`, `snapshot_classify.go` |
| Backfill | New CLI command or management endpoint (e.g. `cmd/backfill-explanations/` or admin handler) |
| Models | `internal/model/recommendation_set_native.go`, `namespace_recommendation_set_native.go`, `node_cpu_mem_recommendation.go`, `detail_response.go`, `vm_recommendation.go` |
| API handlers | `handlers_pvc.go`, `handlers_vm_recs.go`, `handlers_quota_recs.go`, `handlers_node_utilization.go`, `handlers_gpu_mig.go`, `handlers_snapshot.go` (+ `include` query param parsing) |
| OpenAPI | `docs/openapi/` |
| Tests | `internal/engine/*_test.go`, `internal/api/handlers_*_integration_test.go` |
| Docs | ADR-0296 (this plan), optional `docs-site/` UI section |
| Docs-site | `docs-site/architecture/understanding-recommendations.md` (new), `docs-site/operations/` backfill section, `docs-site/ui-integration-guide.md` update |
| MkDocs nav | `mkdocs.yml` — add `architecture/understanding-recommendations.md` under Architecture; add backfill section under Operations (when Phase 4 docs are written) |
| Frontend (`koku-ui`) | `apps/koku-ui-ros/src/api/ros/recommendations.ts`, `recommendations.test.ts`, `store/ros/rosActions.ts`, `routes/optimizations/optimizationsBreakdown/optimizationsBreakdown.tsx`, `RecommendationExplanation.tsx`, `optimizationsBreakdownUtilization.tsx`, `optimizationsBreakdownChart.tsx`, `locales/messages.ts` |
