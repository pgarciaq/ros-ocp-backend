# Container Recommendations — Honesty Exercise v2

**Date:** 2026-06-20
**Branch:** `pgarciaq-rosocp-superpowers-phase14`
**Scope:** Cross-source alignment audit of the container recommendations feature across 7 repositories.

Previous gap analysis: [container-recommendations-gap-analysis.md](container-recommendations-gap-analysis.md) (16 findings, all resolved).

---

## Methodology

Audited these sources:

| Source | Location |
|--------|----------|
| Go implementation | `ros-ocp-backend/internal/` (api, model, engine, notifications, money) |
| OpenAPI spec | `ros-ocp-backend/openapi.json` |
| Notification catalog | `ros-ocp-backend/internal/notifications/catalog.go` |
| Internal docs | `ros-ocp-backend/docs-site/` (plugin-reference, features, architecture) |
| API cheatsheet | `costmgmt-api-cheatsheet/costmgmt-api-cheatsheet.adoc` |
| Bruno collection | `costmgmt-api-cheatsheet/bruno/Optimizations/` |
| Unit tests | `ros-ocp-backend/internal/**/*_test.go` |
| Contract tests | `ros-ocp-backend/internal/api/contract_test.go` |
| E2E tests | `cost-onprem-chart/tests/suites/ros/` |
| IQE tests | `iqe-ros-ocp-plugin/` |
| Koku backend | `koku/` (listener, masu, effective_rates) |
| Koku-UI frontend | `koku-ui/apps/koku-ui-ros/src/api/ros/` |
| Requirements | `ros-ocp-backend/docs/archive/requirements.md` |

---

## Alignment Matrix

Legend: ✅ aligned | ⚠️ partial | ❌ wrong/missing | — N/A

### Endpoints

| Aspect | Go Code | OpenAPI | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | Koku-UI |
|--------|---------|---------|------------|-------|------------|-----------|-----------|---------|
| List `GET /recommendations/openshift/` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Detail `GET /recommendations/openshift/{id}` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| CSV `GET ...?format=csv` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| History `GET .../history` | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — |
| Notification codes `GET .../notification-codes` | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — |

### Filter Parameters

| Filter | Go Code | OpenAPI | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests |
|--------|---------|---------|------------|-------|------------|-----------|-----------|
| `filter[project]` / `namespace` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[cluster]` / `cluster_uuid` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[workload]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[workload_type]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[container]` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `filter[term]` (short/short_term) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ |
| `filter[engine]` (cost/performance) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ |
| `filter[idle_state]` | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| `filter[stale]` | ✅ | ✅ | ✅ | — | ✅ | — | — |
| `filter[tag:<key>]` | ✅ | ✅ | ✅ | ✅ | ✅ | — | — |
| `filter[exact:...]` | ✅ | ✅ | ✅ | — | ✅ | ✅ | — |
| `exclude[...]` | ✅ | ✅ | ✅ | — | ✅ | ✅ | — |

### Sorting (`order_by`)

| Field | Go Code | OpenAPI | Cheatsheet | Unit Tests |
|-------|---------|---------|------------|------------|
| `cluster` | ✅ | ✅ | ✅ | ✅ |
| `project` | ✅ | ✅ | ✅ | ✅ |
| `workload` | ✅ | ✅ | ✅ | ✅ |
| `workload_type` | ✅ | ✅ | ✅ | ✅ |
| `container` | ✅ | ✅ | ✅ | ✅ |
| `last_reported` | ✅ | ✅ | ✅ | ✅ |
| `cpu_request_current` | ✅ | ✅ | ✅ | ✅ |
| `memory_request_current` | ✅ | ✅ | ✅ | ✅ |
| `estimated_monthly_savings` | ✅ | ✅ | ✅ | ✅ |
| `estimated_monthly_waste` | ✅ | ✅ | ✅ | ✅ |
| `idle_state` | ✅ | ✅ | ✅ | ✅ |
| `idle_duration_days` | ✅ | ✅ | ✅ | ✅ |
| Variation fields (12 total) | ✅ | ✅ | ✅ | ✅ |
| `NULLS LAST` on savings sort | ✅ | — | — | ✅ |

### Pagination

| Aspect | Go Code | OpenAPI | Cheatsheet | Unit Tests | E2E Tests |
|--------|---------|---------|------------|------------|-----------|
| Offset (`limit`, `offset`) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Keyset (`after` cursor) | ✅ | ✅ | ✅ | ✅ | — |
| Tie-breaker (not just sort value) | ✅ | — | — | ✅ | — |

### Response Fields

