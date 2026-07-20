# Adversarial Due Diligence Review — ros-ocp-backend

## Version & Date

Version: 11 | Date: 2026-07-20 | Reviewer: AI-assisted (incremental, post-engine-refactor)

## Scope

Incremental review covering ~39 commits since v10 (2026-07-12). Major changes:

- **Engine God-package refactoring (Phases 1–4):** Extracted 8 sub-packages:
  `engine/vm/`, `engine/snapshot/`, `engine/core/`, `engine/pvc/`,
  `engine/namespace/`, `engine/node/`, `engine/quota/`, `engine/gpu/`,
  `engine/container/`. Root engine retains orchestrators with backward-compatible
  type/function aliases in `compat.go` (369 lines). Root engine reduced from
  245 files to 45 files (~9K lines).
- **Correctness fix:** Snapshot `notification_codes` NULL for active
  classification — `classifySnapshot()` returned nil instead of `[]int16{}`,
  causing NOT NULL constraint violations that silently dropped ~97% of snapshot
  recommendations.
- **Performance fix:** `DecayTableLookup` OOM with large half-life values —
  added `maxDecayTableEntries = 100_000` cap with math.Exp fallback for
  half-lives that would produce >800 KB tables.
- **Build optimization:** Test dependency reduction, `-race` OOM fixes, dead
  `goleak` removal, migration test helper renaming to `_test.go`.
