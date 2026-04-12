# Phase 4: OOM Feedback and Recommendation Quality

## Problem Statement

The native engine currently **observes** OOM events but does **not act on them**. When `OOMCountSum > 0`, `EvaluateNotifications` emits `NotifOOMDetected` (code 3) as an informational alert, but `RecommendMemory` ignores OOM data entirely. The engine recommends 10-16% less memory than Kruize for large workloads (documented in [phase-1-2-3-go-engine.md](phase-1-2-3-go-engine.md) appendix), and the explicit design assumption is that Phase 4's OOM feedback closes this safety gap.

Additionally, the **koku-metrics-operator does not collect OOM data at all** -- there are no Prometheus queries for `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` and no `oom_count` column in the ROS container CSV. This means `oom_count_sum` in `daily_container_digests` is always 0 in production, and `NotifOOMDetected` never fires for real workloads.

The `recommendation_quality` and `recommendation_history` tables (migration 027) exist as schema-only -- no Go code reads from or writes to them.

## Repositories Touched

- **koku-metrics-operator** (`~/dev/koku/koku-metrics-operator/`) -- OOM Prometheus query, CSV column, tests
- **ros-ocp-backend** (`~/dev/koku/ros-ocp-backend/`) -- Engine OOM feedback, quality tracking, history, tests
- **nise** (`~/dev/koku/nise/`) -- `oom_count` in generated ROS CSV for testing
- **iqe-ros-ocp-plugin** (`~/dev/koku/iqe-ros-ocp-plugin/`) -- IQE tests for OOM notification and quality API

## Prerequisites

### Align Native CSV Parser with Operator Column Names

The native CSV parser (`internal/ingestion/csvparser.go`) uses abbreviated column
names (`workload_name`, `cpu_request`, `mem_usage`, ...) that do **not** match the
operator/nise output (`workload`, `cpu_request_container_avg`,
`memory_usage_container_avg`, ...). The Kruize path uses the operator names via
`csvColumnMapping.go` and sends structured JSON to Kruize -- it never hits the
native parser.

Since the native engine has not shipped to production, there is no backward
compatibility to maintain. **Rename the native parser columns to match the
operator/nise output:**

| Native parser (current) | Operator/Nise (correct) |
|-------------------------|------------------------|
| `workload_name` | `workload` |
| `cpu_request` | `cpu_request_container_avg` |
| `cpu_limit` | `cpu_limit_container_avg` |
| `cpu_usage` | `cpu_usage_container_avg` |
| `cpu_throttle` | `cpu_throttle_container_avg` |
| `mem_request` | `memory_request_container_avg` |
| `mem_limit` | `memory_limit_container_avg` |
| `mem_usage` | `memory_usage_container_avg` |
| `mem_rss` | `memory_rss_usage_container_avg` |

Files to update:
- `internal/ingestion/csvparser.go` -- switch cases and required columns list
- `internal/ingestion/csvparser_test.go` -- test CSV headers
- `internal/services/report_processor_test.go` -- CSV fixtures in integration tests
- Any other test fixtures using the abbreviated headers

**Tests:** Update all existing `csvparser_test.go` tests to use operator-style
headers. Verify that `buildColumnIndex` maps operator column names correctly.
Verify that `ParseCSVRows` still produces the same `MetricRow` output with the
renamed columns. Verify that a CSV missing optional columns (e.g., `oom_count`)
still parses successfully.

## Execution Strategy: Two Parallel Tracks

Work proceeds in two independent tracks that can execute concurrently:

- **Track A** (operator + nise): Data collection -- add OOM Prometheus queries and CSV columns
- **Track B** (backend engine + quality): Backend engine OOM feedback and quality tracking

Track A and Track B have no code dependencies between them. The backend already parses `oom_count` as optional -- Track B can be tested with synthetic CSV fixtures before Track A delivers real OOM data.

## Architecture

```
Operator (koku-metrics-operator)
  Prometheus Query: kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}
    -> ROS CSV: + oom_count column

ros-ocp-backend
  CSV Parser (reads oom_count)
    -> Daily Digest (oom_count_sum)
      -> RecommendMemory (OOM bump: log-scale, post-margin)
        -> recommendation_quality (all 4 metrics)
```

