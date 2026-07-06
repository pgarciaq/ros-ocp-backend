# Performance Audit Report v3: ros-ocp-backend Native Engine

## Date and Scope

**Date:** July 5, 2026
**Branch:** `pgarciaq-rosocp-superpowers-phase15` (HEAD `00d3b246`)
**Prior audit:** [`native-engine-audit-v2-2026-06.md`](native-engine-audit-v2-2026-06.md)
**Scope:** Follow-up audit across all 11 dimensions — regression verification on prior "Do Not Regress" items, phase14-15 new code (replica optimization, per-pod CV, node/VM hourly heatmaps, GPU MIG persistence, category classification, fleet heatmap, quota trend, snapshot cost aggregation, rate limiting, CSV sanitization, security enforcement), deferred-item trigger review, and new optimization opportunities.

**Deployment modes considered:** SaaS (multi-tenant, RDS, ingress ~30s budget) and on-prem (single-tenant PostgreSQL, 512Mi–8Gi chart profiles, NetworkPolicy-isolated internal APIs).

**Commits reviewed:** 239 commits since June 15, 2026.

---

## Prior Audit Status

The v2 audit (June 15, 2026) reported **all P0/P1 items implemented** (DB-N1 batched savings, API-N1 page-scoped GPU enrichment, DB-N2 batched tag sync). Strategic items **S1–S3** and profiling-gated deferrals (**G-3, B-3, B-6 PGO, A-5, I-1 partial**) remained open with documented revisit triggers.

**Phase14-15 additions verified in this audit:**

| Area | Status |
|------|--------|
| Per-pod CV for StatefulSet confidence (`computeCPUUsageCVBP`) | Implemented — new allocation concern (DIGEST-1) |
| Replica count optimization (`ComputeRecommendedReplicas`) | Implemented |
| Category classification engine (`ClassifyResource`, `ClassifyOverall`) | Implemented — integer comparisons, correct |
| Node hourly utilization heatmap (`hourly_node_digests`) | Implemented — new DDL concern (NODE-1) |
| VM hourly activity heatmap (`hourly_vm_digests`) | Implemented — new allocation concern (VM-1) |
| GPU MIG recommendation persistence (`gpu_mig_persist.go`) | Implemented — new capacity concern (GPU-1) |
| Fleet heatmap endpoint with LRU cache | Implemented — working well |
| Per-org rate limiting middleware | Implemented — working well (ceiling at ~500 req/s) |
| CSV formula injection sanitization (`csv_sanitize.go`) | Implemented |
| Graduated security enforcement (`security.go`) | Implemented |
| Context propagation through CSV processors | Implemented |
| Safety LIMIT on fleet heatmap | Implemented |
| `hashicorp/golang-lru/v2` migration | Implemented |
| Quota headroom trend endpoint | Implemented — new concerns (PERF-01, PERF-04) |
| Snapshot cost-by-type aggregation | Implemented — missing timeout (PERF-05) |
| GPU MIG SQL keyset pagination | Implemented — working well |
| Business hours plots overlay | Implemented |
| Stale filter unification through `org_container_keys` | Implemented |

---

## Regression Check (Do Not Regress items)

Each item from the v2 audit's "What Is Working Well" list was re-verified. **No regressions found.**

| Pattern | Location | Verified |
|---------|----------|----------|
| `DigestRow` int64 data plane | `internal/engine/types.go` | ✅ |
| Percentiles at ingest | `internal/ingestion/digest.go` | ✅ |
| `MarginScale` / `ApplyScaledMargin` | `internal/engine/margin_scaled.go` | ✅ |
| GPU classification int BP | `internal/engine/gpu_recommender.go` | ✅ |
| Streaming recommend `streamBatchSize = 500` | `internal/engine/recommend_all.go` | ✅ |
| `sync.Pool` digest buffers | `internal/ingestion/digest.go` | ✅ |
| `pgx.Batch` container/namespace/PVC/GPU writes | Multiple files | ✅ |
| Cost LRU cache | `internal/costdata/cache.go` — migrated to `hashicorp/golang-lru/v2` | ✅ |
| Zero-copy `windowBounds` | `internal/engine/window_bounds.go` | ✅ |
| Fused `RecommendCPUAndMemory` | `internal/engine/recommend_cpu_and_memory.go` | ✅ |
| Decay lookup table | `internal/engine/decay.go`, `decay_table.go` | ✅ |
| Deferred `RefreshOrgMetadata` | `report_processor.go`, `recommend_all.go` | ✅ |
| `org_container_keys` list pagination | `getNativeRecommendationsFromOrgKeys` | ✅ |
| Integer micro-cents savings | `internal/engine/savings_int.go` | ✅ |
| Graceful Kafka shutdown drain | `internal/services/` | ✅ |
| Bounded Prometheus labels | `internal/metrics/metrics.go` | ✅ |
| Slim list + typed `Collection[T]` | `internal/model/list_response.go` | ✅ |
| Identity parsed once | `internal/api/middleware/identity.go` | ✅ |
| Batched savings recalc (DB-N1) | `internal/engine/savings_recalculate.go` | ✅ |
| Page-scoped GPU enrichment (API-N1) | `internal/api/gpu_enrichment.go` | ✅ |
| Batched tag sync (DB-N2) | `internal/tags/sync.go` | ✅ |
| `org_namespace_keys` pagination (DB-N3) | Migration 000153 | ✅ |

