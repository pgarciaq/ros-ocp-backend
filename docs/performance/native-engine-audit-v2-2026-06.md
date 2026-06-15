# Performance Audit Report v2: ros-ocp-backend Native Engine

## Date and Scope

**Date:** June 15, 2026  
**Branch:** `pgarciaq-rosocp-superpowers-phase13` (HEAD `79308ba0`)  
**Prior audit:** [`native-engine-audit-2026-06.md`](native-engine-audit-2026-06.md)  
**Scope:** Follow-up audit across all 11 dimensions — regression verification on prior “Do Not Regress” items, phase13 new code (adversarial fixes, percentile-band plots, notification dedup, slim list contracts, HPA, statement timeouts, tag sync auth, Grafana dashboard, `/healthz`), deferred-item trigger review, and new optimization opportunities.

**Deployment modes considered:** SaaS (multi-tenant, RDS, ingress ~30s budget) and on-prem (single-tenant PostgreSQL, 512Mi–8Gi chart profiles, NetworkPolicy-isolated internal APIs).

---

## Prior Audit Status

The June 2026 audit reported **all P0/P1 items implemented** and nearly all P2/quick-win roadmap items complete. Strategic items **S1–S3** and profiling-gated deferrals (**G-3, B-3, B-6 PGO, A-5, I-1 partial**) remained open with documented revisit triggers.

**Phase13 additions verified in this audit:**

| Area | Status |
|------|--------|
| Per-endpoint statement timeouts (`WithHeavyStatementTimeout`, `WithHeavyGORMStatementTimeout`) | Implemented — savings summary + fleet-wide container list |
| `/healthz` deep liveness (`internal/health/healthz.go`) | Implemented |
| Tag sync push API (`internal/tags/sync.go`) + internal auth | Implemented |
| Percentile-band plots from digests (`internal/model/boxplot.go`, ADR-0292) | Implemented — no raw sample reads |
| Slim list DTOs (`internal/model/list_response.go`, namespace variant) | Implemented |
| Notification dedup (engine-only in detail; `notification_codes` in list) | Implemented |
| Worker pools for threshold/savings recalc (max 3 concurrent) | Implemented |
| Request-scoped enrichment cache (`internal/api/enrichment_cache.go`) | **New in phase13** |
| Synth manifest debouncer lifecycle guard (`manifest_recommendation_debouncer.go`) | **New in phase13** |
| Vendor cleanup | 174 MiB vendor, 0 `*_test.go` files in vendor |
| Release binary (`-ldflags="-s -w"`) | **52 MiB** (prior audit: 58 MiB) |

---

## Regression Check (Do Not Regress items)

Each item from the prior audit’s “What Is Working Well” list was re-verified via grep and source inspection. **No regressions found.**

