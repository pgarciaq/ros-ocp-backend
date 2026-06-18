# Performance Reviews

Systematic performance audits of the ros-ocp-backend native recommendation engine,
covering algorithm hot paths, database query plans, memory allocation, API latency,
and horizontal scaling characteristics.

## Methodology

Each audit examines the codebase across multiple dimensions:

- **Algorithm / Math** — hot-path CPU analysis (decay loops, percentile computation, margin application)
- **API Serialization** — response assembly, payload size, middleware overhead
- **Ingestion / Memory** — streaming vs batched processing, allocation patterns, GC pressure
- **PostgreSQL Tuning** — query plans (`EXPLAIN ANALYZE`), indexing, connection pooling, statement timeouts
- **Observability** — metric cardinality, label safety, histogram buckets
- **Data Lifecycle** — retention, autovacuum, partition management
- **Horizontal Scaling** — what scales linearly vs what hits cliffs

Findings are classified as P0 (critical), P1 (high), P2 (medium), or Strategic (deferred).

## Audit Cycles

| Audit | Date | Focus | Findings | Status |
|-------|------|-------|----------|--------|
| Native engine v1 | June 2026 | Full 9-dimension review | ~50 across P0/P1/P2/Strategic | All P0–P2 implemented; Strategic deferred |
| Native engine v2 | June 2026 | Phase 13 regression check + new findings | 7 new (3 P1, 4 P2) | P1 all implemented; P2 open |
| Scalability analysis | June 2026 | Connection budget, Kafka parallelism, caching, SLIs | 10 risks identified | Documented with mitigations |
| UXSNO benchmark | June 2026 | Real-world constrained hardware (200m–1 core) | Baseline established | Published |

## Headline Results

The native Go engine replaced the legacy Kruize Java service:

| Metric | Legacy (Kruize) | Native Engine | Improvement |
|--------|-----------------|---------------|-------------|
| Ingestion throughput | 8 containers/sec | 15,000 containers/sec | 1,900x |
| Recommendation throughput | — | 60,000 containers/sec | — |
| Memory (876 containers) | ~2 GB (JVM) | 70 MB | 28x less |
| Storage (50K containers x 91 days) | 5.7 TB | 6 GB | 950x less |
| List API p95 (200 containers) | 3–8s | <200ms | 15–40x |

## Top Issues Found and Fixed

### P0 — Critical (all implemented)

| # | Finding | Impact | Resolution |
|---|---------|--------|------------|
| P0-1 | **`math.Exp` in decay weighting** — called per digest row x term x engine (~28k–60k calls per cycle) | 40–50% of recommend-phase CPU | Precomputed lookup table indexed by integer hour/half-life; `sync.Map` lazy init |
| P0-2 | **Duplicate CPU + memory passes** — separate `RecommendCPU` and `RecommendMemory` each recomputed decay weights | 12 passes per container instead of 6 | Fused into single `RecommendCPUAndMemory` with 10 extractors per call |
| P0-3 | **Org metadata refresh every batch** — `DISTINCT ON` scan of entire org after each 500-container write batch | 20 full-org scans per 10k-container reconcile | Deferred to single end-of-reconcile refresh |
| P0-4 | **List API paginates via `DISTINCT ON`** — page selection scanned all `recommendation_sets` | ~1.3s at 200k containers | Keyset pagination via `org_container_keys` index table (~0.3ms) |

### P1 — High (all implemented)

| # | Finding | Impact | Resolution |
|---|---------|--------|------------|
| P1-1 | **Savings entirely float64** — per-row float multiply chains with rounding error accumulation | Code duplication + precision loss | Integer micro-cents (`MicroCentsPerDollar = 100_000_000`); rate conversion once at boundary |
| P1-2 | **Adaptive margin float detour** — float CV computation then scale back to integer | Unnecessary float division per rec | Pure integer `ComputeAdaptiveMarginScaledDirect` |
| P1-3 | **`filterByWindow` allocates per term** — 4 slice copies per container (3 terms + idle) | 15–25% allocation/GC overhead | Zero-copy `windowBounds` returning `(lo, hi)` index ranges |
| P1-4 | **`rh_accounts` join anti-pattern** — org filtering via FK join instead of direct `WHERE org_id` | 10–5000x slower per EXPLAIN audit | Direct `org_id` filter on all affected queries |

### P2 — Medium (all implemented)

