# T-Digest for Koku Cost Analytics — Feasibility Analysis

## 1. Current State

Koku's entire cost calculation pipeline uses **simple additive aggregations** — `SUM`, `MAX`, `MIN`, `COUNT`, `GROUP BY`. There are no percentile, variance, or distribution-aware computations anywhere in the codebase:

| Layer | Functions Used | Statistical Methods |
|---|---|---|
| Masu SQL templates (all 3 paths) | `SUM`, `MAX`, `MIN`, `COUNT`, `GROUP BY` | None. No `AVG`, no percentile, no variance. |
| Cost distribution SQL | `SUM(usage) / SUM(total_usage) * overhead_cost` | Proportional allocation — deterministic ratio math |
| API layer (Django ORM) | `Sum`, `Max`, `Count`, `Coalesce` | None |
| Forecast module | `numpy.percentile` (IQR), `statsmodels.OLS` | Only statistical code in the entire backend — runs in Python, not SQL |
| Trino SQL | `SUM`, `MAX`, `GROUP BY` | `approx_percentile` available but **not used** |

The forecast module (`koku/forecast/forecast.py`) is the only component with any statistical computation:
- **Model**: `statsmodels.OLS` (linear regression on daily cost totals)
- **Outlier removal**: `numpy.percentile([75, 25])` with 1.5×IQR fence (Tukey box-plot method)
- **Output**: `LinearForecastResult` with prediction intervals via `wls_prediction_std`

## 2. Candidate Extension: tvondra/tdigest

