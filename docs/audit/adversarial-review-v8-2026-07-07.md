# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date
Version: 8.0 | Date: 2026-07-07 | Reviewer: AI-assisted (incremental)

**Previous review:** v7.0 (2026-07-05) — findings #102–#111, all resolved or accepted  
**Scope:** 42 commits since `0dac0db`, covering: performance audit v3 quick wins (13 items), PROF-2/3 GORM→pgx migration, DIGEST-1 sync.Pool, PERF-01 quota_id, DB-002 partition DROP retention, DB-003/DB-008/DB-009 autovacuum tuning, REPLICA-1/DIGEST-2 integer math, pprof endpoints, FedRAMP documentation, CSV sanitization.

---

## Executive Summary

The 42-commit window represents a major performance and hardening sprint. The highest-impact changes (PROF-2, DIGEST-1, DB-002, PERF-01) are architecturally sound and correctly implemented. However, the review identified **2 High-severity findings** and **7 Medium-severity findings** that require attention:

- **#112 (HIGH)**: `category`, `category_cpu`, `category_memory` columns are populated by the engine but never included in the API SELECT statements. Every API response returns empty strings for these fields despite filtering working correctly. This is a functional regression from commit `04d62001`.
- **#113 (HIGH)**: `SweepPartitionedTables` issues `DROP TABLE IF EXISTS` without `lock_timeout`, creating a potential 25-second lock convoy when the daily retention sweep conflicts with active queries. The identifier is also unquoted.

No **Critical** findings. No cross-org data leakage. No SQL injection (parameterized queries used throughout). Authentication and RBAC remain solid. The integer math optimizations (DIGEST-2, REPLICA-1) are provably correct.

---

## Scorecard