---

## Overall Assessment

Phase14-15 adds significant new functionality (hourly heatmaps, GPU MIG persistence, category classification, replica optimization, fleet heatmap with cache, rate limiting) without regressing core engine optimizations. The **container recommendation hot path remains integer-first** with fused passes and decay lookup tables.

**New bottlenecks** are concentrated in:
1. **Allocation pressure** from `computeCPUUsageCVBP` (DIGEST-1) — 750k heap objects per reconcile
2. **Per-row UPDATE loops** for GPU timeslicing cross-refs (DB-004)
3. **Missing statement timeouts** on 5+ new heavy handlers (DB-001)
4. **Quota key resolution** doing a full org scan on every trend request (PERF-01)
5. **PVC decay** still bypassing the lookup table (PRE-2) — 135k `math.Exp` calls

Strategic deferrals **S1–S3** remain appropriate. No trigger conditions met.

---

## What Is Working Well (Updated)

Prior list items remain valid. **Phase14-15 additions:**

- **Fleet heatmap LRU cache** (`internal/fleetheatmap/cache.go`) — `expirable.LRU` with configurable TTL/size, org-level invalidation wired to 9 code paths. Safety `LIMIT` on the SQL query prevents unbounded result sets.
- **Category classification** (`internal/engine/category.go`) — pure integer comparisons against constant thresholds; zero allocations.
- **GPU MIG keyset pagination** — SQL-level cursor pagination for GPU timeslicing list; no `DISTINCT ON`.
- **`hashicorp/golang-lru/v2` migration** — typed LRU caches replacing hand-rolled implementations; correct eviction and TTL.
- **Rate limiter health bypass** — `/healthz`, `/readyz`, `/status` exempt from rate limiting.
- **`computeCPUUsageCVBP` output** stored as `*int64` basis points — float64 confined inside function.
- **Node digest accumulator** — uses `[nodeDayHours]int64` fixed arrays with pre-allocated sample slices.
- **`latestReplicaCounts`** — O(n) pass using `.After()` on pre-aggregated `DigestRow.BucketDate`.
- **GPU MIG batch writes** — `pgx.Batch` with `maxPgxBatchQueue = 500` chunking.
- **Context threading** in `runContainerRecommendations` and `runManifestRecommendations` — `ctx` properly threaded through all CSV processing functions.
- **Stale filter unification** — all container list paths route through `org_container_keys` keyset.
- **Echo Prometheus `url` label** uses route template (bounded cardinality).

---

## New Findings

### P1 — High

#### DIGEST-1. `computeCPUUsageCVBP` allocates fresh maps per container-day

| Field | Value |
|-------|-------|
| **ID** | DIGEST-1 |
| **Severity** | P1 |
| **Location** | `internal/ingestion/digest.go:493-546` — `computeCPUUsageCVBP` |
| **Current state** | Every invocation allocates `podHourUsage` (`map[podHourKey]int64`), `hourPods` (`map[hourKey]map[string]struct{}`), plus a per-hour `[]float64` slice grown without capacity. The nested `hourPods` map additionally allocates one inner map per distinct hour. |
| **Quantification** | 1,000 containers × 30-day window = 30,000 calls/reconcile. Each: ≥2 outer maps + up to 24 inner maps + 24 float64 slices = **~750,000 heap allocations per reconcile**. Also `math.Sqrt` up to 24×/call = 720,000 calls (secondary). |
| **Proposed fix** | Add a pool of `scratchCVBuffers` (similar to `weightedDigestScratchPool`). Pre-allocate maps, clear with range-delete before reuse. Pre-allocate `values` with `make([]float64, 0, 8)`. |
| **Expected impact** | Eliminates ~750k heap objects per reconcile; directly reduces GC pause frequency. |
| **Risk** | Medium — scratch pool must correctly reset between calls. |
| **Effort** | M (2–3 days) |

---

#### DB-001. Missing statement timeouts on new heavy handlers

