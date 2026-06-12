# Performance Audit Report: ros-ocp-backend Native Engine

**Date:** June 2026
**Scope:** Full performance review of the native recommendation engine across 9 dimensions
**Status:** Tracking remediation progress (see Status column in tables below)

---

## Overall Assessment

**The rewrite achieved its primary goal:** digest data is fully `int64`, percentiles are precomputed at ingest time (not at recommendation time), and the streaming recommendation engine bounds memory. The `MarginScale`/basis-points pattern is solid where used. However, **float64 persists in three critical hot paths** (decay weighting, savings estimation, adaptive margin), and **database write amplification** on org metadata refresh is the single largest scalability bottleneck.

---

## What Is Working Well (Do Not Regress)

- `DigestRow` fields are fully `int64` -- no boxing in the primary data plane
- Percentiles precomputed at ingest via exact sort on ~24 samples (negligible cost)
- `MarginScale = 10000` with `ApplyScaledMargin` for integer margin application
- GPU classification on integer basis points after threshold conversion
- Streaming container recommendations (`streamBatchSize = 500`) -- bounded memory
- `sync.Pool` for digest computation buffers at ingest
- `pgx.Batch` for container/namespace recommendation writes (chunk 500)
- Cost data cached in LRU with TTL per cluster
- `filterByWindow` uses binary search for window boundaries
- `MultiWeightedPercentileWithExtras` fuses 5 extractors + idle + trend in one pass

---

## P0 -- Critical Findings

### P0-1. `math.Exp` in decay weighting (per container x term x engine x row) — **IMPLEMENTED**

**Location:** `internal/engine/decay.go`, `internal/engine/decay_table.go` — `DecayWeight`, `MultiWeightedPercentileWithExtras`

For every digest row in every window, the code called `math.Exp(-ageHours * math.Ln2 / halfLifeHours)` and accumulated `float64` weighted sums. On a 500-container cluster with medium+long terms: ~28k-60k `math.Exp` calls per recommendation cycle. This was the densest float hot path.

**Fix implemented (2026-06):**
- **Lookup table:** `DecayWeight` quantizes age and half-life to integer hours and looks up precomputed weights from `decay_table.go`. Tables are built lazily on first use via `sync.Map` (typically 2–3 tables per invocation, microseconds total). Non-integer half-lives still fall back to `math.Exp`.
- **Auto-derive half-life:** When a tenant DB override sets `window_days` but leaves `decay_halflife_hours` NULL, `term_config.go` derives `window_days × 12` instead of retaining the plugin default.

**Impact:** Eliminates `math.Exp` from the hot path for standard integer half-lives.

**ADR:** [ADR-0288](../adr/0288-decay-weight-lookup-tables.md)

### P0-2. Duplicate CPU + memory weighted passes per term x engine — **Open**

**Location:** `internal/engine/recommend_cpu.go`, `internal/engine/recommend_memory.go`

`RecommendCPU` and `RecommendMemory` each call `MultiWeightedPercentileWithExtras` separately with their own extractors, recomputing decay weights for the same rows. That's **12 passes** per container (3 terms x 2 engines x 2 resources) when it could be **6** (fused CPU+memory in one pass with 10 extractors).

**Fix:** Merge into a single `RecommendCPUAndMemory` that calls `MultiWeightedPercentileWithExtras` once with all 10 extractors.

**Impact:** ~40-50% reduction in recommend-phase CPU per container.

### P0-3. Org-wide metadata refresh on every write batch — **Implemented**

**Location:** `internal/engine/recommend_all.go` — `WriteRecommendations`, `RefreshOrgMetadata`; `internal/services/report_processor.go` — `runContainerRecommendations`

Every streaming batch (500 containers) triggered `RefreshOrgContainerKeys` + `RefreshOrgRecommendationStats`, each doing a full `DISTINCT ON` scan of `recommendation_sets` for the entire org. For a 10k-container org, that's ~20 full-org scans per reconciliation cycle.

**Fix implemented (2026-06):**
- Removed per-batch refresh from `WriteRecommendations`.
- Added `RefreshOrgMetadata` called once at end of `runContainerRecommendations` and `recalculateContainerCluster`.
- Tests/tooling use `WriteRecommendationsAndRefreshOrg` for single-batch writes.

