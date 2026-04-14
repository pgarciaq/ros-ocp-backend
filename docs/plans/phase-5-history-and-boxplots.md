# Phase 5: Recommendation History, Boxplots, and Retention

## Status

- **Phase**: Planning complete, implementation not started.
- **Branch**: `pgarciaq-rosocp-superpowers-phase5` (ros-ocp-backend, iqe-ros-ocp-plugin)
- **Depends on**: Phase 4 (OOM feedback, recommendation quality, CSV alignment).

## Scope

Three deliverables:

1. **Recommendation History** — snapshot every recommendation into
   `recommendation_history` (already created in migration 027) for trend
   analysis, adoption detection, and audit.
2. **Boxplots** — compute hourly usage statistics during ingestion, serve them
   in the native detail endpoint matching the Kruize JSON shape the UI already
   consumes.
3. **Retention** — periodic goroutine that drops old monthly partitions from
   `hourly_container_stats`, `daily_container_digests`,
   `recommendation_history`, and `recommendation_quality`.

---

## Architecture Decisions

### AD-1: Hourly stats table (new) vs reuse daily_container_digests

**Decision: New `hourly_container_stats` table.**

The UI expects different granularity per term:

| Term | UI X-axis | Expected data points |
|------|-----------|---------------------|
| short_term (24h) | Time of day (`kk:mm`) | ~24 hourly buckets |
| medium_term (7d) | Calendar day (`MMM d`) | ~7 daily buckets |
| long_term (15d) | Calendar day (`MMM d`) | ~15 daily buckets |

`daily_container_digests` is daily-only and lacks min/q1/q3 columns (it stores
p50/p95/p99/max/mean — percentiles used by the recommendation engine, not the
five-number summary needed for boxplots). Creating a separate hourly table:

- Gives exact short_term support (hourly buckets).
- Medium/long term boxplots are derived by aggregating hourly rows into daily
  rows at query time (one SQL `GROUP BY date_trunc('day', bucket_hour)`).
- Clean separation: digests feed the recommendation engine, hourly stats feed
  the boxplot UI.
- No schema changes to the existing `daily_container_digests` table.

### AD-2: Native detail response → Kruize-compatible shape

**Decision: `ToKruizeCompatibleShape()` wrapper in the detail handler.**

The UI types expect the Kruize response shape:

```
recommendations.recommendation_terms.<term>.plots.plots_data
recommendations.monitoring_end_time
```

The current native response has a different structure:

```
recommendations.<term>.cost / .performance   (no recommendation_terms wrapper)
```

This incompatibility is masked today because the fallback handler often serves
Kruize data for detail requests. To make boxplots (and future native features)
work without koku-ui changes, the native detail handler will apply a
`ToKruizeCompatibleShape()` transformation that wraps the native data in the
Kruize-compatible JSON structure. The list endpoint is unaffected (it already
strips plots).

This is an **additive, backward-compatible** change to the native detail
endpoint (adds `recommendation_terms`, `monitoring_end_time`, `plots`).

### AD-3: `monitoring_end_time` source

**Decision: `MAX(bucket_date)` from `daily_container_digests`.**

This represents the last day we have actual usage data for a container, which
is the most semantically correct definition of "when monitoring ended."
`updated_at` on `recommendation_sets` represents when the recommendation was
computed, not when data was collected.

Queried once per detail request; single indexed `MAX()` per container is cheap.

### AD-4: Retention mechanism

**Decision: Periodic goroutine with `time.Ticker`.**

ros-ocp-backend is a Go service (no Celery). A background goroutine:

- Runs every 24 hours (checked at startup, then ticker).
- `DROP TABLE IF EXISTS` partitions older than `ROS_RETENTION_MONTHS` (default 6).
- Covers: `hourly_container_stats`, `daily_container_digests`,
  `recommendation_history`, `recommendation_quality`.
- Self-contained, no external config or CronJob needed.
- Graceful shutdown via context cancellation.

---

## Database Schema

### New table: `hourly_container_stats` (migration 000029)