| Field | Value |
|-------|-------|
| **ID** | DB-001 |
| **Severity** | P1 |
| **Location** | `handlers_node_hourly.go`, `handlers_vm_hourly.go`, `handlers_fleet_heatmap.go`, `handlers_gpu_timeslicing_history.go`, `handlers_snapshot_cost.go` |
| **Current state** | Only `handlers_savings_summary.go` uses `WithHeavyStatementTimeout`. All new handlers rely on the 25s session default. A runaway query could hold a connection for 25s instead of aborting at the heavy-query threshold. |
| **Quantification** | 5+ new handler functions missing explicit timeout. Under load, one misbehaving query could exhaust the pool (`ROS_DB_MAX_CONNS=5`). |
| **Proposed fix** | Add `db.WithHeavyStatementTimeout` (or `WithHeavyGORMStatementTimeout`) to all new heavy-query handlers. Copy pattern from `handlers_savings_summary.go`. |
| **Expected impact** | Prevents connection pool exhaustion from slow queries; bounds worst-case latency. |
| **Risk** | Low — existing pattern, copy-paste. |
| **Effort** | S (hours) |

---

#### DB-004. GPU timeslicing cross-refs: per-container UPDATE loop

| Field | Value |
|-------|-------|
| **ID** | DB-004 |
| **Severity** | P1 |
| **Location** | `internal/engine/gpu_mig_persist.go` — `updateTimeslicingCandidateCrossRefs` |
| **Current state** | One `UPDATE recommendation_sets SET time_slicing_node = $1 WHERE ...` per candidate container in a loop. |
| **Quantification** | A GPU cluster with 50 timeslicing candidates = 50 UPDATE round-trips per reconcile. |
| **Proposed fix** | Replace with `pgx.Batch` or VALUES-based bulk UPDATE (same pattern as batched savings recalc). |
| **Expected impact** | 10–50× reduction in timeslicing cross-ref write time. |
| **Risk** | Low — established pattern. |
| **Effort** | S (hours) |

---

#### PERF-01. `ResolveQuotaKeyByID` full org-table scan

| Field | Value |
|-------|-------|
| **ID** | PERF-01 |
| **Severity** | P1 |
| **Location** | `internal/model/` — quota trend handler |
| **Current state** | Fetches all `quota_recommendation_sets` for the org without LIMIT, then scans in Go to resolve a single quota key by ID. |
| **Quantification** | At 500 quota entries, fetches all 500 rows to find 1. O(n) per trend request. |
| **Proposed fix** | Add a direct `WHERE` clause with the quota ID, or add a `quota_id` generated column with a unique index. |
| **Expected impact** | O(1) lookup instead of O(n) scan; eliminates unnecessary row fetching. |
| **Risk** | Low. |
| **Effort** | M (2–3 days) |

---

#### PRE-2. PVC growth slope still calls `math.Exp` directly (pre-existing)

| Field | Value |
|-------|-------|
| **ID** | PRE-2 |
| **Severity** | P1 (upgraded from P2 — quantification shows higher volume than estimated in v2 audit) |
| **Location** | `internal/engine/pvc_recommend.go:368` — `computePVCGrowthSlopeWLS` |
| **Current state** | Direct `math.Exp(-lambda * ageHours)` per digest row. The container engine uses `DecayWeight` (lookup table); the PVC engine does not. |
| **Quantification** | 500 PVCs × 3 terms × 90 digest rows = **135,000 `math.Exp` calls** per reconcile. |
| **Proposed fix** | Call `engine.DecayWeight(ageHours, halfLifeHours)` which hits the precomputed table. |
| **Expected impact** | Eliminates 135k `math.Exp` calls; consistency with container path. |
| **Risk** | Low — `DecayWeight` already handles the same math. |
| **Effort** | S (hours) |

---

### P2 — Medium

#### VM-1. `vmHourlyAccumulator` slices initialized nil

| Field | Value |
|-------|-------|
| **ID** | VM-1 |
| **Severity** | P2 |
| **Location** | `internal/ingestion/vm_hourly_digest.go:35-39` |
| **Current state** | `cpuUsage`, `memUsage`, `diskReadIOPS`, `diskWriteIOPS` declared as `[]float64` with zero capacity. First append triggers 3 reallocations per bucket (1→2→4 for typical 4-sample hour). |
| **Quantification** | 200 VMs × 24 hours × 4 slices × 3 reallocations = **57,600 unnecessary slice copies** per ingest. |
| **Proposed fix** | Add `newVMHourlyAccumulator()` with `make([]float64, 0, 4)`. |
| **Expected impact** | Eliminates all reallocation copies during VM hourly digest. |
| **Risk** | Low. |
| **Effort** | S |

