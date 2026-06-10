# Namespace Boxplots — Performance Analysis

> **Date:** 2026-03-24
> **Status:** Implemented
> **Depends on:** Phase 5 container boxplots (implemented), Phase 6 native namespace
> recommendations (implemented)

---

## Summary

This document analyzes the performance implications of adding boxplot
visualizations for namespace recommendations, mirroring the existing container
boxplot infrastructure. The recommended approach creates a new
`namespace_usage_samples` table storing raw per-interval measurements, with
boxplots computed at query time via PostgreSQL `percentile_cont()` — the same
pattern used for containers.

**Conclusion:** The performance impact is negligible. Namespace cardinality is
10–50× lower than container cardinality, so every dimension (storage, write
amplification, query latency) is proportionally smaller than what the system
already handles for containers.

---

## Current Architecture

### Container boxplots (Phase 5, implemented)

```
CSV (15-min samples)
  │
  ├──▶ upsertUsageSamples → container_usage_samples (raw row per interval)
  │
  └──▶ ComputeContainerDigest → daily_container_digests (1 row per container per day)
```

At query time, `AssembleBoxplots()` runs:

```sql
SELECT <bucket>,
       MIN(cpu_usage_mc), percentile_cont(0.25), percentile_cont(0.5),
       percentile_cont(0.75), MAX(cpu_usage_mc),
       -- same for mem_usage_kib
FROM container_usage_samples
WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
  AND workload = $4 AND container_name = $5
  AND sample_time >= $6 AND sample_time < $7
GROUP BY bucket ORDER BY bucket
```

### Namespace digests (Phase 6, implemented)

```
CSV (15-min samples)
  │
  └──▶ ProcessNamespaceCSVToDigests → daily_namespace_digests (1 row per namespace per day)
```

No raw samples are currently stored for namespaces — the CSV rows are parsed,
grouped by namespace+day, aggregated into daily percentiles, and the raw
intervals are discarded.

---

## Proposed Architecture

### New table: `namespace_usage_samples`

```sql
CREATE TABLE IF NOT EXISTS namespace_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, sample_time)
) PARTITION BY RANGE (sample_time);
```

Compared to `container_usage_samples`:
- **3 fewer PK columns** (no `workload`, `workload_type`, `container_name`)
- **Same 2 metric columns** (`cpu_usage_mc`, `mem_usage_kib`)
- **Same partitioning strategy** (monthly RANGE on `sample_time`)

### Ingestion change

`ProcessNamespaceCSVToDigests` gains a parallel write path:

```
CSV (15-min samples)
  │
  ├──▶ upsertNamespaceUsageSamples → namespace_usage_samples  [NEW]
  │
  └──▶ ComputeNamespaceDigest → daily_namespace_digests (unchanged)
```

### Query-time boxplot assembly

Same pattern as containers — `AssembleNamespaceBoxplots()` queries
`namespace_usage_samples` with `percentile_cont()` grouped by time bucket.

---

## Row Cardinality

The operator queries Prometheus every 15 minutes (4 samples/hour, 96/day).
Each namespace in a cluster produces 1 CSV row per interval.

| Scale factor | Container samples | Namespace samples | Ratio |
|---|---|---|---|
| Per cluster per day | C × 96 (C = containers) | N × 96 (N = namespaces) | N/C ≈ 1/10 to 1/50 |
| Typical cluster (200 containers, 20 namespaces) | 19,200 rows/day | 1,920 rows/day | 10× fewer |
| 90-day retention window | 1,728,000 rows | 172,800 rows | 10× fewer |
| 100 clusters, 90 days | 172.8M rows | 17.3M rows | 10× fewer |

Namespace samples are an order of magnitude smaller than container samples
because there are far fewer namespaces than containers (typically 10–50×),
and the PK has 3 text columns instead of 5.

---

## Storage Footprint

| | Container sample row | Namespace sample row |
|---|---|---|
| PK fields | 5 text + 1 timestamp | 3 text + 1 timestamp |
| Metric fields | 2 bigint (16 bytes) | 2 bigint (16 bytes) |
| Estimated row size (with index overhead) | ~140 bytes | ~90 bytes |

### Per-cluster storage (90 days, 20 namespaces)

- Namespace samples: 172,800 × 90 bytes ≈ **15 MB**
- Container samples (200 containers): 1,728,000 × 140 bytes ≈ **230 MB**

### 100 clusters, 90-day retention

| Table | Storage |
|---|---|
| `container_usage_samples` (existing) | ~23 GB |
| `namespace_usage_samples` (proposed) | ~1.5 GB |
| **Incremental overhead** | **~6%** |

---

## Write Amplification During Ingestion

### Current namespace ingestion path

1. Parse CSV rows → group by namespace+day → compute digests → batch upsert to
   `daily_namespace_digests`

### With samples added

1. Parse CSV rows → **batch upsert to `namespace_usage_samples`** → group by
   namespace+day → compute digests → batch upsert to `daily_namespace_digests`

### Cost per ingestion run

- One additional `pgx.Batch` with N rows of `ON CONFLICT DO UPDATE` statements
- Namespace CSVs are small: ~20 namespaces × 96 intervals = ~1,920 rows/upload
- The batch upsert has only 6 columns (simpler than digests with 29 columns)
- `SendBatch` amortizes network round-trips — all 1,920 statements in one call

**Estimated additional latency: <100ms per upload cycle** (dominated by batch
round-trip, not computation). The container pipeline's `upsertUsageSamples`
handles thousands of rows per batch with no reported bottleneck; the namespace
version handles ~10× fewer rows.

---

## Query-Time Performance (Boxplot Assembly)

