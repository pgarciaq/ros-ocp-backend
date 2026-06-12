# Native Engine: Performance Profile and Scale Concerns

## Benchmark Environment

Benchmarks were run on a developer laptop (22 cores, 62 GB RAM, NVMe SSD)
using `cmd/bench/main.go`, which spins up a PostgreSQL 16 container via
testcontainers-go, seeds synthetic data, and measures the full pipeline.

This represents a **worst-case baseline**: Docker-in-Docker PostgreSQL with
cold caches, shared resources, and overlay filesystem I/O.

## Benchmark Results

| Containers | Recommend (ms) | Write (ms) | List p50 (ms) | List p99 (ms) | Detail (ms) | Peak RSS (MB) |
|-----------|----------------|-----------|---------------|---------------|-------------|--------------|
| 10,000    | 857            | 1,617     | 22.6          | 27.7          | 26.7*       | 175.6        |
| 100,000   | 10,485         | 19,171    | 313.2         | 353.8         | 239.5*      | 1,790.1      |

\* Detail latency includes the **fallback scan path only** — see below.

## Detail Endpoint: Not a Concern

The 239ms detail latency at 100K is a **benchmark artifact**, not a production
concern. The benchmark does not populate the `container_id` column in
`recommendation_sets`, so every detail query falls back to the bounded
composite-key scan path (`getNativeRecommendationByIDFallback`).

In production, once `WriteRecommendations` populates `container_id` (Phase 3
task), the detail query becomes:

```sql
WHERE rs.container_id = $1 AND ra.org_id = $2 AND rs.stale = false
```

This is a B-tree index seek — **sub-millisecond on any hardware**, including
low-spec on-prem nodes. The fallback path exists only for pre-migration data
and will become unreachable after one full reprocessing cycle.

## List Endpoint: COUNT DISTINCT Bottleneck

The list endpoint runs two queries per request:

1. **Data query** (fast): Fetches one page of containers with `LIMIT`/`OFFSET`.
   At 10 containers per page (60 rows), this is consistently <5ms.

2. **Count query** (bottleneck): Computes the total number of distinct containers
   for pagination metadata:

   ```sql
   SELECT COUNT(DISTINCT (rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name))
   FROM recommendation_sets rs
   JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
   JOIN rh_accounts ra ON ra.id = c.tenant_id
   WHERE ra.org_id = $1 AND rs.stale = false
   ```

   At 100K containers × 6 rows each (3 terms × 2 engines) = 600K rows,
   PostgreSQL must scan every matching row and hash-aggregate the distinct
   tuples. No index can short-circuit a `COUNT(DISTINCT tuple)`.

### Production Latency Estimates

| Environment | 10K containers | 100K containers (cold) | 100K containers (warm cache) |
|-------------|---------------|----------------------|---------------------------|
| AWS RDS db.m6g.xlarge (4 vCPU/16 GB) | ~15-25ms | ~200-250ms | **~80-120ms** |
| AWS RDS db.m6g.2xlarge (8 vCPU/32 GB) | ~10-15ms | ~120-160ms | **~50-80ms** |
| On-prem (local SSD) | ~20-30ms | ~200-300ms | **~100-150ms** |
| On-prem (Ceph/ODF storage) | ~30-50ms | ~300-500ms | **~150-250ms** |

Warm cache is the steady-state condition: after the first request for an org,
the relevant pages are in PostgreSQL's `shared_buffers` and OS page cache.

### Assessment by Deployment Type

**On-prem (typical 1K-10K containers):** No concern. List latency under 30ms
warm on any reasonable hardware.

**SaaS with <50K containers per org:** Acceptable. Warm-cache latency of
60-80ms is within normal API response time for paginated list endpoints.

**SaaS with 100K+ containers per org:** Marginal. The count query alone takes
80-120ms on RDS warm cache, pushing total API response toward 150-200ms.
Within typical SLA (500ms-1s) but not snappy for interactive use.

## Recommended Optimization Path

### Phase 2 (Current): Instrument

Add timing instrumentation to the count query so production metrics reveal
whether optimization is actually needed:

