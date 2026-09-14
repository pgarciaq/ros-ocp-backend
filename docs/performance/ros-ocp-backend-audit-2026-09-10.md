# Performance Audit Report: ros-ocp-backend (incremental follow-up)

## Date and Scope

**Date:** September 10, 2026
**Branch:** `pgarciaq-rosocp-superpowers-phase17` (`568ad1ad`)
**Prior audit:** [`ros-ocp-backend-audit-2026-09.md`](ros-ocp-backend-audit-2026-09.md) (September 2, 2026)
**Scope:** Incremental review per skill Prior Art rules — 61 commits since Sept 2. The delta is
dominated by **closing 9 of the 13 Sept findings** (BH indexes, GPU `org_id`, namespace BH
de-persist, detail digest reuse, capacity hints, CGO guard, quality-off-GORM, `rh_accounts`
elimination) plus API hardening (RBAC cache copies, per-org rate limiter, tenant-scoped alias
joins, quality column-count pins), build changes (ubi10, quay publish, image smoke, migration
lint, mapstructure v2), and test hygiene (goleak TestMains).

**Deployment modes considered:** SaaS (multi-tenant, RDS) and on-prem (single-tenant PostgreSQL).
Processor ingest remains the production hot path.

**Verification performed:** migration reads (000186–000193), targeted code reads (quality scans,
rate limiter, RBAC cache, enrichment cache, node detail, prune, backfill, debouncer), `rh_accounts`
caller audit (production vs test), Echo store mutex verification in module source, `float64` /
`math` / sort / `Sprintf` / `interface{}` sweeps, metric label cardinality check, and one full
`make test-short` run (see TEST-GOLEAK-SKIP — it fails at HEAD).

---

## Prior Audit Status — implemented items verified

All 9 closed Sept findings were re-verified against HEAD for completeness (no bypass elsewhere).
One correction and one残留 noted inline.