| Pattern | Location | Verified |
|---------|----------|----------|
| `DigestRow` int64 data plane | `internal/engine/types.go` | ✅ All numeric fields `int64` |
| Percentiles at ingest | `internal/ingestion/digest.go` | ✅ `ComputeContainerDigestWeighted` on `metricSample` |
| `MarginScale` / `ApplyScaledMargin` | `internal/engine/margin_scaled.go` | ✅ |
| GPU classification int BP | `internal/engine/gpu_recommender.go` — `normalizeGPUThresholds` | ✅ |
| Streaming recommend `streamBatchSize = 500` | `internal/engine/recommend_all.go:41` | ✅ |
| `sync.Pool` digest buffers | `internal/ingestion/digest.go` — `fieldBufferPool`, `weightBufferPool` | ✅ |
| `pgx.Batch` container/namespace/PVC/GPU writes | `recommend_all.go`, `pvc_recommend.go`, `gpu_recommender.go` | ✅ |
| Cost LRU cache | `internal/costdata/cache.go` | ✅ |
| Zero-copy `windowBounds` | `internal/engine/window_bounds.go` | ✅ Index ranges, no row copy |
| Fused `RecommendCPUAndMemory` | `internal/engine/recommend_cpu_and_memory.go` | ✅ |
| Decay lookup table (no hot-path `math.Exp`) | `internal/engine/decay.go`, `decay_table.go` | ✅ Integer half-life → table |
| Deferred `RefreshOrgMetadata` | `report_processor.go:718-719`, `recommend_all.go:443` | ✅ Once per reconcile |
| `org_container_keys` list pagination | `getNativeRecommendationsFromOrgKeys` — default via `applyRecommendationStaleFilter` | ✅ |
| Integer micro-cents savings | `internal/engine/savings_int.go` | ✅ |
| Graceful Kafka shutdown drain | `internal/services/` + `asyncjobs` | ✅ |
| VM recs deferred post-manifest | `report_processor.go` | ✅ |
| Node utilization BP | `internal/engine/recommend_nodes.go` | ✅ |
| Bounded Prometheus labels | `internal/metrics/metrics.go` — no `org_id`/`cluster_uuid` | ✅ |
| `GOMEMLIMIT` in Helm | cost-onprem chart (documented) | ✅ (chart-side) |
| `/healthz` + readiness `/readyz` | `internal/api/handlers.go`, `server.go` | ✅ |
| Slim list + typed `Collection[T]` | `internal/model/list_response.go`, `internal/api/common.go` | ✅ |
| Identity parsed once | `internal/api/middleware/identity.go` + `entitlement.go` reads context | ✅ |
| Namespace CSV streaming | `internal/ingestion/namespace_stream.go` | ✅ |
| Sample retention separate from digest | `internal/engine/retention.go`, `ROS_SAMPLE_RETENTION_DAYS` | ✅ |
| Ingest gauge only on flush | `pipeline_stream.go:288` — not per-row | ✅ |
| Single-tx guard 25k rows / 5k groups | `internal/ingestion/pipeline.go:24-33` | ✅ |

**Note:** `getNativeRecommendationsDistinct` still exists but is only selected when `filter[stale]=true` (show stale + active). Default API path adds `rs.stale = ?` → `false` via `applyRecommendationStaleFilter`, routing to `org_container_keys`. Not a regression.

---

## Overall Assessment

Phase13 closes the remaining operational gaps from the first audit (statement timeouts, health probes, tag sync, UI contract alignment) without undoing core engine optimizations. The **container recommendation hot path remains integer-first** with fused passes and decay lookup tables. **API list performance is strong** for the default container path (`org_container_keys` + slim DTO + count-only enrichment skip).

**New bottlenecks** are concentrated in: (1) **savings-only recalculation write path** (row-at-a-time UPDATEs), (2) **GPU list enrichment** (cluster-wide digest scan), (3) **namespace list pagination** (still `DISTINCT ON`), and (4) **tag sync** (full-org reset + per-namespace UPDATE loop). Strategic deferrals **S1–S3** remain appropriate — F-1 histograms exist but no evidence that S2’s 30s recommend trigger is met in typical deployments.

---

## What Is Working Well (Updated)

Prior list items remain valid. **Phase13 additions:**

- **Request-scoped enrichment cache** (`internal/api/enrichment_cache.go`) — deduplicates Koku `GetEffectiveRates`, currency, and GPU threshold resolution within one HTTP request (1 Masu call per cluster per page instead of per container).
- **Batched plot queries** — `AssembleAllTermBoxplots` uses a single `UNION ALL` over `daily_container_digests` instead of 3 separate round-trips (`internal/model/boxplot.go:206-287`).
- **Percentile-band plots read digests only** — eliminates raw `container_usage_samples` reads on detail API (ADR-0292); aligns with `ROS_SAMPLE_RETENTION_DAYS=45`.
- **Heavy-query statement timeouts** — `db.WithHeavyStatementTimeout` / `WithHeavyGORMStatementTimeout` on fleet-wide list and savings summary (`handlers_savings_summary.go:79`, `recommendation_set_native.go:407`).
- **Coalesced async recalcs** — `threshold_recalc_guard.go` and `savings_recalc_guard.go` prevent duplicate in-flight org work; worker cap `thresholdRecalcMaxConcurrent = 3`.
- **BH enrichment page-scoped** — `QueryContainerDigestsByScheduleTypeForContainers` with `unnest` tuple filter (`recommend_business_hours.go:111-156`).
- **Echo Prometheus `url` label uses route template** (`server.go:138`) — bounded cardinality vs raw paths.
- **Vendor hygiene** — 174 MiB, zero test files under `vendor/`.

---

## New Findings

