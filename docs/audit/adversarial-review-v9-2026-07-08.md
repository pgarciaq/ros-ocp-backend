# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date

Version: 9 | Date: 2026-07-08 | Reviewer: AI-assisted (incremental, post-ARV remediation)

## Scope

Incremental review covering changes since v8 (2026-07-07). All 17 ARV findings
from v8 have been implemented and closed. This review verifies the fixes are
complete and checks for new issues introduced by the remediation work itself.

## Executive Summary

The ARV-1 through ARV-17 remediation was executed cleanly. The codebase is
materially improved: sync.Pool buffers are capped, autovacuum is tuned, the
statement timeout invariant is enforced by a lint test, CSV helpers are
deduplicated, and seven "Won't Fix" decisions are now recorded as ADRs.

However, the remediation work introduced one **High** severity issue: the
namespace fallback scan (ARV-7 fix) removed the LIMIT 500 guard but did not
add a compensating statement timeout, creating a DoS amplification vector for
tenants with many namespaces. Two **Medium** issues exist: a data race on the
CSV download HTTP client singleton, and missing ADR README entries. Nine **Low**
issues are minor robustness and documentation improvements.

Overall assessment: **Good** — the systematic remediation demonstrates strong
engineering discipline. The remaining issues are localized and straightforward
to fix.

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★☆ | DetermineCSVType contains-fallback exploitable with crafted filenames |
| Correctness | ★★★★☆ | Data race on csvDownloadHTTPClientSingleton lazy init |
| Auditability | ★★★★☆ | v8 document findings status not updated |
| Operational robustness | ★★★★☆ | Namespace fallback scan missing timeout; Echo pprof POST broken |
| Performance | ★★★★☆ | Weighted digest outer allocations unPooled; scratch slices uncapped |
| Design quality | ★★★★★ | Clean separation, consistent patterns |
| Maintainability | ★★★★☆ | optionalFloat32Str precision difference undocumented |
| Governance | ★★★★☆ | ADR index incomplete; ARV-12 missing from CHANGELOG |

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 1 | csvDownloadHTTPClientSingleton data race | Medium | Correctness | Open |
| 2 | Namespace fallback scan runs without statement timeout | High | Operational | Open |
| 3 | Statement timeout lint test brace tracking is brittle | Low | Maintainability | Open |
| 4 | scratch.counts and scratch.sorted uncapped in pool | Low | Performance | Open |
| 5 | Unquoted table name in ensureEntityQualityPartitions ALTER TABLE | Low | Security | Open |
| 6 | DetermineCSVType Contains fallback exploitable with crafted filenames | Low | Security | Open |
| 7 | ADR README index missing 0311–0317 | Medium | Governance | Open |
| 8 | ARV-12 silently dropped from CHANGELOG | Low | Governance | Open |
| 9 | v8 audit document findings status not updated | Low | Auditability | Open |
| 10 | optionalFloat32Str precision difference undocumented | Low | Maintainability | Open |
| 11 | Business-hours weighted digest path lacks pool coverage | Low | Performance | Open |
| 12 | Echo pprof Symbol endpoint rejects POST | Low | Operational | Open |

## Findings Detail

### Finding 1: csvDownloadHTTPClientSingleton data race

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `internal/api/utils.go` (csvDownloadHTTPClientSingleton lazy init) |
| **Description** | The CSV download HTTP client is lazily initialized without synchronization. Multiple goroutines hitting the CSV download endpoint concurrently during the first request can race on the nil check and initialization, potentially creating multiple clients or reading a partially-initialized pointer. |
| **Risk** | Under concurrent first-requests, the client may be initialized twice (wasted resources) or a goroutine may read a partially-written pointer (undefined behavior). Low probability in practice due to single-flight nature of CSV downloads. |
| **Recommendation** | Replace with `sync.Once`. |
| **Effort** | S |

### Finding 2: Namespace fallback scan runs without statement timeout

| Field | Value |
|-------|-------|
| **Severity** | High |
| **Dimension** | Operational robustness |
| **Location** | `internal/model/namespace_recommendation_set_native.go` — `getNativeNamespaceByIDFallback` |
| **Description** | The ARV-7 fix removed the LIMIT 500 guard and added a TOCTOU retry, but the fallback scan now runs an unbounded `SELECT DISTINCT` across the full tenant's recommendation_sets table with no statement timeout. For large tenants (~100k+ containers), this query can run for tens of seconds, holding a connection and consuming DB resources. |
| **Risk** | A tenant with many namespaces triggers the fallback path (cache miss or stale org_container_keys), the DISTINCT scan runs unbounded, consuming a pool connection and DB CPU. Multiple concurrent requests amplify into connection pool exhaustion. |
| **Recommendation** | Wrap the fallback query in `database.WithHeavyStatementTimeout` or `database.WithStatementTimeout` with a bounded duration (e.g., 10s). If it times out, return 503 with a Retry-After header rather than hanging. |
| **Effort** | S |