```sql
CREATE TABLE IF NOT EXISTS hourly_container_stats (
    bucket_hour         TIMESTAMPTZ NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    workload_type       TEXT NOT NULL,
    container_name      TEXT NOT NULL,
    cpu_usage_min_mc    BIGINT,
    cpu_usage_q1_mc     BIGINT,
    cpu_usage_median_mc BIGINT,
    cpu_usage_q3_mc     BIGINT,
    cpu_usage_max_mc    BIGINT,
    mem_usage_min_kib   BIGINT,
    mem_usage_q1_kib    BIGINT,
    mem_usage_median_kib BIGINT,
    mem_usage_q3_kib    BIGINT,
    mem_usage_max_kib   BIGINT,
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, bucket_hour)
) PARTITION BY RANGE (bucket_hour);
```

Initial partitions: current + next 2 months (same pattern as migration 022).

### Existing tables (no schema changes)

- `recommendation_history` (migration 027) — already exists, needs Go wiring.
- `recommendation_quality` (migration 027) — already exists, no Phase 5 changes.
- `daily_container_digests` (migration 022) — unchanged, retention only.

---

## Milestones

### Milestone 1: Recommendation History Writer

**Goal**: Wire `recommendation_history` table so every recommendation run
creates a snapshot.

**Files**:
- `internal/ingestion/history.go` (new)
  - `WriteRecommendationHistory(ctx, pool, recs []ContainerRec, sourceBinary string) error`
  - Uses `pgx.Batch` (same pattern as digest upsert).
  - `EnsureHistoryPartitions(ctx, pool)` — creates monthly partitions, called
    at startup and before writes.
- `internal/ingestion/pipeline.go`
  - Call `WriteRecommendationHistory` after `WriteRecommendations` in the
    pipeline (always-on, no feature flag).

**Tests**:
- `internal/ingestion/history_test.go`
  - `TestWriteRecommendationHistory_InsertsSnapshot`
  - `TestWriteRecommendationHistory_MultipleTermsAndEngines`
  - `TestEnsureHistoryPartitions_CreatesPartition`

**Volume estimates**: ~6 rows per container per run (3 terms × 2 engines).
10K containers → 60K rows/day (~6 MB/day, ~180 MB/month).

### Milestone 2: Hourly Container Stats Table

**Goal**: Create `hourly_container_stats` and populate it during ingestion.

**Files**:
- `migrations/000029_create_hourly_container_stats.up.sql` (new)
- `migrations/000029_create_hourly_container_stats.down.sql` (new)
- `internal/ingestion/models.go`
  - Add `HourlyStatsKey` struct (same as `DigestKey` but with `BucketHour time.Time`
    instead of `BucketDate`).
  - Add `HourlyBoxplot` result struct (min, q1, median, q3, max for CPU and mem).
- `internal/ingestion/digest.go`
  - Add `GroupCSVRowsByHour()` — groups `MetricRow` by container+hour.
  - Add `ComputeHourlyBoxplot(key, rows)` — sorts values, computes five-number
    summary using `percentileFromSorted` (p0=min, p25=q1, p50=median, p75=q3,
    p100=max).
- `internal/ingestion/pipeline.go`
  - After daily digest upsert, also group by hour, compute boxplots, and
    batch-upsert to `hourly_container_stats`.
  - Add `EnsureHourlyPartitions(ctx, pool, keys)`.

**Tests**:
- `internal/ingestion/digest_test.go`
  - `TestGroupCSVRowsByHour`
  - `TestComputeHourlyBoxplot`
- `internal/ingestion/pipeline_test.go`
  - `TestProcessCSVToDigests_AlsoWritesHourlyStats`
  - `TestEnsureHourlyPartitions`

**Storage estimates**: ~24 rows per container per day.
10K containers → 240K rows/day (~24 MB/day, ~720 MB/month).

### Milestone 3: Boxplot Assembly

**Goal**: Query hourly stats and assemble the `plots.plots_data` structure
matching the Kruize JSON shape.

**Files**:
- `internal/model/boxplot.go` (new)
  - `AssembleBoxplots(ctx, pool, containerKey, termDays int) (*PlotData, error)`
    - Queries `hourly_container_stats` for the container over the term window.
    - **Short term (1 day)**: Returns hourly buckets directly.
    - **Medium/Long term (7/15 days)**: Aggregates hourly rows into daily
      buckets using `GROUP BY date_trunc('day', bucket_hour)` with
      `MIN(min)`, percentile-of-percentiles for q1/median/q3, `MAX(max)`.
    - Returns a map of ISO timestamp → `{cpuUsage, memoryUsage}` matching
      the Kruize `PlotsData` structure.
  - `MonitoringEndTime(ctx, pool, containerKey) (time.Time, error)`
    - `SELECT MAX(bucket_date) FROM daily_container_digests WHERE ...`

