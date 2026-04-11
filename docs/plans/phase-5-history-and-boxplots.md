# Phase 5: Recommendation History, Boxplots, and Retention (Stub)

This is a tracking document for deferred items from Phase 4. It will be
expanded into a full plan when Phase 5 begins.

## Deferred Items

### 1. `recommendation_history` Table Wiring

The table exists (migration 027) with columns for timestamped recommendation
snapshots: `rec_cpu_request_millicores`, `rec_memory_request_kib`, etc.,
partitioned by `recorded_at`.

**Use cases:**
- Debugging recommendation instability (did recommendations oscillate? converge?)
- Adoption detection over time (did the user apply our recommendation? revert?)
- Confidence trend dashboards (how long until recommendations stabilize for new workloads?)
- Historical audit trail for compliance

**Implementation sketch** (from the original Phase 4 design):

```go
func WriteRecommendationHistory(
    ctx context.Context, pool *pgxpool.Pool,
    recs []ContainerRec, sourceBinary string,
) error
```

- Inserts a snapshot per recommendation into `recommendation_history` with
  `recorded_at = now()`, `engine`, and `source_binary` (git hash or build tag).
- Called after `WriteRecommendations` in `processContainerCSVNative`, always-on.
- Uses `pgx.Batch` (same pattern as `WriteRecommendations`).
- `EnsureHistoryPartitions(ctx, pool)` creates monthly partitions at startup.
- Fatal-with-metrics error handling for missing partitions at write time.

**Write volume estimates:**
- 10K containers: 60K rows/day, ~6 MB/day, 180 MB/month
- 100K containers: 600K rows/day, ~60 MB/day, 1.8 GB/month

### 2. `recommendation_history` Retention Policy

Monthly partitions enable `DROP TABLE` for old months. Expected approach:
6-month rolling window (configurable). Could be a periodic Celery task or
a startup sweep in ros-ocp-backend.

### 3. Boxplots in Native Engine

The native engine does not produce boxplot data. Boxplots are a Kruize-only
feature today -- Kruize computes `plots` (min/q1/median/q3/max) and they are
stored in the `recommendation_sets.recommendations` JSONB column.

**Approach:** Compute boxplots from `daily_container_digests`, which already
stores percentile/min/max/mean aggregates per bucket. The native
`EngineRecommendation` struct would need a `Plots` field, and the assembly
logic in `assembleNativeResults` would need to query digests for the
relevant time window and compute the five-number summary.

**API integration:** The native detail handler (`GetNativeRecommendationSet`)
would include plots in the response. The native list handler would strip
them (matching the Kruize path's `dropBoxPlotsObject` behavior).

## Dependencies

- Phase 4 must be complete (OOM feedback, quality metrics, CSV alignment).
- No cross-repo dependencies -- all work is in ros-ocp-backend.
