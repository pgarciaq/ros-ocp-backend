# librobne Scalability Analysis for Local Mode

!!! warning "Status: Planned / Future Work"
    This document extends the [Local Mode](../../docs/features/local-mode.md)
    planned feature with scalability analysis for the extracted recommendation
    engine library (librobne). Numbers are estimates based on profiling the
    current ros-ocp-backend engine and PostgreSQL write throughput benchmarks.

!!! info "Quick Facts"
    **Target scale:** 200,000 containers on a single OpenShift cluster  
    **Recommended settings:** collection 60s, engine 300s (5 min)  
    **Operator memory:** 512 MiB – 1 GiB  
    **PostgreSQL PVC:** 2–5 GiB  
    **ADR:** [0303-library-extraction-librobne](../../docs/adr/0303-library-extraction-librobne.md)

---

## Context

The robne-operator (Local Mode) embeds the librobne recommendation engine
on-cluster. Unlike ros-ocp-backend, which processes batch CSV uploads every
6–12 hours, the operator queries Prometheus at configurable intervals (15s–5m)
and runs the engine on fresh data. At large cluster scale (200K containers),
both the collection cycle (Prometheus queries + daily summary upserts) and the
engine cycle (librobne computation + recommendation writes) must complete within
their respective intervals.

This analysis quantifies the feasibility boundaries and recommends default
settings.

---

## Collection Cycle: Prometheus Queries + Daily Summary Upserts

Each collection cycle queries Prometheus for current metrics and upserts daily
summary rows into PostgreSQL. At 200K containers, the upsert volume is the
bottleneck — Prometheus vectorized queries (`quantile_over_time`) return all
containers in a single response.

**Upsert rate by collection interval:**

| Collection interval | Upserts/cycle | Upserts/second | PostgreSQL load | Feasibility |
|---------------------|---------------|----------------|-----------------|-------------|
| 15s | 200,000 | ~13,300 | Very heavy | Feasible but tight |
| 60s | 200,000 | ~3,300 | Comfortable | Recommended |
| 120s | 200,000 | ~1,700 | Very comfortable | Conservative |
| 300s | 200,000 | ~670 | Light | Over-conservative |

At 60s intervals, PostgreSQL must sustain ~3,300 upserts/second. With batched
`INSERT ... ON CONFLICT DO UPDATE` (500-row batches, matching
[ADR-0093](../../docs/adr/0093-chunked-pgx-batches-500.md)), this requires
~6.6 batch executions per second — well within single-connection PostgreSQL
throughput on modern hardware.

At 15s intervals, 13,300 upserts/second is achievable with connection pooling
and parallel batch writers, but leaves minimal headroom for query spikes or
garbage collection pauses.

---

## Engine Cycle: librobne Computation + Recommendation Writes

The engine cycle reads daily summary rows, calls librobne per-entity functions,
and writes recommendations to PostgreSQL.

### Compute time

librobne functions are pure CPU-bound computation with no I/O. Profiling the
current ros-ocp-backend engine on representative data:

| Entity | Calls at 200K | CPU time | Notes |
|--------|---------------|----------|-------|
| Container | 200K x 3 terms x 2 engines = 1.2M | ~6–12s | Decay weighting + percentile computation |
| Namespace | ~10K namespaces | <1s | Aggregation over container results |
| Node | ~500 nodes x 3 terms x 2 engines | <1s | Allocatable vs capacity ratios |
| GPU | ~2K GPUs (if present) | <1s | Multi-metric tree classification |
| PVC | ~20K PVCs | <1s | Regression + trend projection |
| Quota | ~10K namespace quotas | <1s | Headroom + risk band computation |
| Cluster Quota | ~100 CRQs | <1s | Namespace rollup |
| VM | ~5K VMs (if present) | ~1–2s | Instance catalog matching |
| Snapshot | ~10K snapshots | <1s | Priority-ordered classification |
| **Total compute** | | **~10–18s** | |

Container recommendations dominate at 200K scale. The 1.2M calls
(200K containers x 3 terms x 2 engines) each involve weighted percentile
computation over 14-day digest windows — the most expensive per-call operation
in the engine.

### Write time

After computation, 1.2M recommendation rows must be upserted to PostgreSQL:

| Batch size | Batches | Time (estimated) | Notes |
|------------|---------|------------------|-------|
| 500 rows | 2,400 | ~10–30s | Depends on index count and disk I/O |
| 1,000 rows | 1,200 | ~8–20s | Larger batches reduce round-trip overhead |

### Total engine cycle time

| Component | Time |
|-----------|------|
| Read daily summaries | ~2–5s |
| librobne computation | ~10–18s |
| Write recommendations | ~10–30s |
| **Total** | **~25–50s** |