**Structs** (matching `internal/types/kruizePayload/common.go`):
```go
type NativePlot struct {
    DataPoints int                          `json:"datapoints"`
    PlotsData  map[string]NativePlotsData   `json:"plots_data"`
}

type NativePlotsData struct {
    CPUUsage    *BoxPlotDetails `json:"cpuUsage,omitempty"`
    MemoryUsage *BoxPlotDetails `json:"memoryUsage,omitempty"`
}

type BoxPlotDetails struct {
    Min    float64 `json:"min"`
    Q1     float64 `json:"q1"`
    Median float64 `json:"median"`
    Q3     float64 `json:"q3"`
    Max    float64 `json:"max"`
    Format string  `json:"format"`
}
```

**Tests**:
- `internal/model/boxplot_test.go`
  - `TestAssembleBoxplots_ShortTerm_HourlyGranularity`
  - `TestAssembleBoxplots_MediumTerm_DailyAggregation`
  - `TestAssembleBoxplots_LongTerm_DailyAggregation`
  - `TestAssembleBoxplots_NoData_ReturnsEmpty`
  - `TestMonitoringEndTime`

### Milestone 4: API Integration

**Goal**: Serve boxplots in the native detail endpoint using the
Kruize-compatible JSON shape.

**Files**:
- `internal/model/recommendation_set_native.go`
  - Add `MonitoringEndTime string` to `NativeContainerResult`.
  - Add `Plots *NativePlot` to `TermRecommendation`.
  - Add `ToKruizeCompatibleShape() map[string]interface{}` method that wraps
    the native response in the Kruize JSON structure:
    ```json
    {
      "recommendations": {
        "monitoring_end_time": "2026-03-26T00:00:00Z",
        "recommendation_terms": {
          "short_term": {
            "plots": { "datapoints": 24, "plots_data": { ... } },
            "recommendation_engines": {
              "cost": { ... },
              "performance": { ... }
            }
          }
        }
      }
    }
    ```
- `internal/api/handlers.go`
  - In `GetNativeRecommendationSet` (and the native branch of
    `GetRecommendationSetWithFallback`): after fetching the native result,
    call `AssembleBoxplots` for each term and populate the `Plots` field.
    Query `MonitoringEndTime` once. Return via `ToKruizeCompatibleShape()`.
  - List handler: no changes (plots not included in list responses, matching
    the existing `dropBoxPlotsObject` behavior for Kruize).

**Tests**:
- `internal/api/handlers_integration_test.go`
  - `TestGetNativeRecommendationSet_IncludesBoxplots`
  - `TestGetNativeRecommendationSet_IncludesMonitoringEndTime`
  - `TestGetNativeRecommendationSetList_ExcludesBoxplots`
  - `TestToKruizeCompatibleShape_MatchesUITypes`

### Milestone 5: Retention Policy

**Goal**: Periodic background goroutine that drops old partitions.

**Files**:
- `internal/retention/sweep.go` (new)
  - `StartRetentionSweep(ctx context.Context, pool *pgxpool.Pool, retentionMonths int)`
    - Runs immediately at startup, then every 24 hours via `time.Ticker`.
    - For each managed table (`hourly_container_stats`,
      `daily_container_digests`, `recommendation_history`,
      `recommendation_quality`): lists partitions from `pg_class` /
      `pg_inherits`, identifies any older than `retentionMonths`, drops them.
    - Respects context cancellation for graceful shutdown.
  - Config: `ROS_RETENTION_MONTHS` env var (default 6).
- `cmd/ros-ocp-backend/main.go`
  - Start `RetentionSweep` goroutine alongside HTTP server.

**Tests**:
- `internal/retention/sweep_test.go`
  - `TestRetentionSweep_DropsOldPartitions`
  - `TestRetentionSweep_KeepsRecentPartitions`
  - `TestRetentionSweep_ConfigurableMonths`