---

#### NODE-1. `EnsureHourlyNodeDigestPartitions` DDL on every ingest without caching

| Field | Value |
|-------|-------|
| **ID** | NODE-1 |
| **Severity** | P2 |
| **Location** | `internal/ingestion/node_hourly_digest.go:98`, `vm_hourly_digest.go:133` |
| **Current state** | DDL `CREATE TABLE IF NOT EXISTS` fired before every `pgx.Batch` write; no process-level cache. Partition DDL serializes in PostgreSQL. |
| **Quantification** | 2 DDL statements per ingest event × 10 workers = 20 serialized DDL round-trips/minute. |
| **Proposed fix** | `sync.Map[string, struct{}]` cache (same pattern as `decay_table.go`). Skip DDL when partition already created in this process lifetime. |
| **Expected impact** | Reduces DDL round-trips to 0 after first ingest of each month. |
| **Risk** | Low. |
| **Effort** | S |

---

#### GPU-1. `PersistGPUMIGRecommendationSets` writes slice without capacity hint

| Field | Value |
|-------|-------|
| **ID** | GPU-1 |
| **Severity** | P2 |
| **Location** | `internal/engine/gpu_mig_persist.go:120` |
| **Current state** | `var writes []gpuMIGRecSetWrite` (nil). Grows through ~9 reallocations for 150 writes. |
| **Proposed fix** | `writes := make([]gpuMIGRecSetWrite, 0, len(gpuRecs)*3)` |
| **Effort** | S |

---

#### DB-002. Hourly digest retention uses row-level DELETE instead of partition DROP

| Field | Value |
|-------|-------|
| **ID** | DB-002 |
| **Severity** | P2 |
| **Location** | Retention logic for `hourly_node_digests` and `hourly_vm_digests` |
| **Current state** | Tables are range-partitioned monthly but retention uses row-level DELETE. |
| **Proposed fix** | Use `SweepPartitionedTables` for complete months (same as `container_usage_samples`). |
| **Effort** | M |

---

#### DB-003. New high-UPSERT tables lack autovacuum tuning

| Field | Value |
|-------|-------|
| **ID** | DB-003 |
| **Severity** | P2 |
| **Location** | `hourly_node_digests`, `hourly_vm_digests`, `gpu_mig_recommendation_sets`, `node_gpu_timeslicing_recommendations` |
| **Current state** | Default 20% dead-tuple threshold before autovacuum. These tables get frequent UPSERTs. |
| **Proposed fix** | Migration adding `autovacuum_vacuum_scale_factor=0.05`, `fillfactor=85` (same pattern as migration 000144). |
| **Effort** | M |

---

#### DB-005. `appendNodeGPUTimeslicingHistory` inserts one row at a time

| Field | Value |
|-------|-------|
| **ID** | DB-005 |
| **Severity** | P2 |
| **Location** | `internal/engine/gpu_mig_persist.go` |
| **Proposed fix** | Replace with `pgx.Batch` (same pattern already used in the same file). |
| **Effort** | S |

---

#### DB-006. GPU timeslicing history list: non-sargable OR pattern

| Field | Value |
|-------|-------|
| **ID** | DB-006 |
| **Severity** | P2 |
| **Location** | `ListNodeGPUTimeslicingRecommendationHistory` query |
| **Current state** | `$4 = '' OR gpu_model = $4` prevents full composite index usage. |
| **Proposed fix** | Build WHERE clause dynamically (omit `gpu_model` condition when empty). |
| **Effort** | S |

---

#### DB-007. Fleet heatmap RBAC filters in Go after fetching 1000 rows

| Field | Value |
|-------|-------|
| **ID** | DB-007 |
| **Severity** | P2 |
| **Location** | Fleet heatmap handler |
| **Current state** | Fetches `maxNodes+1` rows then discards non-RBAC-permitted nodes in Go. |
| **Proposed fix** | Push node allow-list into SQL `WHERE` as `AND nr.node = ANY($6)` when RBAC restricts nodes. |
| **Effort** | M |

---

#### DB-008. `LoadPersistedGPUTimeslicingCrossRefs` missing partial index

| Field | Value |
|-------|-------|
| **ID** | DB-008 |
| **Severity** | P2 |
| **Location** | Query on `recommendation_sets` with `time_slicing_node <> ''` |
| **Proposed fix** | Add partial index `CREATE INDEX CONCURRENTLY ... ON recommendation_sets(...) WHERE time_slicing_node <> ''`. |
| **Effort** | S |

---

#### DB-009. `*_recommendation_quality` tables: no retention, no autovacuum