### P0 — Critical

*None.* No regressions on prior critical paths; no new P0 issues identified in static audit.

---

### P1 — High

#### DB-N1. Savings recalculation uses per-row UPDATE loops

| Field | Value |
|-------|-------|
| **ID** | DB-N1 |
| **Severity** | P1 |
| **Location** | `internal/engine/savings_recalculate.go` — `updateContainerSavings` (498-521), `updateNodeSavings` (524-550), `updatePVCSavings` (553-569), `updateQuotaSavings`, `updateClusterQuotaSavings` |
| **Current state** | After `ApplySavingsEstimates`, each recommendation row is updated with an individual `tx.Exec` UPDATE inside a loop. Container path: 1 UPDATE per `(container, term, engine)` row. |
| **Quantification** | 500 containers × 3 terms × 2 engines = **3,000 UPDATE statements** per cluster per savings recalc. Org with 20 clusters → **60,000 UPDATEs** per cost-model change. Contrast: `WriteRecommendations` uses `pgx.Batch` chunks of 500. |
| **Proposed fix** | Mirror `WriteRecommendations`: queue UPDATEs in `pgx.Batch` (chunk 500), single transaction per cluster per rec type. Optionally `COPY` to temp table + `UPDATE FROM` for very large orgs. |
| **Expected impact** | 10–50× reduction in savings-recalc wall time for large orgs; lower WAL/dead-tuple pressure on `recommendation_sets`. |
| **Risk** | Batch failure rolls back entire chunk — acceptable with existing retry/coalesce pattern. |
| **Effort** | M (2–3 days) |
| **SaaS vs on-prem** | Both — triggered on every Koku cost-model rate change (`TriggerSavingsRecalculationAsync`). SaaS sees higher org/cluster counts. |

---

#### API-N1. GPU list enrichment scans full cluster digests (not page-scoped)

| Field | Value |
|-------|-------|
| **ID** | API-N1 |
| **Severity** | P1 |
| **Location** | `internal/api/gpu_enrichment.go:59-64` → `engine.QueryGPURecommendations` (`internal/engine/gpu_query.go:35-51`) |
| **Current state** | For each distinct `cluster_uuid` on the list page, enrichment runs `QueryGPURecommendations`, which reads **all** `gpu_container_digests` rows for that cluster in the lookback window (`WHERE cluster_uuid = $1`, no container filter). Results are then matched to page rows in memory. |
| **Quantification** | 100-item page spanning 5 clusters on a GPU-heavy cluster (2,000 GPU containers) → **5 full-cluster digest scans** + **5× `LoadPersistedGPUSavings`** per list request. BH enrichment on the same page uses page-scoped `unnest` — GPU path does not. |
| **Proposed fix** | Add `QueryGPURecommendationsForContainers(ctx, pool, orgID, pageKeys []PageGPUKey, ...)` filtering `(namespace, workload, container_name) IN unnest(...)`, mirroring `QueryContainerDigestsByScheduleTypeForContainers`. |
| **Expected impact** | List API p95 −30–80% on GPU-enabled multi-cluster pages; proportional to cluster GPU container count. |
| **Risk** | Low — read-only enrichment; must preserve node-map side data for MIG display. |
| **Effort** | M (2–4 days) |
| **SaaS vs on-prem** | Both — worse on SaaS GPU-heavy tenants. On-prem smaller clusters → smaller absolute gain but same pattern. |

---

#### DB-N2. Tag sync performs full-org reset plus per-namespace UPDATE loop

| Field | Value |
|-------|-------|
| **ID** | DB-N2 |
| **Severity** | P1 |
| **Location** | `internal/tags/sync.go` — `SyncOrgTags` (62-138) |
| **Current state** | (1) `UPDATE org_container_keys SET resolved_tags = '{}'` for entire org. (2) Loop: one `UPDATE ... WHERE org_id AND cluster_uuid AND namespace` per namespace in payload. (3) Metadata upsert. |
| **Quantification** | Org with 10k containers across 200 namespaces → **1 full-table touch + 200 UPDATE round-trips** per sync. Koku `ros_tag_sync` runs on tag settings change. |
| **Proposed fix** | Single statement: `UPDATE org_container_keys SET resolved_tags = v.tags::jsonb FROM (VALUES ...) AS v(cluster_uuid, namespace, tags) WHERE ...`. Skip org-wide reset; use `jsonb_build_object` merge or full-replace only for namespaces in payload. Consider `pgx.Batch` for >500 namespaces. |
| **Expected impact** | 5–20× faster tag sync; reduced lock duration on `org_container_keys`. |
| **Risk** | Medium — must preserve full-replace semantics per ADR/tag-sync docs; test tag removal paths. |
| **Effort** | M (2–3 days) |
| **SaaS vs on-prem** | Both — on-prem push path (`ROS_TAGS_SOURCE=api`) now wired in phase13; sync frequency depends on Settings tag changes. |

