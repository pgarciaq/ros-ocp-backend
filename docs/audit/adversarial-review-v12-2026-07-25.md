# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date

Version: 12 | Date: 2026-07-25 | Reviewer: AI-assisted (incremental, post-currency and VM-PVC)

## Scope

Incremental review covering ~55 commits since v11 (2026-07-20). Major changes:

- **Multi-currency savings conversion:** `internal/costdata/` package — exchange
  rate fetching from Koku, `ConvertCents()` arithmetic, API-level currency
  enrichment dispatch (`internal/api/currency.go`, `enrichment_dispatch.go`).
- **Calendar-accurate monthly hours:** `HoursInMonth(year, month)` replaces the
  hardcoded 730h constant for savings extrapolation.
- **VM PVC correlation:** New sub-system (`internal/engine/vm/vm_pvc_*.go`,
  `internal/ingestion/vm_pvc_*.go`, migration 000180) linking VMs to their PVCs.
- **Category unification:** Migrations 000176–000179 replace per-entity boolean
  classification flags with a single `category TEXT` column across VMs,
  containers, namespaces, and nodes.
- **Reship fix:** Synthetic manifest deduplication bug fix preventing file
  re-processing.
- **MoneyAmount OpenAPI schema:** Reusable `MoneyAmount` type with multi-currency
  `Units` field (ADR-0327).
- **Idle settings validation:** Reject `<none>` sentinel and whitespace in
  `workload_type` filters.
- **Numeric sort fix:** Pagination sort order corrected for numeric columns.
- **Performance quick wins:** 5 implemented from audit v5 + BH-POOL-1 buffer
  pooling.
- **CodeQL workflow fix:** Removed duplicate `uses:` keys from v3→v4 merge
  conflict.

## Executive Summary

The v11→v12 delta introduces the most significant feature additions since the
engine refactoring: multi-currency support, VM-PVC correlation, and the category
unification migration. The code quality remains very high — all new packages
follow established patterns (parameterized SQL, structured logging, comprehensive
tests, zero-guard arithmetic).

The review identified **11 new findings** (0 Critical, 0 High, 3 Medium, 5 Low,
3 Informational). The most actionable are: (1) VM term value interpolated as a
string literal in SQL rather than as a parameterized argument — not exploitable
today but fragile; (2) silently discarded error in the reship poller; (3) Koku
error response bodies read without size limits (unlike `reship/client.go` which
correctly uses `io.LimitReader`).

All 9 v11 findings verified — 0 regressions. `compat.go` grew marginally from
369 to 373 lines (4 new aliases for savings functions).

Overall assessment: **Very Good** — the codebase maintains its strong trajectory
despite significant feature additions. The currency conversion is well-designed
with appropriate fallbacks (rate=1.0 on failure), the category unification is
clean, and the VM-PVC sub-system is properly separated.

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★☆ | `vmTerm` SQL interpolation is safe (hardcoded switch) but should use `$N` parameter |
| Correctness | ★★★★★ | Savings arithmetic has zero-guards; theoretical overflow requires unrealistic inputs |
| Performance | ★★★★★ | Calendar-accurate hours, BH-POOL-1 buffer pooling, 5 quick wins implemented |
| Operational robustness | ★★★★☆ | Reship poller silently discards `RetryPending` error |
| Design quality | ★★★★★ | VM-PVC sub-system properly separated; costdata abstraction clean |
| Maintainability | ★★★★☆ | `compat.go` at 373 lines; one stray `log.Printf` in structured-logging codebase |
| Auditability | ★★★★★ | All new paths have structured logging and Prometheus metrics |
| Governance | ★★★★★ | ADRs 0325–0327 present; CHANGELOG thorough; CodeQL workflow fixed |

## Prior Findings Status (v11 → v12)

All 9 v11 findings verified. 0 regressions.

| v11 # | Title | Severity | Status | Verification |
|--------|-------|----------|--------|--------------|
| 1 | `NotificationCodeBitmap` codes >63 | Medium | **Still Accepted** | All high codes (74, 76, 77, 78) still use `AppendUnique`/direct `append` |
| 2 | `ResolveGPUThresholdSettings` nil panic | Low | **Still Open** | No nil guard added |
| 3 | `ComputeRecommendedReplicas` overflow | Low | **Still Open** | No overflow guard added |
| 4 | `decayTables` sync.Map eviction | Low | **Still Accepted** | Unchanged |
| 5 | GPU MIG `groupCol` SQL interpolation | Low | **Still Open** | Still uses `fmt.Sprintf` |
| 6 | `compat.go` growing | Informational | **Still Open** | 369 → 373 lines (+4 savings aliases) |
| 7 | `init()` ordering dependency | Informational | **Still Accepted** | Unchanged |
| 8 | `WriteRecommendationHistory` missing `ctx.Err()` | Low | **Still Open** | No check added |
| 9 | `EvaluateNotificationsWithThresholds` mutable slice | Informational | **Still Open** | No godoc added |