| Field | Value |
|-------|-------|
| **ID** | DB-009 |
| **Severity** | P2 |
| **Location** | Four `*_recommendation_quality` tables |
| **Current state** | Partitioned but not in `retention.go`; no autovacuum tuning. Disk grows unbounded. |
| **Proposed fix** | Add to `retainedTables` + autovacuum migration. |
| **Effort** | S |

---

#### PERF-03. Node/VM hourly queries missing statement timeout

| Field | Value |
|-------|-------|
| **ID** | PERF-03 |
| **Severity** | P2 |
| **Location** | Node hourly, VM hourly handlers |
| **Proposed fix** | Wrap in `WithStatementTimeout`. |
| **Effort** | S |

---

#### PERF-04. Quota trend and OOM timeline: unbounded date range

| Field | Value |
|-------|-------|
| **ID** | PERF-04 |
| **Severity** | P2 |
| **Location** | Quota trend, OOM timeline handlers |
| **Current state** | Accept arbitrary date ranges with no span cap and no LIMIT clause. |
| **Proposed fix** | Cap max date range to 90 days; add LIMIT. |
| **Effort** | S |

---

#### PERF-07. `getClustersForOrg` extra DB query per hourly detail request

| Field | Value |
|-------|-------|
| **ID** | PERF-07 |
| **Severity** | P2 |
| **Location** | Node/VM hourly detail handlers |
| **Proposed fix** | Fold into the main hourly query or reuse fleet-summary cache. |
| **Effort** | M |

---

#### PERF-08. Fleet heatmap node-level RBAC filtering in Go

| Field | Value |
|-------|-------|
| **ID** | PERF-08 |
| **Severity** | P2 |
| **Location** | Fleet heatmap RBAC filter |
| **Proposed fix** | Push allow-list into SQL WHERE. |
| **Effort** | M |

---

#### PERF-11. VM CSV and history CSV bypass `sanitizeCSVRow`

| Field | Value |
|-------|-------|
| **ID** | PERF-11 |
| **Severity** | P2 (security/correctness, not performance) |
| **Location** | VM CSV export, history CSV export handlers |
| **Proposed fix** | Apply `sanitizeCSVRow` (two-line fix). |
| **Effort** | S |

---

### P3 — Low

#### DIGEST-2. `computeVariation` uses float64 for integer percentage

| Field | Value |
|-------|-------|
| **ID** | DIGEST-2 |
| **Severity** | P3 |
| **Location** | `internal/engine/recommend_all.go:530-536` |
| **Proposed fix** | Replace with integer ceiling: `int32((rec-current)*100 / current)`. |
| **Effort** | S |

---

#### VM-2. `BuildHourlyVMDigests` uses float64 sort + percentile

| Field | Value |
|-------|-------|
| **ID** | VM-2 |
| **Severity** | P3 |
| **Location** | `internal/ingestion/vm_hourly_digest.go:59-100` |
| **Current state** | Values are whole-number millicores/KiB stored as float64; sorts and percentiles use float64 path. |
| **Proposed fix** | Switch to `[]int64` and `percentileInt64` (already implemented in `node_digest.go`). |
| **Effort** | M |

---

#### GPU-2. `strings.SplitN` on composite key per GPU container

| Field | Value |
|-------|-------|
| **ID** | GPU-2 |
| **Severity** | P3 |
| **Location** | `internal/engine/gpu_mig_persist.go:122` |
| **Proposed fix** | Change map key to struct type. |
| **Effort** | S |

---

#### REPLICA-1. `ComputeRecommendedReplicas` uses float64 for integer ceiling

| Field | Value |
|-------|-------|
| **ID** | REPLICA-1 |
| **Severity** | P3 |
| **Location** | `internal/engine/replica_optimization.go:67-68` |
| **Proposed fix** | Integer ceiling: `(a + b - 1) / b`. |
| **Effort** | S |

---

#### PRE-1. `computeReplicaCounts` uses `time.Date` for hourKey comparison

| Field | Value |
|-------|-------|
| **ID** | PRE-1 |
| **Severity** | P3 |
| **Location** | `internal/ingestion/digest.go:673-674, 691-692` |
| **Quantification** | 30,000 container-days × 96 `time.Date` calls = **2.88M unnecessary allocations**. |
| **Proposed fix** | Add `func (h hourKey) isAfter(other hourKey) bool` with sequential field comparison. |
| **Effort** | S |

---

#### PERF-05. Snapshot cost-by-type missing `WithHeavyStatementTimeout`

| Field | Value |
|-------|-------|
| **ID** | PERF-05 |
| **Severity** | P3 |
| **Location** | Snapshot cost-by-type handler |
| **Effort** | S |

---

#### PERF-09. Rate limiter global mutex ceiling

