# Performance Reviews

!!! note "Historical"
    Frozen documentation — not maintained as current product docs. Grouped under
    [Historical](../historical/index.md) in the site nav for reference.

Systematic performance audits of the ros-ocp-backend native recommendation engine,
covering algorithm hot paths, database query plans, memory allocation, API latency,
ingestion pipelines, observability overhead, data lifecycle, and horizontal scaling
characteristics.

---

## Methodology

Each audit examines the codebase across seven dimensions using static code analysis,
`EXPLAIN ANALYZE` query plans, memory profiling, and production-like benchmarks:

| Dimension | What's Measured |
|-----------|-----------------|
| **Algorithm / Math** | Hot-path CPU: decay loops, percentile computation, margin application, integer vs float division |
| **API Serialization** | Response assembly, DTO shapes, payload size, middleware cost, pagination efficiency |
| **Ingestion / Memory** | Streaming vs batched parsing, allocation per row, GC pressure, flush thresholds |
| **PostgreSQL Tuning** | Query plans, connection pooling, statement timeouts, indexing strategy, autovacuum |
| **Observability** | Metric cardinality, label safety, histogram buckets, log overhead |
| **Data Lifecycle** | Partition retention, sample vs digest TTL, autovacuum tuning, growth projections |
| **Horizontal Scaling** | What scales linearly, what hits connection/partition/memory cliffs |

Findings are classified by severity:

- **P0** — Critical: blocking production readiness or causing measurable regressions
- **P1** — High: significant performance or correctness impact at scale
- **P2** — Medium: optimization opportunity with moderate ROI
- **Strategic** — Deferred: high-risk refactors with documented revisit triggers

---

## Audit History

| Audit | Date | Focus | Findings | Status |
|-------|------|-------|----------|--------|
| Native engine v1 | June 2026 | Full 9-dimension review | ~50 across P0/P1/P2/Strategic | All P0–P2 implemented; Strategic deferred |
| Native engine v2 | June 2026 | Phase 13 regression check + new findings | 13 new (3 P1, 6 P2, 4 P3) | P1 all implemented; P2 open |
| Scalability analysis | June 2026 | Connection budget, Kafka parallelism, caching, SLIs | 10 risks, 14 recommendations | Documented with mitigations |

---

## Architecture Overview (Performance Lens)

```
OpenShift cluster
    → Ingress (presigned URL in Kafka payload)
        → ros-ocp processor (cmd/start processor)
            → utils.ReadCSVBodyFromUrl (SSRF + 500MiB cap)
            → ingestion.ProcessCSVToDigests / plugin CSV ingestors
                → container_usage_samples (partitioned, optional retention 45d)
                → daily_container_digests (partitioned, 6mo default)
            → services.runManifestRecommendations (after all files complete)
                → engine.RecommendWorkloadsStreaming → WriteContainerRecBatch
                → engine.RefreshOrgMetadata (org_container_keys, org_recommendation_stats)
                → parallel: GPU classify + node recs (errgroup)
                → quota / cluster-quota / PVC / VM / snapshot plugins
        → PostgreSQL commit → kafka.CommitMessage

UI / IQE
    → ros-ocp api (cmd/start api)
        → middleware: Identity → Entitlement → RBAC (cached)
        → model.getNativeRecommendationsFromOrgKeys (default list)
        → enrichment_cache (Masu rates per request)
        → fleetsummary LRU (5m TTL)
```

**Process split (each is a separate Deployment):**

| Command | Role | Hot Resources |
|---------|------|---------------|
| `rosocp start processor` | Kafka consumer + ingest + recommend | CPU, DB writes, network (CSV download) |
| `rosocp start api` | REST + `/metrics` on Prometheus port | DB reads, JSON encode, RBAC HTTP |
| `rosocp start housekeeper` | Partition retention, Sources cleanup | DB DDL/DML bursts |

The native engine is unconditionally active. On-prem deployments
run without Kruize for optimal performance.

---

## Headline Results

The native Go engine replaced the legacy Kruize Java service:

| Metric | Legacy (Kruize) | Native Engine | Improvement |
|--------|-----------------|---------------|-------------|
| Ingestion throughput | 8 containers/sec | 15,000 containers/sec | **1,900x** |
| Recommendation throughput | — | 60,000 containers/sec | — |
| Memory (876 containers) | ~2 GB (JVM) | 70 MB | **28x less** |
| Storage (50K containers × 91 days) | 5.7 TB | 6 GB | **950x less** |
| List API p95 (200 containers) | 3–8s | <200ms | **15–40x** |

### End-to-End Latency Budget (500 containers, 30-day CSV)

| Phase | Est. Wall-Clock | Bound |
|-------|-----------------|-------|
| Kafka receive | <100ms | I/O |
| CSV download | 5–30s | I/O (HTTP/S3) |
| CSV parse + digest + DB writes | 15–45s | CPU + DB |
| Recommendations computed | 0.5–3s | CPU |
| Recommendations written | 1–5s | DB |
| Post-processing (GPU + node) | 2–10s | CPU + DB |
| Org metadata refresh | <1s | DB |
| **Total** | **~30–90s** | |

### API Latency Profile (typical list request)

| Phase | p50 | p95 |
|-------|-----|-----|
| Middleware (identity + RBAC) | <2ms | <150ms (RBAC cache miss) |
| DB query (list via org_container_keys) | <5ms | <10ms |
| Enrichment (BH, currency, GPU) | <5ms | <20ms |
| JSON assembly + gzip | 2–8ms | 10–25ms |
| **Total** | **~15ms** | **~200ms** |

### Constrained-Hardware Benchmark (UXSNO)

