> **Status: Implemented.** See [`docs/phase5-implementation-notes.md`](../phase5-implementation-notes.md) for as-built details.

# Phase 5: Recommendation History, Boxplots, and Retention

## Status

- **Phase**: Implemented (history, boxplots, retention).
- **Branch**: Landed on main development branches (see implementation notes).
- **Depends on**: Phase 4 (OOM feedback, recommendation quality, CSV alignment) — satisfied.

## Scope

Three deliverables:

1. **Recommendation History** — snapshot every recommendation into
   `recommendation_history` (already created in migration 027) for trend
   analysis, adoption detection, and audit.
2. **Boxplots** — persist raw per-sample usage measurements during ingestion,
   compute exact boxplot statistics at query time, and serve them in the native
   detail endpoint matching the Kruize JSON shape the UI already consumes.
3. **Retention** — periodic goroutine that drops old monthly partitions from
   `container_usage_samples`, `daily_container_digests`,
   `recommendation_history`, and `recommendation_quality`.

---

## Architecture Decisions

### AD-1: Raw samples table (new) — exact query-time boxplots

**Decision: New `container_usage_samples` table storing raw per-sample (15-min)
measurements. Boxplots computed at query time via PostgreSQL `percentile_cont()`.**

The operator produces ~4 samples per hour (15-minute intervals), ~96 per day.
Pre-computing boxplots per hour from only 4 data points is statistically thin,
and aggregating pre-computed hourly boxplots into daily boxplots via
"percentile-of-percentiles" is mathematically incorrect (median of medians ≠
true median).

Instead, we store the raw scalar values (just `cpu_usage_mc` and
`mem_usage_kib` per sample) and compute **exact** five-number summaries at
query time by grouping into the appropriate window:

| Term | Window size | Samples per window | Data points on chart |
|------|-------------|-------------------|---------------------|
| short_term (24h) | 6 hours | ~24 | 4 (matching Kruize convention) |
| medium_term (7d) | 1 day | ~96 | 7 |
| long_term (15d) | 1 day | ~96 | 15 |

Benefits:
- **Exact statistics** — `percentile_cont(0.25)`, `percentile_cont(0.75)` on
  raw values, no approximation.
- **Simple schema** — 2 metric columns (CPU, memory) vs 10+ pre-computed stats.
- **Max query scope is 1440 rows per container** (15 days × 96/day) — trivially
  fast even without pagination.
- Clean separation: digests feed the recommendation engine, samples feed boxplots.

### AD-2: Strongly-typed `DetailResponse` struct (not runtime JSON manipulation)

**Decision: Create typed Go structs matching the Kruize JSON shape. Populate
from native data field-by-field. Marshal directly to JSON.**

The existing Kruize path's `transformComponentUnits()` uses loosely-typed
`map[string]interface{}` with fragile type assertions. For the native path, a
strongly-typed approach gives:

- **Compile-time safety** — field mismatches caught at build time, not runtime.
- **No JSON round-trip overhead** — Go struct → JSON marshal in one pass.
- **Testable** — compare struct fields directly, no JSON parsing in tests.

The detail handler builds a `DetailResponse` from native data and marshals it.
The list handler is unaffected (no plots in list responses).

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
- Covers: `container_usage_samples`, `daily_container_digests`,
  `recommendation_history`, `recommendation_quality`.
- Self-contained, no external config or CronJob needed.
- Graceful shutdown via context cancellation.

### AD-5: Unit conversion in boxplot values

**Decision: Convert in Go assembly function, not in SQL.**

Data is stored in raw units (millicores, KiB). The SQL query returns raw
integers. The Go `AssembleBoxplots()` function converts to the requested units
(e.g., cores = mc / 1000, MiB = KiB / 1024) and sets the `format` field
(`"cores"`, `"MiB"`). This keeps SQL clean and makes unit conversion testable
in isolation.

### AD-6: Recommendation history lives in `engine/` package

**Decision: `WriteRecommendationHistory` in `internal/engine/` alongside
`WriteRecommendations`.**

The function operates on `[]ContainerRec` (an `engine` type) and is called
from `services/report_processor.go` immediately after `WriteRecommendations`.
Keeping both write functions in the same package maintains cohesion.

---

## Database Schema

### New table: `container_usage_samples` (migration 000029)