- **Kruize image rollback:** v0.11 → v0.10 (#696).
- **gosec linter:** Added to CI pipeline.
- **Lightweight `model/types` sub-package:** Reduces engine import weight.

## Executive Summary

The v10→v11 delta is dominated by the engine refactoring (Phases 1–4), a
well-executed structural decomposition that reduces the root engine from 245 to
45 files while maintaining full backward compatibility via `compat.go` type
aliases. The refactoring is clean — no behavioral changes were introduced, and
all prior v10 findings remain resolved.

Two production bugs were fixed: the snapshot notification_codes NULL bug (which
silently dropped 97% of snapshot recommendations) and the DecayTableLookup OOM
(which could crash the processor with artificially large half-life values). Both
fixes are well-targeted with appropriate test coverage.

The review identified **9 new findings** (1 Medium, 8 Low/Informational). The
most significant is the `NotificationCodeBitmap` silent truncation for codes
>63, which the CHANGELOG documents as already known and mitigated (high codes
use `AppendUnique`/direct append, not the bitmap path). The remaining findings
are defense-in-depth improvements around the function-variable wiring pattern,
integer arithmetic bounds, and the growing `compat.go` maintenance burden.

Overall assessment: **Very Good** — the codebase continues its strong trajectory.
The engine refactoring is the most significant structural improvement since the
project's inception, reducing cognitive load substantially while preserving API
stability. All v10 findings verified as still resolved.

## Scorecard

| Dimension | Rating | Key gap |
|-----------|--------|---------|
| Security | ★★★★☆ | GPU MIG `groupCol` SQL interpolation is safe (hardcoded values) but lacks `pgx.Identifier` quoting |
| Correctness | ★★★★★ | Snapshot NULL fix resolves critical data-loss bug; bitmap limitation documented and mitigated |
| Performance | ★★★★★ | DecayTable OOM fix excellent; `sync.Map` growth bounded by distinct half-life count |
| Operational robustness | ★★★★★ | Kruize rollback shows responsive ops; build improvements reduce CI flakiness |
| Design quality | ★★★★★ | Engine refactoring is exemplary; function-variable wiring is acceptable tradeoff |
| Maintainability | ★★★★☆ | `compat.go` at 369 lines is growing; IDE navigation burden from aliases |
| Auditability | ★★★★★ | CHANGELOG documents all changes including known limitations |
| Governance | ★★★★★ | gosec added; ADR discipline maintained; test coverage strong (400+ tests across sub-packages) |

## Prior Findings Status (v10 → v11)

All 20 v10 findings remain resolved. Verified:

| v10 # | Title | Severity | Status | Verification |
|--------|-------|----------|--------|--------------|
| 1 | Cluster UUID cache mutable slice | Medium | **Still Resolved** | Defensive copy in `cache.go` verified |
| 2 | Cluster cache stale after source addition | Medium | **Still Resolved** | `InvalidateOrg` in ingest path verified |
| 3 | `loadDigestRows` unbounded rows | Medium | **Still Resolved** | Hard cap with configurable `ROS_MAX_DIGEST_ROWS_PER_CLUSTER` verified |
| 4 | VM history append outside transaction | Medium | **Still Resolved** | History inside transaction boundary verified |
| 5 | Duplicate `maxPgxBatchQueue` | Medium | **Still Resolved** | Shared `db.MaxPgxBatchQueue` in `internal/db/batch.go` verified |
| 6 | `fetchAndCache` unwrapped errors | Medium | **Still Resolved** | Errors wrapped with context verified |
| 7 | Migration 000174 advisory comment | Medium | **Still Resolved** | Comment present |
| 8 | Retention.go identifier quoting | Low | **Still Resolved** | `pgx.Identifier.Sanitize()` in `purgeDateRetainedTable` verified |
| 9 | `flushRecommendationBatch` trusts count | Low | **Still Resolved** | Now uses `batch.Len()` directly via `core.FlushRecommendationBatch` |
| 10 | VM digest capacity hint | Low | **Still Resolved** | Verified |
| 11 | Autovacuum tuning missing `node_recommendations` | Low | **Still Resolved** | Verified in follow-up migration |
| 12 | `flushRecommendationBatch` error context | Low | **Still Resolved** | Row index in error message verified |
| 13 | Cluster cache health check | Low | **Accepted** | Unchanged — acceptable risk |
| 14 | Inconsistent `chunkEnd` clamping | Low | **Still Resolved** | All sites use `min()` verified |
| 15 | `GPUContainerKey` duplicates `gpuMIGQualityKey` | Low | **Still Resolved** | Unified in `gpu/mig_quality.go` |
| 16 | Cluster cache structured logging | Low | **Still Resolved** | Logging on DB fallback verified |
| 17 | VM lock documentation | Low | **Still Resolved** | Comment present |
| 18 | v4 audit HEAD reference stale | Low | **Still Resolved** | Updated |
| 19 | Migration 000175 IF EXISTS guards | Low | **Still Resolved** | Guards added |
| 20 | Engine God package | Low | **Resolved** | Phases 1–4 complete; root reduced to 45 files/9K lines |

**Summary:** 19 Resolved, 1 Accepted, 0 Regressed.

## Findings Status Summary

| # | Title | Severity | Dimension | Status |
|---|-------|----------|-----------|--------|
| 1 | `NotificationCodeBitmap` silently drops codes >63 | Medium | Correctness | **Accepted** |
| 2 | `ResolveGPUThresholdSettings` function variable nil panic risk | Low | Correctness | Open |
| 3 | `ComputeRecommendedReplicas` integer overflow with large clusters | Low | Correctness | Open |
| 4 | `decayTables` sync.Map never evicts entries | Low | Performance | **Accepted** |
| 5 | GPU MIG `groupCol` SQL interpolation without `pgx.Identifier` | Low | Security | Open |
| 6 | `compat.go` growing maintenance burden (369 lines) | Informational | Maintainability | Open |
| 7 | `init()` ordering dependency between `gpu_wire.go` and `idle_classification.go` | Informational | Design | **Accepted** |
| 8 | Container sub-package `WriteRecommendationHistory` missing `ctx.Err()` check | Low | Performance | Open |
| 9 | `EvaluateNotificationsWithThresholds` returns mutable initial slice | Informational | Correctness | Open |

## Findings Detail

### Finding 1: `NotificationCodeBitmap` silently drops codes >63

| Field | Value |
|-------|-------|
| **Severity** | Medium |
| **Dimension** | Correctness |
| **Location** | `internal/engine/core/notifications_bitmap.go:6-22` |
| **Description** | `NotificationCodeBitmap` is a `uint64` supporting codes 1–63 only. `Has()` and `Add()` silently return/no-op for codes outside this range. Codes 74, 76, 77, and 78 (`NotifNodePodSchedulingLimit`, `NotifNodeFleetConsolidation`, `NotifSparseData`, `NotifGPUMultiDevice`) are now defined. `MergeNotificationCodes` uses the bitmap internally via `NotificationCodesFromSlice`, so any existing codes >63 passed as the `existing` parameter would be silently dropped during merge. |
| **Risk** | If `MergeNotificationCodes` is called on a slice containing codes 74+ (e.g., from a database read that already has these codes), those codes would be silently lost. Currently mitigated because high-code paths use direct `append` or `AppendUnique` (linear scan, not bitmap), and the CHANGELOG documents this limitation. |
| **Recommendation** | Either: (a) extend to `[2]uint64` supporting codes 1–127, or (b) add a runtime assertion/log warning in `Add()` for codes >63, or (c) document in the type's godoc that `MergeNotificationCodes` must not be used for codes >63. |
| **Effort** | S |
| **Status** | **Accepted** — CHANGELOG Phase 3 entry documents this as a known limitation. All high-code paths use `AppendUnique` (linear scan) or direct `append`, avoiding the bitmap. `MergeNotificationCodes` is only called from `compat.go` as an alias and production callers currently pass only low codes through it. Risk is latent — a future developer could inadvertently route high codes through the bitmap path. |

### Finding 2: `ResolveGPUThresholdSettings` function variable nil panic risk

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/engine/gpu/settings.go:75`, `internal/engine/gpu/query.go:110` |
| **Description** | `ResolveGPUThresholdSettings` is a package-level `var` (function pointer) wired via `init()` in `gpu_wire.go`. If `gpu.ResolveGPUThresholdSettings` is called before the `engine` package's `init()` runs (possible in unit tests that import `gpu` directly without importing `engine`), it panics with a nil function call. Unlike `LoadGPUIdleConfig`, which has a nil guard in its own `init()`, `ResolveGPUThresholdSettings` has no such guard. |
| **Risk** | Low — only manifests in isolated unit tests of the `gpu` package that don't import root `engine`. Production code always imports root `engine` (which triggers `gpu_wire.go` init). |
| **Recommendation** | Add a nil guard in `gpu/settings.go` similar to `idle_classification.go:38-40`, defaulting to `DefaultGPUThresholdSettings()` on nil. |
| **Effort** | S |

### Finding 3: `ComputeRecommendedReplicas` integer overflow with large clusters

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Correctness |
| **Location** | `internal/engine/container/replica_optimization.go:55-60` |
| **Description** | The replica calculation multiplies: `int64(latestDigest.CPUUsageP95MC) * int64(currentReplicas) * 100`. With a large cluster (CPUUsageP95MC=2_000_000 mc for a 2000-core workload, 1000 replicas), the numerator is `2e6 * 1000 * 100 = 2e11`, well within int64 range. However, with extreme values (CPUUsageP95MC close to MaxInt64/100/replicas), overflow could produce negative values leading to negative replica recommendations. The code has no overflow guard. |
| **Risk** | Negligible in practice — requires CPUUsageP95MC > 92 billion millicores (92 million cores), which is physically impossible. Defense-in-depth concern only. |
| **Recommendation** | Add a bounds check on the result: `if recommended < 0 { recommended = currentReplicas }`. Alternatively, document the safe range. |
| **Effort** | S |

### Finding 4: `decayTables` sync.Map never evicts entries

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/engine/core/decay_table.go:24` |
| **Description** | The `decayTables` sync.Map caches precomputed decay tables keyed by integer half-life hours. Entries are never evicted. Each entry is at most 800 KB (capped by `maxDecayTableEntries`). The comment states "ros-ocp-backend runs per-batch, not as a long-lived daemon" — but the API server IS long-lived, and `DecayTableLookup` is called from recommendation reprojection in API handlers. |
| **Risk** | Low — production half-lives are limited to a small set (short/medium/long term × configurable values), so in practice 3–6 entries accumulate (~36 KB total). An adversarial tenant setting many distinct half-lives via settings API is bounded by the settings validation allowlist. |
| **Recommendation** | No immediate action. The OOM cap at 100K entries already prevents large individual tables. Total memory is bounded by the number of distinct half-life values ever encountered (typically <10). Add a comment documenting this reasoning. |
| **Effort** | S |
| **Status** | **Accepted** — bounded by settings validation; total memory negligible. |

### Finding 5: GPU MIG `groupCol` SQL interpolation without `pgx.Identifier`

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Security |
| **Location** | `internal/engine/gpu/mig_list.go:166-195` |
| **Description** | `CountGPUMIGGrouped` and `ListGPUMIGGrouped` interpolate `groupCol` (either `"m.namespace"` or `"m.cluster_uuid"`) into SQL via `fmt.Sprintf`. The values are hardcoded based on a boolean parameter `groupByCluster`, making injection impossible. However, this violates the project's own convention established after v9 finding #5 (DDL identifier quoting) and v10 finding #8 (retention identifier quoting), where all SQL identifiers are sanitized via `pgx.Identifier`. |
| **Risk** | Zero current risk (hardcoded values). Future maintenance risk if the function signature is extended to accept user-controlled group-by columns. Defense-in-depth inconsistency. |
| **Recommendation** | Replace `groupCol` string interpolation with `pgx.Identifier{"m", "namespace"}.Sanitize()` / `pgx.Identifier{"m", "cluster_uuid"}.Sanitize()`. This is a two-line change. |
| **Effort** | S |

### Finding 6: `compat.go` growing maintenance burden (369 lines)

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Maintainability |
| **Location** | `internal/engine/compat.go` |
| **Description** | The backward-compatibility shim file (`compat.go`) contains 369 lines of type aliases, constant aliases, and function variable aliases mapping root `engine` exports to sub-package implementations. Every new exported symbol in a sub-package requires a corresponding alias here. The file has no tests (aliases are tested through their original callsites), making it easy for a stale alias to go unnoticed. |
| **Risk** | Low immediate risk — the aliases are purely mechanical. Medium-term risk: (a) IDE "go to definition" navigates to the alias rather than the implementation; (b) dead aliases accumulate as callers migrate to direct sub-package imports; (c) no automated staleness detection. |
| **Recommendation** | Add a test or lint rule that verifies all exported symbols from sub-packages have corresponding aliases in `compat.go` (or conversely, that all aliases point to valid targets). Consider establishing a deprecation timeline for the aliases once all internal callers migrate. |
| **Effort** | M |

### Finding 7: `init()` ordering dependency between `gpu_wire.go` and `idle_classification.go`

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Design quality |
| **Location** | `internal/engine/gpu_wire.go:12-14`, `internal/engine/gpu/idle_classification.go:38-40` |
| **Description** | Two `init()` functions compete to set `gpu.LoadGPUIdleConfig`: `gpu/idle_classification.go` sets a default if nil, and `engine/gpu_wire.go` unconditionally overwrites with the real implementation. Go guarantees that imported packages' init functions run before the importer's, so `gpu`'s init runs first (sets default), then `engine`'s init overwrites (sets real). This is correct but fragile — it relies on import ordering guarantees and would break if the wiring moved to a separate package. |
| **Risk** | Negligible — Go's init ordering is deterministic within a compilation unit. The pattern is common in Go projects breaking circular deps. |
| **Recommendation** | Add a comment in `gpu/idle_classification.go` documenting the expected override: `// This default is overridden by engine/gpu_wire.go init() when the root engine package is imported.` |
| **Effort** | S |
| **Status** | **Accepted** — standard Go pattern; comment would help but not critical. |

### Finding 8: Container `WriteRecommendationHistory` missing `ctx.Err()` check

| Field | Value |
|-------|-------|
| **Severity** | Low |
| **Dimension** | Performance |
| **Location** | `internal/engine/container/history.go:28-74` |
| **Description** | `WriteRecommendationHistory` iterates over potentially thousands of `ContainerRec` items in chunks, sending pgx.Batch operations. Unlike the streaming pipeline which checks `ctx.Err()` every `streamBatchSize` iterations, this function has no cancellation check between chunks. If the context is cancelled (e.g., shutdown signal), the function continues batching all remaining chunks before returning. |
| **Risk** | Low — the function runs during the processing pipeline (not API handlers), and graceful shutdown waits for in-flight work. The delay is bounded by the number of chunks × batch execution time. For 10K containers with 2000-per-batch, this is 5 batches (~seconds, not minutes). |
| **Recommendation** | Add `if err := ctx.Err(); err != nil { return err }` at the top of the chunk loop. Consistent with `ctx.Err()` checks elsewhere in the codebase. |
| **Effort** | S |

### Finding 9: `EvaluateNotificationsWithThresholds` returns mutable initial slice

| Field | Value |
|-------|-------|
| **Severity** | Informational |
| **Dimension** | Correctness |
| **Location** | `internal/engine/container/notifications.go:13` |
| **Description** | `EvaluateNotificationsWithThresholds` initializes `codes := []int16{}` and appends to it. The returned slice is the caller's to mutate, which is correct. However, if no conditions match, the function returns an empty but allocated slice (`[]int16{}` vs `nil`). This is actually the CORRECT behavior per the snapshot fix (commit 431fb95a) — nil would violate NOT NULL constraints. The pattern is consistent with the fix. |
| **Risk** | None — this is actually a positive pattern. Documenting for completeness. |
| **Recommendation** | No action needed. This is correct behavior. Consider adding a godoc comment: "Returns a non-nil empty slice when no notifications apply (required for NOT NULL database constraints)." |
| **Effort** | S |

## Items Verified Clean

The following areas were inspected and found correct:

- **Engine sub-package extraction:** All 8 sub-packages (`core`, `vm`, `snapshot`, `pvc`, `namespace`, `node`, `quota`, `gpu`, `container`) have clean import graphs with no circular dependencies. Each sub-package imports only from `core` and other leaf packages (`config`, `db`, `model/types`, `fixedpoint`). Root engine imports sub-packages unidirectionally.

- **`compat.go` type aliases:** All aliases use `=` (true type aliases, not new types), preserving assignability and method sets. Function variable aliases use `var f = subpkg.F` pattern correctly.

- **Snapshot notification_codes fix:** The fix correctly returns `[]int16{}` (empty non-nil slice) for the 'active' classification, matching the database NOT NULL constraint. Test assertions updated from `assert.Nil` to `assert.NotNil` + `assert.Empty`.

- **DecayTableLookup OOM cap:** The `maxDecayTableEntries = 100_000` constant correctly caps table allocation at ~800 KB. For half-lives exceeding the cap, the code falls back to direct `math.Exp` computation — mathematically equivalent, just slower. The cap is generous enough that all production half-lives (max ~360h → table size ~720 entries) are served from the precomputed table.

- **`core.FlushRecommendationBatch`:** Correctly uses `batch.Len()` internally (resolving v10 finding #9), eliminating the trusted-caller-count issue. Error messages include 1-based statement index for debugging.

- **Function-variable wiring pattern (`gpu_wire.go`):** The `init()` function correctly wires `gpu.ResolveGPUThresholdSettings` and `gpu.LoadGPUIdleConfig`. The pattern breaks circular imports cleanly — `gpu` package defines the function variable; root `engine` provides the implementation via its `init()`.

- **`model/types` sub-package:** Contains only struct definitions with no business logic — a pure data transfer types package. Reduces import weight for packages that only need type definitions (e.g., `gpu/mig_list.go`).

- **gosec integration:** Added to CI workflow, scanning for common Go security issues (G101 credentials, G304 file path traversal, G401 weak crypto, etc.). No `#nosec` annotations found in production code, indicating no suppressed findings.

- **Retention SQL quoting:** `purgeDateRetainedTable` correctly uses `pgx.Identifier{dt.Table}.Sanitize()` and `pgx.Identifier{dt.DateColumn}.Sanitize()` — v10 finding #8 still properly resolved.

- **No hardcoded secrets, permissive CORS, or missing rate limits** in new code.

- **No `_ = err` silent error suppression** in changed code.

- **Test coverage is comprehensive:** 400+ test functions across the 8 sub-packages. Container sub-package has replica optimization (10 tests), quality (1), savings (1). GPU sub-package has 99 tests. VM sub-package has 70+ tests. Node has 59 tests.

## Priority Remediation Order

| Priority | Finding | Severity | Title | Effort | Status |
|----------|---------|----------|-------|--------|--------|
| 1 | 5 | Low | GPU MIG `groupCol` — use `pgx.Identifier` quoting | S | Open |
| 2 | 2 | Low | `ResolveGPUThresholdSettings` — add nil guard | S | Open |
| 3 | 8 | Low | `WriteRecommendationHistory` — add `ctx.Err()` check | S | Open |
| 4 | 3 | Low | `ComputeRecommendedReplicas` — add overflow guard | S | Open |
| 5 | 9 | Informational | `EvaluateNotificationsWithThresholds` — add godoc | S | Open |
| 6 | 6 | Informational | `compat.go` — add staleness lint | M | Open |
| 7 | 1 | Medium | `NotificationCodeBitmap` — document or extend | S | **Accepted** |
| 8 | 4 | Low | `decayTables` sync.Map — document reasoning | S | **Accepted** |
| 9 | 7 | Informational | `init()` ordering — add documentation comment | S | **Accepted** |

## Accepted Risks

- **#1 — `NotificationCodeBitmap` codes >63:** The bitmap is documented as limited
  to codes 1–63. All codes >63 are handled via `AppendUnique` (linear scan) or
  direct `append`, bypassing the bitmap. `MergeNotificationCodes` is only used
  for container-level code merging where all active codes are ≤35. The limitation
  is documented in the CHANGELOG Phase 3 entry. If codes exceed 127 in the future,
  extending to `[2]uint64` is a trivial change.

- **#4 — `decayTables` sync.Map growth:** Bounded by the number of distinct
  half-life values (typically 3–6 in production). Each table is capped at 100K
  entries (~800 KB). Total memory for the map is negligible (<50 KB typically).
  Settings validation prevents adversarial half-life injection.

- **#7 — `init()` ordering dependency:** Standard Go idiom for breaking circular
  imports. The import graph guarantees correct ordering: `engine/gpu` init runs
  before `engine` init (child before parent). The pattern is well-documented
  in the codebase and unlikely to be disrupted without breaking compilation.

- **#300 (prior) — Cluster cache health check:** Unchanged from v10. The cache
  is a lightweight in-memory LRU with no external dependencies.

## Current State

- **Total v11 findings:** 9 (0 Critical, 1 Medium, 5 Low, 3 Informational)
- **Open (actionable):** 6 (#2, #3, #5, #6, #8, #9)
- **Accepted:** 3 (#1, #4, #7)
- **Prior findings verified:** 20/20 still resolved (1 accepted)
- **Regressed:** 0

## Strengths Noted

1. **Exemplary refactoring execution:** The engine decomposition across 4 phases
   demonstrates disciplined incremental architecture improvement. Each phase is
   self-contained, tested, and preserves backward compatibility.

2. **Rapid bug response:** The snapshot notification_codes NULL bug (silently
   dropping 97% of snapshot recommendations) was identified, root-caused, fixed,
   and tested in a single commit with clear commit message and CHANGELOG entry.

3. **Defense-in-depth culture:** The DecayTableLookup OOM cap was added
   proactively — no production incident triggered it. The 100K-entry limit is
   calibrated to allow all legitimate half-lives while preventing pathological
   allocations.

4. **Test infrastructure investment:** Moving integration tests out of
   sub-packages to prevent `-race` OOM shows attention to CI reliability, not
   just feature coverage.

5. **Comprehensive CHANGELOG discipline:** Every behavioral change, known
   limitation, and architectural decision is documented with issue references.