---

## Track A, Milestone 1: Operator -- OOM Collection (koku-metrics-operator)

**Goal:** Add a Prometheus query that detects OOM kills per container per interval, and include the count in the ROS container CSV.

### 1a. Add OOM Prometheus Query

**Target: OpenShift 4.19+ only.** KSM v2.x is guaranteed, so
`kube_pod_container_status_last_terminated_reason` and
`kube_pod_container_status_restarts_total` are stable metrics.

Add a new entry to `QueryMap` in `internal/collector/queries.go`:

```promql
ros:oom_count_container_sum:
  sum by(container, pod, namespace) (
    increase(kube_pod_container_status_restarts_total{container!='', container!='POD', pod!=''}[15m])
    * on(pod, namespace, container) group_left
      (kube_pod_container_status_last_terminated_reason{reason='OOMKilled'} > 0)
    * on(namespace) group_left kube_namespace_labels{...}
  )
```

Joining `increase(restarts_total[15m])` with the OOMKilled reason gauge gives an
OOM count per interval rather than just a binary flag. No fallback query needed --
both metrics are stable on 4.19+.

**OpenShift 4.19+ simplification opportunity:** All existing ROS queries duplicate
a namespace filter with an `OR` for two label names
(`label_insights_cost_management_optimizations` and
`label_cost_management_optimizations`). If 4.19+ standardizes on a single label,
the OOM query (and all existing ROS queries) could collapse the `OR` halves into a
single `kube_namespace_labels` matcher. This is a product decision to evaluate
during implementation.

### 1b. Add CSV Column

- `internal/collector/types.go`: Add `OOMCount` field to `rosContainerRow`. Add `"oom_count"` to `csvHeader()`. Format in `csvRow()`.
- Wire the query result into `rosContainerRow` during collection in `internal/collector/collector.go`.

### 1c. Tests

- Unit test: Verify `csvHeader()` includes `"oom_count"` at the expected position.
- Unit test: Verify `csvRow()` formats the OOM count value correctly (0, positive integer).
- Integration test (Ginkgo/envtest): Mock Prometheus response with OOM data, verify CSV output contains the column.

**Compatibility:** The new column is additive. Old backends that don't recognize `oom_count` will ignore it (the `ros-ocp-backend` CSV parser already handles `oom_count` as optional via `if idx.oomCount >= 0`). No breaking change.

---

## Track A, Milestone 2: Nise -- OOM Test Data (nise)

**Goal:** Allow `nise report ocp --ros-ocp-info` to emit `oom_count` in the ROS container CSV so end-to-end dev environment tests can exercise the full OOM pipeline.

- Add an `oom_count` column to the ROS container CSV generator.
- Default to 0 for 90% of rows; inject 1-3 OOM events for the remaining 10% using weighted random selection.
- **Deferred:** A static data YAML field `oom_rate` (float, 0.0-1.0) to control OOM frequency per-pod was not implemented. The hardcoded 90/10 ratio is sufficient for Phase 4 testing. If configurable OOM rates are needed, this can be added as an enhancement.

### Tests

- Unit (`test_ocp_generator.py`): Verify `OCP_ROS_USAGE_COLUMN` includes `"oom_count"`.
- Unit: Verify generated rows include `oom_count` column with value 0 when `oom_rate` is 0.
- Unit: Verify generated rows include nonzero `oom_count` values when `oom_rate` > 0 (seed random for determinism).

---

## Track B, Milestone 1: Engine -- OOM Feedback in RecommendMemory (ros-ocp-backend)

**Goal:** When OOM events are detected in the analysis window, adjust memory recommendations upward using a post-margin, logarithmic-scale bump.

### Design Decision: Post-Margin OOM Bump

The OOM bump is applied **after** the adaptive margin, as a separate multiplicative step:

```
recommendation = weighted_usage_percentile * adaptive_margin * oom_bump
```

**Why post-margin (not raising the margin floor or shifting percentiles):**