---

### P2 — Medium

#### DB-N3. Namespace list still paginates via DISTINCT ON

| Field | Value |
|-------|-------|
| **ID** | DB-N3 |
| **Severity** | P2 |
| **Location** | `internal/model/namespace_recommendation_set_native.go:127-162` — `GetNativeNamespaceRecommendations` |
| **Current state** | Page selection uses `DISTINCT ON (ns.cluster_uuid, ns.namespace_name)` over `namespace_recommendation_sets`, analogous to pre-P0-4 container path. No `org_namespace_keys` table exists. |
| **Quantification** | At 5k namespaces, DISTINCT pagination is O(n) sort per page — container path improved ~1000× with key table (prior audit: ~1.3s → ~0.3ms at 200k). Namespace cardinality is lower but same scaling law applies. |
| **Proposed fix** | Add `org_namespace_keys` materialized key table + `RefreshOrgNamespaceKeys` at end of namespace reconcile (mirror `org_container_keys` / `RefreshOrgContainerKeys`). |
| **Expected impact** | 10–100× namespace list pagination on large orgs. |
| **Risk** | Medium — new table, refresh timing, migration. |
| **Effort** | M (3–5 days) |
| **SaaS vs on-prem** | SaaS benefits more (large multi-cluster orgs). |

---

#### ALG-N1. PVC growth slope still uses math.Exp per digest point

| Field | Value |
|-------|-------|
| **ID** | ALG-N1 |
| **Severity** | P2 |
| **Location** | `internal/engine/pvc_recommend.go:346-367` — `computePVCGrowthSlopeWLS` |
| **Current state** | Weighted least-squares loop calls `w := math.Exp(-lambda * ageHours)` for each digest point. Container decay migrated to lookup tables (P0-1); PVC path did not. |
| **Quantification** | ~15–90 `math.Exp` calls per PVC per term (window days). Low volume vs containers (typically &lt;500 PVCs/cluster) but same pattern. |
| **Proposed fix** | Reuse `decay_table.go` lookup with `ageHours` quantized to day-granularity (already computed as `float64(n-1-i) * 24.0`). |
| **Expected impact** | Minor absolute CPU; consistency with container path. |
| **Risk** | Low — PVC growth is advisory; ≤0.2% weight error acceptable (per ADR-0288). |
| **Effort** | S (hours) |
| **SaaS vs on-prem** | Both |

---

#### ALG-N2. VM recommender remains float64-heavy

| Field | Value |
|-------|-------|
| **ID** | ALG-N2 |
| **Severity** | P2 |
| **Location** | `internal/engine/vm_recommender.go` (throughout), `vm_power_schedule.go`, `vm_numa_node_memory.go` |
| **Current state** | VM sizing uses float margins (`recommendedMC := float64(maxCPUP95) * (1 + cpuMargin)`), float hysteresis thresholds, float disk growth slopes. Prior audit basis-points map listed “VM sizing margins — float64” as remaining gap. |
| **Quantification** | VMs are low cardinality (typically &lt;100/cluster) — **~10–50 VMs × ~20 float ops** per reconcile vs **500 containers × 42–90 decay ops**. Not on critical path for fleet scale. |
| **Proposed fix** | Migrate margins to `ApplyScaledMargin` / basis points; keep float only at API JSON boundary. |
| **Expected impact** | Low–medium absolute; code consistency. |
| **Risk** | Medium — VM sizing is customer-visible; requires regression tests. |
| **Effort** | M (3–5 days) |
| **SaaS vs on-prem** | Both |

---

#### OBS-N1. GPU unrecognized-model counter uses raw model_name label

