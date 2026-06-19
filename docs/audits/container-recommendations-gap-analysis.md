# Container Recommendations Gap Analysis

**Date:** 2026-06-19
**Scope:** Full audit of container recommendations across API, engine, ingestion, tests, and UI.

---

## CRITICAL

### 1. Silent zero savings when `KOKU_MASU_URL` is not configured

- **Gap**: When `KOKU_MASU_URL` is empty (the default), the system uses `NilCostDataProvider` which returns zero-valued `ClusterCostData`. All savings estimates silently become `$0.00` with no notification in the API response distinguishing "no savings data available" from "genuinely zero savings." Users/testers will see `estimated_monthly_savings: {value: "0.00", units: "USD"}` with no indication that the configuration is broken.
- **File**: `internal/costdata/provider.go` lines 175-189, `internal/engine/savings_recalculate.go` lines 277-285, `internal/config/config.go` line 620 (default empty)
- **Fix**: Add a notification code (e.g. `NotifNoCostData`) to the recommendation response when `NilCostDataProvider` is active, or return `null` for savings instead of `0.00`. At minimum, add a startup warning log.

### 2. ~~No memory floor — zero-usage containers get 0 KiB memory recommendation~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `MemFloorKiB` (default 4096 KiB = 4 MiB) to `SizingThresholdSettings`. The floor is applied via `applyFloor()` in both `RecommendMemory` and `RecommendCPUAndMemory` after OOM bump, before limit computation. A `MemFloorApplied` explanation factor records when the floor was triggered. Configurable via `ROS_CONTAINER_MEM_FLOOR_KIB` / `ROS_NAMESPACE_MEM_FLOOR_KIB` env vars and the Settings API.

### 3. `estimated_monthly_savings` is functional for containers but undocumented in OpenAPI spec

- **Gap**: The container list endpoint's `order_by` enum in `openapi.json` (lines 361-385) does NOT include `estimated_monthly_savings`, but the backend code at `list_options.go` line 94 maps it to `recommendation_sets.estimated_savings_cents`. API consumers relying on the spec will not discover this sort option. Conversely, the spec documents `estimated_monthly_waste` which IS in the code. The inconsistency means testers can't sort by the most business-relevant field without trial and error.
- **File**: `openapi.json` lines 361-385 (enum), `internal/api/listoptions/list_options.go` line 94 (`ContainerAllowedOrderBy`)
- **Fix**: Add `"estimated_monthly_savings"` to the `order_by` enum in the OpenAPI spec for `/recommendations/openshift`.

---

## HIGH

### 4. UI TypeScript `id` field typed as `number`, backend returns `string` (UUID)

- **Gap**: `koku-ui/apps/koku-ui-ros/src/api/ros/ros.ts` line 15 defines `id?: number;`. The backend's `ListResponse.ID` and `DetailResponse.ID` are both `string` (UUID format like `"a1b2c3d4-..."`). JavaScript's `typeof "uuid" === "string"` will break any numeric comparisons or `.toFixed()` calls the UI might make.
- **File**: `koku-ui/apps/koku-ui-ros/src/api/ros/ros.ts` line 15, `ros-ocp-backend/internal/model/list_response.go` line 14
- **Fix**: Change to `id?: string;` in the TypeScript interface.

### 5. UI TypeScript `replicas` interface missing `avg` and `source` fields

- **Gap**: The backend's `ReplicaInfo` struct (detail_response.go lines 69-76) returns `avg` (average observed replicas) and `source` (data source). The UI TypeScript interface only defines `min`, `max`, `desired`, `available`. The UI cannot display or use average replica count or data provenance.
- **File**: `koku-ui/apps/koku-ui-ros/src/api/ros/recommendations.ts` lines 90-95, `ros-ocp-backend/internal/model/detail_response.go` lines 69-76
- **Fix**: Add `avg?: number;` and `source?: string;` to the `replicas` interface.

### 6. `tags` field returned by API but absent from OpenAPI spec

- **Gap**: `ListResponse` struct has `Tags map[string]string` (list_response.go line 35) which is serialized in every list response. The `RecommendationListItem` schema in `openapi.json` does not include this field. API consumers and code generators won't know it exists.
- **File**: `internal/model/list_response.go` line 35, `openapi.json` lines 8349-8437
- **Fix**: Add `"tags": {"type": "object", "additionalProperties": {"type": "string"}}` to the `RecommendationListItem` schema.

### 7. `exclude[...]` and `filter[exact:...]` syntaxes work but are undocumented

- **Gap**: The code in `internal/api/queryparams/queryparams.go` (lines 20-21) defines `FilterExactPrefix = "filter[exact:"` and `ExcludePrefix = "exclude["`. These are functional (used in `MapQueryParameters` via `applyParamFilter`), but `openapi.json` documents neither syntax for the container endpoint. Testers wanting exact-match or exclusion filters won't know they exist.
- **File**: `internal/api/queryparams/queryparams.go` lines 18-21, `internal/api/utils.go` lines 180-197, `openapi.json` (no mention of `exclude` or `exact:`)
- **Fix**: Document `exclude[project]`, `exclude[workload]`, etc. and `filter[exact:project]` in the OpenAPI spec parameters section, or explicitly remove support and return 400.

### 8. No integration test for container list sorted by `estimated_monthly_savings`

- **Gap**: Despite being a key sort field for the primary use case (finding highest-savings containers), there is no test in `internal/api/*_test.go` that exercises `order_by=estimated_monthly_savings` on the container list endpoint. Node and PVC endpoints have savings-related test coverage, but the container endpoint — the primary one — does not.
- **File**: `internal/api/handlers_integration_test.go` (no test with `order_by=estimated_monthly_savings` for container path)
- **Fix**: Add an integration test that seeds 2+ containers with different savings values and asserts correct descending/ascending order.