Real-world validation on resource-constrained SNO hardware (Dell R640, 200m–1 core CPU, 512Mi–1Gi memory):

| Metric | Result |
|--------|--------|
| Data points ingested | 3.13M across 28 clusters |
| Recommendations generated | 7,715 |
| CSV ingestion rate | ~1,100 rows/sec |
| Recommendation engines | <1 second combined (all 4 engines) |
| End-to-end per manifest | ~76s (includes 30s debounce) |
| 40 manifests total | ~35 minutes |
| Processor RSS peak | 70 MB |
| Expected on production hardware | 3–5x higher throughput |

---

## Hot Path Execution Map (Container — Highest Volume)

```
RecommendWorkloadsStreaming
  per container (stream batch 500)
    ClassifyIdleState .............. max-of-daily-P95 (no sort)
    per term (short/medium/long)
      windowBounds ................. binary search, zero-copy indices
      per engine (cost/performance)
        RecommendCPUAndMemory ...... single fused decay loop O(W)
          decay weight ............. lookup table (no math.Exp)
        computeVariation x4 ....... float round (could be integer)
        EvaluateNotifications ...... integer comparisons
  ApplySavingsEstimates (batch) .... integer micro-cents per rec
```

**Inner loop per container:** 3 terms × 2 engines × 1 fused CPU+mem pass × ~7–15 digest rows = **42–90 decay weight lookups** (down from 84–180 before optimization).

---

## All Findings by Priority and Area

### P0 — Critical (All Implemented)

#### P0-1: `math.Exp` in Decay Weighting

- **Area:** Algorithm / Math
- **Issue:** For every digest row in every window, the engine called `math.Exp(-ageHours * math.Ln2 / halfLifeHours)` and accumulated float64 weighted sums. On a 500-container cluster with medium+long terms: ~28k–60k `math.Exp` calls per recommendation cycle. This was the densest float hot path.
- **Resolution:** Precomputed lookup table indexed by quantized integer age and half-life hours. Tables built lazily via `sync.Map` (typically 2–3 tables per invocation, microseconds total). Non-integer half-lives still fall back to `math.Exp`. Accuracy trade-off: ~0.2% weight error, acceptable per ADR documentation.
- **Impact:** Eliminates `math.Exp` from the hot path for standard integer half-lives.

#### P0-2: Duplicate CPU + Memory Weighted Passes

- **Area:** Algorithm / Math
- **Issue:** `RecommendCPU` and `RecommendMemory` each called `MultiWeightedPercentileWithExtras` separately with their own extractors, recomputing decay weights for the same rows. That was **12 passes** per container (3 terms × 2 engines × 2 resources) when it could be **6** (fused CPU+memory in one pass with 10 extractors).
- **Resolution:** Merged into single `RecommendCPUAndMemory` which calls the multi-percentile function once with all 10 extractors per term × engine.
- **Impact:** ~40–50% reduction in recommend-phase CPU per container.

#### P0-3: Org-wide Metadata Refresh on Every Write Batch

- **Area:** Database
- **Issue:** Every streaming batch (500 containers) triggered `RefreshOrgContainerKeys` + `RefreshOrgRecommendationStats`, each doing a full `DISTINCT ON` scan of `recommendation_sets` for the entire org. For a 10k-container org: ~20 full-org scans per reconciliation cycle.
- **Resolution:** Removed per-batch refresh. Added single `RefreshOrgMetadata` call once at end of reconcile.
- **Impact:** 50–90% reduction in recommendation write time for 10k+ container orgs.

#### P0-4: List API Paginates via `DISTINCT ON`

- **Area:** Database / API
- **Issue:** Despite `org_container_keys` existing for fast counts, page selection still ran `DISTINCT ON` over `recommendation_sets`. Internally measured at ~1.3s vs ~0.3ms for key-table pagination at 200k containers.
- **Resolution:** Page selection routed through `org_container_keys` with indexed keyset seek; `recommendation_sets` joined only for selected page rows.
- **Impact:** ~1000× improvement on pagination subquery for large tenants.

#### A-1: Double Assembly Per Container in List API

- **Area:** API Serialization
- **Issue:** Each page row was assembled twice: first into `NativeContainerResult` with full Kruize-shaped nested JSON, then rebuilt in `BuildDetailResponse`. This cost ~10–30ms CPU per 100-item page on top of DB/enrichment time.
- **Resolution:** Introduced slim list DTO via `BuildListResponse` (table columns + codes only). List APIs skip full detail assembly; detail-by-ID still uses complete response.
- **Impact:** Significant CPU reduction per list page; smaller JSON payloads.

#### A-2: Notification Triplication in JSON

- **Area:** API Serialization
- **Issue:** The same notification codes were copied to three JSON levels (`recommendations.notifications`, per-term, per-engine). This added several KB per container redundantly.
- **Resolution:** Detail emits notifications at engine level only. List uses integer `notification_codes` array; UI resolves via catalog endpoint.
- **Impact:** 30–50% JSON payload size reduction.

#### B-1: Digest Groups Retain Full MetricRow Structs

- **Area:** Ingestion / Memory
- **Issue:** Between flushes, grouped data stored full `MetricRow` (~456 bytes + heap strings) per sample. For 1000 container-days × 96 samples/day = ~96,000 copies = **50–120 MB** peak.
- **Resolution:** Replaced with slim `metricSample` struct (6× int64 + time.Time = ~72 bytes). Convert from `MetricRow` once at append time.
- **Impact:** ~5–10× RAM reduction during ingest grouping.

#### C-1: Readiness Probe Points at Static /status