| Field | Value |
|-------|-------|
| **ID** | PERF-09 |
| **Severity** | P3 |
| **Location** | `internal/api/middleware/rate_limiter.go` |
| **Current state** | Global `sync.Mutex` serializes all rate-limit checks. |
| **Quantification** | Ceiling at ~500 req/s on modern hardware. |
| **Proposed fix** | Monitor; replace with sharded map or `sync.Map` only if p99 degrades. |
| **Effort** | M (if needed) |

---

#### PERF-12. Fleet heatmap: conditional `fleet_reduction` CTE column

| Field | Value |
|-------|-------|
| **ID** | PERF-12 |
| **Severity** | P3 |
| **Location** | Fleet heatmap SQL |
| **Proposed fix** | Skip `fleet_reduction` CTE computation when not requested. |
| **Effort** | M |

---

## Deferred Items — Revisit Trigger Check

| ID | Item | Trigger (prior audit) | Met? | Assessment |
|----|------|----------------------|------|------------|
| **S1** | Unified windowed digest recommender | 6th recommendation type | **No** | Still 5 subsystems. |
| **S2** | Parallel container recommend by namespace | F-1 shows recommend phase >30s | **No evidence** | No production histograms available. |
| **S3** | Namespace recs from container rollups | Product rollup spec | **No** | Accuracy argument unchanged. |
| **G-3** | Distributed debouncer | Multiple processor pods | **No** | Still single-pod in typical deployments. |
| **B-3** | String interning for DigestKey | Memory profiling shows dup | **No** | No evidence. |
| **B-6 (PGO)** | Profile-guided optimization | CI supports PGO | **No** | Deferred. |
| **A-5** | Legacy Kruize `map[string]interface{}` | Legacy path retained | **N/A** | Still deprecated. |
| **I-1** | AWS SDK v1 removal | `platform-go-middlewares` drops v1 | **No** | v1 still indirect. |

---

## Accuracy Trade-off Register

| Trade-off | Introduced | Still valid? | Notes |
|-----------|------------|--------------|-------|
| Decay weight lookup quantization (~0.2% error) | P0-1 / ADR-0288 | ✅ | |
| Idle P95 → max-of-daily-P95 | P2-5 | ✅ | |
| Percentile-band plots (p50/p95/p99/max) | ADR-0292 | ✅ | |
| Separate sample vs digest retention | E-2 | ✅ | |
| Slim list contract (short_term cost only) | ADR-0294 | ✅ | |
| Savings integer micro-cents | ADR-0291 | ✅ | |
| PVC math.Exp weights (PRE-2, open) | ✅ Acceptable | Low volume per prior audit; upgraded to P1 by volume in this audit. |
| VM float64 sizing (ALG-N2) | ✅ | Low cardinality; accuracy-sensitive. |
| Statement timeout cancellation | Phase13 | ✅ | |
| `computeVariation` ±1 rounding (DIGEST-2) | Phase14 | ✅ New | Integer division differs by ≤1 from `math.Round`; does not affect 10% category bands. |

---

## ROI-Ordered Implementation Roadmap

### Quick Wins (hours each)

| Rank | ID | Title | Impact | Effort |
|------|-----|-------|--------|--------|
| 1 | **PRE-2** | PVC decay → lookup table | Eliminates 135k `math.Exp` | S |
| 2 | **DB-001** | Add statement timeouts to 5+ handlers | Prevents pool exhaustion | S |
| 3 | **DB-004** | Batch GPU timeslicing cross-ref UPDATEs | 10–50× write speedup | S |
| 4 | **NODE-1** | Cache hourly partition DDL | Eliminates 20 DDL/min | S |
| 5 | **VM-1** | Pre-allocate VM accumulator slices | Eliminates 57k copies | S |
| 6 | **GPU-1** | Capacity hint on writes slice | Eliminates 9 reallocations | S |
| 7 | **DB-005** | Batch timeslicing history INSERTs | Reduces round-trips | S |
| 8 | **DB-006** | Dynamic WHERE for timeslicing history | Sargable index usage | S |
| 9 | **PERF-04** | Cap date range on trend endpoints | Prevents unbounded queries | S |
| 10 | **PERF-11** | Apply CSV sanitization to VM/history | Security correctness | S |
| 11 | **PRE-1** | Integer hourKey comparison | Eliminates 2.88M `time.Date` | S |

### High-Value Investments (days each)