- **Kubernetes VPA** uses the same approach: compute base recommendation from usage percentiles with a safety margin, then apply an additional OOM bump that raises the recommendation above the container's last memory limit. VPA's OOM logic is a separate, clearly identifiable adjustment.
- **Separation of concerns:** The adaptive margin handles workload variability (P95-P50 spread). The OOM bump handles observed kill events. Mixing the two signals into a single margin parameter makes tuning and debugging harder.
- **Kruize** has no explicit OOM feedback -- it implicitly avoids OOM by using absolute-max as its base statistic. Our approach is more targeted: we use P95-average as the base (more cost-efficient) and add explicit OOM correction only when kills are observed.

### Design Decision: Logarithmic Scaling

The bump follows a logarithmic curve with diminishing returns:

```
bump = min(OOMMaxBump, 1.0 + OOMBaseBump * log2(1 + oom_count))
```

**Why logarithmic (not linear or binary):**

- **Diminishing returns:** The first OOM is the most informative signal ("you're under-provisioned"). Additional OOMs in the same window add less new information -- the workload is probably still running the same under-provisioned configuration.
- **Prevents over-reaction:** With linear scaling, a workload that OOMs 10 times (e.g., a CrashLoopBackOff) would get a 100% bump. Logarithmic scaling gives: 1 OOM = +15%, 3 OOMs = +30%, 7 OOMs = +45%, 15 OOMs = +60% (at cap).
- **VPA comparison:** VPA uses an exponential backoff in the opposite direction -- each successive OOM doubles the headroom, which can lead to massive over-provisioning. Our logarithmic approach is more conservative.

### Implementation

Modify `MemoryConfig` in `internal/engine/types.go`:

```go
type MemoryConfig struct {
    // ... existing fields ...
    OOMCountSum int64   // Total OOM events in the analysis window
    OOMBaseBump float64 // Base bump per log2(1+oom_count) (default 0.15)
    OOMMaxBump  float64 // Maximum OOM bump multiplier (default 1.60)
}
```

In `internal/engine/recommend_all.go`, pass `sumOOMCounts(windowRows)` into `memCfg.OOMCountSum` before calling `RecommendMemory`.

In `internal/engine/recommend_memory.go`, after computing `costRequest` / `perfRequest` (post-margin):

```go
if cfg.OOMCountSum > 0 {
    bump := math.Min(cfg.OOMMaxBump, 1.0 + cfg.OOMBaseBump*math.Log2(1+float64(cfg.OOMCountSum)))
    costRequest = int64(math.Round(float64(costRequest) * bump))
    perfRequest = int64(math.Round(float64(perfRequest) * bump))
    costLimit = int64(math.Round(float64(costRequest) * cfg.LimitMultiplier))
    perfLimit = int64(math.Round(float64(perfRequest) * cfg.LimitMultiplier))
}
```

### Default OOM Parameters

```go
OOMBaseBump: 0.15  // 15% at 1 OOM, ~30% at 3 OOMs, ~45% at 7 OOMs
OOMMaxBump:  1.60  // cap at 60% above baseline
```

**Bump curve at default parameters:**

| OOM count | Bump multiplier | Increase |
|-----------|----------------|----------|
| 1         | 1.15           | +15%     |
| 3         | 1.30           | +30%     |
| 7         | 1.45           | +45%     |
| 15        | 1.60           | capped   |

Configurable via env vars `ROS_OOM_BASE_BUMP` and `ROS_OOM_MAX_BUMP`.

### Tests

- Unit: `TestRecommendMemory_OOMBump` -- seed with `OOMCountSum = 1`, verify ~15% higher than identical data with `OOMCountSum = 0`.
- Unit: `TestRecommendMemory_OOMLogScale` -- table-driven with `OOMCountSum` of 1, 3, 7, 15; verify the bump follows the logarithmic curve (not linear).
- Unit: `TestRecommendMemory_OOMMaxBumpCap` -- seed with `OOMCountSum = 100`, verify capped at `OOMMaxBump` (1.60).
- Unit: `TestRecommendMemory_ZeroOOM` -- verify no change when `OOMCountSum = 0`.
- Unit: `TestRecommendMemory_OOMCustomParams` -- verify custom `OOMBaseBump`/`OOMMaxBump` env var values override defaults.
- Integration (`recommend_all_test.go`): Seed digests with OOM counts, call `RecommendAllWorkloads`, verify bumped `RecMemRequestKiB` and `NotifOOMDetected` notification present.