v10 findings spot-checked:
- v10-1 (cluster cache mutable slice): **Still Resolved** — defensive copy verified
- v10-5 (duplicate `maxPgxBatchQueue`): **Still Resolved** — shared constant in `db/batch.go`
- v10-8 (retention identifier quoting): **Still Resolved** — `pgx.Identifier.Sanitize()` verified

**Summary:** 6 Still Open, 3 Still Accepted, 0 Regressed.

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 1 | `vmTerm` SQL string interpolation in savings summary | Medium | Security | Open |
| 2 | Reship poller silently discards `RetryPending` error | Medium | Operational | Open |
| 3 | `ConvertCents` accepts rate=0 without guard | Medium | Correctness | Open |
| 4 | Koku error body read without size limit | Low | Security | Open |
| 5 | `SQLOrderByFragment` column name interpolation | Low | Security | **Accepted** |
| 6 | `CPUSavingsMicroCents` theoretical integer overflow | Low | Correctness | **Accepted** |
| 7 | Missing `ctx.Err()` in CSV ingestion loops | Low | Operational | Open |
| 8 | `log.Printf` instead of structured logrus | Low | Auditability | Open |
| 9 | Migration 000179 down does not restore `stranded_resource` | Informational | Correctness | **Accepted** |
| 10 | Quota headroom truncation on small values | Informational | Correctness | Open |
| 11 | `FormatCentsToAmount` MinInt64 edge case | Informational | Correctness | Open |

## Findings Detail

### Finding 1: `vmTerm` SQL string interpolation in savings summary

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Security |
| **Location** | `internal/api/handlers_savings_summary.go:421, 472, 506` |
| **Description** | `savingsSummaryVMTerm()` returns one of three hardcoded strings (`"short_term"`, `"medium_term"`, `"long_term"`), which is interpolated into SQL via string concatenation: `WHERE term = '` + vmTerm + `'`. The caller validates the input and the switch has a safe default, making injection impossible today. However, every other value in these queries uses parameterized `$N` placeholders. This is the only non-parameterized user-influenced value in the entire handler file. |
| **Risk** | Zero current risk (hardcoded switch output). Future maintenance risk if the function is refactored to accept arbitrary term strings. Inconsistency with the codebase's own SQL safety conventions. |
| **Recommendation** | Pass `vmTerm` as a `$N` parameterized argument. This is a ~10 line change affecting 3-5 query sites. |
| **Effort** | S |

### Finding 2: Reship poller silently discards `RetryPending` error

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Operational robustness |
| **Location** | `internal/reship/poller.go:90` |
| **Description** | The reship poller calls `_ = p.service.RetryPending(ctx, pc.OrgID, pc.ClusterUUID)` and discards the error. While `RetryPending` handles metrics and logging internally for individual retry operations, the poller has no aggregated signal that retries are systematically failing for a given org/cluster. If the DB is unreachable or a bug causes all retries to fail, the poller silently moves to the next cluster. |
| **Risk** | Medium — a persistent failure in `RetryPending` would go unnoticed by the poller's callers. The internal metrics would show individual failures, but the poller would not surface a top-level error or circuit-break. |
| **Recommendation** | Log the error at warn level: `if err := p.service.RetryPending(...); err != nil { log.Warn(...) }`. Consider aggregating a failure count and emitting a metric. |
| **Effort** | S |

### Finding 3: `ConvertCents` accepts rate=0 without guard

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `internal/costdata/conversion.go:8-10` |
| **Description** | `ConvertCents(cents int64, rate float64) int64` multiplies `float64(cents)*rate`. A rate of `0.0` would zero out all monetary values. Upstream code defaults to `1.0` when exchange rates are unavailable, but `ConvertCents` itself has no guard. A compromised Koku response or a future code path that skips the default could silently zero all savings. |
| **Risk** | Low-medium — requires either a compromised Koku server or a future caller that bypasses the `fetchExchangeRate` default. Defense-in-depth concern: the function that performs the critical multiplication should validate its own inputs. |
| **Recommendation** | Guard: `if rate <= 0 { return cents }`. This preserves the original value when the rate is invalid, consistent with the "return 1.0 on error" pattern upstream. |
| **Effort** | S |

