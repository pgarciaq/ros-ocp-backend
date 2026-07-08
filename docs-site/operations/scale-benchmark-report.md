# Scale Benchmark Report: Native Engine (4K Containers)

> **Last updated:** 2026-07-09  
> **Environment:** Single-Node OpenShift (SNO), x86-64, Dell PowerEdge R640 (dell-r640-082)  
> **Engine:** ROS-OCP native engine (Go)

This report documents scale stress tests of the ROS-OCP **native engine** processing bulk uploads of synthetic data for 4,000 containers. Two benchmarks are presented: a **baseline** run before optimizations, and a **post-optimization** run after applying fixes from issues #255–#258, #263.

!!! warning "Extreme bulk-load scenario"
    These benchmarks represent worst-case scenarios: weeks of data uploaded in large tarballs. Normal operator uploads are hourly with ~100-500 containers, processing in seconds. The numbers below stress-test what happens during bulk re-ingestion, disaster recovery, or data migration.

---

## Test Environment

| Component | Detail |
|-----------|--------|
| Platform | Single-Node OpenShift (SNO) |
| Architecture | x86-64 (amd64) |
| Hardware | Dell PowerEdge R640 (8 cores, 78 GB RAM) |
| Cluster ID | dell-r640-082 |
| OpenShift version | 4.21 |
| Database | PostgreSQL 16 |
| Message bus | Kafka (Strimzi) |
| Object storage | MinIO (S3-compatible), 10 GiB PVC |

---

## Results: Before vs. After Optimization

| Metric | Baseline (July 8) | Optimized (July 9) | Improvement |
|--------|-------------------|---------------------|-------------|
| Data duration | 15 days | 30 days (June) | 2× more data |
| Containers | ~7,000 (NISE bug inflated count) | 4,000 (NISE bug fixed) | Accurate count |
| Total CSV rows | ~6.7 million | ~3 million (30 × 100K) | — |
| Tarballs | 1 × 1,013 MiB | 1 × 831 MiB | — |
| Processing mode | Single-threaded (deadlock forced) | Multi-threaded (default) | Deadlock fixed |
| Digests created | 114,890 | 124,000 | — |
| **Total processing time** | **12,818s (3.5 hours)** | **~450s (7.5 minutes)** | **~28× faster** |
| Throughput | 523 rows/s | ~6,700 rows/s | ~13× |
| Container recommendations | Failed (statement_timeout) | 44,436 ✅ | Fixed |
| Node recommendations | — | 150 | — |
| Namespace recommendations | — | 3,000 | — |
| Quota recommendations | — | 500 | — |
| VM recommendations | — | 24 | — |

!!! success "28× faster with all optimizations applied"
    The post-optimization run processed **2× more data** (30 days vs. 15) in **7.5 minutes** vs. the baseline's **3.5 hours**. On an equivalent data volume, the improvement would be approximately **56×**.

---

## Post-Optimization Benchmark (July 9, 2026)

### Configuration