---

## Constraint Matrix

The collection interval and engine interval can be configured independently.
The collection cycle feeds daily summary rows; the engine cycle reads them and
produces recommendations. Multiple collection cycles between engine runs refine
today's summary row (incremental aggregation).

| Collection | Engine | Prometheus load | PG write load | Recommendation freshness | Feasible? |
|------------|--------|-----------------|---------------|--------------------------|-----------|
| 15s | 15s | Very heavy | Very heavy | 15s | **No** — engine cannot finish in 15s |
| 15s | 60s | Very heavy | Heavy | 60s | Tight — collection load dominates |
| 60s | 60s | Moderate | Tight | 60s | Tight — engine takes 25–50s, leaving <10s margin |
| 60s | 300s | Moderate | Comfortable | 5 min | **Yes — recommended** |
| 120s | 300s | Light | Very comfortable | 5 min | Yes — conservative |
| 120s | 600s | Light | Light | 10 min | Yes — for resource-constrained clusters |
| 300s | 900s | Very light | Very light | 15 min | Yes — minimal resource overhead |

**Recommended default at 200K containers:** collection 60s, engine 300s (5 min).

This provides:

- 60-second Prometheus query resolution (sufficient for rightsizing trends)
- 5-minute recommendation freshness (adequate for sizing decisions)
- Comfortable PostgreSQL write budget (~25–50s compute + write within 300s window)
- ~20% CPU duty cycle (active for ~1 minute out of every 5)

---

## Resource Requirements at 200K Containers

### Operator pod

| Resource | Estimate | Rationale |
|----------|----------|-----------|
| Memory | 512 MiB – 1 GiB | 200K containers x 14 days x ~70 bytes/digest = ~200 MB digest data in memory during engine run. Plus Go runtime, Prometheus client buffers, and recommendation result buffers. |
| CPU | 200m average, 1–2 cores burst | ~20% duty cycle at 60s/300s settings. Burst during engine computation phase. |

### PostgreSQL

| Resource | Estimate | Rationale |
|----------|----------|-----------|
| PVC size | 2–5 GiB | 1.2M recommendation rows (~400 bytes each) = ~480 MB. Daily summary rows (200K x 14 days x ~100 bytes) = ~280 MB. Plus indexes, WAL, and headroom. |
| Memory | 256–512 MiB | Shared buffers for recommendation and digest table indexes. |
| CPU | 200m average | Dominated by upsert batches during collection and engine cycles. |

### Prometheus impact

At 60-second collection intervals, the operator fires ~30–40 vectorized PromQL
queries per cycle. Each query returns all matching containers in a single
response (instant vector). Prometheus resource impact:

| Metric | Impact |
|--------|--------|
| Query rate | ~0.5–0.7 queries/second |
| Memory | Minimal incremental (queries use existing TSDB blocks) |
| CPU | Low (vectorized queries are efficient in Prometheus) |

The queries use `quantile_over_time()`, `avg_over_time()`, and
`max_over_time()` with `rate(...[5m])` inner windows — all computed server-side
by Prometheus without transmitting raw samples.

---

## What Would Break at 200K

These are the components that require explicit configuration or sizing changes
when scaling to 200K containers:

1. **Operator memory.** Default ~200 MiB is insufficient. The engine loads all
   digest data for the active window into memory during computation. At 200K
   containers with 14-day lookback, this requires 512 MiB–1 GiB. CRD resource
   limits must be increased, or the operator should auto-detect and request
   appropriate limits.

2. **Recommendation table size.** 1.2M rows with proper indexes for API queries
   (keyset pagination, tag filtering, namespace/cluster scoping) generates
   significant index overhead. Without appropriate indexes, list queries degrade
   from sub-100ms to multi-second. The managed PostgreSQL must create the same
   composite indexes used by ros-ocp-backend.

3. **PostgreSQL PVC size.** Default 1 GiB is insufficient for 200K containers.
   Recommendation rows + daily summaries + indexes + WAL requires 2–5 GiB.
   The CRD `spec.database.managed_config.storage_size` must be increased.

4. **Engine cycle interval.** Cannot be set below ~60 seconds at 200K scale
   (engine computation alone takes 25–50s). The 15-second default appropriate
   for small clusters must be overridden. CRD validation should warn when
   `engine.recommendation_cycle` is less than an estimated minimum based on
   container count.

5. **Connection pool sizing.** Default PostgreSQL `max_connections` (100) is
   sufficient for the operator's single-connection batch writer, but if the
   embedded API serves concurrent requests during engine write phases,
   connection contention can stall upserts. Recommend dedicated connections for
   the engine writer path.

---

## Decoupling Collection from Engine

The key architectural insight enabling scalability is that collection and engine
cycles are fully independent:

```
Collection cycle (every 60s):
  1. Query Prometheus (vectorized — all containers in one response)
  2. Upsert daily summary rows (incremental aggregation)

Engine cycle (every 300s):
  1. Read daily summary rows for active window
  2. Call librobne functions (pure CPU computation)
  3. Write recommendation rows
```

**Daily summary rows serve as the buffer** between collection and engine —
no in-memory queue is needed. Prometheus computes `quantile_over_time()`
server-side; the operator persists the results. Between engine runs, multiple
collection cycles refine today's summary row (running p50/p95/p99/min/max/avg
are updated in-place via SQL aggregation).

This decoupling means:

- Collection can run at high frequency (60s) without triggering engine
  computation each time.
- Engine can run at lower frequency (300s) without missing data — all collection
  results are persisted in daily summary rows.
- If the engine falls behind (takes >300s), the next run processes accumulated
  daily summary data without loss. The system degrades gracefully to longer
  recommendation freshness rather than dropping data.
- Pod restarts do not lose statistical history — daily summary rows persist in
  PostgreSQL.

---

## Scaling Beyond 200K

At 500K+ containers, additional measures become necessary:

| Scale | Bottleneck | Mitigation |
|-------|-----------|------------|
| 500K | Engine compute time (~60–120s) | Parallel per-namespace computation; engine interval 600s+ |
| 500K | PG write throughput | Multiple writer goroutines with connection pooling |
| 1M | Prometheus query latency | Thanos federation; per-namespace query sharding |
| 1M | Operator memory (~2–4 GiB) | Streaming computation (process one namespace at a time, flush, continue) |
| 1M | Recommendation table (6M rows) | Table partitioning by namespace hash or date |

These mitigations are not needed at the 200K target scale and are documented
here for future reference only.

---

## Write Optimization: COPY FROM for Large-Scale Deployments

At 200K containers, the engine cycle writes ~1.2M recommendation rows per run.
With standard upsert (`INSERT ... ON CONFLICT DO UPDATE`), this takes ~10–30
seconds depending on index count and disk I/O. PostgreSQL `COPY FROM` with a
staging table pattern can reduce write time significantly.

### Staging table pattern

1. `CREATE TEMP TABLE staging (...)` — matching the `recommendation_sets` schema
2. `COPY FROM` binary into staging (~1–3 seconds for 1.2M rows)
3. `INSERT INTO recommendation_sets SELECT FROM staging ON CONFLICT DO UPDATE`
   (~5–10 seconds)
4. Temp table dropped on commit

Total write time drops from ~10–30 seconds (batched upsert) to ~6–13 seconds
(COPY + upsert from staging).

### Why this fits the operator model

The `COPY FROM` approach is particularly effective for the robne-operator because:

- **Full reconcile per engine cycle.** Each engine run produces a complete set of
  recommendations (not incremental deltas like ros-ocp-backend's per-manifest
  ingestion). The entire result set can be staged and upserted in one pass.
- **Sole writer.** The operator is the only writer to its managed PostgreSQL
  instance — no concurrent producer contention or lock conflicts on the
  `recommendation_sets` table during the COPY phase.
- **Operator-controlled connection pool.** No shared pool pressure from other
  services. The engine writer can hold a connection for the full COPY + upsert
  duration without starving API queries.

### When to use COPY FROM

**Recommended threshold:** consider `COPY FROM` when container count exceeds 50K
(>300K recommendation rows per engine cycle). Below that threshold, the standard
`pgx.Batch` upsert path (500-row batches per
[ADR-0093](../../docs/adr/0093-chunked-pgx-batches-500.md)) completes in under
5 seconds and adds no implementation complexity.

**Not needed for ros-ocp-backend's current ingestion path**, where per-manifest
batch sizes are small (~500 digest rows) and `pgx.Batch` is sufficient.

### robne CLI use case

A future `robne` CLI tool could use `COPY FROM` for both loading digest data
from NISE-generated CSVs and writing recommendation results. When the CLI owns
the full data lifecycle (no concurrent writers, clean database), no conflict
resolution is needed — `COPY FROM` directly into target tables without staging.

---

## Related

- [ADR-0303: Library Extraction of the Native Engine](../../docs/adr/0303-library-extraction-librobne.md)
- [Local Mode feature doc](../../docs/features/local-mode.md)
- [ADR-0287: Operator 14-day Prometheus lookback](../../docs/adr/0287-operator-14-day-prometheus-lookback-integration-boundary.md)
- [ADR-0093: Chunked pgx batches](../../docs/adr/0093-chunked-pgx-batches-500.md)
- [ADR-0295: Integer-first architecture](../../docs/adr/0295-integer-first-architecture.md)