- **Area:** Observability / Operations
- **Issue:** Helm chart wired readiness to `/status` (static JSON, no dependency checks) instead of `/readyz` (which pings the database). Pods marked ready while DB was down.
- **Resolution:** Readiness probe pointed at `/readyz`; liveness on new `/healthz` deep endpoint.
- **Impact:** Production correctness — prevents routing traffic to pods without DB connectivity.

#### D-1: No PostgreSQL Tuning Exposed in Helm Chart

- **Area:** PostgreSQL / Operations (On-Prem)
- **Issue:** Bundled DB was stock Red Hat PostgreSQL with 512Mi RAM and 30Gi shared disk. No `postgresql.conf` customization. At 10k containers, `container_usage_samples` alone can reach 86M rows in 90 days.
- **Resolution:** Helm chart exposes `database.postgresqlConfiguration` with curated knobs (shared_buffers, work_mem, effective_cache_size, max_connections, maintenance_work_mem, autovacuum settings). Database tuning guide with sizing profiles (Demo/Small/Medium/Large) published.
- **Impact:** Enables production-grade on-prem deployments.

#### D-2: Connection Budget Exceeds max_connections

- **Area:** PostgreSQL / Operations
- **Issue:** With default pool sizes across ~6 ROS pods + Koku + RBAC, total connections could reach 85–150 against PostgreSQL's default `max_connections=100`.
- **Resolution:** `ROS_DB_MAX_CONNS` default lowered to 5 per process. Rule of thumb documented: `ROS_DB_MAX_CONNS × replicas ≤ 70% of PG max_connections`.
- **Impact:** Prevents pool exhaustion on shared on-prem instances.

#### H-1: Badge/Summary Fetch 100-Row List for a Count

- **Area:** Frontend / API Contract
- **Issue:** UI badge and summary components called the list API with `limit=100` but only read `meta.count`. Each call materialized up to 100 full recommendation objects with all enrichment.
- **Resolution:** UI now passes `limit=1` (count still available via `meta.count`).
- **Impact:** Eliminates 99% of wasted list work on badge/summary renders.

#### H-2: Projects Table Fetches Then Overwrites with Mocks

- **Area:** Frontend / API Contract
- **Issue:** The projects table made a full list API call then replaced the response with hardcoded mock data. Wasted network + backend work on every mount.
- **Resolution:** Removed mock override; table renders live API response.
- **Impact:** Eliminates completely wasted API calls.

---

### P1 — High Priority (All Implemented)

#### P1-1: Savings Estimation Entirely float64

- **Area:** Algorithm / Math
- **Issue:** Per recommendation row: millicore → cores (`/1000.0`), KiB → GiB, multiply by float64 rates, multiply by 730.0 hours/month, then `math.Round(total*100)/100`. Seven modules used this pattern.
- **Resolution:** Integer micro-cents (`MicroCentsPerDollar = 100_000_000`). Rate conversion happens once at boundary; cents returned at API boundary.
- **Impact:** Eliminates float accumulation errors; reduces code duplication across seven modules.

#### P1-2: Adaptive Margin Float CV Detour

- **Area:** Algorithm / Math
- **Issue:** `ComputeAdaptiveMargin` computed `cv = float64(p95-p50) / float64(mean)` then scaled back to `MarginScale`. The integer equivalent is pure integer division.
- **Resolution:** Replaced float CV path with `ComputeAdaptiveMarginScaledDirect` using scaled integer division.
- **Impact:** One float division per recommendation eliminated; aligns with existing integer margin pattern.

#### P1-3: `filterByWindow` Allocates Per Term Per Container

- **Area:** Algorithm / Memory
- **Issue:** Each term created a new `[]DigestRow` copy. With 3 terms + idle window = 4 allocations per container.
- **Resolution:** `windowBounds` returns `(lo, hi)` index ranges into the shared backing slice. Windows processed sequentially over the same slice.
- **Impact:** 15–25% less allocation/GC during recommend on large clusters.

#### P1-4: `rh_accounts` Join Anti-Pattern

- **Area:** Database
- **Issue:** Tables with `org_id` columns joined `rh_accounts` for org filtering instead of filtering directly on `org_id`. Found in recommendation quality queries, detail queries, and cluster last-reported queries.
- **Resolution:** All affected queries use direct `WHERE org_id = ?` filtering.
- **Impact:** 10–5000× improvement per EXPLAIN audit on affected query plans.

#### A-3: `Collection.Data []interface{}` Forces Boxing

- **Area:** API Serialization
- **Issue:** Every list item was heap-allocated and interface-boxed. Blocked compile-time JSON optimizations.
- **Resolution:** Replaced with generic `Collection[T any]` and `Data []T`, eliminating per-item boxing.
- **Impact:** Reduced allocation pressure in list response serialization.

#### A-4: Double Identity Parsing Per Request

- **Area:** API Middleware
- **Issue:** The `x-rh-identity` header was base64-decoded and JSON-unmarshaled twice (identity middleware + entitlement middleware). ~0.2–0.8ms per request.
- **Resolution:** Identity middleware parses once and stores parsed identity on context. Entitlement middleware reads from context.
- **Impact:** Eliminates redundant decode/unmarshal on every request.

#### B-2: Namespace CSV Not Streaming

- **Area:** Ingestion / Memory
- **Issue:** Namespace CSV was fully materialized into a slice before grouping, unlike the container path which streams.
- **Resolution:** Added streaming namespace parser mirroring the container streaming path. Digest groups flush at configurable batch size (default 5000). Raw usage sample tables removed in #258.
- **Impact:** Prevents memory spikes on large namespace CSVs.

#### B-4: Per-Row Prometheus Gauge Update During CSV Ingest