| Rank | ID | Title | Impact | Effort |
|------|-----|-------|--------|--------|
| 12 | **DIGEST-1** | Pool `computeCPUUsageCVBP` scratch buffers | Eliminates 750k allocs/reconcile | M |
| 13 | **PERF-01** | Direct quota key lookup by ID | O(1) instead of O(n) per trend | M |
| 14 | **DB-003/DB-009** | Autovacuum migration for new tables | Prevents table bloat | M |
| 15 | **DB-002** | Partition DROP for hourly digests | Faster retention, less WAL | M |
| 16 | **PERF-07** | Eliminate extra `getClustersForOrg` query | −1 DB round-trip per detail | M |
| 17 | **DB-007/PERF-08** | Push RBAC filter into SQL | Eliminates Go-side discard | M |
| 18 | **DB-008** | Partial index for timeslicing cross-refs | Faster cross-ref lookup | S |

### Defer (monitor or low ROI)

| Rank | ID | Title | Trigger |
|------|-----|-------|---------|
| 19 | **PERF-09** | Rate limiter mutex → sharded | Only if p99 degrades above 500 req/s |
| 20 | **VM-2** | VM hourly int64 migration | When VM volume justifies effort |
| 21 | **PERF-12** | Conditional fleet_reduction CTE | Minor; only for large fleets |
| 22 | **DIGEST-2** | Integer `computeVariation` | Free but trivial impact |
| 23 | **GPU-2** | Struct key for GPU MIG map | Code cleanliness |
| 24 | **REPLICA-1** | Integer ceiling replicas | Negligible |

---

## Appendix: Call Count Estimates (Updated)

### Container reconciliation (1,000 containers, 30-day lookback)

| Phase | Operations | Notes |
|-------|-----------|-------|
| Load term config | 1 (cached) | |
| Stream digests + CV computation | 30,000 `computeCPUUsageCVBP` calls | **750k heap objects** (DIGEST-1) |
| Recommend compute | 45,000 decay lookups | Table hits (correct) |
| Category classify | 24,000 integer comparisons | Correct |
| Variation compute | 24,000 float64 round-trips | DIGEST-2 (low priority) |
| Replica optimization | 1,200 float64 ceilings | REPLICA-1 (low priority) |
| Write batches | ~6 `pgx.Batch` sends | Correct |
| GPU MIG persist | 1 `pgx.Batch` + 50 cross-ref UPDATEs | DB-004 |
| `RefreshOrgMetadata` | 2 | Correct |

### PVC reconciliation (500 PVCs, 3 terms, 90-day window)

| Phase | Operations | Notes |
|-------|-----------|-------|
| Growth slope computation | **135,000 `math.Exp` calls** | PRE-2 — should be 0 with table |

### Ingest (VM hourly heatmap, 200 VMs, 24 hours)

| Phase | Operations | Notes |
|-------|-----------|-------|
| Accumulate samples | 57,600 slice reallocation copies | VM-1 |
| Ensure partitions | 2 DDL round-trips (wasteful after 1st) | NODE-1 |
| Batch write | 1 `pgx.Batch` | Correct |

---

## Summary

| Severity | New findings | Pre-existing |
|----------|-------------|---|
| P1 | 4 | 1 (PRE-2 upgraded) |
| P2 | 16 | 0 |
| P3 | 8 | 1 (PRE-1) |
| **Regressions** | **0** | |

**Top 5 ROI items:**
1. **PRE-2** — PVC decay lookup table (135k `math.Exp` eliminated, S effort)
2. **DB-001** — Statement timeouts on new handlers (prevents pool exhaustion, S effort)
3. **DIGEST-1** — Pool CV scratch buffers (750k allocs eliminated, M effort)
4. **DB-004** — Batch timeslicing UPDATEs (50 round-trips → 1, S effort)
5. **NODE-1** — Cache partition DDL (20 DDL/min → 0, S effort)

**No regressions** on the prior audit's "Do Not Regress" list. Phase14-15 changes are additive and align with the performance strategy established in the first audit.

---

## Implementation Status

**Quick wins implemented (July 5, 2026, commit `bb356bd4`):**

| Finding | Status | Commit |
|---------|--------|--------|
| PRE-2 (PVC decay → lookup table) | Implemented | `bb356bd4` |
| PRE-1 (Integer hourKey comparison) | Implemented | `bb356bd4` |
| VM-1 (Pre-allocate VM accumulator slices) | Implemented | `bb356bd4` |
| NODE-1 (Cache hourly partition DDL) | Implemented | `bb356bd4` |
| GPU-1 (Capacity hint on writes slice) | Implemented | `bb356bd4` |
| DB-004 (Batch GPU timeslicing cross-ref UPDATEs) | Implemented | `bb356bd4` |
| DB-005 (Batch timeslicing history INSERTs) | Implemented | `bb356bd4` |
| DB-006 (Dynamic WHERE for timeslicing history) | Implemented | `bb356bd4` |
| DB-001 (Statement timeouts on new handlers) | Implemented | `bb356bd4` |
| PERF-03 (Node/VM hourly statement timeout) | Implemented | `bb356bd4` |
| PERF-04 (Date range cap at MaxLookbackDays) | Implemented | `bb356bd4` |
| PERF-05 (Snapshot cost-by-type statement timeout) | Implemented | `bb356bd4` |
| PERF-06 (Cap bucket_boundaries input) | Implemented | `bb356bd4` |
| PERF-11 (CSV sanitization on VM/history) | Implemented | `bb356bd4` |