---

## MEDIUM

### 9. `business_hours` detail returns empty `limits` object even when a reason is present

- **Gap**: `businessHoursToDetail()` in `detail_response.go` (lines 282-297) always initializes `Limits: DetailResourcePair{}` (line 289), which serializes to `{"limits": {"cpu": null, "memory": null}}`. When the engine provides a `Reason` (e.g., "insufficient data") but no limits, the response has an uninformative empty limits block alongside the explanation text.
- **File**: `internal/model/detail_response.go` lines 282-297
- **Fix**: Use `omitempty` or conditionally omit `Limits` when both CPU and Memory are nil, so the JSON only shows `{"reason": "..."}` without the misleading empty limits.

### 10. No CrashLoopBackOff / high-restart detection or annotation

- **Gap**: The engine has no concept of pod restart count or container lifecycle stability. A container in CrashLoopBackOff (running for seconds, restarted 100+ times) will still receive recommendations based on its brief usage spikes, which are not representative of steady-state behavior. No notification or flag warns users that the recommendation may be unreliable.
- **File**: `internal/engine/recommend_all.go`, `internal/engine/recommend_cpu_and_memory.go` (no restart-count input)
- **Fix**: If restart count data is available from the operator's CSV (it isn't currently), add a notification code. Otherwise, document this as a known limitation. Consider using very short `monitoring_end_time - monitoring_start_time` as a proxy for instability.

### 11. Explanation fields are all pointer-typed in API — all can be `null` without documentation

- **Gap**: `ContainerExplanationAPI` (explanation_api.go lines 4-26) uses `*int64`, `*float32`, `*bool` for every field. While the `buildContainerExplanationAPI` function always populates them from the internal struct, the JSON schema declares them with no `nullable` annotation. Consumers can't distinguish "field is 0" from "field is null/omitted."
- **File**: `internal/model/explanation_api.go` lines 4-26, `openapi.json` (explanation schema lacks nullable annotations)
- **Fix**: Either mark all explanation fields as `nullable: true` in the OpenAPI spec, or switch to non-pointer types with `omitempty` removed to guarantee they're always present.

### 12. Partial savings ambiguity — zero CPU usage yields zero CPU savings even with memory savings

- **Gap**: `EffectiveRateMicroCentsPerMCHour` returns 0 when `requestCoreHours <= 0` (savings_int.go line 50). If a container has real memory usage (and thus memory savings potential) but zero CPU requests/usage, the total `estimated_monthly_savings` will only reflect memory savings. The API shows a single aggregate savings number with no breakdown by resource, making it impossible to tell whether savings are partial or complete.
- **File**: `internal/engine/savings_int.go` lines 49-53 (CPU), lines 56-61 (memory), `internal/engine/savings.go` (aggregate)
- **Fix**: Consider adding `cpu_savings` and `memory_savings` breakdown fields to the detail response, or add a notification when one dimension returns zero due to missing data.

### 13. `filter[container]` is documented nowhere in the OpenAPI spec but works

- **Gap**: `MapQueryParameters` at utils.go line 195 applies `applyParamFilter(c, queryParams, "container", ...)`. This means `?filter[container]=my-app` works. But the OpenAPI spec's parameter list (lines 210-513) does not list `filter[container]` as a valid query parameter. Testers won't discover this filter from the spec.
- **File**: `internal/api/utils.go` line 195, `openapi.json` (parameters section, lines 210-513)
- **Fix**: Add `filter[container]` to the documented parameters in the OpenAPI spec.

---

## LOW

### 14. Filter aliases (`cluster_uuid`, `namespace`) work but aren't documented

- **Gap**: `queryparams.go` line 59 maps `"cluster"` to also accept `"cluster_uuid"`, and line 62 maps `"project"` to also accept `"namespace"`. These legacy aliases work in the code but aren't documented in the spec, creating a wider undocumented API surface.
- **File**: `internal/api/queryparams/queryparams.go` lines 57-68
- **Fix**: Either document the aliases or deprecate them with a migration path. Low priority since canonical names work.

### 15. No explicit test for savings computation with zero configured rates

- **Gap**: While `TestApplySavingsEstimates_NilCostData` tests nil cost data, and `TestApplySavingsEstimates_ZeroUsageHours` tests zero usage hours, there's no test for the scenario where `ClusterCostData` is non-nil but has `ConfiguredRates` with explicitly zero `cpu_core_per_hour` and `memory_gb_per_hour`. The code would correctly produce zero savings, but the absence of a dedicated test means this behavior isn't documented or regression-protected.
- **File**: `internal/engine/savings_test.go`
- **Fix**: Add `TestApplySavingsEstimates_ZeroConfiguredRates` testing the scenario where the Koku API returns valid cost data but with zero rate values.

### 16. OpenAPI `RecommendationListItem` vs `ContainerRecommendationDetail` field alignment drift

- **Gap**: The list and detail schemas in the OpenAPI spec have minor structural differences that aren't strictly necessary (e.g., list has `peak_memory_bytes` at top level, detail nests it differently). While not a bug, it creates confusion for spec-driven code generators about what fields appear where.
- **File**: `openapi.json` schemas `RecommendationListItem` (lines 8349-8437) vs `ContainerRecommendationDetail` (lines 8856-8990)
- **Fix**: Audit both schemas for intentional vs. accidental structural differences and align or document the rationale.
