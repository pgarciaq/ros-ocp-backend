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

### 3. ~~`estimated_monthly_savings` is functional for containers but undocumented in OpenAPI spec~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `"estimated_monthly_savings"` to the `order_by` enum in `openapi.json` for the `/recommendations/openshift` endpoint.

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

### 6. ~~`tags` field returned by API but absent from OpenAPI spec~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `tags` property (object with string additionalProperties) to the `RecommendationListItem` schema in `openapi.json`.

### 7. ~~`exclude[...]` and `filter[exact:...]` syntaxes work but are undocumented~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Already documented — `exclude[...]` and `filter[exact:...]` parameters were present in `openapi.json` at lines 56-181. No additional changes needed.

### 8. ~~No integration test for container list sorted by `estimated_monthly_savings`~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `TestGetNativeRecommendationSetList_OrderByEstimatedMonthlySavings` in `handlers_integration_test.go` — seeds 3 containers with different savings, asserts correct descending and ascending order.

---

## MEDIUM

### 9. ~~`business_hours` detail returns empty `limits` object even when a reason is present~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Changed `businessHoursToDetail()` to only set `Limits` when at least one of CPULimitMillicores or MemLimitKiB is non-nil. Updated tests to verify limits are omitted when empty and present when populated.

### 10. No CrashLoopBackOff / high-restart detection or annotation

- **Gap**: The engine has no concept of pod restart count or container lifecycle stability. A container in CrashLoopBackOff (running for seconds, restarted 100+ times) will still receive recommendations based on its brief usage spikes, which are not representative of steady-state behavior. No notification or flag warns users that the recommendation may be unreliable.
- **File**: `internal/engine/recommend_all.go`, `internal/engine/recommend_cpu_and_memory.go` (no restart-count input)
- **Fix**: If restart count data is available from the operator's CSV (it isn't currently), add a notification code. Otherwise, document this as a known limitation. Consider using very short `monitoring_end_time - monitoring_start_time` as a proxy for instability.

### 11. ~~Explanation fields are all pointer-typed in API — all can be `null` without documentation~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `nullable: true` to all pointer-typed fields in the `ContainerExplanation` schema in `openapi.json`. Also added missing fields from the Go struct (usage percentiles, trend slopes, mem_floor_applied).

### 12. Partial savings ambiguity — zero CPU usage yields zero CPU savings even with memory savings

- **Gap**: `EffectiveRateMicroCentsPerMCHour` returns 0 when `requestCoreHours <= 0` (savings_int.go line 50). If a container has real memory usage (and thus memory savings potential) but zero CPU requests/usage, the total `estimated_monthly_savings` will only reflect memory savings. The API shows a single aggregate savings number with no breakdown by resource, making it impossible to tell whether savings are partial or complete.
- **File**: `internal/engine/savings_int.go` lines 49-53 (CPU), lines 56-61 (memory), `internal/engine/savings.go` (aggregate)
- **Fix**: Consider adding `cpu_savings` and `memory_savings` breakdown fields to the detail response, or add a notification when one dimension returns zero due to missing data.

### 13. ~~`filter[container]` is documented nowhere in the OpenAPI spec but works~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Already documented — `filter[container]` was present in `openapi.json` at line 201. No additional changes needed.

---

## LOW

### 14. ~~Filter aliases (`cluster_uuid`, `namespace`) work but aren't documented~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Updated `filter[cluster]` and `filter[project]` descriptions in `openapi.json` to mention `filter[cluster_uuid]` and `filter[namespace]` as accepted aliases.

### 15. ~~No explicit test for savings computation with zero configured rates~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Added `TestApplySavingsEstimates_ZeroConfiguredRates` in `savings_test.go` — verifies zero savings (no panic/NaN) when cost data has zero rate values.

### 16. ~~OpenAPI `RecommendationListItem` vs `ContainerRecommendationDetail` field alignment drift~~ **FIXED**

- **Status**: Fixed in `pgarciaq-rosocp-superpowers-phase14`.
- **Resolution**: Audited both schemas against Go structs (`ListResponse`, `DetailResponse`). Schemas already accurately reflect each endpoint's output — differences are intentional (list has `tags`, detail does not; list aggregates `notification_codes`, detail puts notifications per-engine).
