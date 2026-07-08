# Performance Audit Report: ros-ocp-backend Native Engine

**Date:** June 2026
**Scope:** Full performance review of the native recommendation engine across 9 dimensions
**Status:** Tracking remediation progress (see Status column in tables below)

---

## Overall Assessment

**The rewrite achieved its primary goal:** digest data is fully `int64`, percentiles are precomputed at ingest time (not at recommendation time), and the streaming recommendation engine bounds memory. The `MarginScale`/basis-points pattern is solid where used. **All P0, P1, Medium Effort, and nearly all P2 roadmap items are now implemented** (decay lookup tables, fused CPU+memory passes, zero-copy window slicing, integer adaptive margin, deferred org metadata refresh, batched PVC/GPU writes, list API pagination via `org_container_keys`, integer micro-cents savings, VM recommendation deferral, autovacuum tuning, graceful shutdown drain, GPU/node basis-point thresholds, high-cardinality label cleanup, `GOMEMLIMIT` in Helm, `/healthz` liveness probes, Grafana dashboard modernization, Kafka partition scaling, OCP breakdown dedup, namespace CSV streaming, digest-based percentile-band plots, separate sample retention, PostgreSQL tuning in Helm chart, per-phase pipeline histograms, notification deduplication, typed `Collection[T]` list responses, HPA for API pods, slim list contract S4 with UI alignment, and operational quick wins). **Strategic items S1–S3 remain explicitly deferred** with detailed rationale. Remaining gaps are likewise deferred with documented revisit triggers (see [Deferred Items — Revisit Triggers](#deferred-items--revisit-triggers) below). The only partially implemented item is I-1 (AWS SDK v1 audit).

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
- `windowBounds` uses binary search for zero-copy window slicing
- `RecommendCPUAndMemory` fuses CPU + memory weighted passes in one decay loop
- `MultiWeightedPercentileWithExtras` fuses 5 extractors + idle + trend in one pass
- `pgx.Batch` for container, namespace, PVC, and GPU recommendation writes
- Integer micro-cents savings (`savings_int.go`, `MicroCentsPerDollar = 100_000_000`) — rate conversion once at boundary
- Graceful processor shutdown drain (`sync.WaitGroup`, `ROS_SHUTDOWN_TIMEOUT_SECONDS`, `ctx.Done()` between phases)
- VM recommendations deferred to post-manifest phase (matching container recommendation pattern)
- GPU classification thresholds precomputed as basis points at load time (`normalizeGPUThresholds`)
- Node utilization classification uses `UtilizationBasisPoints` (integer BP, no float ratios)
- Fleet Prometheus metrics use bounded labels only; tenant context via structured logs (ADR-0243)
- `GOMEMLIMIT` injected into all ROS deployments via cost-onprem Helm chart (`ros.goMemLimit`)
- `/healthz` deep liveness endpoint (goroutine count, GC pause, scheduler deadlock canary)
- Grafana dashboard modernized for on-prem + SaaS (pod patterns, null→N/A mappings, static validation tests)

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

### P0-2. Duplicate CPU + memory weighted passes per term x engine — **Implemented**

**Location:** `internal/engine/recommend_cpu_and_memory.go` — `RecommendCPUAndMemory`

`RecommendCPU` and `RecommendMemory` each called `MultiWeightedPercentileWithExtras` separately with their own extractors, recomputing decay weights for the same rows. That's **12 passes** per container (3 terms x 2 engines x 2 resources) when it could be **6** (fused CPU+memory in one pass with 10 extractors).

**Fix implemented (2026-06):**

- Merged into `RecommendCPUAndMemory` in `recommend_cpu_and_memory.go`, which calls `MultiWeightedPercentileWithExtras` once with all 10 extractors per term x engine.

**Impact:** ~40-50% reduction in recommend-phase CPU per container.

### P0-3. Org-wide metadata refresh on every write batch — **Implemented**

**Location:** `internal/engine/recommend_all.go` — `WriteRecommendations`, `RefreshOrgMetadata`; `internal/services/report_processor.go` — `runContainerRecommendations`

Every streaming batch (500 containers) triggered `RefreshOrgContainerKeys` + `RefreshOrgRecommendationStats`, each doing a full `DISTINCT ON` scan of `recommendation_sets` for the entire org. For a 10k-container org, that's ~20 full-org scans per reconciliation cycle.

**Fix implemented (2026-06):** [ADR-0289](../adr/0289-defer-org-metadata-refresh-end-of-reconcile.md)

- Removed per-batch refresh from `WriteRecommendations`.
- Added `RefreshOrgMetadata` called once at end of `runContainerRecommendations` and `recalculateContainerCluster`.
- Tests/tooling use `WriteRecommendationsAndRefreshOrg` for single-batch writes.

**Impact:** 50-90% reduction in recommendation write time for 10k+ container orgs.

### P0-4. List API still paginates via `DISTINCT ON recommendation_sets` — **Implemented**

**Location:** `internal/model/container_recommendation_pagination.go`, `internal/model/recommendation_set_native.go`

Despite `org_container_keys` existing for fast counts, page selection still ran `DISTINCT ON` over `recommendation_sets`. Documented internally as ~1.3s vs ~0.3ms for key-table pagination at 200k containers.

**Fix implemented (2026-06):**

- Page selection now goes through `org_container_keys` with indexed lookup; `recommendation_sets` is joined only for selected page rows.
- New helpers in `container_recommendation_pagination.go`.

**Impact:** ~1000x on pagination subquery for large tenants.

---

## P1 -- High Priority

### P1-1. Savings estimation is entirely float64 — **Implemented**

**Location:** `internal/engine/savings_int.go`, `savings.go`, `gpu_recommender.go`, `vm_savings.go`, `node_savings.go`, `pvc_savings.go`, `recommend_cluster_quota.go`, `recommend_quota.go`

Per recommendation row: millicore -> cores (`/1000.0`), KiB -> GiB (`/1024/1024`), multiply by `float64` rates, multiply by `730.0` hours/month, then `math.Round(total*100)/100`.

**Fix implemented (2026-06):** [ADR-0291](../adr/0291-integer-micro-cents-savings-computation.md)

- New `savings_int.go` with `MicroCentsPerDollar = 100_000_000`. Seven modules migrated from `float64` to integer math.
- Rate conversion happens once at boundary; cents returned at API boundary.

**Impact:** Moderate-significant depending on cluster size; also reduces code duplication.

### P1-2. Adaptive margin float CV detour — **Implemented**

**Location:** `internal/engine/margin.go` — `ComputeAdaptiveMarginScaledDirect`

`ComputeAdaptiveMargin` computed `cv = float64(p95-p50) / float64(mean)` then scaled back to `MarginScale`. The integer equivalent is `cvScaled = (p95-p50) * MarginScale / mean` -- pure integer division.

**Fix implemented (2026-06):**

- Replaced float CV path with integer-only `ComputeAdaptiveMarginScaledDirect` using scaled integer division.

**Impact:** Moderate -- one float division per rec eliminated; aligns with existing `ApplyScaledMargin` pattern.

### P1-3. `filterByWindow` allocates per term per container — **Implemented**

**Location:** `internal/engine/window_bounds.go` — `windowBounds`

Each term created a new `[]DigestRow` copy. With 3 terms + idle window = 4 allocations per container.

**Fix implemented (2026-06):**

- `windowBounds` returns `(lo, hi)` index ranges into the shared backing slice instead of copying rows. Windows are processed sequentially over the same slice.

**Impact:** 15-25% less allocation/GC during recommend on large clusters.

### P1-4. `rh_accounts` join anti-pattern — **Implemented**

**Location:** `GetRecommendationQuality`, `nativeContainerDetailQuery`, `loadClusterLastReportedAt`

Tables that have `org_id` columns joined `rh_accounts` for org filtering instead of filtering directly on `org_id`.

**Fix implemented (2026-06):**

- All affected queries now use direct `WHERE org_id = ?` filtering instead of joining `rh_accounts`.

**Impact:** 10-5000x on affected query plans per internal EXPLAIN audit.

---

## P2 -- Medium Priority

### P2-1. GPU threshold conversion at every classify call — **Implemented**

**Location:** `internal/engine/gpu_recommender.go` — `normalizeGPUThresholds`, `GPUThresholds`

`GPUThresholds` stored six `float64` thresholds, converted to basis points via `ThresholdToBasisPoints` at each classify call.

**Fix implemented (2026-06):**

- `normalizeGPUThresholds` precomputes all six classification thresholds as `int32` basis points at load time (`GPUThresholdsFromConfig`, `DefaultGPUThresholds`). `Classify` and `SelectMIGProfile` use precomputed BP fields directly.

**Impact:** Eliminates per-classify float→BP conversion on GPU-heavy clusters.

### P2-2. Node utilization as float ratios instead of basis points — **Implemented**

**Location:** `internal/engine/recommend_nodes.go` — `UtilizationBasisPoints`

`cpuUtil50 := float64(d.CPUUsageP50MC) / float64(allocCPU)` computed float ratios at classification time.

**Fix implemented (2026-06):**

- Replaced float ratio comparisons with `UtilizationBasisPoints(usage, alloc)` integer math. `NodeRecommendationConfig` stores `UnderutilThresholdBP` and `OvercommitThresholdBP` as precomputed basis points.

**Impact:** Completes basis-point migration for node classification; removes float division from hot path.

### P2-3. PVC and GPU writes not batched — **Implemented**

**Location:** `internal/engine/pvc_recommend.go`, `internal/engine/gpu_recommender.go`

Container/namespace used `pgx.Batch`; PVC and GPU did not.

**Fix implemented (2026-06):**

- `WritePVCRecommendations` and `StoreGPUClassifications` now use `pgx.Batch` for batched writes.

**Impact:** Reduced DB round-trips for PVC and GPU recommendation persistence.

### P2-4. VM recommendations run synchronously on ingest — **Implemented**

**Location:** `internal/services/report_processor.go`

`RunVMRecommendations` ran inline after VM digest upsert, blocking the ingest pipeline.

**Fix implemented (2026-06):**

- VM recommendations moved from inline during CSV ingest to deferred post-manifest phase (matching container recommendation pattern).

**Impact:** Reduced ingest latency; VM recs no longer block CSV processing.

### P2-5. Idle classification sorts when max/weighted-max would suffice — **Implemented**

**Location:** `internal/engine/idle_classification.go` — `maxDailyCPUUsageP95`, `maxDailyMemUsageP95`

`percentile95Int64` created a copy and sorted for exact P95 across ~15 daily values. Max of daily P95s is cheaper and semantically close enough for idle detection.

**Fix implemented (2026-06):**

- Replaced idle P95 sort with `maxDailyCPUUsageP95` / `maxDailyMemUsageP95` (max of per-day P95 values).

**Impact:** Eliminates 2 sorts per container during idle classification.

---

## Basis Points Consistency Map


| Area                    | Status                                         | Gap                                  |
| ----------------------- | ---------------------------------------------- | ------------------------------------ |
| Digest data (CPU/mem)   | int64 millicores/KiB                           | None                                 |
| GPU utilization         | int32 basis points                             | None                                 |
| Quota/CRQ headroom      | int BP via `applyHeadroom`                     | None                                 |
| VM power-off idle ratio | int32 BP                                       | None                                 |
| Margin application      | `MarginScale` + `ApplyScaledMargin`            | None                                 |
| Margin computation      | int BP via `ComputeAdaptiveMarginScaledDirect` | None                                 |
| GPU thresholds          | int32 BP (precomputed at load)                 | None                                 |
| Node utilization        | int32 BP via `UtilizationBasisPoints`          | None                                 |
| PVC usage ratio         | **float64**                                    | Could be BP                          |
| Savings rates           | int64 micro-cents                              | None                                 |
| VM sizing margins       | **float64**                                    | Mirror container `ApplyScaledMargin` |


---

## Hot Path Execution Map (Container -- Highest Volume)

```
RecommendWorkloadsStreaming
  per container (stream batch 500)
    ClassifyIdleState .............. max-of-daily-P95 (no sort)     [P2-5 done]
    per term (short/medium/long)
      windowBounds ................. binary search, zero-copy indices [Q2 done]
      per engine (cost/performance)
        RecommendCPUAndMemory ...... single fused decay loop O(W)   [Q1 done]
          decay weight ............. lookup table (no math.Exp)     [P0-1 done]
        computeVariation x4 ....... float round (could be integer)
        EvaluateNotifications ...... integer comparisons (good)
  ApplySavingsEstimates (batch) .... integer micro-cents per rec  [P1-1 done]
```

**Total inner loop iterations per container:** 3 terms x 2 engines x 1 (fused CPU+mem) x ~7-15 digest rows = **42-90 decay weight computations** (down from 84-180 before Q1).

---

## Prioritized Remediation Roadmap

### Quick Wins (1-2 days each, high ROI)


| ID  | Change                                           | Impact                            | Risk              | Status                                        |
| --- | ------------------------------------------------ | --------------------------------- | ----------------- | --------------------------------------------- |
| Q1  | Fuse CPU + memory weighted passes                | ~40-50% recommend CPU             | Low               | Implemented                                   |
| Q2  | Zero-copy window slicing                         | ~15-25% less alloc/GC             | Low               | Implemented                                   |
| Q3  | Reuse decay weights across engines within a term | ~15% recommend CPU                | Low               | Declined (negligible after P0-1; ~2ns lookup) |
| Q4  | Replace idle P95 sort with max-of-daily-P95      | Eliminates 2 sorts x N containers | Medium (behavior) | Implemented                                   |
| Q5  | Batch PVC writes via pgx.Batch                   | High DB latency improvement       | Low               | Implemented                                   |
| Q6  | Batch GPU classification UPDATEs                 | Significant for GPU clusters      | Low               | Implemented                                   |


### Medium Effort (3-5 days each)


| ID  | Change                                            | Impact                           | Risk   | Status      |
| --- | ------------------------------------------------- | -------------------------------- | ------ | ----------- |
| M1  | Defer org metadata refresh to end of reconcile    | 50-90% write time for large orgs | Medium | Implemented |
| M2  | Migrate list API pagination to org_container_keys | ~1000x on page query             | Medium | Implemented |
| M3  | Integer savings in micro-cents                    | Eliminates float in savings      | Medium | Implemented |
| M4  | Decay weight lookup table                         | Eliminates math.Exp              | Low    | Implemented |
| M5  | Integer adaptive margin (remove float CV)         | Completes BP migration           | Low    | Implemented |
| M6  | Decouple VM recommend from ingest                 | Large ingest latency win         | Medium | Implemented |


### Strategic (1-2 weeks each)


| ID  | Change                                              | Impact                          | Risk           | Status   |
| --- | --------------------------------------------------- | ------------------------------- | -------------- | -------- |
| S1  | Unified windowed digest recommender framework       | 30-50% less code duplication    | High           | Deferred    |
| S2  | Parallel container recommend by namespace partition | Linear speedup on multi-core    | High           | Deferred    |
| S3  | Namespace recs from container rollups               | Eliminates duplicate engine run | High (product) | Deferred    |
| S4  | Slim list contract with UI team                     | High (API+UI payload)           | Med            | Implemented |


---

## Theme A: API Response Assembly and Serialization

### A-1. Double assembly per container in list API (P0) — **Implemented**

**Location:** `internal/api/list_response.go` — `BuildListResponse`

Each page row was assembled twice: `assembleNativeResults` built `NativeContainerResult` with `MapToKruizeFormat`, then `BuildDetailResponse` rebuilt the full Kruize-shaped nested JSON. This cost ~10-30ms CPU per 100-item page on top of DB/enrichment time.

**Fix implemented (2026-06):**

- Introduced slim list DTO via `BuildListResponse` (table columns + codes only). List APIs skip `BuildDetailResponse`; detail-by-ID still uses full assembly.

### A-2. Notification triplication in JSON (P0) — **Implemented**

**Location:** `internal/model/detail_response.go`

The same notification codes were copied to three JSON levels: `recommendations.notifications`, `recommendation_terms.<term>.notifications`, and `recommendation_engines.<engine>.notifications`. This added several KB per container redundantly.

**Fix implemented (2026-06):**

- Detail: emit notifications at engine level only (ADR-0293).
- List: `recommendations.notification_codes` integer array; UI resolves via catalog endpoint.

### A-3. `Collection.Data []interface{}` forces boxing (P1) — **Implemented**

**Location:** `internal/api/common.go` — `Collection[T any]`

Every list item was heap-allocated and interface-boxed. Blocked compile-time JSON optimizations.

**Fix implemented (2026-06):**

- Replaced `Collection.Data []interface{}` with generic `Collection[T any]` and `Data []T`, eliminating per-item boxing in list responses.

### A-4. Double identity parsing per request (P1) — **Implemented**

**Location:** `internal/api/middleware/identity.go`, `entitlement.go`

The `x-rh-identity` header was base64-decoded and JSON-unmarshaled twice (identity middleware + entitlement middleware). ~0.2-0.8ms per request.

**Fix implemented (2026-06):**

- Identity middleware parses `x-rh-identity` once and stores parsed identity on `echo.Context`. Entitlement middleware reads from context instead of re-parsing.

### A-5. Legacy Kruize path: `map[string]interface{}` (P1) — **Deferred**

**Location:** `internal/plugins/kruize/api_compat.go:453`

Legacy list/detail unmarshals JSONB into generic maps, mutates in place, then marshals again. Worst path for allocator churn and reflection. Impact only when Kruize fallback is active.

**Deferred:** The legacy Kruize code path is deprecated. No point optimizing code that will be removed. Revisit only if the legacy path is retained long-term.

### A-6. Cache headers on notification catalog (P2) — **Implemented**

`GetNotificationCodes` is static in-memory.

**Fix implemented (2026-06):**

- `GetNotificationCodes` now returns `Cache-Control: public, max-age=86400`.

### S4. Slim list contract with UI team (Strategic) — **Implemented**

**Location:** `internal/model/list_response.go`, `internal/api/handlers.go`, ADR-0294

Namespace list responses previously assembled full `NamespaceDetailResponse` rows for every list item. Container list already used a slim DTO.

**Fix implemented (2026-06):**

- Slim list contract defined and implemented. Container list already had slim DTO (`BuildListResponse`). Namespace list now has `BuildNamespaceListResponse` (opt-in via `term`/`engine` params, full response by default for backward compatibility). ADR-0294 documents the contract.

---

## Theme B: Ingestion Pipeline and Memory

### B-1. Digest groups retain full MetricRow structs (P0) — **Implemented**

**Location:** `internal/ingestion/pipeline_stream.go` — `metricSample`

Between flushes, `groupedAll` and `groupedBH` stored full `MetricRow` (~456 bytes + heap strings) per sample. For 1000 container-days x 96 samples/day = ~96,000 copies = **50-120 MB**.

**Fix implemented (2026-06):**

- Replaced `MetricRow` storage with slim `metricSample` struct (6x int64 + time.Time = ~72 bytes). Convert from `MetricRow` once at append time.

**Impact:** ~5-10x RAM reduction during ingest grouping.

### B-2. Namespace CSV not streaming (P1) — **Implemented**

**Location:** `internal/ingestion/namespace_stream.go` — `forEachNamespaceCSVRow`, `parseAndDigestNamespaceCSVStream`

Namespace CSV was fully materialized into `[]NamespaceMetricRow` before grouping, unlike the container path which streams.

**Fix implemented (2026-06):**

- Added `forEachNamespaceCSVRow` + `parseAndDigestNamespaceCSVStream` mirroring container `forEachCSVRow` / `parseAndDigestCSVStream`.
- Digest groups flush at `ROS_INGEST_FLUSH_BATCH_SIZE` (default 5000).
- Quota digests accumulate incrementally per row instead of requiring a full-row slice.

### B-3. No string interning for repeated keys (P2) — **Deferred**

`DigestKey` copies `Namespace`, `Workload`, `ContainerName` from every row. These repeat heavily across rows. An intern table could reduce heap pressure on large clusters.

**Deferred:** Diminishing returns; Go string interning via `sync.Pool` or manual interning adds complexity for marginal memory savings. Revisit only if memory profiling shows string duplication as a significant contributor.

### B-4. Per-row Prometheus gauge update during CSV ingest (P1) — **Implemented**

**Location:** `internal/ingestion/pipeline_stream.go`

`SetIngestGroupsInMemory(groupCount)` was called on **every CSV row** in the streaming loop. On a 500k-row manifest, that's 500k Prometheus gauge updates with mutex + label lookup.

**Fix implemented (2026-06):**

- Removed per-row gauge update from the streaming loop. Gauge is updated only on flush boundaries.

### B-5. Single-tx fast path holds all deferred samples in RAM (P2) — **Implemented**

When `rowCount <= 50,000` and no incremental flushes have occurred, the single-tx path held all samples in memory before committing. Could spike to tens of MB on large single-file manifests.

**Fix implemented (2026-06):**

- Lowered row threshold to 25,000 and added digest group threshold of 5,000. Single-tx fast path requires both limits and zero incremental flushes (`singleIngestTxEligible` in `internal/ingestion/pipeline.go`).

### B-6. No PGO or GOMEMLIMIT in deployment (P2) — **Partially Implemented**

- Profile-Guided Optimization (`-pgo`) in the Docker build — **Deferred**
- `GOMEMLIMIT` not set in the container — **resolved**

**Fix implemented (2026-06) for GOMEMLIMIT:**

- cost-onprem Helm chart injects `GOMEMLIMIT` via `ros.goMemLimit` template (~90% of container memory limit) into API, processor, housekeeper, recommendation-poller, and partition-cleaner deployments.
- Documented in `docs/operations/configuration.md` and validated by `tests/suites/helm/test_chart_lint.py`.

**PGO deferred:** Requires production CPU profiles and CI pipeline changes. `GOMEMLIMIT` is already done. Revisit when CI pipeline supports PGO build integration.

---

## Theme C: Observability and Operational Overhead

### C-1. ROS API readiness probe points at /status, not /readyz (P0) — **Resolved**

**Location:** `cost-onprem/templates/ros/api/deployment.yaml`

The Helm chart wired readiness to `/status` (static JSON, no dependency checks) instead of `/readyz` (which pings the database). Pods were marked ready while DB was down.

**Fix:** Point `readinessProbe` at `/readyz`; keep `livenessProbe` on `/healthz` (deep runtime checks). Implemented in cost-onprem-chart (`ros.api.readinessProbe.path` defaults to `/readyz`; `livenessProbe.path` defaults to `/healthz`).

### C-2. VM CSV warn-log storms (P1) — **Implemented**

**Location:** `internal/ingestion/vm_csv.go:294`

Every bad VM CSV row logged `Warnf`. A noisy file could generate thousands of log lines per manifest. Container CSV correctly uses `Debugf`.

**Fix implemented (2026-06):**

- Per-row `Warnf` downgraded to `Debugf`.
- New `rosocp_csv_rows_skipped_total{report_type="vm"}` counter.
- Summary `Warnf` at end when skips > 0.

### C-3. High-cardinality Prometheus labels (P1) — **Implemented**

Several metrics used `org_id` and `cluster_uuid` as labels: `rosocp_analytics_incomplete_total`, `ros_recommendation_stability`, `ros_reship_in_progress`, etc. This created unbounded cardinality.

**Fix implemented (2026-06):** [ADR-0243](../adr/0243-high-cardinality-analytics-incomplete-labels.md)

- Removed tenant labels from fleet metrics; bounded labels only (`error_type`, `recommendation_type`, `status`, etc.).
- Gauges converted to unlabeled histograms where appropriate (`ros_recommendation_stability`, `ros_recommendation_adoption_rate`, `ros_recommendation_oom_rate`).
- Per-org/cluster context emitted via structured logs at metric call sites.
- Grafana dashboard queries updated to match new label schema.

**Impact:** Prevents Prometheus memory blow-up at fleet scale; breaking change for dashboards filtering by org/cluster (mitigated by dashboard modernization).

### C-4. Processor shutdown doesn't drain in-flight work (P1) — **Implemented**

On SIGTERM, the Kafka consumer loop exited but running `ProcessReport` handlers (which may be mid-CSV or mid-recommendation) were not awaited. Risk: partial processing, duplicate work on retry.

**Fix implemented (2026-06):**

- `sync.WaitGroup` for in-flight Kafka handlers.
- Configurable `ROS_SHUTDOWN_TIMEOUT_SECONDS` (default 30).
- `ProcessReport` checks `ctx.Done()` between phases.
- Interrupted processing treats `context.Canceled` as transient for safe redelivery.

### C-5. `ReportCaller: true` in production logging (P2) — **Declined**

**Location:** `internal/logging/logging.go`

Every log line walks the call stack. Minor but unnecessary overhead in production.

**Fix:** Disable `ReportCaller` when `LOG_LEVEL >= INFO`.

**Status:** Reverted — negligible performance gain (~200-500ns/line) does not justify loss of file:line in production logs for support/debugging.

### C-6. No `make test-short` for fast local iteration (P2) — **Implemented**

Full test suite requires Docker and takes ~25 minutes serial. No documented fast-path for unit-only runs.

**Fix implemented (2026-06):**

- New Makefile target `make test-short` runs `go test -short ./... -count=1`, completes in ~15 seconds.
- Documented in `CONTRIBUTING.md`.

### C-7. No deep liveness endpoint (P1) — **Implemented**

**Location:** `internal/health/healthz.go`, `internal/api/handlers_healthz.go`, cost-onprem `livenessProbe`

Only `/status` (static 200) existed. No goroutine leak, GC pause, or deadlock detection for Kubernetes liveness.

**Fix implemented (2026-06):**

- New `GET /healthz` endpoint checks goroutine count (`ROS_HEALTHZ_MAX_GOROUTINES`), last GC pause (`ROS_HEALTHZ_MAX_GC_PAUSE_MS`), and scheduler responsiveness (deadlock canary).
- cost-onprem chart wires `livenessProbe` to `/healthz` for API, processor, and recommendation-poller.
- Readiness remains on `/readyz` (DB connectivity).

**Impact:** Pods with goroutine leaks or GC stalls are restarted automatically instead of serving degraded traffic indefinitely.

### C-8. Grafana dashboard stale for native engine and on-prem (P1) — **Implemented**

**Location:** `dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml`

Dashboard retained SaaS-specific variables (RDS, Kafka lag by topic), missing native-engine panels, and null-value mappings that rendered as "null" in Grafana.

**Fix implemented (2026-06):**

- Removed SaaS-only template variables; added on-prem pod label patterns (`app.kubernetes.io/component`).
- Added engine performance, cache, ingest, and lifecycle panels.
- Fixed null→N/A value mappings across stat panels.
- Updated PromQL to match C-3 label schema changes.

### C-9. No static validation for Grafana dashboard (P2) — **Implemented**

**Location:** `internal/dashboard/dashboard_test.go`

Dashboard JSON was hand-edited with no CI guard against broken PromQL, missing metrics, or stale metric names.

**Fix implemented (2026-06):**

- Static validation tests parse the ConfigMap YAML, extract dashboard JSON, and verify: all PromQL metric references exist in `internal/metrics/` or documented externals; no removed high-cardinality label filters; panel structure integrity.
- Runs in `make test-short` (~15s).

---

## Theme D: PostgreSQL Server-Side Tuning

**Deployment context:** ROS runs against two very different PostgreSQL environments:


| Aspect      | SaaS (console.redhat.com)                                     | On-Prem (cost-onprem chart)                               |
| ----------- | ------------------------------------------------------------- | --------------------------------------------------------- |
| Engine      | **AWS RDS PostgreSQL** (managed)                              | **Red Hat build of PostgreSQL** (container in Helm chart) |
| Tuning      | RDS parameter groups, automated backups, auto-scaling storage | No `postgresql.conf` exposed; stock image defaults        |
| RAM         | RDS instance class (e.g., db.r6g.xlarge = 32Gi)               | **512Mi** (chart default)                                 |
| Storage     | GP3/io2, auto-expanding                                       | **30Gi PVC** shared across Koku + ROS + RBAC              |
| Connections | RDS scales with instance class; IAM auth possible             | `max_connections=100` (stock default)                     |
| Autovacuum  | RDS-managed, tunable via parameter groups                     | Stock defaults; no per-table tuning                       |
| HA          | Multi-AZ, read replicas                                       | Single StatefulSet pod                                    |
| Monitoring  | CloudWatch, Performance Insights, Enhanced Monitoring         | `pg_stat_statements` only (if enabled)                    |


**Key implication:** D-1 through D-3 are primarily **on-prem concerns**. SaaS RDS deployments have managed tuning, adequate resources, and monitored vacuum. On-prem is where the chart's undersized defaults become production risks. However, D-4 and D-5 (retention gaps) affect **both** deployment modes -- they are application-level bugs.

### D-1. No PostgreSQL tuning exposed in Helm chart (CRITICAL -- on-prem only) — **Implemented**

The bundled DB is stock Red Hat PostgreSQL with **512Mi RAM and 30Gi shared disk**. No `postgresql.conf` customization. At 10k containers, `container_usage_samples` alone can reach **86M rows** in 90 days, needing 100-150Gi disk just for ROS.

SaaS: Not applicable -- RDS instance class and parameter groups are managed separately.

**Recommended on-prem production profile (10k containers):**

- PG memory: 4-8Gi, storage: 100-150Gi
- `shared_buffers`: 1-2GB, `work_mem`: 16-32MB
- `max_connections`: 200 (or add PgBouncer)
- Expose a `database.server.postgresqlConfiguration` in chart values or document mandatory external DB specs for production

**Fix implemented (2026-06):**
- cost-onprem Helm chart exposes `database.postgresqlConfiguration` with curated knobs (`shared_buffers`, `work_mem`, `effective_cache_size`, `max_connections`, `maintenance_work_mem`, `random_page_cost`, autovacuum settings).
- ConfigMap renders `custom-postgresql.conf` mounted at `/opt/app-root/src/postgresql-cfg/` (sclorg include hook).
- `database.resources` and `database.storage.size` exposed for container memory and PVC sizing.
- Database tuning guide with sizing profiles (Demo/Small/Medium/Large) in `docs/operations/database-tuning.md`.

### D-2. Connection budget exceeds max_connections (CRITICAL -- on-prem; MEDIUM -- SaaS) — **Implemented**

With default `ROS_DB_MAX_CONNS=10` per process across ~6 ROS pods + Koku + RBAC + Kruize, total connections could reach **85-150** against PostgreSQL's pldefault `max_connections=100`. Chart set legacy `DB_POOL_SIZE` that ros-ocp-backend did not read.

**Fix implemented (2026-06):**

- `ROS_DB_MAX_CONNS` default lowered to **5** per process to fit within connection budget.
- `DB_POOL_SIZE` retained as a deprecated alias that maps to `ROS_DB_MAX_CONNS` for backward compatibility.

SaaS: RDS instances typically have higher `max_connections`; the lower per-process default still reduces pool pressure on shared clusters.

### D-3. UPDATE-heavy tables need autovacuum tuning (HIGH -- on-prem; MEDIUM -- SaaS) — **Implemented**

`recommendation_sets` and `container_usage_samples` get frequent UPSERTs creating dead tuples. No `fillfactor` or per-table autovacuum tuning existed.

**Fix implemented (2026-06):**

- Migration 000144 sets `autovacuum_vacuum_scale_factor=0.05`, `autovacuum_analyze_scale_factor=0.02`, `fillfactor=85` on `recommendation_sets` and `container_usage_samples` partitions.
- New partitions inherit reloptions. Works on both RDS and on-prem.

### D-4. `node_recommendations` retention gap (HIGH -- both modes) — **Implemented**

Listed in retention plugin but was **not partitioned** -- sweep was a no-op. Rows were only removed on Sources destroy, not by age.

**Fix implemented (2026-06):**

- Added `node_recommendations` to `dateRetainedTables` in `retention.go` for age-based cleanup.

### D-5. `namespace_recommendation_sets` and `pvc_recommendation_sets` lack age-based retention (HIGH -- both modes) — **Implemented**

No periodic cleanup; only removed on Sources destroy.

**Fix implemented (2026-06):**

- Added `namespace_recommendation_sets` and `pvc_recommendation_sets` to `dateRetainedTables` in `retention.go` for age-based cleanup.

---

## Theme E: Data Lifecycle

### Growth projections


| Scale           | `container_usage_samples` (90d) | `recommendation_sets` | `recommendation_history` (90d) | Disk estimate |
| --------------- | ------------------------------- | --------------------- | ------------------------------ | ------------- |
| 1k containers   | 8.6M                            | 6k                    | 540k                           | ~5-10Gi       |
| 10k containers  | 86M                             | 60k                   | 5.4M                           | ~35-70Gi      |
| 100k containers | 864M                            | 600k                  | 54M                            | ~350-700Gi    |


**Key insight:** `container_usage_samples` drives 90%+ of storage. Consider shorter sample retention vs digest retention -- digests alone suffice for the 90-day recommendation lookback.

### E-1. Partition DROP for digests/samples (GOOD)

Monthly partition dropping avoids mass DELETE autovacuum death spirals. Already implemented and working.

### E-2. Consider separate sample retention (MEDIUM) — **Implemented**

Samples are no longer needed for plot drill-down (detail API reads `daily_*_digests`).
`ROS_SAMPLE_RETENTION_DAYS` (default 45) sweeps `container_usage_samples` and
`namespace_usage_samples` independently of `ROS_RETENTION_MONTHS` (default 6).
Plot response shape changed from boxplot (`min`/`q1`/`median`/`q3`/`max`) to
percentile-band (`p50`/`p95`/`p99`/`max`). See [ADR-0292](../adr/0292-digest-based-plot-percentile-bands.md).

---

## Theme F: End-to-End Latency Budget

### Ingest flow (500 containers, 30-day CSV)


| Phase                          | Est. wall-clock | Bound         |
| ------------------------------ | --------------- | ------------- |
| Kafka receive                  | <100ms          | I/O           |
| CSV download                   | **5-30s**       | I/O (HTTP/S3) |
| CSV parse + digest + DB writes | **15-45s**      | CPU + DB      |
| Recommendations computed       | **0.5-3s**      | CPU           |
| Recommendations written        | **1-5s**        | DB            |
| Post-processing (GPU + node)   | **2-10s**       | CPU + DB      |
| Org metadata refresh           | <1s             | DB            |
| **Total**                      | **~30-90s**     |               |


**CSV download and parse+digest dominate** (~~70-80% of wall-clock). Recommendation compute is fast (~~5% of total). The biggest latency wins are in ingest I/O and DB write batching, not in the recommendation math.

### API flow (p50/p95)


| Phase                                   | p50       | p95                |
| --------------------------------------- | --------- | ------------------ |
| Middleware (identity + RBAC)            | <2ms      | <150ms (RBAC miss) |
| DB query (list with org_container_keys) | <5ms      | <10ms              |
| Enrichment (BH, currency, GPU)          | <5ms      | <20ms              |
| JSON assembly + gzip                    | 2-8ms     | 10-25ms            |
| **Total**                               | **~15ms** | **~200ms**         |


**RBAC cache misses dominate p95.** DB queries are fast with proper indexing.

### Instrumentation gaps

- ~~Pipeline phases `recommend`, `write`, `quality`, `history` are not separately histogrammed~~ **Resolved (F-1):** `rosocp_pipeline_phase_duration_seconds{phase}` histogram added for 7 phases
- No OpenTelemetry tracing (deferred — Prometheus histograms sufficient for now)
- Masu `GetEffectiveRates` has no duration histogram

---

## Theme G: Horizontal Scaling

### Scaling ceilings (ordered by when you hit them)


| Fleet size    | First wall          | Bottleneck                                                                                                  |
| ------------- | ------------------- | ----------------------------------------------------------------------------------------------------------- |
| 1k            | None                | --                                                                                                          |
| 10k           | Ingest throughput   | 3 Kafka partitions cap parallel ingest                                                                      |
| 50k           | DB connections      | ~~N pods x 10 conns > max_connections~~ **Resolved (D-2):** default `ROS_DB_MAX_CONNS=5` fits within budget |
| 100k          | DB write throughput | `recommendation_sets` UPSERT contention                                                                     |
| 100k+ per org | API aggregation     | Fleet/savings summary cache misses                                                                          |


### Key constraints

- **Kafka: ~~3 partitions~~ configurable (default 12)** -- G-1 resolved; partition count now set via Helm values (`kafka.topics.rosEvents.partitions`).
- **~~No HPA~~** -- **Resolved (G-2):** CPU-based HPA available for ROS API pods (`ros.api.autoscaling.enabled`; disabled by default)
- **Synth manifest debouncer is process-local** -- not coordinated across pods; can cause duplicate rec runs (G-3 deferred; see revisit triggers)
- **In-process caches not shared** -- fleet/savings/RBAC/threshold caches are per-pod with TTL staleness

### Scaling model

- **API pods:** Stateless, scale freely (watch DB pool x replicas)
- **Processor pods:** Scale up to Kafka partition count (default 12 after G-1)
- **Housekeeper:** Singleton
- **Scaling ingest:** Partition count now configurable via Helm values (G-1 implemented)

---

## Theme H: Frontend/API Contract Efficiency

**This may be the single highest-ROI optimization area.**

### H-1. Badge/summary fetch 100-row list for a count (P0) — **Implemented**

`OptimizationsBadge` and `OptimizationsSummary` called the list API with default `limit=100` but only read `meta.count`. Each call materialized up to 100 full recommendation objects with all enrichment.

**Fix implemented (2026-06):**

- `optimizationsBadge.tsx` and `optimizationsSummary.tsx` now pass `limit=1` to the list API (count still available via `meta.count`).

### H-2. Projects table fetches then overwrites with mocks (P0) — **Implemented**

`optimizationsProjectsTable.tsx` made a full list API call then replaced the response with hardcoded mock data (`// Todo: Testing`). Wasted network + backend work on every mount.

**Fix implemented (2026-06):**

- Removed mock override in `optimizationsProjectsTable.tsx`; table now renders live API response.

### H-3. OCP breakdown fires duplicate list calls (P1) — **Implemented**

The breakdown page rendered both `OptimizationsProjectsTable` and `OptimizationsContainersTable`, each making separate list requests with overlapping filters.

**Fix implemented (2026-06):**

- Lifted shared fetch logic into `useOptimizationsNamespacesReport` hook in koku-ui.
- Both tables now receive the shared report via props instead of making independent API calls.
- Eliminates ~50% of breakdown-page API traffic.

### H-4. List responses include full detail shape (P1) — **Implemented**

Backend builds `BuildDetailResponse` per list row (all 3 terms x 2 engines with config, variation, notifications). The table UI only displays one term+engine combination at a time.

**Fix implemented (2026-06):**

- UI passes explicit `term=short_term&engine=cost` on list calls. Backend skips enrichment (GPU/business-hours/currency) for `limit<=1` count-only calls. Namespace list supports `term`/`filter[term]` param.

### H-5. Offset pagination when keyset cursor available (P1) — **Implemented**

UI uses `offset` while the backend supports `?after=<cursor>`. Offset degrades linearly with dataset size.

**Fix implemented (2026-06):**

- UI prefers keyset cursor (`after=next_cursor`) for forward pagination; offset fallback for backward/arbitrary page jumps. Backend already supported cursors.

### H-6. No cross-component cache sharing (P2) — **Implemented**

Redux cache is keyed by exact query string. Badge, summary, and table each produce different strings, so the same data is fetched multiple times.

**Fix implemented (2026-06):**

- Redux `counts` cache updated by any list response. `useRosCount` hook lets badge/summary reuse table's count without separate API calls.

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

### I-1. Binary slimming (P2) — **Partially Implemented**

Release build uses `-ldflags="-s -w"` to strip debug symbols. **Remaining (deferred):** AWS SDK v1 removal requires upstream middleware change; revisit when `platform-go-middlewares` drops CloudWatch v1 dependency.

---

## Complete Priority Matrix (All 9 Themes)

### P0 -- Do First (highest ROI)


| ID   | Theme     | Change                                                                                  | Est. Impact                        | Status      |
| ---- | --------- | --------------------------------------------------------------------------------------- | ---------------------------------- | ----------- |
| H-1  | UI        | Badge/summary: pass `limit=1`                                                           | Eliminates 99% of wasted list work | Implemented |
| H-2  | UI        | Remove projects table mock override                                                     | Eliminates wasted API calls        | Implemented |
| D-1  | DB Config | Add PostgreSQL tuning to Helm chart (on-prem)                                           | Production correctness             | Implemented |
| D-2  | DB Config | Fix connection budget (expose ROS_DB_MAX_CONNS, remove dead DB_POOL_SIZE) -- both modes | Prevents pool exhaustion           | Implemented |
| P0-1 | Math      | Decay weight lookup table or fixed-point                                                | Eliminates math.Exp from hot path  | Implemented |
| P0-2 | Math      | Fuse CPU + memory weighted passes                                                       | ~40-50% recommend CPU              | Implemented |
| P0-3 | DB        | Defer org metadata refresh to end of reconcile                                          | 50-90% write time for large orgs   | Implemented |
| P0-4 | DB        | Migrate list API pagination to org_container_keys                                       | ~1000x on page query               | Implemented |
| A-1  | API       | Slim list DTO (skip BuildDetailResponse)                                                | 10-30ms CPU per page               | Implemented |
| A-2  | API       | Notification deduplication in JSON (ADR-0293)                                           | 30-50% JSON payload size           | Implemented |
| B-1  | Ingest    | Slim digest group storage                                                               | 5-10x less peak RAM                | Implemented |
| C-1  | Ops       | Fix readiness probe to use /readyz; liveness on /healthz                                | Correctness                        | Resolved    |


### P1 -- High Priority


| ID   | Theme     | Change                                                                           | Status      |
| ---- | --------- | -------------------------------------------------------------------------------- | ----------- |
| H-3  | UI        | Single fetch on OCP breakdown                                                    | Implemented |
| H-4  | API       | List field projection (term/engine/fields)                                       | Implemented |
| H-5  | UI        | Adopt cursor pagination                                                          | Implemented |
| P1-1 | Math      | Integer savings in micro-cents                                                   | Implemented |
| P1-2 | Math      | Integer adaptive margin                                                          | Implemented |
| P1-3 | Math      | Zero-copy window slicing                                                         | Implemented |
| P1-4 | DB        | Fix rh_accounts join anti-pattern                                                | Implemented |
| A-3  | API       | Typed `Collection[T]` (eliminate `[]interface{}` boxing)                       | Implemented |
| A-4  | API       | Parse x-rh-identity once (identity middleware)                                   | Implemented |
| A-6  | API       | Cache-Control on notification catalog                                            | Implemented |
| D-3  | DB Config | Per-table autovacuum tuning (migration for both modes; chart tuning for on-prem) | Implemented |
| D-4  | DB Config | Fix node_recommendations retention (both modes -- app bug)                       | Implemented |
| D-5  | DB Config | Add namespace/PVC retention (both modes -- app bug)                              | Implemented |
| B-2  | Ingest    | Stream namespace CSV                                                             | Implemented |
| B-4  | Ingest    | Remove per-row Prometheus gauge                                                  | Implemented |
| C-2  | Ops       | VM CSV logs: Debug + counter                                                     | Implemented |
| C-3  | Ops       | Audit high-cardinality Prometheus labels                                         | Implemented |
| C-4  | Ops       | Drain in-flight Kafka handlers on shutdown                                       | Implemented |
| C-7  | Ops       | `/healthz` deep liveness endpoint                                                | Implemented |
| C-8  | Ops       | Grafana dashboard modernization                                                  | Implemented |
| C-9  | Ops       | Dashboard static validation tests                                                | Implemented |


### P2 -- Medium Priority


| ID    | Theme     | Change                                       | Status                |
| ----- | --------- | -------------------------------------------- | --------------------- |
| E-2   | Lifecycle | Separate sample vs digest retention          | Implemented           |
| G-1   | Scale     | Increase Kafka partitions (3 -> 12)          | Implemented           |
| G-2   | Scale     | Add HPA for API pods                         | Implemented           |
| P2-1  | Math      | GPU BP config (store thresholds as int32 BP) | Implemented           |
| P2-2  | Math      | Node utilization as basis points             | Implemented           |
| P2-3  | DB        | Batch PVC/GPU writes via pgx.Batch           | Implemented           |
| P2-4  | Ingest    | Decouple VM recommend from ingest            | Implemented           |
| P2-5  | Math      | Replace idle P95 sort with max-of-daily-P95  | Implemented           |
| B-3   | Ingest    | String interning                             | Deferred              |
| B-5   | Ingest    | Narrow single-tx threshold                   | Implemented           |
| B-6   | Ingest    | GOMEMLIMIT in Helm (PGO deferred)            | Partially Implemented |
| C-6   | Ops       | Add make test-short                          | Implemented           |
| I-1   | Deps      | Binary slimming (release ldflags, SDK audit) | Partially Implemented |


### Strategic


| ID  | Theme         | Change                                                    | Status      |
| --- | ------------- | --------------------------------------------------------- | ----------- |
| S1  | Algo          | Unified windowed digest recommender framework             | Deferred    |
| S2  | Algo          | Parallel container recommend by partition                 | Deferred    |
| S3  | Algo          | Namespace recs from container rollups                     | Deferred    |
| S4  | API           | Slim list contract with UI team                           | Implemented |
| F-1 | Observability | Add per-phase pipeline histograms (OpenTelemetry deferred) | Implemented |
| G-3 | Scale         | Distributed debouncer (DB/Redis) for multi-pod processors | Deferred    |


---

## Remaining Items — ROI Analysis (June 2026)

Most performance audit items are now implemented. The tables below record completed work and the **seven remaining deferred items** (S1–S3, G-3, B-3, B-6 PGO, A-5, I-1 partial) with documented revisit triggers (see [Deferred Items — Revisit Triggers](#deferred-items--revisit-triggers)). Prioritized by **impact × feasibility / risk** for when each trigger fires.

### Tier 1 — Completed

All Tier 1 items have been implemented: D-1 (PostgreSQL tuning), G-1 (Kafka partitions), H-3 (OCP breakdown dedup), E-2 (digest-based plots + sample retention), B-2 (namespace CSV streaming).

### Tier 2 — Completed

| Rank | ID      | Effort   | Impact                         | Risk    | Status | Rationale |
| ---- | ------- | -------- | ------------------------------ | ------- | ------ | --------- |
| 6    | ~~**H-4**~~ | ~~3–5 days~~ | ~~**Med–High**~~ | — | **Implemented** | UI passes explicit term/engine; backend skips count-only enrichment; namespace list supports term filter. |
| 7    | ~~**A-2**~~ | ~~2–3 days~~ | ~~**Med**~~ | — | **Implemented** | Engine-only notifications + list `notification_codes` (ADR-0293). |
| 8    | ~~**G-2**~~ | ~~1–2 days~~ | ~~**Med**~~ | — | **Implemented** | CPU-based HPA for ROS API pods in Helm chart. |
| 9    | ~~**H-5**~~ | ~~2–3 days~~ | ~~**Med**~~ | — | **Implemented** | UI prefers keyset cursor for forward pagination; offset fallback for backward jumps. |
| 10   | ~~**F-1**~~ | ~~3–5 days~~ | ~~**Med**~~ | — | **Implemented** | `rosocp_pipeline_phase_duration_seconds` histogram for 7 phases + total pipeline histogram. |
| 11   | ~~**B-5**~~ | ~~1 day~~ | ~~**Med**~~ | — | **Implemented** | Row threshold 25k + group threshold 5k guard on single-tx ingest fast path. |


### Tier 3 — Deferred (low ROI or profiling-gated)


| Rank | ID            | Effort   | Impact                          | Risk | Status | Rationale                                                                                                                                                     |
| ---- | ------------- | -------- | ------------------------------- | ---- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 12   | ~~**H-6**~~   | ~~2–3 days~~ | ~~**Med**~~ | — | **Implemented** | Redux counts cache + `useRosCount` hook share count across badge/summary/table. |
| 13   | **G-3**       | 5–8 days | **Med** (multi-pod correctness) | High | **Deferred** | Only needed when running multiple processor pods concurrently. Revisit when scaling to multiple processor pods.                                                |
| 14   | **B-3**       | 3–5 days | **Low–Med** (ingest GC)         | Med  | **Deferred** | Diminishing returns after B-1. Revisit only if memory profiling shows string duplication as a significant contributor.                                          |
| 15   | ~~**A-3**~~   | ~~2–3 days~~ | ~~**Low**~~ | — | **Implemented** | Generic `Collection[T any]` replaces `[]interface{}` boxing in list responses. |
| 16   | **A-5**       | N/A      | **Low** (legacy path)           | Low  | **Deferred** | Legacy Kruize path is deprecated. Revisit only if the legacy path is retained long-term.                                                                       |
| 17   | **I-1**       | 3–5 days | **Low** (startup/deploy)        | Low  | **Partially Implemented** | Release ldflags done. AWS SDK v1 removal requires upstream middleware change; revisit when `platform-go-middlewares` drops CloudWatch v1 dependency.          |
| 18   | **B-6 (PGO)** | 5+ days  | **Low–Med** (CPU)               | Med  | **Deferred** | Requires production CPU profiles and CI pipeline changes. GOMEMLIMIT already done. Revisit when CI supports PGO build integration.                           |


### Tier 4 — Strategic (deferred; coordinate with product)


| Rank | ID     | Effort    | Impact                         | Risk           | Status | Rationale                                                                                                              |
| ---- | ------ | --------- | ------------------------------ | -------------- | ------ | ---------------------------------------------------------------------------------------------------------------------- |
| 19   | ~~**S4**~~ | ~~1–2 weeks~~ | ~~**High**~~ | — | **Implemented** | Slim list contract (ADR-0294); namespace list slim DTO opt-in via term/engine params. |
| 20   | **S1** | 1–2 weeks | **Med** (code maintainability) | High           | **Deferred** | Pure maintainability refactor with high regression risk. Touches all five recommendation subsystems (container, namespace, PVC, node, VM) simultaneously. No runtime performance benefit. The current duplication acts as a safety boundary — a bug in one recommender can't affect others. Revisit when adding a 6th recommendation type. |
| 21   | **S2** | 1–2 weeks | **High** (CPU at scale)        | High           | **Deferred** | Intra-cluster parallelism is unnecessary because existing inter-cluster parallelism (Kafka partitions + multi-pod workers via G-1 and G-2) already distributes work across cores. On-prem: clusters too small to benefit. SaaS: cores already busy processing other clusters concurrently. S2 would compete for cores already doing useful work. Revisit when F-1 histograms show recommendation phase > 30s per cluster. |
| 22   | **S3** | 1–2 weeks | **High** (compute savings)     | High (product) | **Deferred** | Deferred to preserve recommendation accuracy. Container rollup P95 values systematically overestimate namespace-level needs by 15–30% for mixed workloads (up to 100% for anti-correlated workloads) because P95(A+B) ≠ P95(A) + P95(B). Namespace-native Prometheus metrics capture actual aggregate usage including timing offsets between container spikes, which is the correct basis for ResourceQuota sizing. The rollup approach would be acceptable for rough cost estimation but not for accurate sizing recommendations. Revisit when product defines the rollup specification. |


### Sprint Status

**Completed:** D-1 (PostgreSQL tuning), F-1 (pipeline histograms), A-2 (notification dedup), A-3 (typed `Collection[T]`), G-2 (HPA), B-5 (single-tx guard), G-1 (Kafka partitions), H-3 (OCP breakdown dedup), B-2 (namespace CSV streaming), E-2 (digest-based plots + sample retention), S4 (slim list contract), H-4 (list field projection), H-5 (cursor pagination), H-6 (cross-component count cache).

All Critical and High Priority production gaps are now closed. Remaining performance items — strategic items S1–S3 plus profiling-gated ingest/ops optimizations — are explicitly deferred with detailed rationale and documented revisit triggers below.

---

## Deferred Items — Revisit Triggers

| ID | Item | Status | Revisit trigger | Rationale |
| --- | --- | --- | --- | --- |
| **S1** | Unified windowed digest recommender framework | Deferred | Adding a 6th recommendation type (beyond container, namespace, PVC, node, VM) | Pure maintainability refactor with high regression risk. Touches all five recommendation subsystems (container, namespace, PVC, node, VM) simultaneously. No runtime performance benefit. The current duplication acts as a safety boundary — a bug in one recommender can't affect others. |
| **S2** | Parallel container recommend by namespace partition | Deferred | F-1 pipeline phase histograms show recommendation phase exceeding 30s for a single cluster | Intra-cluster parallelism is unnecessary because existing inter-cluster parallelism (Kafka partitions + multi-pod workers via G-1 and G-2) already distributes work across cores. On-prem: clusters too small to benefit. SaaS: cores already busy processing other clusters concurrently. S2 would compete for cores already doing useful work. |
| **S3** | Namespace recs from container rollups | Deferred | Product defines rollup specification for aggregating container-level recommendations into namespace-level ones | Deferred to preserve recommendation accuracy. Container rollup P95 values systematically overestimate namespace-level needs by 15–30% for mixed workloads (up to 100% for anti-correlated workloads) because P95(A+B) ≠ P95(A) + P95(B). Namespace-native Prometheus metrics capture actual aggregate usage including timing offsets between container spikes, which is the correct basis for ResourceQuota sizing. The rollup approach would be acceptable for rough cost estimation but not for accurate sizing recommendations. |
| **G-3** | Distributed debouncer (DB/Redis) for multi-pod processors | Deferred | Scaling to multiple processor pods concurrently | Only needed when running multiple processor pods concurrently. |
| **B-3** | No string interning for repeated keys | Deferred | Memory profiling shows string duplication as a significant contributor | Diminishing returns after B-1. |
| **B-6 (PGO)** | Profile-guided optimization | Deferred | CI pipeline supports PGO build integration | Requires production CPU profiles and CI pipeline changes. GOMEMLIMIT already done. |
| **A-5** | Legacy Kruize path: `map[string]interface{}` | Deferred | Legacy Kruize path is retained long-term | Legacy Kruize path is deprecated. |
| **I-1** (SDK audit) | AWS SDK v1 removal | Partially Implemented | `platform-go-middlewares` drops CloudWatch v1 dependency | Release ldflags (`-s -w`) done. AWS SDK v1 removal requires upstream middleware change. |