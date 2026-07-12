# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date

Version: 10 | Date: 2026-07-12 | Reviewer: AI-assisted (incremental, post-v4 performance audit)

## Scope

Incremental review covering ~38 commits since v9 (2026-07-08). Major changes:

- **Performance audit implementation:** pgx.Batch for node/VM recommendation
  persistence, N+1 query fix in cluster quota reprojection, cluster UUID cache
  (`internal/clustercache/`), capacity hints for slice/map pre-sizing, `ctx.Err()`
  cancellation checks in streaming pipelines, covering index (migration 000174),
  autovacuum tuning (migration 000175), heatmap correlated subquery rewrite.
- **GPU-2 refactor:** String composite key → `GPUContainerKey` struct across 17
  production and 12 test call sites.
- **Documentation:** v4 performance audit report, terminology updates, ADR index
  backfill (0311–0319), CHANGELOG updates.

Three parallel subagent reviews were conducted covering all 8 dimensions:
Security, Correctness, Performance, Operational Robustness, Design Quality,
Maintainability, Auditability, and Governance.

## Executive Summary

The v9→v10 delta demonstrates continued strong engineering execution. The
performance audit implementations are well-designed: pgx.Batch chunking is
correctly calibrated, `ctx.Err()` checks are placed at appropriate iteration
boundaries, and the cluster UUID cache has clean separation with comprehensive
Prometheus observability. Four of twelve v9 findings were resolved (pool caps,
ADR index, float precision docs, BH digest coverage), and no findings regressed.