| Parameter | Value |
|-----------|-------|
| ROS processor | Single replica, multi-threaded (default workers) |
| `ROS_KAFKA_WORKERS` | 3 (default) |
| `ROS_MANIFEST_DOWNLOAD_WORKERS` | 3 (default) |
| `ROS_INGEST_FLUSH_BATCH_SIZE` | 5,000 (increased from 1,000 in [#256](https://github.com/pgarciaq/ros-ocp-backend/issues/256)) |
| `maxPgxBatchQueue` | 2,000 (increased from 500 in [#257](https://github.com/pgarciaq/ros-ocp-backend/issues/257)) |
| `container_usage_samples` writes | **Removed** ([#258](https://github.com/pgarciaq/ros-ocp-backend/issues/258)) |
| Deadlock prevention | Deterministic key sorting ([#255](https://github.com/pgarciaq/ros-ocp-backend/issues/255)) |
| Recommendation query | Buffered reads + covering index ([#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)) |

### Workload

| Parameter | Value |
|-----------|-------|
| Data generator | NISE (koku-nise), with pod-spreading bug fix ([#254](https://github.com/pgarciaq/ros-ocp-backend/issues/254)) |
| Target containers | 4,000 |
| Actual distinct containers | 4,000 (NISE bug fixed — accurate count) |
| Duration | 30 days (June 2026) |
| Tarball size | 831 MiB |
| Cluster UUID | `3129c7a8-44be-4eba-b6ce-3fd5ba13f9ee` |

### Timeline

| Time (UTC) | Event |
|------------|-------|
| 21:39:36 | Listener received Kafka message for June tarball (831 MiB) |
| 21:40:17 | Manifest identified, ROS CSVs uploaded to S3 |
| 21:43:18 | First CSV file processed by ROS processor |
| 21:49:30 | Last CSV file processed (manifest complete) |
| 21:50:19 | `loadDigestRows`: buffered 124K digest rows in memory |
| 21:50:41 | Container recommendations: wrote 44,436 recommendation sets |
| 21:50:42 | Node recommendations: upserted 150 |
| 21:50:43 | Quota recommendations: wrote 500 |
| 21:50:43 | VM recommendations: upserted 24 |
| 21:50:45 | Namespace recommendations: wrote 3,000 |
| 21:50:47 | All recommendation engines complete |

### Per-CSV performance

Each CSV file contains ~100,000 rows (one day of data for 4,000 containers):

| Metric | Value |
|--------|-------|
| Parse time (CSV → digest groups) | ~550 ms |
| Upsert time (digest groups → DB) | ~1,500 ms |
| Total per CSV | ~2,100 ms |
| Incremental flush threshold | 5,000 digest groups |
| CSVs requiring incremental flush | ~1 in 3 (files with 2× density) |
| Time for CSVs with incremental flush | ~3,800–4,400 ms |

### Recommendation phase

| Step | Time | Details |
|------|------|---------|
| Load all digest rows | ~1s | 124,000 rows buffered via `loadDigestRows()` with ingest timeout (120s) |
| Compute container recommendations | ~20s | 4,000 containers × 2 terms × ~5 metrics |
| Write container recommendations | ~1s | 44,436 rows via `pgx.Batch` |
| Node + namespace + quota + VM | ~6s | 150 + 3,000 + 500 + 24 recommendations |
| **Total recommendation phase** | **~28s** | — |

!!! note "No more statement_timeout"
    The previous benchmark failed during the container recommendation phase with a `statement_timeout` (SQLSTATE 57014). This was caused by the streaming query holding the DB connection open for the entire processing duration, exceeding the 25-second API timeout that was applied to all connections.

    [Issue #263](https://github.com/pgarciaq/ros-ocp-backend/issues/263) fixed this with three changes:

    1. **Buffered reads**: `loadDigestRows()` fetches all rows into memory and commits the transaction, releasing the DB connection before processing begins
    2. **Ingest timeout**: The digest read uses `ROS_DB_INGEST_STATEMENT_TIMEOUT` (120s) instead of the API default (25s)
    3. **Covering index**: `idx_daily_container_digests_recommend` eliminates the external merge sort

---

## Baseline Benchmark (July 8, 2026)

This was the first scale benchmark, run **before** any optimizations were applied.

### Configuration

| Parameter | Value |
|-----------|-------|
| ROS processor | Single replica, **single-threaded** |
| `ROS_KAFKA_WORKERS` | 1 (forced to avoid deadlock) |
| `ROS_MANIFEST_DOWNLOAD_WORKERS` | 1 (forced to avoid deadlock) |
| `ROS_INGEST_FLUSH_BATCH_SIZE` | 1,000 (default at the time) |
| `maxPgxBatchQueue` | 500 (default at the time) |
| `container_usage_samples` writes | **Active** (60-70% of parse_digest time) |

### Workload

| Parameter | Value |
|-----------|-------|
| Profile name | `scale-medium-15d` |
| Target containers | 4,000 |
| Actual distinct containers | ~7,000 (NISE pod-spreading bug inflated count) |
| Duration | 15 days of historical data |
| Tarball size | 1,013 MiB (1 GiB) |
| CSV files in tarball | 76 files |
| Total CSV rows | ~6.7 million |

### Results

| Metric | Value |
|--------|-------|
| Upload time | 12 seconds |
| Total processing time | 12,818 seconds (~213 min / 3.5 hours) |
| Digests created | 114,890 rows in `daily_container_digests` |
| Distinct containers | ~7,000 |
| Processing throughput | ~523 CSV rows/second (single-threaded) |

### Pipeline Phase Breakdown

| Phase | Time (min) | % of Total | Description |
|-------|-----------|------------|-------------|
| `parse_digest` | ~152 | 71.5% | CSV parsing, digest computation, **sample writes** |
| `write_digests` | ~60 | 28.1% | Final digest upserts to PostgreSQL |
| `recommend` | ~0.4 | 0.2% | Recommendation engine execution |
| `write_recommendations` | ~0.2 | 0.1% | Persist recommendation sets |

```mermaid
pie title Baseline Pipeline Phase Time Distribution
    "parse_digest (71.5%)" : 71.5
    "write_digests (28.1%)" : 28.1
    "recommend (0.2%)" : 0.2
    "write_recommendations (0.1%)" : 0.1
```

The dominant bottleneck was `container_usage_samples` writes during `parse_digest`, accounting for an estimated 60-70% of the phase. These raw 15-minute metric samples were **not used by the recommendation engine** and have since been removed ([#258](https://github.com/pgarciaq/ros-ocp-backend/issues/258)).

---

## Optimizations Applied

All optimizations identified during the baseline benchmark have been implemented and verified:

| # | Optimization | Issue | Impact |
|---|---|---|---|
| 1 | **Fix deadlock, enable multi-threaded processing** — Sort digest keys before upsert for consistent lock ordering; add retry on `40P01` | [#255](https://github.com/pgarciaq/ros-ocp-backend/issues/255) ✅ | Enabled `ROS_KAFKA_WORKERS=3` |
| 2 | **Increase digest flush batch size** (1,000 → 5,000), `csv.Reader.ReuseRecord`, pre-create partitions | [#256](https://github.com/pgarciaq/ros-ocp-backend/issues/256) ✅ | ~30% fewer DB round-trips during parse |
| 3 | **Increase `maxPgxBatchQueue`** (500 → 2,000) | [#257](https://github.com/pgarciaq/ros-ocp-backend/issues/257) ✅ | ~4× fewer DB round-trips for digest upserts |
| 4 | **Remove `container_usage_samples` and `namespace_usage_samples` writes** — Tables dropped, all related code removed | [#258](https://github.com/pgarciaq/ros-ocp-backend/issues/258) ✅ | Eliminated 60-70% of parse_digest time |
| 5 | **Fix recommendation `statement_timeout`** — Buffer digest reads, use ingest timeout, add covering index | [#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263) ✅ | Recommendations no longer fail at scale |
| 6 | **Fix NISE pod-spreading bug** — `_gen_namespaces` no longer drops pods when same namespace appears on multiple nodes | [#254](https://github.com/pgarciaq/ros-ocp-backend/issues/254) ✅ | Accurate container counts for benchmarks |

---

## Architecture: Data Flow for One CSV File

The following diagram traces the post-optimization processing path for a single CSV file:

```mermaid
flowchart TD
    KM["Kafka message received<br/>(contains S3 URL for CSV)"] --> DL["Download CSV from S3/MinIO<br/>(buffered to temp file on disk)"]
    DL --> Parse["parseAndDigestCSVStream:<br/>read row by row"]
    Parse --> Row["Parse row → MetricRow struct<br/>(string → int64 conversions)"]
    Row --> GA["Append to groupedAll map<br/>(keyed by container+day+schedule)"]
    GA --> DF{Every 5,000 groups?}
    DF -->|Yes| Compute["ComputeContainerDigestWeighted:<br/>sort each metric field,<br/>extract p50/p60/p95/p98/p99/max/mean"]
    DF -->|No| Parse
    Compute --> UDigest["commitIngestInSingleTx:<br/>upsert digests + node digests<br/>in one transaction"]
    UDigest --> Parse
    Parse --> EOF{End of file?}
    EOF -->|Yes| FlushRem["Flush remaining<br/>digest groups"]
    FlushRem --> ManifestCheck{"All manifest<br/>files complete?"}
    ManifestCheck -->|No| Done["Done — wait for<br/>remaining files"]
    ManifestCheck -->|Yes| LoadDigests["loadDigestRows:<br/>buffer all digests in memory<br/>(ingest timeout, 120s)"]
    LoadDigests --> Recommend["Run recommendation engines<br/>(container, node, namespace,<br/>quota, VM, cluster-quota)"]
    Recommend --> WriteRec["Write recommendation_sets"]

    style KM fill:#e74c3c,color:#fff
    style UDigest fill:#3498db,color:#fff
    style Compute fill:#9b59b6,color:#fff
    style LoadDigests fill:#e67e22,color:#fff
    style Recommend fill:#2ecc71,color:#fff
```

### Key differences from baseline

1. **No more `container_usage_samples` writes** — The raw 15-minute sample tables have been removed entirely. Only digest aggregates are written.

2. **Larger batch flushes** — Digest groups are flushed every 5,000 groups (up from 1,000), reducing DB round-trips by ~80%.

3. **Single-transaction commit** — Digests and node digests are committed in a single transaction via `commitIngestInSingleTx`, ensuring atomicity.

4. **Buffered recommendation reads** — `loadDigestRows()` fetches all digest rows into memory and releases the DB connection before the recommendation engine processes them, preventing timeout issues.

5. **Streaming architecture preserved** — The processor still never holds the entire CSV in memory. Rows are processed one at a time with periodic batch flushes.

---

## Why Recommendations Wait for All CSVs

A common question: why not produce recommendations as each CSV is ingested?

The answer is architectural, codified in [ADR-0166: Report File Status Manifest Gating](https://github.com/pgarciaq/ros-ocp-backend/blob/main/docs/adr/0166-report-file-status-manifest-gating.md):

1. **Recommendations require the full picture.** A recommendation for container X depends on X's complete usage history across all days in the lookback window. If CSV-12 contains June 25 data and CSV-13 contains June 26 data, running recommendations after CSV-12 would produce results based on incomplete data — potentially recommending too-small resources because June 26's peak wasn't seen yet.

2. **One run, not N runs.** With 30+ CSV files, running recommendations after each would execute the engine 30+ times. Since the engine queries the entire cluster's digest history each time, this would be 30× slower than running once after all data is ingested.

3. **Recommendations are not the bottleneck.** At ~28 seconds for 4,000 containers, the recommendation phase is a small fraction of the pipeline. The optimization opportunity was always in ingestion, which these benchmarks confirmed.

```mermaid
flowchart LR
    subgraph "30 CSV files"
        CSV1["CSV-1"] --> Ingest1["Ingest"]
        CSV2["CSV-2"] --> Ingest2["Ingest"]
        CSVN["CSV-30"] --> IngestN["Ingest"]
    end
    Ingest1 --> DB[(daily_container_digests)]
    Ingest2 --> DB
    IngestN --> DB
    DB --> |"All files complete"| Load["loadDigestRows<br/>(buffer in memory)"]
    Load --> Recommend["Recommend (once)"]
    Recommend --> Results["recommendation_sets"]

    style Recommend fill:#2ecc71,color:#fff
    style DB fill:#3498db,color:#fff
    style Load fill:#e67e22,color:#fff
```

---

## Issues Encountered and Resolved

### 1. Context deadline exceeded (HTTP timeout)

**Symptom:** ROS processor logged `context deadline exceeded` while parsing large CSVs.

**Root cause:** The HTTP client timeout (`ROS_CSV_DOWNLOAD_TIMEOUT_SECS`, default 120s) applied to the entire duration of reading the response body — including CSV parsing and database upserts that took much longer than 120 seconds for 525 MiB files.

**Fix:** Modified `ReadCSVBodyFromUrl` to buffer the entire HTTP response to a temporary file on disk before returning an `io.ReadCloser`. This decouples the HTTP download timeout from processing time.

### 2. PostgreSQL deadlock (SQLSTATE 40P01)

**Symptom:** With `ROS_KAFKA_WORKERS=3`, the processor logged deadlock errors and processing stalled.

**Root cause:** Multiple Kafka workers processed CSVs for the same cluster concurrently. Each worker called `upsertContainerDigests()`, which iterates over a Go map (random key order) and executes `INSERT ON CONFLICT` statements. Two transactions locking the same rows in different orders caused a classic deadlock.

**Fix ([#255](https://github.com/pgarciaq/ros-ocp-backend/issues/255)):** Sort digest keys deterministically before upsert to guarantee consistent lock ordering across transactions. Added retry-on-deadlock logic as a safety net. Multi-threaded processing now works reliably.

### 3. S3 storage full

**Symptom:** MinIO returned HTTP 500 with "minimum free drive threshold" error.

**Root cause:** The 10 GiB MinIO PVC filled up from accumulated tarballs and extracted CSV data.

**Fix:** Cleaned up old data from MinIO buckets before each benchmark run.

### 4. Statement timeout during container recommendations (SQLSTATE 57014)

**Symptom:** With 4,000 containers and 15+ days of data, the container recommendation phase failed with `canceling statement due to statement timeout`.

**Root cause:** Three factors combined:

- The `RecommendWorkloadsStreaming` function used a streaming cursor that held the DB connection open while processing recommendations row by row
- All DB connections inherited the API session-level `statement_timeout` of 25 seconds
- With 124K digest rows, the ORDER BY required an external merge sort (14-19 MiB spill to disk), and the slow client-side processing caused TCP backpressure that stalled the server-side query

**Fix ([#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)):** Three-part solution:

1. **Buffered reads:** New `loadDigestRows()` function fetches all rows into a `[]digestRowWithKey` slice, commits the transaction, and releases the DB connection before processing begins
2. **Ingest timeout:** The digest read transaction uses `SetLocalIngestStatementTimeout` (120s) instead of the API default (25s)
3. **Covering index:** Migration 000173 creates `idx_daily_container_digests_recommend` on `(org_id, cluster_uuid, schedule_type, namespace, workload, workload_type, container_name, bucket_date)`, satisfying both the WHERE clause and ORDER BY from a single index scan — eliminating the external merge sort

### 5. NISE pod-spreading bug

**Symptom:** Requesting 4,000 containers from NISE produced ~7,000 distinct containers in the generated data.

**Root cause:** NISE's `_gen_namespaces()` function rebuilt the namespace list from scratch for each node, dropping pods when the same namespace appeared on multiple nodes.

**Fix ([#254](https://github.com/pgarciaq/ros-ocp-backend/issues/254)):** Fixed the namespace generator to use a single shared pod index across all nodes, ensuring accurate container counts.

---

## Comparison to Production Patterns

| Scenario | Containers | Data Duration | Expected Time |
|---|---|---|---|
| Normal hourly upload | 100-500 | 1 hour | < 5 seconds |
| Daily catch-up | 500-2,000 | 24 hours | 1-5 minutes |
| Bulk re-ingestion (post-optimization) | 4,000 | 30 days | ~7.5 minutes |
| Bulk re-ingestion (baseline, pre-optimization) | 7,000 | 15 days | 3.5 hours |

---

## Comparison with Kruize 0.11

The native engine was built to replace [Kruize](https://github.com/kruize/autotune) (Java) as the recommendation engine for Red Hat Cost Management. Kruize 0.11 published [scalability test results](https://github.com/kruize/autotune/blob/master/tests/test_plans/test_plan_rel_0.11.md) that provide a direct comparison point.

### Performance comparison

Kruize's closest comparable benchmark is their "short scalability run" on OpenShift: 5K container experiments with 15 days of usage data, using 10 Kruize replicas.

| Metric | Kruize 0.11 (v1 API) | Native Engine (post-opt) | Ratio |
|---|---|---|---|
| **Containers** | 5,000 | 4,000 | ~comparable |
| **Data duration** | 15 days | 30 days | Native has **2× more data** |
| **Result entries** | 7.2M (72 Lakhs) | 124K digests (from ~3M CSV rows) | — |
| **Processing time** | **3h 17m** | **7.5 minutes** | **26× faster** |
| **Replicas** | **10** | **1** | 10× fewer |
| **Max CPU** | 11.72 cores | ~1–2 cores (single pod) | ~8× less |
| **Max Memory** | 43.52 GB | < 1 GB | ~50× less |
| **DB size** | 22,012 MB (~22 GB) | ~250 MB | ~88× smaller |
| **DB resources** | 10 GiB req / 30 GiB limit, 2 cores | Default pod resources | — |
| **Engine resources** | 4 GiB req / 8 GiB limit × 10 replicas | Single pod, default limits | — |
| **Infrastructure** | Multi-node OCP cluster | Single-node OCP (Dell R640) | — |

Normalizing for data volume (30 days vs. 15 days), the native engine is approximately **52× faster**. Normalizing for replicas (10 vs. 1), per-replica efficiency is approximately **260× better**.

!!! note "GPU containers"
    Kruize's GPU container benchmark (5K GPU containers, 15 days) took **5h 50m** with v0.11. The native engine handles GPU containers as part of the regular pipeline — they are additional metrics in the same CSV parsing and digest computation, with no separate processing mode or additional overhead.

### Feature coverage comparison

The native engine provides significantly broader recommendation coverage:

| Feature | Kruize 0.11 | Native Engine |
|---|:---:|:---:|
| **Container CPU/memory recommendations** | ✅ | ✅ |
| **Namespace recommendations** | ✅ | ✅ |
| **GPU container recommendations** | ✅ (5h 50m for 5K) | ✅ (no additional overhead) |
| **Node recommendations** | ❌ | ✅ |
| **Cluster quota recommendations** | ❌ | ✅ |
| **VM (OpenShift Virtualization) recommendations** | ❌ | ✅ |
| **PVC rightsizing** | ❌ | ✅ |
| **GPU MIG slice recommendations** | ❌ | ✅ |
| **GPU time-slicing recommendations** | ❌ | ✅ |
| **GPU idle detection** | ❌ | ✅ |
| **Container/namespace/node idle detection** | ❌ | ✅ |
| **Business hours scheduling** | ❌ | ✅ |
| **Data decay (weighted digests)** | ❌ | ✅ |
| **Estimated savings ($ integration with cost models)** | ❌ | ✅ |
| **Snapshot staleness detection** | ❌ | ✅ |
| **Multiple terms (short/medium/long)** | ✅ | ✅ |
| **Multiple engines (cost/performance)** | ❌ | ✅ |
| **JVM/Java recommendations** | ✅ | ❌ (planned) |

Kruize's only unique feature is JVM/Java recommendations (heap sizing, GC tuning). The native engine covers 15+ recommendation types that Kruize does not support.

### What drives the performance gap

1. **Language and runtime**: Go (compiled, minimal GC pressure) vs. Java (JVM — Kruize peaked at 43.52 GB memory across 10 replicas, indicating significant GC overhead)

2. **Data model**: The native engine computes and stores **daily digests** (percentile summaries) during ingestion. Kruize stores 7.2M individual result entries, then computes recommendations from those raw results — a fundamentally more expensive approach

3. **Database efficiency**: 22 GB (Kruize) vs. ~250 MB (native). The digest-based model is orders of magnitude more compact because percentiles are computed in-process during CSV parsing, not stored as raw samples

4. **Processing model**: The native engine parses CSVs in a streaming fashion from Kafka/S3 and computes digests in-process. Kruize receives data via REST API calls (HTTP overhead per result entry), stores raw results in PostgreSQL, then runs a separate recommendation step

5. **Single-pod architecture**: Kruize requires 10 replicas with 4–8 GiB each, introducing coordination overhead and connection pool pressure. The native engine's single-pod design with deterministic key sorting avoids all inter-replica serialization

---

## Future Optimization Opportunities

The following optimizations have been identified but not yet implemented:

| Optimization | Target | Expected Impact | Issue |
|---|---|---|---|
| NISE: eliminate `deepcopy()` in hot loop | Data generation | ~2× faster data gen | [#259](https://github.com/pgarciaq/ros-ocp-backend/issues/259) |
| NISE: cache date parsing | Data generation | ~10-15% faster | [#260](https://github.com/pgarciaq/ros-ocp-backend/issues/260) |
| NISE: eliminate redundant aggregation | Data generation | ~20-30% faster | [#261](https://github.com/pgarciaq/ros-ocp-backend/issues/261) |
| NISE: use `list.append()` instead of `+=` | Data generation | Marginal | [#262](https://github.com/pgarciaq/ros-ocp-backend/issues/262) |

---

## Appendix: Prometheus Queries

The following queries were used to collect benchmark metrics:

```promql
# Pipeline phase breakdown
rosocp_pipeline_phase_duration_seconds_sum

# Digest counts
rosocp_db_query_duration_seconds_count{operation="upsert_container_digests"}

# Recommendation timing
rosocp_recommendation_duration_seconds_sum
```