```sql
CREATE TABLE IF NOT EXISTS container_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    workload        TEXT NOT NULL,
    workload_type   TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, sample_time)
) PARTITION BY RANGE (sample_time);
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
- `internal/engine/history.go` (new)
  - `WriteRecommendationHistory(ctx, pool, recs []ContainerRec, sourceBinary string) error`
  - Uses `pgx.Batch` (same pattern as `WriteRecommendations`).
  - `EnsureHistoryPartitions(ctx, pool)` — creates monthly partitions, called
    at startup and before writes.
- `internal/services/report_processor.go`
  - Call `WriteRecommendationHistory` after `WriteRecommendations` (line ~435).
  - Always-on, no feature flag.

**Tests**:
- `internal/engine/history_test.go`
  - `TestWriteRecommendationHistory_InsertsSnapshot`
  - `TestWriteRecommendationHistory_MultipleTermsAndEngines`
  - `TestEnsureHistoryPartitions_CreatesPartition`

**Volume estimates**: ~6 rows per container per run (3 terms × 2 engines).
10K containers → 60K rows/day (~6 MB/day, ~180 MB/month).

### Milestone 2: Container Usage Samples Table

**Goal**: Create `container_usage_samples` and populate it during ingestion.

**Files**:
- `migrations/000029_create_container_usage_samples.up.sql` (new)
- `migrations/000029_create_container_usage_samples.down.sql` (new)
- `internal/ingestion/models.go`
  - Add `SampleKey` struct (container identity + `SampleTime time.Time`).
- `internal/ingestion/pipeline.go`
  - After daily digest upsert, batch-upsert raw CSV rows into
    `container_usage_samples` (one row per `MetricRow`, storing only
    `IntervalStart`, `CPUUsageMC`, `MemUsageKiB`).
  - Add `EnsureSamplePartitions(ctx, pool, rows []MetricRow)`.

**Tests**:
- `internal/ingestion/pipeline_test.go`
  - `TestProcessCSVToDigests_AlsoWritesSamples`
  - `TestEnsureSamplePartitions`

**Storage estimates**: ~96 rows per container per day (~30 bytes each).
10K containers → 960K rows/day (~28 MB/day, ~860 MB/month).
With 6-month retention: ~5 GB max for 10K containers.

### Milestone 3: Boxplot Assembly

**Goal**: Query raw samples and compute exact boxplot statistics at query time
using PostgreSQL `percentile_cont()`.

**Files**:
- `internal/model/boxplot.go` (new)
  - `AssembleBoxplots(ctx, pool, containerKey, termConfig) (*PlotData, error)`
    - Queries `container_usage_samples` for the container over the term window.
    - **Short term (24h)**: `GROUP BY` 6-hour windows (4 data points, ~24
      samples each). Uses `floor(extract(epoch from sample_time) / 21600)`
      for bucketing.
    - **Medium/Long term (7/15 days)**: `GROUP BY date_trunc('day', sample_time)`
      (7 or 15 data points, ~96 samples each).
    - SQL computes exact `MIN()`, `percentile_cont(0.25)`, `percentile_cont(0.5)`,
      `percentile_cont(0.75)`, `MAX()` per bucket.
    - Go converts millicores → cores (`/ 1000.0`) and KiB → MiB (`/ 1024.0`),
      sets `format` to `"cores"` / `"MiB"`.
    - Returns map of ISO timestamp → `{cpuUsage, memoryUsage}`.
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
  - `TestAssembleBoxplots_ShortTerm_6HourWindows` — 24 samples across 24h
    → 4 boxplots with exact percentiles.
  - `TestAssembleBoxplots_MediumTerm_DailyWindows` — 672 samples across 7d
    → 7 boxplots.
  - `TestAssembleBoxplots_LongTerm_DailyWindows` — 1440 samples across 15d
    → 15 boxplots.
  - `TestAssembleBoxplots_NoData_ReturnsEmpty`
  - `TestAssembleBoxplots_UnitConversion` — verify mc → cores, KiB → MiB.
  - `TestMonitoringEndTime`

### Milestone 4: API Integration

**Goal**: Serve boxplots in the native detail endpoint using a strongly-typed
Kruize-compatible response struct.

**Files**:
- `internal/model/detail_response.go` (new)
  - Strongly-typed structs matching the Kruize JSON shape:
    ```go
    type DetailResponse struct {
        ID              string                    `json:"id"`
        ClusterAlias    string                    `json:"cluster_alias"`
        ClusterUUID     string                    `json:"cluster_uuid"`
        Container       string                    `json:"container"`
        Project         string                    `json:"project"`
        Workload        string                    `json:"workload"`
        WorkloadType    string                    `json:"workload_type"`
        SourceID        string                    `json:"source_id"`
        LastReported    string                    `json:"last_reported"`
        Recommendations DetailRecommendations     `json:"recommendations"`
    }

    type DetailRecommendations struct {
        MonitoringEndTime  string                       `json:"monitoring_end_time"`
        RecommendationTerms map[string]DetailTerm       `json:"recommendation_terms"`
    }

    type DetailTerm struct {
        DurationInHours       float64              `json:"duration_in_hours"`
        Plots                 *NativePlot          `json:"plots,omitempty"`
        RecommendationEngines *DetailEngines       `json:"recommendation_engines,omitempty"`
    }

    type DetailEngines struct {
        Cost        *EngineRecommendation `json:"cost,omitempty"`
        Performance *EngineRecommendation `json:"performance,omitempty"`
    }
    ```
  - `func BuildDetailResponse(native *NativeContainerResult, plots map[string]*NativePlot, monitoringEndTime time.Time) *DetailResponse`
    — maps native data into the Kruize-compatible structure.
- `internal/api/handlers.go`
  - In `GetNativeRecommendationSet` and the native branch of
    `GetRecommendationSetWithFallback`: fetch native result, call
    `AssembleBoxplots` for each term, query `MonitoringEndTime`, build
    `DetailResponse`, return as JSON.
  - List handler: no changes (no plots in list responses).

**Tests**:
- `internal/model/detail_response_test.go`
  - `TestBuildDetailResponse_MapsAllFields`
  - `TestBuildDetailResponse_IncludesPlots`
  - `TestBuildDetailResponse_MonitoringEndTimeFormatted`
  - `TestBuildDetailResponse_NilPlots_OmittedInJSON`
- `internal/api/handlers_integration_test.go`
  - `TestGetNativeRecommendationSet_IncludesBoxplots`
  - `TestGetNativeRecommendationSet_IncludesMonitoringEndTime`
  - `TestGetNativeRecommendationSetList_ExcludesBoxplots`

### Milestone 5: Retention Policy

**Goal**: Periodic background goroutine that drops old partitions.

**Files**:
- `internal/retention/sweep.go` (new)
  - `StartRetentionSweep(ctx context.Context, pool *pgxpool.Pool, retentionMonths int)`
    - Runs immediately at startup, then every 24 hours via `time.Ticker`.
    - For each managed table (`container_usage_samples`,
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
    - `test_short_term_has_4_windows` — assert short_term `plots_data` has
      exactly 4 keys (6-hour windows within 24h).
    - `test_medium_long_term_has_daily_granularity` — assert medium/long
      term keys are daily timestamps with expected count (7/15).
    - `test_list_response_excludes_plots` — fetch list, assert no `plots`
      field in any item.
    - `test_monitoring_end_time_present` — assert
      `recommendations.monitoring_end_time` is a valid ISO timestamp.

**Fixture**: Uses existing `ocp_source_with_ingestion_using_default_ros_ocp_yaml`
with `n_days=16` (Nise generates 15-minute interval data via
`_gen_quarter_hourly_ros_ocp_pods_usage`, producing ~96 samples/day/container —
plenty for all three terms).

---

## Data Flow (Ingestion)

```
CSV (15-minute samples from operator)
  │
  ├──▶ GroupCSVRows (by container+day)
  │      └──▶ ComputeContainerDigest → daily_container_digests (existing)
  │
  ├──▶ Batch-upsert raw samples → container_usage_samples  [NEW]
  │    (one row per MetricRow: sample_time, cpu_usage_mc, mem_usage_kib)
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
  ├──▶ MonitoringEndTime(container) → MAX(bucket_date) from digests
  │
  ├──▶ For each term:
  │      AssembleBoxplots(container, term) →
  │        SELECT percentile_cont(0.25, 0.5, 0.75), MIN, MAX
  │        FROM container_usage_samples
  │        GROUP BY <6h window or day>
  │
  └──▶ BuildDetailResponse(native, plots, monitoringEndTime) → JSON
       (strongly-typed struct matching Kruize shape; one marshal pass)