### Finding 3: Statement timeout lint test brace tracking is brittle

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `internal/db/statement_timeout_lint_test.go:62-66` |
| **Description** | The lint test tracks the `setStatementTimeout` function body using a simple "first `}` at column 0 exits the exempt region" heuristic. If the function is ever refactored to contain nested braces (e.g., an if-else block), the exempt region would end prematurely, causing false positives. |
| **Risk** | False positive lint failures after refactoring `setStatementTimeout`. Developer frustration and potential `//nolint` suppression that defeats the purpose. |
| **Recommendation** | Track brace depth (increment on `{`, decrement on `}`, exit when depth returns to 0) or use `go/parser` to find the function's exact byte range. |
| **Effort** | S |

### Finding 4: scratch.counts and scratch.sorted uncapped in pool

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/ingestion/digest.go` — `weightedDigestScratch` |
| **Description** | The ARV-10 fix capped `scratch.pairs` (the largest slice) but left `scratch.counts` and `scratch.sorted` uncapped. These slices grow proportionally to the number of unique sample values. For payloads with high cardinality (many distinct CPU usage values), they can retain large capacities across pool reuse cycles. |
| **Risk** | Minor: these slices are typically smaller than `pairs`, but in extreme cases (thousands of distinct sample values per container), they could retain ~100KB per pooled scratch object unnecessarily. |
| **Recommendation** | Apply the same cap pattern: if `cap(scratch.counts) > maxWeightedPairsCap`, reset to `resetWeightedPairCap`. |
| **Effort** | S |

### Finding 5: Unquoted table name in ensureEntityQualityPartitions ALTER TABLE

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/engine/partitions_startup.go` — reloptions block |
| **Description** | The new `ALTER TABLE %s SET (autovacuum_analyze_scale_factor = 0.05)` uses `fmt.Sprintf` with `partName` which is constructed from the table name and a date suffix. While the current call graph only passes safe identifiers (e.g., `recommendation_quality_2026_07`), the pattern is vulnerable to SQL injection if `tableName` were ever sourced from external input. |
| **Risk** | Currently unexploitable (tableName comes from a hardcoded list). Future maintenance risk if the function is reused with dynamic input. |
| **Recommendation** | Use `pgx.Identifier{partName}.Sanitize()` for defense-in-depth. |
| **Effort** | S |

### Finding 6: DetermineCSVType Contains fallback exploitable with crafted filenames

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/utils/utils.go` — `DetermineCSVType` |
| **Description** | The function uses `strings.Contains` to match report types from filenames (e.g., a filename containing "ros" anywhere matches `ROSUsage`). In restricted-network mode where the operator uploads data, a crafted filename like `evil_ros_pod_usage.csv` could be misclassified, causing data to be routed to the wrong processor. |
| **Risk** | Low — requires compromised operator or MITM on upload path. Misclassification would cause processing errors (schema mismatch), not data corruption. |
| **Recommendation** | Use anchored prefix/suffix matching or regex with word boundaries instead of bare `Contains`. |
| **Effort** | S |

### Finding 7: ADR README index missing 0311–0317

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Governance |
| **Location** | `docs/adr/README.md` (index table ends at ADR-0310) |
| **Description** | Seven new ADR files (0311–0317) exist on disk but are absent from the README index table. The README is the canonical navigation surface — a developer scanning it finds no trace of the seven "Won't Fix / Deferred" decisions. |
| **Risk** | Future implementers may re-propose rejected optimizations (pool gzip writer, streaming JSON, RBAC SQL pushdown) without seeing the documented rationale. |
| **Recommendation** | Add all seven entries to the README index table with status "Rejected" or "Deferred". |
| **Effort** | S |

### Finding 8: ARV-12 silently dropped from CHANGELOG

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Governance |
| **Location** | `CHANGELOG.md` (Unreleased section) |
| **Description** | CHANGELOG contains ARV-1 through ARV-11 and ARV-13 through ARV-17, but ARV-12 (sanitizeCSVRow in-place mutation, closed as "documented, won't fix") has no entry. The "all 17 findings resolved" claim is not fully auditable. |
| **Risk** | Cross-referencing the audit against the CHANGELOG cannot account for ARV-12 without manual investigation. |
| **Recommendation** | Add a CHANGELOG entry: "ARV-12 accepted: `sanitizeCSVRow` in-place mutation accepted as safe — all call sites pass fresh `[]string{...}` literals." |
| **Effort** | S |

### Finding 9: v8 audit document findings status not updated

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Auditability |
| **Location** | `docs/audit/adversarial-review-v8-2026-07-07.md` (Findings Status Summary table) |
| **Description** | The v8 document still shows all 17 findings as `Open` with `Resolved: 0`. Sixteen have been remediated, one accepted. The document is useless as a historical record in its current state. |
| **Risk** | A compliance reviewer reading the v8 document will believe none of the findings have been addressed. |
| **Recommendation** | Update the v8 findings table to mark resolved/accepted status with commit references. |
| **Effort** | S |

### Finding 10: optionalFloat32Str precision difference undocumented

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Maintainability |
| **Location** | `internal/api/utils.go` (`optionalFloat32Str`, precision `3`) vs `internal/api/csv_helpers.go` (`optionalFloat32CSV`, precision `-1`) |
| **Description** | ARV-16 correctly preserved `optionalFloat32Str` because it uses fixed 3-decimal precision (different from `optionalFloat32CSV`). However, neither file documents why two float32 formatters with different precision exist in the same package. |
| **Risk** | A future developer may consolidate them, silently changing the VM CSV export format. |
| **Recommendation** | Add a comment to `optionalFloat32Str` explaining the intentional precision difference. |
| **Effort** | S |

### Finding 11: Business-hours weighted digest path lacks pool coverage

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/ingestion/digest.go:908–958` — `computeAllWeightedFieldDigests` |
| **Description** | When `ROS_BUSINESS_HOURS_ENABLED=true`, `computeAllWeightedFieldDigests` allocates three unPooled slices per invocation (`[]weightedMetricSample`, `[]float64` weights, `[]int64` vals). With 30k container-days per reconcile, this generates ~90k short-lived allocations (~45 MB transient heap) that must be GC'd. The analogous non-weighted path uses `fieldExtractPool`. |
| **Risk** | Elevated GC pause frequency when business hours is enabled. No correctness risk. |
| **Recommendation** | Pool the `weights` and `vals` slices via `fieldExtractPool` or a dedicated pool. |
| **Effort** | S |