| Dimension | Rating | Key gap (since v7.0) |
|-----------|--------|----------------------|
| Security | ★★★★★ | pprof exposure in warn mode (#115) — low real-world risk in containerized deployment |
| Correctness | ★★★☆☆ | Category fields never fetched (#112); namespace fallback LIMIT 500 (#118) |
| Auditability | ★★★★☆ | DEBUG_SAVINGS log noise in hot path (#114) |
| Operational robustness | ★★★★☆ | Partition DROP lock convoy (#113); missing statement timeouts on 2 handlers (#117) |
| Performance | ★★★★★ | Major improvements (PROF-2, DIGEST-1, PROF-3); GPU label cardinality risk (#119) |
| Design quality | ★★★★☆ | GORM fallback inconsistency (#116); pprof dual registration (#115) |
| Maintainability | ★★★★★ | Column-alignment tests, good test coverage overall |
| Governance | ★★★★★ | CHANGELOG discipline, GitHub issue cross-references; ADR gap (#126) |

---

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 112 | Category fields missing from API SELECT statements | High | Correctness | **Resolved** (ARV-1, [#237](https://github.com/pgarciaq/ros-ocp-backend/issues/237)) |
| 113 | Partition DROP: no lock_timeout + unquoted identifier | High | Operational/Security | **Resolved** (ARV-2, [#238](https://github.com/pgarciaq/ros-ocp-backend/issues/238)) |
| 114 | DEBUG_SAVINGS log statements in hot API path | Medium | Performance/Security | **Resolved** (ARV-3, [#239](https://github.com/pgarciaq/ros-ocp-backend/issues/239)) |
| 115 | pprof: no auth, warn-not-fatal, DoS vector, dual registration | Medium | Security/Operational | **Resolved** (ARV-4, [#240](https://github.com/pgarciaq/ros-ocp-backend/issues/240)) |
| 116 | PROF-2 fallback path still uses GORM `.Find()` reflection | Medium | Design/Correctness | **Resolved** (ARV-5, [#241](https://github.com/pgarciaq/ros-ocp-backend/issues/241)) |
| 117 | Quota trend / OOM timeline missing `WithHeavyStatementTimeout` | Medium | Operational | **Resolved** (ARV-6, [#242](https://github.com/pgarciaq/ros-ocp-backend/issues/242)) |
| 118 | `getNativeNamespaceByIDFallback` LIMIT 500 with no TOCTOU retry | Medium | Correctness | **Resolved** (ARV-7, [#243](https://github.com/pgarciaq/ros-ocp-backend/issues/243)) |
| 119 | GPU `model_name` label cardinality unbounded on unrecognized models | Medium | Performance | **Resolved** (ARV-8, [#244](https://github.com/pgarciaq/ros-ocp-backend/issues/244)) |
| 120 | Fleet heatmap LRU: entry-count cap only, no memory cap | Medium | Performance | **Resolved** (ARV-9, [#245](https://github.com/pgarciaq/ros-ocp-backend/issues/245)) |
| 121 | sync.Pool `cvScratch.spareInner` grows uncapped between GC cycles | Low | Performance | **Resolved** (ARV-10, [#246](https://github.com/pgarciaq/ros-ocp-backend/issues/246)) |
| 122 | Autovacuum 0.05/0.02 too aggressive for INSERT-only tables; new partitions don't inherit | Low | Operational | **Resolved** (ARV-11, [#247](https://github.com/pgarciaq/ros-ocp-backend/issues/247)) |
| 123 | `sanitizeCSVRow` in-place mutation | Informational | Correctness | **Accepted** (ARV-12 — all call sites pass fresh `[]string{...}` literals) |
| 124 | No compile-time column count guard for positional scan | Informational | Design | **Resolved** (ARV-13, [#249](https://github.com/pgarciaq/ros-ocp-backend/issues/249)) |
| 125 | `computeVariation` negative half-integer rounding untested | Informational | Test Coverage | **Resolved** (ARV-14, [#250](https://github.com/pgarciaq/ros-ocp-backend/issues/250)) |
| 126 | "Won't Fix" / "Deferred" decisions not recorded in `docs/adr/` | Informational | Governance | **Resolved** (ARV-15, [#251](https://github.com/pgarciaq/ros-ocp-backend/issues/251)) |
| 127 | CSV helper function split with inconsistent naming | Informational | Maintainability | **Resolved** (ARV-16, [#252](https://github.com/pgarciaq/ros-ocp-backend/issues/252)) |
| 128 | Statement timeout `SET` in AfterConnect: layered-trust hazard | Informational | Design | **Resolved** (ARV-17, [#253](https://github.com/pgarciaq/ros-ocp-backend/issues/253)) |

---

## Findings Detail

### #112 — Category Fields Missing from API SELECT Statements

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Dimension** | Correctness |
| **Location** | `internal/model/recommendation_set_native.go:732-765` (`nativeDetailSelect`), `internal/model/namespace_recommendation_set_native.go:128-147` (`nativeNSSelect`) |
| **Description** | Migration `dab531cf` added `category`, `category_cpu`, `category_memory` to the database. Commit `04d62001` added struct fields with GORM tags and JSON annotations, and `assembleNativeResults` reads them. But these columns are **not present** in `nativeDetailSelect` or `nativeNSSelect`. The engine writes them (`rec.CategoryCPU = ClassifyResource(...)`) so data exists in the DB — it's simply never fetched. |
| **Risk** | Every container and namespace API response returns `""` for category fields. The `filter[category]=...` WHERE clause works (it references the DB column directly), but the response never shows the category value. This is a silent functional regression. |
| **Recommendation** | Add `rs.category, rs.category_cpu, rs.category_memory` to `nativeDetailSelect` (→ 82 columns) and corresponding scan targets. Add `ns.category_cpu, ns.category_memory` to `nativeNSSelect` (→ 55 columns). Update column-alignment tests. |
| **Effort** | S |

---

### #113 — Partition DROP: No lock_timeout + Unquoted Identifier

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Dimension** | Operational / Security |
| **Location** | `internal/engine/retention.go:284` |
| **Description** | `DROP TABLE IF EXISTS` requires `AccessExclusiveLock`. Without `lock_timeout`, if a concurrent read holds `AccessShareLock` on the partition, the DROP queues indefinitely (up to the 25s session-level `statement_timeout`). During this wait, all subsequent queries to the parent table queue behind the lock request — a lock convoy. Additionally, the partition name is interpolated via `fmt.Sprintf("%s", part)` without identifier quoting. |
| **Risk** | The daily retention sweep on `hourly_node_digests` or `hourly_vm_digests` can cause a 25-second API unavailability window for node/VM hourly endpoints on busy systems. The unquoted identifier is not exploitable today (source is `pg_class.relname`) but violates secure SQL construction. |
| **Recommendation** | (1) Wrap in a transaction with `SET LOCAL lock_timeout = '2s'`; treat timeout errors as non-fatal (retry next sweep). (2) Use `pgx.Identifier{part}.Sanitize()` for the table name. |
| **Effort** | S |

---

### #114 — DEBUG_SAVINGS Log Statements in Hot API Path

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance / Data Leakage |
| **Location** | `internal/model/recommendation_set_native.go:912-914` |
| **Description** | Two `logrus.Infof("DEBUG_SAVINGS: ...")` calls execute for every container recommendation in `assembleNativeResults`. On large tenants (hundreds of namespaces × containers × terms), every list API call logs thousands of lines containing financial data (`savings_cents`). |
| **Risk** | Log volume explosion (millions of lines/hour in multi-tenant). Financial data in logs may violate data residency or audit requirements. Measurable I/O overhead in the hot API path. |
| **Recommendation** | Remove both lines. If savings debugging is needed, use `logrus.Debugf` (gated by log level) or a feature flag. |
| **Effort** | S |

---

### #115 — pprof: No Auth, Warn-Not-Fatal, DoS Vector, Dual Registration

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Security / Operational |
| **Location** | `internal/api/server.go:147-154`, `internal/utils/utils.go:282-289`, `internal/config/security.go:129-135` |
| **Description** | When `ROS_ENABLE_PPROF=true`: (1) Five pprof handlers register on the metrics port with no authentication. (2) On-prem default enforcement is "Warn" — a log line is emitted but endpoints start. (3) `/debug/pprof/profile?seconds=30` is a CPU-burning DoS vector reachable by any pod in the namespace. (4) `/debug/pprof/cmdline` exposes the full process argument list. (5) `server.go` registers 6 routes; `utils.go` registers only 5 (missing `/:name`). |
| **Risk** | In-cluster lateral movement from a compromised pod can enumerate process state, degrade CPU, or map memory layout. `ROS_ENABLE_PPROF` is also undocumented in `docs/operations/configuration.md`. |
| **Recommendation** | (1) Bind pprof to `127.0.0.1` only or add a static token gate. (2) Remove `pprof.Cmdline`. (3) Extract shared registration helper to fix the 5-vs-6 asymmetry. (4) Document in ops reference. (5) Consider upgrading to Fatal enforcement outside DEVELOPMENT mode. |
| **Effort** | M |

---

### #116 — PROF-2 Fallback Path Still Uses GORM `.Find()` Reflection

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Design / Correctness |
| **Location** | `internal/model/namespace_recommendation_set_native.go:349,375` |
| **Description** | The PROF-2 commit claims "Replaced GORM `.Find()` in all 6 list/detail functions." But `getNativeNamespaceByIDFallback()` still calls GORM `.Find()` twice — the expensive `reflect.New` + `scanIntoStruct` path. This fallback triggers whenever `namespace_id IS NULL` (pre-migration rows). |
| **Risk** | The column-alignment test only covers the primary positional path. Schema changes could break the fallback silently. The commit message creates false confidence about GORM removal scope. |
| **Recommendation** | Convert fallback to `.Rows()` + `scanNativeNamespaceRowsNoSort()`. Or add explicit comment documenting the GORM usage and exclude it from the PROF-2 coverage claim. |
| **Effort** | S |

---

### #117 — Quota Trend / OOM Timeline Missing `WithHeavyStatementTimeout`

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Operational |
| **Location** | `internal/api/handlers_quota_trend.go:67`, `internal/api/handlers_oom_timeline.go:67` |
| **Description** | Six heavy handlers were upgraded to `WithHeavyStatementTimeout` (5s) in DB-001. These two new time-series handlers were omitted. They query `daily_namespace_quota_digests` and `daily_container_digests` — tables that grow proportionally with cluster uptime — using only the 25s session-level default. |
| **Risk** | A planner regression on these tables under load could hold connections for the full 25s, risking pool exhaustion. The 90-day date cap reduces scan breadth but doesn't cap query duration. |
| **Recommendation** | Wrap in `db.WithHeavyStatementTimeout()` following the pattern in `handlers_node_hourly.go:120`. |
| **Effort** | S |

---

### #118 — `getNativeNamespaceByIDFallback` LIMIT 500 With No TOCTOU Retry

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `internal/model/namespace_recommendation_set_native.go:330-384` |
| **Description** | The fallback fetches `DISTINCT (cluster_uuid, namespace_name)` with `LIMIT 500`, then linearly scans computing `NativeNamespaceID()` per row. For orgs with >500 distinct namespace+cluster pairs, namespaces beyond the 500th silently return 404. No TOCTOU retry (unlike the equivalent fix in PERF-01's `ResolveQuotaKeyByID`). |
| **Risk** | Large managed clusters (50+ clusters × 20 namespaces = 1000) will get silent 404s for valid namespace recommendations. The fallback fires for pre-migration rows. |
| **Recommendation** | Add TOCTOU retry (same pattern as `quota_trend.go:88-99`). Remove or raise the LIMIT, or scope to `WHERE namespace_id IS NULL` only. |
| **Effort** | S |

---

### #119 — GPU `model_name` Label Cardinality Unbounded

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance |
| **Location** | `internal/engine/gpu_metadata.go:72-75, 148-155` |
| **Description** | `rosocp_gpu_model_unrecognized_total` uses raw (truncated-to-64-bytes) GPU model strings as label values. Custom GPU names, misconfigured DCGM exporters, or firmware version suffixes each create a new Prometheus time series that is never GC'd. |
| **Risk** | Gradual memory growth leading to OOM in long-running deployments. Prometheus scrape degradation from large `/metrics` responses. |
| **Recommendation** | Use a single `"unknown"` label value (the normalized catalog key is already computed by `matchGPUModelKey`). Cap cardinality at O(1). |
| **Effort** | S |

---

### #120 — Fleet Heatmap LRU: Entry-Count Cap Only, No Memory Cap

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Performance |
| **Location** | `internal/fleetheatmap/cache.go:16-68` |
| **Description** | The heatmap LRU is bounded by entry count (default 256). Worst-case: 256 entries × 1000 nodes × ~200 bytes/node = ~50 MB. If `ROS_FLEET_SUMMARY_CACHE_CAPACITY` is raised (shared with fleet summary cache), memory can balloon. Each RBAC scope + metric + term + engine combination generates a separate entry. |
| **Risk** | Memory pressure / OOM when capacity config is raised or `FleetHeatmapMaxNodes` is set high. Not an issue at defaults. |
| **Recommendation** | Split the config from fleet summary cache. Add memory-aware eviction or document memory implications per `MaxNodes` setting. |
| **Effort** | M |

---

### #121 — sync.Pool `cvScratch.spareInner` Grows Uncapped Between GC Cycles

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/ingestion/digest.go:93-132` |
| **Description** | `spareInner []map[string]struct{}` accumulates cleared maps on each `reset()`. A single large-payload reconciliation (24+ hours) can grow `spareInner` to 24+ entries. These persist in the pool until GC evicts the pool item. `weightedDigestScratch.pairs` similarly upsizes permanently per pool item. |
| **Risk** | GC latency spikes during burst ingestion. Not an OOM risk (GC clears pool items), but contributes to heap pressure. |
| **Recommendation** | Cap `spareInner` at ~32 entries on `Put()`. Trim `pairs` if `cap > 4*len`. |
| **Effort** | S |

---

### #122 — Autovacuum Settings Too Aggressive for INSERT-Only Tables

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational |
| **Location** | `migrations/000168_autovacuum_high_write_tables.up.sql` |
| **Description** | `autovacuum_vacuum_scale_factor=0.05` on INSERT-only quality tables (`recommendation_quality`, etc.) causes needless I/O — there are no dead tuples from UPDATEs. `autovacuum_analyze_scale_factor=0.02` triggers ANALYZE after every reconcile cycle (~1000 inserts on a 50k-row partition). New quality partitions created at runtime don't inherit these settings. |
| **Risk** | Wasted I/O from unnecessary autovacuum activity. Maintenance drift on new partitions. |
| **Recommendation** | Raise vacuum scale factor to 0.10 for INSERT-only tables. Keep analyze at 0.05. Add reloptions to `EnsureQualityPartition`. |
| **Effort** | S |

---

### #123–#128 — Informational Findings

| # | Title | Location | Recommendation |
|---|-------|----------|----------------|
| 123 | `sanitizeCSVRow` mutates caller's slice in-place | `csv_sanitize.go:26-31` | Return a copy or add defensive comment |
| 124 | No compile-time column count guard for 68+ field scan | `native_pgx_scan.go`, `recommendation_set_native.go:733` | Add `TestNativeDetailSelectColumnCount` (count commas, no DB needed) |
| 125 | `computeVariation` negative half-integer rounding untested | `recommend_all_test.go:246-267` | Add test cases: `{current:3, rec:2, want:-33}`, `{current:6, rec:5, want:-17}` |
| 126 | "Won't Fix" decisions not in `docs/adr/` | `native-engine-audit-v3-2026-07.md` | Create stub ADRs with status "Rejected" for PROF-4, PERF-07, PERF-08 |
| 127 | CSV helper function split with inconsistent naming | `utils.go:1116`, `csv_helpers.go:14` | Consolidate into `csv_helpers.go` with single naming convention |
| 128 | `SET statement_timeout` in AfterConnect: layered-trust hazard | `internal/db/db.go:79-81` | Add lint rule blocking bare `SET statement_timeout` outside AfterConnect |

---

## Priority Remediation Order

| Priority | Finding | Severity | Effort | Rationale |
|----------|---------|----------|--------|-----------|
| 1 | **#112** — Category fields missing from SELECT | High | S | Silent functional regression; users see empty category in every response |
| 2 | **#113** — Partition DROP lock convoy + unquoted identifier | High | S | Production availability risk; 25s endpoint unavailability window |
| 3 | **#114** — DEBUG_SAVINGS in hot path | Medium | S | Remove 2 lines; eliminates log noise and data leakage |
| 4 | **#117** — Missing `WithHeavyStatementTimeout` on 2 handlers | Medium | S | Copy existing pattern; prevents pool exhaustion |
| 5 | **#119** — GPU label cardinality | Medium | S | Change label value to `"unknown"`; prevents OOM |
| 6 | **#118** — Namespace fallback LIMIT 500 | Medium | S | Silent 404s for large orgs |
| 7 | **#116** — GORM fallback in namespace detail | Medium | S | Convert to positional scan or document explicitly |
| 8 | **#115** — pprof hardening | Medium | M | Bind to localhost, remove Cmdline, consolidate registration |
| 9 | **#120** — Heatmap LRU memory cap | Medium | M | Split config, add memory-aware eviction |
| 10 | **#121** — sync.Pool cap on spareInner | Low | S | Add trim on Put() |
| 11 | **#122** — Autovacuum tuning for INSERT-only tables | Low | S | Raise vacuum scale factor; add reloptions to EnsurePartition |
| 12 | **#123–#128** — Informational items | Info | S each | Address opportunistically |

---

## Areas Confirmed Safe

The following were specifically examined and found to be correctly implemented:

| Area | Verdict | Notes |
|------|---------|-------|
| DIGEST-1 sync.Pool | ✅ Safe | `reset()` correctly clears maps; no stale data on reuse; type assertions safe (New always returns concrete type) |
| DIGEST-2 `computeVariation` | ✅ Safe | Integer overflow impossible at realistic values; division-by-zero guarded; rounding formula correct |
| REPLICA-1 integer ceiling | ✅ Safe | Overflow impossible (10^12 max at extreme inputs); division-by-zero doubly guarded by early returns |
| PERF-01 quota_id TOCTOU | ✅ Safe | At most 3 queries; no infinite loop; UUID v5 deterministic and correct |
| PROF-2 positional scan (primary path) | ✅ Safe | 79-column alignment test with unique sentinels provides strong regression guard |
| Statement timeout architecture | ✅ Safe | `SET LOCAL` in transactions; `AfterConnect` for session default; no sticky timeout risk |
| CSV sanitization | ✅ Safe | All 17+ `writer.Write` paths call `sanitizeCSVRow`; OWASP formula-injection prefixes covered |

---

## Current State

| Metric | Count |
|--------|-------|
| Total findings (this review) | 17 |
| High | 2 |
| Medium | 7 |
| Low | 2 |
| Informational | 6 |
| Resolved | 16 |
| Accepted | 1 |
| Open | 0 |

**Overall assessment:** All 17 findings have been addressed. 16 findings were resolved through code changes (ARV-1 through ARV-11, ARV-13 through ARV-17). ARV-12 (`sanitizeCSVRow` in-place mutation) was accepted as safe — all call sites pass fresh `[]string{...}` literals.