The boxplot query uses `percentile_cont()`, an aggregate that sorts data. Cost
is proportional to rows scanned per bucket.

### Container boxplot query (existing baseline)

- Filters by 5 columns: `org_id, cluster_uuid, namespace, workload, container_name`
- Short-term (24h): ~96 rows → 4 buckets of ~24 rows each
- Long-term (15d): ~1,440 rows → 15 buckets of ~96 rows each

### Namespace boxplot query (proposed)

- Filters by 3 columns: `org_id, cluster_uuid, namespace`
- Short-term (24h): ~96 rows → 4 buckets of ~24 rows each
- Long-term (15d): ~1,440 rows → 15 buckets of ~96 rows each

**Same cardinality per query** — the query is always scoped to a single
namespace. The composite PK index gives direct access. PostgreSQL's
`percentile_cont` on 96 rows is sub-millisecond. Even long_term with 1,440
rows is trivially fast.

Partition pruning eliminates irrelevant monthly partitions, so even with
90 days of data, only 1–2 partitions are touched.

**Estimated query time: <5ms** per boxplot request.

---

## Partition Management and Retention

Identical pattern to `container_usage_samples`:

- **Ingestion**: `EnsureNamespaceSamplePartitions()` creates monthly partitions
  as needed (same as `EnsureSamplePartitions`)
- **Retention**: Add `"namespace_usage_samples"` to `retainedTables` in
  `internal/engine/retention.go`
- **Drop**: `DROP TABLE IF EXISTS` is instant — no vacuum, no row-level delete

No new infrastructure needed; the retention sweep already handles partitioned
tables with the `<table>_YYYYMM` naming convention.

---

## Why Daily Digests Cannot Replace Raw Samples

Three reasons prevent deriving boxplots from `daily_namespace_digests`:

### Problem A: Insufficient granularity for short_term

Short-term boxplots use 6-hour time buckets. Daily digests aggregate an entire
day into one row. A daily P50 cannot be split into four 6-hour buckets.

### Problem B: Wrong statistical property

Each daily digest condenses 96 interval observations into percentile summaries
(P50, P60, P95, etc.). A boxplot of daily P50 values across 15 days shows
*variation between days*, not *variation within each day*. The boxplot should
show "how did this namespace's usage fluctuate within this time bucket?" — that
requires raw per-interval data.

### Problem C: Missing percentiles

Boxplots need Q1 (P25) and Q3 (P75). The digests store P50/P60/P95/P98/P99
but not P25 or P75. Even adding them wouldn't fix problems A and B.

---

## Alternative Considered: Pre-Aggregated Sub-Daily Buckets

Instead of raw samples, store pre-computed 6-hour digests (with min, Q1, median,
Q3, max). This would reduce storage ~4× (4 rows/day instead of 96) and
eliminate query-time percentile computation.

**Rejected because:**

- Locks us into a fixed bucket size — if term configurations change to need
  different buckets, we'd have to re-aggregate
- Given that namespace samples are already 10× smaller than container samples,
  the storage savings don't justify the flexibility loss

---

## What Namespace Boxplots Show (vs Container Boxplots)

| | Container boxplot | Namespace boxplot |
|---|---|---|
| **Shows** | How a single container's usage varies over time | How the namespace's aggregate usage varies over time |
| **Captures** | Container-level scaling (vertical) | Namespace-level scaling (horizontal — pods come and go) |
| **Use case** | "Is this container's request right-sized?" | "Is this namespace's total footprint stable or spiky?" |
| **Insight** | Moment-to-moment variability | Aggregate demand variation as pods scale |

Namespace boxplots are arguably *more* informative than container boxplots for
capacity planning, because they show how the total demand for a namespace
fluctuates — which is what cluster admins care about when setting quotas.

---

## Risk Assessment

| Risk | Impact | Mitigation |
|---|---|---|
| Storage growth | ~1.5 GB per 100 clusters (6% of container samples) | Already mitigated by existing retention sweep; configurable via `ROS_RETENTION_MONTHS` |
| Ingestion latency increase | <100ms per upload cycle | Batch upsert amortizes cost; negligible vs CSV download time |
| Query latency | <5ms per boxplot | PK index + partition pruning; same pattern proven for containers |
| Concurrent DROP during queries | Unlikely (retention drops >6-month-old partitions; API never queries that far back) | Same risk exists for container samples; already mitigated |
| New partition creation at ingestion | Could fail with insufficient permissions | Same risk as `EnsureNamespaceDigestPartitions`; non-fatal (logged as warning) |

---

## Implementation

All components listed below have been implemented and tested.

| Component | Files |
|---|---|
| Migration (`000033_create_namespace_usage_samples`) | `migrations/000033_create_namespace_usage_samples.{up,down}.sql` |
| `EnsureNamespaceSamplePartitions()` | `internal/ingestion/namespace.go` |
| `upsertNamespaceUsageSamples()` | `internal/ingestion/namespace.go` |
| `AssembleNamespaceBoxplots()` + `NamespaceMonitoringEndTime()` | `internal/model/boxplot.go` |
| API wiring (`enrichNativeNamespaceDetail`) | `internal/api/handlers.go` |
| Retention (added to `retainedTables`) | `internal/engine/retention.go` |
| `TermRecommendation.Plots` field | `internal/model/recommendation_set_native.go` |
| Go unit/integration tests (16 new) | `internal/model/boxplot_test.go`, `internal/ingestion/namespace_test.go`, `internal/engine/retention_test.go` |
| IQE E2E tests (6 new) | `iqe_ros_ocp/tests/rest/test_namespace_boxplots.py` |

See [phase6-namespace-boxplots-implementation.md](phase6-namespace-boxplots-implementation.md) for details.