| # | Finding | Impact | Resolution |
|---|---------|--------|------------|
| P2-1 | **GPU threshold float conversion per classify** — 6 float-to-BP conversions per GPU container | Repeated work on GPU-heavy clusters | `normalizeGPUThresholds` precomputes int32 basis points at load |
| P2-2 | **Node utilization as float ratios** — `float64(usage) / float64(alloc)` at classification time | Float division in hot path | `UtilizationBasisPoints` integer function; thresholds stored as BP |
| P2-3 | **PVC and GPU writes not batched** — individual `INSERT` per recommendation | Extra DB round-trips | `pgx.Batch` for PVC and GPU writes |
| P2-4 | **VM recommendations block ingest** — ran inline after VM digest upsert | Ingest latency spike | Deferred to post-manifest phase |
| P2-5 | **Idle classification sorts unnecessarily** — `percentile95Int64` sorted ~15 daily values | 2 sorts per container | Replaced with `maxDailyP95` (max of per-day P95 values) |

### Phase 13 New Findings (P1, all implemented)

| # | Finding | Impact | Resolution |
|---|---------|--------|------------|
| DB-N1 | **Savings recalculation per-row UPDATEs** — 3,000 UPDATE statements per cluster | Connection exhaustion at scale | Batched `pgx.Batch` with chunk-500 writes |
| API-N1 | **GPU list enrichment scans full cluster** — loaded all GPU digests instead of page-scoped | O(cluster) per page request | Page-scoped `unnest` filter on container IDs |
| DB-N2 | **Tag sync full-org reset** — DELETE all + per-namespace INSERT loop | Write amplification on every sync | Single `unnest`-based UPDATE statement |

### Still Open (P2, deferred with revisit triggers)

| # | Finding | Revisit When |
|---|---------|--------------|
| DB-N3 | Namespace list still uses `DISTINCT ON` (no `org_namespace_keys`) | Namespace cardinality exceeds 5,000 per org |
| ALG-N1 | PVC growth slope still calls `math.Exp` | PVC plugin processes >1,000 PVCs per cluster |
| ALG-N2 | VM recommender remains float64-heavy | VM plugin graduates from feature gate |
| OBS-N1 | GPU unrecognized-model counter uses raw model label | More than ~50 distinct model names observed |

### Strategic (explicitly deferred)

| # | Finding | Rationale for deferral |
|---|---------|----------------------|
| S1 | Unified windowed recommender (shared decay across all plugins) | Current per-plugin engines are correct and maintainable; unify only if adding 3+ new plugins |
| S2 | Parallel container recommendation (goroutine per term x engine) | Current fused single-pass is faster than goroutine overhead at <10k containers per batch |
| S3 | Namespace recs from container rollups (skip namespace CSV) | Namespace CSV provides quota data not available from containers; rollup would lose signal |

## Basis Points Migration

A key architectural pattern: replacing `float64` arithmetic with integer basis points
(1 BP = 0.01%) throughout the engine. Current status:

| Area | Status |
|------|--------|
| Digest data (CPU/mem) | int64 millicores/KiB |
| GPU utilization | int32 basis points |
| Quota/CRQ headroom | int BP |
| VM power-off idle ratio | int32 BP |
| Margin application | `MarginScale` + `ApplyScaledMargin` |
| Margin computation | int BP via `ComputeAdaptiveMarginScaledDirect` |
| GPU thresholds | int32 BP (precomputed at load) |
| Node utilization | int32 BP via `UtilizationBasisPoints` |
| Savings rates | int64 micro-cents |
| PVC usage ratio | **float64** (could be BP) |
| VM sizing margins | **float64** (mirror container pattern pending) |

## Benchmark Results (UXSNO)

Real-world benchmark on constrained SNO hardware (Dell R640, 200m–1 core CPU,
512Mi–1Gi memory):

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

## Internal Documentation

Full audit documents with code locations, EXPLAIN ANALYZE output, and ADR references
are maintained in `docs/performance/` (not published externally):

- `docs/performance/native-engine-audit-2026-06.md` — v1 full audit (~50 findings)
- `docs/performance/native-engine-audit-v2-2026-06.md` — v2 regression check + new findings
- `docs/audits/performance-scalability-analysis.md` — architectural deep-dive (connections, Kafka, caching, SLIs)

## Related

- [Performance and Scalability](performance-and-scalability.md) — operator-facing headline numbers
- [UXSNO Benchmark Report](benchmark-report.md) — detailed constrained-hardware results
- [Performance Engineering Guide](performance-engineering-guide.md) — tuning and troubleshooting
- [Query Performance](../query-performance.md) — EXPLAIN ANALYZE audit guide