### Milestone 6: IQE Tests

**Goal**: End-to-end tests validating boxplots and `monitoring_end_time` on a
live cluster.

**Repo**: `iqe-ros-ocp-plugin` (branch `pgarciaq-rosocp-superpowers-phase5`).

**Files**:
- `iqe_ros_ocp/tests/rest/test_boxplots.py` (new)
  - `TestBoxplots`
    - `test_detail_response_includes_plots` — fetch detail by ID, assert
      `recommendations.recommendation_terms.<term>.plots.plots_data` is
      present and non-empty for at least one term.
    - `test_plots_data_has_correct_shape` — assert each data point has
      `cpuUsage` and `memoryUsage` with `min`, `q1`, `median`, `q3`, `max`,
      `format`.
    - `test_short_term_has_hourly_granularity` — assert short_term
      `plots_data` keys are ISO timestamps with different hours within the
      same day.
    - `test_medium_long_term_has_daily_granularity` — assert medium/long
      term keys are daily timestamps.
    - `test_list_response_excludes_plots` — fetch list, assert no `plots`
      field in any item.
    - `test_monitoring_end_time_present` — assert
      `recommendations.monitoring_end_time` is a valid ISO timestamp.

**Fixture**: Uses existing `ocp_source_with_ingestion_using_default_ros_ocp_yaml`
with `n_days=16` (already produces enough data for long_term).

---

## Data Flow (Ingestion)

```
CSV (hourly samples from operator)
  │
  ├──▶ GroupCSVRows (by container+day)
  │      └──▶ ComputeContainerDigest → daily_container_digests (existing)
  │
  ├──▶ GroupCSVRowsByHour (by container+hour)      [NEW]
  │      └──▶ ComputeHourlyBoxplot → hourly_container_stats  [NEW]
  │
  └──▶ RunNativeEngine → recommendation_sets (existing)
         └──▶ WriteRecommendationHistory → recommendation_history  [NEW]
```

## Data Flow (API — Detail Request)

```
GET /recommendations/openshift/:id
  │
  ├──▶ GetNativeRecommendationByID → NativeContainerResult
  │
  ├──▶ MonitoringEndTime(container) → MAX(bucket_date)
  │
  ├──▶ For each term:
  │      AssembleBoxplots(container, termDays) → PlotData
  │
  └──▶ ToKruizeCompatibleShape() → JSON response matching UI types
```

---

## Cross-Repo Dependencies

| Repo | Branch | Changes |
|------|--------|---------|
| ros-ocp-backend | `pgarciaq-rosocp-superpowers-phase5` | Milestones 1-5 (all backend work) |
| iqe-ros-ocp-plugin | `pgarciaq-rosocp-superpowers-phase5` | Milestone 6 (IQE tests) |

No changes needed in koku, koku-ui, nise, or koku-metrics-operator.
The UI already supports the Kruize boxplot JSON shape — no frontend changes
required thanks to the `ToKruizeCompatibleShape()` wrapper.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Hourly stats storage grows large (720 MB/month per 10K containers) | Disk pressure on small clusters | 6-month retention sweep; configurable via `ROS_RETENTION_MONTHS` |
| Aggregating hourly→daily in SQL at query time may be slow for large datasets | Slow detail endpoint | Index on `(org_id, cluster_uuid, namespace, workload, container_name, bucket_hour)`; bounded by term window (max 15 days × 24h = 360 rows) |
| Native detail shape change breaks existing consumers | API regression | Additive change only (new fields); existing fields unchanged |
| `ToKruizeCompatibleShape()` diverges from actual Kruize format over time | UI breaks | Covered by IQE tests that validate the exact field paths |
| Operator sends < 24 samples/day for some containers | Sparse boxplots | UI already handles empty `plots_data` with padding from `monitoring_end_time` |

---

## Milestone Order

1. **M1: History Writer** — no dependencies, purely additive
2. **M2: Hourly Stats Table** — migration + ingestion pipeline change
3. **M3: Boxplot Assembly** — depends on M2 (needs hourly data)
4. **M4: API Integration** — depends on M3 (needs boxplot assembly)
5. **M5: Retention** — independent, can be done in parallel with M3/M4
6. **M6: IQE Tests** — depends on M4 (needs API to serve boxplots)