**Database tuning migrations (July 5, 2026, migration 000168-000169):**

| Finding | Status | Migration |
|---------|--------|-----------|
| DB-003 (Autovacuum + fillfactor on UPSERT tables) | Implemented | `000168` |
| DB-008 (Partial index for timeslicing cross-refs) | Implemented | `000169` |
| DB-009 (Autovacuum on quality tables) | Implemented | `000168` |

Notes:
- DB-009 retention was already handled (`historyRetainedTables` in `retention.go`). Only autovacuum tuning was missing.
- Partitioned tables: settings applied to child partitions only (PG rejects reloptions on partitioned parents).
- Quality tables get `autovacuum` only (no fillfactor — they're INSERT-only).
- Runtime partition inheritance: `EnsureHourlyNodeDigestPartitions` and
  `EnsureHourlyVMDigestPartitions` now apply reloptions to new partitions
  at creation time (commit `2b28f1b8`), closing the gap where future months
  would silently revert to defaults.

**Remaining (require deeper refactoring or profiling data):**

| Finding | Status | Notes |
|---------|--------|-------|
| DIGEST-1 (Pool computeCPUUsageCVBP scratch buffers) | Implemented | `sync.Pool` with inner-map free-list. 256 → 1 alloc/op (99.6%), 89 KB → 12 B/op (99.99%), ~32% faster. |
| DB-002 (Partition DROP for hourly digest retention) | Open | M effort |
| PERF-01 (ResolveQuotaKeyByID full scan) | Open | M effort, combine with PROF-2 |
| PERF-02 (Rate limiter: sync.Map vs sharded) | Open | S effort, needs benchmarking |
| PERF-07 (Eliminate extra getClustersForOrg query) | Open | M effort |
| DB-007/PERF-08 (Push RBAC filter into SQL) | Open | M effort |
| PERF-09, PERF-12 | Open | P3, monitor triggers |
| DIGEST-2, REPLICA-1, VM-2, GPU-2 | Open | P3, low priority |

**New findings from live profiling (2026-07-05, `docs/performance/profiling-2026-07-05.md`):**

| Finding | Severity | Status | Notes |
|---------|----------|--------|-------|
| PROF-1 (Pool gzip.Writer in Echo middleware) | — | Won't Fix | Echo v4.15.2 already pools via `sync.Pool` (~98% reuse). Residual 1.9% allocs are GC-clearing the pool between bursts — inherent to Go's `sync.Pool` design. A channel-based pool would save ~7MB/30s (0.24MB/s) but requires maintaining a custom middleware for <2% gain. Not justified. |
| PROF-2 (Replace GORM with pgx on list handlers) | P1 | Implemented | Replaced GORM `.Find()` reflection with `.Rows()` + manual positional `sql.Scan` in all 6 list/detail functions. Eliminates ~116MB/session from `reflect.New` + `scanIntoStruct`. All integration tests pass. |
| PROF-3 (Pre-allocate assembleNativeResults slices) | P2 | Implemented | S effort, 15-25% alloc reduction → **actual: 54-56% memory reduction, 28-42% faster** (benchmark validated) |
| PROF-4 (Streaming JSON for list responses) | — | Won't Fix | Handler pipeline requires full materialization (enrichment, projection filtering, meta-before-data JSON). Paginated results (10-100 items) are bounded and small after PROF-2+3. CSV export already streams via `io.Pipe`. |
| PROF-5 (Prometheus /metrics overhead monitor) | P3 | Open | Monitor only, no action now |

**Updated implementation roadmap (post-profiling):**

1. ~~**PROF-1** (S effort, quick-win) — gzip pool~~ Won't Fix (already pooled by Echo)
2. **PROF-3** (S effort) — pre-allocate response slices ✅
3. **PROF-2** (M effort, highest ROI) — manual row scanning on list handlers ✅
4. ~~**PROF-4** (M effort) — streaming JSON~~ Won't Fix (pipeline requires full materialization; paginated results bounded after PROF-2+3)
5. **DIGEST-1** (M effort) — sync.Pool for scratch buffers ✅