**Repository**: [github.com/tvondra/tdigest](https://github.com/tvondra/tdigest)

| Property | Value |
|---|---|
| Language | C (PostgreSQL extension) |
| License | PostgreSQL license |
| PostgreSQL versions | 12–17 |
| Latest release | v1.4.3 (December 2024) |
| Stars | ~95 |
| Installation | `pgxn install tdigest` or compile from source |
| Dependency | None (standalone, does NOT require TimescaleDB) |

### Key capabilities

- `tdigest(value, compression)` — aggregate function that builds a t-digest
- `tdigest_percentile(digest, percentile)` — extract percentile from pre-computed digest
- `tdigest_percentile(value, compression, percentile)` — one-shot percentile (replaces `percentile_cont`)
- `tdigest_union(digest1, digest2)` — merge two digests (enables hierarchical rollup)
- `tdigest_add(digest, value)` — incremental update without re-reading all data
- `tdigest_avg(digest, low, high)` / `tdigest_sum(digest, low, high)` — trimmed mean/sum
- `tdigest` data type — storable column (~1 KB per digest at compression=100)

### Accuracy

With compression=100: ~1% error relative to total data range. Error is lower at the tails (p1, p99) and higher near the median — designed for extreme percentile estimation.

## 3. Data Volume Analysis: Would Koku Benefit?

### Koku operates on pre-aggregated daily summary rows

The UI summary tables (`reporting_ocp_cost_summary_p`, `reporting_ocp_cost_summary_by_project_p`, etc.) contain **one row per (day, cluster, namespace, cost_model_rate_type)**. This is the data any percentile query would operate on.

### Row counts for typical on-prem deployments

| Scenario | Clusters | Namespaces | Rate Types | Days (3yr) | Rows per group | Total rows |
|---|---|---|---|---|---|---|
| Small (1 cluster, 20 ns) | 1 | 20 | 7 | 1,095 | 1,095 | ~153K |
| Medium (3 clusters, 50 ns) | 3 | 50 | 7 | 1,095 | 1,095 | ~1.15M |
| Large (10 clusters, 200 ns) | 10 | 200 | 7 | 1,095 | 1,095 | ~15.3M |

### Performance comparison at Koku scale

| Query | Rows sorted | `percentile_cont` | `tdigest_percentile` | Difference |
|---|---|---|---|---|
| Single namespace, 3yr | 1,095 | < 1 ms | < 1 ms | None |
| All namespaces, 3yr (200 ns) | 219K (200 groups × 1,095) | ~10-50 ms | ~5-20 ms | Negligible |
| All namespaces, 3yr (2000 groups) | 2.19M | ~100-300 ms | ~50-150 ms | Minor |
| Worst case: project × node × rate_type | 15.3M | ~500 ms - 1 sec | ~200-400 ms | Moderate |

**Conclusion**: At Koku's on-prem scale, `percentile_cont` (built-in, exact, no extension needed) is performant enough for 3+ years of data. The t-digest extension does NOT provide a meaningful speed improvement because the number of values per group is bounded by the number of days (~1,095 for 3 years).

## 4. Where T-Digest DOES Add Value

The value of the tvondra extension is **not speed** — it's the **`tdigest` data type** and its structural properties: mergeability, incremental updates, and pre-computation.

### 4.1 Pre-computed percentile summaries

```sql
-- Pre-compute weekly cost digests during nightly summarization
CREATE TABLE cost_digests (
    week         DATE NOT NULL,
    cluster_id   TEXT NOT NULL,
    namespace    TEXT NOT NULL,
    cost_digest  tdigest,
    PRIMARY KEY (week, cluster_id, namespace)
);

INSERT INTO cost_digests
SELECT
    date_trunc('week', usage_start)::date AS week,
    cluster_id, namespace,
    tdigest(
        (infrastructure_raw_cost + infrastructure_markup_cost
         + cost_model_cpu_cost + cost_model_memory_cost
         + cost_model_volume_cost + distributed_cost)::double precision,
        100
    ) AS cost_digest
FROM reporting_ocp_cost_summary_by_project_p
GROUP BY 1, 2, 3;
```

Each row stores ~1 KB. The entire 3-year digest table for 200 namespaces: `156 weeks × 200 namespaces × 1 KB ≈ 30 MB`. Compare to the source summary table at ~15M rows × ~200 bytes ≈ 3 GB.

### 4.2 Hierarchical rollup (impossible with `percentile_cont`)

```sql
-- Monthly p95 from weekly digests — no raw data access
SELECT date_trunc('month', week),
       tdigest_percentile(cost_digest, 0.95)
FROM cost_digests
WHERE namespace = 'banking-app'
GROUP BY 1 ORDER BY 1;

-- Quarterly p95 from the same weekly digests
SELECT date_trunc('quarter', week),
       tdigest_percentile(cost_digest, 0.95)
FROM cost_digests
GROUP BY 1 ORDER BY 1;

-- Cross-namespace cluster-wide p95 (merges all namespace digests)
SELECT tdigest_percentile(cost_digest, 0.95)
FROM cost_digests
WHERE week >= now() - interval '3 years';
```

With `percentile_cont`, computing a monthly p95 from weekly pre-aggregates is **structurally impossible** — you cannot derive an exact percentile from sub-period aggregates. You must re-read all daily rows. T-digest digests are mergeable, so you can roll up time and dimensions arbitrarily.

### 4.3 Incremental updates

When new daily data arrives:

```sql
UPDATE cost_digests
SET cost_digest = tdigest_add(cost_digest, new_daily_cost)
WHERE week = date_trunc('week', now()) AND namespace = 'banking-app';
```

No need to re-read all daily rows for the week. This is relevant for the forecast module — instead of re-querying and re-sorting all historical data on every forecast request, maintain a running digest.

### 4.4 Cost anomaly detection (new capability)

```sql
-- Flag today's cost as anomalous if it exceeds the historical p99
SELECT namespace, today_cost,
       tdigest_percentile(cost_digest, 0.99) AS p99_threshold,
       CASE WHEN today_cost > tdigest_percentile(cost_digest, 0.99)
            THEN 'ANOMALY' ELSE 'NORMAL' END AS status
FROM cost_digests
CROSS JOIN (SELECT namespace, SUM(total_cost) AS today_cost
            FROM reporting_ocp_cost_summary_by_project_p
            WHERE usage_start = CURRENT_DATE
            GROUP BY namespace) today
WHERE week >= now() - interval '90 days';
```

This is **fundamentally impossible** with SUM/AVG — you need the distribution tail.

### 4.5 Percentile-based forecasting (replacing current OLS)

The current forecast uses linear OLS on daily cost totals, which fails for cyclical/bursty spend. With t-digest:

```sql
-- "What's the typical (p50) vs worst-case (p95) daily cost?"
SELECT
    tdigest_percentile(cost_digest, 0.50) AS typical_daily_cost,
    tdigest_percentile(cost_digest, 0.95) AS peak_daily_cost,
    tdigest_percentile(cost_digest, 0.50) * days_remaining AS projected_month_p50,
    tdigest_percentile(cost_digest, 0.95) * days_remaining AS projected_month_p95
FROM cost_digests
WHERE week >= now() - interval '90 days'
  AND namespace = 'banking-app';
```

This answers "what's our worst realistic month going to cost?" — which is what finance actually wants, vs the current linear projection ± wide confidence interval.

### 4.6 Variance-weighted cost allocation (policy enhancement)

The current proportional distribution uses daily SUM ratios:

```sql
distributed_cost = SUM(project_usage) / SUM(total_usage) * platform_cost
```

A project that uses 10% of CPU on average but peaks at 80% gets the same allocation as one that's steady at 10%. With t-digest:

```sql
-- Distribution weighted by p95 usage (penalizes bursty workloads)
distributed_cost = tdigest_percentile(project_cpu_digest, 0.95)
                 / SUM(tdigest_percentile(all_project_cpu_digests, 0.95))
                 * platform_cost
```

This is a **fairer** allocation model for shared infrastructure costs. Note: this is a **policy change** with business implications, not just a technical optimization.

## 5. Use Case Priority Matrix

| Use Case | Value | Effort | On-Prem | SaaS | Requires t-digest? |
|---|---|---|---|---|---|
| **Cost anomaly detection** | High | Moderate | Yes | Yes | No (`percentile_cont` works), but t-digest enables pre-computation and O(1) checks |
| **P50/P95 cost reporting** | High | Low | Yes | Yes | No, but t-digest enables dashboard-speed queries from pre-computed digests |
| **Percentile-based forecasting** | High | Moderate | Yes | Yes | No, but t-digest avoids re-reading all historical rows |
| **Multi-year trend analysis** | Medium | Low | Yes | Yes | No at on-prem scale; yes at SaaS scale (1000s of tenants) |
| **Variance-weighted allocation** | Medium | High (policy) | Yes | Yes | Yes (mergeability required for cross-dimension rollup) |
| **Cross-tenant analytics (SaaS)** | High | High | N/A | Yes | **Yes** (`percentile_cont` breaks at 10K+ tenant scale) |

## 6. Recommendation

### For on-prem Koku (1-10 clusters, ≤3 years)

**Start with `percentile_cont`** (built-in, no extension). It handles the data volumes with sub-second latency. Add new capabilities (anomaly detection, p50/p95 reporting, improved forecasting) using standard PostgreSQL without any extension dependency.

**Switch to tvondra/tdigest if and when**:
1. You want pre-computed percentile summaries for dashboard-speed responses (< 5ms)
2. You want incremental digest updates (avoid re-reading history on every forecast)
3. You want hierarchical time rollups (weekly → monthly → quarterly percentiles)

### For SaaS Koku (console.redhat.com, 1000s of tenants)

**The tvondra/tdigest extension is the right tool**. At SaaS scale, cross-tenant analytics and per-tenant anomaly detection require mergeable, pre-computed summaries. `percentile_cont` cannot scale to billions of daily summary rows across thousands of schemas.

However, if TimescaleDB is already deployed (as proposed in the ROS superpowers architecture), its built-in `timescaledb_toolkit` provides an equivalent t-digest implementation. No need for a separate extension.

### Extension choice summary

| Deployment | Recommended Approach |
|---|---|
| On-prem, simple | `percentile_cont` (built-in) |
| On-prem, advanced (anomaly detection, pre-computed dashboards) | tvondra/tdigest (standalone, lightweight) |
| SaaS with TimescaleDB (ROS superpowers) | `timescaledb_toolkit` t-digest (already included) |
| SaaS without TimescaleDB | tvondra/tdigest |

## 7. Implementation Sketch (if pursued)

### Phase 1: Add p50/p95 to cost API (no extension needed)

```sql
-- New columns in reporting_ocp_cost_summary_by_project_p query
SELECT
    namespace,
    SUM(total_cost) AS total_cost,
    percentile_cont(0.50) WITHIN GROUP (ORDER BY daily_cost) AS daily_cost_p50,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY daily_cost) AS daily_cost_p95
FROM (
    SELECT namespace, usage_start,
           SUM(infrastructure_raw_cost + infrastructure_markup_cost
               + cost_model_cpu_cost + cost_model_memory_cost
               + cost_model_volume_cost + distributed_cost) AS daily_cost
    FROM reporting_ocp_cost_summary_by_project_p
    WHERE usage_start >= {{start_date}} AND usage_start <= {{end_date}}
    GROUP BY namespace, usage_start
) daily_totals
GROUP BY namespace;
```

### Phase 2: Pre-computed digests (requires tvondra/tdigest)

```sql
CREATE EXTENSION tdigest;

-- Nightly job: compute/update weekly digests
INSERT INTO cost_digests (week, cluster_id, namespace, cost_digest)
SELECT date_trunc('week', usage_start)::date, cluster_id, namespace,
       tdigest(total_daily_cost::double precision, 100)
FROM daily_cost_totals
WHERE usage_start >= now() - interval '7 days'
GROUP BY 1, 2, 3
ON CONFLICT (week, cluster_id, namespace)
DO UPDATE SET cost_digest = EXCLUDED.cost_digest;
```

### Phase 3: Anomaly detection API endpoint

```sql
-- Compute anomaly flags for all namespaces
SELECT namespace,
       today_cost,
       tdigest_percentile(merged_digest, 0.95) AS p95,
       tdigest_percentile(merged_digest, 0.99) AS p99,
       CASE
           WHEN today_cost > tdigest_percentile(merged_digest, 0.99) THEN 'critical'
           WHEN today_cost > tdigest_percentile(merged_digest, 0.95) THEN 'warning'
           ELSE 'normal'
       END AS anomaly_level
FROM (
    SELECT namespace, tdigest_percentile(cost_digest, 0.95) AS merged_digest
    FROM cost_digests
    WHERE week >= now() - interval '90 days'
    GROUP BY namespace
) historical
JOIN today_costs USING (namespace);
```

### Phase 4: Replace forecast OLS with percentile-based projection

Replace the Python `forecast.py` OLS model with SQL-based percentile forecasting, eliminating the Python→SQL round-trip and the `numpy`/`statsmodels` dependency for this code path.

## 8. Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Extension not available in managed PostgreSQL (RDS, etc.) | Cannot use t-digest on managed DB | Phase 1 uses `percentile_cont` only; t-digest is optional |
| Approximate results confuse users expecting exact values | Trust issues with anomaly detection | Document accuracy bounds; use compression=200 for < 0.5% error |
| Extension maintenance (tvondra is a single maintainer) | Risk of abandonment | Extension is simple C code (~1 file); could be maintained internally if needed. Also available in TimescaleDB toolkit. |
| Policy implications of variance-weighted allocation | Business pushback | Keep as opt-in; default remains current SUM-based proportional allocation |