- **Area:** Ingestion / Observability
- **Issue:** `SetIngestGroupsInMemory(groupCount)` was called on every CSV row in the streaming loop. On a 500k-row manifest: 500k Prometheus gauge updates with mutex + label lookup.
- **Resolution:** Gauge updated only on flush boundaries.
- **Impact:** Eliminates 500k unnecessary mutex acquisitions per large manifest.

#### C-2: VM CSV Warn-Log Storms

- **Area:** Observability
- **Issue:** Every bad VM CSV row logged at Warn level. A noisy file could generate thousands of log lines per manifest.
- **Resolution:** Per-row warn downgraded to Debug. New counter metric for skipped rows. Summary warn at end when skips > 0.
- **Impact:** Prevents log volume explosion on malformed VM data.

#### C-3: High-Cardinality Prometheus Labels

- **Area:** Observability
- **Issue:** Several metrics used `org_id` and `cluster_uuid` as labels, creating unbounded cardinality that could blow up Prometheus memory.
- **Resolution:** Removed tenant labels from fleet metrics; bounded labels only. Gauges converted to unlabeled histograms. Per-org/cluster context emitted via structured logs.
- **Impact:** Prevents Prometheus memory blow-up at fleet scale.

#### C-4: Processor Shutdown Doesn't Drain In-Flight Work

- **Area:** Operations / Kafka
- **Issue:** On SIGTERM, running handlers (mid-CSV or mid-recommendation) were not awaited. Risk: partial processing, duplicate work on retry.
- **Resolution:** `sync.WaitGroup` for in-flight Kafka handlers. Configurable shutdown timeout (default 30s). `ProcessReport` checks `ctx.Done()` between phases.
- **Impact:** Clean shutdowns without data loss or duplicate processing.

#### C-7: No Deep Liveness Endpoint

- **Area:** Operations
- **Issue:** Only `/status` (static 200) existed. No goroutine leak, GC pause, or deadlock detection.
- **Resolution:** New `/healthz` endpoint checks goroutine count (configurable max), last GC pause, and scheduler responsiveness (deadlock canary).
- **Impact:** Pods with goroutine leaks or GC stalls restarted automatically.

#### C-8: Grafana Dashboard Stale for Native Engine

- **Area:** Observability
- **Issue:** Dashboard retained SaaS-specific variables, missing native-engine panels, and null-value mappings that rendered as "null".
- **Resolution:** Removed SaaS-only template variables; added on-prem pod label patterns. Added engine performance, cache, ingest, and lifecycle panels. Fixed null→N/A mappings.
- **Impact:** Actionable monitoring for both deployment modes.

#### C-9: No Static Validation for Grafana Dashboard

- **Area:** Observability / Testing
- **Issue:** Dashboard JSON was hand-edited with no CI guard against broken PromQL or stale metric names.
- **Resolution:** Static validation tests parse the ConfigMap, verify all PromQL references exist in the metrics package, check for removed high-cardinality labels.
- **Impact:** Prevents dashboard rot from metric renames or removals.

#### D-3: UPDATE-Heavy Tables Need Autovacuum Tuning

- **Area:** PostgreSQL
- **Issue:** `recommendation_sets` and `container_usage_samples` get frequent UPSERTs creating dead tuples. No `fillfactor` or per-table autovacuum tuning existed.
- **Resolution:** Migration sets `autovacuum_vacuum_scale_factor=0.05`, `autovacuum_analyze_scale_factor=0.02`, `fillfactor=85` on hot tables. New partitions inherit reloptions.
- **Impact:** Reduces table bloat and vacuum pauses on high-churn tables.

#### D-4: `node_recommendations` Retention Gap

- **Area:** Data Lifecycle
- **Issue:** Listed in retention plugin but was not partitioned — sweep was a no-op. Rows only removed on Sources destroy.
- **Resolution:** Added to `dateRetainedTables` for age-based cleanup.
- **Impact:** Prevents unbounded growth of historical node recommendation data.

#### D-5: Namespace/PVC Recommendation Sets Lack Retention

- **Area:** Data Lifecycle
- **Issue:** No periodic cleanup; only removed on Sources destroy.
- **Resolution:** Added `namespace_recommendation_sets` and `pvc_recommendation_sets` to age-based retention.
- **Impact:** Prevents unbounded growth for stale namespace and PVC recommendations.

#### H-3: OCP Breakdown Fires Duplicate List Calls

- **Area:** Frontend / API Contract
- **Issue:** The breakdown page rendered both projects and containers tables, each making separate list requests with overlapping filters.
- **Resolution:** Shared fetch logic lifted into a hook; both tables receive data via props.
- **Impact:** Eliminates ~50% of breakdown-page API traffic.

#### H-4: List Responses Include Full Detail Shape

- **Area:** API Contract
- **Issue:** Backend built full detail response per list row (all 3 terms × 2 engines with config, variation, notifications). Table UI only displays one combination.
- **Resolution:** UI passes explicit `term`/`engine` on list calls. Backend skips enrichment for count-only calls.
- **Impact:** Significant reduction in response assembly CPU and payload size.

#### H-5: Offset Pagination When Keyset Cursor Available

- **Area:** Frontend / API Contract
- **Issue:** UI used `offset` while the backend supports keyset cursors. Offset degrades linearly with dataset size.
- **Resolution:** UI prefers keyset cursor (`after=next_cursor`) for forward pagination; offset fallback for backward/arbitrary page jumps.
- **Impact:** Constant-time pagination regardless of dataset size.

#### DB-N1: Savings Recalculation Per-Row UPDATEs