| Sept item | Issue | Status now |
|-----------|-------|------------|
| BH-IDX-NODE | [#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514) | ✅ Complete — `000186` creates `idx_daily_node_digests_cluster_sched_date (org_id, cluster_uuid, schedule_type, bucket_date, node)`. Migration header explicitly corrects the issue text (keeps `bucket_date` before `node` so the date range stays contiguous; the ORDER-BY claim was overclaimed) |
| BH-IDX-GPU | [#515](https://github.com/pgarciaq/ros-ocp-backend/issues/515) | ✅ Complete with cleanup — `000186` added the cluster-only index, then `000190` dropped it once PR-4 made every GPU SELECT/DELETE predicate `org_id` (`000187` covering index serves them). No write-amplification residue. `librobne/pgdigest/read_gpu.go:39` filters `org_id` + `cluster_uuid` |
| GPU-ORG-1 | [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) | ✅ Complete — `000187` (nullable + backfill + covering index), `000188` (NOT NULL), `000189` (unique key gains `org_id`), PR-4 org-scoped reads/housekeeper/prune. Known residues are documented, not forgotten: colliding-UUID backfill limitation has a detection probe ([#548](https://github.com/pgarciaq/ros-ocp-backend/issues/548)); orphan rows stay NULL until re-ingest (migration header) |
| BH-NS-2PASS | [#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516) | ✅ Complete — `000193` deletes dev BH rec rows, drops `schedule_type` from namespace rec tables; `rejectBusinessHoursNamespaceRecs` refuses BH persists; BH persist/history/read-time contract documented ([#527](https://github.com/pgarciaq/ros-ocp-backend/issues/527)) |
| BH-DETAIL-DUP | [#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517) | ✅ Complete — `QueryNodeBHDetailDigests` loads one node in a single CTE (`WITH bounds AS (SELECT MAX…)`), chart payload sliced in memory; VM detail same shape (`2f05c586`) |
| NODE-CLASSALLOC / PGDIGEST-NCAP | [#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520) | ✅ Complete — shared `DefaultDigestCapacity` (512) between pgdigest and `QueryNodeDigests`; `cpuMeans`/`imbalances` sized to `len(days)` (`7370115c`) |
| BUILD-CGO | [#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522) | ✅ Complete — `Dockerfile:9` builds with `CGO_ENABLED=1`, comment matches; `build.yml:27-46` fails the build if a `CGO_ENABLED=0` line appears or if a CGO=0 build unexpectedly succeeds ([#524](https://github.com/pgarciaq/ros-ocp-backend/issues/524)) |
| API-N6 (quality) | [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523) | ✅ Complete as scoped — positional pgx scans (`quality_pgx_scan.go`), negative-capacity clamp ([#561](https://github.com/pgarciaq/ros-ocp-backend/issues/561)), SELECT-vs-struct column-count pins ([#567](https://github.com/pgarciaq/ros-ocp-backend/issues/567)). Remainder: COUNT(*)+OFFSET SQL pattern persists by design (new P3 QUALITY-COUNT); namespace-history GORM remainder stays [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375) |
| SAVINGS-JOIN / directory joins | [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445) | ✅ Complete — `clusters.org_id` denormalized (`000191` + trigger, `000192` NOT NULL); heatmap + savings-by-cluster + machineset + GPU MIG alias joins tenant-scoped (`03302c62`, `26c48763`, `be80cac3`, `30ee1221`). Remaining production `rh_accounts` touches audited and legitimate: account create/validate ([#551](https://github.com/pgarciaq/ros-ocp-backend/issues/551)), tags startup probe, CLI `EnsureAccountCluster`, GPU backfill org scan (small directory table, batch path). No list/aggregation JOIN remains |

Still open from Sept (unchanged): BENCH-GAP [#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518),
LIBROBNE-DAY-1 [#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519),
MERGE-ALLOC [#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521) (no new hot callers —
only compat aliases reference it), BUILD-PGO [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372),
BUILD-GOTA [#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373),
COMPAT-SIZE [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513),
namespace-history [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375).

---

## Regression Check (Do Not Regress)

Sept "What Is Working Well" items spot-reverified against `568ad1ad`. **No hot-path regressions**
on ingest → recommend → write. New additions to the list at the bottom.

| Pattern | Location | Verified |
|---------|----------|----------|
| `DigestRow` int64 data plane | `librobne/types`, `internal/engine/core` | ✅ (no new `float64` on ingest/recommend/write; new sqrt sites are CLI/VM-adaptive/small-N only) |
| Percentiles at ingest | `internal/ingestion/digest.go`; `librobne/csv/digest.go` | ✅ |
| `MarginScale` / integer micro-cents | librobne / `savings_int.go` | ✅ |
| GPU classification int BP | `librobne/gpu` | ✅ |
| Streaming recommend batch 500 | `recommend_all.go:38`, `librobne/engine/recommend.go` | ✅ |
| Digest/CV/weighted `sync.Pool`s | `internal/ingestion/digest.go`, `librobne/digest/digest.go:89` | ✅ |
| `pgx.Batch` writes | `librobne/pgrec`, `pgdigest/batch.go` | ✅ |
| Cost LRU / cluster UUID LRU | `internal/costdata`, `internal/clustercache` | ✅ |
| Fused CPU/memory recommend | `librobne/engine` → `librobne/container` | ✅ |
| Decay lookup table | `librobne/types/decay*.go` | ✅ (`math.Exp` only for non-integer half-lives + table build) |
| Bounded Prometheus labels | `internal/metrics/metrics.go`, `internal/services/metrics.go` | ✅ (new `malformed_json_total{site}` allowlisted to `Site*` constants, [#553](https://github.com/pgarciaq/ros-ocp-backend/issues/553); reship trigger metric uses bounded `reason`) |
| Slim list + `org_container_keys` | `getNativeRecommendationsFromOrgKeys` | ✅ |
| Manual positional pgx scan | `native_pgx_scan.go` + new `quality_pgx_scan.go` | ✅ |
| Covering index `idx_daily_container_digests_recommend` | 000173 | ✅ |
| Context cancellation at flush | streaming recommend / librobne emit | ✅ |
| `GPUContainerKey` struct | `librobne/gpu` | ✅ |
| CSV `ReuseRecord` + `ctx.Err()` per 10k rows | `librobne/csv` | ✅ |
| Single-pass dual-stream ingest | `pipeline_stream.go` `groupedAll` + `groupedBH` | ✅ |
| Page-scoped BH enrichment | `ForEachScheduleForContainers` | ✅ (skill checklist §4 still holds — no `ForClusters` from list paths) |
| Namespace list omits BH | list enrichment | ✅ |
| Notification `AppendUnique` on hot path | tiny `[]int16` | ✅ (`MergeNotificationCodes` still unused on hot path) |
| Hourly retention knobs | `ROS_HOURLY_*_RETENTION_DAYS`, default 90 | ✅ |
| CGO correctness | Dockerfile + CI guard | ✅ (corrected — see Sept audit) |

---

## Overall Assessment

The Sept performance program is **~70% shipped and verified**: 9 findings closed with unusual
discipline (migration headers document the trade-offs, backfill hazards got detection probes,
dropped indexes were actually dropped). The processor hot path remains in excellent shape, and
the new API-layer work (quality pgx, tenant-scoped joins, column pins) follows the same
evidence-based pattern.

Two things changed the priority picture since Sept 2:

1. **BENCH-GAP is now the highest-value open item.** Nine landed fixes (indexes, org pruning,
   de-persisted second pass, shared detail reads) have never been measured together — the next
   benchmark doesn't just restore confidence, it quantifies completed work.
2. **`make test-short` is red at HEAD** (TEST-GOLEAK-SKIP) — the new goleak gate from
   [#536](https://github.com/pgarciaq/ros-ocp-backend/issues/536) catches a real skip-path leak in
   debouncer tests. Test-only, one-line fix, but it blocks the blessed local check.

**New findings:** 0 P0, 1 P2 (process), 3 P3. No new P1.

---

## What Is Working Well (Additions — Do Not Regress)

- **Index lifecycle hygiene** — `000190` drops the superseded cluster-only GPU index instead of
  leaving write amplification; `000193` shrinks unique keys after deleting dev rows. Future index
  PRs should follow this add-covering-then-drop-superseded pattern.
- **Migration headers as decision records** — 000186–000193 document *why* (column order vs WHERE
  not ORDER BY, DISTINCT ON collision semantics, trigger rationale, CONCURRENTLY runbook pointer).
  Do not "simplify" these comments away.
- **Quality scan contract** — positional scans + capacity clamp + compile-time column-count pins
  ([#561](https://github.com/pgarciaq/ros-ocp-backend/issues/561), [#567](https://github.com/pgarciaq/ros-ocp-backend/issues/567))
  make SELECT/scan drift a test failure instead of a production panic. Extend the pattern to any
  new pgx scan (namespace history, [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375)).
- **Bounded observability for new counters** — malformed-JSON `site` allowlist drops unknown values
  instead of admitting them as labels; reship failure metric is reason-bounded.
- **Rate limiter ships dark** — `ROS_API_RATE_LIMIT_ENABLED` defaults false; Echo store contention
  (RATE-MUTEX) cannot bite until explicitly enabled.
- **RBAC cache copies are cheap** — deep copy on read/store covers small `map[string][]string`
  permission sets (dozens of strings); correctness fix with negligible cost.

---

## Findings by Priority

### P0 — Critical

None.

### P1 — High

None.

### P2 — Medium

#### TEST-GOLEAK-SKIP. `make test-short` fails at HEAD: debouncer watcher leaks on the skip path

| Field | Value |
|-------|-------|
| **ID** | TEST-GOLEAK-SKIP |
| **Severity** | P2 (process — blessed local gate red; **no production impact**) |
| **Location** | `internal/services/manifest_recommendation_debouncer_test.go:26-44` (`TestInitSynthManifestDebouncer_StaleShutdownGoroutineIgnored`); watcher `manifest_recommendation_debouncer.go:68-75` |
| **Current state** | The test calls `InitSynthManifestDebouncer(ctx1)` (spawns watcher G1 on ctx1), then `SetupTestDB(t)` at line 33. In `-short` mode that calls `tb.Skip` → `Goexit`: deferred `cancel2()` runs (G2 dies) but the mid-test `cancel1()` at line 44 never executes → G1 blocks on `<-parent.Done()` forever → `testmain_test.go` goleak gate fails the whole `internal/services` package. Reproduced: `make test-short` → `FAIL internal/services` with `InitSynthManifestDebouncer.func1` in chan receive. Full `make test` (with Docker) passes, since line 44 runs |
| **Proposed fix** | `defer cancel1()` (or `t.Cleanup(cancel1)`) immediately after line 26. Audit other mid-test cancels before `SkipNow`-capable calls — the line-149 `Init` already uses `t.Cleanup(cancel` ✅ |
| **Expected impact** | Green `make test-short`; unblocks every contributor's pre-push check |
| **Risk** | None (test-only) |
| **Effort** | S |

### P3 — Low

#### RATE-MUTEX. Echo memory rate-limiter store takes one global mutex + full-map stale scan per request when enabled

| Field | Value |
|-------|-------|
| **ID** | RATE-MUTEX |
| **Severity** | P3 (dormant — limiter defaults **off**) |
| **Location** | `internal/api/middleware/rate_limiter.go:42-48` → Echo v4.15.2 `RateLimiterMemoryStore` (`mutex sync.Mutex`, `Allow` locks then `cleanupStaleVisitors()` scans the whole visitors map per request) |
| **Current state** | Per-org buckets via shared store. When enabled, every API request serializes on one mutex and pays O(visitors) stale-scan work under it. At SaaS scale (thousands of orgs × 5-min TTL) that is a per-request O(n) scan on the API hot path |
| **Proposed fix** | Before enabling in SaaS: either accept with a documented rps ceiling, or swap the store for a sharded/lock-free implementation (per-shard mutexes, lazy expiry without full scan). Keep the `UnknownOrgSentinel` single-bucket behavior either way |
| **Revisit trigger** | `ROS_API_RATE_LIMIT_ENABLED=true` in any environment, or API p99 evidence >500 req/s (existing PERF-09 trigger) |
| **Effort** | S (document + ceiling) / M (custom store) |

#### QUALITY-COUNT. Quality endpoints still run GORM-built COUNT(*) + OFFSET per request

| Field | Value |
|-------|-------|
| **ID** | QUALITY-COUNT |
| **Severity** | P3 |
| **Location** | `internal/model/recommendation_quality.go:51-74` (+ pvc/vm/snapshot/gpu-mig siblings): GORM `COUNT(*)` with identical filters, then `Order(...).Offset(...).Limit(...).Rows()` into the positional scanner |
| **Current state** | [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523) removed reflection from row materialization only; the two-query COUNT+page pattern and OFFSET remain. Mitigated today: quality tables are small, `(org_id, engine, measured_at)` indexes exist (000131/000159–000162), and these are low-traffic diagnostic endpoints — not the list hot path (skill checklist §3 unaffected) |
| **Proposed fix** | Defer. If quality traffic or table size grows: keyset on `(measured_at, id)` + replace exact COUNT with the stats-table pattern used for containers, or cache counts |
| **Revisit trigger** | Quality p95 >500 ms or quality tables >1M rows/org |
| **Effort** | M |

#### ENRICH-SERIAL. Koku rate fetch happens while holding the request-scoped enrichment mutex

| Field | Value |
|-------|-------|
| **ID** | ENRICH-SERIAL |
| **Severity** | P3 (latent — no concurrent caller today) |
| **Location** | `internal/api/enrichment_cache.go:66-80` — `GetCachedCostRates` holds `cache.mu` across `provider.GetEffectiveRates` (network I/O) |
| **Current state** | Harmless today: the cache is request-scoped and all enrichment call sites are sequential, so the mutex is uncontended and acts as a correct coarse single-flight. If enrichment is ever fanned out across goroutines sharing one request ctx, same-request Koku fetches serialize and latencies add |
| **Proposed fix** | If parallelizing enrichment: move the fetch outside the lock with `singleflight` (or per-cluster promise map). Until then, do not "optimize" — the current shape is the correct one for sequential callers |
| **Revisit trigger** | Any `go func` enrichment fan-out sharing a request ctx |
| **Effort** | S (when triggered) |

---

## Deferred Items — Revisit Triggers

| ID | Item | Trigger | Met? | Assessment |
|----|------|---------|------|------------|
| BENCH-GAP | Streaming 100K re-benchmark, BH on/off | Was: repeat scale-benchmark | **No — now top priority** | Nine fixes landed since the last numbers; re-run quantifies shipped work, not just restores confidence |
| S2 | Parallel container recommend | Recommend phase >30s in production | No | No new production timing evidence; BENCH-GAP first |
| VM-2 | VM hourly int64 migration | VM volume >5000 | No | BH doubles VM digest rows but no volume evidence |
| PERF-09 | Rate limiter sharding | p99 >500 req/s or limiter enabled | No | RATE-MUTEX documents the dormant shape |
| B-3 | `DigestKey` string interning | Dup strings in heap profile | No | No new 100K RSS profile (blocked on BENCH-GAP) |
| PERF-12 | Conditional `fleet_reduction` CTE | Heatmap p95 >500 ms | No | |
| MERGE-ALLOC | `MergeNotificationCodes` O(n²) | Hot caller appears | No | Compat aliases only; still deferred |
| CLI-LOAD | `csv.Load` bounds | robne pointed at multi-GB dumps | No | Accepted trust model |
| S1/S3/G-3 | Unify recommender / rollup recs / distributed debouncer | As previously defined | No | Unchanged |
| BUILD-PGO / BUILD-GOTA / COMPAT-SIZE / #375 | Carry-forwards | CI profiles / Kruize removal / caller migration / history rewrite | No | Unchanged; quality-scan contract should extend to #375 when it lands |

---

## Accuracy Trade-off Register (additions)

| Trade-off | Introduced | Still valid? | Notes |
|-----------|------------|--------------|-------|
| Namespace BH persist removed; BH sizing GET-time | [#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516) / 000193 | ✅ New | Contract change, not precision loss: History stays all-hours; detail computes BH from digests |
| Malformed-JSON keep-going coercion counted by site | [#538](https://github.com/pgarciaq/ros-ocp-backend/issues/538) / [#553](https://github.com/pgarciaq/ros-ocp-backend/issues/553) | ✅ New | Behavior unchanged (coerce-to-empty); observability only, cardinality-bounded |
| GPU colliding-UUID backfill picks lowest account id | [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) / [#548](https://github.com/pgarciaq/ros-ocp-backend/issues/548) | ✅ New | Documented with detection probe; self-heals on re-ingest |
| Prior register (decay quantization, idle P95, integer micro-cents, dual-stream BH, …) | Sept audit | ✅ | No changes in this delta |

---

## ROI-Ordered Implementation Roadmap

### Fix now (S)

| Rank | ID | Title |
|------|-----|--------|
| 1 | **TEST-GOLEAK-SKIP** | `defer cancel1()` in debouncer test — greens `make test-short` |
| 2 | **BENCH-GAP** [#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518) | Re-run streaming 100K, BH on/off — measures 9 shipped fixes |

### Next value (M)

| Rank | ID | Title |
|------|-----|--------|
| 3 | **LIBROBNE-DAY-1** [#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519) | Pool CLI `computeDayWeighted` if robne is a scale tool |
| 4 | **API-N6 remainder** [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375) | Namespace history off GORM, reusing the quality-scan contract |
| 5 | **BUILD-PGO** [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372) | Still gated on CI profiles |

### Defer (with triggers above)

RATE-MUTEX, QUALITY-COUNT, ENRICH-SERIAL, COMPAT-SIZE [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513),
BUILD-GOTA [#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373), MERGE-ALLOC
[#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521), S1–S3, CLI-LOAD.

---

## Appendix: Call Count Estimates (delta vs Sept 2)

| Path | Before (Sept 2) | Now (HEAD) |
|------|-----------------|------------|
| Namespace recommend+write, BH on | 2× load+compute+write (BH-NS-2PASS) | **1×** — BH second pass removed (#516) |
| Node/VM detail, BH + Visual Insights on | MAX + range + duplicate chart reads (BH-DETAIL-DUP) | **1 CTE + in-memory slice** (#517) |
| GPU org prune | `DELETE … USING clusters JOIN rh_accounts` full-join prune | **Index-only** `WHERE org_id AND schedule_type` (#512) |
| Fleet savings/heatmap final SELECT | Surplus `rh_accounts` join | **Dropped** (#445) |
| Quality row materialization | GORM reflection per row | **Positional pgx scan** (#523) |
| `make test-short` at HEAD | n/a | **RED** — TEST-GOLEAK-SKIP (test-only) |

**Throughput:** no new production-path measurements in this delta (BENCH-GAP still open).
Do not quote July numbers as current.