**Impact:** 50-90% reduction in recommendation write time for 10k+ container orgs.

### P0-4. List API still paginates via `DISTINCT ON recommendation_sets` — **Open**

**Location:** `internal/model/recommendation_set_native.go:380`

Despite `org_container_keys` existing for fast counts, page selection still runs `DISTINCT ON` over `recommendation_sets`. Documented internally as ~1.3s vs ~0.3ms for key-table pagination at 200k containers.

**Fix:** Paginate `org_container_keys` directly (join `recommendation_sets` only for detail after page is selected).

**Impact:** ~1000x on pagination subquery for large tenants.

---

## P1 -- High Priority

### P1-1. Savings estimation is entirely float64 — **Open**

**Location:** `internal/engine/savings.go` and duplicated in `gpu_recommender.go`, `vm_savings.go`, `node_savings.go`, `pvc_savings.go`

Per recommendation row: millicore -> cores (`/1000.0`), KiB -> GiB (`/1024/1024`), multiply by `float64` rates, multiply by `730.0` hours/month, then `math.Round(total*100)/100`.

**Fix:** Compute in integer micro-cents using millicore-hours and byte-hours directly. Rates in micro-cents per millicore-hour. Convert to cents once at the end. Unify the six duplicated savings modules.

**Impact:** Moderate-significant depending on cluster size; also reduces code duplication.

### P1-2. Adaptive margin float CV detour — **Open**

**Location:** `internal/engine/margin.go`

`ComputeAdaptiveMargin` computes `cv = float64(p95-p50) / float64(mean)` then scales back to `MarginScale`. The integer equivalent is `cvScaled = (p95-p50) * MarginScale / mean` -- pure integer division.

**Fix:** Replace `ComputeAdaptiveMargin` with integer-only `ComputeAdaptiveMarginScaledDirect`.

**Impact:** Moderate -- one float division per rec eliminated; aligns with existing `ApplyScaledMargin` pattern.

### P1-3. `filterByWindow` allocates per term per container — **Open**

**Location:** `internal/engine/recommend_all.go`

Each term creates a new `[]DigestRow` copy. With 3 terms + idle window = 4 allocations per container.

**Fix:** Return `(lo, hi)` indices into the shared backing slice. Process windows sequentially (already the case).

**Impact:** 15-25% less allocation/GC during recommend on large clusters.

### P1-4. `rh_accounts` join anti-pattern — **Open**

**Location:** `GetRecommendationQuality`, `nativeContainerDetailQuery`, `loadClusterLastReportedAt`

Tables that have `org_id` columns still join `rh_accounts` for org filtering instead of filtering directly on `org_id`.

**Fix:** Use `WHERE org_id = $1` directly.

**Impact:** 10-5000x on affected query plans per internal EXPLAIN audit.

---

## P2 -- Medium Priority

### P2-1. GPU threshold conversion at every classify call — **Open**

**Location:** `internal/engine/gpu_recommender.go`

`GPUThresholds` stores six `float64` thresholds, converted to basis points via `ThresholdToBasisPoints` at each classify call. Could store as BP in settings JSON directly.

**Fix:** Store thresholds as int32 BP in config; remove per-classify conversion.

### P2-2. Node utilization as float ratios instead of basis points — **Open**

**Location:** `internal/engine/recommend_nodes.go`

`cpuUtil50 := float64(d.CPUUsageP50MC) / float64(allocCPU)` -- could be `usage * 10000 / alloc` (basis points).

### P2-3. PVC and GPU writes not batched — **Open**

**Location:** `WritePVCRecommendations` (per-rec `Exec`), `StoreGPUClassifications` (per-rec UPDATE)

Container/namespace use `pgx.Batch`; PVC and GPU do not.

**Fix:** Switch to `pgx.Batch` or single SQL `UPDATE FROM` with values list.

### P2-4. VM recommendations run synchronously on ingest — **Open**

**Location:** `internal/services/report_processor.go`

`RunVMRecommendations` runs inline after VM digest upsert, blocking the ingest pipeline.