| Field | Value |
|-------|-------|
| **ID** | OBS-N1 |
| **Severity** | P2 |
| **Location** | `internal/engine/gpu_metadata.go:72-75` — `rosocp_gpu_model_unrecognized_total{model_name}` |
| **Current state** | Unrecognized DCGM model strings become Prometheus label values. Catalog covers known NVIDIA models; exotic MIG/custom strings could vary. |
| **Quantification** | Typically &lt;30 distinct label values; worst case unbounded if clusters report unique arbitrary strings. |
| **Proposed fix** | Bucket to `"recognized"` / `"unknown"` label only; emit full model string in structured log (ADR-0243 pattern). |
| **Expected impact** | Prevents rare Prometheus cardinality growth. |
| **Risk** | Low — dashboard filter by model would move to logs. |
| **Effort** | S (hours) |
| **SaaS vs on-prem** | SaaS fleet-wide scrape more sensitive to cardinality. |

---

#### DB-N4. Filtered list COUNT still subqueries DISTINCT/keys

| Field | Value |
|-------|-------|
| **ID** | DB-N4 |
| **Severity** | P2 |
| **Location** | `internal/model/recommendation_set_native.go:455-509` — `resolveOrgContainerCount` |
| **Current state** | Unfiltered lists hit `org_recommendation_stats` cache (fast). Any cluster/project/container filter builds `countQuery` subquery over keys/DISTINCT and runs `COUNT(*)`. |
| **Quantification** | Filtered list: **3 queries** (page keys + detail join + count). Count adds 5–50ms on large orgs with ILIKE filters. |
| **Proposed fix** | Pre-aggregated filter stats (deferred P2 in query-performance skill) or approximate count for UI when filters active. |
| **Expected impact** | 5–30ms p50 on filtered lists; larger at 100k+ scale. |
| **Risk** | Low for approximate; medium for materialized counts. |
| **Effort** | M |
| **SaaS vs on-prem** | SaaS |

---

### P3 — Low

#### API-N2. Duplicate loadTermWindows on detail requests

| Field | Value |
|-------|-------|
| **ID** | API-N2 |
| **Severity** | P3 |
| **Location** | `internal/api/handlers.go:885-920` → `AssembleAllTermBoxplots` → `loadTermWindows` (`boxplot.go:90-136`) |
| **Current state** | Detail enrichment may call `loadTermWindows` twice per request (once per term batch path). Single indexed query on `org_recommendation_terms` (~0.1ms). |
| **Proposed fix** | Pass preloaded `map[string]TermWindow` from handler context or request cache. |
| **Expected impact** | ~0.1–0.5ms per detail request. |
| **Effort** | S |
| **SaaS vs on-prem** | Both |

---

#### ALG-N3. OOM bump uses float math.Log2

| Field | Value |
|-------|-------|
| **ID** | ALG-N3 |
| **Severity** | P3 |
| **Location** | `internal/engine/margin_scaled.go:53-57` — `ApplyOOMBumpScaled` |
| **Current state** | `math.Log2(1+float64(oomCount))` per memory recommendation with OOM history. |
| **Quantification** | ≤6 calls per container (3 terms × 2 engines) when OOM present — negligible vs decay loop. |
| **Proposed fix** | Small lookup table for `oomCount` 0–20. |
| **Effort** | S |
| **SaaS vs on-prem** | Both |

---

#### BUILD-N1. Binary size and AWS SDK v1 (I-1 partial)

| Field | Value |
|-------|-------|
| **ID** | BUILD-N1 |
| **Severity** | P3 |
| **Location** | `go.mod:49` — `github.com/aws/aws-sdk-go v1.49.13 // indirect` via `platform-go-middlewares` |
| **Current state** | Release binary **52 MiB** with `-ldflags="-s -w"` (CGO Kafka). AWS SDK v1 remains indirect; vendor **174 MiB**, clean (0 test files). |
| **Proposed fix** | Upstream `platform-go-middlewares` CloudWatch v2 migration (I-1 deferred trigger). Optional PGO when CI supports profiles (B-6). |
| **Expected impact** | ~5–10 MiB binary reduction when v1 removed. |
| **Effort** | L (upstream dependency) |
| **SaaS vs on-prem** | Both — deploy/pull time. |