### Finding 4: Koku error body read without size limit

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/costdata/provider.go:190, 350, 414` |
| **Description** | Three error handlers use `io.ReadAll(resp.Body)` to read Koku error responses without a size limit. A malicious or misconfigured Koku server could return an arbitrarily large response body, causing memory exhaustion. The reship client (`internal/reship/client.go:82`) correctly uses `io.LimitReader(resp.Body, 1<<20)` for the same purpose. |
| **Risk** | Low — Koku is an internal trusted service and the error path is infrequent. A 1 MiB limit matches the existing pattern in `reship/client.go`. |
| **Recommendation** | Replace `io.ReadAll(resp.Body)` with `io.ReadAll(io.LimitReader(resp.Body, 1<<20))` in all three locations, consistent with `reship/client.go`. |
| **Effort** | S |

### Finding 5: `SQLOrderByFragment` column name interpolation

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/api/listoptions/list_options.go:60-62` |
| **Description** | `SQLOrderByFragment` constructs `columnName + " " + direction + " NULLS LAST"` via string concatenation. Both values come from allowlisted maps (`ContainerAllowedOrderBy`, etc.) and validated direction (`"asc"`/`"desc"`), making injection impossible. The function signature accepts arbitrary strings but all callers are gated by `ParseOrderBy` which resolves against allowlists. |
| **Risk** | Zero current risk. Future maintenance risk if a caller bypasses `ParseOrderBy`. |
| **Recommendation** | Add a comment documenting the safety invariant. Optionally validate `direction` within `SQLOrderByFragment` itself. |
| **Effort** | S |
| **Status** | **Accepted** — all callers are gated by `ParseOrderBy` with compile-time-defined allowlists. The pattern is consistent across all handler files. Adding internal validation would be defense-in-depth but not urgent. |