**Fix:** Queue like container recs (post-CSV phase).

### P2-5. Idle classification sorts when max/weighted-max would suffice — **Open**

**Location:** `internal/engine/idle_classification.go`

`percentile95Int64` creates a copy and sorts for exact P95 across ~15 daily values. Max of daily P95s is cheaper and semantically close enough for idle detection.

---

## Basis Points Consistency Map

| Area | Status | Gap |
|------|--------|-----|
| Digest data (CPU/mem) | int64 millicores/KiB | None |
| GPU utilization | int32 basis points | None |
| Quota/CRQ headroom | int BP via `applyHeadroom` | None |
| VM power-off idle ratio | int32 BP | None |
| Margin application | `MarginScale` + `ApplyScaledMargin` | None |
| **Margin computation** | **float64 CV** | Could be integer |
| **GPU thresholds** | **float64 in config** | Convert to BP at load |
| **Node utilization** | **float64 ratio** | Could be BP |
| **PVC usage ratio** | **float64** | Could be BP |
| **Savings rates** | **float64 USD** | Could be micro-cents |
| **VM sizing margins** | **float64** | Mirror container `ApplyScaledMargin` |

---

## Hot Path Execution Map (Container -- Highest Volume)

```
RecommendWorkloadsStreaming
  per container (stream batch 500)
    ClassifyIdleState .............. 2 sorts on ~15 values (could be max)
    per term (short/medium/long)
      filterByWindow ............... binary search + COPY (could be zero-copy)
      per engine (cost/performance)
        RecommendCPU ............... decay loop O(W), math.Exp per row
        RecommendMemory ............ DUPLICATE decay loop O(W)     <-- fuse with CPU
        computeVariation x4 ....... float round (could be integer)
        EvaluateNotifications ...... integer comparisons (good)
  ApplySavingsEstimates (batch) .... float64 cost math per rec    <-- could be int cents
```

**Total inner loop iterations per container:** 3 terms x 2 engines x 2 (CPU+mem) x ~7-15 digest rows = **84-180 decay weight computations**. After fusing CPU+mem: **42-90**.

---

## Prioritized Remediation Roadmap

### Quick Wins (1-2 days each, high ROI)

| ID | Change | Impact | Risk | Status |
|----|--------|--------|------|--------|
| Q1 | Fuse CPU + memory weighted passes | ~40-50% recommend CPU | Low | Open |
| Q2 | Zero-copy window slicing | ~15-25% less alloc/GC | Low | Open |
| Q3 | Reuse decay weights across engines within a term | ~15% recommend CPU | Low | Declined (negligible after P0-1; ~2ns lookup) |
| Q4 | Replace idle P95 sort with max-of-daily-P95 | Eliminates 2 sorts x N containers | Medium (behavior) | Open |
| Q5 | Batch PVC writes via pgx.Batch | High DB latency improvement | Low | Open |
| Q6 | Batch GPU classification UPDATEs | Significant for GPU clusters | Low | Open |

### Medium Effort (3-5 days each)

| ID | Change | Impact | Risk | Status |
|----|--------|--------|------|--------|
| M1 | Defer org metadata refresh to end of reconcile | 50-90% write time for large orgs | Medium | In Progress |
| M2 | Migrate list API pagination to org_container_keys | ~1000x on page query | Medium | Open |
| M3 | Integer savings in micro-cents | Eliminates float in savings | Medium | Open |
| M4 | Decay weight lookup table | Eliminates math.Exp | Low | Implemented |
| M5 | Integer adaptive margin (remove float CV) | Completes BP migration | Low | Open |
| M6 | Decouple VM recommend from ingest | Large ingest latency win | Medium | Open |

### Strategic (1-2 weeks each)

| ID | Change | Impact | Risk | Status |
|----|--------|--------|------|--------|
| S1 | Unified windowed digest recommender framework | 30-50% less code duplication | High | Open |
| S2 | Parallel container recommend by namespace partition | Linear speedup on multi-core | High | Open |
| S3 | Namespace recs from container rollups | Eliminates duplicate engine run | High (product) | Open |

---

## Theme A: API Response Assembly and Serialization

### A-1. Double assembly per container in list API (P0) — **Open**

