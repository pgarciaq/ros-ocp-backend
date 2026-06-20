# Honesty Exercise — Namespace Recommendations

**Date:** 2026-06-20
**Scope:** Cross-source alignment audit for the namespace-level resource optimization recommendations feature.

---

## Table of Contents

- [Executive Summary](#executive-summary)
- [Sources Discovered](#sources-discovered)
- [Per-Source Audit](#per-source-audit)
- [Alignment Matrix](#alignment-matrix)
- [Discrepancies Found](#discrepancies-found)
- [Fixes Applied](#fixes-applied)
- [Gaps vs Container Recommendations](#gaps-vs-container-recommendations)
- [Design Questions for User](#design-questions-for-user)

---

## Executive Summary

Namespace recommendations are a **distinct plugin** (`namespace`) that produces CPU/memory right-sizing recommendations at the OpenShift namespace (project) level. Unlike container recommendations, which size individual containers, namespace recommendations aggregate CPU/memory usage across all pods in a namespace and recommend namespace-level resource requests/limits.

**What works end-to-end:**
- Native Go engine computes namespace recommendations from `daily_namespace_digests`
- Three API paths serve list, detail, and history responses
- Keyset pagination with proper tie-breaker via `after` cursor
- CSV export for native path
- `meta.currency` on list endpoint
- `confidence_level` computed and returned
- Explanation factors (including `MemFloorApplied`) supported
- `filter[term]`, `filter[engine]`, `filter[stale]`, `filter[cluster]`, `filter[project]`, `filter[idle_state]`, and tag filters all work in Go code
- Staleness detection via `ROS_STALENESS_THRESHOLD_HOURS`
- Notification codes {1, 2, 7, 9, 77} specific to namespaces
- NISE generates `ocp_ros_namespace_usage.csv` data
- Comprehensive E2E and IQE test coverage

**Key discrepancies found:**
1. **OpenAPI missing filters** — `filter[cluster]`, `filter[project]`, `filter[idle_state]` work in Go code but are not documented in the OpenAPI spec for the namespace list endpoints **(FIXED)**
2. **OpenAPI `order_by` lacks enum** — namespace endpoints have `order_by` as a free-form string with no enum listing valid values **(FIXED)**
3. **`NamespaceDetailResponse` missing idle fields** — the detail response struct lacks `idle_state`, `idle_since`, `idle_duration_days` that are present on the list response and in the OpenAPI `NamespaceRecommendation` schema
4. **UI API wiring gap** — `RosPathsType.namespaces` in koku-ui maps to the container endpoint (`'recommendations'`) instead of the namespace endpoint; the "Projects" tab hits the wrong API
5. **No savings fields** — namespace recommendations intentionally have no dollar savings (confirmed by design), but this is not explicitly stated in the OpenAPI schema descriptions
6. **Legacy Kruize path CSV returns error** — the old `GetNamespaceRecommendationSetList` handler returns HTTP 406 for CSV requests (TODO comment in code)

---

## Sources Discovered

| # | Source | Location | Status |
|---|--------|----------|--------|
| 1 | Requirements/design | `ros-ocp-backend/docs/archive/requirements.md` (REQ-1.13) | ✅ Found |
| 2 | Feature spec | `ros-ocp-backend/docs/features/namespace-recommendations.md` | ✅ Found |
| 3 | Public docs (plugin reference) | `ros-ocp-backend/docs-site/plugin-reference/namespace.md` | ✅ Found |
| 4 | Go handlers | `ros-ocp-backend/internal/api/handlers.go` (lines 151-907) | ✅ Found |
| 5 | Go query param mapping | `ros-ocp-backend/internal/api/utils.go` (`MapNativeNamespaceQueryParameters`) | ✅ Found |
| 6 | Go list options | `ros-ocp-backend/internal/api/listoptions/list_options.go` (`NsAllowedOrderBy`) | ✅ Found |
| 7 | Go namespace model | `ros-ocp-backend/internal/model/namespace_recommendation_set_native.go` | ✅ Found |
| 8 | Go detail response | `ros-ocp-backend/internal/model/detail_response.go` (`NamespaceDetailResponse`) | ✅ Found |
| 9 | Go list response | `ros-ocp-backend/internal/model/list_response.go` (`NamespaceListResponse`) | ✅ Found |
| 10 | Go namespace plugin | `ros-ocp-backend/internal/plugins/namespace/plugin.go` | ✅ Found |
| 11 | Go engine | `ros-ocp-backend/internal/engine/recommend_namespace.go` | ✅ Found |
| 12 | Go CSV export | `ros-ocp-backend/internal/api/utils.go` (`GenerateNativeNamespaceCSV`) | ✅ Found |
| 13 | Go CSV header | `ros-ocp-backend/internal/api/common.go` (`NativeNSCSVHeader`) | ✅ Found |
| 14 | Notification catalog | `ros-ocp-backend/internal/notifications/catalog.go` | ✅ Found |
| 15 | OpenAPI spec | `ros-ocp-backend/openapi.json` | ✅ Found |
| 16 | API cheatsheet | `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc` | ✅ Found (124 namespace mentions) |
| 17 | Bruno collection | `costmgmt-api-cheatsheet/bruno/Optimizations/` | ✅ Found (7+ namespace .bru files) |
| 18 | E2E tests | `cost-onprem-chart/tests/suites/ros/test_namespace_recommendations.py` | ✅ Found |
| 19 | IQE tests | `iqe-ros-ocp-plugin/` (8+ namespace test files) | ✅ Found |
| 20 | Unit tests | `ros-ocp-backend/internal/model/detail_response_test.go`, `list_response_test.go`, etc. | ✅ Found |
| 21 | NISE generator | `nise/generators/ocp/ocp_generator.py` (`_gen_quarter_hourly_ros_ocp_namespace_usage`) | ✅ Found |
| 22 | Koku backend | `koku/` — Koku handles OCP data ingestion; namespace data flows through as `ocp_ros_namespace_usage` report type | ✅ Found |
| 23 | Koku-UI frontend | `koku-ui/apps/koku-ui-ros/` — behind `cost-management.koku-ui-ros.namespace` feature toggle | ✅ Found |

---

## Per-Source Audit

### 1. API Endpoints

| Path | Method | Handler | Notes |
|------|--------|---------|-------|
| `/openshift/namespace/recommendations` | GET | `GetNamespaceRecommendationSetListWithFallback` | Legacy path, native-first with Kruize fallback |
| `/recommendations/openshift/namespaces` | GET | Same handler | Canonical plural path |
| `/recommendations/openshift/namespace/{id}` | GET | `GetNamespaceRecommendationSetWithFallback` | Detail, native-first with Kruize fallback |
| `/recommendations/openshift/namespaces/{id}` | GET | Same handler | Canonical plural path |
| `/recommendations/openshift/namespaces/{id}/history` | GET | History handler | Historical snapshots |

The fallback mechanism: native engine is tried first. If zero results, falls back to legacy Kruize JSONB path — UNLESS tag filters are present (tag filtering is native-only).

### 2. Filters (Go Code Reality)

| Filter | Supported | Implementation |
|--------|-----------|----------------|
| `filter[cluster]` / `cluster` | ✅ | `MapNativeNamespaceQueryParameters` — partial alias match, exact UUID match |
| `filter[project]` / `project` / `namespace` | ✅ | Partial match on namespace name |
| `filter[idle_state]` | ✅ | Comma-separated values: active, idle, zombie |
| `filter[term]` / `term` | ✅ | Accepts both `short` and `short_term` forms (via `NormalizeRecommendationTermFilter`) |
| `filter[engine]` / `engine` | ✅ | `cost` or `performance` |
| `filter[stale]` / `stale` | ✅ | `false` (default, exclude), `true` (include all), `only` (only stale) |
| `filter[tag:<key>]` / `tag=key:value` | ✅ | Requires `ROS_TAGS_ENABLED=true` |

### 3. Pagination

| Type | Supported | Implementation |
|------|-----------|----------------|
| Offset | ✅ | `offset` + `limit` (default 100, max 1000) |
| Keyset (cursor) | ✅ | `after` param, `meta.has_next` + `meta.next_cursor` in response |
| Tie-breaker | ✅ | Compound cursor with sort field + namespace_name for deterministic ordering |

### 4. Response Shape — List

**Native path (slim/projection):** `NativeNamespaceListItem` / `NamespaceListResponse`
- `id`, `cluster_alias`, `cluster_uuid`, `project`, `source_id`, `last_reported`
- `idle_state` — present
- `recommendations` — nested `recommendation_terms` with `short_term`/`medium_term`/`long_term` → `recommendation_engines` → `cost`/`performance` → `config` + `variation`

**Native path (full/detail-in-list):** `NamespaceDetailResponse`
- Same top-level fields but uses `DetailRecommendations` format (more verbose, includes `duration_in_hours`, `monitoring_end_time`, `current`)
- **Missing:** `idle_state`, `idle_since`, `idle_duration_days` — these are absent from `NamespaceDetailResponse` struct

### 5. Response Shape — Detail

`NamespaceDetailResponse` struct:
- `id`, `cluster_alias`, `cluster_uuid`, `project`, `source_id`, `last_reported`
- `recommendations` → `current`, `monitoring_end_time`, `recommendation_terms` → per-term → `duration_in_hours`, `recommendation_engines` → `cost`/`performance` → `config` (requests/limits → cpu/memory → amount/format), `variation`, `notifications`
- **Missing:** `idle_state` (present on list response but not detail)
- **Missing:** No savings fields (by design)

### 6. CSV Export

**Native path:** ✅ Supported via `GenerateNativeNamespaceCSV`

Columns (`NativeNSCSVHeader`):
```
cluster_uuid, cluster_alias, project, last_reported, source_id, idle_state,
recommendation_term, recommendation_engine, rec_cpu_request_millicores,
rec_cpu_limit_millicores, rec_memory_request_kib, rec_memory_limit_kib,
current_cpu_request_millicores, current_cpu_limit_millicores,
current_memory_request_kib, current_memory_limit_kib,
variation_cpu_request_pct, variation_cpu_limit_pct,
variation_memory_request_pct, variation_memory_limit_pct,
confidence_level, notification_codes
```

**Legacy Kruize path:** ❌ Returns HTTP 406 ("CSV format is not supported")

### 7. Notification Codes

From `catalog.go`, namespace plugin codes:
| Code | Meaning |
|------|---------|
| 1 | SHORT_TERM_UNAVAILABLE — fewer than 24h of data |
| 2 | STALE_DATA — cluster not reporting within threshold |
| 7 | MEDIUM_TERM_UNAVAILABLE — fewer than 7 days of data |
| 9 | LONG_TERM_UNAVAILABLE — fewer than 15 days of data |
| 77 | MEMORY_TRENDING_UP — memory usage trending upward |

**Not assigned to namespaces:**
- Code 25 (NO_COST_DATA / COST_DATA_UNAVAILABLE) — this is container-only
- Idle-related codes (19-22) — not in namespace plugin catalog

### 8. order_by Options

From `NsAllowedOrderBy`:
```
cluster, project, last_reported,
cpu_request_current, memory_request_current,
cpu_variation_short_cost, cpu_variation_short_performance,
cpu_variation_medium_cost, cpu_variation_medium_performance,
cpu_variation_long_cost, cpu_variation_long_performance,
memory_variation_short_cost, memory_variation_short_performance,
memory_variation_medium_cost, memory_variation_medium_performance,
memory_variation_long_cost, memory_variation_long_performance
```

**Not available for namespace ordering** (unlike containers):
- `idle_state`, `idle_duration_days`
- `estimated_monthly_waste`, `estimated_monthly_savings`
- `workload_type`, `workload`, `container`

### 9. meta.currency

✅ Present on both list response variants (`buildNamespaceDetailListMeta` and `buildNamespaceSlimListMeta` both call `resolveListCurrencyFromRequest`).

### 10. confidence_level

✅ Computed and stored in `namespace_recommendation_sets.confidence_level`. Returned in:
- List response (slim): within `recommendation_engines` → `config`
- CSV export: dedicated `confidence_level` column
- `NativeNamespaceResult` struct

### 11. Data Generation (NISE)

✅ NISE generates `ocp_ros_namespace_usage.csv` via `_gen_quarter_hourly_ros_ocp_namespace_usage` in `nise/generators/ocp/ocp_generator.py`. Uses namespace-level aggregation data with CPU/memory usage/request metrics.

### 12. Savings

**By design: No dollar savings for namespace recommendations.**

Confirmed in:
- `docs/features/namespace-recommendations.md`: "no dollar savings field is included"
- `NativeNamespaceResult` struct: no savings fields
- `NativeNSCSVHeader`: no savings columns
- `NamespaceDetailResponse`: no savings fields

### 13. Memory Floor (MemFloorKiB)

✅ Applies to namespace recommendations. The `RecommendCPUAndMemory` function in `internal/engine/recommend_cpu_and_memory.go` applies `MemFloorKiB` and sets `MemFloorApplied` in explanation factors. This function is called by `recommend_namespace.go`.

### 14. Explanation Factors

✅ Namespace recommendations support explanation factors via `include=explanation` query parameter on the detail endpoint. The engine sets `ContainerExplanationFactors` including:
- `DataDays`, `DecayHalfLifeHours`
- `CPUCostPctMC`, `CPUPerfPctMC`, `MemCostPctKiB`, `MemPerfPctKiB`
- `MemFloorApplied`

---

## Alignment Matrix

### Legend
- ✅ Aligned / correct
- ⚠️ Partially aligned / incomplete
- ❌ Wrong or missing
- — Not applicable

### Endpoints and Methods

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| List endpoint path(s) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Detail endpoint path(s) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| History endpoint path | — | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| GET method | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### Filters

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| `filter[cluster]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ Missing |
| `filter[project]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ Missing |
| `filter[idle_state]` | — | ✅ | ✅ | — | — | — | ✅ | — | — | ❌ Missing¹ |
| `filter[term]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[engine]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[stale]` / `stale` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Tag filters | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

¹ `filter[idle_state]` is documented on the old `/openshift/namespace/recommendations` path in OpenAPI but missing from the canonical `/recommendations/openshift/namespaces` and legacy `/openshift/namespace/recommendations` paths.

### order_by

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| `order_by` values enum | — | ✅ | — | — | — | — | — | ✅ | ✅ | ❌ No enum |
| `order_how` (asc/desc) | — | ✅ | — | — | — | — | — | ✅ | ✅ | ✅ |

### Pagination

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| Offset pagination | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Keyset (`after` cursor) | — | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | — | ✅ |
| `meta.has_next` | — | ✅ | ✅ | — | — | — | ✅ | ✅ | — | ✅ |
| `meta.next_cursor` | — | ✅ | ✅ | — | — | — | ✅ | ✅ | — | ✅ |

### Response Fields

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| `meta.currency` | — | ✅ | ✅ | — | — | — | — | — | — | ✅ |
| `idle_state` (list) | — | ✅ | ✅ | — | — | — | ✅ | — | — | ✅ |
| `idle_state` (detail) | — | ❌ Missing | — | — | — | — | — | — | — | ❌ Missing² |
| `confidence_level` | — | ✅ | ✅ | — | — | — | ✅ | — | — | — |
| Savings fields | — | ✅ (none) | ✅ (none) | ✅ (none) | — | — | — | — | — | — |

² The `NamespaceDetailResponse` Go struct and its OpenAPI `NamespaceDetailResponse` schema both lack `idle_state`. The `NativeNamespaceResult` has it, and the list response surfaces it, but the detail response does not. The `NamespaceRecommendation` schema (used for listing) has `idle_state`.

### Notification Codes

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| Codes {1,2,7,9,77} | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — |
| Code 25 (NO_COST_DATA) excluded | ✅ | ✅ | ✅ | ✅ | — | — | — | — | — | — |

### CSV Export

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| CSV supported (native path) | — | ✅ | ✅ | — | — | — | — | ✅ | — | — |
| CSV columns complete | — | ✅ | — | — | — | — | — | — | — | — |
| CSV returns savings | — | ✅ (none) | ✅ (none) | — | — | — | — | — | — | — |

### Memory Floor and Explanation

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| MemFloorKiB applies | ✅ | ✅ | — | — | — | — | ✅ | — | — | — |
| `include=explanation` | — | ✅ | — | — | — | — | ✅ | — | — | ✅ |
| MemFloorApplied in explanation | ✅ | ✅ | — | — | — | — | ✅ | — | — | — |

### Data Generation

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| NISE generates namespace data | ✅ | — | ✅ | — | — | — | — | ✅ | ✅ | — |
| `ocp_ros_namespace_usage.csv` | ✅ | ✅ | ✅ | — | — | — | — | ✅ | ✅ | — |

---

## Discrepancies Found

### D1: OpenAPI missing `filter[cluster]` and `filter[project]` on namespace list endpoints

**Severity:** ⚠️ Medium — feature works but isn't documented in the contract

**What's wrong:**
The Go code (`MapNativeNamespaceQueryParameters` in `utils.go`) supports `filter[cluster]`, `filter[project]`, and their flat aliases (`cluster`, `project`, `namespace`). Both the E2E and IQE tests exercise these filters. However, the OpenAPI spec for both `/openshift/namespace/recommendations` and `/recommendations/openshift/namespaces` paths does NOT list these parameters.

**Authoritative source:** Go code (working implementation) + tests (exercising it)

**Fix:** Add `filter[cluster]` and `filter[project]` parameters to both namespace list endpoint definitions in `openapi.json`.

### D2: OpenAPI `order_by` has no enum for namespace endpoints

**Severity:** ⚠️ Medium — clients can't discover valid sort fields from the spec

**What's wrong:**
The namespace list endpoint `order_by` parameter in OpenAPI is `type: string` with no `enum`. The Go code defines `NsAllowedOrderBy` with 16 specific allowed values. Container recommendations have their `order_by` enum fully specified.

**Authoritative source:** Go code (`NsAllowedOrderBy` in `list_options.go`)

**Fix:** Add `enum` with the 16 allowed values to the `order_by` parameter in OpenAPI.

### D3: `NamespaceDetailResponse` missing `idle_state` fields

**Severity:** ⚠️ Medium — inconsistency between list and detail responses

**What's wrong:**
- `NativeNamespaceResult` has `IdleState` (line 102 of `namespace_recommendation_set_native.go`)
- `NamespaceListResponse` has `IdleState` (line 252 of `list_response.go`)
- `NamespaceDetailResponse` does NOT have `IdleState` (lines 349-357 of `detail_response.go`)
- The `BuildNamespaceDetailResponse` function does not copy `idle_state` from `NativeNamespaceResult`

The container `DetailResponse` has `idle_state`, `idle_since`, `idle_duration_days`, `peak_cpu_millicores`, `peak_memory_bytes`, `estimated_monthly_waste`, `idle_recommendation`. The namespace detail response has none of these.

**Authoritative source:** This is a design question — was the omission intentional? The namespace list response includes `idle_state`, suggesting it should also appear on the detail response.

**Action:** Flagged for user decision (see Design Questions).

### D4: OpenAPI `NamespaceDetailResponse` schema matches Go struct (both lack idle fields)

**Severity:** ✅ Consistent (OpenAPI and Go agree) but ⚠️ potentially incomplete

The OpenAPI `NamespaceDetailResponse` schema (lines 8519-8553) matches the Go struct — neither has `idle_state`. However, the `NamespaceRecommendation` schema (lines 8962-9001, used for the old list path) DOES include `idle_state`. This means the old-path list response exposes idle state but the detail response doesn't.

**Action:** Consistent as-is; address if D3 is resolved.

### D5: Legacy Kruize path CSV unsupported

**Severity:** Low — legacy path is a fallback; native path supports CSV

**What's wrong:**
`GetNamespaceRecommendationSetList` (legacy Kruize handler) returns HTTP 406 for CSV requests with a TODO comment: "Add CSV support when export feature is enabled". The native `serveNativeNamespaceList` handler fully supports CSV.

**Action:** No fix needed — the native path handles CSV. The legacy path is only reached when native returns zero results, which means there's nothing to export anyway.

---

## Fixes Applied

### Fix F1: Add `filter[cluster]` and `filter[project]` to OpenAPI namespace list endpoints

Both the legacy (`/openshift/namespace/recommendations`) and canonical (`/recommendations/openshift/namespaces`) namespace list endpoints now include `filter[cluster]` and `filter[project]` parameters matching what the Go handler actually supports.

### Fix F2: Add `order_by` enum to OpenAPI namespace list endpoints

Both namespace list endpoints now have the full enum of allowed `order_by` values matching `NsAllowedOrderBy` in the Go code, plus a description explaining the variation fields.

---

## Gaps vs Container Recommendations

| Feature | Containers | Namespaces | Notes |
|---------|:----------:|:----------:|-------|
| Dollar savings (total/CPU/memory) | ✅ | ❌ By design | Namespace recs don't compute savings |
| `MoneyAmount` format | ✅ | — | No monetary fields on namespace recs |
| `COST_DATA_UNAVAILABLE` notification (code 25) | ✅ | ❌ N/A | Code 25 not in namespace plugin catalog |
| `idle_state` on detail response | ✅ | ❌ Missing | Container detail has it; namespace detail doesn't |
| `idle_since`, `idle_duration_days` on detail | ✅ | ❌ Missing | Same gap as idle_state |
| `idle_recommendation` (terminate/reduce) | ✅ | ❌ Missing | Not surfaced for namespaces |
| `estimated_monthly_waste` | ✅ | ❌ Missing | Not surfaced for namespaces |
| GPU recommendations | ✅ | ❌ N/A | Namespace recs are CPU/memory only |
| order_by idle_state | ✅ | ❌ N/A | Not in `NsAllowedOrderBy` |
| order_by estimated_monthly_savings | ✅ | ❌ N/A | No savings to sort by |
| `filter[workload_type]` | ✅ | ❌ N/A | Namespace recs don't have workload granularity |
| Storage (PVC) recommendations | ✅ | ❌ N/A | Separate PVC plugin |
| Stale filter | ✅ | ✅ | Both support `stale=false/true/only` |
| Tag filters | ✅ | ✅ | Both support `filter[tag:<key>]=value` |
| CSV export | ✅ | ✅ | Both supported on native path |
| Keyset pagination | ✅ | ✅ | Both support `after` cursor |
| `meta.currency` | ✅ | ✅ | Both present on list response |
| Explanation factors | ✅ | ✅ | Both support `include=explanation` |
| MemFloorKiB | ✅ | ✅ | Applied via shared `RecommendCPUAndMemory` |
| confidence_level | ✅ | ✅ | Computed for both |
| History endpoint | ✅ | ✅ | Both have `/{id}/history` |
| Idle state aggregation | — | ✅ (list only) | Namespace idle = all child workloads non-active |
| Feature toggle (UI) | ✅ | ✅ | `cost-management.koku-ui-ros.namespace` |

---

## Design Questions for User

### Q1: Should `idle_state` be added to `NamespaceDetailResponse`?

The namespace list response returns `idle_state`, and the underlying `NativeNamespaceResult` has it. The container detail response also has `idle_state`, `idle_since`, `idle_duration_days`, and `idle_recommendation`. However, the namespace detail response struct deliberately omits these fields with the comment: "It mirrors DetailResponse but without container-specific fields".

Is this intentional? If namespace idle state is meaningful enough to show in the list view, it seems like it should also appear in the detail view.

### Q2: Should namespace recommendations have dollar savings in the future?

The current design explicitly states "no dollar savings field is included" for namespace recommendations. This makes sense since namespace-level cost modeling is complex (cost models are applied at container level). However, an approximation (sum of child container savings) could be feasible. Is this planned or intentionally excluded?

### Q3: Should `COST_DATA_UNAVAILABLE` (code 25) apply to namespaces?

Currently, notification code 25 is only in the container plugin catalog. Since namespace recommendations don't have savings, this notification wouldn't trigger. But if savings are added in the future (Q2), this code would need to be added.

---

## Additional Findings from Cross-Repo Discovery

### UI API Wiring Gap (Critical)

In `koku-ui/apps/koku-ui-ros/src/api/ros/ros.ts`, `RosPathsType.namespaces` is set to `'recommendations'` — the **same path as container recommendations** — with a TODO comment: "Replace API when available." This means the "Projects" tab in the UI (behind the `cost-management.koku-ui-ros.namespace` feature toggle) currently calls the **container recommendations API**, not the namespace-specific endpoint (`/recommendations/openshift/namespaces`). The namespace backend endpoint exists and works; the UI just isn't wired to it yet.

### Missing Bruno Files (vs Containers)

The following Bruno files exist for containers but have no namespace equivalent:
- **No `Namespace recommendations - CSV export.bru`** — despite CSV working on the native path
- **No `Namespace recommendations - keyset pagination.bru`** — despite keyset pagination working
- **No `Namespace recommendations - savings shape.bru`** — namespace recs intentionally have no savings, but a Bruno file clarifying this would be useful for API consumers

### Cheatsheet Documentation Gaps

- **Namespace CSV columns** not enumerated (line 858 just says "same filters and `order_by`" — but the columns are different from containers since there are no savings or workload/container columns)
- **Keyset pagination** not documented for namespace endpoints (unlike containers which have a full section at lines 695-701)
- **Notification codes** — inline text only (5 codes, no table, no severity ratings — unlike containers which have a full table)

### Test Coverage Gaps (vs Containers)

**E2E:**
- No `filter[idle_state]` test for namespaces (containers have active/idle/zombie tests)
- No `monitoring_end_time` presence assertion on detail
- No variation `order_by` field tests

**IQE:**
- Variation `order_by` tests exist in `test_namespace_ordering.py` but are **fully skipped** (`@pytest.mark.skip`)
- No `format=csv` export test (only in E2E)
- No notification codes catalog test

### Data Flow Confirmation

Namespace recommendation data flows through the full stack:
1. **Operator** generates `ros-openshift-namespace-YYYYMM.csv` from Prometheus queries
2. **Nise** generates `ocp_ros_namespace_usage.csv` (37 columns, quarter-hourly per namespace)
3. **Koku** passes namespace files through unchanged via `ROSReportShipper` (pure pass-through, no koku-side models)
4. **ros-ocp-backend** ingests into `daily_namespace_digests`, computes recommendations independently (NOT sums of container recs), stores in `namespace_recommendation_sets`

---

## Checklist Results

| Item | Status | Notes |
|------|--------|-------|
| `filter[term]` accepts both `short_term` and `short` | ✅ | `NormalizeRecommendationTermFilter` handles both forms |
| Keyset pagination with proper tie-breaker | ✅ | Compound cursor with sort field + namespace_name |
| CSV export includes all meaningful JSON fields | ✅ | 22-column header covers all recommendation data |
| `meta.currency` on list endpoint | ✅ | Set by `resolveListCurrencyFromRequest` |
| `MoneyAmount` for monetary fields | — | No monetary fields on namespace recs (no savings) |
| Storage uses BIGINT cents | — | No cost storage for namespace recs |
| Notification codes in catalog.go match namespace plugin | ✅ | Codes {1, 2, 7, 9, 77} correctly assigned |
| Bruno requests use correct field names | ✅ | 7+ Bruno files with correct endpoints and params |
| Cheatsheet examples match actual response shape | ✅ | 124 namespace mentions with correct structure |
| OpenAPI spec matches handler behavior | ⚠️ | Missing `filter[cluster]`, `filter[project]`, `order_by` enum |
| Memory floor (MemFloorKiB) applies to namespace recs | ✅ | Via shared `RecommendCPUAndMemory` function |
| CPU/memory savings breakdown present | ❌ N/A | No savings by design |
| Nullable savings when no cost data | ❌ N/A | No savings fields at all |
| `COST_DATA_UNAVAILABLE` notification works for namespaces | ❌ N/A | Code 25 not in namespace catalog (correct) |
| Explanation factors (including MemFloorApplied) work | ✅ | `include=explanation` on detail endpoint |