---

## Track B, Milestone 2: Recommendation Quality Tracking (ros-ocp-backend)

**Goal:** Wire the `recommendation_quality` table with all four quality metrics.

### Quality Writer

New file: `internal/engine/quality.go`

```go
func WriteRecommendationQuality(
    ctx context.Context, pool *pgxpool.Pool,
    recs []ContainerRec, oomCountsByContainer map[containerKey]int64,
) error
```

For each unique container in `recs` (deduplicate across terms/engines, use the first cost-engine entry as representative), compute:

- **`oom_events_after_rec`**: Total OOM events from the current processing batch (sum of `oom_count_sum` from digests in the analysis window). **Design note:** The original spec called for querying OOM events "after the recommendation's `updated_at`", but this is impractical because (a) the operator sends data hourly so there's no historical OOM accumulation across batches, and (b) for first-time containers there's no `updated_at` boundary. The current-batch semantics accurately reflect the OOM signal available to the engine.
- **`stability_pct`**: `max(0, 1.0 - (|variation_cpu_request_pct|/100)*0.5 - (|variation_memory_request_pct|/100)*0.5)`. Computed by reading the previous recommendation from `recommendation_sets` before `WriteRecommendations` overwrites it. Does not depend on `recommendation_history`.
- **`adoption_detected`**: `true` if current resource config matches the recommendation within 5% tolerance.
- **`recommendation_age_hours`**: hours since the most recent `updated_at` on `recommendation_sets`. Stored as `BIGINT` (truncated integer hours). At one recommendation per hour max, sub-hour precision adds no value.

Batch-insert into `recommendation_quality` using `pgx.Batch`.

### Partition Management

`EnsureQualityPartitions(ctx, pool)`: create monthly partitions (current + next 2 months), called before each quality write batch. Same `CREATE TABLE IF NOT EXISTS ... PARTITION OF ...` pattern as migration 027. Partition DDL failures are **non-fatal** (log warning, continue) since concurrent workers may race. Per-batch invocation is safe due to `IF NOT EXISTS` idempotency and provides self-healing if a partition is dropped or missed.

### Pipeline Integration

In `processContainerCSVNative` (`internal/services/report_processor.go`), the
pipeline ordering must be:

```
1. ReadOldRecommendations(ctx, pool, containerKeys) → oldRecs map
2. WriteRecommendations(ctx, pool, recs)             // overwrites old values
3. if oldRecs != nil → WriteRecommendationQuality(ctx, pool, recs, oldRecs, oomCounts)
```

If `ReadOldRecommendations` fails (returns nil), quality writes are skipped to
avoid writing misleading stability/adoption metrics with "no prior rec" defaults.

`ReadOldRecommendations` is a new helper that queries `recommendation_sets` for
the current CPU/memory request values before `WriteRecommendations` overwrites
them. These old values are needed by `stability_pct` (delta between old and new)
and `adoption_detected` (compare current resource config to old recommendation).

Updated function signature:

```go
func WriteRecommendationQuality(
    ctx context.Context, pool *pgxpool.Pool,
    newRecs []ContainerRec,
    oldRecs map[containerKey]OldRecommendation,
    oomCountsByContainer map[containerKey]int64,
) error
```

Where `OldRecommendation` holds the previous `rec_cpu_request_millicores`,
`rec_memory_request_kib`, and `updated_at` from `recommendation_sets`.

### Error Handling

Use the **fatal-with-metrics** pattern: check for `"no partition"` in errors, increment the `rosocp_quality_partition_missing_total` Prometheus counter, and return a hard error. This runs after `WriteRecommendations` succeeds, so quality write failures do not block the primary recommendation pipeline, but they are visible in monitoring.

### Tests