**Location:** `internal/api/handlers.go:629`

Each page row is assembled twice: `assembleNativeResults` builds `NativeContainerResult` with `MapToKruizeFormat`, then `BuildDetailResponse` rebuilds the full Kruize-shaped nested JSON. This costs ~10-30ms CPU per 100-item page on top of DB/enrichment time.

**Fix:** Introduce a slim list DTO (table columns + codes only); reserve `BuildDetailResponse` for detail-by-ID.

### A-2. Notification triplication in JSON (P0) — **Open**

**Location:** `internal/model/detail_response.go:164`

The same notification codes are copied to three JSON levels: `recommendations.notifications`, `recommendation_terms.<term>.notifications`, and `recommendation_engines.<engine>.notifications`. This adds several KB per container redundantly.

**Fix:** Emit notifications at one level only (engine); or return `notification_codes: [1,7]` in list and let the UI resolve via the catalog endpoint.

### A-3. `Collection.Data []interface{}` forces boxing (P1) — **Open**

**Location:** `internal/api/common.go:75`

Every list item is heap-allocated and interface-boxed. Blocks compile-time JSON optimizations.

**Fix:** Use `[]*DetailResponse` or `[]json.RawMessage`.

### A-4. Double identity parsing per request (P1) — **Open**

**Location:** `internal/api/middleware/identity.go`, `entitlement.go`

The `x-rh-identity` header is base64-decoded and JSON-unmarshaled twice (identity middleware + entitlement middleware). ~0.2-0.8ms per request.

**Fix:** Parse once; store entitlement flag on context.

### A-5. Legacy Kruize path: `map[string]interface{}` (P1) — **Open**

**Location:** `internal/plugins/kruize/api_compat.go:453`

Legacy list/detail unmarshals JSONB into generic maps, mutates in place, then marshals again. Worst path for allocator churn and reflection. Impact only when Kruize fallback is active.

### A-6. Cache headers on notification catalog (P2) — **Open**

`GetNotificationCodes` is static in-memory, but returns no cache headers. Could add `Cache-Control: public, max-age=86400`.

---

## Theme B: Ingestion Pipeline and Memory

### B-1. Digest groups retain full MetricRow structs (P0) — **Open**

**Location:** `internal/ingestion/pipeline_stream.go:181`

Between flushes, `groupedAll` and `groupedBH` store full `MetricRow` (~456 bytes + heap strings) per sample. For 1000 container-days x 96 samples/day = ~96,000 copies = **50-120 MB**.

**Fix:** Store a slim `metricSample` (6x int64 + time.Time = ~72 bytes) instead. Convert from `MetricRow` once at append time. 5-10x RAM reduction.

### B-2. Namespace CSV not streaming (P1) — **Open**

**Location:** `internal/ingestion/` -- `ParseNamespaceCSVRows`

Namespace CSV is fully materialized into `[]NamespaceMetricRow` before grouping, unlike the container path which streams.

**Fix:** Mirror `forEachCSVRow` + incremental flush.

### B-3. No string interning for repeated keys (P2) — **Open**

`DigestKey` copies `Namespace`, `Workload`, `ContainerName` from every row. These repeat heavily across rows. An intern table could reduce heap pressure on large clusters.

### B-4. Per-row Prometheus gauge update during CSV ingest (P1) — **Open**

**Location:** `internal/ingestion/pipeline_stream.go:240`

`SetIngestGroupsInMemory(groupCount)` is called on **every CSV row** in the streaming loop. On a 500k-row manifest, that's 500k Prometheus gauge updates with mutex + label lookup.

**Fix:** Update only on flush boundaries (already done at flush end; remove the per-row one).

### B-5. Single-tx fast path holds all deferred samples in RAM (P2) — **Open**

When `rowCount <= 50,000` and no incremental flushes have occurred, the single-tx path holds all samples in memory before committing. Can spike to tens of MB.

**Fix:** Lower threshold or require both row count AND group count below threshold.

### B-6. No PGO or GOMEMLIMIT in deployment (P2) — **Open**

- No Profile-Guided Optimization (`-pgo`) in the Docker build
- `GOMEMLIMIT` not set in the container (documented but not enforced)