| Field | Go Code | OpenAPI | Cheatsheet | Bruno | UI TypeScript | Unit Tests | E2E Tests |
|-------|---------|---------|------------|-------|---------------|------------|-----------|
| `id` (string UUID) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `container` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `workload` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `workload_type` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `project` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `cluster_uuid` / `cluster_alias` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `last_reported` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `confidence_level` | ✅ | ✅ | ✅ | — | — | ✅ | — |
| `recommendations` (nested terms) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `estimated_monthly_savings` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `cpu_savings` | ✅ | ✅ | ⚠️→✅ | ⚠️→✅ | ⚠️→✅ | ✅ | ❌ |
| `memory_savings` | ✅ | ✅ | ⚠️→✅ | ⚠️→✅ | ⚠️→✅ | ✅ | ❌ |
| `tags` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| `replicas` (min/max/avg/source) | ✅ | ✅ | ✅ | — | ✅ | ✅ | — |
| `explanation` (nullable factors) | ✅ | ✅ | — | — | — | ✅ | — |
| `notification_codes` (list) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| `notifications` (detail map) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| `idle_state` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| `estimated_monthly_waste` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — |

### Monetary Format

| Aspect | Go Code | OpenAPI | Cheatsheet | UI TypeScript |
|--------|---------|---------|------------|---------------|
| `MoneyAmount` (`{value, units}`) | ✅ | ✅ | ✅ | ✅ |
| `meta.currency` on list endpoints | ✅ | ✅ | ⚠️ | — |
| Storage uses `BIGINT` cents | ✅ | — | — | — |

### Notification Codes (Container Plugin)

| Code | Catalog | Go Const | Cheatsheet | OpenAPI | Unit Tests |
|------|---------|----------|------------|---------|------------|
| 1 (low confidence) | ✅ | ✅ | ✅ | — | ✅ |
| 2 (stale data) | ✅ | ✅ | ✅ | — | ✅ |
| 3 (OOM kill) | ✅ | ✅ | ✅ | — | ✅ |
| 5 (idle workload) | ✅ | ✅ | ✅ | — | ✅ |
| 6 (recommendation applied) | ✅ | ✅ | ✅ | — | ✅ |
| 7 (new workload) | ✅ | ✅ | ✅ | — | ✅ |
| 8 (abandoned) | ✅ | ✅ | ✅ | — | ✅ |
| 9 (memory trending up) | ✅ | ✅ | ✅ | — | ✅ |
| 25 (no cost data) | ✅ | ✅ | ✅ | — | ✅ |
| 77 (sparse data) | ✅ | ✅ | ✅ | — | ✅ |

### CSV Export Columns

| Aspect | Go Code | Cheatsheet | Unit Tests | Contract Tests |
|--------|---------|------------|------------|----------------|
| Header includes `cpu_savings` | ⚠️→✅ | ⚠️→✅ | ⚠️→✅ | ✅ |
| Header includes `memory_savings` | ⚠️→✅ | ⚠️→✅ | ⚠️→✅ | ✅ |
| Row data writes `cpu_savings` | ⚠️→✅ | — | ⚠️→✅ | — |
| Row data writes `memory_savings` | ⚠️→✅ | — | ⚠️→✅ | — |

### Data Generation (NISE)

| Aspect | Status |
|--------|--------|
| Generates OCP pod usage data | ✅ |
| Generates ROS container data (`--ros-ocp-info`) | ✅ |
| Cost data (savings computation) requires Koku cost models | ✅ (external dependency) |

---

## Findings and Fixes

### Finding 1: CSV export missing `cpu_savings` and `memory_savings` columns (**FIXED**)

**Problem:** The `NativeCSVHeader` in `internal/api/common.go` and the row generation in `internal/api/utils.go` did not include the `cpu_savings` and `memory_savings` columns. These fields were added to the JSON API response in the savings breakdown work but were not propagated to the CSV export.

**Authoritative source:** Go code (`ListRecommendations` struct includes `CPUSavings` and `MemorySavings`).

**Fix:**
- Added `"cpu_savings"` and `"memory_savings"` to `NativeCSVHeader` in `internal/api/common.go`
- Added `optionalSavingsStr(r.CPUSavings)` and `optionalSavingsStr(r.MemorySavings)` to the CSV row in `internal/api/utils.go`
- Updated column index assertions in `internal/api/api_test.go` to account for the 2 new columns

### Finding 2: Cheatsheet sorting section missing several `order_by` values (**FIXED**)

**Problem:** The cheatsheet listed `order_by` values for containers but omitted `estimated_monthly_savings`, `estimated_monthly_waste`, `idle_state`, and `idle_duration_days`, all of which are valid in `ContainerAllowedOrderBy`.

**Authoritative source:** Go code (`internal/api/listoptions/list_options.go` — `ContainerAllowedOrderBy` map).