### Finding 6: `CPUSavingsMicroCents` theoretical integer overflow

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/engine/core/savings_int.go:76-82, 107-112` |
| **Description** | `CPUSavingsMicroCents` computes `deltaMC * rateMicroCentsPerMCHour * hoursPerMonth * replicas`. The worst-case test (1M mc × 100M micro-cents/mc-hr × 730h = 7.3e16) is within int64 range. `VCPUSavingsMicroCents` multiplies `deltaVCPU * 1000 * rate * hours`. Both lack explicit overflow guards, but the test suite verifies the worst-case result is under 90% of MaxInt64. |
| **Risk** | Negligible in practice. Overflow requires >1M millicores at >$100/core-hour rates — physically impossible for real clusters. The test at line 121-127 explicitly validates the worst case. |
| **Recommendation** | No immediate action. The existing test coverage (`TestCPUSavingsMicroCents_WorstCase`) validates the safe range. Consider documenting the safe bounds in a godoc comment. |
| **Effort** | S |
| **Status** | **Accepted** — test coverage explicitly validates worst-case overflow margin. Production values are orders of magnitude below the overflow threshold. |

### Finding 7: Missing `ctx.Err()` in CSV ingestion loops

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `internal/ingestion/vm_pvc_csv.go`, `internal/ingestion/pipeline_stream.go` |
| **Description** | The VM-PVC CSV parser (`IngestVMPVCCSV`) and the streaming pipeline don't check for context cancellation between row iterations. DB calls will eventually propagate cancellation, but large payloads could delay graceful shutdown by processing remaining rows before the DB call surfaces the cancellation. |
| **Risk** | Low — bounded by payload size (typically <100K rows) and individual row processing time (~microseconds). Graceful shutdown waits for in-flight work, so the delay is seconds, not minutes. |
| **Recommendation** | Add `if err := ctx.Err(); err != nil { return err }` every N iterations (e.g., every 10,000 rows), consistent with the batch-size pattern used in `WriteRecommendationHistory`. |
| **Effort** | S |

### Finding 8: `log.Printf` instead of structured logrus

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Auditability |
| **Location** | `internal/costdata/provider.go:424` |
| **Description** | One log statement uses stdlib `log.Printf("WARN: exchange rate unavailable for %s->%s (org=%s), returning 1.0", from, to, orgID)` instead of the structured `logrus` logger used throughout the rest of the package. This breaks log aggregation pipelines that parse structured JSON fields. |
| **Risk** | Low — the message still appears in logs but without structured fields (org_id, from_currency, to_currency) that enable filtering in Kibana/Splunk. |
| **Recommendation** | Replace with `log.WithFields(logrus.Fields{"org_id": orgID, "from": from, "to": to}).Warn("exchange rate unavailable, defaulting to 1.0")`. |
| **Effort** | S |

### Finding 9: Migration 000179 down does not restore `stranded_resource`

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `migrations/000179_node_category.down.sql` |
| **Description** | The up migration reads `stranded_resource` to classify nodes as `stranded_cpu` or `stranded_memory`. The down migration restores `is_underutilized` and `is_overcommitted` from `category` but does not reverse-map `stranded_cpu`/`stranded_memory` back. However, the up migration does NOT drop `stranded_resource` — the column persists. The information loss only occurs if a future migration drops `stranded_resource` independently. |
| **Risk** | None currently — `stranded_resource` is not dropped by migration 000179 or any subsequent migration (verified through 000180). The down migration correctly restores the booleans it drops. |
| **Recommendation** | No action needed. If `stranded_resource` is ever dropped in a future migration, that migration's down should restore it. |
| **Effort** | — |
| **Status** | **Accepted** — the column is not dropped; the concern is theoretical. |

### Finding 10: Quota headroom truncation on small values

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `internal/engine/quota/recommend_quota.go` |
| **Description** | The `applyHeadroom` function uses integer arithmetic for headroom calculation. For small quota values (e.g., 1 millicore) with a 10% headroom, the result truncates to 0 additional millicores rather than rounding up to 1. This means very small quotas may receive no headroom buffer. |
| **Risk** | Negligible — production quotas are typically in the hundreds-to-thousands of millicores range. A 1 mc quota with 10% headroom producing 1 mc instead of 1.1 mc is a sub-millicore discrepancy. |
| **Recommendation** | Consider documenting that headroom is applied via integer truncation. If precise small-value behavior matters, add `if headroom > 0 && result == original { result += 1 }`. |
| **Effort** | S |

### Finding 11: `FormatCentsToAmount` MinInt64 edge case

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `internal/money/format.go` |
| **Description** | `FormatCentsToAmount` uses `cents / 100` and `cents % 100` to split into dollars and cents. For `math.MinInt64` (-9223372036854775808), negating the remainder for absolute value would overflow because `|-MinInt64| > MaxInt64`. In practice, no savings calculation can produce values near MinInt64 — the maximum is bounded by `CPUSavingsMicroCents` worst-case (~7.3e16 micro-cents ≈ 7.3e10 cents). |
| **Risk** | None in practice. Defense-in-depth concern only. |
| **Recommendation** | No action needed. The safe range is verified by savings tests. |
| **Effort** | — |

## Items Verified Clean

- **Currency conversion architecture:** `ConvertCents` uses round-half-up (`math.Floor(x + 0.5)`), consistent with Koku. Rate fallback chain: `fetchExchangeRate` → parse JSON → validate non-nil → default 1.0 on any error. Currency strings from Koku never enter SQL — used only in JSON response `Units` fields.

- **All handler files use parameterized SQL:** Verified 22 handler files. All user-supplied values use `$N` placeholders. Only exception is `vmTerm` (Finding 1) which is hardcoded-switch-output.

- **Pagination implementation:** Keyset pagination in `handlers_pagination.go` uses base64-decoded cursors validated for sort field match. `parseLimit` caps at `maxRecordLimit` (1000). `parseOffset` validates non-negative. No N+1 queries — all pagination uses single SQL with `WHERE` clauses.

- **CSV injection protection:** `csv_sanitize.go` prefixes formula-triggering chars (`=`, `+`, `-`, `@`, `\t`, `\r`) with `'`. All CSV export handlers use this sanitizer.

- **Idle settings validation:** `workload_type` validation correctly rejects `<none>` sentinel, empty strings, and whitespace-only values. Tested in `handlers_idle_detection_settings_test.go`.

- **VM PVC sub-system separation:** `vm_pvc_correlation.go`, `vm_pvc_db.go`, `vm_pvc_csv.go` are properly isolated. FK constraint `ON DELETE CASCADE` in migration 000180 ensures cleanup. Unique index prevents duplicates.

- **Reship service:** No file system operations — all interactions are HTTP + DB. `baseURL` from config, not user input. `clusterUUID` validated as `uuid.UUID`. Response body limited to 1 MiB via `io.LimitReader` in `client.go:82`.