**Fix:** Set `GOMEMLIMIT` to ~90% of container memory limit in Helm values. Consider PGO build after collecting production CPU profiles.

---

## Theme C: Observability and Operational Overhead

### C-1. ROS API readiness probe points at /status, not /readyz (P0) — **Open**

**Location:** `cost-onprem/templates/ros/api/deployment.yaml:108`

The Helm chart wires readiness to `/status` (static JSON, no dependency checks) instead of `/readyz` (which pings the database). Pods are marked ready while DB is down.

**Fix:** Point `readinessProbe` at `/readyz`; keep `livenessProbe` on `/status`.

### C-2. VM CSV warn-log storms (P1) — **Open**

**Location:** `internal/ingestion/vm_csv.go:294`

Every bad VM CSV row logs `Warnf`. A noisy file can generate thousands of log lines per manifest. Container CSV correctly uses `Debugf`.

**Fix:** Downgrade to Debug + increment `rosocp_csv_rows_skipped_total{report_type=vm}`.

### C-3. High-cardinality Prometheus labels (P1) — **Open**

Several metrics use `org_id` and `cluster_uuid` as labels: `rosocp_analytics_incomplete_total`, `ros_recommendation_stability`, `ros_reship_in_progress`, etc. This creates unbounded cardinality.

**Fix:** Use bounded labels (`error_type`, `stage`); log tenant context via structured logging instead.

### C-4. Processor shutdown doesn't drain in-flight work (P1) — **Open**

On SIGTERM, the Kafka consumer loop exits but running `ProcessReport` handlers (which may be mid-CSV or mid-recommendation) are not awaited. Risk: partial processing, duplicate work on retry.

**Fix:** Pass lifecycle `context.Context` into `ProcessReport`; on shutdown, stop accepting new messages, `WaitGroup` for in-flight handlers.

### C-5. `ReportCaller: true` in production logging (P2) — **Open**

**Location:** `internal/logging/logging.go`

Every log line walks the call stack. Minor but unnecessary overhead in production.

**Fix:** Disable `ReportCaller` when `LOG_LEVEL >= INFO`.

### C-6. No `make test-short` for fast local iteration (P2) — **Open**

Full test suite requires Docker and takes ~25 minutes serial. No documented fast-path for unit-only runs.

**Fix:** Add `make test-short` -> `go test -short ./...` for sub-5-minute local runs.

---

## Theme D: PostgreSQL Server-Side Tuning

**Deployment context:** ROS runs against two very different PostgreSQL environments:

| Aspect | SaaS (console.redhat.com) | On-Prem (cost-onprem chart) |
|--------|--------------------------|----------------------------|
| Engine | **AWS RDS PostgreSQL** (managed) | **Red Hat build of PostgreSQL** (container in Helm chart) |
| Tuning | RDS parameter groups, automated backups, auto-scaling storage | No `postgresql.conf` exposed; stock image defaults |
| RAM | RDS instance class (e.g., db.r6g.xlarge = 32Gi) | **512Mi** (chart default) |
| Storage | GP3/io2, auto-expanding | **30Gi PVC** shared across Koku + ROS + RBAC |
| Connections | RDS scales with instance class; IAM auth possible | `max_connections=100` (stock default) |
| Autovacuum | RDS-managed, tunable via parameter groups | Stock defaults; no per-table tuning |
| HA | Multi-AZ, read replicas | Single StatefulSet pod |
| Monitoring | CloudWatch, Performance Insights, Enhanced Monitoring | `pg_stat_statements` only (if enabled) |

**Key implication:** D-1 through D-3 are primarily **on-prem concerns**. SaaS RDS deployments have managed tuning, adequate resources, and monitored vacuum. On-prem is where the chart's undersized defaults become production risks. However, D-4 and D-5 (retention gaps) affect **both** deployment modes -- they are application-level bugs.

### D-1. No PostgreSQL tuning exposed in Helm chart (CRITICAL -- on-prem only) — **Open**

The bundled DB is stock Red Hat PostgreSQL with **512Mi RAM and 30Gi shared disk**. No `postgresql.conf` customization. At 10k containers, `container_usage_samples` alone can reach **86M rows** in 90 days, needing 100-150Gi disk just for ROS.

