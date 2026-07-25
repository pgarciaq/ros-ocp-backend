# Performance Audit Report: ros-ocp-backend

## Date and Scope

**Date:** July 25, 2026
**Branch:** `pgarciaq-rosocp-superpowers-phase16` (38 commits since v5 audit on July 20, 2026)
**Prior audit:** v5 (same file, overwritten — see git history for prior version)
**Scope:** Full 11-dimension audit covering multi-currency savings conversion (#363, #364), calendar-accurate monthly hours (#316), per-PVC VM shared storage detection (#359), unified category classification, numeric sort fix, workload_type validation, adversarial review v11/v12 correctness fixes, and the vm_pvc_digests schema.

**Deployment modes considered:** SaaS (multi-tenant, RDS) and on-prem (single-tenant PostgreSQL, 512Mi–8Gi chart profiles).

**Commits reviewed:** 38 commits since July 20, 2026 (v5 audit).

---

## Prior Audit Status

The v5 audit (July 20, 2026) reported:
- **0 P0 findings**
- **0 P1 findings**
- **5 P2 findings** — 4 implemented (BH-POOL-1, BH-CONFIG-1, PLUGIN-ALLOC-1, BUILD-CGO), 1 open (COMPAT-1)
- **6 P3 findings** — 2 implemented (DOCKERFILE-DEAD, CONC-RO), 4 open (COMPAT-SIZE, KAFKA-FMT, BUILD-PGO, BUILD-GOTA)

**Key changes since v5:**

| Area | Status |
|------|--------|
| Multi-currency savings conversion (#363, #364) | Implemented — LRU-cached exchange rates, integer `ConvertCents`, API-time conversion |
| Calendar-accurate monthly hours (#316) | Implemented — `HoursInMonth(year, month)` replaces fixed 730h constant |
| Per-PVC VM shared storage detection (#359) | Implemented — `vm_pvc_digests` table, CSV parsing, in-memory correlation |
| Unified category classification (container/namespace/node) | Implemented — single `category` column replaces boolean flags |
| VM category column migration (000176) | Implemented — replaces 4 booleans with 1 TEXT column + partial index |
| Numeric sort order fix | Implemented — `::bigint` cast for numeric page sort columns |
| Workload type validation | Implemented — per-request string checks (length, sentinel, whitespace) |
| Adversarial review v12 fixes | Implemented — `io.LimitReader`, structured logging, vmTerm parameterization, `ctx.Err()` checks |
| Adversarial review v11 remaining fixes | Implemented — `ResolveGPUThresholdSettings` nil guard, `pgx.Identifier` for MIG groupCol, `ctx.Err()` in `WriteRecommendationHistory` |

---

## Regression Check (Do Not Regress items)

Each item from the v5 audit's "What Is Working Well" list was re-verified. **No regressions found.**

| Pattern | Location | Verified |
|---------|----------|----------|
| `DigestRow` int64 data plane | `internal/engine/core/types.go` | ✅ |
| Percentiles at ingest | `internal/ingestion/digest.go` | ✅ |
| `MarginScale` / `ApplyScaledMargin` | `internal/engine/core/margin_scaled.go` | ✅ |
| GPU classification int BP | `internal/engine/gpu/recommender.go` | ✅ |
| Streaming recommend `streamBatchSize = 500` | `internal/engine/recommend_all.go:36` | ✅ |
| `sync.Pool` digest buffers (CV, field, weighted) | `internal/ingestion/digest.go` | ✅ |
| `pgx.Batch` writes (container/namespace/PVC/GPU/node/VM) | Multiple files | ✅ |
| Cost LRU cache | `internal/costdata/provider.go:75` (`expirable.LRU`) | ✅ |
| Zero-copy `windowBounds` | Engine root | ✅ |
| Fused `RecommendCPUAndMemory` | Engine root | ✅ |
| Decay lookup table | `internal/engine/core/decay_table.go` | ✅ |
| Integer micro-cents savings | `internal/engine/core/savings_int.go` | ✅ |
| Bounded Prometheus labels | `internal/metrics/metrics.go` | ✅ |
| Slim list + typed `Collection[T]` | `internal/model/list_response.go` | ✅ |
| Manual positional `pgx.Scan` (PROF-2) | `internal/model/native_pgx_scan.go` | ✅ |
| Pre-allocated response slices (PROF-3) | `internal/model/recommendation_set_native.go` | ✅ |
| Covering index `idx_daily_container_digests_recommend` | Migration 000173 | ✅ |
| DB pool startup validation | `internal/config/` | ✅ |
| Context cancellation at flush boundary (ENG-CTX) | `recommend_all.go:368-370` | ✅ |
| Pre-computed CPU/Mem configs outside profile loop | `recommend_all.go:270-273` | ✅ |
| Cluster UUID LRU cache (API-N2) | `internal/clustercache/cache.go` | ✅ |
| `GPUContainerKey` struct (GPU-2) | `internal/engine/gpu/query.go` | ✅ |
| Node/VM partition DDL caching | `node_hourly_digest.go`, `vm_hourly_digest.go` | ✅ |
| `NotificationCodeBitmap` integer-based set | `internal/engine/core/notifications_bitmap.go` | ✅ |
| `FlushRecommendationBatch` shared utility | `internal/engine/core/types.go` | ✅ |
| Engine sub-package extraction (zero-cost aliases) | `internal/engine/compat.go` | ✅ |
| BH-POOL-1 weighted scratch pool | `internal/ingestion/digest.go` | ✅ |
| PLUGIN-ALLOC-1 cached plugin sets | `internal/plugin/registry.go` | ✅ |
| BH-CONFIG-1 pre-computed configs | `internal/engine/recommend_business_hours.go` | ✅ |
| BUILD-CGO `CGO_ENABLED=0` | `Dockerfile:6` | ✅ |
| CONC-RO read-only transaction | `internal/engine/recommend_all.go` | ✅ |

---

## Overall Assessment

The codebase remains in **excellent performance shape**. The 38 commits since v5 are dominated by **correctness/safety fixes** (adversarial reviews v11/v12), **feature additions** (multi-currency, VM PVC correlation, category unification), and **documentation** (upstreaming plans, ADRs). None of the changes touch the recommendation or ingestion hot paths in a performance-degrading way.

**Multi-currency architecture is well-designed:**
- Exchange rates cached in `expirable.LRU` with configurable TTL (not fetched per-request)
- User currency cached in `expirable.LRU` per org_id
- Stored currency cached per-request in `enrichmentCache`
- Conversion uses integer math (`ConvertCents`) — no float64 in the financial pipeline
- Conversion happens at API response time (not ingestion/recommendation), bounded by pagination

**Adversarial review fixes have negligible performance impact:**
- `io.LimitReader` wraps error-path reads only (not success-path)
- `ctx.Err()` checks every 10,000 CSV rows — one comparison per 10K iterations
- `ctx.Err()` in `WriteRecommendationHistory` — one check per batch chunk (~500 recs)
- `pgx.Identifier.Sanitize()` in GPU MIG queries — called per API request, not per row
- Structured logging (`logrus.WithFields`) replaces `log.Printf` — same overhead

**New findings:** This audit identified **12 items** (0 P0, 0 P1, 1 P2, 11 P3) — the P2 is a carry-forward (COMPAT-1). Two new P3 findings (PVC-SCAN, EXCHRATE-LOOP) have straightforward fixes that could yield measurable improvement at scale. The remaining items are low-severity or deferred.

---

## What Is Working Well (Updated — Do Not Regress)

Prior list items remain valid. **Post-v5 additions:**

- **Multi-currency exchange rate LRU cache** (`internal/costdata/provider.go:262-305`) — `exchangeRateCache` is a process-wide `expirable.LRU[string, float64]` with configurable TTL and max entries. Prevents repeated HTTP calls to Koku for exchange rates. Cache key uses null-byte separator (`orgID + "\x00" + from + "\x00" + to`) — zero-allocation for cache hits.
- **User currency LRU cache** (`internal/costdata/provider.go:206-232`) — `userCurrencyCache` avoids per-request HTTP call to `GET /api/cost-management/v1/user-settings/`.
- **Per-request `enrichmentCache`** (`internal/api/enrichment_cache.go:82-93`) — Request-scoped currency memoization prevents redundant cache lookups within a single API response.
- **Integer `ConvertCents`** (`internal/costdata/conversion.go`) — Currency conversion uses `int64(math.Floor(float64(cents)*rate + 0.5))`. The float64 is confined to this single multiplication; all upstream values are int64 cents. No float64 accumulation across rows.
- **`HoursInMonth` calendar-accurate** (`internal/engine/core/savings_int.go:25-28`) — Simple `time.Date` call (deterministic, no syscalls). Called once per month per recommendation type, not per container.
- **VM PVC in-memory correlation** (`internal/engine/vm/vm_pvc_correlation.go`) — Shared PVC detection operates on already-loaded `DailyVMDigest` slices. No additional DB queries for correlation logic.
- **VM category unification** (migration 000176) — Replaces 4 boolean columns + 3 partial boolean indexes with 1 TEXT column + 1 partial index on `category`. Reduces index maintenance overhead on VM writes.
- **`ctx.Err()` checks at ingestion boundaries** (`internal/ingestion/csvparser.go`, `vm_pvc_csv.go`, `container/history.go`) — Graceful cancellation with negligible overhead (one comparison per 10,000 rows or per batch chunk).

---

## Findings

### P2 — Medium

#### COMPAT-1. `ContainerExplValuePlaceholders` rebuilds constant string on every batch write

| Field | Value |
|-------|-------|
| **ID** | COMPAT-1 (carry-forward from v5, originally v4 EXPL-1) |
| **Severity** | P2 |
| **Location** | `internal/engine/core/explanation_persist.go:43-52` |
| **Current state** | Builds a 21-placeholder SQL fragment via string concatenation loop on every call. Called 3× per reconciliation (from `recommend_all.go`, `recommend_namespace.go`). |
| **Proposed fix** | Pre-compute the 3 variants at package init or use `strings.Builder` with pre-allocated capacity. |
| **Expected impact** | Eliminates 3 × 21 string concatenations per reconcile. Minor. |
| **Risk** | Low. |
| **Effort** | S (hours) |

---

### P3 — Low

#### VMPVC-N1. `IngestVMPVCCSV` issues per-VM lookup queries

| Field | Value |
|-------|-------|
| **ID** | VMPVC-N1 |
| **Severity** | P3 |
| **Location** | `internal/ingestion/vm_pvc_db.go:53-66` |
| **Current state** | For each unique (vm_name, namespace, bucket_date) group in the PVC CSV, `LookupVMDigestID` executes a `SELECT id FROM daily_vm_digests WHERE org_id=$1 AND cluster_uuid=$2 AND vm_name=$3 AND namespace=$4 AND bucket_date=$5::date`. Then `UpsertVMPVCDigests` starts a transaction with DELETE + individual INSERTs per PVC. Typical volume: ~50–200 VMs per cluster, 1–3 PVCs per VM. |
| **Index coverage** | `idx_daily_vm_digests_org_cluster (org_id, cluster_uuid)` covers the first two columns. The remaining three columns (`vm_name`, `namespace`, `bucket_date`) require a heap filter. At ~6000 rows per org+cluster (200 VMs × 30 days), this is a small scan per lookup. |
| **Proposed fix** | Batch-lookup all VM digest IDs in one query: `SELECT id, vm_name, namespace, bucket_date FROM daily_vm_digests WHERE org_id=$1 AND cluster_uuid=$2 AND (vm_name, namespace, bucket_date) IN (...)`. Build an in-memory map, then batch-upsert PVC rows. Alternatively, add a covering index on `(org_id, cluster_uuid, vm_name, namespace, bucket_date)`. |
| **Expected impact** | Reduces ~200 individual queries to 1 bulk query + 1 bulk insert. At current VM volumes (~200), saves ~200 round-trips (~20ms at 0.1ms/query). |
| **Trigger for upgrade to P2** | VM count per cluster exceeds 1000. |
| **Risk** | Low — batch lookup is straightforward. |
| **Effort** | M (days) |

---

#### VMPVC-INS. `UpsertVMPVCDigests` does per-row INSERT instead of batch

| Field | Value |
|-------|-------|
| **ID** | VMPVC-INS |
| **Severity** | P3 |
| **Location** | `internal/ingestion/vm_pvc_db.go:27-33` |
| **Current state** | Individual `tx.Exec` INSERT per PVC row within a transaction. Typical volume: 1–3 PVCs per VM × 200 VMs = ~600 INSERTs across ~200 transactions. |
| **Proposed fix** | Use `pgx.Batch` to batch all INSERTs per VM, or use a single `INSERT ... VALUES (...)` with multiple value tuples. |
| **Expected impact** | Reduces ~600 individual INSERTs to ~200 batched operations. Minor improvement (~10ms savings). |
| **Trigger for upgrade to P2** | PVC volume per cluster exceeds 5000. |
| **Risk** | Low. |
| **Effort** | S (hours) |

---

#### CURRENCY-PARSEROUND. `ParseCentsFromAmount` round-trips string → float64 → int64

| Field | Value |
|-------|-------|
| **ID** | CURRENCY-PARSEROUND |
| **Severity** | P3 |
| **Location** | `internal/money/format.go:78-87` — `ParseCentsFromAmount` |
| **Current state** | **RESOLVED.** `MoneyAmount` now has a `Cents int64 \`json:"-"\`` field populated by all formatters. `ParseCentsFromAmount` uses the cached value when available (~1.9 ns/op), falling back to `strconv.ParseFloat` for hand-built MoneyAmounts (~58 ns/op). ~30x speedup on the hot path. |
| **Quantification** | Called per MoneyAmount field per result. With 100 paginated results × 4–6 MoneyAmount fields = 400–600 calls per API response. Savings: ~34μs per response. |
| **Fix** | Added `Cents int64 \`json:"-"\`` to `MoneyAmount`. Populated in `FormatCentsToAmount`, `FormatUSDToAmount`, `SetAmountFromCents`. `ParseCentsFromAmount` returns cached value when `Cents != 0`. |
| **Benchmark** | Cached: 1.9 ns/op, 0 allocs. Fallback: 58 ns/op, 0 allocs. |
| **Risk** | Low — `json:"-"` ensures backward compatibility. |
| **Effort** | S (hours) — **Done** |

---

#### KAFKA-FMT. `partitionLockKey` uses `fmt.Sprintf` per message

| Field | Value |
|-------|-------|
| **ID** | KAFKA-FMT (carry-forward from v5) |
| **Severity** | P3 |
| **Location** | `internal/kafka/consumer.go:38-39`, `internal/kafka/lag.go:82` |
| **Current state** | `fmt.Sprintf("%s:%d", *tp.Topic, tp.Partition)` per Kafka message. Also appears in `lag.go` (new since v5). |
| **Expected impact** | ~3K string allocations per reconcile. |
| **Effort** | S |

---

#### BUILD-PGO. Profile-Guided Optimization not applied

| Field | Value |
|-------|-------|
| **ID** | BUILD-PGO (carry-forward from v5) |
| **Severity** | P3 |
| **Location** | Repository root (no `default.pgo` file) |
| **Current state** | No PGO profile. Go 1.25 supports PGO for 2–7% CPU improvement. |
| **Trigger for upgrade to P2** | CI infrastructure supports profile collection. |
| **Effort** | M (days) |

---

#### BUILD-GOTA. `go-gota/gota` vendored in legacy Kruize path

| Field | Value |
|-------|-------|
| **ID** | BUILD-GOTA (carry-forward from v5) |
| **Severity** | P3 |
| **Location** | `go.mod:13` |
| **Current state** | 4 source files import `go-gota/gota` — all in legacy Kruize aggregation path. |
| **Trigger for removal** | Kruize path fully deprecated. |
| **Effort** | S (once gated) |

---

#### COMPAT-SIZE. `compat.go` alias maintenance

| Field | Value |
|-------|-------|
| **ID** | COMPAT-SIZE (carry-forward from v5) |
| **Severity** | P3 |
| **Location** | `internal/engine/compat.go` |
| **Current state** | 388 lines, 93 aliases. Comprehensive godoc added (adversarial review v11 #6). Zero runtime cost — code organization debt only. 19 files outside `internal/engine/` now import sub-packages directly (e.g., `internal/api/handlers_vm_*.go`, `internal/plugins/vm/plugin.go`), indicating organic migration is happening. |
| **Trigger** | Code quality initiative, not performance-gated. |
| **Effort** | L (weeks) |

---

#### API-N6. GORM query builder in legacy namespace history

| Field | Value |
|-------|-------|
| **ID** | API-N6 (carry-forward from v5, reduced scope) |
| **Severity** | P3 |
| **Location** | `internal/api/handlers_namespace_history.go:10,134` and `internal/api/utils.go:18` |
| **Current state** | GORM usage reduced from 14 production references to 2 files: namespace history handler (`gorm.ErrRecordNotFound` check) and utils (`gorm.io/datatypes` import for `datatypes.JSONType`). No GORM query building remains in the native recommendation paths. |
| **Trigger for removal** | When namespace history is migrated to native pgx queries. |
| **Effort** | M (days) |

---

#### PVC-SCAN. O(N²×P) in-memory PVC sharing detection per engine run

| Field | Value |
|-------|-------|
| **ID** | PVC-SCAN |
| **Severity** | P3 |
| **Location** | `internal/engine/vm/vm_pvc_correlation.go:31-57`, called from `vm_runner.go` per VM |
| **Current state** | `detectSharedPVCsByName()` iterates all `clusterLatest` VMs (O(N)) for each VM being recommended. Total complexity is O(N × N × P) where N = VMs per cluster, P = PVCs per VM. For 100 VMs × 3 PVCs = 30K map lookups. For 500 VMs × 5 PVCs = 1.25M map lookups. Also allocates one `currentPVCNames map[string]bool` per VM per call. |
| **Proposed fix** | Pre-build a `pvcToVMs map[pvcName][]vmName` from `clusterLatest` once before the per-VM loop in `vm_runner.go`. Turns `detectSharedPVCsByName()` from O(N×P) per VM into O(P) per VM. |
| **Expected impact** | At 200 VMs: reduces ~120K map lookups to ~600. At 500 VMs: reduces ~1.25M to ~2500. |
| **Trigger for upgrade to P2** | VM count per cluster exceeds 500. |
| **Risk** | Low — reverse index is a straightforward pre-computation. |
| **Effort** | S (hours) |

---

#### EXCHRATE-LOOP. Per-row exchange rate cache lookup in VM recs handler

| Field | Value |
|-------|-------|
| **ID** | EXCHRATE-LOOP |
| **Severity** | P3 |
| **Location** | `internal/api/currency.go:205-215` (`convertNodeGPURecsToUserCurrency`), similar pattern in `enrichContainerCurrency` lines 79-106 |
| **Current state** | **RESOLVED.** `convertNodeGPURecsToUserCurrency` now uses the same `sampleCluster` hoist pattern as `enrichContainerCurrency` and `enrichNamespaceCurrency`. Rate is computed once for `recs[0].ClusterUUID` before the loop; only rows with a different cluster UUID trigger a re-fetch. |
| **Fix** | Added empty-slice guard, hoisted `fetchClusterCurrency` + `fetchExchangeRate` before loop, conditional re-fetch inside loop. |
| **Expected impact** | Eliminates ~100 mutex acquisitions per single-cluster API response (the common case). |
| **Risk** | Low. |
| **Effort** | S (hours) — **Done** |

---

#### EXCHRATE-KEYFMT. Exchange rate cache key allocates per lookup

| Field | Value |
|-------|-------|
| **ID** | EXCHRATE-KEYFMT |
| **Severity** | P3 |
| **Location** | `internal/costdata/provider.go:267-269` — `exchangeRateCacheKey` |
| **Current state** | `orgID + "\x00" + from + "\x00" + to` allocates a new string per cache lookup. For a single-currency environment (rate=1.0, early return at line 368-370), this allocation is skipped. For multi-currency, it's called per distinct cluster in the result set (typically 1–5 per API response). |
| **Expected impact** | ~5 string allocations per API response in multi-currency mode. Negligible. |
| **Resolution** | **Closed as won't fix** ([#378](https://github.com/pgarciaq/ros-ocp-backend/issues/378)). ROI not justified: struct key would break `cache.RemoveByPrefix` used by `InvalidateCostDataCache`, and the `from == to` early return already skips the key construction for single-currency deployments. |
| **Effort** | S — **Won't fix** |

---

## Deferred Items — Revisit Trigger Check

| ID | Item | Trigger | Met? | Assessment |
|----|------|---------|------|------------|
| **S1** | Unified windowed digest recommender | 6th recommendation type | **No** | Still 5 subsystems. |
| **S2** | Parallel container recommend by namespace | Recommend phase >30s | **No** | 100K: 1.7s. |
| **S3** | Namespace recs from container rollups | Product rollup spec | **No** | Unchanged. |
| **G-3** | Distributed debouncer | Multiple processor pods | **No** | Single-pod. |
| **B-3** | String interning for DigestKey | Memory profiling shows dup | **No** | No evidence. |
| **A-5** | Legacy Kruize `map[string]interface{}` | Legacy path retained | **N/A** | Still deprecated. |
| **I-1** | AWS SDK v1 removal | `platform-go-middlewares` drops v1 | **No** | v1 still indirect. |
| **PERF-09** | Rate limiter global mutex → sharded | p99 >500 req/s | **No** | No evidence. |
| **VM-2** | VM hourly int64 migration | VM volume >5000 | **No** | Still ~200 VMs. |
| **PERF-12** | Conditional `fleet_reduction` CTE | Large fleet p95 >500ms | **No** | No reports. |

---

## Accuracy Trade-off Register

| Trade-off | Introduced | Still valid? | Notes |
|-----------|------------|--------------|-------|
| Decay weight lookup quantization (~0.2% error) | P0-1 / ADR-0288 | ✅ | |
| Idle P95 → max-of-daily-P95 | P2-5 | ✅ | |
| Percentile-band plots (p50/p95/p99/max) | ADR-0292 | ✅ | |
| Sample tables dropped (digest-only) | Migration 000172 | ✅ | |
| Slim list contract (short_term cost only) | ADR-0294 | ✅ | |
| Savings integer micro-cents | ADR-0291 | ✅ | |
| VM float64 sizing (ALG-N2) | v3 | ✅ | Low cardinality; accuracy-sensitive. |
| Statement timeout cancellation | Phase13 | ✅ | |
| `computeVariation` ±1 rounding | Phase14 | ✅ | |
| Weighted percentile float64 accumulation | v1 | ✅ | Mathematically necessary for decay-weighted averaging. |
| Calendar-accurate monthly hours (ADR-0326) | Phase16 | ✅ | **New.** Replaces fixed 730h with `HoursInMonth(year, month)`. Precision improvement, not a trade-off — correct values now used. |
| Currency conversion float64 multiplication | Phase16 | ✅ | **New.** `ConvertCents` uses `int64(math.Floor(float64(cents)*rate + 0.5))`. Single float64 multiplication per MoneyAmount; cents input/output are int64. Rounding error bounded to ±1 cent per conversion (round-half-up). Acceptable for display-currency conversion. |

---

## ROI-Ordered Implementation Roadmap

### Quick Wins (S effort, hours each)

| Rank | ID | Title | Impact | Status |
|------|-----|-------|--------|--------|
| 1 | **COMPAT-1** | Pre-compute explanation placeholders | Eliminates 3 × 21 string concats per reconcile | Open |
| 2 | **EXCHRATE-LOOP** | Hoist exchange rate lookup outside single-cluster loops | ~100 fewer RWMutex acquisitions per API response | **Done** |
| 3 | **PVC-SCAN** | Pre-build PVC→VM reverse index | Reduces map lookups from O(N²×P) to O(N×P) per engine run | Open (new) |
| 4 | **CURRENCY-PARSEROUND** | Avoid string→float64 round-trip in currency conversion | Eliminates 400–600 ParseFloat per API response | **Done** |
| 5 | **KAFKA-FMT** | Pre-compute partition lock keys | 3K fewer string allocs per reconcile | Open |
| 6 | **VMPVC-INS** | Batch PVC INSERT per VM | ~10ms savings in VM ingestion | Open (new) |
| 7 | **EXCHRATE-KEYFMT** | Struct key for exchange rate cache | ~5 fewer string allocs per API response | Won't fix |

### High-Value Investments (M effort, days each)

| Rank | ID | Title | Impact | Status |
|------|-----|-------|--------|--------|
| 8 | **VMPVC-N1** | Batch VM digest lookup | ~200 fewer queries per VM PVC ingest | Open (new) |
| 9 | **BUILD-PGO** | Profile-Guided Optimization | 2–7% CPU throughput | Deferred (needs CI infra) |
| 10 | **API-N6** | Migrate namespace history off GORM | Removes last GORM production usage | Open (reduced scope) |

### Defer / Monitor

| Rank | ID | Title | Trigger |
|------|-----|-------|---------|
| — | **COMPAT-SIZE** | Migrate consumers off compat layer | Code quality initiative |
| — | **BUILD-GOTA** | Remove go-gota dependency | Kruize deprecation |
| — | **S1–S3** | Strategic architectural changes | See deferred triggers above |

---

## Appendix: New Feature Performance Impact Analysis

### Multi-Currency Savings Conversion (#363, #364)

**Architecture:** Three-tier caching prevents HTTP call amplification:

```
API Handler → enrichContainerCurrency()
  ├── resolveUserCurrency() → userCurrencyCache (LRU, per orgID, configurable TTL)
  ├── GetCachedCurrency()   → enrichmentCache (per-request, mutex-guarded)
  └── fetchExchangeRate()   → exchangeRateCache (LRU, per org+from+to, configurable TTL)
       └── (cache miss) → HTTP GET /api/cost-management/v1/currency-rates/
```

**Hot path impact:** None. Conversion is API-response-time only (post-query, pre-serialization). Bounded by pagination (10–100 results per response). Integer math (`ConvertCents`) for the actual conversion.

**Worst-case per response:** 1 `resolveUserCurrency` call (cached) + N `fetchExchangeRate` calls where N = result rows (one per row, hitting LRU cache). In the common single-cluster case, all rows have the same rate but the LRU mutex is acquired per row — see EXCHRATE-LOOP finding for proposed optimization.

### Per-PVC VM Shared Storage Detection (#359)

**Architecture:** PVC data ingested separately from VM usage CSVs via `IngestVMPVCCSV`. Stored in `vm_pvc_digests` table (FK to `daily_vm_digests`). Correlation runs in-memory on already-loaded `DailyVMDigest` slices during recommendation.

**Hot path impact:** None on the container/namespace recommendation hot path. Impact on VM recommendation: `detectSharedPVCsByName` is called per VM inside the recommendation loop, iterating all cluster-latest VMs each time. Actual complexity is O(N² × P) per engine run where N = total VMs in cluster, P = PVCs per VM. At 100 VMs × 3 PVCs = 30K map lookups; at 500 VMs × 5 PVCs = 1.25M. See PVC-SCAN finding for proposed fix (pre-build reverse index).

**DB impact:** New table `vm_pvc_digests` with proper indexes (FK, unique, parent). Per-VM lookup + upsert during ingestion — see VMPVC-N1 finding.

### Adversarial Review Fixes (v11, v12)

**Hot path impact:** None. All changes are on error paths (io.LimitReader), safety checks (ctx.Err() every 10K rows), logging (logrus.WithFields), or API-request-time operations (pgx.Identifier.Sanitize for MIG queries).

---

## Appendix: Call Count Estimates (Updated)

### Container reconciliation (100K containers, 30-day lookback)

| Phase | Operations | Notes |
|-------|-----------|-------|
| Load digests | 1 query → ~1.8M rows | Covered by index (no sort spill) |
| CV computation | 3M calls | Pooled scratch (✅) |
| Recommend compute | 4.5M decay lookups | Table hits (✅) |
| Category classify | 2.4M integer comparisons | Correct (✅) |
| Write batches | ~600 `pgx.Batch` sends | Correct (✅) |
| BH weighted digests | ~30K calls (BH clusters only) | Pooled (✅) (BH-POOL-1) |

### API response (100 paginated results, multi-currency)

| Phase | Operations | Notes |
|-------|-----------|-------|
| DB query | 1 paginated query | Native pgx, positional scan (✅) |
| Currency resolution | 1 userCurrency + 1–5 exchangeRate cache lookups | LRU cached (✅) |
| Currency conversion | 400–600 `ConvertCents` | Integer math (✅), ParseFloat overhead (P3 finding) |
| JSON serialization | 100 result structs | Standard `encoding/json` |

### VM PVC ingestion (200 VMs, 30 days)

| Phase | Operations | Notes |
|-------|-----------|-------|
| CSV parse | 1 streaming pass | `ctx.Err()` check per 10K rows (✅) |
| Digest lookup | ~200 individual queries | P3 finding (VMPVC-N1) |
| PVC upsert | ~200 transactions × 1–3 INSERTs each | P3 finding (VMPVC-INS) |

### Throughput (100K benchmark, observed — unchanged from v5)

| Metric | Value |
|--------|-------|
| Ingestion throughput | 14,700 containers/sec |
| Recommendation throughput | 60,000 containers/sec |
| Peak RSS | ~600 MB |
| List endpoint p95 (100 items) | ~12 ms |
| Detail endpoint p95 | ~5 ms |

---

## Summary

| Severity | Findings | Status |
|----------|----------|--------|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 1 | Open (COMPAT-1, carry-forward) |
| P3 | 9 | 4 carry-forward (KAFKA-FMT, BUILD-PGO, BUILD-GOTA, COMPAT-SIZE), 2 carry-forward with updated scope (API-N6), 3 new (VMPVC-N1, VMPVC-INS, CURRENCY-PARSEROUND, EXCHRATE-KEYFMT) |
| **Regressions** | **0** | All Do Not Regress items verified intact |
| **Total** | **10** | **0 Implemented, 10 Open** (5 carry-forward, 5 new — all low severity) |

**Assessment:** The codebase maintains its excellent performance posture through v6. The multi-currency feature introduces well-architected caching layers that prevent HTTP call amplification. The VM PVC feature has minor N+1 patterns but at volumes (~200 VMs) that make them negligible. No adversarial review fix has measurable performance impact.

The highest-impact unimplemented optimization remains PGO (BUILD-PGO, P3, deferred for CI infrastructure). The only P2 finding (COMPAT-1) is a minor string pre-computation that has been open since v4 — its impact is 3 string concatenations per reconcile, making it a code hygiene issue rather than a performance bottleneck.

Production headroom remains excellent: 14,700 containers/sec ingestion and 60,000 containers/sec recommendation throughput against a SaaS target of ~70 containers/sec — a 200× margin.