- **Area:** Database (Phase 13)
- **Issue:** After computing savings, each recommendation row was updated with an individual `tx.Exec` UPDATE inside a loop. 500 containers × 3 terms × 2 engines = **3,000 UPDATE statements** per cluster. Org with 20 clusters → 60,000 UPDATEs per cost-model change.
- **Resolution:** Mirror the recommendation write pattern: queue UPDATEs in `pgx.Batch` (chunk 500), single transaction per cluster per rec type.
- **Impact:** 10–50× reduction in savings-recalc wall time for large orgs.

#### API-N1: GPU List Enrichment Scans Full Cluster Digests — Superseded by [#102](https://github.com/redhatinsights/ros-ocp-backend/issues/102)

- **Area:** API (Phase 13)
- **Issue:** For each distinct `cluster_uuid` on the list page, enrichment read **all** `gpu_container_digests` rows for that cluster. 100-item page spanning 5 GPU-heavy clusters → 5 full-cluster digest scans.
- **Original resolution:** Page-scoped `unnest` filter on container IDs, mirroring the business-hours enrichment pattern.
- **Superseded:** [#102](https://github.com/redhatinsights/ros-ocp-backend/issues/102) replaced the per-request MIG enrichment loop entirely with a persisted `gpu_mig_recommendation_sets` table populated during the background engine cycle. The MIG list handler now reads directly from SQL with full keyset pagination, sorting, and filtering — no per-request digest scans at all.
- **Impact:** MIG list endpoint is now O(page_size) regardless of cluster GPU density. The original page-scoped filter was an interim improvement; persistence eliminates the concern entirely.

#### DB-N2: Tag Sync Full-Org Reset + Per-Namespace Loop

- **Area:** Database (Phase 13)
- **Issue:** (1) `UPDATE org_container_keys SET resolved_tags = '{}'` for entire org. (2) Loop: one UPDATE per namespace in payload. Org with 10k containers across 200 namespaces → 1 full-table touch + 200 UPDATE round-trips per sync.
- **Resolution:** Single `unnest`-based UPDATE for all namespaces in payload. Eliminates full-org reset and per-namespace loop.
- **Impact:** 5–20× faster tag sync; reduced lock duration.

---

### P2 — Medium Priority (Implemented)

#### P2-1: GPU Threshold Float Conversion at Every Classify Call

- **Area:** Algorithm / Math
- **Issue:** `GPUThresholds` stored six float64 thresholds, converted to basis points via `ThresholdToBasisPoints` at each classify call.
- **Resolution:** `normalizeGPUThresholds` precomputes all six as int32 basis points at load time. `Classify` uses precomputed BP fields directly.
- **Impact:** Eliminates per-classify float→BP conversion on GPU-heavy clusters.

#### P2-2: Node Utilization as Float Ratios

- **Area:** Algorithm / Math
- **Issue:** `float64(usage) / float64(alloc)` computed float ratios at classification time.
- **Resolution:** `UtilizationBasisPoints` integer function; `NodeRecommendationConfig` stores thresholds as precomputed basis points.
- **Impact:** Removes float division from node classification hot path.

#### P2-3: PVC and GPU Writes Not Batched

- **Area:** Database
- **Issue:** Container/namespace used `pgx.Batch`; PVC and GPU did individual INSERTs.
- **Resolution:** Both now use `pgx.Batch` for batched writes.
- **Impact:** Reduced DB round-trips for PVC and GPU persistence.

#### P2-4: VM Recommendations Block Ingest

- **Area:** Ingestion
- **Issue:** `RunVMRecommendations` ran inline after VM digest upsert, blocking the CSV ingest pipeline.
- **Resolution:** VM recommendations deferred to post-manifest phase (matching container pattern).
- **Impact:** Reduced ingest latency; VM recs no longer block CSV processing.

#### P2-5: Idle Classification Sorts Unnecessarily

- **Area:** Algorithm / Math
- **Issue:** `percentile95Int64` created a copy and sorted for exact P95 across ~15 daily values. Max of daily P95s is cheaper and semantically sufficient for idle detection.
- **Resolution:** Replaced with `maxDailyP95` (max of per-day P95 values).
- **Impact:** Eliminates 2 sorts per container during idle classification.

#### E-2: Separate Sample vs Digest Retention

- **Area:** Data Lifecycle
- **Issue:** Samples and digests shared the same retention window despite samples no longer being needed for the detail API (plots now read digests only).
- **Resolution:** `ROS_SAMPLE_RETENTION_DAYS` (default 45) sweeps samples independently of `ROS_RETENTION_MONTHS` (default 6). Plot response changed from boxplot to percentile-band shape.
- **Impact:** Up to 60% storage reduction for long-running deployments.

#### G-1: Kafka Partitions Fixed at 3

- **Area:** Horizontal Scaling
- **Issue:** Kafka topic had only 3 partitions. Adding processor pods beyond 3 provided no additional parallelism.
- **Resolution:** Partition count configurable via Helm values (default 12).
- **Impact:** Allows linear scaling of ingest throughput up to 12 processor pods.

#### G-2: No HPA for API Pods

- **Area:** Horizontal Scaling
- **Issue:** API pods had no autoscaling configuration.
- **Resolution:** CPU-based HPA available for ROS API pods (opt-in via Helm values).
- **Impact:** Automatic scale-out on API traffic spikes.

#### B-5: Single-Tx Fast Path Holds All Samples in RAM

- **Area:** Ingestion / Memory
- **Issue:** When `rowCount <= 50,000` and no flushes occurred, the single-tx path held all samples in memory.
- **Resolution:** Lowered threshold to 25,000 rows + 5,000 digest groups. Both limits required for single-tx eligibility.
- **Impact:** Prevents memory spikes on large single-file manifests.

#### B-6: GOMEMLIMIT Not Set in Deployment

- **Area:** Memory / Operations
- **Issue:** No `GOMEMLIMIT` set in container, allowing GC to overshoot and risk OOMKill.
- **Resolution:** Helm chart injects `GOMEMLIMIT` (~90% of container memory limit) into all ROS deployments.
- **Impact:** Prevents OOMKill during recommendation spikes by enabling soft memory targeting.

#### C-6: No Fast Local Test Path

- **Area:** Developer Experience
- **Issue:** Full test suite requires Docker and takes ~25 minutes. No documented fast-path.
- **Resolution:** `make test-short` runs unit-only tests in ~15 seconds.
- **Impact:** Faster development iteration.

#### A-6: No Cache Headers on Notification Catalog

- **Area:** API
- **Issue:** `GetNotificationCodes` is static in-memory but had no HTTP caching.
- **Resolution:** Returns `Cache-Control: public, max-age=86400`.
- **Impact:** Eliminates redundant catalog fetches by browsers and CDN.

#### H-6: No Cross-Component Cache Sharing

- **Area:** Frontend / API Contract
- **Issue:** Redux cache keyed by exact query string. Badge, summary, and table each fetched the same data.
- **Resolution:** Redux `counts` cache updated by any list response. Hook enables reuse across components.
- **Impact:** Eliminates duplicate API calls for count data.

---

### P2 — Medium Priority (Open / Deferred)

#### DB-N3: Namespace List Still Paginates via DISTINCT ON

- **Area:** Database
- **Issue:** No `org_namespace_keys` equivalent table exists. Namespace pagination uses `DISTINCT ON` over `namespace_recommendation_sets`, analogous to the pre-P0-4 container path.
- **Revisit When:** Namespace cardinality exceeds 5,000 per org.

#### ALG-N1: PVC Growth Slope Still Uses `math.Exp`

- **Area:** Algorithm / Math
- **Issue:** Weighted least-squares loop calls `math.Exp(-lambda * ageHours)` for each digest point. Container decay migrated to lookup tables; PVC path did not.
- **Revisit When:** PVC plugin processes >1,000 PVCs per cluster.

#### ALG-N2: VM Recommender Remains Float64-Heavy

- **Area:** Algorithm / Math
- **Issue:** VM sizing uses float margins, float hysteresis thresholds, float disk growth slopes. VMs are low cardinality (~10–50 per cluster) so absolute impact is minimal.
- **Revisit When:** VM plugin graduates from feature gate to general availability.

#### OBS-N1: GPU Unrecognized-Model Counter Uses Raw Label

- **Area:** Observability
- **Issue:** Unrecognized GPU model strings become Prometheus label values. Typically <30 distinct values but theoretically unbounded.
- **Revisit When:** More than ~50 distinct model names observed in production.

#### DB-N4: Filtered List COUNT Still Subqueries DISTINCT/Keys

- **Area:** Database
- **Issue:** Any cluster/project/container filter builds a count subquery over keys. Adds 5–50ms on large orgs with ILIKE filters.
- **Revisit When:** Filtered list usage patterns show consistent p95 >100ms.

---

### Strategic (Explicitly Deferred)

| ID | Finding | Rationale for Deferral |
|----|---------|------------------------|
| S1 | Unified windowed recommender framework | Current per-plugin engines are correct and maintainable. Unification touches all 5 subsystems simultaneously with high regression risk and no runtime performance benefit. Revisit when adding a 6th recommendation type. |
| S2 | Parallel container recommend by namespace partition | Inter-cluster parallelism (Kafka partitions + multi-pod) already distributes work across cores. Intra-cluster parallelism would compete for cores doing useful work on other clusters. Revisit when pipeline histograms show recommend phase >30s per cluster. |
| S3 | Namespace recs from container rollups | P95(A+B) ≠ P95(A) + P95(B). Container rollups systematically overestimate namespace needs by 15–30% for mixed workloads (up to 100% for anti-correlated). Namespace-native metrics capture actual aggregate usage. Revisit when product defines a rollup specification. |
| G-3 | Distributed debouncer for multi-pod processors | Debouncer is process-local (`sync.Map`). Only needed when running multiple processor pods against same partitions. Revisit when scaling to multiple processor replicas. |

---

## Basis Points Architecture

A key architectural decision: replacing `float64` arithmetic with integer basis points
(1 BP = 0.01%) throughout the engine to eliminate floating-point accumulation errors
and reduce CPU cost:

| Area | Status |
|------|--------|
| Digest data (CPU/mem) | int64 millicores/KiB |
| GPU utilization | int32 basis points |
| Quota/CRQ headroom | int BP via `applyHeadroom` |
| VM power-off idle ratio | int32 BP |
| Margin application | `MarginScale` + `ApplyScaledMargin` |
| Margin computation | int BP via `ComputeAdaptiveMarginScaledDirect` |
| GPU thresholds | int32 BP (precomputed at load) |
| Node utilization | int32 BP via `UtilizationBasisPoints` |
| Savings rates | int64 micro-cents |
| PVC usage ratio | **float64** (could be BP — low priority) |
| VM sizing margins | **float64** (mirror container pattern pending) |

---

## Scaling Characteristics

### What Scales Linearly

- **Kafka partitions × processor replicas** — consumer group rebalancing distributes work
- **API replicas** for read traffic — stateless; pool per pod
- **PostgreSQL read IOPS** for list queries — proper indexes and `work_mem` tuning

### What Hits Cliffs

| Scale Trigger | First Failure Mode |
|---------------|-------------------|
| 1 partition, high ingest rate | Single-threaded partition processing |
| 5 conn × 20 API pods = 100 PG conns | `too many connections` on shared on-prem DB |
| 20k containers, 3 terms, 2 engines | Recommend phase 60–120s+; Kafka lag |
| Namespace list at 5k namespaces | DISTINCT ON sort spill |
| Cost model change, 30 clusters | Savings recalc minutes-long (now batched) |
| CSV 500 MiB | Download memory if not streamed; timeout at 120s |

### Scaling Ceilings by Fleet Size

| Fleet Size | First Wall | Bottleneck |
|------------|------------|------------|
| 1k | None | — |
| 10k | Ingest throughput | Kafka partition count caps parallel ingest |
| 50k | DB connections | Pool × pods exceeds max_connections (resolved: default 5 per process) |
| 100k | DB write throughput | `recommendation_sets` UPSERT contention |
| 100k+ per org | API aggregation | Fleet/savings summary cache misses |

### Horizontal Scaling Model

- **API pods:** Stateless, scale freely (watch DB pool × replicas against max_connections)
- **Processor pods:** Scale up to Kafka partition count (configurable, default 12)
- **Housekeeper:** Singleton (no horizontal scaling)
- **Scaling ingest:** Increase partitions before adding processor pods

### Storage Growth Projections

| Scale | `container_usage_samples` (90d) | `recommendation_sets` | `recommendation_history` (90d) | Disk Estimate |
|-------|--------------------------------|----------------------|-------------------------------|---------------|
| 1k containers | 8.6M rows | 6k | 540k | ~5–10Gi |
| 10k containers | 86M rows | 60k | 5.4M | ~35–70Gi |
| 100k containers | 864M rows | 600k | 54M | ~350–700Gi |

`container_usage_samples` drives 90%+ of storage. Separate sample retention (45d default)
vs digest retention (6 months) enables significant storage savings.

---

## Risk Register

| ID | Risk | Likelihood | Impact | Mitigation |
|----|------|------------|--------|------------|
| R1 | Shared on-prem PostgreSQL exhausted by Koku + ROS connections | High | Outage | Connection budgeting (`ROS_DB_MAX_CONNS × replicas ≤ 70% max_connections`); PgBouncer; separate ROS DB |
| R2 | Single Kafka partition limits ingest throughput | Medium | Lag growth | Increase partitions before scaling processors (configurable via Helm) |
| R3 | Namespace DISTINCT ON degrades UI namespace tab | Medium | Timeouts | Future `org_namespace_keys` table (P2 deferred) |
| R4 | Large CSV + strict analytics blocks commit on history failure | Low | Lag | `ROS_INGEST_STRICT_ANALYTICS=false` for degraded mode |
| R5 | No API rate limit → DB exhaustion | Medium | API outage | External ingress rate limit; future per-org token bucket middleware |
| R6 | Term config cache unbounded growth | Low | Memory creep | Bounded by tenant count; monitor `rosocp_term_config_cache_size` |
| R7 | SaaS ingress timeout on savings summary | Medium | 504 errors | `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=28000` |
| R8 | Savings recalc during business hours | Medium | DB spike | Coalescing guard (max 3 concurrent); schedule off-peak masu webhooks |
| R9 | Poison messages misclassified as transient | Low | DLQ noise | Monitor DLQ depth; tune transient error classifier |
| R10 | `filter[stale]=true` on large org | Medium | List timeout | Document as admin-only; add index covering stale+sort |

---

## SLI / SLO Recommendations

| SLI | Measurement | Suggested Target (On-Prem Medium) | Source |
|-----|-------------|-----------------------------------|--------|
| Ingest success rate | `rosocp_pipeline_total_duration_seconds{status="success"}` / total | ≥99% | Pipeline metrics |
| Ingest latency (p95) | Pipeline histogram | <10 min per manifest (5k containers) | Phase histograms |
| Recommend latency (p95) | `rosocp_recommendation_duration_seconds{type="container"}` | <120s per cluster | Engine metrics |
| API availability | `/readyz` success | 99.9% | K8s probes |
| API latency (p95) | `rosocp_echo_request_duration_seconds` | List <2s; detail <1s | Echo prometheus |
| DB pool health | `rosocp_db_pool_acquired_conns / rosocp_db_pool_max_conns` | <80% sustained | Pool collector |
| Statement timeout rate | `ros_api_statement_timeout_cancellations_total` | <0.1% of queries | Statement timeout metrics |
| Kafka lag | External broker metric | <1000 messages/processor | Broker JMX / Strimzi |
| Cache effectiveness | `fleet_summary_cache_hits / (hits+misses)` | >60% after warm-up | Fleet summary cache |

**Error budget consumers:** Kafka DLQ depth (`rosocp_kafka_dlq_messages_total`), analytics incomplete flag on API responses.

---

## On-Prem vs SaaS Performance Differences

| Aspect | On-Prem | SaaS |
|--------|---------|------|
| Engine | Native default; Kruize disabled | Native default; Kruize for legacy tenants |
| Config source | Helm env vars | Clowder AppConfig |
| RBAC | Often disabled locally (`RBAC_ENABLE=false`) | Enabled; LRU cache critical (500 entries, 60s TTL) |
| PostgreSQL | Shared with Koku on one instance (512Mi–8Gi) | Dedicated ROS RDS; still connection-limited |
| Ingress timeout | Route/ingress configurable | ~30s gateway → tune heavy API timeout to 28s |
| CSV URLs | MinIO internal URLs on allowlist | S3 presigned URLs |
| Savings / Masu | `KOKU_MASU_URL` to internal masu | Same pattern, higher latency variance |
| Metrics | Prometheus scrape `:5005/metrics` | Clowder Prometheus port |
| `GOMEMLIMIT` | Helm `ros.goMemLimit` (~90% of limit) | Clowder memory limit injection |
| Connection budget | Tight (shared DB, fewer total connections) | More headroom (dedicated RDS instance class) |
| Autovacuum | Chart-injected tuning required | RDS-managed, tunable via parameter groups |
| HA | Single StatefulSet pod | Multi-AZ, read replicas available |

**Key implication:** On-prem deployments are more sensitive to PostgreSQL sizing and
connection budgeting because the database is shared and resource-constrained. SaaS
benefits from managed infrastructure but faces stricter ingress timeout constraints
(~30s gateway budget).

---

## Caching Architecture

| Cache | Max Entries | TTL | Invalidation |
|-------|-------------|-----|--------------|
| RBAC permissions | 500 | 60s | Expiry only |
| Fleet summary | 256 | 300s | `InvalidateOrg` on rec refresh |
| Savings summary | 256 | 300s | `InvalidateOrg` |
| Cost effective rates | 1000 | Per-entry TTL | Prefix delete on org |
| Term config | Unbounded map | 60s | `InvalidateTermCache` on settings PUT |

**Singleflight / coalescing:**

- Threshold recalc guard — per `(org_id, rec_type)` mutex + pending flag
- Savings recalc guard — same pattern
- Worker pool capped at 3 concurrent recalculations

**Request-scoped cache:** Enrichment cache deduplicates Masu `GetEffectiveRates` within one HTTP request (1 call per cluster per page instead of per container).

**Hit ratio monitoring:** Prometheus counters for fleet summary, savings summary, RBAC, and cost cache hits/misses.

---

## Statement Timeout Tiers

| Tier | Default | Used By |
|------|---------|---------|
| API / GORM | 25,000 ms | All pooled connections via `AfterConnect` |
| Heavy API | 45,000 ms | Savings summary, large fleet aggregates |
| Ingest | 120 s | `SET LOCAL` inside ingest transactions only |

SaaS deployments should set `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=28000` to stay within
the ~30s ingress gateway budget.

---

## What Is Genuinely Well-Engineered

The following patterns represent engineering decisions that should be preserved:

- **Unified pgxpool** shared by GORM and raw pgx — single connection pool, no duplication
- **Manual Kafka commit** with transient retry + DLQ — at-least-once without stuck partitions
- **Partition-scoped parallelism** with serialized commits — ordering within partition, concurrency across
- **Statement timeouts split by path** — API 25s, ingest 120s, heavy 45s
- **Bounded LRU caches** with Prometheus metrics — observable, eviction-safe
- **Coalesced async recalc** — max 3 concurrent clusters prevent thundering herd
- **SSRF-hardened CSV download** — host allowlist, private network deny, 500MiB cap
- **Prometheus pipeline phase histograms** without tenant labels — bounded cardinality
- **Integer data plane** — `DigestRow` fields fully int64; no boxing in primary data path
- **Percentiles precomputed at ingest** — amortized cost; recommendation reads are cheap
- **`sync.Pool` for compute buffers** — reduced GC pressure during high-throughput ingest
- **Streaming recommendation with bounded memory** — `streamBatchSize = 500` caps peak allocation
- **Cost data cached per request** — enrichment deduplication eliminates N+1 Masu calls
- **Keyset pagination** with tuple tie-breakers — constant-time page access at any dataset size

---

## Engineering Investment Summary

This performance work spans **three comprehensive audits** examining the system across
**9 dimensions** (Algorithm, API, Ingestion, PostgreSQL, Observability, Data Lifecycle,
Scaling, Frontend Contract, Dependencies).

**By the numbers:**

- **~65 individual findings** identified across all audits
- **~50 implemented** (all P0, all P1, most P2)
- **7 explicitly deferred** with documented revisit triggers
- **0 regressions** verified in follow-up audit
- **12 P0 critical fixes** — each addressing a fundamental scalability bottleneck
- **17 P1 high-priority fixes** — algorithm, database, API, and observability improvements
- **16 P2 medium-priority fixes** — completing the optimization pass

**Quantified improvements from implemented work:**

| Optimization | Measured Improvement |
|-------------|---------------------|
| Decay lookup table (P0-1) | Eliminates ~28k–60k `math.Exp` calls/cycle |
| Fused CPU+memory pass (P0-2) | ~40–50% recommend-phase CPU reduction |
| Deferred metadata refresh (P0-3) | 50–90% recommendation write-time reduction |
| Keyset pagination (P0-4) | ~1000× improvement on page queries |
| Slim ingest storage (B-1) | 5–10× less peak RAM during ingest |
| Integer savings (P1-1) | Eliminates float accumulation errors across 7 modules |
| Zero-copy windows (P1-3) | 15–25% less GC pressure |
| rh_accounts fix (P1-4) | 10–5000× per EXPLAIN on affected queries |
| Batched savings recalc (DB-N1) | 10–50× faster savings updates |
| Persisted MIG table (API-N1, [#102](https://github.com/redhatinsights/ros-ocp-backend/issues/102)) | MIG list O(page_size); no per-request digest scans |

The system is designed to scale **vertically** (bigger processor pod, tuned PostgreSQL)
and **horizontally** (more Kafka partitions + processor replicas) before requiring
architectural rewrites. Connection budgeting, partition-scoped parallelism, and bounded
caching ensure predictable behavior under load.

---

## Related Documentation

- [Performance and Scalability](performance-and-scalability.md) — operator-facing headline numbers
- [UXSNO Benchmark Report](benchmark-report.md) — detailed constrained-hardware results
- [Performance Engineering Guide](performance-engineering-guide.md) — tuning and troubleshooting
- [Query Performance](../query-performance.md) — EXPLAIN ANALYZE audit guide