SaaS: Not applicable -- RDS instance class and parameter groups are managed separately.

**Recommended on-prem production profile (10k containers):**
- PG memory: 4-8Gi, storage: 100-150Gi
- `shared_buffers`: 1-2GB, `work_mem`: 16-32MB
- `max_connections`: 200 (or add PgBouncer)
- Expose a `database.server.postgresqlConfiguration` in chart values or document mandatory external DB specs for production

### D-2. Connection budget exceeds max_connections (CRITICAL -- on-prem; MEDIUM -- SaaS) — **Open**

With default `ROS_DB_MAX_CONNS=10` per process across ~6 ROS pods + Koku + RBAC + Kruize, total connections can reach **85-150** against PostgreSQL's default `max_connections=100`. Chart sets legacy `DB_POOL_SIZE` that ros-ocp-backend does not read.

SaaS: RDS instances typically have higher `max_connections` (e.g., db.r6g.xlarge allows ~5000), but the dead `DB_POOL_SIZE` env var and missing `ROS_DB_MAX_CONNS` exposure in Helm still apply.

### D-3. UPDATE-heavy tables need autovacuum tuning (HIGH -- on-prem; MEDIUM -- SaaS) — **Open**

`recommendation_sets` and `container_usage_samples` get frequent UPSERTs creating dead tuples. No `fillfactor` or per-table autovacuum tuning exists.

SaaS: RDS autovacuum is managed but uses instance-level defaults. Per-table `ALTER TABLE ... SET (autovacuum_vacuum_scale_factor = ...)` would still be beneficial and is supported in RDS -- this should be applied via a migration.

On-prem: Stock autovacuum with 512Mi RAM can fall behind on large UPSERT tables, leading to table bloat and degraded query plans.

### D-4. `node_recommendations` retention gap (HIGH -- both modes) — **Open**

Listed in retention plugin but **not partitioned** -- sweep is a no-op. Rows only removed on Sources destroy, not by age. This is an application-level bug that affects both SaaS and on-prem.

### D-5. `namespace_recommendation_sets` and `pvc_recommendation_sets` lack age-based retention (HIGH -- both modes) — **Open**

No periodic cleanup; only removed on Sources destroy. Application-level gap in both deployment modes.

---

## Theme E: Data Lifecycle

### Growth projections

| Scale | `container_usage_samples` (90d) | `recommendation_sets` | `recommendation_history` (90d) | Disk estimate |
|-------|--------------------------------|----------------------|-------------------------------|---------------|
| 1k containers | 8.6M | 6k | 540k | ~5-10Gi |
| 10k containers | 86M | 60k | 5.4M | ~35-70Gi |
| 100k containers | 864M | 600k | 54M | ~350-700Gi |

**Key insight:** `container_usage_samples` drives 90%+ of storage. Consider shorter sample retention vs digest retention -- digests alone suffice for the 90-day recommendation lookback.

### E-1. Partition DROP for digests/samples (GOOD)

Monthly partition dropping avoids mass DELETE autovacuum death spirals. Already implemented and working.

### E-2. Consider separate sample retention (MEDIUM) — **Open**

Samples are needed for boxplot drill-down but not for recommendations (which use digests). A 30-day sample retention with 6-month digest retention would dramatically reduce disk usage.

---

## Theme F: End-to-End Latency Budget

### Ingest flow (500 containers, 30-day CSV)

| Phase | Est. wall-clock | Bound |
|-------|----------------|-------|
| Kafka receive | <100ms | I/O |
| CSV download | **5-30s** | I/O (HTTP/S3) |
| CSV parse + digest + DB writes | **15-45s** | CPU + DB |
| Recommendations computed | **0.5-3s** | CPU |
| Recommendations written | **1-5s** | DB |
| Post-processing (GPU + node) | **2-10s** | CPU + DB |
| Org metadata refresh | <1s | DB |
| **Total** | **~30-90s** | |

**CSV download and parse+digest dominate** (~70-80% of wall-clock). Recommendation compute is fast (~5% of total). The biggest latency wins are in ingest I/O and DB write batching, not in the recommendation math.