```

---

## Cross-Repo Dependencies

| Repo | Branch | Changes |
|------|--------|---------|
| ros-ocp-backend | `pgarciaq-rosocp-superpowers-phase5` | Milestones 1-5 (all backend work) |
| iqe-ros-ocp-plugin | `pgarciaq-rosocp-superpowers-phase5` | Milestone 6 (IQE tests) |

No changes needed in koku, koku-ui, nise, or koku-metrics-operator.
The UI already supports the Kruize boxplot JSON shape — no frontend changes
required thanks to the strongly-typed `DetailResponse` struct.

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Raw sample storage (~860 MB/month per 10K containers) | Disk pressure on small clusters | 6-month retention sweep; configurable via `ROS_RETENTION_MONTHS`; max ~5 GB for 10K containers |
| `percentile_cont()` at query time | Slow detail endpoint | Max 1440 rows per container per query (15 days); PK index covers the WHERE clause; benchmarked trivial |
| Native detail shape change breaks existing consumers | API regression | Additive change only (new fields); existing consumers ignore unknown fields |
| `DetailResponse` struct diverges from Kruize format over time | UI breaks | IQE tests validate the exact field paths; struct fields are compile-time checked |
| Operator sends < 96 samples/day for some containers | Sparse boxplots | UI already handles empty `plots_data` with padding from `monitoring_end_time` |
| Concurrent `DROP TABLE` during retention vs active queries | Query errors | Retention drops partitions > 6 months old; no API queries target that range |

---

## Milestone Order

1. **M1: History Writer** — no dependencies, purely additive
2. **M2: Usage Samples Table** — migration + ingestion pipeline change
3. **M3: Boxplot Assembly** — depends on M2 (needs sample data)
4. **M4: API Integration** — depends on M3 (needs boxplot assembly)
5. **M5: Retention** — independent, can be done in parallel with M3/M4
6. **M6: IQE Tests** — depends on M4 (needs API to serve boxplots)