**Unit tests (`quality_test.go`):**

- `TestStabilityPct` -- table-driven with known CPU/memory variation percentages, verify formula output. Cases: no change (1.0), 50% CPU change (0.75), 50% both (0.5), 100% both (0.0), negative clamping to 0.
- `TestAdoptionDetection` -- table-driven: current matches recommended within 5% (true), current differs beyond 5% (false), edge case at 0 recommended value.
- `TestOOMEventsAfterRec` -- verified via current-batch OOM totals (see design note above); tested through `TestWriteRecommendationQuality_FullPipeline` and the E2E `TestProcessContainerCSVNative_WithOOMData`.
- `TestRecommendationAgeHours` -- verify truncated integer hours since `updated_at`.

**Integration tests (`quality_test.go`):**

- `TestWriteRecommendationQuality_FullPipeline` -- seed digests, run `RecommendAllWorkloads` + `WriteRecommendations` + `WriteRecommendationQuality`, query `recommendation_quality` table, verify all four fields populated with expected values.
- `TestWriteRecommendationQuality_StabilityAcrossCycles` -- seed a first recommendation, run the pipeline again with changed data, verify `stability_pct` reflects the delta between old and new recommendations (read-before-overwrite pattern).
- `TestWriteRecommendationQuality_MissingPartition` -- attempt to write a quality row for a date with no partition, verify the `partitionMissing` Prometheus counter increments and a hard error is returned.

**Partition tests (`quality_test.go`):**

- `TestEnsureQualityPartitions` -- verify partitions are created for current + next 2 months. Call twice, verify idempotent (no error on second call).
- `TestEnsureQualityPartitions_ConcurrentSafe` -- call from two goroutines simultaneously, verify no errors (exercises `IF NOT EXISTS` idempotency).

---

## End-to-End Pipeline Test (ros-ocp-backend)

Extend or add to the existing `TestProcessContainerCSVNative_EndToEnd` integration
test (`internal/services/report_processor_test.go`):

- `TestProcessContainerCSVNative_WithOOMData` -- feed a CSV containing rows with
  `oom_count > 0` through the full `processContainerCSVNative` pipeline. Verify:
  1. `daily_container_digests` has `oom_count_sum > 0` for affected containers.
  2. `recommendation_sets` has bumped `rec_memory_request_kib` (higher than the
     same workload without OOM data).
  3. `notification_codes` includes `NotifOOMDetected` (code 3).
  4. `recommendation_quality` row is written with `oom_events_after_rec >= 0`.

This test exercises the complete data flow: CSV parsing -> digest upsert ->
recommendation engine (with OOM bump) -> quality writer.

---

## IQE Tests (iqe-ros-ocp-plugin)

### Phase 3: Native Engine Contract (test_native_engine.py)

Phase 3 introduced the native Go engine with a new response format. These tests
validate the API contract that koku-ui depends on:

- `TestNativeResponseStructure.test_top_level_fields_present` -- every item has
  cluster_alias, cluster_uuid, container, id, recommendations, etc.
- `TestNativeResponseStructure.test_deterministic_id_format` -- id is a valid UUID.
- `TestMultiTermRecommendations.test_all_terms_present` -- short_term, medium_term,
  long_term keys exist in recommendations.
- `TestMultiTermRecommendations.test_each_term_has_cost_and_performance` -- both
  engines populated for each term.
- `TestEngineFields.test_engine_has_millicore_and_kib_fields` -- cpu_request_millicores
  and memory_request_kib present.
- `TestConfidenceLevel.test_confidence_level_present_and_valid` -- confidence_level is
  a float in [0.0, 1.0].
- `TestNotificationCodes.test_notification_codes_are_int_array` -- notification_codes
  is a list of integers.
- `TestNotificationCodes.test_notifications_map_matches_codes` -- notifications map
  keys match notification_codes entries, each with type/message/code.

### Phase 4: OOM Feedback (test_oom.py)

- `TestOOMNotificationPresent.test_oom_notification_present` -- ingest OOM-prone
  nise data, verify at least one container has NotifOOMDetected (code 3).