However, eight v9 findings remain open, including the original High-severity
namespace/quota fallback scan without statement timeout (v9 #2). The new code
introduces seven Medium-severity findings, the most significant being: the
cluster cache returning mutable slice references (enabling cross-request data
corruption), `loadDigestRows` buffering unbounded rows without a hard cap (OOM
risk on anomalous clusters), and VM recommendation history operations running
outside the main transaction (inconsistency on failure). Thirteen Low-severity
findings cover defense-in-depth gaps, documentation debt, and maintainability
improvements.

Overall assessment: **Good** — the codebase continues to improve, but the
accumulation of 8 unresolved prior findings alongside 20 new findings (7 Medium,
13 Low) indicates that remediation velocity should increase. The Medium findings
are all straightforward fixes (S effort).

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★☆ | DDL `fmt.Sprintf` without identifier quoting persists in 3 locations |
| Correctness | ★★★★☆ | Cluster cache returns mutable slice; csvDownloadHTTPClient data race still open |
| Performance | ★★★★☆ | `loadDigestRows` unbounded memory; `node_recommendations` missing from autovacuum tuning |
| Operational robustness | ★★★★☆ | Quota/namespace fallback scan now guarded by statement timeout |
| Design quality | ★★★★☆ | `engine` package is 245 files / 49K lines; `clustercache` is well-separated |
| Maintainability | ★★★★☆ | Duplicate `maxPgxBatchQueue` constant; inconsistent chunk clamping idiom |
| Auditability | ★★★★☆ | v8 audit document still shows all findings as Open; cluster cache lacks structured logging |
| Governance | ★★★★☆ | Migration 000174 missing concurrent-index advisory comment; ARV-12 still absent from CHANGELOG |

## Prior Findings Status (v9 → v10)

| v9 # | Title | Severity | Status | Issue | Notes |
|------|-------|----------|--------|-------|-------|
| 1 | `csvDownloadHTTPClientSingleton` data race | Medium | **Still Open** | [#280](https://github.com/pgarciaq/ros-ocp-backend/issues/280) | Lazy init still uses bare nil check without `sync.Once` at `internal/utils/utils.go:42-57` |
| 2 | Namespace fallback scan runs without statement timeout | High | **Resolved** | [#281](https://github.com/pgarciaq/ros-ocp-backend/issues/281) | Both `getNativeNamespaceByIDFallback` and `ResolveQuotaKeyByID` fallback scans now wrapped in `WithHeavyGORMStatementTimeout` / `WithHeavyStatementTimeout` with `RecordStatementTimeoutCancellation`. |
| 3 | Statement timeout lint test brace tracking is brittle | Low | **Still Open** | [#282](https://github.com/pgarciaq/ros-ocp-backend/issues/282) | `statement_timeout_lint_test.go:65` still uses `line == "}"` heuristic; not regressed but not improved |
| 4 | scratch.counts and scratch.sorted uncapped in pool | Low | **Resolved** | — | `capWeightedPairs()` now caps `pairs` at `maxWeightedPairsCap=512`; `counts` and `sorted` are bounded by `weightedCountingSortMaxSpan=4096` |
| 5 | Unquoted table name in `ensureEntityQualityPartitions` | Low | **Still Open** | [#283](https://github.com/pgarciaq/ros-ocp-backend/issues/283) | `partitions_startup.go:68-70` still interpolates `partName` via `fmt.Sprintf` without `pgx.Identifier.Sanitize()` |
| 6 | `DetermineCSVType` Contains fallback | Low | **Still Open** | [#284](https://github.com/pgarciaq/ros-ocp-backend/issues/284) | Final fallback at `utils.go:442` still returns `PayloadTypeContainer` for unrecognized filenames |
| 7 | ADR README index missing 0311–0317 | Medium | **Resolved** | — | `docs/adr/README.md` now lists ADRs 0311–0319 |
| 8 | ARV-12 silently dropped from CHANGELOG | Low | **Still Open** | [#285](https://github.com/pgarciaq/ros-ocp-backend/issues/285) | CHANGELOG has no entry for ARV-12 by finding number |
| 9 | v8 audit document findings status not updated | Low | **Still Open** | [#286](https://github.com/pgarciaq/ros-ocp-backend/issues/286) | `adversarial-review-v8-2026-07-07.md` still shows all 17 findings as Open |
| 10 | `optionalFloat32Str` precision difference undocumented | Low | **Resolved** | — | CHANGELOG documents the `float32` vs `float64` variant distinction |
| 11 | Business-hours weighted digest path lacks pool coverage | Low | **Resolved** | — | `computeAllWeightedFieldDigests()` now evaluates weights once and reuses across all metric fields; both paths pool-covered |
| 12 | Echo pprof Symbol endpoint rejects POST | Low | **Still Open** | [#287](https://github.com/pgarciaq/ros-ocp-backend/issues/287) | `pprof.go:22` still registers `/debug/pprof/symbol` as `e.GET(...)` only; `go tool pprof` symbolize uses POST |

**Summary:** 5 Resolved, 7 Still Open, 0 Regressed.

## Findings Status Summary

| # | Title | Severity | Dimension | Issue | Status |
|---|-------|----------|-----------|-------|--------|
| 1 | Cluster UUID cache returns/stores mutable slice reference | Medium | Correctness | [#288](https://github.com/pgarciaq/ros-ocp-backend/issues/288) | **Resolved** |
| 2 | Cluster cache serves stale data after source addition | Medium | Operational | [#289](https://github.com/pgarciaq/ros-ocp-backend/issues/289) | Open |
| 3 | `loadDigestRows` buffers unbounded rows without hard cap | Medium | Performance | [#290](https://github.com/pgarciaq/ros-ocp-backend/issues/290) | Open |
| 4 | `PersistVMRecommendations` history append/prune outside transaction | Medium | Operational | [#291](https://github.com/pgarciaq/ros-ocp-backend/issues/291) | Open |
| 5 | Duplicate `maxPgxBatchQueue` constant across packages | Medium | Maintainability | [#292](https://github.com/pgarciaq/ros-ocp-backend/issues/292) | Open |
| 6 | `fetchAndCache` returns unwrapped errors | Medium | Auditability | [#293](https://github.com/pgarciaq/ros-ocp-backend/issues/293) | Open |
| 7 | Migration 000174 uses non-concurrent `CREATE INDEX` without advisory comment | Medium | Governance | [#294](https://github.com/pgarciaq/ros-ocp-backend/issues/294) | Open |
| 8 | Retention cleanup interpolates table/column names via `fmt.Sprintf` | Low | Security | [#295](https://github.com/pgarciaq/ros-ocp-backend/issues/295) | Open |
| 9 | `flushRecommendationBatch` trusts caller-provided count parameter | Low | Correctness | [#296](https://github.com/pgarciaq/ros-ocp-backend/issues/296) | Open |
| 10 | `QueryDailyVMDigests` missing capacity hint and `ctx.Err()` check | Low | Performance | [#297](https://github.com/pgarciaq/ros-ocp-backend/issues/297) | Open |
| 11 | Autovacuum tuning (000175) missing `node_recommendations` table | Low | Performance | [#298](https://github.com/pgarciaq/ros-ocp-backend/issues/298) | Open |
| 12 | `flushRecommendationBatch` error lacks row-level context | Low | Operational | [#299](https://github.com/pgarciaq/ros-ocp-backend/issues/299) | Open |
| 13 | No health check coverage for cluster cache component | Low | Operational | [#300](https://github.com/pgarciaq/ros-ocp-backend/issues/300) | Open |
| 14 | Inconsistent `chunkEnd` clamping idiom across pgx.Batch call sites | Low | Maintainability | [#301](https://github.com/pgarciaq/ros-ocp-backend/issues/301) | Open |
| 15 | `GPUContainerKey` duplicates `gpuMIGQualityKey` struct | Low | Maintainability | [#302](https://github.com/pgarciaq/ros-ocp-backend/issues/302) | Open |
| 16 | Cluster cache has no structured logging on DB fallback path | Low | Auditability | [#303](https://github.com/pgarciaq/ros-ocp-backend/issues/303) | Open |
| 17 | `PersistVMRecommendations` lacks advisory lock documentation | Low | Design | [#304](https://github.com/pgarciaq/ros-ocp-backend/issues/304) | Open |
| 18 | v4 audit report HEAD reference is stale | Low | Governance | [#305](https://github.com/pgarciaq/ros-ocp-backend/issues/305) | Open |
| 19 | Migration 000175 lacks `IF EXISTS` guards | Low | Governance | [#306](https://github.com/pgarciaq/ros-ocp-backend/issues/306) | Open |
| 20 | `engine` package is a God package (245 files, 49K lines) | Low | Design | [#307](https://github.com/pgarciaq/ros-ocp-backend/issues/307) | Open |

## Findings Detail

### Finding 1 ([#288](https://github.com/pgarciaq/ros-ocp-backend/issues/288)): Cluster UUID cache returns/stores mutable slice reference

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `internal/clustercache/cache.go:81-84` (Get) and `cache.go:136` (store) |
| **Description** | Both `GetClustersForOrg` and `GetClustersForOrgWithPool` return the cached `[]string` slice directly from the LRU without copying. Similarly, `fetchAndCache` stores the query-result slice directly via `c.Add(orgID, uuids)`. If any caller appends to, sorts, or filters the returned slice in-place, the cached data is corrupted for all subsequent cache hits. |
| **Risk** | Any API handler that filters or manipulates the returned cluster list (e.g., `filterClustersByRBAC`, `restrictClustersToQueryFilter`) could silently corrupt the cache, causing one org's requests to see different cluster lists depending on request ordering. This is a cross-request data integrity issue. |
| **Recommendation** | Return a defensive copy from both Get functions: `return append([]string(nil), val...), nil`. Alternatively, store a copy in `fetchAndCache`. At least one side must copy. |
| **Effort** | S |

### Finding 2 ([#289](https://github.com/pgarciaq/ros-ocp-backend/issues/289)): Cluster cache serves stale data after source addition

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Operational robustness |
| **Location** | `internal/clustercache/cache.go` (missing invalidation call in ingest path) |
| **Description** | The cluster UUID cache has a 30-second TTL (`defaultTTLSecs = 30`) and is invalidated on source deletion (called in `sourcesCleaner.go:106`, `retention.go:210`, and after recommendation recalculations). However, when a new cluster is added (registered via source events or ingested for the first time), there is no `InvalidateOrg` call in the ingest/pipeline path. Until the TTL expires, API list endpoints will not show the new cluster's recommendations. |
| **Risk** | Users may see missing data for up to 30 seconds after a new source registers. In multi-replica deployments, each replica has its own cache, so some may serve stale data differently. |
| **Recommendation** | Call `clustercache.InvalidateOrg(orgID)` in the ingest completion path (e.g., `report_processor.go` after successful manifest completion) alongside existing `fleetsummary.InvalidateOrg` calls. |
| **Effort** | S |

### Finding 3 ([#290](https://github.com/pgarciaq/ros-ocp-backend/issues/290)): `loadDigestRows` buffers unbounded rows without hard cap

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance |
| **Location** | `internal/engine/recommend_all.go:60-144` |
| **Description** | `loadDigestRows` pre-allocates 8192 capacity and appends unboundedly, buffering the entire query result set. For a cluster with 10K containers × 15 days (short_term window), this is ~150K rows. Each `digestRowWithKey` is ~464 bytes, so 150K rows ≈ 70 MiB. For a 50K-container cluster, this approaches ~350 MiB in a single allocation. The `statement_timeout` protects the DB query but the Go-side allocation is unbounded. |
| **Risk** | A single anomalous cluster (many containers or many days of data) can cause OOM in the processor. |
| **Recommendation** | Add a configurable hard cap (e.g., `ROS_MAX_DIGEST_ROWS_PER_CLUSTER=500000`) and return an error if exceeded. Log a metric when >80% of cap is reached. |
| **Effort** | S |

### Finding 4 ([#291](https://github.com/pgarciaq/ros-ocp-backend/issues/291)): `PersistVMRecommendations` history append/prune outside transaction

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Operational robustness |
| **Location** | `internal/engine/vm_db.go:220-225` |
| **Description** | After the main transaction commits (line 216), `AppendVMRecommendationHistory` and `PruneVMRecommendationHistory` are called outside the transaction. If `AppendVMRecommendationHistory` fails, the recommendations are committed but no history record exists. If `PruneVMRecommendationHistory` fails, the error propagates and the caller treats the entire operation as failed — even though recommendations were already persisted, which can cause unnecessary retries and potentially duplicate history entries. |
| **Risk** | History tracking inconsistency: recommendations appear but corresponding history entries are missing. Prune failure causes unnecessary retry of the entire batch. |
| **Recommendation** | Either: (a) move history append into the same transaction, or (b) make `PruneVMRecommendationHistory` non-fatal (log warning, don't return error) since it's a housekeeping operation. |
| **Effort** | S |

### Finding 5 ([#292](https://github.com/pgarciaq/ros-ocp-backend/issues/292)): Duplicate `maxPgxBatchQueue` constant across packages

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Maintainability |
| **Location** | `internal/engine/recommend_all.go:24` and `internal/ingestion/pipeline.go:24` |
| **Description** | The constant `maxPgxBatchQueue = 2000` is defined independently in two packages (`engine` and `ingestion`). Both carry matching comments ("Matched to the ingestion-side constant"), implying they must stay in sync. There is no compile-time or test-time enforcement that they remain equal. |
| **Risk** | Low probability but high confusion cost. A developer updating one constant but not the other gets no feedback until production shows different batch behavior. |
| **Recommendation** | Extract to a shared package (e.g., `internal/dbutil/batch.go`) or add a test asserting the values are equal. |
| **Effort** | S |

### Finding 6 ([#293](https://github.com/pgarciaq/ros-ocp-backend/issues/293)): `fetchAndCache` returns unwrapped errors

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Auditability |
| **Location** | `internal/clustercache/cache.go:118-132` |
| **Description** | `fetchAndCache` returns bare `err` at three points (lines 119, 127, 132) without wrapping context via `fmt.Errorf`. Compare with the rest of the codebase where errors are consistently wrapped (e.g., `recommend_nodes.go:1035` → `fmt.Errorf("query node digests: %w", err)`). When a cluster cache query fails, the caller receives an opaque pgx error with no indication it came from the cluster cache layer. |
| **Risk** | In a production incident where the cluster UUID query fails, operators see a raw pgx error in logs with no mention of "cluster cache" or "fetchAndCache," complicating root-cause analysis. |
| **Recommendation** | Wrap all three error returns: `return nil, fmt.Errorf("cluster cache fetch for org %s: %w", orgID, err)`. |
| **Effort** | S |

### Finding 7 ([#294](https://github.com/pgarciaq/ros-ocp-backend/issues/294)): Migration 000174 uses non-concurrent `CREATE INDEX` without advisory comment

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Governance |
| **Location** | `migrations/000174_idx_snapshot_cost_by_type.up.sql` |
| **Description** | The migration creates `idx_snapshot_cost_by_type` using `CREATE INDEX IF NOT EXISTS` (non-concurrent). The migrations README explicitly states: "Prefer `CREATE INDEX CONCURRENTLY` for new indexes on large tables so deployments do not block writes during index builds." Other recent migrations include comments referencing `migrations/README.md` for the concurrent equivalent, but migration 000174 has no such comment. |
| **Risk** | On a large `snapshot_recommendation_sets` table (100K+ rows), this acquires a `SHARE` lock, blocking all writes for the duration of the index build. In SaaS with continuous ingestion, this causes write queue backup. |
| **Recommendation** | Add the standard advisory comment and add the concurrent pre-migration SQL block to `migrations/README.md`. |
| **Effort** | S |

### Finding 8 ([#295](https://github.com/pgarciaq/ros-ocp-backend/issues/295)): Retention cleanup interpolates table/column names via `fmt.Sprintf`

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/engine/retention.go:184,215` |
| **Description** | `purgeDateRetainedTable` interpolates `dt.Table` and `dt.DateColumn` into DELETE SQL via `fmt.Sprintf`. These values come from `RetentionTable` structs defined as compile-time constants. However, the function accepts an arbitrary `RetentionTable` parameter with no validation that the table/column names are safe identifiers. |
| **Risk** | Minimal — all callers use hardcoded struct literals. Future dynamic retention config would be vulnerable. Defense-in-depth concern. |
| **Recommendation** | Use `pgx.Identifier.Sanitize()` or add a static allowlist check. |
| **Effort** | S |

### Finding 9 ([#296](https://github.com/pgarciaq/ros-ocp-backend/issues/296)): `flushRecommendationBatch` trusts caller-provided count parameter

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/engine/recommend_all.go:395-404` |
| **Description** | `flushRecommendationBatch` accepts an `n int` parameter and calls `br.Exec()` exactly `n` times. If a caller miscalculates `n` (passes a value larger than the batch queue depth), the function calls `br.Exec()` on an exhausted batch, which returns an error. All current call sites correctly compute `n`, but the function has no internal validation. `batch.Len()` is available for assertion. |
| **Risk** | Future callers adding a new batch path could miscalculate `n`, causing a confusing pgx error instead of a clear mismatch error. |
| **Recommendation** | Assert `n == batch.Len()` at the top, or use `batch.Len()` directly and drop the `n` parameter. |
| **Effort** | S |

### Finding 10 ([#297](https://github.com/pgarciaq/ros-ocp-backend/issues/297)): `QueryDailyVMDigests` missing capacity hint and `ctx.Err()` check

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/engine/vm_db.go:45` |
| **Description** | `var result []model.DailyVMDigest` is initialized with zero capacity (no pre-allocation hint), unlike `loadDigestRows` (8192) and `QueryNodeDigests` (512). The row scanning loop also has no `ctx.Err()` cancellation check, unlike `RecommendWorkloadsStreaming` which checks every `streamBatchSize` containers. |
| **Risk** | Minor: repeated slice re-allocation for large VM fleets, and cancellation latency (up to full scan before noticing cancellation). |
| **Recommendation** | Add `make([]model.DailyVMDigest, 0, 256)` capacity hint and `ctx.Err()` check every ~1000 rows. |
| **Effort** | S |

### Finding 11 ([#298](https://github.com/pgarciaq/ros-ocp-backend/issues/298)): Autovacuum tuning (000175) missing `node_recommendations` table

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `migrations/000175_tune_quota_vm_autovacuum.up.sql` |
| **Description** | The migration tunes `quota_recommendation_sets`, `cluster_quota_recommendation_sets`, and `vm_recommendations` with `autovacuum_vacuum_scale_factor=0.05` and `fillfactor=85`. However, `node_recommendations` is also a full-table UPSERT target (see `PersistNodeRecommendations` with `ON CONFLICT ... DO UPDATE`). With pgx.Batch chunking at 2000 rows, nodes accumulate dead tuples at a similar rate. |
| **Risk** | `node_recommendations` will have delayed vacuuming compared to its siblings, potentially leading to index bloat. |
| **Recommendation** | Add `node_recommendations` to a follow-up migration (000176). |
| **Effort** | S |

### Finding 12 ([#299](https://github.com/pgarciaq/ros-ocp-backend/issues/299)): `flushRecommendationBatch` error lacks row-level context

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `internal/engine/recommend_all.go:395-404` |
| **Description** | `flushRecommendationBatch` iterates `n` times calling `br.Exec()` and returns the first error. The error is wrapped by the caller with the chunk start index but there's no way to know which specific row within the chunk failed. Since the batch runs inside a transaction, failure causes full rollback (no inconsistent state), but lack of row-level context makes debugging data-specific issues harder. |
| **Risk** | Debugging only — no data inconsistency risk since the transaction rolls back atomically. |
| **Recommendation** | Log the row index within the chunk on error: `return fmt.Errorf("batch row %d/%d: %w", i, n, err)`. |
| **Effort** | S |

### Finding 13 ([#300](https://github.com/pgarciaq/ros-ocp-backend/issues/300)): No health check coverage for cluster cache component

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `internal/clustercache/cache.go`, `internal/api/server.go` |
| **Description** | The cluster cache has comprehensive Prometheus metrics but is not included in readiness/health probe checks. If cache initialization fails (`sync.Once` panic), the LRU remains nil and `getCache()` would panic on subsequent access. The `sync.Once` pattern means this would crash the process (which Kubernetes restarts), so it's self-healing. |
| **Risk** | Very low. The cache is a simple in-memory LRU with no external dependencies. Failure would require a programming bug, not an operational issue. |
| **Recommendation** | No immediate action. Consider adding a `cache != nil` check to the readiness probe if cache complexity grows. |
| **Effort** | S |

### Finding 14 ([#301](https://github.com/pgarciaq/ros-ocp-backend/issues/301)): Inconsistent `chunkEnd` clamping idiom across pgx.Batch call sites

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | 13 call sites across `internal/engine/` and `internal/ingestion/` |
| **Description** | Newer files (`recommend_nodes.go:1084`, `vm_db.go:97`) use `chunkEnd := min(chunkStart+maxPgxBatchQueue, len(recs))`, while 11 older call sites use the three-line idiom: `chunkEnd := chunkStart + maxPgxBatchQueue; if chunkEnd > len(x) { chunkEnd = len(x) }`. Both are correct but the inconsistency hinders grep-based auditing. |
| **Risk** | Negligible functional risk — purely maintainability debt. |
| **Recommendation** | Standardize on `min()` (Go 1.21+) across all 13 call sites. |
| **Effort** | S |

### Finding 15 ([#302](https://github.com/pgarciaq/ros-ocp-backend/issues/302)): `GPUContainerKey` duplicates `gpuMIGQualityKey` struct

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `internal/engine/gpu_query.go:24-28` and `internal/engine/gpu_mig_quality.go:14-18` |
| **Description** | `GPUContainerKey` (exported) and `gpuMIGQualityKey` (unexported) have identical fields: `{Namespace, Workload, ContainerName}`. The MIG quality code uses its own private struct as a map key and converts from `GPUContainerKey` at the boundary. |
| **Risk** | If a field is added to `GPUContainerKey` (e.g., `ClusterUUID`), `gpuMIGQualityKey` would silently miss it, causing join failures. |
| **Recommendation** | Replace `gpuMIGQualityKey` with `GPUContainerKey` — they are structurally identical. |
| **Effort** | S |

### Finding 16 ([#303](https://github.com/pgarciaq/ros-ocp-backend/issues/303)): Cluster cache has no structured logging on DB fallback path

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Auditability |
| **Location** | `internal/clustercache/cache.go:112-139` |
| **Description** | `fetchAndCache` executes a database query on every cache miss but produces no log entry. Cache hit/miss is tracked via Prometheus counters (good for dashboards) but not for per-request debugging. When a specific org's cache refresh fails, operators have counter increments but no log line connecting the failure to a specific org_id. |
| **Risk** | During incident triage, operators can see that cache misses increased but cannot correlate a specific failure to an org without debug-level logging. |
| **Recommendation** | Add structured log at INFO on success: `logging.Log.Infof("cluster cache refreshed org=%s clusters=%d", orgID, len(uuids))` and at ERROR on failure. |
| **Effort** | S |

### Finding 17 ([#304](https://github.com/pgarciaq/ros-ocp-backend/issues/304)): `PersistVMRecommendations` lacks advisory lock documentation

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Design quality |
| **Location** | `internal/engine/vm_db.go:79-229` vs `internal/engine/recommend_nodes.go:1063-1185` |
| **Description** | `PersistNodeRecommendations` acquires `pg_advisory_xact_lock(7358001)` to prevent deadlocks with migration 000058 (PK rebuild). `PersistVMRecommendations` follows the same pgx.Batch pattern but has no advisory lock. While no VM-specific PK rebuild migration exists today, the asymmetry means a future migration could deadlock without the developer knowing to check the node equivalent. |
| **Risk** | Low — only materializes if a future migration modifies `vm_recommendations` under concurrent writes. |
| **Recommendation** | Add a code comment: "No advisory lock needed — no concurrent migration modifies vm_recommendations PK. See recommend_nodes.go:nodeRecsAdvisoryLock for the pattern if a future migration requires one." |
| **Effort** | S |

### Finding 18 ([#305](https://github.com/pgarciaq/ros-ocp-backend/issues/305)): v4 audit report HEAD reference is stale

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Governance |
| **Location** | `docs/performance/native-engine-audit-v4-2026-07.md:7` |
| **Description** | The v4 audit header states it was done at commit `dc783f44` (Jul 11), but later commits updated the document post-hoc. The stated HEAD is misleading — the document reflects a later state than claimed. |
| **Risk** | Negligible — someone cross-referencing the audit against `dc783f44` would find the document includes changes from after that commit. |
| **Recommendation** | Update the HEAD reference to the actual last commit reviewed, or add a "Last updated" field separate from the initial audit commit. |
| **Effort** | S |

### Finding 19 ([#306](https://github.com/pgarciaq/ros-ocp-backend/issues/306)): Migration 000175 lacks `IF EXISTS` guards

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Governance |
| **Location** | `migrations/000175_tune_quota_vm_autovacuum.up.sql` |
| **Description** | The migration applies `ALTER TABLE ... SET (...)` directly on `quota_recommendation_sets`, `cluster_quota_recommendation_sets`, and `vm_recommendations`. If any table doesn't exist (e.g., fresh installation with out-of-order migrations), the migration fails. All three statements lack existence guards, unlike the `CREATE INDEX IF NOT EXISTS` pattern used elsewhere. |
| **Risk** | Low — in normal sequential migration execution, tables exist by migration 000175. Only a risk in non-standard deployment scenarios. |
| **Recommendation** | Wrap each ALTER TABLE in a DO block with existence check, or document that migrations must run sequentially. |
| **Effort** | S |

### Finding 20 ([#307](https://github.com/pgarciaq/ros-ocp-backend/issues/307)): `engine` package is a God package (245 files, 49K lines)

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Design quality |
| **Location** | `internal/engine/` |
| **Description** | The `engine` package contains 245 `.go` files totaling ~49K lines. While individual files are reasonably sized (largest non-test: `recommend_nodes.go` at 1,206 lines), the package owns container recommendations, node recommendations, VM recommendations, PVC recommendations, quota recommendations, GPU classification, GPU MIG profiling, GPU time-slicing, savings, quality tracking, history, retention, idle detection, threshold settings, term config, business hours, and category classification. |
| **Risk** | Increasing cognitive load. The flat 245-file namespace degrades IDE autocomplete and symbol search discoverability. No circular dependency risk (leaf package), but a maintenance concern. |
| **Recommendation** | Consider incremental sub-packaging: `engine/node/`, `engine/vm/`, `engine/gpu/`, `engine/quota/`. Long-term refactor, not urgent. |
| **Effort** | L |

## Items Verified Clean

The following new code areas were inspected across all three review dimensions and found correct:

- **pgx.Batch chunking in `PersistNodeRecommendations` and `PersistVMRecommendations`:** Uses `min(chunkStart+maxPgxBatchQueue, len(recs))` correctly, avoiding off-by-one. Partial final batches are handled. Errors from `flushRecommendationBatch` propagate and trigger `tx.Rollback` via defer.

- **N+1 fix in `applyClusterQuotaListReprojection`:** Groups items by `clusterUUID` before querying (lines 106-111), fetches `QueryContainerQuotaAggregates`, `QueryNamespaceQuotaSnapshotsForNamespaces`, and `FetchRecommendationCostData` once per cluster. Correctly handles empty `Namespaces` lists (empty `nsSet` → no-op query). Reduces O(items) to O(clusters) queries.

- **GPUContainerKey struct refactor:** All 17 production and 12 test call sites consistently use the struct. No residual string composite keys found. Map comparisons use Go's native struct equality for `{string, string, string}`.

- **ctx.Err() cancellation checks:** Correctly placed at iteration boundaries: every `streamBatchSize` containers in `RecommendWorkloadsStreaming`, per-PVC group, per-namespace, per-cluster in recalculation workers. Returns the context error without swallowing it.

- **Heatmap query RBAC:** All SQL uses parameterized queries (`$1`, `$2`, etc.) with `ANY($4)` for cluster arrays. No string interpolation of user input. The `term`, `engine`, and `metric` parameters are validated against fixed allowlists before use.

- **Covering index migration (000174):** Static DDL with hardcoded table/column names. The index `ON snapshot_recommendation_sets (org_id, recommendation_type) INCLUDE (estimated_cost_cents)` correctly covers the `GetSnapshotCostByType` query, enabling index-only scans when the visibility map is up to date. The autovacuum tuning in 000175 complements this well.

- **Autovacuum tuning migration (000175):** Static DDL `ALTER TABLE` with hardcoded table names. Settings (`vacuum_scale_factor=0.05`, `analyze_scale_factor=0.02`, `fillfactor=85`) are appropriate for UPSERT-heavy tables.

- **Map pre-sizing constants in `pipeline_stream.go`:** `defaultGroupedAllCapacity=4096`, `defaultGroupedBHCapacity=1024`, `defaultNodeAccumCapacity=256` are initial hints, not hard limits. Go maps grow automatically when exceeded. Values are reasonable for typical cluster sizes.

- **Cluster quota reprojection N+1 fix:** At O(clusters) queries (3 per cluster), this is acceptable for typical orgs with <10 clusters. An org with 100 clusters would issue 300 queries, but the endpoint is protected by `WithHeavyStatementTimeout`.

- **Statement timeout coverage:** Comprehensive 3-tier model (API 25s, Heavy API 28-45s, Ingest 120s) with proper `SET LOCAL` scoping.

- **`clustercache` package design:** Clean separation with minimal imports (only `config`, `db`). No coupling to engine or API layers. `sync.Once` initialization is correct. Prometheus metrics cover hits, misses, size, removals, and invalidations. Test coverage includes hit/miss, TTL expiry, invalidation, and capacity eviction.

- **pgx.Batch pattern consistency:** The shared `flushRecommendationBatch` helper via `pgxBatchSender` interface is used by all three persist paths (container, node, VM). Error wrapping at the caller level includes chunk offset context. The advisory lock in node recommendations is a thoughtful safety measure.

- **No hardcoded secrets, permissive CORS, or missing rate limits** found in new code.

- **No `_ = err` silent error suppression** found in changed code.

- **CHANGELOG discipline is strong:** All significant changes (pgx.Batch queue depth, digest flush default, CSV reuse, covering index, sample table removal) are documented with issue references.

- **ADR coverage is comprehensive:** 319+ ADRs with maintained index. "Won't Fix" and "Deferred" decisions have proper ADR entries (0311-0317). Recent performance decisions (0318-0321) are documented.

## Priority Remediation Order

| Priority | Finding | Issue | Severity | Title | Effort |
|----------|---------|-------|----------|-------|--------|
| 1 | 1 | [#288](https://github.com/pgarciaq/ros-ocp-backend/issues/288) | Medium | Cluster cache mutable slice — defensive copy on return | S | **Resolved** |
| 2 | v9 #2 | [#281](https://github.com/pgarciaq/ros-ocp-backend/issues/281) | High (prior) | Namespace/quota fallback scan — add statement timeout or LIMIT | S | **Resolved** |
| 3 | 3 | [#290](https://github.com/pgarciaq/ros-ocp-backend/issues/290) | Medium | `loadDigestRows` — add configurable row cap | S |
| 4 | 4 | [#291](https://github.com/pgarciaq/ros-ocp-backend/issues/291) | Medium | VM history — move into transaction or make prune non-fatal | S |
| 5 | 2 | [#289](https://github.com/pgarciaq/ros-ocp-backend/issues/289) | Medium | Cluster cache — add `InvalidateOrg` in ingest completion path | S |
| 6 | 6 | [#293](https://github.com/pgarciaq/ros-ocp-backend/issues/293) | Medium | `fetchAndCache` — wrap errors with context | S |
| 7 | 5 | [#292](https://github.com/pgarciaq/ros-ocp-backend/issues/292) | Medium | `maxPgxBatchQueue` — extract to shared package or add sync test | S |
| 8 | 7 | [#294](https://github.com/pgarciaq/ros-ocp-backend/issues/294) | Medium | Migration 000174 — add concurrent-index advisory comment | S |
| 9 | v9 #1 | [#280](https://github.com/pgarciaq/ros-ocp-backend/issues/280) | Medium (prior) | `csvDownloadHTTPClientSingleton` — replace with `sync.Once` | S |
| 10 | v9 #5 | [#283](https://github.com/pgarciaq/ros-ocp-backend/issues/283) | Low (prior) | DDL identifier quoting in `partitions_startup.go` | S |
| 11 | 8 | [#295](https://github.com/pgarciaq/ros-ocp-backend/issues/295) | Low | Retention.go identifier quoting | S |
| 12 | 11 | [#298](https://github.com/pgarciaq/ros-ocp-backend/issues/298) | Low | Add `node_recommendations` to autovacuum tuning | S |
| 13 | 9 | [#296](https://github.com/pgarciaq/ros-ocp-backend/issues/296) | Low | Assert `batch.Len()` in `flushRecommendationBatch` | S |
| 14 | 15 | [#302](https://github.com/pgarciaq/ros-ocp-backend/issues/302) | Low | Replace `gpuMIGQualityKey` with `GPUContainerKey` | S |
| 15 | 10 | [#297](https://github.com/pgarciaq/ros-ocp-backend/issues/297) | Low | VM digest capacity hint + `ctx.Err()` check | S |
| 16 | 16 | [#303](https://github.com/pgarciaq/ros-ocp-backend/issues/303) | Low | Add structured logging to cluster cache | S |
| 17 | 12 | [#299](https://github.com/pgarciaq/ros-ocp-backend/issues/299) | Low | Add row index to batch error messages | S |
| 18 | 14 | [#301](https://github.com/pgarciaq/ros-ocp-backend/issues/301) | Low | Standardize `chunkEnd` on `min()` across 13 call sites | S |
| 19 | v9 #6 | [#284](https://github.com/pgarciaq/ros-ocp-backend/issues/284) | Low (prior) | Tighten `DetermineCSVType` fallback | S |
| 20 | 17 | [#304](https://github.com/pgarciaq/ros-ocp-backend/issues/304) | Low | Document advisory lock pattern in `vm_db.go` | S |
| 21 | v9 #12 | [#287](https://github.com/pgarciaq/ros-ocp-backend/issues/287) | Low (prior) | Fix Echo pprof Symbol POST handling | S |
| 22 | v9 #3 | [#282](https://github.com/pgarciaq/ros-ocp-backend/issues/282) | Low (prior) | Improve lint test brace tracking | S |
| 23 | v9 #8 | [#285](https://github.com/pgarciaq/ros-ocp-backend/issues/285) | Low (prior) | Add ARV-12 entry to CHANGELOG | S |
| 24 | v9 #9 | [#286](https://github.com/pgarciaq/ros-ocp-backend/issues/286) | Low (prior) | Update v8 audit document status | S |
| 25 | 18 | [#305](https://github.com/pgarciaq/ros-ocp-backend/issues/305) | Low | Update v4 audit HEAD reference | S |
| 26 | 19 | [#306](https://github.com/pgarciaq/ros-ocp-backend/issues/306) | Low | Add IF EXISTS guards to migration 000175 | S |
| 27 | 13 | [#300](https://github.com/pgarciaq/ros-ocp-backend/issues/300) | Low | Cluster cache health check (defer unless complexity grows) | S |
| 28 | 20 | [#307](https://github.com/pgarciaq/ros-ocp-backend/issues/307) | Low | Engine God package sub-packaging | L |

## Accepted Risks

None explicitly accepted. All findings are actionable. The following informational
items were reviewed and found to be correctly designed, requiring no action:

- **Covering index 000174:** Correctly covers `GetSnapshotCostByType` for index-only scans.
- **Map pre-sizing constants:** Reasonable capacity hints that grow automatically.
- **Cluster quota reprojection O(clusters):** Acceptable at current scale (<10 clusters per org).

## Current State

- **Total new findings this version:** 20 (7 Medium, 13 Low)
- **Resolved from v9:** 5 (v9 #2, #4, #7, #10, #11)
- **Still open from v9:** 7 (v9 #1, #3, #5, #6, #8, #9, #12)
- **Total open:** 27 (8 Medium [7 new + 1 prior], 19 Low [13 new + 6 prior])
- **Accepted:** 0
- **Regressed:** 0