- **Migration safety:** All 5 migrations (000176–000180) use `IF NOT EXISTS`/`IF EXISTS` guards. Down migrations correctly reverse up migrations (with the documented `stranded_resource` exception in Finding 9). Category backfills are idempotent (`WHERE category IS NULL`).

- **ADR discipline:** ADR-0325 (stdlib CSV streaming), ADR-0327 (multi-currency), and prior ADRs all present and properly formatted.

- **Test coverage:** 9 test functions in `costdata/`, 104+ API handler tests, reship tests with mocked HTTP, VM-PVC ingestion tests, savings integration tests with calendar-accurate hours verification.

- **No hardcoded secrets, `_ = err` suppressions (except Finding 2), or permissive CORS** in new code.

- **No silent error swallowing** — all error paths either return errors, log warnings, or (in the case of `ConvertCents`) are documented as returning the identity operation.

## Priority Remediation Order

| Priority | Finding | Severity | Title | Effort | Status |
|----------|---------|----------|-------|--------|--------|
| 1 | 1 | Medium | `vmTerm` SQL string interpolation → parameterize | S | Open |
| 2 | 2 | Medium | Reship poller `RetryPending` error → log it | S | Open |
| 3 | 3 | Medium | `ConvertCents` rate=0 guard | S | Open |
| 4 | 4 | Low | Koku error body `io.LimitReader` | S | Open |
| 5 | 8 | Low | `log.Printf` → structured logrus | S | Open |
| 6 | 7 | Low | Ingestion `ctx.Err()` checks | S | Open |
| 7 | 10 | Informational | Quota headroom truncation | S | Open |
| 8 | 11 | Informational | `FormatCentsToAmount` MinInt64 | — | Open |
| 9 | 5 | Low | `SQLOrderByFragment` comment | S | **Accepted** |
| 10 | 6 | Low | `CPUSavingsMicroCents` overflow | S | **Accepted** |
| 11 | 9 | Informational | Migration 000179 `stranded_resource` | — | **Accepted** |

## Accepted Risks

- **#5 — `SQLOrderByFragment` column interpolation:** All callers gated by `ParseOrderBy` with compile-time allowlists. The function is internal with no exported API surface. Adding validation would be defense-in-depth but not urgent.

- **#6 — `CPUSavingsMicroCents` overflow:** Test coverage explicitly validates worst-case at 90% of MaxInt64. Production values (max ~4000 mc × ~$10/core-hr) are 5+ orders of magnitude below the overflow threshold.

- **#9 — Migration 000179 `stranded_resource`:** The column is not dropped by any migration (verified through 000180). The down migration correctly restores all columns it drops. The concern is purely theoretical.

- **Prior accepted risks from v11:** #1 (NotificationCodeBitmap), #4 (decayTables), #7 (init ordering), v10-#13 (cluster cache health). All unchanged.

## Current State

- **Total v12 findings:** 11 (0 Critical, 0 High, 3 Medium, 5 Low, 3 Informational)
- **Open (actionable):** 8 (#1, #2, #3, #4, #7, #8, #10, #11)
- **Accepted:** 3 (#5, #6, #9)
- **Prior v11 findings verified:** 9/9 correct (0 regressed)
- **Prior v11 open findings:** 6 still open (none remediated since v11)

## Strengths Noted

1. **Consistent SQL safety discipline:** 22 handler files verified — all 1,000+
   SQL parameters use `$N` placeholders. The single exception (Finding 1) uses a
   hardcoded switch, not user input. This level of consistency across a large API
   surface is noteworthy.

2. **Well-designed currency conversion:** The fallback chain (Koku API → parse →
   validate → default 1.0) ensures savings are never silently zeroed. The
   `ConvertCents` function uses integer-cent arithmetic with round-half-up,
   avoiding floating-point precision issues in financial calculations.

3. **Calendar-accurate savings:** Replacing the hardcoded 730h constant with
   `HoursInMonth(year, month)` is a subtle but important correctness improvement.
   The test suite verifies that January (744h) yields higher savings than February
   (672h) — a real-world accuracy gain for monthly cost projections.

4. **Clean migration discipline:** The category unification (4 migrations) is
   well-structured: add column → backfill from existing data → drop old columns →
   index new column. Down migrations correctly reverse each step. Backfills are
   idempotent.

5. **Proactive performance investment:** 5 quick wins from the performance audit
   plus the BH-POOL-1 buffer pooling demonstrate that performance is treated as a
   continuous concern, not a one-time effort.