- `TestOOMNotificationPresent.test_oom_notification_has_correct_kruize_entry` --
  code 3 maps to type=CRITICAL with an OOM-related message.
- `TestOOMBumpedRecommendation.test_oom_bumped_memory_recommendation` -- container
  with OOM notification has memory_request_kib > current_memory_request_kib.
- `test_recommendation_quality_populated` -- **skipped in Phase 4**. Quality
  data is internal-only (written to `recommendation_quality` table but not
  exposed via API). Validated by Go integration tests. API exposure deferred
  to a future phase.

**Note:** IQE tests depend on both Track A (nise generating OOM data) and Track B
(engine processing it). They should be the last tests written, after both tracks
are functional.

---

## Known Gaps (Out of Scope -- Future Phases)

- **Boxplots in native engine:** Deferred to [Phase 5](phase-5-history-and-boxplots.md). The native engine does not produce boxplot data. Boxplots are Kruize-only today (stored in `recommendation_sets.recommendations` JSONB). Computing them from `daily_container_digests` percentiles is feasible.
- **`recommendation_history` table wiring:** Deferred to [Phase 5](phase-5-history-and-boxplots.md). The `stability_pct` quality metric does NOT depend on history -- it is computed by reading the current values from `recommendation_sets` before `WriteRecommendations` overwrites them.
- **`recommendation_history` retention policy:** Deferred to [Phase 5](phase-5-history-and-boxplots.md).

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| OOM bump parameters (15% base / 1.60x cap) may need tuning | Configurable via env vars from the start; structured logging of applied bump |
| Quality write overhead at 100K containers | `pgx.Batch` (same as `WriteRecommendations`); table partitioned monthly |
| Nise + operator changes lag behind backend | Track B fully testable with synthetic CSV fixtures; no blocking dependency |
| Partition DDL race between concurrent workers | `CREATE TABLE IF NOT EXISTS` makes partition creation idempotent; DDL failures are non-fatal (warning only). Missing partitions at write time use fatal-with-metrics pattern for visibility. |

## Implementation Status

| Milestone | Status | Notes |
|-----------|--------|-------|
| Prerequisite: CSV column rename | **Done** | `csvparser.go` + all test fixtures updated |
| Track A M1: Operator OOM PromQL + CSV | **Done** | `ros:oom_count_container_sum` query, `OOMCount` field in `rosContainerRow`, golden CSV updated |
| Track A M2: Nise oom_count | **Done** | `oom_count` in `OCP_ROS_USAGE_COLUMN`, 90/10 weighted random generation |
| Track B M1: OOM bump in RecommendMemory | **Done** | Post-margin log-scale bump, configurable via `ROS_OOM_BASE_BUMP`/`ROS_OOM_MAX_BUMP`, 6 unit tests |
| Track B M2: Quality writer | **Done** | `quality.go` with all 4 metrics, 3-step pipeline, partition management, Prometheus counter, unit + integration tests |
| E2E pipeline test | **Done** | `TestProcessContainerCSVNative_WithOOMData` -- validates digest OOM accumulation, memory bump, and quality metrics |
| Audit fixes | **Done** | `ReadOldRecommendations` now filters by container keys; `qualityPartitionMissing` Prometheus counter added; `TestOOMCountsByContainer` + `TestWriteRecommendationQuality_MissingPartition` + operator `types_test.go` + nise OOM unit tests added |
| IQE tests | **Done** | `test_native_engine.py` (8 Phase 3 tests) + `test_oom.py` (3 Phase 4 tests) + nise YAML + fixture -- awaiting cluster deployment to execute |

## Branching

- **koku-metrics-operator:** `pgarciaq-rosocp-superpowers-phase4` off `main`
- **ros-ocp-backend:** `pgarciaq-rosocp-superpowers-phase4` off `pgarciaq-rosocp-superpowers-phase3`
- **nise:** `pgarciaq-rosocp-superpowers-phase4` off `main`
- **iqe-ros-ocp-plugin:** `pgarciaq-rosocp-superpowers-phase4` off `pgarciaq-rosocp-superpowers-phase3`