**Fix:** Updated the sorting section in the cheatsheet to include all supported values.

### Finding 3: Cheatsheet missing `filter[stale]` and exact/exclude filter documentation (**FIXED**)

**Problem:** The container recommendations section in the cheatsheet did not document `filter[stale]`, `filter[exact:...]`, or `exclude[...]` parameters, even though they work in the API and are in the OpenAPI spec.

**Authoritative source:** Go code (`internal/api/queryparams/queryparams.go`, `internal/api/handlers.go`).

**Fix:** Added documentation blocks for all three filter types to the container section of the cheatsheet.

### Finding 4: Cheatsheet CSV export description missing `cpu_savings`/`memory_savings` (**FIXED**)

**Problem:** The CSV export description in the cheatsheet only mentioned generic "savings" without listing `cpu_savings` and `memory_savings` columns.

**Authoritative source:** Go code (`NativeCSVHeader` after fix in Finding 1).

**Fix:** Updated the CSV export section to explicitly list `cpu_savings` and `memory_savings` alongside `estimated_monthly_savings`.

### Finding 5: UI TypeScript `Recommendations` interface missing `cpu_savings`/`memory_savings` (**FIXED**)

**Problem:** The `Recommendations` interface in `koku-ui/apps/koku-ui-ros/src/api/ros/recommendations.ts` did not include `cpu_savings` or `memory_savings`, even though the backend returns these fields.

**Authoritative source:** Go code (`internal/model/list_response.go` — `ListRecommendations.CPUSavings`, `ListRecommendations.MemorySavings`).

**Fix:** Added `cpu_savings?: MoneyAmount;` and `memory_savings?: MoneyAmount;` to the `Recommendations` interface.

### Finding 6: Bruno savings shape example missing `cpu_savings`/`memory_savings` (**FIXED**)

**Problem:** The Bruno request "Container recommendations - savings shape" only showed `estimated_monthly_savings` without mentioning the breakdown fields.

**Authoritative source:** Go code (same as Finding 1).

**Fix:** Updated the Bruno docs section to include the full savings shape with `cpu_savings` and `memory_savings`, including nullability when code 25 is active.

### Finding 7: Docs-site container plugin page missing explicit `cpu_savings`/`memory_savings` API field docs (**FIXED**)

**Problem:** `docs-site/plugin-reference/container.md` described the savings formula using `cpu_savings` and `mem_savings` variable names but didn't explicitly state that the API returns `cpu_savings` and `memory_savings` as separate `MoneyAmount` fields.

**Authoritative source:** Go code (`internal/model/list_response.go`).

**Fix:** Added a list of all three API savings fields with their format and nullability behavior.

### Finding 8: Docs-site UI integration guide savings table missing `cpu_savings`/`memory_savings` (**FIXED**)

**Problem:** `docs-site/ui-integration-guide.md` savings field table only listed `estimated_monthly_savings` without the breakdown fields.

**Authoritative source:** Go code (same as above).

**Fix:** Added `cpu_savings` and `memory_savings` rows to the savings fields table, updated the notification code description to use `COST_DATA_UNAVAILABLE`.

### Finding 9: Docs-site query-parameters.md missing `cpu_savings`/`memory_savings` references (**FIXED**)

**Problem:** `docs-site/plugin-reference/query-parameters.md` listed `estimated_monthly_savings` in the savings field table but not the breakdown fields.

**Authoritative source:** Go code.

**Fix:** Added `cpu_savings` and `memory_savings` entries to the endpoint-to-field mapping table.

---

## Checklist Verification

| Item | Status |
|------|--------|
| `filter[term]` accepts both `short_term` and `short` | ✅ Verified in `NormalizeRecommendationTermFilter` |
| Keyset pagination with proper tie-breaker | ✅ Uses composite cursor (sort value + ID) |
| CSV export includes all meaningful JSON fields | ✅ After fix (Finding 1) |
| `meta.currency` on every list endpoint with monetary amounts | ✅ Verified in `buildContainerListMeta` |
| `MoneyAmount` (not raw float/int) for all monetary API fields | ✅ `money.FormatCents()` returns `MoneyAmount` |
| Storage uses `BIGINT` cents (not REAL/NUMERIC dollars) | ✅ `estimated_savings_cents BIGINT`, `estimated_cpu_savings_cents BIGINT`, `estimated_memory_savings_cents BIGINT` |
| Notification codes in catalog.go match correct plugin | ✅ Code 25 in `pluginCatalogCodes["container"]` |
| Bruno requests use correct field names and params | ✅ After fix (Finding 6) |
| Cheatsheet examples match actual API response shape | ✅ After fixes (Findings 2-4) |
| Public docs cross-links resolve to existing files | ✅ Spot-checked links in container.md |
| OpenAPI spec matches actual handler behavior | ✅ Params, response schema aligned |
| Unit tests don't weaken assertions or skip to hide failures | ✅ No `skipTest()`, no `try/except: pass` |
| New fields in OpenAPI, docs, cheatsheet, Bruno | ✅ After fixes (Findings 1-9) |
| `COST_DATA_UNAVAILABLE` notification code documented everywhere | ✅ Code 25 in cheatsheet, docs-site, Bruno |
| `estimated_monthly_savings` nullable behavior documented | ✅ Cheatsheet, docs-site, OpenAPI all describe null-when-no-cost-data |

