# Performance Audit Report: ros-ocp-backend

## Date and Scope

**Date:** September 2, 2026
**Branch:** `pgarciaq-rosocp-superpowers-phase17` (`08c8f80e`)
**Prior audit:** [`ros-ocp-backend-audit-2026-07.md`](ros-ocp-backend-audit-2026-07.md) (July 25, 2026, phase16)
**Related:** [adversarial review v13](../audit/adversarial-review-v13-2026-08-31.md) (correctness/security of the same delta); [librobne extract baseline](librobne-baseline-841639f3/README.md)

**Scope:** Full 11-dimension follow-up covering ~124 commits since July 25. The delta is dominated by:

1. **librobne nested-module extract** — container/namespace/node/GPU/VM/PVC/quota/snapshot compute, CSV `ForEach*`, and `pgdigest`/`pgrec` I/O moved into `librobne/`
2. **Ingest dedup** onto `librobne/csv.ForEach*` (#475, #501) and `pgdigest.ForEachSchedule` (#476)
3. **Business-hours extra cases** — dual-stream digests for node/GPU/timeslicing/VM (migrations 000182–000185), overnight windows, Peak hours Visual Insights
4. **robne CLI** Phases 1–3 (CSV recommend, PostgreSQL upsert, BH siblings, explain)
5. **Notification bitmap removal** (#509) — slice-based `MergeNotificationCodes`; codes 79–82

**Deployment modes considered:** SaaS (multi-tenant, RDS) and on-prem (single-tenant PostgreSQL, 512Mi–8Gi chart profiles). Processor ingest remains the production hot path; robne CLI is a separate in-process path that materializes tarballs.

**Commits reviewed:** 124 since 2026-07-25.

**GitHub tracking** (`pgarciaq/ros-ocp-backend`):

| ID | Issue | Notes |
|----|-------|-------|
| BH-IDX-NODE | [#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514) | new |
| BH-IDX-GPU | [#515](https://github.com/pgarciaq/ros-ocp-backend/issues/515) | new |
| GPU-ORG-1 | [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) | existing; prune/index impact raised to P2 in this audit |
| BH-NS-2PASS | [#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516) | new |
| BH-DETAIL-DUP | [#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517) | new |
| BENCH-GAP | [#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518) | new |
| LIBROBNE-DAY-1 | [#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519) | new |
| NODE-CLASSALLOC / PGDIGEST-NCAP | [#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520) | new (combined) |
| SAVINGS-JOIN | [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445) | existing leftover join |
| MERGE-ALLOC | [#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521) | new; deferred/postponed |
| BUILD-CGO-COMMENT | [#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522) | new |
| API-N6 (quality OFFSET) | [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523) | new; namespace-history remainder remains [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375) |
| BUILD-PGO | [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372) | July carry-forward |
| BUILD-GOTA | [#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373) | July carry-forward |
| COMPAT-SIZE | [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513) | July carry-forward |

---

## Prior Audit Status

The July 25 audit reported **0 P0 / 0 P1**, one P2 (COMPAT-1, resolved in that cycle), and leftover P3s: BUILD-PGO, BUILD-GOTA, COMPAT-SIZE, API-N6 (reduced scope).

| July item | Status now |
|-----------|------------|
| COMPAT-1 explanation placeholders | Still resolved (`explPlaceholderCache`) |
| EXCHRATE-LOOP, CURRENCY-PARSEROUND, PVC-SCAN, VMPVC-N1/INS, KAFKA-FMT | Still resolved |
| PLUGIN-ALLOC-1 cached plugin sets | **Still resolved** — `plugin.Boot()` parses allow/deny once (`internal/plugin/registry.go`) |
| BH-POOL-1 / DIGEST-1 scratch pools | **Still resolved on processor ingest** (`internal/ingestion/digest.go`). librobne CLI digest path is a new, unpooled sibling ([#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519)) |
| CONC-RO read-only digest tx | **Still resolved** — `pool.BeginTx(..., AccessMode: pgx.ReadOnly)` in `loadDigestRows` |
| BUILD-CGO `CGO_ENABLED=0` | **Mis-stated in July as a win.** Processor binary **must** use CGO for `confluent-kafka-go`. At `08c8f80e` the Dockerfile RUN line is still `CGO_ENABLED=0` with a matching comment — restore `CGO_ENABLED=1` and fix the comment ([#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522)) |
| BUILD-PGO | Still open — [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372) |
| BUILD-GOTA | Still open — [#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373) (4 legacy Kruize files) |
| COMPAT-SIZE | Still open — [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513) |
| API-N6 GORM on list hot path | Still reduced — native list is pgx; quality endpoints remain GORM OFFSET ([#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523)); namespace history remainder [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375) |

---

## Regression Check (Do Not Regress)

July “What Is Working Well” items were re-verified against phase17 HEAD. **No hot-path regressions** on the processor ingest → recommend → write loop. One July checklist item (BUILD-CGO=0) was never a valid production constraint.

| Pattern | Location | Verified |
|---------|----------|----------|
| `DigestRow` int64 data plane | `librobne/types`, `internal/engine/core` | ✅ |
| Percentiles at ingest | `internal/ingestion/digest.go` (processor); `librobne/csv/digest.go` (CLI) | ✅ processor |
| `MarginScale` / `ApplyScaledMargin` | librobne / engine core | ✅ |
| GPU classification int BP | `librobne/gpu` | ✅ |
| Streaming recommend `streamBatchSize = 500` | `internal/engine/recommend_all.go:38`; `librobne/engine/recommend.go` | ✅ |
| `sync.Pool` digest/CV/weighted scratch | `internal/ingestion/digest.go` (`cvScratchPool`, `fieldBufferPool`, `weightedScratchPool`) | ✅ processor |
| `weightedDigestScratchPool` | `librobne/digest/digest.go:89` | ✅ (CLI + any ComputeWeightedDigest caller) |
| `pgx.Batch` writes | `librobne/pgrec`, `librobne/pgdigest/batch.go`, ingestion flush | ✅ |
| Cost LRU cache | `internal/costdata` | ✅ |
| Fused CPU/memory recommend | `librobne/engine` → `librobne/container` | ✅ |
| Decay lookup table | `librobne/types/decay.go` + `decay_table.go` | ✅ |
| Integer micro-cents savings | librobne / `internal/engine/core/savings_int.go` | ✅ |
| Bounded Prometheus labels | `internal/metrics/metrics.go` | ✅ |
| Slim list + `org_container_keys` | `getNativeRecommendationsFromOrgKeys` | ✅ |
| Manual positional pgx scan | `internal/model/native_pgx_scan.go` | ✅ |
| Covering index `idx_daily_container_digests_recommend` | migration 000173 | ✅ (includes `schedule_type`) |
| Context cancellation at flush | `RecommendWorkloadsStreaming` / librobne emit | ✅ |
| Cluster UUID LRU | `internal/clustercache` | ✅ |
| `GPUContainerKey` struct | `librobne/gpu` | ✅ |
| CSV `ReuseRecord = true` | all `librobne/csv` `ForEach*` parsers | ✅ **new** |
| `ctx.Err()` every 10_000 CSV rows | `librobne/csv` | ✅ |
| Single-pass dual-stream ingest | `pipeline_stream.go` `groupedAll` + `groupedBH` | ✅ **new** |
| Page-scoped BH enrichment | `ForEachScheduleForContainers` | ✅ |
| Namespace list omits BH (#497) | `EnrichNativeNamespaceListResults` | ✅ |

---

## Overall Assessment

The processor hot path is still in **excellent shape**. The librobne extract was a packaging move, not a rewrite of the integer data plane: 10k `cmd/bench` recommend time **improved 6.1%** versus the extract baseline (`4,536 ms → 4,261 ms`) with **lower** peak Sys (`422.8 MB → 365.7 MB`). CSV ingest now streams through shared `ForEach*` parsers with `ReuseRecord` and cancellation checks.

The new risk is **dual-stream business hours on every major entity**, not the extract itself:

- Ingest writes `all_hours` + `business_hours` in one CSV pass (correct). Storage and index leaf visits roughly **double** on BH-enabled clusters for node/GPU/VM digest tables whose unique keys put `schedule_type` last.
- Namespace recommend **runs twice** (all-hours persist + BH persist) on the processor. Container BH sizing stays API-time (sibling digest read) — the better pattern.
- Detail handlers for node (and the same shape for VM) **re-query** BH digests when Visual Insights is on.
- `gpu_container_digests` still has **no `org_id`**, so org-wide BH prune joins `clusters → rh_accounts` (known [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512)).

**Production-path 100K numbers from July have not been re-run** on phase17 with dual-stream BH. Do not quote 14,700 containers/sec ingest / 60,000 rec/sec as current until `cmd/bench` streaming (or the scale-benchmark skill) is repeated.

**New findings:** 0 P0, 0 P1, 6 P2, 7 P3. Highest ROI: BH digest indexes, GPU `org_id`, node/VM detail digest reuse, namespace BH second-pass documentation/optional skip.

---

## What Is Working Well (Updated — Do Not Regress)

Prior list remains valid. **Post-July additions:**

- **librobne extract without a compute regression** — 10k recommend gate passed; Peak Sys dropped (copy detector: no second `[]DigestRow`). `librobne/engine.RecommendWorkloads` is sequential batch-emit (“pool-free” means no goroutine worker pool — S2 is still the right deferral).
- **Shared streaming CSV parsers** — `ForEachRow` / `ForEachNamespace` / `ForEachPVC` / `ForEachVM` / … set `csv.Reader.ReuseRecord = true` and honor `ctx.Err()` every 10_000 accepted rows.
- **One CSV pass, two digest streams** — `parseAndDigestCSVStream` appends all-hours and BH samples, GPU, and node accumulators in the same row callback (`pipeline_stream.go:223-246`). No second parse.
- **Processor digest pools survived** — `computeCPUUsageCVBP` and weighted field digests still use `sync.Pool` in `internal/ingestion/digest.go`. Do not “simplify” this away when touching ingest.
- **`pgdigest.ForEachSchedule` / `ForEachAllHours`** — callback iteration instead of buffering an entire cluster then filtering.
- **BH list contract** — container list BH enrichment is page-keyed; namespace/node/VM/GPU **lists** stay all-hours; Peak hours lives on detail (#497).
- **Notification codes 64+** — slice merge keeps codes 79–82; bitmap cardinality limit is gone. Hot path uses `AppendUnique` (tiny `[]int16`), not `MergeNotificationCodes`.
- **Kafka CGO is required** — processor image must be `CGO_ENABLED=1` for `confluent-kafka-go`. At audit HEAD the Dockerfile still has the July `CGO_ENABLED=0` line ([#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522)). robne CLI stays `CGO_ENABLED=0` (`Makefile` `robne` target).
- **Hourly Visual Insights retention knobs** — `ROS_HOURLY_VM_DIGESTS_RETENTION_DAYS` / `ROS_HOURLY_NODE_DIGESTS_RETENTION_DAYS` (default 90). VM plugin `SweepRetention` deletes `daily_vm_digests` by `bucket_date` (not missing from retention).

---

## Findings by Priority

### P0 — Critical

None.

### P1 — High

None. Dual-stream BH is by design; cost is storage and extra queries, not a correctness-breaking hot-path stall at current on-prem node counts.

### P2 — Medium

#### BH-IDX-NODE ([#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514)). Cluster-wide node BH queries cannot use `schedule_type` until after `node` + `bucket_date`

| Field | Value |
|-------|-------|
| **ID** | BH-IDX-NODE |
| **Severity** | P2 |
| **Location** | `migrations/000182_node_business_hours.up.sql` PK `(org_id, cluster_uuid, node, bucket_date, schedule_type)`; query in `librobne/pgdigest/read_node.go:27-40` and `internal/engine/node/recommend.go:64-78` |
| **Current state** | Recommend/CLI load: `WHERE org_id AND cluster_uuid AND bucket_date BETWEEN AND schedule_type = $5`. PK prefix is `(org_id, cluster_uuid, node, …)`. After dual-stream, each node-day has two rows. The planner can use `(org_id, cluster_uuid)` then filter `schedule_type` across **both** streams. |
| **Quantification** | 100 nodes × 90 days = 9,000 all-hours rows; BH doubles to ~18,000 index/heap visits per cluster recommend. At 1,000 nodes (heatmap-scale fleets) that is ~180k visits vs ~90k with a matching index. |
| **Proposed fix** | `CREATE INDEX CONCURRENTLY idx_daily_node_digests_cluster_sched_date ON daily_node_digests (org_id, cluster_uuid, schedule_type, bucket_date, node);` — matches the WHERE + `ORDER BY node, bucket_date`. |
| **Expected impact** | BH cluster scans skip all-hours leaves; ~2× less I/O on BH recommend/CLI Path A. |
| **Risk** | Low — additive index; write amplification on ingest UPSERT. |
| **Effort** | S |

#### BH-IDX-GPU ([#515](https://github.com/pgarciaq/ros-ocp-backend/issues/515)). GPU digest unique key buries `schedule_type` after wide text columns

| Field | Value |
|-------|-------|
| **ID** | BH-IDX-GPU |
| **Severity** | P2 |
| **Location** | `migrations/000183_gpu_business_hours.up.sql` unique `(cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start, schedule_type)`; `librobne/pgdigest/read_gpu.go:31-44` |
| **Current state** | Read path is `WHERE cluster_uuid AND interval_start range AND schedule_type`. Unique index can use `cluster_uuid` + partition pruning on `interval_start`, but `schedule_type` is last after several TEXT keys. BH reads still walk all-hours keys for the same containers. Table has **no `org_id`**. |
| **Proposed fix** | `CREATE INDEX idx_gpu_container_digests_cluster_sched_start ON gpu_container_digests (cluster_uuid, schedule_type, interval_start);` Pair with GPU-ORG-1 when adding `org_id`. |
| **Expected impact** | Faster BH GPU detail/timeslicing enrichment on GPU-dense clusters. |
| **Risk** | Low. |
| **Effort** | S |

#### GPU-ORG-1 ([#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512)). `gpu_container_digests` has no `org_id`; org BH prune joins `rh_accounts`

| Field | Value |
|-------|-------|
| **ID** | GPU-ORG-1 |
| **Severity** | P2 (performance of prune/maintenance; schema debt [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512)) |
| **Location** | `migrations/000042_create_gpu_container_digests.up.sql`; `internal/bhschedule/prune.go:104-111`; `librobne/pgdigest/read_gpu.go` (cluster UUID only) |
| **Current state** | Cluster prune is `DELETE … WHERE cluster_uuid AND schedule_type` (fine). **Org** prune:

```sql
DELETE FROM gpu_container_digests g
USING clusters c JOIN rh_accounts a ON c.tenant_id = a.id
WHERE g.cluster_uuid = c.cluster_uuid AND a.org_id = $1
  AND g.schedule_type = 'business_hours'
```

Every other digest table filters `org_id` directly. |
| **Proposed fix** | Migration: add nullable `org_id`, backfill from `clusters`/`rh_accounts`, `SET NOT NULL`, index `(org_id, cluster_uuid, schedule_type, interval_start)`. Stamp `org_id` on GPU ingest writes. Rewrite org prune to `DELETE WHERE org_id = $1 AND schedule_type = 'business_hours'`. |
| **Expected impact** | Index-only org prune; unblocks covering org-scoped GPU indexes. |
| **Risk** | Medium — partitioned table backfill; ingest write path must set the column. |
| **Effort** | M |

#### BH-NS-2PASS ([#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516)). Namespace recommend + write runs twice when BH digests exist

| Field | Value |
|-------|-------|
| **ID** | BH-NS-2PASS |
| **Severity** | P2 |
| **Location** | `internal/services/report_processor.go:505-550` — `RecommendAllNamespaces` then `RecommendBusinessHoursNamespaces` + second `WriteNamespaceRecommendations` |
| **Current state** | Container BH is computed at **API** time from sibling digests (no second persist of container recs). Namespace BH **persists** a second recommendation set per cycle. Two full digest loads, two compute passes, two batch writes. |
| **Quantification** | Namespace cardinality is typically tens–hundreds per cluster, not 100K. Absolute CPU is modest; the pattern is the risk if copied to container/node persist. |
| **Proposed fix** | Prefer the container pattern: persist all-hours only; compute BH namespace sizing on detail/list enrichment from `daily_namespace_digests` `business_hours`. If persist is required for history, fuse both streams in one load (two `schedule_type` queries in one function, one write batch). |
| **Expected impact** | ~2× namespace recommend+write wall time on BH clusters today; fuse or API-time BH removes the extra pass. |
| **Risk** | Medium if switching to API-time — history/quality for BH namespace rows must keep a home. |
| **Effort** | M |

#### BH-DETAIL-DUP ([#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517)). Node (and VM) detail fetches BH digests twice when Visual Insights is on

| Field | Value |
|-------|-------|
| **ID** | BH-DETAIL-DUP |
| **Severity** | P2 |
| **Location** | `internal/api/handlers_node_detail.go:198-214`; `internal/engine/recommend_node_business_hours.go:53-59` (`nodeBHDigestWindow` MAX query + `QueryNodeDigestsForNodeBySchedule`). VM detail has the same split (enrich + `QueryDailyVMDigestsForVMBySchedule`). |
| **Current state** | `EnrichNodeDetailWithBusinessHours` runs `SELECT MAX(bucket_date)` then a range select of BH rows to size. With Visual Insights, the handler **again** calls `queryNodeDailyDigests(…, "business_hours")` for the chart payload. Overlapping reads of the same partition key. |
| **Quantification** | 1 extra MAX + 1 extra range query per node-detail request when both BH and Visual Insights are enabled. Not on the list hot path. |
| **Proposed fix** | Return raw digests from enrich (or share one query) and attach `DailyDigestsBusinessHours` without a second round-trip. Collapse MAX+range into `WHERE bucket_date >= (SELECT MAX(...) - window)` in one statement. |
| **Expected impact** | 1–2 fewer round-trips per detail page (~single-digit to tens of ms on local PG; more on RDS). |
| **Risk** | Low. |
| **Effort** | S |

#### BENCH-GAP ([#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518)). No production-path 100K re-benchmark since librobne + dual-stream BH

| Field | Value |
|-------|-------|
| **ID** | BENCH-GAP |
| **Severity** | P2 (process / confidence, not a measured stall) |
| **Location** | July audit appendix vs `docs/performance/librobne-baseline-841639f3/README.md` |
| **Current state** | July quoted **14,700 containers/sec ingest** and **60,000/sec recommend** on a live-like 100K run. Extract baseline `cmd/bench` 100k is **38.5 s recommend / 4.3 GB Sys** — **different harness** (all-in-memory, 3M digest rows, `MemStats.Sys`). 10k extract gate passed; 100k production-path + BH dual-write has not been repeated. |
| **Proposed fix** | Re-run the scale-benchmark skill (streaming ingest + recommend, BH on and off) on phase17. Record next to `librobne-baseline-841639f3/`. Do not mix `cmd/bench` Sys with RSS. |
| **Expected impact** | Restores a number operators can compare to July headroom (200× vs ~70 containers/sec SaaS target). |
| **Risk** | None (measurement only). |
| **Effort** | M |

### P3 — Low

#### LIBROBNE-DAY-1 ([#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519)). `computeDayWeighted` allocates seven slices per container-day (CLI path)

| Field | Value |
|-------|-------|
| **ID** | LIBROBNE-DAY-1 |
| **Severity** | P3 for processor (not on that path); **P2 if robne CLI is used on 100K CSVs** |
| **Location** | `librobne/csv/digest.go:169-177` |
| **Current state** | Processor ingest still computes digests in `internal/ingestion/digest.go` with pooled buffers. `DailyDigests` / `DailyDigestsWeighted` (robne CLI, tests) allocate `cpuReq`, `cpuUse`, `cpuThr`, `memReq`, `memUse`, `memRss`, `weights` per day with no pool. `computePodCounts` / `computeReplicaCounts` build per-hour maps from nil. |
| **Quantification** | CLI: 100K containers × 30 days × 7 slices ≈ 21M short-lived allocations per `robne recommend` on a full month CSV. Processor unaffected. |
| **Proposed fix** | Port `fieldBufferPool` / hour-map scratch from `internal/ingestion/digest.go` into `librobne/csv`. Pre-size `make([]int64, 0, 24)` for hour maps. |
| **Risk** | Medium — pool reset bugs (same as DIGEST-1). |
| **Effort** | M |

#### NODE-CLASSALLOC ([#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520)). `cpuMeans` / `imbalances` grown from nil (carry-forward)

| Field | Value |
|-------|-------|
| **ID** | NODE-CLASSALLOC |
| **Severity** | P3 |
| **Location** | `librobne/node/recommend.go:192-193` |
| **Current state** | Still `var cpuMeans []float64`; append per day. 100 nodes × 3 terms × ~90 days. |
| **Proposed fix** | `make([]float64, 0, len(days))`. |
| **Effort** | S |

#### PGDIGEST-NCAP ([#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520)). librobne `ReadNodeDigestsWithSchedule` result slice has no capacity hint

| Field | Value |
|-------|-------|
| **ID** | PGDIGEST-NCAP |
| **Severity** | P3 |
| **Location** | `librobne/pgdigest/read_node.go:46` `var out []node.DigestRow` |
| **Current state** | Processor node query in `internal/engine/node/recommend.go:86` uses `defaultNodeDigestCapacity = 512`. CLI/`pgdigest` path does not. |
| **Proposed fix** | `make([]node.DigestRow, 0, 512)` (or 2× after BH if loading both streams). |
| **Effort** | S |

#### SAVINGS-JOIN ([#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445)). Redundant `rh_accounts` join on fleet savings final SELECT

| Field | Value |
|-------|-------|
| **ID** | SAVINGS-JOIN |
| **Severity** | P3 |
| **Location** | `internal/api/handlers_savings_summary.go:534-536` |
| **Current state** | Upstream CTEs already filter `org_id = $1`. Final SELECT `LEFT JOIN rh_accounts a ON a.id = c.tenant_id AND a.org_id = $1` contributes no output columns (alias comes from `clusters`). Mitigated by savings-summary LRU. |
| **Proposed fix** | Drop the `rh_accounts` join; keep `LEFT JOIN clusters` for `cluster_alias`. |
| **Effort** | S |

#### MERGE-ALLOC ([#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521)). `MergeNotificationCodes` is O(n²) + extra copy (unused on hot path)

| Field | Value |
|-------|-------|
| **ID** | MERGE-ALLOC |
| **Severity** | P3 |
| **Location** | `librobne/types/notifications_merge.go:16-40` |
| **Current state** | Nested `AppendUnique` + `SortedNotificationCodes` copies. Production recommend uses `AppendUnique` only; `MergeNotificationCodes` is aliased and tested, not called from ingest/recommend. Fine until BH code lists are merged in a tight loop. |
| **Proposed fix** | Pre-size `out`, sort in place, or a 128-bit/map set if call sites appear on the 100K path. |
| **Effort** | S |

#### BUILD-CGO-COMMENT ([#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522)). Dockerfile comment disagrees with the build line

| Field | Value |
|-------|-------|
| **ID** | BUILD-CGO-COMMENT |
| **Severity** | P3 (docs + build line). **Not** a request to keep `CGO_ENABLED=0` on the processor image. |
| **Location** | `Dockerfile:5-6` |
| **Current state** | Comment and RUN line both say `CGO_ENABLED=0` (July BUILD-CGO). `confluent-kafka-go` needs CGO. robne CLI correctly uses `CGO_ENABLED=0`. |
| **Proposed fix** | Processor image: `CGO_ENABLED=1` for librdkafka; FIPS downstream also CGO=1; CLI is static. Update the comment to match. |
| **Effort** | S |

#### BUILD-PGO / BUILD-GOTA / COMPAT-SIZE / API-N6

Carry-forwards from July. Triggers unchanged: CI profile collection ([#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372)); Kruize removal ([#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373)); caller migration ([#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513)); quality-endpoint rewrite (GORM COUNT+OFFSET, [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523)); namespace-history GORM remainder ([#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375)).

---

## Deferred Items — Revisit Triggers

| ID | Item | Trigger | Met? | Assessment |
|----|------|---------|------|------------|
| **S1** | Unified windowed digest recommender | “6th recommendation type” | **Met in spirit** (VM, snapshot, quota, cluster-quota, timeslicing exist) | Still defer: digest shapes differ (hourly GPU vs daily container vs inventory snapshot). Unifying would be a large accuracy+schema project, not a quick win. |
| **S2** | Parallel container recommend by namespace | Recommend phase >30s **in production** | **Not evidenced** | July live-like 100K was 1.7s. `cmd/bench` 100k is 38s — different harness. Revisit after BENCH-GAP. |
| **S3** | Namespace recs from container rollups | Product rollup spec | **No** | Unchanged. BH-NS-2PASS is the nearer namespace cost. |
| **G-3** | Distributed debouncer | Multiple processor pods | **No** | Typical on-prem remains single processor. ADR-0318 still the path. |
| **B-3** | String interning for `DigestKey` | Dup strings in heap profile | **No** | No new 100K RSS profile. |
| **PERF-09** | Rate limiter mutex → sharded | p99 >500 req/s | **No** | |
| **VM-2** | VM hourly int64 migration | VM volume >5000 | **No** | Dual-stream BH increases VM digest rows ~2×; still not 5k VMs. |
| **PERF-12** | Conditional `fleet_reduction` CTE | Heatmap p95 >500ms | **No** | |
| **CLI-LOAD** | Bound tar/gzip/row size in `csv.Load` | v13 finding 6, accepted | **No** | CLI trust model. Revisit if robne is pointed at multi-GB operator dumps. |

---

## Accuracy Trade-off Register

| Trade-off | Introduced | Still valid? | Notes |
|-----------|------------|--------------|-------|
| Decay weight lookup quantization (~0.2% error) | P0-1 / ADR-0288 | ✅ | `DecayWeight` still rounds age to hours then table-looks up integer half-lives (`librobne/types/decay.go:25-35`). Node/PVC inner-loop `DecayWeight` calls **do** hit the table when half-life is integer. |
| Idle P95 → max-of-daily-P95 | P2-5 | ✅ | |
| Percentile-band plots | ADR-0292 | ✅ | Peak hours Visual Insights is a **second digest stream**, not a coarser percentile. |
| Sample tables dropped | 000172 | ✅ | |
| Slim list contract | ADR-0294 | ✅ | BH omitted from unfiltered namespace list (#497). |
| Savings integer micro-cents | ADR-0291 | ✅ | |
| VM float64 sizing | v3 ALG-N2 | ✅ | |
| Weighted percentile float64 accumulation | v1 | ✅ | |
| Calendar-accurate monthly hours | ADR-0326 | ✅ | |
| Currency conversion float64 multiply | Phase16 | ✅ | |
| Dual-stream BH (office window vs 24h) | 000182–000185 | ✅ **New.** BH sizing **intentionally** excludes off-hours. Codes 79–82 warn when Peak-unsafe. Storage ~2× digest rows on enabled clusters. |
| Overnight BH wall-clock minutes | #507 | ✅ | DST gap/overlap documented; not a performance trade-off. |

---

## ROI-Ordered Implementation Roadmap

### Quick wins (S)

| Rank | ID | Title |
|------|-----|--------|
| 1 | **BH-IDX-NODE** [#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514) | Index `(org_id, cluster_uuid, schedule_type, bucket_date, node)` |
| 2 | **BH-IDX-GPU** [#515](https://github.com/pgarciaq/ros-ocp-backend/issues/515) | Index `(cluster_uuid, schedule_type, interval_start)` |
| 3 | **BH-DETAIL-DUP** [#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517) | Reuse BH digests on node/VM detail + Visual Insights |
| 4 | **SAVINGS-JOIN** [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445) | Drop unused `rh_accounts` join |
| 5 | **NODE-CLASSALLOC** / **PGDIGEST-NCAP** [#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520) | Capacity hints |
| 6 | **BUILD-CGO-COMMENT** [#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522) | Restore processor `CGO_ENABLED=1` and fix the comment |

### High-value (M)

| Rank | ID | Title |
|------|-----|--------|
| 7 | **GPU-ORG-1** [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512) | Add `org_id` to `gpu_container_digests` + prune rewrite |
| 8 | **BENCH-GAP** [#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518) | Re-run streaming 100K with BH on/off |
| 9 | **BH-NS-2PASS** [#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516) | Fuse or API-time namespace BH |
| 10 | **LIBROBNE-DAY-1** [#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519) | Pool CLI `computeDayWeighted` if robne is a scale tool |
| 11 | **BUILD-PGO** [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372) | Still gated on CI profiles |
| 12 | **API-N6** [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523) | Quality endpoints off GORM COUNT+OFFSET |

### Defer

COMPAT-SIZE ([#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513)), BUILD-GOTA ([#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373)), S1–S3, MERGE-ALLOC until a hot caller exists ([#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521)), CLI tar bounds (accepted).

---

## Appendix: Call Count Estimates

### Processor reconciliation (100K containers, 30-day lookback, BH **off**)

Unchanged shape from July if BH schedules are disabled:

| Phase | Operations | Notes |
|-------|------------|-------|
| CSV parse | 1 stream / file, `ReuseRecord` | librobne `ForEachRow` |
| Digest compute | pooled CV + field buffers | `internal/ingestion/digest.go` |
| Load digests | 1 query → ~1.8–3.0M rows | `pgdigest.ForEachAllHours`; cap `ROS_MAX_DIGEST_ROWS_PER_CLUSTER` |
| Recommend | batch 500 | `librobne/engine.RecommendWorkloads` |
| Write | `pgx.Batch` | `pgrec` |

### Same cluster, BH **on** (new)

| Phase | Extra work |
|-------|------------|
| Ingest | Same CSV pass; `groupedBH` + node/GPU BH accumulators; ~1.3–2× digest UPSERTs |
| Container recommend | **No** second persist (API-time BH) |
| Namespace recommend | **Second** load+compute+write (BH-NS-2PASS) |
| Node/GPU/VM recommend | Extra `schedule_type='business_hours'` digest read if BH sizing persisted or CLI Path A; detail enrichment as BH-DETAIL-DUP |
| Storage | ~2× rows in `daily_node_digests` / `gpu_container_digests` / `daily_vm_digests` / container+namespace digest tables for scheduled entities |

### robne CLI (`csv.Load` + `DailyDigests`)

| Phase | Cost |
|-------|------|
| Load | Materializes every entity slice from the tarball (v13 finding 6, accepted) |
| Digest | LIBROBNE-DAY-1 unpooled 7-slice days |
| Recommend | Same librobne engine as processor (good) |

### Throughput (last **measured**)

| Source | Ingest | Recommend | Memory | List |
|--------|--------|-----------|--------|------|
| July 2026 live-like 100K | 14,700 c/s | 60,000 c/s | ~600 MB RSS | p95 ~12 ms / 100 items |
| Extract `cmd/bench` 10k (phase17 P4) | n/a (in-memory) | 4.26 s / 10k | 366 MB Sys | — |
| Extract `cmd/bench` 100k | n/a | 38.5 s / 100k | 4.3 GB Sys | p50 590 ms (harness) |

**Do not treat the last two rows as a regression of the first.** Re-measure (BENCH-GAP).

---

## Summary

| Severity | Findings | Notes |
|----------|----------|-------|
| P0 | 0 | |
| P1 | 0 | |
| P2 | 6 | BH-IDX-NODE [#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514), BH-IDX-GPU [#515](https://github.com/pgarciaq/ros-ocp-backend/issues/515), GPU-ORG-1 [#512](https://github.com/pgarciaq/ros-ocp-backend/issues/512), BH-NS-2PASS [#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516), BH-DETAIL-DUP [#517](https://github.com/pgarciaq/ros-ocp-backend/issues/517), BENCH-GAP [#518](https://github.com/pgarciaq/ros-ocp-backend/issues/518) |
| P3 | 7 | LIBROBNE-DAY-1 [#519](https://github.com/pgarciaq/ros-ocp-backend/issues/519), NODE-CLASSALLOC / PGDIGEST-NCAP [#520](https://github.com/pgarciaq/ros-ocp-backend/issues/520), SAVINGS-JOIN [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445), MERGE-ALLOC [#521](https://github.com/pgarciaq/ros-ocp-backend/issues/521), BUILD-CGO-COMMENT [#522](https://github.com/pgarciaq/ros-ocp-backend/issues/522), plus July carry-forwards [#372](https://github.com/pgarciaq/ros-ocp-backend/issues/372) [#373](https://github.com/pgarciaq/ros-ocp-backend/issues/373) [#513](https://github.com/pgarciaq/ros-ocp-backend/issues/513) [#375](https://github.com/pgarciaq/ros-ocp-backend/issues/375) [#523](https://github.com/pgarciaq/ros-ocp-backend/issues/523) |
| **Regressions vs July Do-Not-Regress** | **0** on the integer ingest→recommend→write plane | July BUILD-CGO=0 was an incorrect checklist item |

**Assessment:** Phase17 is a large **structural** change (librobne + dual-stream BH) with a **small** compute delta on the 10k gate. The integer data plane, processor digest pools, batch writes, and key-table list path are intact. Spend the next performance cycle on **BH read indexes**, **GPU `org_id`**, **detail query reuse**, and a **streaming 100K + BH** benchmark — not on another extract or on disabling CGO for the processor image.