---

#### API-N3. Plot JSON uses float64 for percentile values

| Field | Value |
|-------|-------|
| **ID** | API-N3 |
| **Severity** | P3 |
| **Location** | `internal/model/boxplot.go:12-18` — `PlotDetails` |
| **Current state** | Digest int64 millicores/KiB converted to `float64` cores/MiB at API boundary for Kruize-compatible JSON. |
| **Proposed fix** | Optional integer API v2; not required for performance (conversion is O(datapoints), ~7–45 per term). |
| **Effort** | L (contract change) |
| **SaaS vs on-prem** | Both |

---

## Deferred Items — Revisit Trigger Check

| ID | Item | Trigger (prior audit) | Met? | Assessment |
|----|------|----------------------|------|------------|
| **S1** | Unified windowed digest recommender | 6th recommendation type | **No** | Still 5 subsystems; duplication remains intentional safety boundary. |
| **S2** | Parallel container recommend by namespace | F-1 shows recommend phase &gt;30s per cluster | **No evidence** | `rosocp_pipeline_phase_duration_seconds{phase="recommend"}` exists (`metrics.go:42-48`) but static audit cannot read production histograms. Inter-cluster parallelism (Kafka 12 partitions, multi-pod) still preferred. |
| **S3** | Namespace recs from container rollups | Product rollup spec | **No** | Accuracy argument unchanged (P95(A+B) ≠ P95(A)+P95(B)). |
| **G-3** | Distributed debouncer | Multiple processor pods concurrent | **Partial** | Debouncer still `sync.Map` process-local (`manifest_recommendation_debouncer.go:39`). Lifecycle generation guard added in phase13 — reduces stale timer risk but not cross-pod coordination. **Trigger not met** unless running &gt;1 processor replica. |
| **B-3** | String interning for DigestKey | Memory profiling shows string dup | **No** | `metricSample` already dropped string metadata from grouped samples (B-1). |
| **B-6 (PGO)** | CI PGO integration | CI supports PGO builds | **No** | `GOMEMLIMIT` done; PGO still deferred. |
| **A-5** | Legacy Kruize `map[string]interface{}` | Legacy path retained | **N/A** | `api_compat.go` unchanged; native engine default when Kruize plugin disabled. |
| **I-1** | AWS SDK v1 removal | `platform-go-middlewares` drops v1 | **No** | v1 still indirect in `go.mod`. |

---

## Accuracy Trade-off Register

| Trade-off | Introduced | Still valid? | Notes |
|-----------|------------|--------------|-------|
| Decay weight lookup quantization (~0.2% error) | P0-1 / ADR-0288 | ✅ | Integer half-life hours use table; non-integer falls back to `math.Exp`. |
| Idle P95 → max-of-daily-P95 | P2-5 | ✅ | Semantically looser idle detection; large CPU win. |
| Percentile-band plots (p50/p95/p99/max) vs boxplot | E-2 / ADR-0292 | ✅ **Phase13** | Reads digests only; 45-day sample retention safe. |
| Separate sample vs digest retention | E-2 | ✅ | `ROS_SAMPLE_RETENTION_DAYS=45` vs `ROS_RETENTION_MONTHS=6`. |
| Slim list contract (short_term cost only default) | S4 / ADR-0294 | ✅ | UI passes `term`/`engine` when needed. |
| Savings integer micro-cents | P1-1 / ADR-0291 | ✅ | Rates converted once at boundary. |
| PVC math.Exp weights | ALG-N1 (open) | ✅ Acceptable | Low volume; accuracy for growth slope matters more than speed. |
| VM float64 sizing | ALG-N2 (open) | ✅ | Low cardinality; accuracy-sensitive. |
| Statement timeout cancellation | Phase13 | ✅ **New** | Heavy endpoints 45s default; SaaS should set ~28s (`HeavyAPIStatementTimeoutMS`). |

---

## ROI-Ordered Implementation Roadmap