### Finding 12: Echo pprof Symbol endpoint rejects POST

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Operational robustness |
| **Location** | `internal/debug/pprof.go:21` — `RegisterEchoPprof` |
| **Description** | The Symbol endpoint is registered as `e.GET(...)` only. `go tool pprof` resolves symbols via POST to `/debug/pprof/symbol`. Echo returns 405 for POST, making remote symbol resolution fail silently and producing unsymbolized flamegraphs. The Mux-based registration (`RegisterMuxPprof`) correctly handles both methods. |
| **Risk** | Profiling the API server produces unusable output. No production impact (pprof is opt-in dev-only). |
| **Recommendation** | Use `e.Any("/debug/pprof/symbol", ...)` to handle both GET and POST. |
| **Effort** | S |

## Priority Remediation Order

1. **Finding 2** (High) — Add statement timeout to namespace fallback scan [S effort]
2. **Finding 1** (Medium) — Replace lazy init with `sync.Once` [S effort]
3. **Finding 7** (Medium) — Add ADR 0311–0317 to README index [S effort]
4. **Finding 5** (Low) — Quote identifier in ALTER TABLE [S effort]
5. **Finding 4** (Low) — Cap remaining scratch slices [S effort]
6. **Finding 3** (Low) — Improve lint test brace tracking [S effort]
7. **Finding 6** (Low) — Tighten DetermineCSVType matching [S effort]
8. **Finding 8** (Low) — Add ARV-12 entry to CHANGELOG [S effort]
9. **Finding 9** (Low) — Update v8 audit document status [S effort]
10. **Finding 10** (Low) — Document optionalFloat32Str precision difference [S effort]
11. **Finding 11** (Low) — Pool weighted digest outer allocations [S effort]
12. **Finding 12** (Low) — Fix Echo pprof Symbol POST handling [S effort]

## Accepted Risks

None explicitly accepted yet. All findings are actionable.

## Verification of Prior Fixes (ARV-1 through ARV-17)

| ARV | Fix | Verified |
|-----|-----|----------|
| ARV-1 | Category fields in API SELECT | ✓ Present in nativeDetailSelect |
| ARV-2 | Partition DROP lock_timeout | ✓ SweepPartitionedTables uses lock_timeout |
| ARV-3 | DEBUG_SAVINGS removed from hot path | ✓ No debug logging in request path |
| ARV-4 | pprof hardened (auth, warn, no dual reg) | ✓ internal/debug/pprof.go |
| ARV-5 | GORM .Find() replaced with pgx scan | ✓ scanNativeContainerRows* |
| ARV-6 | WithHeavyStatementTimeout on quota/OOM | ✓ handlers use timeout wrapper |
| ARV-7 | Namespace fallback LIMIT removed, TOCTOU retry | ✓ (but see Finding 2) |
| ARV-8 | GPU model_name label → plain Counter | ✓ No CounterVec |
| ARV-9 | Fleet heatmap cache split | ✓ Separate config |
| ARV-10 | sync.Pool caps on spareInner and pairs | ✓ (but see Findings 4, 11) |
| ARV-11 | Autovacuum relaxed, reloptions on new partitions | ✓ Migration + startup code |
| ARV-12 | sanitizeCSVRow: documented, won't fix | ✓ Closed (but see Finding 8) |
| ARV-13 | Column count guard test | ✓ TestNativeDetailSelectColumnCount |
| ARV-14 | Variation rounding test cases | ✓ Two new cases |
| ARV-15 | ADRs 0311–0317 created | ✓ 7 files (but see Finding 7) |
| ARV-16 | CSV helpers consolidated | ✓ 3 duplicates removed (but see Finding 10) |
| ARV-17 | Statement timeout lint test | ✓ (but see Finding 3) |

## Current State

- **Total findings this version:** 12
- **Resolved from prior version:** 17/17 (all ARV findings closed)
- **New open:** 12 (1 High, 2 Medium, 9 Low)
- **Accepted:** 0