```go
t0 := time.Now()
countQuery.Scan(&totalContainers)
log.Infof("native list count query: %dms (%d containers)", time.Since(t0).Milliseconds(), totalContainers)
```

### Phase 3 (If Metrics Confirm Need): Redis Count Cache

The container count for an org changes only when new data is ingested (daily
batch, not per-request). Cache the count in Valkey/Redis with a 60-second TTL:

```go
cacheKey := fmt.Sprintf("native-container-count:%s", orgID)
if cached, err := redis.Get(cacheKey); err == nil {
    totalContainers = cached
} else {
    // run COUNT DISTINCT query
    redis.Set(cacheKey, totalContainers, 60*time.Second)
}
```

This turns every list request after the first into a ~5ms operation (data query
only + cache hit). Zero schema changes, trivially reversible.

**Effort:** Low (10-15 lines of code).
**Risk:** Stale count for up to 60 seconds after ingestion — acceptable for
pagination metadata.

### Phase 3 (Alternative): Denormalized Count Column

Add an `active_container_count` column to `rh_accounts` or `clusters`, updated
by `RefreshOrgMetadata` at the end of each reconcile cycle. The list query reads a
single integer instead of scanning 600K rows.

**Effort:** Medium (migration + write-path change).
**Risk:** Count becomes stale if the write path has bugs. More durable than
cache but harder to implement correctly.

### Not Recommended: Approximate Count via EXPLAIN

Parsing `EXPLAIN` output for the planner's row estimate can be off by 10x and
is unreliable for exact pagination metadata that the UI depends on.

## Batch Operations: Streaming Pipeline

`RecommendWorkloadsStreaming` processes digests row-by-row from the database,
exploiting the `ORDER BY namespace, workload, workload_type, container_name, bucket_date`
guarantee to detect container group boundaries. Recommendations are computed and
written in batches of 500 containers (~3000 recs per batch). Adoption detection,
savings estimates, history, and quality metrics all run per-batch.

`RecommendAllWorkloads` is retained as a convenience wrapper for tests (collects
all streaming results into a single slice).

Total wall-clock time scales linearly (10x containers → ~10x time) and is
acceptable for background batch operations triggered after data ingestion.

## Memory Usage

| Scale | Peak RSS (old) | Peak RSS (streaming) | Context |
|-------|---------------|---------------------|---------|
| 10K containers | 176 MB | ~50 MB | Batch: only 500 containers buffered at a time |
| 100K containers | 1,790 MB | ~80 MB | Batch: bounded by streamBatchSize, not cluster size |
| 200K containers | ~3,500 MB | ~80 MB | Streaming keeps memory constant regardless of scale |

Peak memory for the streaming pipeline is bounded by:
- `streamBatchSize` (500) × terms (3) × engines (2) × ~500 bytes = ~1.5 MB per batch
- `ReadClusterOldRecommendations`: ~100 bytes × container count (e.g. 200K = ~20 MB)
- One container's digest rows in flight: ~15 days × ~300 bytes = ~4.5 KB

Pod resource limits:
- **API server pod:** 256-512 MB limit (handles paginated responses)
- **Recommendation worker pod:** 256-512 MB limit (streaming keeps memory bounded)

## Summary of Action Items

| Item | Phase | Priority | Status |
|------|-------|----------|--------|
| Populate `container_id` in `WriteRecommendations` | 3 | High | **COMPLETED** (Phase 3) |
| Add count query timing instrumentation | 2/3 | Medium | **COMPLETED** (Phase 3) |
| Redis count cache (if metrics warrant) | 4+ | Medium | Deferred -- monitor production metrics first |
| Set pod resource limits for batch worker | 4+ | Low | Reduced: streaming keeps memory bounded |
| `WriteRecommendationQuality` batch writes | 4 | Medium | **COMPLETED** -- runs per-batch in streaming pipeline |
| `WriteRecommendationHistory` batch writes | 4 | Medium | **COMPLETED** -- runs per-batch in streaming pipeline |
| OOM bump computation overhead | 4 | Low | Negligible -- single `math.Log2` per container with OOM events |