| Rank | ID | Title | Impact | Effort | Risk |
|------|-----|-------|--------|--------|------|
| 1 | **DB-N1** | Batch savings-recalc UPDATEs | High on cost-model changes | M | Low |
| 2 | **API-N1** | Page-scope GPU enrichment | High on GPU list API p95 | M | Low |
| 3 | **DB-N2** | Batch tag sync UPDATEs | Medium on tag settings change | M | Medium |
| 4 | **DB-N3** | `org_namespace_keys` pagination | Medium–high at 1k+ namespaces | M | Medium |
| 5 | **ALG-N1** | PVC decay lookup table | Low absolute | S | Low |
| 6 | **OBS-N1** | Bound GPU model metric labels | Low (preventive) | S | Low |
| 7 | **ALG-N2** | VM basis-point migration | Low absolute | M | Medium |
| 8 | **DB-N4** | Filtered count optimization | Low–medium | M | Low |

**Do not pursue yet:** S1, S2, S3, G-3 (unless multi-processor), B-3, B-6, A-5, I-1 (blocked on upstream).

---

## Appendix: Call Count Estimates

### Container reconciliation (500 containers, 30-day lookback, 3 terms, 2 engines)

| Phase | DB round-trips / computations | Notes |
|-------|------------------------------|-------|
| Load term config | 1 (cached) | `LoadTermConfigCached` |
| Stream digests | 1 cursor query | ~15 rows/container × 500 = 7,500 rows |
| Recommend compute | 0 DB | 500 × 3 × 2 × ~10 extractors × ~15 rows ≈ **45,000 decay lookups** (not `math.Exp`) |
| Write batches | 6 × (batch write + history + quality) | 3000 rec rows / 500 = 6 batches; each uses `pgx.Batch` |
| `RefreshOrgMetadata` | 2 | `RefreshOrgContainerKeys` + `RefreshOrgRecommendationStats` |
| Post-process GPU + node | 2–4 | errgroup parallel |
| **Total DB statements** | **~25–35** | Down from ~20× org scans pre-P0-3 |

### API container list (100 items, 3 clusters, default filters)

| Step | Queries | Notes |
|------|---------|-------|
| Page keys | 1 | `org_container_keys` + seek |
| Detail join | 1 | `recommendation_sets` for page only |
| Count | 0–1 | `org_recommendation_stats` hit OR subquery |
| BH enrichment | 3 | schedules + window + page-scoped digests |
| GPU enrichment | **6** | **2 per cluster (digest + savings)** — cluster-wide scan |
| Currency | 0–3 | enrichment cache hits after first cluster |
| **Total** | **11–14** | GPU page-scope would reduce to ~5–8 |

### Savings recalc (500 containers, 1 cluster, all rec types)

| Step | Statements | Notes |
|------|------------|-------|
| Load recs | 1 SELECT per type | Container/node/PVC/quota |
| Compute | 0 DB | Integer micro-cents in-process |
| Update | **3000+ UPDATEs** | DB-N1 — should be 6 `pgx.Batch` sends |
| Refresh stats | 1 | If container type included |

### Tag sync (200 namespaces, 10k container keys)

| Step | Statements | Notes |
|------|------------|-------|
| Reset tags | 1 | Full org `org_container_keys` touch |
| Apply namespaces | **200** | Per-namespace UPDATE |
| Metadata | 1 | Upsert |
| **Total** | **202** | DB-N2 target: **1–2** |

### Ingest (1000 containers × 30 days ≈ 96k CSV rows)

| Phase | Est. wall-clock | Dominant factor |
|-------|-----------------|-----------------|
| CSV download | 5–30s | I/O |
| Parse + digest + sample flush | 15–45s | CPU + DB (flush every 1000 rows) |
| Recommend + write | 0.5–3s | CPU (5% of total per prior audit) |
| Peak RAM grouping | ~7 MB | `metricSample` 72 B × 96k ≈ 6.9 MB (vs 50–120 MB pre-B-1) |

---

## Summary

| Severity | New findings count |
|----------|-------------------|
| P0 | 0 |
| P1 | 3 |
| P2 | 6 |
| P3 | 4 |
| **Regressions** | **0** |

**Top 3 ROI items:** (1) **DB-N1** — batch savings-recalc UPDATEs, (2) **API-N1** — page-scope GPU enrichment, (3) **DB-N2** — batch tag sync writes.

**Regressions:** None detected on the prior audit’s “Do Not Regress” list. Phase13 changes are additive and align with the performance strategy established in the first audit.