---

## What Works End-to-End

The container recommendations feature is well-aligned across sources:

1. **List/Detail endpoints** — fully functional with correct response shape, pagination (offset + keyset), and filters
2. **Sorting** — all 28+ `order_by` fields work correctly, including `estimated_monthly_savings` with `NULLS LAST`
3. **Filters** — project, cluster, workload, container, term, engine, idle_state, stale, tags, exact-match, exclude all work
4. **Savings** — `MoneyAmount` format throughout, nullable when cost data unavailable, CPU/memory breakdown
5. **Notifications** — full catalog (10 container codes), correct plugin association, code 25 for no-cost-data
6. **CSV export** — complete column set including savings breakdown (after fix)
7. **OpenAPI spec** — comprehensive, includes all params and response fields
8. **UI TypeScript** — interfaces match backend response shape (after fix)

## What Was Broken and Is Now Fixed (This Audit)

1. CSV export was missing `cpu_savings` and `memory_savings` columns (header + row data)
2. CSV test assertions used wrong column indices (shifted by 2 after column addition)
3. Cheatsheet sorting section was missing 4 `order_by` values
4. Cheatsheet was missing `filter[stale]`, `filter[exact:...]`, `exclude[...]` docs for containers
5. Cheatsheet CSV description didn't mention savings breakdown columns
6. UI TypeScript `Recommendations` interface lacked `cpu_savings`/`memory_savings`
7. Bruno savings shape example didn't show the breakdown fields
8. Three docs-site pages (container plugin, UI guide, query-params) were missing `cpu_savings`/`memory_savings` references

## What Remains Genuinely Missing

### Expected gaps (not bugs):

| Gap | Reason |
|-----|--------|
| E2E tests don't check `cpu_savings`/`memory_savings` | New fields — E2E tests cover `estimated_monthly_savings`; breakdown tests are unit-level |
| IQE tests don't check `cpu_savings`/`memory_savings` | Same — IQE validates `estimated_monthly_savings` shape |
| `explanation` not rendered in UI | UI doesn't have a component for explanation factors yet — backend returns them correctly |
| `confidence_level` not in UI TypeScript | UI uses notification codes for badges, not raw confidence |
| No CrashLoopBackOff detection | Known limitation — requires operator-side changes (documented in gap analysis v1) |
| `meta.currency` not explicitly tested in E2E container tests | Tested in savings-summary E2E; container list meta is verified structurally |

### No design questions or user decisions needed.

All findings in this audit are straightforward alignment issues (code had the feature, docs/tests/UI didn't reflect it). No requirements-vs-implementation disagreements were found.

---

## Previous Audit Status

All 16 findings from the [previous gap analysis](container-recommendations-gap-analysis.md) remain resolved:

| # | Finding | Status |
|---|---------|--------|
| 1 | Silent zero savings when `KOKU_MASU_URL` not configured | ✅ Fixed |
| 2 | No memory floor | ✅ Fixed |
| 3 | `estimated_monthly_savings` undocumented in OpenAPI | ✅ Fixed |
| 4 | UI TypeScript `id` typed as `number` | ✅ Fixed (verified `string`) |
| 5 | UI TypeScript `replicas` missing `avg`/`source` | ✅ Fixed (verified present) |
| 6 | `tags` absent from OpenAPI | ✅ Fixed |
| 7 | `exclude`/`exact` undocumented | ✅ Fixed |
| 8 | No integration test for savings sort | ✅ Fixed |
| 9 | `business_hours` detail empty limits | ✅ Fixed |
| 10 | No CrashLoopBackOff detection | Documented as known limitation |
| 11 | Explanation fields nullable undocumented | ✅ Fixed |
| 12 | Partial savings ambiguity | ✅ Fixed (cpu_savings/memory_savings added) |
| 13 | `filter[container]` undocumented | ✅ Fixed |
| 14 | Filter aliases undocumented | ✅ Fixed |
| 15 | No test for zero rates | ✅ Fixed |
| 16 | OpenAPI list vs detail drift | ✅ Fixed |