### API flow (p50/p95)

| Phase | p50 | p95 |
|-------|-----|-----|
| Middleware (identity + RBAC) | <2ms | <150ms (RBAC miss) |
| DB query (list with org_container_keys) | <5ms | <10ms |
| Enrichment (BH, currency, GPU) | <5ms | <20ms |
| JSON assembly + gzip | 2-8ms | 10-25ms |
| **Total** | **~15ms** | **~200ms** |

**RBAC cache misses dominate p95.** DB queries are fast with proper indexing.

### Instrumentation gaps

- Pipeline phases `recommend`, `write`, `quality`, `history` are not separately histogrammed
- No OpenTelemetry tracing
- Masu `GetEffectiveRates` has no duration histogram

---

## Theme G: Horizontal Scaling

### Scaling ceilings (ordered by when you hit them)

| Fleet size | First wall | Bottleneck |
|------------|-----------|------------|
| 1k | None | -- |
| 10k | Ingest throughput | 3 Kafka partitions cap parallel ingest |
| 50k | DB connections | N pods x 10 conns > max_connections |
| 100k | DB write throughput | `recommendation_sets` UPSERT contention |
| 100k+ per org | API aggregation | Fleet/savings summary cache misses |

### Key constraints

- **Kafka: 3 partitions** -- hard cap on parallel upload processing. A 4th processor pod sits idle.
- **No HPA** -- chart has no HorizontalPodAutoscaler for any ROS component
- **Synth manifest debouncer is process-local** -- not coordinated across pods; can cause duplicate rec runs
- **In-process caches not shared** -- fleet/savings/RBAC/threshold caches are per-pod with TTL staleness

### Scaling model

- **API pods:** Stateless, scale freely (watch DB pool x replicas)
- **Processor pods:** Scale up to Kafka partition count (currently 3)
- **Housekeeper:** Singleton
- **To go beyond 3 parallel ingests:** Increase topic partitions first (3 -> 12 is typical next step)

---

## Theme H: Frontend/API Contract Efficiency

**This may be the single highest-ROI optimization area.**

### H-1. Badge/summary fetch 100-row list for a count (P0) — **Open**

`OptimizationsBadge` and `OptimizationsSummary` call the list API with default `limit=100` but only read `meta.count`. Each call materializes up to 100 full recommendation objects with all enrichment.

**Fix (5 minutes):** Pass `limit=1` from the UI. Better: use fleet-summary endpoint.

### H-2. Projects table fetches then overwrites with mocks (P0) — **Open**

`optimizationsProjectsTable.tsx` makes a full list API call then replaces the response with hardcoded mock data (`// Todo: Testing`). Wasted network + backend work on every mount.

### H-3. OCP breakdown fires duplicate list calls (P1) — **Open**

The breakdown page renders both `OptimizationsProjectsTable` and `OptimizationsContainersTable`, each making separate list requests with overlapping filters.

### H-4. List responses include full detail shape (P1) — **Open**

Backend builds `BuildDetailResponse` per list row (all 3 terms x 2 engines with config, variation, notifications). The table UI only displays one term+engine combination at a time.

**Fix:** Add `?term=short&engine=cost` or `?fields=summary` to the API.

### H-5. Offset pagination when keyset cursor available (P1) — **Open**

UI uses `offset` while the backend supports `?after=<cursor>`. Offset degrades linearly with dataset size.

### H-6. No cross-component cache sharing (P2) — **Open**

Redux cache is keyed by exact query string. Badge, summary, and table each produce different strings, so the same data is fetched multiple times.

---

## Theme I: Dependency Weight

### Binary: 58MB (large for a Go service)

Main drivers: CGO Kafka, dual AWS SDK (v1 for CloudWatch hook + v2 for S3), `go-gota` -> `gonum`.

### GORM vs pgx: properly shared pool (GOOD)

Both share a single `pgxpool` via `stdlib.OpenDBFromPool`. GORM adds query builder overhead but is not catastrophic. Targeted migration of heaviest list SQL to pgx is reasonable; wholesale removal is not.

### logrus: ReportCaller overhead (P2)

`ReportCaller: true` walks the call stack on every log line. Disable in production.

