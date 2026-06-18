# ros-ocp-backend-superpowers — TDD Test Plan

> **Parent:** [Requirements Document](./requirements.md)
> **Date:** 2026-03-26 (updated: 2026-04-16)
> **Strategy:** Red-Green-Refactor (strict TDD)
> **Language:** Go 1.25 (all recommendation logic, unit + integration)
> **Frameworks:** `testing` (Go stdlib), `testify/assert` + `testify/require`, `testcontainers-go` (PostgreSQL 16)
> **Architecture note:** All recommendation computation runs in Go ("read once, compute N terms" pattern). No PL/pgSQL functions. SQL is used only for schema migrations and data storage/retrieval.

---

## Table of Contents

- [1. Strategy and Principles](#1-strategy-and-principles)
- [2. Testing Pyramid](#2-testing-pyramid)
- [3. Test Infrastructure](#3-test-infrastructure)
- [4. Phase 0: Critical Fixes](#4-phase-0-critical-fixes)
- [5. Phase 1: Core Recommendation Engine](#5-phase-1-core-recommendation-engine)
- [6. Phase 2: Metrics Pipeline](#6-phase-2-metrics-pipeline)
- [7. Phase 3: Decay Weighting and Custom Timeframes](#7-phase-3-decay-weighting-and-custom-timeframes)
- [8. Phase 4: Memory with OOM Feedback](#8-phase-4-memory-with-oom-feedback)
- [9. Phase 5: GPU Recommendations](#9-phase-5-gpu-recommendations)
- [10. Phase 6: New Recommendation Types](#10-phase-6-new-recommendation-types)
- [11. Phase 7: Replica Count and Total Impact](#11-phase-7-replica-count-and-total-impact)
- [12. Phase 8: HPA, VM, Node/MachineSet](#12-phase-8-hpa-vm-nodemachineset)
- [13. Phase 9: JVM/Quarkus](#13-phase-9-jvmquarkus)
- [14. Phase 10: Legacy Kruize engine (optional path)](#14-phase-10-legacy-kruize-engine-optional-path)
- [15. Non-Functional Requirements](#15-non-functional-requirements)
- [16. Shadow Mode Validation](#16-shadow-mode-validation)
- [17. API Contract Tests](#17-api-contract-tests)
- [18. Cross-Phase Integration Tests](#18-cross-phase-integration-tests)
- [19. Performance and Load Tests](#19-performance-and-load-tests)
- [20. Test Data Catalog](#20-test-data-catalog)
- [21. Coverage Targets](#21-coverage-targets)
- [22. IQE Integration Test Plan](#22-iqe-integration-test-plan)

---

## 1. Strategy and Principles

### Red-Green-Refactor Cycle

Every feature follows this strict ordering:

1. **RED:** Write a failing test that precisely describes the expected behavior. The test must fail for the *right reason* (not a compilation error, not a missing import — the assertion itself must fail).
2. **GREEN:** Write the *minimum* code to make the test pass. No optimizations, no abstractions, no "while I'm here" extras.
3. **REFACTOR:** Improve the code structure (extract functions, rename, eliminate duplication) while keeping all tests green. No new behavior in this step.

### Rules

- **No production code without a failing test.** If there's no RED test that demands the code, don't write it.
- **One assertion per test function** (or one logical assertion group via `require` + `assert`). Test names describe the behavior, not the implementation.
- **Tests are documentation.** A reader should understand the requirement by reading only the test name and assertions, without reading the production code.
- **Test at the boundary, not the implementation.** Test the Go function's return value given known input, not its intermediate variables. Test the DB read/write results, not internal query plans.
- **Deterministic data.** All test data uses fixed dates, fixed UUIDs, fixed values. No `time.Now()` in test fixtures.
- **Each phase's tests must pass independently.** Phase N tests don't depend on Phase N-1 tests running first.

### Naming Convention

```
Test<Unit>_<Behavior>_<Condition>

Examples:
  TestRecommendCPU_CostModel_ReturnP60WeightedByDecay
  TestRecommendCPU_IdleFlag_WhenPerfPctBelow10mc
  TestParseCSV_ConvertsCoresToMillicores_RoundsToNearest
  TestAPI_ListRecommendations_FiltersByOrgID
```

---

## 2. Testing Pyramid

```
                    ┌──────────────┐
                    │   E2E (few)  │  Full Kafka→Go→PG→API pipeline
                   ┌┴──────────────┴┐
                   │  Go Integration │  Go ↔ PostgreSQL 16 (testcontainers)
                  ┌┴────────────────┴┐
                  │  Go Rec. Unit     │  recommendCPU(), recommendMemory(), etc.
                 ┌┴──────────────────┴┐
                 │  Go Unit Tests      │  Pure Go, no DB, no I/O
                └──────────────────────┘
```

| Layer | Count (est.) | Speed | What it tests |
|---|---|---|---|
| **Go Unit** | ~250 | < 1s each | CSV parsing, integer conversion, percentile computation, API response assembly, notification mapping, config validation |
| **Go Rec. Unit** | ~120 | < 1s each | Go recommendation functions (`recommendCPU`, `recommendMemory`, `detectIdle`, `recommendNamespace`, etc.) with in-memory digest data — no DB |
| **Go Integration** | ~80 | < 5s each | Full ingestion path (CSV → Go aggregation → PG upsert → Go "read once, compute N terms" → result verification), RBAC, Kafka consumer, circuit breaker |
| **E2E** | ~15 | < 30s each | Kafka message → S3 download → ingestion → recommendation → API response, shadow mode reconciliation |

**Target:** 100% of RED-GREEN cycles at the two lowest layers. Integration and E2E confirm wiring.

---

## 3. Test Infrastructure

### 3.1 PostgreSQL 16 via testcontainers-go

```go
func SetupTestDB(t *testing.T) *pgxpool.Pool {
    t.Helper()
    ctx := context.Background()
    container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:16",
            ExposedPorts: []string{"5432/tcp"},
            Env:          map[string]string{"POSTGRES_PASSWORD": "test", "POSTGRES_DB": "ros_test"},
            WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(30 * time.Second),
        },
        Started: true,
    })
    require.NoError(t, err)
    t.Cleanup(func() { container.Terminate(ctx) })

    // Run golang-migrate migrations
    RunMigrations(t, connString)

    pool, err := pgxpool.New(ctx, connString)
    require.NoError(t, err)
    t.Cleanup(pool.Close)
    return pool
}
```

**pg_partman:** Tests create partitions explicitly (no `pg_partman` in test container to keep tests hermetic). Production partition management is tested in dedicated NFR-8 integration tests.

### 3.2 Fixture Seeding Pattern

```go
// fixtures.go — deterministic test data
const (
    TestOrgID       = "org_test_1"
    TestClusterUUID = "11111111-1111-1111-1111-111111111111"
    TestNamespace   = "production"
    TestWorkload    = "api-server"
    TestContainer   = "main"
    BaseDate        = "2026-03-01"  // all test dates relative to this
)

func SeedContainerDigest(t *testing.T, pool *pgxpool.Pool, opts ...DigestOption) {
    // Insert a daily_container_digests row with configurable overrides
}

func SeedDigestSeries(t *testing.T, pool *pgxpool.Pool, days int, opts ...DigestOption) {
    // Insert `days` consecutive rows for the same container
}
```

### 3.3 Golden File Pattern for Recommendation Functions

Go recommendation function tests use a **golden-file approach**: expected outputs are stored as JSON files in `testdata/`. When a test fails, it prints the actual output for easy comparison. To update golden files after intentional changes: `go test -update-golden ./...`

### 3.4 Mocks and Fakes

| Dependency | Test Double | Used In |
|---|---|---|
| S3 (CSV download) | In-memory `io.Reader` with test CSV data | Go unit, Go integration |
| Kafka consumer | Channel-based fake (`chan kafka.Message`) | Go integration, E2E |
| Koku cost API | HTTP test server (`httptest.Server`) returning fixed JSON | Go integration (REQ-7.5) |
| Instance catalog API | HTTP test server returning fixed JSON | Go integration (REQ-8c.6) |
| Unleash feature flags | In-memory map (`map[string]bool`) | Go unit, Go integration |
| PostgreSQL | Real container (testcontainers) — never mocked | Go integration, E2E |

---

## 4. Phase 0: Critical Fixes

> Phase 0 tests are written against the **existing codebase** (ros-ocp-backend fork), not the new superpowers binary. They verify bug fixes before any architectural changes.

### T-0.1: RBAC nil pointer (REQ-0.1)

| Step | Detail |
|---|---|
| **RED** | `TestRBACMiddleware_NilIdentity_Returns403`: Send request with no `x-rh-identity` header. Assert HTTP 403 (not panic). |
| **GREEN** | Add nil check before `identity.User.Access` dereference. |
| **REFACTOR** | Extract RBAC identity parsing into a testable `ParseIdentity(header string) (*Identity, error)` function. |

### T-0.2: RBAC strings.Split panic (REQ-0.2)

| Step | Detail |
|---|---|
| **RED** | `TestRBACParseAccess_EmptyString_ReturnsEmptySlice`: Call `parseRBACAccess("")`. Assert no panic, returns `[]string{}`. |
| **GREEN** | Guard `strings.Split` with empty-string check. |
| **REFACTOR** | None needed. |

### T-0.3: HTTP 200 on DB failure (REQ-0.3)

| Step | Detail |
|---|---|
| **RED** | `TestAPIHandler_DBError_Returns500`: Inject a DB connection that returns `sql.ErrConnDone`. Assert HTTP 500, not 200. Assert response body contains `"error"` key. |
| **GREEN** | Check `gorm.DB.Error` and map to appropriate HTTP status. |
| **REFACTOR** | Extract error-to-status mapping into a reusable `HTTPErrorFromDBError(err error) int` function. |

### T-0.4: Kafka type assertion panic (REQ-0.4)

| Step | Detail |
|---|---|
| **RED** | `TestKafkaHandler_UnexpectedMessageType_LogsAndSkips`: Send a Kafka message with an unexpected `PayloadType`. Assert no panic, assert structured warning log emitted. |
| **GREEN** | Replace bare type assertion with type switch + default case. |
| **REFACTOR** | None needed. |

### T-0.5: Kafka subscribe failure (REQ-0.5)

| Step | Detail |
|---|---|
| **RED** | `TestKafkaConsumer_SubscribeError_RetriesWithBackoff`: Inject a Kafka client that returns error on `Subscribe()`. Assert retry with exponential backoff (up to 3 attempts). Assert structured error log on each failure. |
| **GREEN** | Wrap `Subscribe()` in retry loop. |
| **REFACTOR** | Extract retry logic into generic `RetryWithBackoff(fn func() error, maxAttempts int, baseDelay time.Duration)`. |

### T-0.6: HTTP timeouts (REQ-0.6)

| Step | Detail |
|---|---|
| **RED** | `TestHTTPServer_HasReadWriteTimeouts`: Start HTTP server with production config. Assert `ReadTimeout >= 10s`, `WriteTimeout >= 30s`, `IdleTimeout >= 60s`. |
| **GREEN** | Set timeout fields on `http.Server`. |
| **REFACTOR** | Move timeout constants to config struct. |

### T-0.7: Poison message handling (REQ-0.7)

| Step | Detail |
|---|---|
| **RED** | `TestKafkaHandler_PoisonMessage_MovesToDLQ_After3Retries`: Send a message that always fails processing. Assert it is committed (not redelivered) after 3 attempts. Assert DLQ metric incremented. |
| **GREEN** | Add retry counter and DLQ logic. |
| **REFACTOR** | None needed. |

### T-0.8: GORM `.Where()` bug (REQ-0.8)

| Step | Detail |
|---|---|
| **RED** | `TestHousekeeper_DeletesOnlyExpiredRows`: Seed 5 rows (3 expired, 2 current). Run housekeeper. Assert exactly 3 deleted. |
| **GREEN** | Fix the `.Where()` clause to use correct column reference. |
| **REFACTOR** | None needed. |

### T-0.9: ISO 8601 error handling (REQ-0.9)

| Step | Detail |
|---|---|
| **RED** | `TestConvertDateToISO8601_InvalidInput_ReturnsError`: Call with `"not-a-date"`. Assert returns error (not empty string). |
| **RED** | `TestConvertDateToISO8601_ValidInput_ReturnsFormatted`: Call with `"2026-03-01"`. Assert returns `"2026-03-01T00:00:00Z"`. |
| **GREEN** | Add error return to function signature. |
| **REFACTOR** | None needed. |

### T-0.10: Non-deterministic iteration (REQ-0.10)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendationOutput_IsDeterministic_AcrossRuns`: Run recommendation assembly 100 times with the same input. Assert all outputs are byte-identical. |
| **GREEN** | Sort map keys before iteration. |
| **REFACTOR** | Replace `map` with sorted slice where iteration order matters. |

### T-0.11: Kafka payload logging (REQ-0.11)

| Step | Detail |
|---|---|
| **RED** | `TestKafkaLogger_LargePayload_Truncated`: Process a 1MB Kafka message. Capture log output. Assert no log line exceeds 1KB. |
| **GREEN** | Truncate message payload in log statements. |
| **REFACTOR** | None needed. |

### T-0.12: SendMessage failure (REQ-0.12)

| Step | Detail |
|---|---|
| **RED** | `TestSendMessage_Failure_MarksReconciliationNeeded`: Inject producer that returns error. Assert reconciliation flag is set. Assert retry is scheduled. |
| **GREEN** | Add error handling and reconciliation flag. |
| **REFACTOR** | None needed. |

---

## 5. Phase 1: Core Recommendation Engine

### 5.1 CPU Recommendation

#### T-1.1: Remove 1-core discontinuity (REQ-1.1)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_BelowOneCoreAndAbove_SameAlgorithm`: Seed digest data for container A (all values < 1000mc) and container B (all values > 1000mc). Call `recommendCPU()`. Assert both use the same percentile-based formula. Specifically: container A at p60 of {100, 200, 300, ..., 900}mc should produce `p60 × margin`, NOT `max + throttle`. |
| **GREEN** | Implement `recommendCPU()` in Go with single-path percentile algorithm. |
| **REFACTOR** | Extract percentile bucket selection into helper function `selectPercentile()`. |

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_Floor25m`: Seed digest where raw recommendation would be 12mc. Assert output is ≥ 25mc (floor). |
| **GREEN** | Add `max(25, ...)` floor in Go. |
| **REFACTOR** | Make floor value a configurable parameter. |

#### T-1.2: No per-pod estimation (REQ-1.2)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_TreatsEachRowAsContainer`: Seed 3 digest rows for same container on same day with different values. Assert recommendation uses the stored percentiles directly, not values divided by any pod count. |
| **GREEN** | Implement function that reads digest columns directly (no division). |
| **REFACTOR** | None needed — this is the absence of a bug. |

#### T-1.3: Cost and Performance models (REQ-1.3)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_CostModel_UsesP60`: Seed digest with known p60. Call `recommendCPU()` with cost percentile=0.60. Assert `cost_rec_request_mc` is derived from p60 × margin. |
| **RED** | `TestRecommendCPU_PerfModel_UsesP98`: Same digest. Assert `perf_rec_request_mc` is derived from p98 × margin. |
| **RED** | `TestRecommendCPU_DualOutput_SinglePass`: Call `recommendCPU()` once. Assert it returns both cost and perf fields in the same result struct. |
| **GREEN** | Implement percentile bucket selection in Go and dual output fields. |
| **REFACTOR** | None needed. |

#### T-1.3b: Custom percentile parameters

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_CustomPercentile_P50_UsesP50Column`: Call with cost percentile=0.50. Assert cost recommendation derives from `cpu_usage_p50_mc`. |
| **RED** | `TestRecommendCPU_CustomPercentile_P95_UsesP95Column`: Call with cost percentile=0.95. Assert derives from `cpu_usage_p95_mc`. |
| **GREEN** | Already covered by the percentile selection logic in `recommendCPU()`. |
| **REFACTOR** | None needed. |

### 5.2 Memory Recommendation

#### T-1.5: Basic memory recommendation (REQ-1.5)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendMemory_UsesMaxNotSort`: Seed digest with `memory_usage_max_kib = 4096`. Assert `perf_rec_request_kib >= 4096 × 1.15` (max × min margin). |
| **RED** | `TestRecommendMemory_AdaptiveMargin_StableWorkload`: Seed digest where `p95 ≈ p50 ≈ mean`. Assert margin is close to min (1.15). |
| **RED** | `TestRecommendMemory_AdaptiveMargin_VariableWorkload`: Seed digest where `p95 = 3 × p50`. Assert margin is close to max (1.50). |
| **GREEN** | Implement `recommendMemory()` in Go with `min(maxMargin, max(minMargin, 1.0 + (p95 - p50) / mean))`. |
| **REFACTOR** | Extract margin computation into shared `computeAdaptiveMargin()` function (used by both CPU and memory). |

#### T-1.6: Memory Cost/Performance models (REQ-1.6)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendMemory_CostModel_UsesP95`: Seed digest with known p95 and max. Assert cost uses p95-derived value. |
| **RED** | `TestRecommendMemory_PerfModel_UsesMax`: Assert performance uses `memory_usage_max_kib`-derived value. |
| **GREEN** | Implement cost percentile=0.95, perf percentile=1.0 (max) in `recommendMemory()`. |
| **REFACTOR** | None needed. |

### 5.3 Dual Model and Terms

#### T-1.7: Dual model output (REQ-1.7)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_ReturnsBothModels_InSingleResult`: Assert result struct has fields `CostRecRequestMC`, `CostRecLimitMC`, `PerfRecRequestMC`, `PerfRecLimitMC` per container. |
| **GREEN** | Already part of `recommendCPU()` return type. Test validates output shape. |
| **REFACTOR** | None needed. |

#### T-1.8: Customer-defined term support (REQ-1.8)

| Step | Detail |
|---|---|
| **RED** | `TestTerms_DefaultShortTerm_Uses1DayWindow`: Seed 30 days of digests. Compute with 1-day window. Assert only last day's data influences result. |
| **RED** | `TestTerms_DefaultMediumTerm_Uses7DayWindow`: Compute with 7-day window. Assert older data has less weight (decay). |
| **RED** | `TestTerms_DefaultLongTerm_Uses15DayWindow`: Compute with 15-day window. Assert all 15 days contribute. |
| **RED** | `TestTerms_CustomTerm_20DayWindow`: Insert `org_recommendation_terms` rows defining a 20-day term for test org. Assert 20 days of data contribute with correct decay. |
| **RED** | `TestTerms_CustomTerm_60DayWindow`: Insert `org_recommendation_terms` rows defining a 60-day term. Assert all 60 days contribute. |
| **RED** | `TestTerms_CustomTerm_90DayWindow_MaximumAllowed`: 90-day window. Assert all 90 days contribute, decay correctly applied. |
| **RED** | `TestTerms_ReadOnceComputeN_SingleDBRead`: Seed 90 days of digests. Insert `org_recommendation_terms` with terms (10d, 30d, 90d). Assert Go reads digests exactly once (single SQL query for max window), then computes all 3 terms in memory. |
| **RED** | `TestTerms_InsufficientData_ReturnsEmpty`: Seed 0 days of data. Compute for any term. Assert empty result. |
| **RED** | `TestTerms_InsufficientData_MediumTerm_ThresholdIs2Days`: Seed 1 day. Assert medium-term (7d) returns empty (< min_data_days=2). Seed 2 days. Assert returns results. |
| **RED** | `TestTerms_MinDataScaling_ProportionalToWindow`: 60-day term should require min_data_days = ceil(60/5) = 12. Seed 11 days → empty. Seed 12 days → results. |
| **RED** | `TestTerms_DecayHalfLifeScaling`: 7-day term uses halflife=3.5d. 30-day term uses halflife=15d. Assert different recommendations for same data. |
| **GREEN** | Go "read once, compute N terms" pattern: read `max(term_windows)` days of digests in one query, compute each term by slicing the in-memory data and applying term-specific decay. |
| **RED** | `TestTerms_DefaultsWhenNoOrgRows`: No rows in `org_recommendation_terms` for test org. Assert Go uses hardcoded `DefaultTerms` (1d/7d/15d) with zero DB cost. |
| **REFACTOR** | Extract term config into a `TermConfig` struct with `WindowDays`, `MinDataDays`, `DecayHalfLifeDays`. Defaults in Go constants; overrides from `org_recommendation_terms` table. |

### 5.4 Notifications

#### T-1.9: Notification system (REQ-1.9)

| Step | Detail |
|---|---|
| **RED** | `TestNotification_NotEnoughData_EmittedWhenBelowThreshold`: Container with 10 minutes of data. Assert `INFO_NOT_ENOUGH_DATA` notification for short_term. |
| **RED** | `TestNotification_NotEnoughData_SuppressesRecommendation`: Assert no recommendation row is written when below threshold. |
| **RED** | `TestNotification_IdleWorkload_EmittedAlongsideRecommendation`: Seed idle container (CPU < 10mc). Assert both `IDLE_WORKLOAD` notification AND recommendation are returned. |
| **RED** | `TestNotification_CPULimitNotSet_Informational`: Seed container with no CPU limit in metrics. Assert `WARNING_CPU_LIMIT_NOT_SET` notification returned. |
| **GREEN** | Implement notification logic in Go orchestrator (post-recommendation computation). |
| **REFACTOR** | Map notification codes to a `notification_code_definitions` DB table. |

#### T-1.9b: Namespace notification system

| Step | Detail |
|---|---|
| **RED** | `TestEvaluateNamespaceNotifications_MemoryTrendingUp`: Set `MemTrendSlope=600.0` (above namespace threshold 500 KiB/day). Assert `NotifMemoryTrendingUp` (code 9) in returned codes. |
| **RED** | `TestEvaluateNamespaceNotifications_MemoryTrendBelowThreshold`: Set `MemTrendSlope=200.0` (below threshold). Assert `NotifMemoryTrendingUp` NOT in codes. |
| **RED** | `TestEvaluateNamespaceNotifications_StillNoOOMOrIdle`: Even with high `MemTrendSlope`, assert `NotifOOMDetected` and `NotifIdleWorkload` are never emitted for namespaces. |
| **GREEN** | `EvaluateNamespaceNotifications()` checks `rec.MemTrendSlope > namespaceMemTrendSlopeThreshold` (500 KiB/day). Threshold is 5× the container threshold (100 KiB/day) because namespace-level aggregates naturally exhibit larger absolute swings. Idle detection remains excluded (not meaningful at namespace granularity). |
| **REFACTOR** | None needed; consistent with container notification pattern. |

> **Design note — idle detection and trend slope at namespace level:**
>
> `RecommendCPU()` and `RecommendMemory()` are shared functions that always compute `TrendSlope` and `IsIdle`.
> For namespaces, these values have different applicability:
>
> - **Memory trend slope**: Meaningful at namespace level. If a namespace's aggregate memory P95 is growing by > 500 KiB/day, the user should be notified. Surfaced via `NotifMemoryTrendingUp` with a 5× threshold vs containers.
> - **Idle detection**: Not applicable. The container threshold (10mc) is meaningless at namespace scale. A truly idle namespace would have no digest data in the first place. Excluded intentionally.
> - **CPU trend slope**: Computed but unused for both containers and namespaces — no `NotifCPUTrendingUp` code exists. If ever needed, the infrastructure is ready.

### 5.5 Persistence and Batch

#### T-1.10: Recommendation persistence (REQ-1.10)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_UpsertToRecommendationSets`: Call `recommendCPU()` and persist results, then query `recommendation_sets`. Assert rows exist with correct `term`, `engine`, and typed columns. |
| **RED** | `TestRecommendCPU_Idempotent_SameInputSameOutput`: Call twice with same data. Assert same row count and values (upsert, not duplicate). |
| **GREEN** | Go computes recommendations, then batch writes via `COPY FROM` + temp table + `INSERT ... ON CONFLICT DO UPDATE`. |
| **REFACTOR** | None needed. |

#### T-1.11: Batch entry point (REQ-1.11)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendAllWorkloads_CallsCPUAndMemory`: Seed digests for 3 containers. Call Go `recommendAllWorkloads()`. Assert `recommendation_sets` has 3 containers × 3 terms × 2 engines = 18 rows with both CPU and memory columns populated. |
| **GREEN** | Implement `recommendAllWorkloads()` in Go: reads digests once per cluster, computes all terms for CPU and memory, batch writes results. |
| **REFACTOR** | None needed. |

### 5.6 Namespace Recommendations

#### T-1.13: Namespace recommendations (REQ-1.13)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendNamespace_AggregatesContainerData`: Seed 3 containers in same namespace. Call Go `recommendNamespace()`. Assert result reflects namespace-level aggregates (not per-container). |
| **RED** | `TestRecommendNamespace_WritesToNamespaceRecommendationSets`: Assert results are persisted in `namespace_recommendation_sets` with relational columns. |
| **RED** | `TestRecommendNamespace_DualModel`: Assert both cost and performance outputs. |
| **RED** | `TestRecommendAllNamespaces_MultipleNamespaces`: Seed 2 namespaces with different data. Assert recommendations differ (tests multi-grouping). |
| **RED** | `TestRecommendAllNamespaces_Upsert`: Write namespace recommendations, then write again with updated data. Assert second write updates (not duplicates). |
| **RED** | `TestRecommendAllNamespaces_P60_P98_P99_Integration`: Seed namespace digests with known P60/P98/P99 values. Assert recommendations incorporate these percentiles (not just P50/P95/Max). |
| **GREEN** | Implement `recommendNamespace()` in Go, operating on `daily_namespace_digests` with "read once, compute N terms" pattern. `NamespaceRec` struct includes `MemTrendSlope` populated from `memRec.TrendSlope`. |
| **REFACTOR** | Extract shared recommendation computation into `computeRecommendation(digests, config)` used by container, namespace, VM, and node paths. |

#### T-1.13b: Container memory percentile parity (migration 000035)

| Step | Detail |
|---|---|
| **Context** | `ComputeContainerDigest()` always computed P60/P98/P99 for memory request and usage, but discarded them — only P50/P95/Max were stored. This created an asymmetry with `daily_namespace_digests` (which stored all percentiles since migration 000031). |
| **RED** | `TestMigrationRoundtrip`: Assert latest migration version = `35`. |
| **GREEN** | Migration `000035_add_container_memory_percentiles` adds 6 columns to `daily_container_digests`: `memory_request_p60_kib`, `memory_request_p98_kib`, `memory_request_p99_kib`, `memory_usage_p60_kib`, `memory_usage_p98_kib`, `memory_usage_p99_kib`. `ContainerDigestResult` struct extended. `ProcessCSVToDigests` INSERT/UPDATE includes new columns. `RecommendAllWorkloads` SELECT/Scan reads them into `DigestRow`. |
| **REFACTOR** | None needed; straightforward schema addition and data plumbing. |

> **Why this matters:** Without the P60/P98/P99 columns, any future configuration change (e.g., `ROS_CPU_PERCENTILE=P98`) would silently use `0` for container memory, while namespace memory would correctly have the P98 value. Fixing this now ensures container and namespace recommendation paths are symmetric.

---

## 6. Phase 2: Metrics Pipeline

### 6.1 CSV Parsing and Integer Conversion

#### T-2.3a: CPU conversion (REQ-2.3)

| Step | Detail |
|---|---|
| **RED** | `TestParseCSV_CPUCoresToMillicores_ExactConversion`: Input `"0.250"`. Assert output `int64(250)`. |
| **RED** | `TestParseCSV_CPUCoresToMillicores_Rounding`: Input `"0.2505"`. Assert `int64(251)` (rounds to nearest). |
| **RED** | `TestParseCSV_CPUCoresToMillicores_ZeroInput`: Input `"0"`. Assert `int64(0)`. |
| **RED** | `TestParseCSV_CPUCoresToMillicores_LargeValue`: Input `"128.0"`. Assert `int64(128000)`. |
| **GREEN** | `int64(math.Round(value * 1000))` |
| **REFACTOR** | Extract into `CoreToMillicores(s string) (int64, error)`. |

#### T-2.3b: Memory conversion (REQ-2.3)

| Step | Detail |
|---|---|
| **RED** | `TestParseCSV_MemoryBytesToKiB_ExactConversion`: Input `"1048576"` (1 MiB). Assert `int64(1024)`. |
| **RED** | `TestParseCSV_MemoryBytesToKiB_Rounding`: Input `"1500"`. Assert `int64(1)` (1500/1024 = 1.46, rounds to 1). |
| **RED** | `TestParseCSV_MemoryBytesToKiB_ZeroInput`: Input `"0"`. Assert `int64(0)`. |
| **RED** | `TestParseCSV_MemoryBytesToKiB_LargeValue`: Input `"137438953472"` (128 GiB). Assert `int64(134217728)`. |
| **GREEN** | `int64(math.Round(value / 1024))` |
| **REFACTOR** | Extract into `BytesToKiB(s string) (int64, error)`. |

#### T-2.3c: Validation (REQ-2.1)

| Step | Detail |
|---|---|
| **RED** | `TestParseCSV_NaN_SkipsRow`: CSV row with `"NaN"` for CPU. Assert row skipped, warning logged. |
| **RED** | `TestParseCSV_Inf_SkipsRow`: CSV row with `"Inf"` for memory. Assert row skipped. |
| **RED** | `TestParseCSV_NegativeValue_SkipsRow`: CPU value `"-100"`. Assert skipped. |
| **RED** | `TestParseCSV_EmptyField_SkipsRow`: Missing column. Assert skipped. |
| **RED** | `TestParseCSV_MalformedCSV_ReturnsError`: Truncated CSV. Assert error returned with row number. |
| **GREEN** | Validation in CSV parser loop. |
| **REFACTOR** | Extract validation into `ValidateMetricRow(row []string) error`. |

### 6.2 In-Memory Aggregation

#### T-2.1a: Percentile computation (REQ-2.1)

| Step | Detail |
|---|---|
| **RED** | `TestComputeDigest_P50_Exact`: Input `[]int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}`. Assert p50 = 55 (interpolated median). |
| **RED** | `TestComputeDigest_P60_Exact`: Same input. Assert p60 = 64. |
| **RED** | `TestComputeDigest_P95_Exact`: Same input. Assert p95 = 95.5. |
| **RED** | `TestComputeDigest_P98_Exact`: Assert p98 = 98.2. |
| **RED** | `TestComputeDigest_P99_Exact`: Assert p99 = 99.1. |
| **RED** | `TestComputeDigest_Max`: Assert max = 100. |
| **RED** | `TestComputeDigest_Mean`: Assert mean = 55. |
| **RED** | `TestComputeDigest_SingleValue`: Input `[]int64{42}`. Assert all percentiles = 42, max = 42, mean = 42. |
| **RED** | `TestComputeDigest_TwoValues`: Input `[]int64{10, 20}`. Assert p50 = 15, max = 20. |
| **GREEN** | `slices.Sort(values)`, then index-based percentile extraction. |
| **REFACTOR** | Extract into `ComputeDigest(values []int64) Digest` struct. |

#### T-2.1b: Grouping (REQ-2.1)

| Step | Detail |
|---|---|
| **RED** | `TestGroupCSVRows_ByContainerAndDate`: Parse CSV with 3 containers × 2 dates. Assert 6 groups. Each group has correct values. |
| **RED** | `TestGroupCSVRows_SameContainerDifferentDays_SeparateGroups`: Container "main" on 2026-03-01 and 2026-03-02. Assert 2 separate digest computations. |
| **GREEN** | Group by `(org_id, cluster_uuid, namespace, workload, container_name, date)`. |
| **REFACTOR** | Use `map[DigestKey][]int64` pattern. |

#### T-2.1c: Upsert (REQ-2.1)

| Step | Detail |
|---|---|
| **RED** | `TestUpsertDigest_NewRow_Inserted`: Upsert digest for new container+date. Assert row exists in `daily_container_digests`. |
| **RED** | `TestUpsertDigest_ExistingRow_Updated`: Upsert same container+date twice with different values. Assert single row with latest values. |
| **RED** | `TestUpsertDigest_DifferentContainers_SeparateRows`: Upsert for containers A and B. Assert 2 rows. |
| **GREEN** | `INSERT ... ON CONFLICT DO UPDATE` via `pgx`. |
| **REFACTOR** | Batch upserts using `COPY FROM` + temp table + `INSERT ... ON CONFLICT`. |

### 6.3 Multi-Tenancy

#### T-2.2: Org isolation (REQ-2.2)

| Step | Detail |
|---|---|
| **RED** | `TestMultiTenancy_RecommendCPU_OnlyReturnsOwnOrgData`: Seed digests for org_A and org_B in same cluster UUID (unlikely but ensures isolation). Call `recommendCPU()` for org_A. Assert results only contain org_A data. |
| **RED** | `TestMultiTenancy_API_ListEndpoint_FiltersbyOrgFromIdentity`: Call API with org_A identity. Assert response contains only org_A recommendations. |
| **GREEN** | Go reads digests with `WHERE org_id = $1`. Identity extraction in middleware. |
| **REFACTOR** | None needed — isolation is fundamental, not refactorable. |

### 6.4 JSONB Elimination

#### T-2.4: Drop workload_metrics (REQ-2.4)

| Step | Detail |
|---|---|
| **RED** | `TestIngestionPipeline_DoesNotWriteToWorkloadMetrics`: Run full ingestion. Assert `workload_metrics` table has 0 new rows. |
| **GREEN** | Remove `BatchInsertWorkloadMetrics` call. |
| **REFACTOR** | Remove `WorkloadMetrics` GORM model entirely. |

#### T-2.5: Relational columns (REQ-2.5)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendationSets_HasRelationalColumns`: Query `recommendation_sets` after recommendation run. Assert `rec_cpu_request_millicores`, `rec_memory_request_kib`, `term`, `engine` are populated with correct typed values. |
| **RED** | `TestRecommendationSets_PK_IncludesTermAndEngine`: Insert same container with `term=short, engine=cost` and `term=short, engine=performance`. Assert both rows coexist (PK allows it). |
| **GREEN** | Migration adds columns, updates PK. |
| **REFACTOR** | None needed. |

---

## 7. Phase 3: Decay Weighting and Custom Timeframes

#### T-3.1: Daily digest table populated (REQ-3.1)

| Step | Detail |
|---|---|
| **RED** | `TestDailyDigest_PopulatedByIngestion`: Ingest CSV for 3 days. Assert `daily_container_digests` has 3 rows per container with correct p50, p60, p95, p98, p99, max, mean, sample_count. |
| **GREEN** | Wire ingestion pipeline to upsert digests. |
| **REFACTOR** | None needed. |

#### T-3.2a: Decay weighting (REQ-3.2)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_Decay_RecentDataWeightedMore`: Seed 7 days: day 1 has p60=100mc, days 2-7 have p60=200mc. Call `recommendCPU()` with 168h half-life. Assert cost recommendation is closer to 200 than 100. |
| **RED** | `TestRecommendCPU_NoDecay_ShortTerm`: Seed 1 day of data. Call with very large half-life (effectively infinite). Assert equal weighting for all points within the day. |
| **RED** | `TestRecommendCPU_Decay_OldDataAlmostIgnored`: Seed day 1 (14 days ago) with p60=1000mc, day 14 (today) with p60=100mc. Half-life 72h. Assert recommendation is much closer to 100 than 1000. |
| **GREEN** | `math.Exp(-age / halflife)` weighting in Go `applyDecayWeights()`. |
| **REFACTOR** | None needed — weighting is in the Go recommendation engine. |

#### T-3.2b: Trend detection

| Step | Detail |
|---|---|
| **RED** | `TestRecommendCPU_TrendSlope_IncreasingUsage`: Seed 7 days with linearly increasing p98: 100, 120, 140, ..., 220mc. Assert `trend_slope > 0`. |
| **RED** | `TestRecommendCPU_TrendSlope_StableUsage`: Seed 7 days all at 100mc. Assert `trend_slope ≈ 0`. |
| **RED** | `TestRecommendCPU_TrendSlope_DecreasingUsage`: Seed decreasing values. Assert `trend_slope < 0`. |
| **GREEN** | Go `computeLinearRegressionSlope()` on daily digest values. |
| **REFACTOR** | None needed. |

#### T-3.3: Custom timeframes and customer-defined terms (REQ-3.3)

| Step | Detail |
|---|---|
| **RED** | `TestCustomTimeframe_ArbitraryDateRange`: Seed 30 days. Call Go recommendation with start=day 10, end=day 20. Assert only those 10 days' data contributes. |
| **RED** | `TestCustomTimeframe_SingleDay`: Call with 1-day range. Assert recommendation based on that day only. |
| **RED** | `TestCustomTimeframe_CustomerDefinedProfile_3Terms`: Insert `org_recommendation_terms` rows with terms [3d, 20d, 60d]. Assert all 3 terms computed with correct windows and decay. |
| **RED** | `TestCustomTimeframe_APIEndpoint_AcceptsCustomRange`: `GET /recommendations?start_date=2026-03-10&end_date=2026-03-20`. Assert on-demand Go computation for that range. |
| **GREEN** | Go reads digests with `WHERE bucket_date >= $1 AND bucket_date < $2`, computes in memory. |
| **REFACTOR** | None needed. |

#### T-3.5: Recommendation engine testing and versioning (REQ-3.5)

| Step | Detail |
|---|---|
| **RED** | `TestMigrations_AllApply_CleanDatabase`: Run all golang-migrate migrations on empty PG 16 database. Assert no errors. Assert all tables exist. |
| **RED** | `TestMigrations_UpDown_RoundTrip`: Apply all migrations, then roll back all, then re-apply. Assert clean state. |
| **RED** | `TestRecommendationEngine_GoldenFile_CPUBasic`: Run `recommendCPU()` with fixed input. Assert output matches `testdata/golden/recommend_cpu_basic.json`. |
| **RED** | `TestRecommendationEngine_GoldenFile_MemoryOOM`: Run `recommendMemory()` with OOM data. Assert output matches golden file. |
| **GREEN** | golang-migrate migration files for schema; Go recommendation functions with golden-file test harness. |
| **REFACTOR** | None needed. |

---

## 8. Phase 4: Memory with OOM Feedback

#### T-4.1: OOM event collection (REQ-4.1)

| Step | Detail |
|---|---|
| **RED** | `TestDigest_OOMCountSum_Populated`: Ingest CSV with OOM events. Assert `oom_count_sum` column is populated in digest. |
| **RED** | `TestDigest_OOMCountSum_ZeroWhenNoOOM`: Ingest CSV without OOM events. Assert `oom_count_sum = 0`. |
| **GREEN** | Add OOM count to digest computation. |
| **REFACTOR** | None needed. |

#### T-4.2: Adaptive margin (REQ-4.2)

| Step | Detail |
|---|---|
| **RED** | `TestAdaptiveMargin_StableWorkload_15Percent`: p95=105, p50=100, mean=100. CV=0.05. Assert margin = 1.15 (clamped to min). |
| **RED** | `TestAdaptiveMargin_VariableWorkload_50Percent`: p95=300, p50=100, mean=120. CV=1.67. Assert margin = 1.50 (clamped to max). |
| **RED** | `TestAdaptiveMargin_MediumVariability`: p95=200, p50=100, mean=110. CV=0.91. Assert margin = min(1.50, 1 + 0.91) = 1.50. |
| **RED** | `TestAdaptiveMargin_ZeroMean_UsesMinMargin`: mean=0. Assert margin = 1.15 (zero-guard returns min margin). |
| **GREEN** | Go: `min(maxMargin, max(minMargin, 1.0 + float64(p95 - p50) / float64(mean)))` with zero-mean guard. |
| **REFACTOR** | None needed. |

#### T-4.3: OOM exponential backoff (REQ-4.3)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendMemory_OOM_FirstEvent_1_3xMultiplier`: Digest has `oom_count_sum = 1`. Assert memory limit ≥ `max × 1.3`. |
| **RED** | `TestRecommendMemory_OOM_SecondEvent_1_6xMultiplier`: `oom_count_sum = 2`. Assert ≥ `max × 1.6`. |
| **RED** | `TestRecommendMemory_OOM_ThirdPlus_2_0xMultiplier`: `oom_count_sum = 5`. Assert ≥ `max × 2.0`. |
| **RED** | `TestRecommendMemory_NoOOM_NoBackoff`: `oom_count_sum = 0`. Assert no backoff multiplier. |
| **GREEN** | OOM-aware limit calculation in Go `recommendMemory()`. |
| **REFACTOR** | Extract backoff table into configurable parameter. |

#### T-4.4: Memory trend detection (REQ-4.4)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendMemory_TrendUp_EmitsNotification`: Seed 7 days with increasing mean memory. Assert `WARNING_MEMORY_TRENDING_UP` notification. |
| **RED** | `TestRecommendMemory_TrendFlat_NoNotification`: Stable memory. Assert no trend notification. |
| **GREEN** | Go `computeLinearRegressionSlope()` on memory mean, threshold comparison in Go. |
| **REFACTOR** | None needed. |

#### T-4.6: Separate request/limit (REQ-4.6)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendMemory_CostModel_RequestNotEqualLimit`: Assert `cost_rec_request_kib != cost_rec_limit_kib` (request based on percentile, limit on p99 + headroom). |
| **RED** | `TestRecommendMemory_PerfModel_LimitHigherThanRequest`: Assert `perf_rec_limit_kib >= perf_rec_request_kib`. |
| **RED** | `TestRecommendCPU_Limit_P99Plus5Percent`: Seed digest with p99=1000mc. Assert limit = ROUND(1000 × 1.05) = 1050. |
| **GREEN** | Separate request/limit columns in function output. |
| **REFACTOR** | None needed. |

---

## 9. Phase 5: GPU Recommendations

#### T-5.1: MIG bin-packing (REQ-5.1)

| Step | Detail |
|---|---|
| **RED** | `TestGPU_MIGBinPack_A100_7Profiles`: Input: A100 with 7 MIG profiles. Container uses 15GB VRAM. Assert recommended profile is `3g.20gb` (smallest that fits). |
| **RED** | `TestGPU_MIGBinPack_ExactFit`: Container uses exactly 10GB. Assert `2g.10gb` (not next larger). |
| **RED** | `TestGPU_MIGBinPack_NoFit`: Container uses 90GB on A100 (max 80GB). Assert `FULL_GPU` recommendation. |
| **GREEN** | Go-side MIG bin-packing algorithm. |
| **REFACTOR** | Load MIG profiles from DB reference table. |

#### T-5.2: B200/RTX PRO gating (REQ-5.2)

| Step | Detail |
|---|---|
| **RED** | `TestGPU_B200_Recognized`: Input GPU model `"NVIDIA B200"`. Assert not gated, recommendations produced. |
| **RED** | `TestGPU_RTXPRO_Recognized`: Input `"NVIDIA RTX PRO 6000"`. Assert not gated. |
| **GREEN** | Extend GPU model whitelist. |
| **REFACTOR** | None needed. |

#### T-5.3: Frame buffer gaps (REQ-5.3)

| Step | Detail |
|---|---|
| **RED** | `TestGPU_FrameBufferGaps_A30_Filled`: Input A30 (missing from table). Assert lookup falls back to reference catalog. |
| **GREEN** | Populate GPU reference table with all known NVIDIA professional GPUs. |
| **REFACTOR** | None needed. |

#### T-5.4: GPU underutilization (REQ-5.4)

| Step | Detail |
|---|---|
| **RED** | `TestGPU_Underutilization_Below10Percent`: GPU utilization mean < 10% over 7 days. Assert `GPU_UNDERUTILIZED` notification. |
| **RED** | `TestGPU_Underutilization_Above10Percent_NoNotification`: 15% utilization. Assert no notification. |
| **GREEN** | Threshold check in Go GPU recommendation logic. |
| **REFACTOR** | None needed. |

---

## 10. Phase 6: New Recommendation Types

#### T-6.1: Idle workload detection (REQ-6.1)

| Step | Detail |
|---|---|
| **RED** | `TestDetectIdle_CPUBelow10mc_Flagged`: Seed digest with all CPU values < 10mc. Assert `is_idle = true`. |
| **RED** | `TestDetectIdle_MemoryBelow1024KiB_Flagged`: Memory all < 1024 KiB. Assert idle. |
| **RED** | `TestDetectIdle_AboveThreshold_NotFlagged`: CPU=500mc. Assert `is_idle = false`. |
| **RED** | `TestDetectIdle_ProcedureIntegration`: Call Go `recommendAllWorkloads()`. Assert idle containers have `IDLE_WORKLOAD` notification. |
| **GREEN** | Go `detectIdle()` function. |
| **REFACTOR** | None needed. |

#### T-6.3: PVC right-sizing (REQ-6.3)

| Step | Detail |
|---|---|
| **RED** | `TestRecommendPVC_OversizedBy50Percent_RecommendsSmaller`: PVC capacity 100Gi, max usage 30Gi. Assert recommendation < 100Gi. |
| **RED** | `TestRecommendPVC_NearCapacity_RecommendsLarger`: Max usage 95Gi on 100Gi PVC. Assert recommendation > 100Gi. |
| **RED** | `TestRecommendPVC_JustRight_NoChange`: Usage at 70% of capacity. Assert recommendation ≈ current size. |
| **GREEN** | Go `recommendPVC()` function. |
| **REFACTOR** | None needed. |

#### T-6.4: GOMAXPROCS/GOMEMLIMIT (REQ-6.4)

| Step | Detail |
|---|---|
| **RED** | `TestGoRuntime_GOMAXPROCS_RecommendedFromCPULimit`: Container has CPU limit 2000mc. Assert GOMAXPROCS recommendation = 2. |
| **RED** | `TestGoRuntime_GOMEMLIMIT_RecommendedFromMemoryLimit`: Memory limit 512 MiB. Assert GOMEMLIMIT recommendation = `0.9 × 512MiB`. |
| **RED** | `TestGoRuntime_NonGoContainer_NoRecommendation`: Container without Go runtime marker. Assert no Go runtime recommendation. |
| **GREEN** | Go-side heuristic based on language detection. |
| **REFACTOR** | None needed. |

#### T-6.5: Namespace boxplots (REQ-6.5)

> **Performance analysis:** See [namespace-boxplots-performance-analysis.md](namespace-boxplots-performance-analysis.md)
>
> Mirrors the Phase 5 container boxplot architecture. Creates `namespace_usage_samples`
> table (partitioned by `sample_time`), stores raw per-interval measurements during
> ingestion, computes exact five-number summaries at query time via `percentile_cont()`.
> Row cardinality is ~10× lower than container samples (fewer namespaces than containers).

##### T-6.5a: Namespace usage samples table

| Step | Detail |
|---|---|
| **RED** | `TestNamespaceSamplePartition_CreatedByIngestion`: Ingest namespace CSV. Assert `namespace_usage_samples_YYYYMM` partition exists for the data month. |
| **RED** | `TestNamespaceSampleUpsert_OneRowPerInterval`: Ingest CSV with 96 intervals for 1 namespace. Assert 96 rows in `namespace_usage_samples`. |
| **RED** | `TestNamespaceSampleUpsert_Idempotent`: Ingest same CSV twice. Assert still 96 rows (upsert, not duplicate). |
| **RED** | `TestNamespaceSampleUpsert_MultipleNamespaces`: Ingest CSV with 3 namespaces × 96 intervals. Assert 288 rows. |
| **GREEN** | Migration `000033_create_namespace_usage_samples.up.sql`, `EnsureNamespaceSamplePartitions()`, `upsertNamespaceUsageSamples()` in `internal/ingestion/namespace.go`. |
| **REFACTOR** | None needed. |

##### T-6.5b: Namespace boxplot assembly

| Step | Detail |
|---|---|
| **RED** | `TestAssembleNamespaceBoxplots_ShortTerm_4Buckets`: Seed 96 samples over 24h for 1 namespace. Assert 4 boxplot data points (6-hour windows), each with `min`, `q1`, `median`, `q3`, `max` for both CPU and memory. |
| **RED** | `TestAssembleNamespaceBoxplots_MediumTerm_7Buckets`: Seed 672 samples over 7 days. Assert 7 daily boxplot data points. |
| **RED** | `TestAssembleNamespaceBoxplots_LongTerm_15Buckets`: Seed 1440 samples over 15 days. Assert 15 daily boxplot data points. |
| **RED** | `TestAssembleNamespaceBoxplots_NoData_ReturnsNil`: Query for a namespace with no samples. Assert nil result (not error). |
| **RED** | `TestAssembleNamespaceBoxplots_UnitConversion`: Seed samples with known values. Assert CPU in cores (mc / 1000), memory in MiB (KiB / 1024), format fields set correctly. |
| **RED** | `TestAssembleNamespaceBoxplots_ExactPercentiles`: Seed 24 samples in a 6-hour bucket with known values. Assert Q1 = `percentile_cont(0.25)` and Q3 = `percentile_cont(0.75)` match expected interpolated values. |
| **GREEN** | `AssembleNamespaceBoxplots(ctx, pool, NamespaceKey, termName) (*NativePlot, error)` in `internal/model/boxplot.go`. Reuses `NativePlot`, `NativePlotsData`, `BoxPlotDetails` structs. |
| **REFACTOR** | Extract shared bucket configuration into `termWindows` map (already exists for containers). |

##### T-6.5c: Namespace boxplot API integration

| Step | Detail |
|---|---|
| **RED** | `TestNamespaceDetailEndpoint_IncludesBoxplots`: Seed samples + recommendation for a namespace. `GET /recommendations/openshift/namespaces/{id}`. Assert response contains `recommendations.recommendation_terms.<term>.plots.plots_data` with non-empty data for at least one term. |
| **RED** | `TestNamespaceDetailEndpoint_BoxplotShape`: Assert each data point has `cpuUsage` and `memoryUsage` with `min`, `q1`, `median`, `q3`, `max`, `format`. |
| **RED** | `TestNamespaceListEndpoint_ExcludesBoxplots`: `GET /recommendations/openshift/namespaces`. Assert no `plots` field in list items (boxplots are detail-only). |
| **GREEN** | Wire `AssembleNamespaceBoxplots` into namespace detail handler. |
| **REFACTOR** | None needed. |

##### T-6.5d: Namespace usage sample retention

| Step | Detail |
|---|---|
| **RED** | `TestRetention_NamespaceUsageSamples_OldPartitionsDropped`: Create partition `namespace_usage_samples_202501` (old). Run `RunRetentionSweep`. Assert partition dropped. |
| **RED** | `TestRetention_NamespaceUsageSamples_RecentPartitionsKept`: Create partition for current month. Run sweep. Assert partition still exists. |
| **GREEN** | Add `"namespace_usage_samples"` to `retainedTables` in `internal/engine/retention.go`. |
| **REFACTOR** | None needed. |

##### T-6.5e: Volume and query performance

| Step | Detail |
|---|---|
| **RED** | `TestNamespaceSampleStorage_20Namespaces_96Intervals_FitsExpected`: Seed 20 namespaces × 96 intervals × 30 days. Query `pg_total_relation_size`. Assert < 30 MB (well within the ~15 MB estimate for 90 days). |
| **RED** | `TestAssembleNamespaceBoxplots_LongTerm_Under5ms`: Seed 1440 samples. Benchmark `AssembleNamespaceBoxplots` for long_term. Assert < 5ms (99th percentile over 100 iterations). |
| **GREEN** | PK index + partition pruning handles this naturally. |
| **REFACTOR** | None needed. |

---

## 11. Phase 7: Replica Count and Total Impact

#### T-7.1: Replica count collection (REQ-7.1)

| Step | Detail |
|---|---|
| **RED** | `TestDigest_ReplicaCount_StoredFromCSV`: CSV includes replica count = 3. Assert `desired_replicas` stored in digest. |
| **GREEN** | Parse and store `desired_replicas` column. |
| **REFACTOR** | None needed. |

#### T-7.2: Replica count exposed (REQ-7.2)

| Step | Detail |
|---|---|
| **RED** | `TestAPI_RecommendationResponse_IncludesReplicaCount`: Assert API response includes `replicas: {min: 2, max: 5, avg: 3}`. |
| **GREEN** | Compute min/max/avg from digest series, include in API response assembly. |
| **REFACTOR** | None needed. |

#### T-7.3: Total savings (REQ-7.3)

| Step | Detail |
|---|---|
| **RED** | `TestTotalSavings_PerContainerSavings_MultipliedByReplicas`: Current: 500mc, recommended: 300mc, replicas: 3. Assert total savings = (500-300) × 3 = 600mc. |
| **RED** | `TestTotalSavings_NegativeSavings_ShownAsIncrease`: Current 100mc, recommended 200mc, replicas 2. Assert total = -200mc (increase, not savings). |
| **GREEN** | `(current - recommended) × avg_replicas` in Go. |
| **REFACTOR** | None needed. |

#### T-7.5: Koku cost integration (REQ-7.5)

| Step | Detail |
|---|---|
| **RED** | `TestKokuCostIntegration_DollarSavings_Computed`: Mock Koku API returns rate $0.05/core-hour. CPU savings = 200mc × 730h/month = $7.30/month. Assert `estimated_savings_cents ≈ 7.30`. |
| **RED** | `TestKokuCostIntegration_KokuUnavailable_NilSavings`: Mock Koku returns 503. Assert `estimated_savings_cents = nil` (not error). |
| **GREEN** | HTTP call to Koku cost API with circuit breaker. |
| **REFACTOR** | Extract into `KokuCostClient` interface for testability. |

---

## 12. Phase 8: HPA, VM, Node/MachineSet

### 12.1 HPA (REQ-8.1)

| Step | Detail |
|---|---|
| **RED** | `TestHPA_Detected_RecommendsTargetUtilization`: Container under HPA with current target 80%. Actual usage p95 = 60%. Assert recommended target = higher value (less aggressive scaling). |
| **RED** | `TestHPA_NotDetected_NoHPARecommendation`: No HPA metadata. Assert no HPA recommendation field in output. |
| **GREEN** | Go-side HPA heuristic. |
| **REFACTOR** | None needed. |

### 12.2 VM Recommendations (REQ-8b)

| Step | Detail |
|---|---|
| **RED** | `TestVMDigest_PopulatedFromCSV`: Ingest VM CSV. Assert `daily_vm_digests` rows with CPU, memory, IOPS columns. |
| **RED** | `TestRecommendVM_ProducesCPUAndMemory`: Seed VM digests. Call Go `recommendVM()`. Assert output has CPU and memory recommendations. |
| **RED** | `TestRecommendVM_IOPSConsidered`: VM with high IOPS. Assert recommendation factors in IOPS profile. |
| **RED** | `TestAPI_VMEndpoint_Returns200`: `GET /recommendations/openshift/virtual-machines`. Assert 200 with expected shape. |
| **GREEN** | VM parser, `daily_vm_digests` table, Go `recommendVM()` function, API endpoint. |
| **REFACTOR** | None needed. |

### 12.3 Node Recommendations (REQ-8c)

| Step | Detail |
|---|---|
| **RED** | `TestNodeDigest_PopulatedFromCSV`: Ingest node CSV. Assert `daily_node_digests` rows. |
| **RED** | `TestRecommendNodes_TierOne_UtilizationVisibility`: Seed node digests. Call Go `recommendNodes()`. Assert utilization percentiles returned (not right-sizing actions). |
| **RED** | `TestRecommendNodes_HighUtilization_WarningNotification`: Node at 95% CPU. Assert `NODE_HIGH_UTILIZATION` notification. |
| **RED** | `TestMachineSetRightSizing_OversizedNodes_RecommendsSmaller`: 4 nodes at 20% utilization. Assert recommendation to reduce node count or instance size. |
| **RED** | `TestAPI_NodeEndpoint_Returns200`: `GET /recommendations/openshift/nodes`. Assert 200. |
| **GREEN** | Node parser, `daily_node_digests`, Go `recommendNodes()`, API endpoint. |
| **REFACTOR** | None needed. |

---

## 13. Phase 9: JVM/Quarkus

#### T-9.1: JVM detection (REQ-9.1)

| Step | Detail |
|---|---|
| **RED** | `TestJVMDetection_JavaProcess_Detected`: Container with `JAVA_HOME` env var or `java` entrypoint. Assert JVM detected. |
| **RED** | `TestJVMDetection_NonJava_NotDetected`: Python container. Assert no JVM recommendation. |
| **GREEN** | Heuristic in Go. |
| **REFACTOR** | None needed. |

#### T-9.2: MaxRAMPercentage (REQ-9.2)

| Step | Detail |
|---|---|
| **RED** | `TestJVM_MaxRAMPercentage_RecommendedFromHeapUsage`: Heap usage at 60% of container memory. Assert MaxRAMPercentage recommendation = 75% (headroom for off-heap). |
| **GREEN** | Go heuristic. |
| **REFACTOR** | None needed. |

#### T-9.3: GC policy (REQ-9.3)

| Step | Detail |
|---|---|
| **RED** | `TestJVM_GCPolicy_SmallHeap_RecommendsSerial`: Heap < 256MB. Assert `Serial GC` recommended. |
| **RED** | `TestJVM_GCPolicy_LargeHeap_RecommendsG1`: Heap > 4GB. Assert `G1 GC` recommended. |
| **GREEN** | Decision tree in Go. |
| **REFACTOR** | None needed. |

#### T-9.4: Quarkus thread pool (REQ-9.4)

| Step | Detail |
|---|---|
| **RED** | `TestQuarkus_ThreadPool_RecommendedFromCPUCores`: Quarkus container with 2 cores. Assert IO thread pool recommendation = 2 × cores. |
| **GREEN** | Go heuristic. |
| **REFACTOR** | None needed. |

---

## 14. Phase 10: Legacy Kruize engine (optional path)

The **native Go engine is unconditionally active** as the recommendation path. **Kruize is not removed** in this model: it remains an **optional legacy integration** (disabled by default; enable via `ROS_ENABLED_PLUGINS=kruize`), with product direction to treat it as an explicit legacy plugin per [`plugin-architecture.md`](./plugin-architecture.md) — see §6.5. Phase 10 tests emphasize **isolation and configurability**: native deployments must compute and serve recommendations without calling Kruize; operators who opt into the legacy engine exercise the historical HTTP/Kafka surfaces until a native-only mandate exists.

#### T-10.1: Native mode avoids Kruize experiment lifecycle calls (REQ-10.1 revised)

| Step | Detail |
|---|---|
| **RED** | `TestIngestionPipeline_NativeMode_NoKruizeExperimentLifecycle`: With default/native configuration (native engine unconditionally active), run ingestion behind an HTTP recorder. Assert **zero** requests to Kruize endpoints used for experiment lifecycle (`/createExperiment`, `/updateResults`, `/generateRecommendations`) except when an integration test explicitly enables dual-path/shadow diagnostics. |
| **GREEN** | Guard all Kruize client usage behind legacy configuration; native path uses Go engine + PostgreSQL only. |
| **REFACTOR** | Centralize "legacy vs native" branching so new handlers cannot accidentally call Kruize in native mode. |

#### T-10.1b: Legacy engine configuration remains available (REQ-10.1b)

| Step | Detail |
|---|---|
| **RED** | `TestConfig_LegacyEngine_WiresKruizeComponents`: With `ROS_ENABLED_PLUGINS=kruize`, assert recommendation poller / Kruize client wiring is active per deployment manifests (legacy smoke path). |
| **GREEN** | Preserve legacy stack behind configuration flags — **do not delete** until product commits to native-only and tenants are migrated. |
| **REFACTOR** | Align startup/registry behavior with [`plugin-architecture.md`](./plugin-architecture.md) (mutual exclusivity between native plugins and the Kruize legacy plugin when that registry lands). |

#### T-10.3: Pipeline simplification for native default (REQ-10.3 revised)

| Step | Detail |
|---|---|
| **RED** | `TestIngestionPipeline_NativeMode_MinimalLegacyKafkaFanout`: Under native default, assert Kafka producers/consumers for recommendation batching match the documented deployment model (no redundant legacy-only topics unless legacy engine is enabled). |
| **GREEN** | Single primary ingestion→recommendation path for native deployments; legacy extras gated off. |
| **REFACTOR** | Document topic semantics for `ROS_ENABLED_PLUGINS=kruize` deployments beside native defaults. |

#### T-10.6: Quality metrics (REQ-10.6)

| Step | Detail |
|---|---|
| **RED** | `TestQualityMetrics_StabilityPercentage_Computed`: Seed 30 days of recommendations for same container. 25 days have same value. Assert `stability_pct = 83.3`. |
| **GREEN** | Compute from `recommendation_history`. |
| **REFACTOR** | None needed. |

#### T-10.7: Adoption detection (REQ-10.7)

| Step | Detail |
|---|---|
| **RED** | `TestAdoptionDetection_RequestMatchesRecommendation_MarksApplied`: Current request = previous recommended value. Assert `recommendation_applied_at` is set. |
| **RED** | `TestAdoptionDetection_NoMatch_NotMarked`: Request unchanged. Assert `recommendation_applied_at` is null. |
| **GREEN** | Comparison logic in Go post-recommendation step. |
| **REFACTOR** | None needed. |

#### T-10.8: Staleness detection (REQ-10.8)

| Step | Detail |
|---|---|
| **RED** | `TestStaleness_NoNewDataFor48Hours_MarkedStale`: Last digest is 49 hours old. Assert `stale = true`. |
| **RED** | `TestStaleness_RecentData_NotStale`: Last digest is 1 hour old. Assert `stale = false`. |
| **GREEN** | Staleness check in Go post-recommendation step. |
| **REFACTOR** | None needed. |

---

## 15. Non-Functional Requirements

### NFR-1: Concurrency Safety

| Step | Detail |
|---|---|
| **RED** | `TestConcurrency_TwoWorkersProcessSameCluster_NoRace`: Run 2 goroutines calling `recommendAllWorkloads()` for the same cluster concurrently. Assert no panics, no duplicate rows (upsert handles conflict), final state is consistent. |
| **RED** | `TestConcurrency_RaceDetector_Clean`: Run all tests with `-race` flag. Assert zero data races. |
| **GREEN** | `INSERT ... ON CONFLICT DO UPDATE` handles concurrent writes. Go `sync.Mutex` for any shared in-memory state. |
| **REFACTOR** | None needed. |

### NFR-2: Bounded Memory

| Step | Detail |
|---|---|
| **RED** | `TestBoundedMemory_10KContainerBatch_Under100MB`: Process 10K containers in a single Kafka message. Measure Go heap after GC. Assert < 100MB. |
| **RED** | `TestBoundedMemory_LargeCSV_StreamParsed`: 50MB CSV. Assert peak memory < 200MB (stream, don't load all into memory). |
| **GREEN** | Stream CSV parsing, process per-container groups, release as digests are upserted. |
| **REFACTOR** | Use `sync.Pool` for reusable `[]int64` buffers. |

### NFR-2a: Circuit Breakers

| Step | Detail |
|---|---|
| **RED** | `TestCircuitBreaker_KokuAPI_OpensAfter5Failures`: Mock Koku returns 500 five times. Assert 6th call returns immediately (circuit open) without HTTP request. |
| **RED** | `TestCircuitBreaker_KokuAPI_HalfOpenAfterTimeout`: After circuit open timeout, assert next call attempts HTTP request (half-open). |
| **GREEN** | Circuit breaker pattern with configurable thresholds. |
| **REFACTOR** | Use `sony/gobreaker` or similar. |

### NFR-3: Graceful Degradation

| Step | Detail |
|---|---|
| **RED** | `TestGracefulDegradation_DBDown_IngestsToQueue`: DB unreachable. Assert Kafka message is NACKed (not committed), will be retried. |
| **RED** | `TestGracefulDegradation_PartialCSV_ProcessesValidRows`: CSV with 100 rows, 5 invalid. Assert 95 rows ingested, 5 skipped with logging. |
| **GREEN** | Error handling in ingestion pipeline. |
| **REFACTOR** | None needed. |

### NFR-4: Observability

| Step | Detail |
|---|---|
| **RED** | `TestPrometheus_MetricsRegistered`: Assert Prometheus registry contains: `ros_ingestion_duration_seconds`, `ros_recommendation_duration_seconds`, `ros_csv_rows_processed_total`, `ros_csv_rows_skipped_total`, `ros_digest_upserts_total`, `ros_kafka_messages_processed_total`. |
| **RED** | `TestPrometheus_IngestionDuration_RecordedOnSuccess`: Process a message. Assert histogram has 1 observation. |
| **RED** | `TestHealthCheck_Healthy_Returns200`: `GET /healthz`. Assert 200. |
| **RED** | `TestReadiness_DBConnected_Returns200`: `GET /readyz` with live DB. Assert 200. |
| **RED** | `TestReadiness_DBDown_Returns503`: `GET /readyz` with dead DB. Assert 503. |
| **GREEN** | Prometheus metrics, health/readiness endpoints. |
| **REFACTOR** | None needed. |

### NFR-5: Configuration

| Step | Detail |
|---|---|
| **RED** | `TestConfig_AllEnvVarsHaveDefaults`: Instantiate config with no env vars set. Assert all fields have sensible defaults. Assert no panic. |
| **RED** | `TestConfig_OverrideViaEnv`: Set `ROS_DIGEST_RETENTION_DAYS=90`. Assert config reads 90 (not default 45). |
| **RED** | `TestConfig_InvalidValue_ReturnsError`: Set `ROS_DIGEST_RETENTION_DAYS=abc`. Assert error on startup. |
| **GREEN** | Config struct with env var parsing. |
| **REFACTOR** | None needed. |

### NFR-8: Partition Management

| Step | Detail |
|---|---|
| **RED** | `TestPartitions_CurrentAndNextMonthExist_AfterStartup`: Start binary. Assert partitions exist for current month and next month. |
| **RED** | `TestPartitions_OldPartitionsDropped_AfterRetention`: Create partition for 60 days ago (beyond 45-day retention). Run partition maintenance. Assert partition dropped. |
| **GREEN** | Go startup and background goroutine for partition management (fallback path). |
| **REFACTOR** | None needed. |

### NFR-9a: Org Data Deletion

| Step | Detail |
|---|---|
| **RED** | `TestOrgDeletion_DeletesAllOrgData`: Seed data for org_A and org_B. Delete org_A. Assert zero rows for org_A in all tables. Assert org_B data intact. |
| **GREEN** | `DELETE FROM ... WHERE org_id = $1` across all tables. |
| **REFACTOR** | None needed. |

---

## 16. Shadow Mode Validation

> REQ-1.12 — The most important cross-cutting test.

| Test | Detail |
|---|---|
| `TestShadowMode_NewEngineMatchesLegacy_WithinTolerance` | Ingest 7 days of known CSV data. Run new Go engine (superpowers). Run legacy Kruize path. Compare recommendation_sets vs recommendation_sets_shadow. Assert all values within 1mc / 1 KiB. |
| `TestShadowMode_Divergence_LoggedAsWarning` | Inject a digest that produces a deliberately different recommendation than legacy. Assert structured warning log with org_id, cluster_uuid, container, new value, legacy value, delta. |
| `TestShadowMode_DisabledByDefault` | Assert `ROS_ENABLE_SHADOW_MODE` defaults to `false`. When disabled, assert no shadow rows written. |
| `TestShadowMode_EnabledViaEnv` | Set `ROS_ENABLE_SHADOW_MODE=true`. Assert shadow path executes and writes to `recommendation_sets_shadow`. |
| `TestShadowMode_Metrics_DivergenceCountExposed` | Assert Prometheus counter `ros_shadow_divergence_total` incremented on mismatch. |

---

## 17. API Contract Tests

> These validate that the API response shape matches the existing v1 contract exactly, ensuring backward compatibility (§20).

| Test | Detail |
|---|---|
| `TestAPI_ListContainerRecommendations_V1Shape` | Assert response has `data[].recommendations.short_term.cost.cpu.amount`, `...memory.amount`, `...performance.cpu.amount`, etc. Assert `amount` is numeric string, `format` matches unit convention. |
| `TestAPI_ListContainerRecommendations_Pagination` | Assert `meta.count`, `links.next`, `links.previous` present. Page size = 10, assert 10 items returned. |
| `TestAPI_ListContainerRecommendations_FilterByCluster` | `?filter[cluster]=uuid`. Assert only that cluster's data returned. |
| `TestAPI_ListContainerRecommendations_FilterByNamespace` | `?filter[namespace]=production`. Assert filtered. |
| `TestAPI_GetContainerRecommendation_ByID` | `GET /recommendations/openshift/containers/{id}`. Assert 200 with full recommendation detail. |
| `TestAPI_ListNamespaceRecommendations_LegacyRoute` | `GET /openshift/namespace/recommendations`. Assert 200 (legacy route preserved). |
| `TestAPI_ListNamespaceRecommendations_CanonicalRoute` | `GET /recommendations/openshift/namespaces`. Assert same data as legacy route. |
| `TestAPI_UnitConversion_CPUDefaultCores` | Assert CPU values returned in cores by default (v1 convention). |
| `TestAPI_UnitConversion_CPUMillicoresParam` | `?cpu-unit=millicores`. Assert CPU values in millicores. |
| `TestAPI_UnitConversion_MemoryDefaultBytes` | List endpoint: assert memory in bytes. |
| `TestAPI_UnitConversion_MemoryMiBParam` | `?memory-unit=MiB`. Assert memory in MiB. |
| `TestAPI_NotificationCodes_Endpoint` | `GET /notification-codes`. Assert 200 with all defined codes. |
| `TestAPI_RBAC_Unauthorized_Returns403` | Request with invalid identity. Assert 403. |
| `TestAPI_RBAC_DifferentOrg_CannotSeeOtherOrgData` | Org A requests org B's recommendation by ID. Assert 404 (not 403, to prevent enumeration). |

---

## 18. Cross-Phase Integration Tests

> End-to-end tests spanning multiple phases. Run after each phase is complete.

| Test | Pipeline | Assert |
|---|---|---|
| `TestE2E_FullPipeline_KafkaToAPI` | Kafka msg → S3 download → CSV parse → digest upsert → Go "read once, compute N terms" → API query | API returns non-zero recommendations for all seeded containers |
| `TestE2E_MultiCluster_Isolation` | Ingest data for 2 clusters. | Each cluster's recommendations reference only its own data |
| `TestE2E_MultiOrg_Isolation` | Ingest data for 2 orgs. | API with org_A identity sees only org_A data |
| `TestE2E_LateArrivingData_Recomputes` | Ingest day 1. Wait. Ingest updated day 1 CSV. | Digest for day 1 is recomputed, recommendations updated |
| `TestE2E_RecommendationHistory_Preserved` | Run recommendations on day 1 and day 2 with different data. | `recommendation_history` has 2 snapshots |
| `TestE2E_FeatureFlag_DisabledCapability_NotExecuted` | Set `ROS_ENABLE_GPU_RECS=false`. Ingest GPU data. | No GPU recommendations produced |
| `TestE2E_OnPrem_SameAsSaaS_Pipeline` | Set on-prem config (no Kafka, /ingest endpoint). POST CSV via HTTP. | Same recommendations as Kafka path |

---

## 19. Performance and Load Tests

> Not part of the RED-GREEN cycle (they validate NFRs, not features). Run as a separate CI stage.

| Test | Setup | Pass Criteria |
|---|---|---|
| `BenchmarkDigestComputation_10KContainers` | 10K containers × 96 data points each | < 5 seconds total (REQ-2.1 acceptance criteria) |
| `BenchmarkRecommendCPU_50KContainers` | 50K containers in daily_container_digests, 15 days | Go `recommendCPU()` for all containers completes in < 10 seconds |
| `BenchmarkRecommendAllWorkloads_50KContainers_3Terms` | Full batch for 50K containers, 3 customer-defined terms | < 30 seconds (read once, compute 3 terms) |
| `BenchmarkRecommendAllWorkloads_50KContainers_90DayWindow` | 50K containers, 90-day max window, 3 terms | < 60 seconds |
| `BenchmarkAPIList_1000Results` | 1000 recommendations in DB | `GET /recommendations?limit=100` responds in < 200ms |
| `BenchmarkCSVParse_50MB` | 50MB CSV file | Parse + convert in < 3 seconds |
| `BenchmarkConcurrent_10Clusters` | 10 clusters ingesting simultaneously | All complete within 2× single-cluster time (near-linear scaling) |
| `BenchmarkAssembleNamespaceBoxplots_LongTerm` | 1 namespace, 1440 samples (15 days) | `AssembleNamespaceBoxplots` completes in < 5ms (P99) |
| `BenchmarkNamespaceSampleUpsert_20Namespaces` | 20 namespaces × 96 intervals = 1,920 rows | Batch upsert completes in < 100ms |

---

## 20. Test Data Catalog

All test data lives in `testdata/` directory, organized by phase.

```
testdata/
├── csv/
│   ├── single_container_1day.csv          # 96 rows, 1 container, 1 day
│   ├── multi_container_7days.csv          # 5 containers × 7 days
│   ├── with_nan_and_inf.csv               # Validation test data
│   ├── with_negative_values.csv           # Negative CPU/memory
│   ├── with_oom_events.csv                # OOM kill annotations
│   ├── large_batch_10k_containers.csv     # Performance test
│   ├── gpu_metrics.csv                    # GPU utilization data
│   ├── vm_metrics.csv                     # VM IOPS/throughput data
│   ├── node_metrics.csv                   # Node capacity/utilization
│   ├── namespace_aggregates.csv           # Namespace-level data
│   └── namespace_samples_20ns_15days.csv  # Namespace boxplot test data (20 ns × 96 intervals × 15 days)
├── golden/
│   ├── recommend_cpu_basic.json           # Expected output for basic CPU test
│   ├── recommend_cpu_decay.json           # Expected output with decay weighting
│   ├── recommend_memory_oom.json          # Expected output with OOM backoff
│   ├── recommend_namespace.json           # Namespace recommendation output
│   ├── namespace_boxplot_short_term.json  # Expected namespace boxplot (4 × 6h buckets)
│   └── api_response_v1_shape.json         # Full API response golden file
└── fixtures/
    ├── digest_stable_workload.sql         # INSERT statements for stable workload
    ├── digest_variable_workload.sql       # High-variability workload
    ├── digest_idle_workload.sql           # Idle container
    ├── digest_trending_up.sql             # Memory leak pattern
    ├── digest_multiple_orgs.sql           # Multi-tenancy test data
    └── notification_code_definitions.sql  # Reference table seed
```

### Deterministic Values

All test data uses these fixed constants:

| Constant | Value | Purpose |
|---|---|---|
| `TestOrgID` | `"org_test_1"` | Primary test org |
| `TestOrgID2` | `"org_test_2"` | Cross-org isolation |
| `TestClusterUUID` | `"11111111-..."` | Primary cluster |
| `TestClusterUUID2` | `"22222222-..."` | Multi-cluster tests |
| `BaseDate` | `2026-03-01` | All dates relative to this |
| `StableP50` | `100` (mc) | Stable workload |
| `StableP95` | `105` (mc) | Low variability (CV ≈ 0.05) |
| `VariableP50` | `100` (mc) | Variable workload |
| `VariableP95` | `300` (mc) | High variability (CV ≈ 1.67) |

---

## 21. Coverage Targets

| Layer | Target | Enforcement |
|---|---|---|
| Go unit tests | ≥ 90% line coverage | `go test -coverprofile` in CI, fail if below |
| Go recommendation functions | 100% of exported functions called | Unit tests cover every recommendation function (`recommendCPU`, `recommendMemory`, `detectIdle`, `recommendNamespace`, `recommendVM`, `recommendNodes`, `recommendPVC`, `AssembleNamespaceBoxplots`) |
| API endpoints | 100% of routes hit | Contract tests cover every registered route |
| Error paths | ≥ 80% of error branches | NaN, Inf, nil, empty, DB failure, timeout |
| Edge cases | All identified in spec | Idle, OOM, trend, zero-mean, single-value, overflow |

### CI Pipeline

```
┌─────────────────┐    ┌──────────────────┐    ┌───────────────┐    ┌──────────────┐
│  go vet + lint  │───▶│  Go Unit Tests   │───▶│  Go Rec Unit  │───▶│  Go Integ    │
│  (< 30s)        │    │  (-race, < 2m)   │    │  (< 2m)       │    │  (PG16, < 5m)│
└─────────────────┘    └──────────────────┘    └───────────────┘    └──────────────┘
                                                                           │
                                                                    ┌──────▼──────┐
                                                                    │  E2E Tests  │
                                                                    │  (< 10m)    │
                                                                    └──────┬──────┘
                                                                           │
                                                                    ┌──────▼──────┐
                                                                    │  Coverage   │
                                                                    │  Report     │
                                                                    └─────────────┘
```

**Total CI time target:** < 25 minutes.

---

## 22. IQE Integration Test Plan

> **Scope:** This section covers changes to the **IQE (Integrated QE)** test suites that validate ros-ocp-backend from the outside — as a deployed service in a real OpenShift/staging environment. These are **Python/pytest** tests in separate repositories, distinct from the Go in-repo tests in §§4–21.
>
> **Repositories:**
> - `iqe-ros-ocp-plugin` — Primary ROS test suite (REST API, UI, Kruize direct)
> - `iqe-cost-management-plugin` — Cost management suite with ROS-specific tests (`test_ros.py`, RBAC, S3/Kafka validation)

### 22.1 Phase 1-2: Core CPU + Memory — IQE Updates

The superpowers engine replaces Kruize but preserves API compatibility for existing `/recommendations/openshift` fields. Changes are primarily removals and schema updates.

#### Removed tests (Kruize no longer exists)

| Existing test file | Tests removed | Reason |
|---|---|---|
| `iqe-ros-ocp-plugin/tests/kruise/test_endpoints.py` | `test_create_experiment`, `test_update_valid_results_after_create_exp`, `test_list_recommandations` | Kruize HTTP API (`/createExperiment`, `/updateResults`, `/listRecommendations`) no longer exists. No replacement needed — the Go engine has no equivalent external experiment lifecycle. |

#### Updated tests

| Existing test area | File | What changes |
|---|---|---|
| Response format | `tests/rest/test_recommendations.py` (`test_get_recommendation_response_format_*`) | Assert backwards-compatible fields still present (`recommendation_terms`, `short_term`, `medium_term`, `long_term`). Add assertions for new fields: `daily_digest_metadata`, `engine_version`. |
| Boxplot UI | `tests/ui/test_ui_boxplot.py` (7 tests) | Adapt to new response data structure — boxplot data previously came from Kruize's histogram/percentile format. Update expected data shape or **defer** if boxplot UI is being redesigned in koku-ui. |
| S3/Kafka content | `iqe-cost-management-plugin/tests/rest_api/v1/test_ros.py` (`test_api_ocp_ros_kafka_content`) | Update Kafka message schema assertions for `hccm.ros.events` if message format changes. Update recommendation content validation for new response schema. |
| OpenAPI fuzzing | `tests/rest/test_apispec.py` | Point Schemathesis at new OpenAPI spec (`openapi-superpowers.json`). Update `data/api.ros_ocp.ros_ocp_api.spec.json` snapshot. |
| Notification tests | `tests/rest/test_optimized_workloads.py` | Verify existing notification codes still emitted by Go engine. No new codes in Phase 1-2. |
| RBAC tests | `iqe-cost-management-plugin/tests/rbac/test_rbac.py` (3 ROS tests) | Should work as-is — same `/recommendations/openshift` endpoint. Verify after deployment. |

#### Tests expected to work as-is (verify only)

Pagination (4 tests), sorting (3 tests), search (2 tests), offset/limit, negative/error tests (8 tests).

### 22.2 Phase 3: Custom Timeframes (COST-5691) — IQE New + Updates

Custom timeframes is a net-new feature with no existing IQE coverage.

#### New IQE tests (`iqe-ros-ocp-plugin`)

| Test | Validates |
|---|---|
| `test_custom_timeframe_default_terms_1d_7d_15d` | API returns `short_term` (1d), `medium_term` (7d), `long_term` (15d) by default — backwards-compatible with legacy. Assert `recommendation_terms` structure unchanged for customers using defaults. |
| `test_custom_timeframe_api_accepts_start_end_date` | `GET /recommendations/openshift?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD` returns recommendations computed over that date range. Assert different results vs full-window query. |
| `test_custom_timeframe_term_config_api` | When term configuration API is exposed: POST/PUT/DELETE custom terms for an org. Assert API accepts `window_days` 1–90 and `decay_halflife_hours`. Assert 400 for `window_days` > 90. |
| `test_custom_timeframe_recommendations_differ_with_window` | Configure org with 10d/30d/90d terms. Ingest 90 days of nise data. Assert recommendation values differ from default 1d/7d/15d (wider window = different percentile). |
| `test_custom_timeframe_90_day_maximum_enforced` | Attempt to configure `window_days: 91` via API. Assert 400 or clamped to 90. |

#### Updated existing tests

| Existing test area | Update |
|---|---|
| Namespace recommendation filter tests (`test_namespace_filter.py` — 17 tests, currently `@skip`) | **Un-skip** — these test `start_date`/`end_date` filtering which is the custom timeframes feature. Update expected behavior to match new engine's date-scoped computation. |
| UI drawer duration dropdown (`test_optimizations_drawer_duration_based_optimizations_dropdown`) | Update dropdown options if custom timeframes changes the duration selector labels or adds new options. |

### 22.3 Phase 4: OOM-Aware Memory — IQE New

| Test | Validates |
|---|---|
| `test_oom_notification_code_emitted` | After ingesting nise data with OOM events, assert OOM-related notification code present in API response (e.g., `INFO_OOM_DETECTED`). |
| `test_oom_memory_recommendation_higher_than_non_oom` | Compare memory recommendation for a container with OOM history vs one without. Assert OOM-aware recommendation is higher (exponential backoff applied). |
| `test_oom_floor_applied_to_memory_limit` | Assert memory limit recommendation is never below the last OOM kill value (`oom_floor`). |

**Requires:** Nise data generation with OOM events (new nise feature or manually crafted test data).

### 22.4 Phase 5: GPU Recommendations — IQE New

| Test | Validates |
|---|---|
| `test_gpu_recommendation_present_for_gpu_workload` | Workload with NVIDIA GPU metrics gets GPU recommendation fields in API response (`gpu_request`, `gpu_limit`, `gpu_idle`). |
| `test_gpu_idle_detection_threshold` | GPU workload with <10% utilization flagged as idle. Assert `INFO_GPU_IDLE` notification. |
| `test_gpu_mig_cost_aware_savings` | MIG-enabled GPU workload shows cost comparison field (depends on Koku MIG cost data availability). Graceful `null` if unavailable. |

**Requires:** Nise data with GPU metrics. Operator with GPU queries merged (`cost-7178-mig-metrics` or successor).

### 22.5 Phase 6: PVC Right-Sizing + Go Runtime + Namespace Boxplots — IQE New

| Test | Validates |
|---|---|
| `test_pvc_recommendation_oversized` | PVC using <20% capacity flagged as oversized with resize recommendation. |
| `test_pvc_recommendation_near_full` | PVC using >85% capacity flagged as near-full with growth projection. |
| `test_pvc_orphaned_zero_usage` | PVC with 0 usage flagged as orphaned. |
| `test_go_runtime_advisory_gomaxprocs` | Go workload (detected via `go_info` metric) gets advisory notification: `GOMAXPROCS = ceil(cpu_limit)`, `GOMEMLIMIT = 0.9 × mem_limit`. |
| `test_namespace_detail_includes_boxplots` | Fetch namespace recommendation by ID. Assert `recommendations.recommendation_terms.<term>.plots.plots_data` is present and non-empty for at least one term. |
| `test_namespace_boxplot_shape` | Assert each boxplot data point has `cpuUsage` and `memoryUsage` with `min`, `q1`, `median`, `q3`, `max`, `format` fields. |
| `test_namespace_short_term_has_4_windows` | Assert short_term `plots_data` has exactly 4 keys (6-hour windows within 24h). |
| `test_namespace_medium_long_term_daily_granularity` | Assert medium_term has 7 keys and long_term has 15 keys, all with daily timestamps. |
| `test_namespace_list_excludes_boxplots` | Fetch namespace list. Assert no `plots` field in any item (boxplots are detail-only). |
| `test_namespace_boxplot_monitoring_end_time` | Assert `recommendations.monitoring_end_time` is a valid ISO timestamp in the namespace detail response. |

**Requires:** Nise data with PVC usage variations. Operator with Go runtime query.
Namespace boxplot tests use existing `ocp_source_with_ingestion_using_default_ros_ocp_yaml`
fixture with `n_days=16` (produces ~96 samples/day/namespace — sufficient for all three terms).

### 22.6 Phase 7: Replica Count + Dollar Savings — IQE New

| Test | Validates |
|---|---|
| `test_dollar_savings_present_when_cost_model_configured` | With Koku cost model configured, `estimated_savings_cents` field is non-null in API response. Value is positive (current cost > recommended cost). |
| `test_dollar_savings_null_when_no_cost_model` | Without cost model or with `ROS_ENABLE_COST_INTEGRATION=false`, field is `null` (not 0, not missing). |
| `test_dollar_savings_null_when_koku_unreachable` | Circuit breaker: when Koku API is down, `estimated_savings_cents` degrades to `null` gracefully. No 500 error. |
| `test_replica_count_in_response` | Recommendation includes `current_replicas`, `min_replicas`, `max_replicas`, `avg_replicas` fields for workloads with replica data. |

**Requires:** Koku with cost model configured for the test OCP source.

### 22.7 Phase 8+: HPA, VM, Node/MachineSet — IQE Placeholders

> These are larger feature areas that will each require dedicated IQE test modules. Listed here as placeholders with key scenarios; detailed test design deferred to Phase 8 implementation.

#### Phase 8: HPA

| Test | Validates |
|---|---|
| `test_hpa_saturated_detection` | HPA at max replicas → `INFO_HPA_SATURATED` notification |
| `test_hpa_idle_detection` | HPA at min replicas with low utilization → `INFO_HPA_IDLE` notification |
| `test_hpa_managed_workload_informational_only` | HPA-managed workload gets informational notification instead of VPA-style resize recommendation |

#### Phase 8b: VM Right-Sizing

| Test | Validates |
|---|---|
| `test_vm_cpu_recommendation_whole_vcpus` | VM CPU recommendation rounded to whole vCPUs |
| `test_vm_memory_recommendation_whole_gib` | VM memory recommendation rounded to whole GiB |
| `test_vm_guest_os_baseline_windows` | Windows VM memory recommendation ≥ 2 GiB baseline |
| `test_vm_hysteresis_40_percent` | Recommendation not emitted unless savings exceed 40% threshold |
| `test_vm_instance_type_recommendation` | If `VirtualMachineInstancetype` resources available, smallest-fit recommended |

#### Phase 8c: Node/MachineSet

| Test | Validates |
|---|---|
| `test_node_underutilized_detection` | Node with <30% CPU and <30% memory flagged as underutilized |
| `test_node_overcommitted_detection` | Node with >150% request/allocatable flagged as overcommitted |
| `test_machineset_right_sizing` | MachineSet with underutilized nodes gets replica count + instance type recommendation |

**Requires:** Operator with node/MachineSet queries (Phase 8c operator dependency).

### 22.8 Shadow Mode — IQE Validation

| Test | Validates |
|---|---|
| `test_shadow_mode_both_engines_produce_results` | With shadow mode enabled (Unleash flag on for test org), both Kruize and superpowers process the same data. API returns results from the primary engine (old during rollout, new after cutover). |
| `test_shadow_mode_recommendation_parity_spot_check` | For a known test workload with deterministic nise data, compare old vs new engine `short_term` CPU/memory values. Assert within acceptable tolerance (logged as metric, not hard-fail — engines differ by design). |

### 22.9 IQE Test Fixtures and Data Updates

| Fixture/helper | Repository | Change |
|---|---|---|
| `iqe_ros_ocp/fixtures/sources.py` | `iqe-ros-ocp-plugin` | Add `create_source_or_upload_data_30d`, `_60d`, `_90d` variants for custom timeframe testing. Update existing `_1d`, `_7d`, `_15d` fixtures if nise YAML format changes. |
| `iqe_ros_ocp/fixtures/general_fixtures.py` | `iqe-ros-ocp-plugin` | Update `wait_for_recommendations` polling to handle new engine's processing time characteristics. |
| `iqe_ros_ocp/data/data_conf.yaml` | `iqe-ros-ocp-plugin` | Add datasets: `oom_dataset`, `gpu_dataset`, `pvc_dataset`, `custom_timeframe_90d_dataset`. |
| `iqe_cost_management/fixtures/helpers.py` | `iqe-cost-management-plugin` | Update `wait_for_ros_reports` and `check_recommendations_available` if S3 key naming or Kafka message schema changes. |
| Nise static YAMLs | `iqe-ros-ocp-plugin/data/` | Create new nise YAMLs for OOM, GPU, PVC, Go runtime, HPA, VM, and node workloads with `--ros-ocp-info`. |

### 22.10 IQE Summary

| Phase | Removed | Updated | Net-new | Total IQE impact |
|---|---|---|---|---|
| **1-2** (CPU/Memory) | 3 | ~10 | 0 | ~13 |
| **3** (Custom Timeframes) | 0 | ~18 | 5 | ~23 |
| **4** (OOM) | 0 | 0 | 3 | 3 |
| **5** (GPU) | 0 | 0 | 3 | 3 |
| **6** (PVC/Go/NS Boxplots) | 0 | 0 | 10 | 10 |
| **7** (Replicas/Cost) | 0 | 0 | 4 | 4 |
| **8+** (HPA/VM/Node) | 0 | 0 | ~11 | ~11 |
| **Shadow** | 0 | 0 | 2 | 2 |
| **Totals** | **3** | **~28** | **~38** | **~69** |

> **Coordination note:** IQE test updates for each phase should be merged **after** the corresponding ros-ocp-backend phase is deployed to staging and producing real API responses. The IQE tests validate the deployed system, not a local dev environment. Each phase's IQE PR should be paired with the backend deployment and validated in the staging pipeline before promotion to production.

---

## Appendix: Requirement → Test Traceability Matrix

| REQ | Test IDs | Layer |
|---|---|---|
| REQ-0.1 | T-0.1 | Go Unit |
| REQ-0.2 | T-0.2 | Go Unit |
| REQ-0.3 | T-0.3 | Go Integration |
| REQ-0.4 | T-0.4 | Go Unit |
| REQ-0.5 | T-0.5 | Go Integration |
| REQ-0.6 | T-0.6 | Go Unit |
| REQ-0.7 | T-0.7 | Go Integration |
| REQ-0.8 | T-0.8 | Go Integration |
| REQ-0.9 | T-0.9 | Go Unit |
| REQ-0.10 | T-0.10 | Go Unit |
| REQ-0.11 | T-0.11 | Go Unit |
| REQ-0.12 | T-0.12 | Go Integration |
| REQ-1.1 | T-1.1 | Go Rec. Unit |
| REQ-1.2 | T-1.2 | Go Rec. Unit |
| REQ-1.3 | T-1.3, T-1.3b | Go Rec. Unit |
| REQ-1.5 | T-1.5 | Go Rec. Unit |
| REQ-1.6 | T-1.6 | Go Rec. Unit |
| REQ-1.7 | T-1.7 | Go Rec. Unit |
| REQ-1.8 | T-1.8 | Go Rec. Unit + Go Integration |
| REQ-1.9 | T-1.9 | Go Integration |
| REQ-1.10 | T-1.10 | Go Integration |
| REQ-1.11 | T-1.11 | Go Integration |
| REQ-1.12 | §16 (Shadow Mode) | E2E |
| REQ-1.13 | T-1.13 | Go Rec. Unit + Go Integration |
| REQ-2.1 | T-2.1a, T-2.1b, T-2.1c | Go Unit + Go Integration |
| REQ-2.2 | T-2.2 | Go Integration |
| REQ-2.3 | T-2.3a, T-2.3b, T-2.3c | Go Unit |
| REQ-2.4 | T-2.4 | Go Integration |
| REQ-2.5 | T-2.5 | Go Integration |
| REQ-2.6 | NFR-8 tests | Go Integration |
| REQ-2.7 | T-3.1 | Go Integration |
| REQ-3.1 | T-3.1 | Go Integration |
| REQ-3.2 | T-3.2a, T-3.2b | Go Rec. Unit |
| REQ-3.3 | T-3.3 | Go Rec. Unit + Go Integration |
| REQ-3.5 | T-3.5 | Go Integration |
| REQ-4.1 | T-4.1 | Go Unit |
| REQ-4.2 | T-4.2 | Go Rec. Unit |
| REQ-4.3 | T-4.3 | Go Rec. Unit |
| REQ-4.4 | T-4.4 | Go Rec. Unit |
| REQ-4.6 | T-4.6 | Go Rec. Unit |
| REQ-5.1 | T-5.1 | Go Unit |
| REQ-5.2 | T-5.2 | Go Unit |
| REQ-5.3 | T-5.3 | Go Unit |
| REQ-5.4 | T-5.4 | Go Unit |
| REQ-6.1 | T-6.1 | Go Rec. Unit |
| REQ-6.3 | T-6.3 | Go Rec. Unit |
| REQ-6.4 | T-6.4 | Go Unit |
| REQ-6.5 | T-6.5a, T-6.5b, T-6.5c, T-6.5d, T-6.5e | Go Unit + Go Integration + Benchmark |
| REQ-7.1 | T-7.1 | Go Unit |
| REQ-7.2 | T-7.2 | Go Integration |
| REQ-7.3 | T-7.3 | Go Unit |
| REQ-7.5 | T-7.5 | Go Integration |
| REQ-8.1 | T-8.1 | Go Unit |
| REQ-8b.2–8b.6 | T-8b | Go Rec. Unit + Go Integration |
| REQ-8c.2–8c.10 | T-8c | Go Rec. Unit + Go Integration |
| REQ-9.1–9.4 | T-9.x | Go Unit |
| REQ-10.1 | T-10.1 | Go Integration |
| REQ-10.3 | T-10.3 | Go Integration |
| REQ-10.6 | T-10.6 | Go Integration |
| REQ-10.7 | T-10.7 | Go Integration |
| REQ-10.8 | T-10.8 | Go Integration |
| NFR-1 | NFR-1 tests | Go Integration |
| NFR-2 | NFR-2 tests | Benchmark |
| NFR-2a | NFR-2a tests | Go Unit |
| NFR-3 | NFR-3 tests | Go Integration |
| NFR-4 | NFR-4 tests | Go Unit + Integration |
| NFR-5 | NFR-5 tests | Go Unit |
| NFR-8 | NFR-8 tests | Go Integration |
| NFR-9a | NFR-9a tests | Go Integration |
| API Contract | §17 tests | Go Integration |
| Shadow Mode | §16 tests | E2E |
| Performance | §19 benchmarks | Benchmark |
| REQ-1.1–1.3 | §22.1 IQE response format | IQE E2E |
| REQ-1.8, REQ-3.3 | §22.2 IQE custom timeframes | IQE E2E |
| REQ-4.1–4.6 | §22.3 IQE OOM-aware memory | IQE E2E |
| REQ-5.1–5.4 | §22.4 IQE GPU recommendations | IQE E2E |
| REQ-6.1–6.5 | §22.5 IQE PVC/Go runtime/NS boxplots | IQE E2E |
| REQ-7.1–7.5 | §22.6 IQE replica count/savings | IQE E2E |
| REQ-8.1–8c.10 | §22.7 IQE HPA/VM/Node | IQE E2E |
| REQ-1.12 | §22.8 IQE shadow mode | IQE E2E |