### Viper: negligible runtime cost

Loaded once at startup via `sync.Once`. No action needed.

---

## Complete Priority Matrix (All 9 Themes)

### P0 -- Do First (highest ROI)

| ID | Theme | Change | Est. Impact | Status |
|----|-------|--------|-------------|--------|
| H-1 | UI | Badge/summary: pass `limit=1` | Eliminates 99% of wasted list work | Open |
| H-2 | UI | Remove projects table mock override | Eliminates wasted API calls | Open |
| D-1 | DB Config | Add PostgreSQL tuning to Helm chart (on-prem) | Production correctness | Open |
| D-2 | DB Config | Fix connection budget (expose ROS_DB_MAX_CONNS, remove dead DB_POOL_SIZE) -- both modes | Prevents pool exhaustion | Open |
| P0-1 | Math | Decay weight lookup table or fixed-point | Eliminates math.Exp from hot path | Implemented |
| P0-2 | Math | Fuse CPU + memory weighted passes | ~40-50% recommend CPU | Open |
| P0-3 | DB | Defer org metadata refresh to end of reconcile | 50-90% write time for large orgs | In Progress |
| P0-4 | DB | Migrate list API pagination to org_container_keys | ~1000x on page query | Open |
| A-1 | API | Slim list DTO (skip BuildDetailResponse) | 10-30ms CPU per page | Open |
| A-2 | API | Notification deduplication in JSON | 30-50% JSON payload size | Open |
| B-1 | Ingest | Slim digest group storage | 5-10x less peak RAM | Open |
| C-1 | Ops | Fix readiness probe to use /readyz | Correctness | Open |

### P1 -- High Priority

| ID | Theme | Change | Status |
|----|-------|--------|--------|
| H-3 | UI | Single fetch on OCP breakdown | Open |
| H-4 | API | List field projection (term/engine/fields) | Open |
| H-5 | UI | Adopt cursor pagination | Open |
| P1-1 | Math | Integer savings in micro-cents | Open |
| P1-2 | Math | Integer adaptive margin | Open |
| P1-3 | Math | Zero-copy window slicing | Open |
| P1-4 | DB | Fix rh_accounts join anti-pattern | Open |
| D-3 | DB Config | Per-table autovacuum tuning (migration for both modes; chart tuning for on-prem) | Open |
| D-4 | DB Config | Fix node_recommendations retention (both modes -- app bug) | Open |
| D-5 | DB Config | Add namespace/PVC retention (both modes -- app bug) | Open |
| B-2 | Ingest | Stream namespace CSV | Open |
| B-4 | Ingest | Remove per-row Prometheus gauge | Open |
| C-2 | Ops | VM CSV logs: Debug + counter | Open |
| C-3 | Ops | Audit high-cardinality Prometheus labels | Open |
| C-4 | Ops | Drain in-flight Kafka handlers on shutdown | Open |

### P2 -- Medium Priority

| ID | Theme | Change | Status |
|----|-------|--------|--------|
| E-2 | Lifecycle | Separate sample vs digest retention | Open |
| G-1 | Scale | Increase Kafka partitions (3 -> 12) | Open |
| G-2 | Scale | Add HPA for API pods | Open |
| P2-1-5 | Math/DB | GPU BP config, node BP, batch PVC/GPU writes, VM decouple, idle sort | Open |
| B-3,5,6 | Ingest | String interning, narrow single-tx, GOMEMLIMIT/PGO | Open |
| C-5,6 | Ops | Disable ReportCaller, add make test-short | Open |
| I-1 | Deps | Binary slimming (release ldflags, SDK audit) | Open |

### Strategic

| ID | Theme | Change | Status |
|----|-------|--------|--------|
| S1 | Algo | Unified windowed digest recommender framework | Open |
| S2 | Algo | Parallel container recommend by partition | Open |
| S3 | Algo | Namespace recs from container rollups | Open |
| S4 | API | Slim list contract with UI team | Open |
| F-1 | Observability | Add per-phase pipeline histograms + OpenTelemetry | Open |
| G-3 | Scale | Distributed debouncer (DB/Redis) for multi-pod processors | Open |
