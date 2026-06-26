# ROS OCP Metrics Pipeline — Architecture & Performance Analysis

> **⚠️ Staleness notice (2026-05-21):** This document was written during the design/early implementation phase. Some hotspots and issues referenced here have been resolved in subsequent development (P0/P1/P2 audit batches, streaming pipeline, GPU filter push-to-SQL, etc.). For current performance characteristics, see [`native-engine-performance.md`](../native-engine-performance.md). For the full issue resolution status, see [`490-issues.md`](490-issues.md).

> **Date:** 2026-03-26 (updated: 2026-04-16)
> **Last triage:** 2026-03-26 — all repos switched to `main` (autotune: `mvp_demo`) and triaged. See "Platform Update (March 2026 Triage)" notes in §17, §19, §20, §30 for details.
> **Phase 6 audit (2026-04-16):** Two correctness gaps closed. (1) Container memory P60/P98/P99 percentiles were computed by `ComputeContainerDigest()` but discarded before persistence — migration 000035 adds 6 columns to `daily_container_digests`, restoring parity with `daily_namespace_digests`. (2) Namespace memory trend slope was computed but not surfaced — `EvaluateNamespaceNotifications()` now emits `NotifMemoryTrendingUp` when `MemTrendSlope > 500 KiB/day` (5× the container threshold). Idle detection remains intentionally excluded for namespaces.
> **Context:** Analysis of the current ROS OCP metrics pipeline, alternative architectures, and composable optimizations — evaluated for performance, scalability, and operational complexity.
> **Architecture (v4.0):** Recommendation computation is **native Go** in ros-ocp-backend (`recommendCPU()`, `recommendMemory()`, `recommendAllWorkloads()`, `detectIdle()`, `recommendPVC()`, `recommendVM()`, `recommendNodes()`, etc.) using a **read once, compute N terms** pattern. **PostgreSQL 16+** (plain PostgreSQL, not TimescaleDB) stores interval data, **daily digest** partitioned tables, and recommendation results; SQL is for **migrations and storage/retrieval only** — **no PL/pgSQL** recommendation functions. Percentiles are **exact** via `slices.Sort()` in Go (**t-digest is not used**). Batch persistence uses **`COPY FROM`** where applicable.
> **Related:** COST-5691 (Custom Timeframes), koku-metrics-operator, ros-ocp-backend, Kruize (autotune)

**Execution path note:** The **native Go engine is unconditionally active** in current deployments. Sections below that analyze **Kruize** describe the **legacy path** and apply when `ROS_ENABLED_PLUGINS=kruize` is explicitly set. For how legacy Kruize fits alongside the plugin registry, see [`plugin-architecture.md`](plugin-architecture.md).

---

## Table of Contents 

- [1. Executive Summary](#1-executive-summary)
- [2. Current Architecture](#2-current-architecture)
- [3. Alternative A: CSV → Thanos](#3-alternative-a-csv--thanos)
- [4. Alternative B: Kruize on Cluster (Legacy)](#4-alternative-b-kruize-on-cluster-legacy)
- [5. Performance Comparison](#5-performance-comparison)
- [6. Scalability at 20M Containers/Day](#6-scalability-at-20m-containersday)
- [7. Storage Comparison](#7-storage-comparison)
- [8. Incremental Optimization: Typed Columns (legacy Kruize remote metrics path)](#8-incremental-optimization-typed-columns-legacy-kruize-remote-metrics-path--remote-only)
- [9. Optimization: Integer Types (Millicores/KiB)](#9-optimization-integer-types-millicorekib)
- [10. Optimization: Approximate Percentiles](#10-optimization-approximate-percentiles)
- [11. Combined Scenario: Thanos + Integer Types + Approximate Percentiles](#11-combined-scenario-thanos--integer-types--approximate-percentiles)
- [12. Optimization: CPU Recommendation Algorithm](#12-optimization-cpu-recommendation-algorithm)
- [13. Optimization: Kruize Code-Level Improvements (Legacy)](#13-optimization-kruize-code-level-improvements-legacy)
- [14. Optimization: Kruize Database and API Layer (Legacy)](#14-optimization-kruize-database-and-api-layer-legacy)
- [15. Optimization: ros-ocp-backend](#15-optimization-ros-ocp-backend)
- [16. Optimization: Memory Recommendation Algorithm](#16-optimization-memory-recommendation-algorithm)
- [17. Analysis: GPU Recommendation Algorithm](#17-analysis-gpu-recommendation-algorithm)
- [18. Analysis: JVM/Quarkus Recommendation Algorithm](#18-analysis-jvmquarkus-recommendation-algorithm)
- [19. Additional Kruize Optimizations (Deep Audit) (Legacy)](#19-additional-kruize-optimizations-deep-audit-legacy)
- [20. Additional ros-ocp-backend Optimizations (Deep Audit)](#20-additional-ros-ocp-backend-optimizations-deep-audit)
- [21. Rejected Alternative: TSDB Block Shipping](#21-rejected-alternative-tsdb-block-shipping)
- [22. Findings and Trade-offs](#22-findings-and-trade-offs)
- [23. Additional Recommendation Types (Industry Gap Analysis)](#23-additional-recommendation-types-industry-gap-analysis)
- [24. Strategic Recommendation: Drop Kruize from the Remote Path (Legacy analysis)](#24-strategic-recommendation-drop-kruize-from-the-remote-path-legacy-analysis)
- [25. Performance Model: Current vs "ros-ocp-backend with Superpowers"](#25-performance-model-current-vs-ros-ocp-backend-with-superpowers)
- [26. Replica Count for Total Impact Calculation](#26-replica-count-for-total-impact-calculation)
- [27. JSONB Analysis: Why It Exists, Why It Hurts, and Alternatives](#27-jsonb-analysis-why-it-exists-why-it-hurts-and-alternatives)
- [28. Alternative Metrics Store: TimescaleDB vs Thanos](#28-alternative-metrics-store-timescaledb-vs-thanos)
- [29. Historical: In-database PL/pgSQL hybrid proposal (superseded by v4.0 Go engine)](#29-historical-in-database-recommendation-computation-plpgsql-hybrid-proposal)
- [30. OpenShift Virtualization VM Recommendations](#30-openshift-virtualization-vm-recommendations)
- [Appendix A: Operator ROS Query Inventory](#appendix-a-operator-ros-query-inventory)
- [Appendix B: Kruize Recommendation Logic Details (Legacy)](#appendix-b-kruize-recommendation-logic-details-legacy)
- [Appendix C: Kubernetes VPA Default Configuration](#appendix-c-kubernetes-vpa-default-configuration)
- [Appendix D: Confidence Levels](#appendix-d-confidence-levels)
- [Appendix E: Implementation Reference Guide](#appendix-e-implementation-reference-guide)
- [Appendix F: Phasing Dependency Graph](#appendix-f-phasing-dependency-graph)
- [Appendix G: Assumptions, Scope, and Open Questions for Specification Writers](#appendix-g-assumptions-scope-and-open-questions-for-specification-writers)

---

## 1. Executive Summary

**v4.0:** The remote recommendation path computes workloads **in Go inside ros-ocp-backend** against PostgreSQL 16+ (daily digests + interval rows; exact percentiles with `slices.Sort()`), without Kruize, PL/pgSQL math, TimescaleDB, or t-digest.

The **legacy Kruize `remote_monitoring` baseline** (retained below for comparison with Alternatives A–B and historical benchmarks) serialized and deserialized metrics data **11 times** and stored it in **4 locations** before a recommendation was produced. Its two primary bottlenecks were:

1. **`/updateResults` HTTP call overhead**: 4N HTTP round-trips per hour for N containers (sequential JSON serialization, deserialization, and PostgreSQL INSERT per call).
2. **Recommendation computation in Kruize**: Kruize read billions of JSONB rows from PostgreSQL, requiring CPU-intensive deserialization for each row, then sorted them to compute exact percentiles using boxed `Double` objects.

Two alternative architectures and three composable optimizations were evaluated (including options that predate or extend beyond v4.0):

### Architecture Alternatives


|                                               | Legacy (Remote, pre-v4.0 Kruize) | CSV → Thanos (Remote) | Kruize on Cluster (Local)      |
| --------------------------------------------- | -------------------------------- | --------------------- | ------------------------------ |
| **Kruize mode**                               | `remote_monitoring`              | `remote_monitoring`   | `local_monitoring`             |
| **Metrics serialization hops**                | 11                  | 7                     | 1                              |
| **Metrics storage locations**                 | 4                   | 3                     | 1 (Prometheus, already exists) |
| **Ingestion improvement**                     | Baseline            | ~10-15x               | Infinite (eliminated)          |
| **Recommendation read improvement**           | Baseline            | ~3-5x                 | Infinite (distributed)         |
| **Overall improvement (500 containers)**      | Baseline            | ~2-3x                 | ~4-6x                          |
| **Overall improvement (20M containers, 91d)** | Baseline            | ~5-15x                | ~50-100x                       |
| **Architectural change required**             | None                | Moderate              | Significant                    |


### Composable Optimizations


| Optimization                               | Mode                           | What it does                                                                            | Improvement                                                             |
| ------------------------------------------ | ------------------------------ | --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| **Integer types** (§9)                     | Remote                         | Millicores/KiB from operator through pipeline                                           | ~2-3x Thanos storage, eliminates float precision                        |
| **Approximate percentiles** (§10)          | Both                           | Optional: PromQL `quantile_over_time` or streaming sketches (v4.0 uses **exact** percentiles in Go via `slices.Sort()`) | ~10-50x vs naive JVM sort where applicable; not the v4.0 remote path      |
| **CPU recommendation algorithm** (§12)     | Both                           | Remove 1-core discontinuity, per-pod estimation; add safety margin, temporal decay      | Better rec quality, ~2-5x rec step improvement                          |
| **Kruize code-level fixes** (§13)          | Both (7/10)                    | Per-row transactions → batch, Gson reuse, HTTP pooling, sync fixes                      | ~2-5x ingestion throughput, no arch change                              |
| **Memory recommendation algorithm** (§16)  | Both                           | Adaptive margin, OOM feedback, trend detection, separate request/limit; §16 also discusses digest-style sketches as an optional extension | Better rec quality + safety, ~2-5x memory rec step (exact percentiles in v4.0 Go) |
| **GPU algorithm bug fixes** (§17)          | Both                           | Fix B200/RTX PRO gating bug, frame buffer gaps, add underutilization detection          | Correctness fix (silent failures → working recs)                        |
| **JVM/Quarkus algorithm fixes** (§18)      | Both                           | Fix thread pool undersizing, use actual heap/GC data, add queue-size rec                | Correctness fix + data-driven JVM tuning (mvp_demo branch)              |
| **Additional Kruize fixes** (§19)          | Both (12), Remote (4)          | errorReasons bug, unbounded maps, cross-model dup work, merge data loss, TreeMap, etc.  | 1 critical + 6 high crash/perf fixes, ~2x rec throughput                |
| **Additional ros-ocp-backend fixes** (§20) | Remote                         | RBAC nil panic, API 200-on-failure, Kafka panics, timeout gaps, poison messages         | 1 critical + 3 high crash/correctness fixes                             |
| **New recommendation types** (§23)         | Both                           | Idle detection, PVC right-sizing, HPA optimization, Go/Node.js runtime, QoS             | New savings categories (biggest gap vs industry)                        |
| **Replica count for impact** (§26)         | Both                           | Desired replica count per workload via kube-state-metrics                               | Enables total_savings = per_container × replicas                        |
| **JSONB → relational columns** (§27)       | Remote                         | Replace opaque JSONB blobs with typed columns; drop dead `workload_metrics` table       | ~10-20x storage per rec row, eliminates marshal/unmarshal               |
| **TimescaleDB instead of Thanos** (§28)    | Remote                         | *Alternative-store analysis:* PostgreSQL extension for metrics; COPY FROM for CSV; no new services (not v4.0: **plain PostgreSQL 16+**, no TimescaleDB) | Historical comparison in §28; v4.0 does not use TimescaleDB              |
| **In-database recommendations** (§29)      | Remote                         | *Superseded proposal:* PL/pgSQL `rollup()` + `approx_percentile()` + `regr_slope()` (§29 is **historical**; v4.0 is **all-Go** math) | Retained for trade-off narrative only                                   |
| **VM recommendations** (§30)               | Both                           | vCPU, memory, disk size + IOPS recs for OpenShift Virtualization VMs                    | New savings category (VM sprawl is #1 virt problem)                     |
| **Combined** (§11-13, §16-20, §23, §26-30) | Remote (pipeline) + Both (rec) | Hypothetical stackings (includes pre-v4.0 ideas + Thanos sketches)                      | Rec step: hours → minutes at 20M scale + new savings + total impact     |


A third architecture alternative (TSDB block shipping) was evaluated and rejected (§21). The GPU (§17) and JVM/Quarkus (§18, `mvp_demo` branch) recommendation algorithms were also analyzed. A deep second-pass audit (§19, §20) uncovered additional critical and high-severity issues in both Kruize and ros-ocp-backend. An industry gap analysis (§23) identified 10 additional recommendation types that competitors offer but Kruize does not, including idle workload detection, PVC right-sizing, HPA optimization, and Go/Node.js runtime tuning, with specific Prometheus queries required for each. A replica count mechanism (§26) was identified as essential for calculating total cluster-wide savings impact.

An incremental optimization for the **legacy Kruize remote** metrics path (typed PostgreSQL columns instead of JSONB) was also evaluated (§8). A deep analysis of JSONB usage (§27) revealed that one of the five JSONB columns is written but never read (dead weight), recommendations are deserialized to untyped `map[string]interface{}` then mutated and re-serialized (wasteful), and no PostgreSQL JSON operators are used — making relational columns strictly superior. As **historical alternative-store analysis** (not the v4.0 design), §28 compares TimescaleDB vs Thanos — including features such as extension-bundled approximate percentile tooling and `COPY FROM` for CSV — as contrasts to a separate Thanos stack; **v4.0 uses plain PostgreSQL 16+ without TimescaleDB**, and **does not use t-digest** (exact percentiles in Go).

**For specification writers:** Appendices E-G provide complete implementation reference material — repository maps, database schemas, API contracts, CSV formats, Kafka message structures, key constants, a phasing dependency graph with prerequisites, and a checklist of decisions that technical specifications must address. A future reader starting from freshly-cloned repositories can derive detailed technical specifications for all proposed features using only this report.

### Strategic Recommendation (§24-25)

**v4.0 implements this direction:** recommendation computation runs **natively in ros-ocp-backend (Go)** on **PostgreSQL 16+**, with **exact percentiles** (`slices.Sort()`), **daily digest** tables, and **`COPY FROM`** batch writes — **no** Kruize, PL/pgSQL recommendation functions, TimescaleDB, or t-digest on the remote path.

The table below models the **legacy Kruize remote baseline** vs a **hypothetical “superpowers”** stack that still assumed Thanos + optional approximate percentiles + integer types (see §11); it is **not** a literal description of every v4.0 deployment choice.


| Metric                      | Legacy (pre-v4.0 Kruize remote)   | "ros-ocp-backend with Superpowers"      | Factor         |
| --------------------------- | --------------------------------- | --------------------------------------- | -------------- |
| Ingestion throughput        | 8 containers/sec                  | 15,000 containers/sec                   | **~1,900x**    |
| Recommendation throughput   | 24 containers/sec                 | 60,000 containers/sec                   | **~2,500x**    |
| Max containers (1-hour SLA) | ~1,000                            | ~5,000,000                              | **~5,000x**    |
| Metrics storage (50K, 91d)  | 5.7 TB                            | 6 GB                                    | **~950x**      |
| Application RAM             | 350-700 MB                        | 50-100 MB                               | **~5x**        |
| Infrastructure              | 4 services (2 apps + 2 DBs)       | 1 service (1 app + plain PostgreSQL 16+; Thanos optional per §11) | **4x**         |
| Development cost            | ~52 PRs, 12-18 months, cross-team | ~1,700 LOC, 4-6 months, single team     | **~3x faster** |


---

## 2. Current Architecture

### v4.0 remote path (native Go recommendation engine)

Ingestion still follows operator → CSV → upload → koku → Kafka → **ros-ocp-backend** → **PostgreSQL 16+** (interval and digest storage). **Recommendation computation** runs entirely in **Go** in ros-ocp-backend: load the workload’s intervals **once**, then compute CPU, memory, idle, PVC, VM, node, and related terms (**read once, compute N terms**). Persist results with relational writes and **`COPY FROM`** where used. **No** Kruize HTTP round-trips for recommendations, **no** PL/pgSQL recommendation functions, **no** TimescaleDB, **no** t-digest — percentiles are **exact** via **`slices.Sort()`** on the values held in memory for that pass.

### Legacy data flow (pre-v4.0 `remote_monitoring` + Kruize)

The numbered pipeline below describes the **superseded** remote architecture retained for comparison with §3–§7 and Kruize-focused audits (§13–§16).

```
1. Prometheus (cluster) → operator queries → in-memory
2. in-memory → operator writes → CSV file (disk)
3. CSV → tar.gz → upload → S3/ingress
4. S3 → koku downloads → parses → Kafka → ros-ocp-backend
5. ros-ocp-backend downloads CSV → parses → writes to ros-ocp-backend PostgreSQL
6. ros-ocp-backend reads from PostgreSQL → HTTP POST /updateResults → Kruize
7. Kruize parses JSON → writes to Kruize PostgreSQL
8. Kruize reads from Kruize PostgreSQL → computes recommendations → writes to Kruize PostgreSQL
9. ros-ocp-backend polls /listRecommendations → reads from Kruize PostgreSQL (via API)
10. ros-ocp-backend writes recommendations to ros-ocp-backend PostgreSQL
11. ros-ocp-backend serves via REST API ← reads from ros-ocp-backend PostgreSQL
```

### Bottleneck 1 (legacy Kruize remote): `/updateResults` HTTP call overhead

Per upload cycle (1 hour of data, 4 × 15-min intervals), for N containers:

- **Calls per hour**: 4N (one per container per interval)
- **Per-call cost**: JSON serialization + HTTP POST + JSON deserialization + PostgreSQL INSERT + HTTP response
- **Per-call time**: ~20-50ms under load
- **For 500 containers**: 2,000 calls → ~40-100 seconds
- **For 2,000 containers**: 8,000 calls → ~160-400 seconds
- **Scaling**: Linear with N — no batching, no parallelism in the HTTP call path

### Bottleneck 2 (legacy Kruize remote): recommendation computation (PostgreSQL JSONB read + exact percentile sort)

For `generateRecommendations`, Kruize read all stored intervals for each experiment:

- **Row count**: N × T × 96 (containers × days × intervals per day)
- **Per-row cost**: PostgreSQL JSONB binary → text serialization + JDBC wire transfer + JVM JSON parse + object allocation
- **For 500 containers, 15-day term**: 720,000 JSONB rows
- **For 500 containers, 91-day term**: 4,368,000 JSONB rows
- **For 20M containers, 91-day term**: 7.3 billion JSONB rows

The JSONB deserialization is CPU-bound on both the PostgreSQL side and the JVM side. Each row stores field names redundantly (~50% overhead), and every read requires full parse regardless of which fields are needed.

After reading, Kruize built a `List<Double>` with one entry per interval, sorted it via `Collections.sort()`, and picked the percentile index. For a 91-day term, this means 8,736 boxed `Double` objects (24 bytes each) per container per metric, allocated, sorted, then discarded — causing significant GC pressure at scale.

### Key Observation: Data Format Match

The ROS CSV columns map 1:1 to Kruize's `/updateResults` JSON structure. The CSV contains pre-aggregated 15-minute snapshots (avg/min/max/sum for CPU and memory metrics) — the exact same data Kruize's `remote_monitoring` mode operates on. This means alternative architectures don't face an impedance mismatch; the pre-aggregated format is already what Kruize needs.

---

## 3. Alternative A: CSV → Thanos

### Architecture

Replace ros-ocp-backend's PostgreSQL metrics storage and `/updateResults` calls with Thanos remote write. Kruize pulls metrics from Thanos instead of receiving them via push API.

```
Steps 1-4: unchanged (operator → CSV → upload → koku → Kafka → ros-ocp-backend)
Step 5: ros-ocp-backend parses CSV → writes to Thanos via remote write
Step 6: Kruize queries Thanos via PromQL when generateRecommendations is called
Steps 7-9: Kruize computes recommendations → ros-ocp-backend polls → stores in PostgreSQL
```

### Why This Works

The CSV data is pre-aggregated gauges (not raw Prometheus counters), which can be inserted into Thanos as synthetic metrics:

```
ros_cpu_usage{agg="avg", container="X", pod="Y", namespace="Z"} 0.2
ros_cpu_usage{agg="min", container="X", pod="Y", namespace="Z"} 0.1
ros_cpu_usage{agg="max", container="X", pod="Y", namespace="Z"} 0.4
```

No `rate()` or counter reconstruction is needed — these are simple gauge writes at 15-minute intervals.

### What Changes


| Component           | Change                                                                                                                                        |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| **ros-ocp-backend** | Replace PostgreSQL metrics write with Thanos remote write client (Go library exists). Drop metrics tables.                                    |
| **Kruize**          | Implement Thanos/Prometheus pull-based data source in `remote_monitoring` mode. Query pre-aggregated metrics via simple PromQL gauge queries. |
| **Infrastructure**  | Add Thanos Receive + Store Gateway + Compactor. Multi-tenancy via `org_id` label or tenant headers.                                           |
| **Operator**        | No change.                                                                                                                                    |
| **koku**            | No change.                                                                                                                                    |


### What's Eliminated

- ros-ocp-backend PostgreSQL metrics tables
- `/updateResults` HTTP calls (2,000+ per hour for 500 containers)
- Kruize PostgreSQL for metrics storage (Kruize reads from Thanos on demand)
- Duplicate metrics storage (ros-ocp PG + Kruize PG → Thanos only)

### Kruize Modification Required (Legacy)

Kruize `remote_monitoring` currently receives data via push (`/updateResults`). This alternative requires Kruize to **pull** data from Thanos. The implementation involves:

1. HTTP client for Thanos Query API (PromQL-compatible)
2. Simple gauge queries (no `rate()`, no complex PromQL — data is pre-aggregated)
3. Query triggered by `generateRecommendations` call
4. Eliminates need for Kruize's `kruize_results` metrics storage

The scope is well-defined: an HTTP client + PromQL query builder + data deserializer. Conceptually similar to Kruize's existing `local_monitoring` mode but with simpler queries.

---

## 4. Alternative B: Kruize on Cluster (Legacy)

### Architecture

Run Kruize on the OpenShift cluster in `local_monitoring` mode. Kruize queries Prometheus directly, computes recommendations locally, and the operator includes recommendations in its upload payload.

```
Cluster Prometheus → Kruize (local monitoring) → computes recommendations
                                                        ↓
Operator fetches recommendations from Kruize → includes in upload payload
                                                        ↓
Upload to console.redhat.com → koku → Kafka → ros-ocp-backend
                                                        ↓
ros-ocp-backend stores recommendations in PostgreSQL → serves via API
```

### What's Eliminated

- **ROS CSV files entirely** (`ros-openshift-container-*.csv`, `ros-openshift-namespace-*.csv`)
- **All 30+ ROS Prometheus queries** in the operator
- **The entire backend metrics pipeline**: CSV parsing, PostgreSQL metrics storage, `/updateResults` calls, Kruize backend service, Kruize PostgreSQL
- **Recommendation polling**: ros-ocp-backend receives recommendations directly in the upload payload

### Operator Upload Payload Change

```
Current:                                    Proposed:
├── cm-openshift-pod-usage-*.csv            ├── cm-openshift-pod-usage-*.csv  (unchanged)
├── cm-openshift-node-usage-*.csv           ├── cm-openshift-node-usage-*.csv (unchanged)
├── cm-openshift-storage-usage-*.csv        ├── cm-openshift-storage-usage-*.csv (unchanged)
├── cm-openshift-namespace-usage-*.csv      ├── cm-openshift-namespace-usage-*.csv (unchanged)
├── cm-openshift-vm-usage-*.csv             ├── cm-openshift-vm-usage-*.csv (unchanged)
├── cm-openshift-nvidia-gpu-usage-*.csv     ├── cm-openshift-nvidia-gpu-usage-*.csv (unchanged)
├── ros-openshift-container-*.csv  ←REMOVED │
├── ros-openshift-namespace-*.csv  ←REMOVED │
│                                           ├── ros-recommendations.json (NEW, ~few KB)
└── manifest.json                           └── manifest.json
```

### What Changes


| Component                    | Change                                                                                                 |
| ---------------------------- | ------------------------------------------------------------------------------------------------------ |
| **Operator**                 | Fetch recommendations from local Kruize API, include in upload payload. Disable ROS metric collection. |
| **Kruize**                   | Deploy on cluster in `local_monitoring` mode. Auto-discover workloads in opted-in namespaces.          |
| **ros-ocp-backend**          | Simplified to receive recommendations from upload payload and serve via API. No Kruize interaction.    |
| **Infrastructure (backend)** | Remove Kruize service and Kruize PostgreSQL entirely.                                                  |
| **Infrastructure (cluster)** | Add Kruize (sidecar, operand, or Helm component).                                                      |


### Key Considerations


| Concern                                        | Assessment                                                                                                                                                                    |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Kruize resource overhead on cluster**        | Local monitoring mode: ~256-512 MB RAM (queries Prometheus on demand, doesn't store metrics). Lighter than remote monitoring.                                                 |
| **Prometheus retention for custom timeframes** | 91+ days needed for 90-day terms. Configurable via OpenShift monitoring stack. Thanos integration provides long-term storage. Graceful degradation if insufficient retention. |
| **Custom timeframes settings propagation**     | Operator polls backend for configuration (follows existing communication pattern).                                                                                            |
| **SaaS and on-prem compatibility**             | Works for both. Kruize runs on cluster in both cases. Backend is a lightweight recommendation store.                                                                          |
| **Experiment lifecycle**                       | Kruize local monitoring auto-discovers workloads. No manual experiment creation needed.                                                                                       |


---

## 5. Performance Comparison

### Data Path Efficiency


| Metric                                 | Legacy (Kruize remote) | CSV → Thanos  | Kruize on Cluster   |
| -------------------------------------- | ------------- | ------------- | ------------------- |
| Metrics serialization hops             | 11            | 7             | 1                   |
| Recommendations serialization hops     | 4             | 4             | 4                   |
| Total serialization hops               | 15            | 11            | 5                   |
| Metrics storage locations              | 4             | 3             | 1                   |
| Recommendations storage locations      | 2             | 2             | 1-2                 |
| Data volume leaving cluster (per hour) | ~400 KB (CSV) | ~400 KB (CSV) | ~few KB (recs only) |


### Processing Time (500 containers, 15-day term)


| Step                                 | Legacy (Kruize remote) | CSV → Thanos | Kruize on Cluster             |
| ------------------------------------ | ------------- | ------------ | ----------------------------- |
| Data ingestion                       | 40-100s       | **1-3s**     | **0s** (eliminated)           |
| Recommendation computation           | 30-120s       | 10-40s       | **5-20s** (direct Prometheus) |
| Other steps (upload, Kafka, storage) | 30-50s        | 30-50s       | 5-15s                         |
| **Processing total**                 | **~100-270s** | **~41-93s**  | **~10-35s**                   |
| **Improvement**                      | Baseline      | **~2-3x**    | **~5-8x**                     |


### Processing Time (2,000 containers, 15-day term)


| Step                       | Legacy (Kruize remote) | CSV → Thanos | Kruize on Cluster |
| -------------------------- | ------------- | ------------ | ----------------- |
| Data ingestion             | 160-400s      | 2-5s         | 0s                |
| Recommendation computation | 60-300s       | 20-80s       | 10-40s            |
| Other steps                | 30-50s        | 30-50s       | 5-15s             |
| **Processing total**       | **~250-750s** | **~52-135s** | **~15-55s**       |
| **Improvement**            | Baseline      | **~4-5x**    | **~10-15x**       |


### Scaling Behavior

The improvement factor grows with cluster size because the dominant bottleneck in the **legacy** remote path (`/updateResults` HTTP calls) is O(N) while alternatives are O(1) in HTTP requests (CSV → Thanos) or fully eliminated (Kruize on cluster).


| Containers | Legacy (Kruize remote) | CSV → Thanos        | Kruize on Cluster    |
| ---------- | ----------- | ------------------- | -------------------- |
| 100        | ~30-80s     | ~~15-30s (~~2x)     | ~~10-20s (~~3x)      |
| 500        | ~100-270s   | ~~41-93s (~~2-3x)   | ~~10-35s (~~5-8x)    |
| 2,000      | ~250-750s   | ~~52-135s (~~4-5x)  | ~~15-55s (~~10-15x)  |
| 10,000     | ~900-3,500s | ~~120-400s (~~7-9x) | ~~60-250s (~~14-15x) |


---

## 6. Scalability at 20M Containers/Day

At SaaS scale (20M containers/day, ~833K containers/hour, ~10,000 clusters), the architectural differences become critical.

### Can the System Process the Daily Volume in 24 Hours?

#### Data Ingestion


|                          | Legacy (Kruize remote) | CSV → Thanos                 | Kruize on Cluster |
| ------------------------ | -------------------- | ---------------------------- | ----------------- |
| Calls/requests per hour  | 3.33M HTTP calls     | ~8,000 batched remote writes | 0                 |
| Required throughput      | 925 calls/s          | ~22,000 samples/s            | 0                 |
| Capacity per instance    | ~200-500 calls/s     | ~100K-500K samples/s         | N/A               |
| Instances needed         | 2-5 Kruize           | 1 Thanos Receive             | 0                 |
| Processing time per hour | 28-70 min            | 2-5 min                      | Negligible        |
| **Headroom**             | **None (saturated)** | **90%+ idle**                | **100% idle**     |


#### Recommendation Computation (91-day terms)


|                       | Legacy (Kruize remote)      | CSV → Thanos            | Kruize on Cluster   |
| --------------------- | --------------------------- | ----------------------- | ------------------- |
| Rows/points to read   | 7.3B JSONB rows             | 4.2B time-series points | 0 (distributed)     |
| Read format           | JSONB (parse + deserialize) | Compressed float chunks | N/A                 |
| Estimated time        | 5-20 hours                  | 2-6 hours               | 0                   |
| **Fits in 24 hours?** | **Barely / No**             | **Yes**                 | **Yes (trivially)** |


The **legacy Kruize remote** architecture hit a hard wall at 20M containers with 91-day terms: 7.3 billion JSONB rows may not be processable within 24 hours. The JSONB deserialization is CPU-bound and doesn't parallelize well.

Kruize on cluster makes the problem trivially parallelizable: 10,000 independent 2,000-container problems, each solved by a local Kruize instance with direct Prometheus access.

---

## 7. Storage Comparison

### 20M Containers, 91-Day Retention


| Store                        | Legacy (Kruize remote) | CSV → Thanos   | Kruize on Cluster         |
| ---------------------------- | ----------- | -------------- | ------------------------- |
| ros-ocp-backend PG (metrics) | ~1.5-3.5 TB | **0**          | **0**                     |
| Kruize PG (metrics)          | ~1.5-3.5 TB | **0**          | **0**                     |
| Thanos (metrics)             | 0           | ~6-12 GB       | 0                         |
| Kruize PG (recommendations)  | ~40-100 GB  | ~40-100 GB     | **0** (no backend Kruize) |
| ros-ocp PG (recommendations) | ~40-100 GB  | ~40-100 GB     | ~40-100 GB                |
| **Total backend storage**    | **~3-7 TB** | **~90-210 GB** | **~40-100 GB**            |
| **Improvement**              | Baseline    | **~20-50x**    | **~50-100x**              |


The dominant storage cost in the **legacy Kruize remote** system was JSONB overhead: field names repeated in every row, JSONB structural overhead, and two full copies of the same data (ros-ocp PG + Kruize PG).

---

## 8. Incremental Optimization: Typed Columns (legacy Kruize remote metrics path) — `Remote only`

Independent of architectural changes, replacing JSONB with typed PostgreSQL columns improved the **legacy** Kruize `remote_monitoring` metrics read path (still relevant where JSONB-heavy interval tables exist).

### Change

```sql
-- Instead of:
extended_data JSONB  -- {"cpuUsage": {"avg": 0.2, "min": 0.1, ...}, ...}

-- Use:
cpu_usage_avg DOUBLE PRECISION,
cpu_usage_min DOUBLE PRECISION,
cpu_usage_max DOUBLE PRECISION,
cpu_usage_sum DOUBLE PRECISION,
-- ... (20 typed columns total)
```

### Impact


| Aspect                       | JSONB                          | Typed Columns             | Improvement |
| ---------------------------- | ------------------------------ | ------------------------- | ----------- |
| Storage per row              | ~200-500 bytes                 | ~160 bytes                | ~1.5-3x     |
| Read: PostgreSQL side        | Parse JSONB binary → text      | Read fixed-width doubles  | ~2-4x       |
| Read: JVM side               | JSON parse → object allocation | `getDouble()` → primitive | ~3-5x       |
| Single-metric column scan    | Must read full blob            | 8 bytes per row           | ~40-60x     |
| GC pressure                  | High                           | Low                       | Qualitative |
| **Overall read improvement** | Baseline                       |                           | **~3-5x**   |


### Verdict

Typed columns are a **low-risk, incremental improvement** (~3-5x read improvement) that can be implemented without architectural changes. However, they only address the read bottleneck — the `/updateResults` HTTP call overhead (the larger bottleneck at scale) is unchanged. If committing to CSV → Thanos, typed columns become irrelevant because JSONB is eliminated entirely.

---

## 9. Optimization: Integer Types (Millicores/KiB) — `Remote only`

### Motivation

The current pipeline represents CPU in fractional cores (float64) and memory in bytes (float64). This causes:

1. **Long string representations**: `"0.002346"` (8 chars) for CPU, `"1073741824.000000"` (17 chars) for memory
2. **Float precision artifacts**: `0.002346` is not exactly representable in IEEE 754; it becomes `0.00234600000000000019...`
3. **Unnecessary conversion at output**: Kruize's `formatCpuUnits()` calls `Math.round(amount * 1000)` to produce millicores anyway

### Change

Convert at the source — the operator's `floatToString` function:

```go
// Current:
func floatToString(inputNum float64) string {
    return strconv.FormatFloat(inputNum, 'f', 6, 64)  // → "0.002346"
}

// Proposed for CPU:
millicore := int64(math.Round(inputNum * 1000))
strconv.FormatInt(millicore, 10)  // → "2"

// Proposed for memory:
kib := int64(math.Round(inputNum / 1024))
strconv.FormatInt(kib, 10)  // → "1048576"
```

All downstream systems receive integer values. Since integers up to 2^53 are exactly representable in float64, **no precision is lost in any subsequent step** after the operator's conversion.

### Precision Implications

Kruize already applies a 1-millicore idle threshold — containers with peak CPU ≤ 0.001 cores receive `NOTICE_CPU_RECORDS_ARE_IDLE` and no CPU recommendation. Sub-millicore precision is not used in any recommendation path. For memory, Kruize formats output as integer MiB/GiB via `resource2str()`. Sub-KiB precision is not needed.

### Impact on Each Pipeline Stage


| Stage                                  | Fractional cores/bytes                                  | Integer millicores/KiB                 |
| -------------------------------------- | ------------------------------------------------------- | -------------------------------------- |
| **CSV numeric field** (avg of 20 cols) | ~10-14 chars/value                                      | ~3-5 chars/value                       |
| **CSV file size**                      | baseline                                                | **~50-60% smaller numerics**           |
| **tar.gz upload size**                 | baseline                                                | **~30-40% smaller**                    |
| **Thanos remote write**                | float64 samples                                         | float64 samples (integer-valued)       |
| **Thanos gorilla XOR compression**     | ~6-8 bytes/sample                                       | ~2-3 bytes/sample                      |
| **Kruize PromQL results**              | imprecise floats                                        | exact integers as float64              |
| **Kruize `formatCpuUnits()`**          | `Math.round(x * 1000)`                                  | already millicores — **no conversion** |
| **Percentile phantom values**          | possible (float artifacts create false distinct values) | **eliminated**                         |


### Why Integer-Valued Float64 Compresses Better in Thanos

Thanos/Prometheus uses gorilla XOR encoding. Consecutive values are XOR'd and only differing bits are stored:


| Value pattern                                   | XOR bits needed per sample | Effective compression |
| ----------------------------------------------- | -------------------------- | --------------------- |
| Identical values (2.0, 2.0, 2.0)                | 1 bit                      | ~64:1                 |
| Small integer variation (2.0, 3.0, 2.0)         | ~12-16 bits                | ~4-5:1                |
| Arbitrary floats (0.002346, 0.003127, 0.002891) | ~40-50 bits                | ~1.3-1.6:1            |


Integer millicores share exponent bits and most mantissa bits, producing very short XOR results.

### Storage Impact (20M containers, 91-day retention)


|                               | Thanos (fractional float) | Thanos (integer millicores/KiB) |
| ----------------------------- | ------------------------- | ------------------------------- |
| Samples                       | ~7.3B                     | ~7.3B                           |
| Bytes per sample (compressed) | ~6-8 bytes                | ~2-3 bytes                      |
| **Total Thanos storage**      | **~44-58 GB**             | **~15-22 GB**                   |


### Type Journey Comparison

```
Current (fractional cores, float):
  Prometheus float64 → FormatFloat(6dp) → "0.002346" → CSV → ParseFloat → float64
  → FormatFloat → "0.002346" → JSON → Double.parseDouble → Double(0.002346)
  → JSONB numeric → ... → Double → sort → percentile → Math.round(x*1000) → millicores
                                                                               ↑ finally

Proposed (integer millicores):
  Prometheus float64 → Round(x*1000) → int64(2) → FormatInt → "2" → CSV → ParseInt
  → int64(2) → float64(2.0) → Thanos → ... → float64(2.0) → percentile → done
                                                                            ↑ already millicores
```

### Verdict

Integer types are a **clean, low-risk improvement** that should be done alongside the CSV → Thanos migration. The operator conversion point (`math.Round(value * 1000)`) is the single place where precision is locked to millicore/KiB granularity. Every downstream system benefits from shorter strings, better compression, and no float precision artifacts.

---

## 10. Optimization: Approximate Percentiles — `Both`

**v4.0 note:** The shipped remote engine uses **exact** percentiles in **Go** (`slices.Sort()` on values loaded for each workload pass). This section compares **legacy Kruize (Java)** costs and **optional** approximate / pushdown approaches (t-digest, PromQL, streaming sketches) for readers evaluating Thanos-side or sketch-based designs — it does **not** describe the v4.0 implementation.

### Kruize (Java) percentile implementation (legacy)

Kruize used exact percentile via sort-and-pick:

```java
// CommonUtils.java
public static Double percentile(double percentile, List<Double> items) {
    Collections.sort(items);
    return items.get((int) Math.round(percentile / 100.0 * (items.size() - 1)));
}
```

This is called ~6 times per container (CPU p60, CPU p98, memory max, memory spike max, plus namespace equivalents). For a 91-day term, each call processes 8,736 boxed `Double` objects.

### Per-Container Cost of Exact Percentile (91-day term)


| Resource                    | Cost                                                                       |
| --------------------------- | -------------------------------------------------------------------------- |
| List allocation             | 8,736 × `Double` autobox = 8,736 heap objects × 24 bytes = **210 KB**      |
| Sort                        | `Collections.sort()` on boxed `Double`: ~113K comparisons with unbox/rebox |
| GC                          | 8,736 `Double` objects become garbage after sort                           |
| Time                        | ~1-5 ms per percentile call                                                |
| **Per container (6 calls)** | **~1.26 MB heap, ~6-30 ms, ~52K garbage objects**                          |


### At Scale (20M containers)


|                             | Exact (legacy Kruize JVM)                          |
| --------------------------- | -------------------------------------------------- |
| Percentile computations     | 120M (6 per container)                             |
| Cumulative short-lived heap | ~25 TB                                             |
| Computation time            | ~120-600 seconds                                   |
| GC pressure                 | Severe (trillions of short-lived `Double` objects) |


### Alternative: T-Digest (historical evaluation — not v4.0)

> **Not implemented.** The shipped native engine uses **exact** percentiles via `slices.Sort()` (see v4.0 note at §10). The following compares approximate t-digest to exact sort for **legacy Kruize** and **optional** sketch-based designs only.

T-digest is a streaming approximate percentile algorithm that maintains a compressed distribution using ~~200 centroids (~~3.2 KB), regardless of how many values are fed in.


| Aspect                      | Exact sort                  | T-Digest (δ=200)                  |
| --------------------------- | --------------------------- | --------------------------------- |
| **Memory per computation**  | 8,736 × 24 = 210 KB         | 200 × 16 = 3.2 KB                 |
| **Heap allocations**        | 8,736 `Double` objects      | 0 (primitive `double` internally) |
| **Operations**              | ~113K comparisons (TimSort) | ~8,736 inserts × O(log 200)       |
| **Time per metric**         | ~1-5 ms                     | ~0.1-0.3 ms                       |
| **GC pressure**             | 8,736 objects → garbage     | ~0                                |
| **Per container (6 calls)** | ~6-30 ms, 1.26 MB heap      | ~0.6-1.8 ms, 19 KB heap           |


### Accuracy

For δ=200 and N=8,736 values:


| Percentile | T-digest error                | Equivalent shift          | Impact on recommendation                       |
| ---------- | ----------------------------- | ------------------------- | ---------------------------------------------- |
| p60        | ±0.1-0.3%                     | ±5-26 ranks out of 8,736  | Negligible (~±1.5m CPU on 500m recommendation) |
| p98        | ±0.2-0.5%                     | ±17-44 ranks out of 8,736 | Negligible                                     |
| p100 (max) | Exact (max is always tracked) | 0                         | None                                           |


For context, Kruize's nearest-rank method (no interpolation) has inherent ~0.01% granularity at N=8,736. T-digest accuracy is well within the same order. With integer millicores, the recommendation difference would be at most ±1-2 millicores.

### Note on Memory Percentiles

Both the cost and performance models use **p100 for memory** (which is simply `Collections.max()`). T-digest is overkill for max — a running `max` variable suffices. T-digest's value is specifically for the CPU percentiles (p60 cost, p98 performance).

### Alternative: Push Percentile to Thanos via PromQL

In the CSV → Thanos architecture, instead of Kruize computing percentiles locally, the computation can be pushed to Thanos:

```promql
quantile_over_time(0.98, ros_cpu_derived{container="X", org_id="1234567"}[91d])
```

This eliminates the Java-side computation entirely. Thanos uses Go's sort (primitive `float64`, no boxing) which is inherently more efficient. However, Kruize's CPU recommendation has conditional logic that can't be expressed as a single PromQL query (see Appendix B), so **ros-ocp-backend would need to compute and store the derived metric values** at write time.

### The Bigger Win: Streaming Digests

T-digest's most significant advantage is **streaming computation** — it eliminates the need to re-read all historical values:

```
Current:
  Interval arrives → store value → ... 8,735 more arrivals ...
  → read ALL 8,736 values → sort → pick percentile

Streaming t-digest:
  Interval arrives → digest.add(value)  [O(1)]
  → recommendation available immediately from digest.quantile(0.98)  [O(1)]
```

### Daily digests for custom timeframes (sketch-based alternative — not v4.0)

**v4.0:** Uses **daily digest** **tables** in PostgreSQL 16+ as storage alongside interval rows; the Go engine still computes **exact** percentiles from in-memory slices (`slices.Sort()`), not merged approximate sketches.

The following describes **mergeable sketches** (e.g. t-digest) as a **hypothetical** way to enable flexible windows without re-reading raw samples:

T-digests are **mergeable**, enabling flexible time windows without re-reading data:

```java
// Maintain per-day digests (~3 KB each, stored as compact blobs)
// At recommendation time, merge for the desired window:

// 15-day term:
TDigest merged = TDigest.createMergingDigest(200);
for (int d = today - 14; d <= today; d++) {
    merged.add(dailyDigests.get(d));    // O(δ log δ) per merge
}
double p98 = merged.quantile(0.98);     // O(log δ)

// 91-day term: same code, merge 91 digests
// Business hours: maintain separate business-hours digests per day
```


| Time window | Read-all-from-Thanos + sort | Daily-digest merge        |
| ----------- | --------------------------- | ------------------------- |
| 15-day      | Fetch 1,440 samples → sort  | Merge 15 digests (45 KB)  |
| 91-day      | Fetch 8,736 samples → sort  | Merge 91 digests (273 KB) |
| Time        | ~1-5 ms                     | ~0.05-0.2 ms              |
| **Speedup** | baseline                    | **~10-50x**               |


### At Scale: 20M Containers with Daily Digests


|                            | Thanos-only percentile   | Thanos + daily digests                       |
| -------------------------- | ------------------------ | -------------------------------------------- |
| Percentile data read       | 7.3B samples from Thanos | 20M × 91 digests × 3 KB = ~5.5 TB blob reads |
| Computation time           | ~120-600s                | ~12-36s                                      |
| Thanos query load for recs | Heavy (~4.2B samples)    | **Zero** (digests are local)                 |
| **Recommendation step**    | **2-6 hours**            | **~1-5 minutes**                             |


---

## 11. Combined Scenario: Thanos + Integer Types + Approximate Percentiles — `Remote (pipeline) + Both (rec)`

**v4.0 note:** This is a **hypothetical** combined stack (Thanos + integer samples + sketch/merge percentiles). **Shipped v4.0** uses **plain PostgreSQL 16+** for metrics/digests and **native Go** recommendations with **exact** percentiles — **no** Thanos requirement, **no** t-digest.

### Architecture (hypothetical — not v4.0)

```
Operator:
  Prometheus float64 → math.Round(value * 1000) → int64 millicores → "2" → CSV

ros-ocp-backend:
  CSV "2" → int64 → float64(2.0) → Thanos remote write
  Also (hypothetical): compute per-interval derived values → update daily sketch blob (e.g. t-digest) → store blob

Thanos:
  Stores float64(2.0) with gorilla XOR → ~2-3 bytes/sample
  (Raw metrics available for debugging/auditing)

Service computing recommendation (could be ros-ocp-backend or legacy Kruize, in this thought experiment):
  Read 91 daily sketch blobs → merge → approximate quantile(0.98) → recommendation in millicores
  No Thanos query needed for recommendation computation
  Thanos used only for raw data access if needed
```

### Serialization Hops


|                             | Legacy (Kruize remote) | Thanos only                | Thanos + Int + Digest    |
| --------------------------- | --------------------- | -------------------------- | ------------------------ |
| Metrics hops                | 11                    | 7                          | 5                        |
| Percentile computation hops | N/A (embedded in rec) | Query Thanos → sort in JVM | Read digest blob → merge |
| Total                       | 15                    | 11                         | 5                        |


### End-to-End Performance (20M containers, 91-day terms)


| Step                           | Legacy (Kruize remote) | Thanos only              | **Thanos + Int + Digest**   |
| ------------------------------ | ------------------ | ------------------------ | --------------------------- |
| Ingestion (per hour)           | 28-70 min          | 2-5 min                  | **1.5-4 min**               |
| Rec: data read                 | 5-20 hours (JSONB) | 20-60 min (Thanos query) | **~1-3 min** (digest blobs) |
| Rec: percentile computation    | 2-10 min (sort)    | 2-10 min (sort)          | **~0.2-0.6 min** (merge)    |
| Rec: other (threshold, output) | 5-15 min           | 5-15 min                 | 5-15 min                    |
| **Recommendation total**       | **5-20 hours**     | **30-85 min**            | **~6-19 min**               |
| **Fits in 24 hours?**          | **Barely / No**    | **Yes**                  | **Yes (trivially)**         |
| **Daily headroom**             | 0%                 | ~94-96%                  | **~99%**                    |


### Storage (20M containers, 91-day retention)


| Store                | Legacy (Kruize remote) | Thanos only     | **Thanos + Int + Digest**               |
| -------------------- | ----------- | --------------- | --------------------------------------- |
| Metrics (PG)         | ~3-7 TB     | 0               | 0                                       |
| Metrics (Thanos)     | 0           | ~44-58 GB       | **~15-22 GB**                           |
| Daily digests        | 0           | 0               | **~5.5 TB blob** (or ~55 GB compressed) |
| Recommendations (PG) | ~80-200 GB  | ~80-200 GB      | ~80-200 GB                              |
| **Total**            | **~3-7 TB** | **~124-258 GB** | **~95-277 GB**                          |


Note: Daily digests are compact (~3 KB per container per metric per day). The 5.5 TB raw figure is for uncompressed blobs; with simple compression this drops to ~50-60 GB. Alternatively, digests can be stored in Thanos as custom metrics or in a key-value store.

### What Each Optimization Contributes


| Optimization                  | Primary bottleneck addressed               | Incremental improvement over previous                          |
| ----------------------------- | ------------------------------------------ | -------------------------------------------------------------- |
| **Thanos**                    | `/updateResults` HTTP calls, JSONB storage | Ingestion: **~10-15x**. Rec read: **~5-15x**.                  |
| **+ Integer types**           | Float precision, string/storage overhead   | Thanos storage: **~2-3x smaller**. Pipeline correctness.       |
| **+ Approximate percentiles** | Rec computation (sort + GC + data re-read) | Rec step: **~5-15x faster** (or ~100x with streaming digests). |


---

## 12. Optimization: CPU Recommendation Algorithm — `Both`

### Scope: Container and Namespace Recommendations

This analysis applies to **both** container-level (`getCPURequestRecommendation`) and namespace-level (`getCPURequestRecommendationForNamespace`) CPU recommendations. The namespace variant shares the same 1-core discontinuity, JSONObject/JSONArray overhead, and unused MIN computation. One notable difference: the namespace version does **not** use the fragile per-pod estimation (`numPods = sum/avg`) — it directly uses `cpuUsageTotal = cpuUsage + cpuThrottle`. This is correct behavior that should be backported to the container version.

### Problems with the Current Algorithm

Kruize's CPU recommendation algorithm (`GenericRecommendationModel.getCPURequestRecommendation`) has five structural problems that produce suboptimal recommendations independently of the data pipeline architecture.

#### Problem 1: The 1-Core Discontinuity

The algorithm changes behavior at two points based on a 1-core threshold:

**Per-interval level:** If `cpuUsageTotal < 1.0`, the raw value is used. If `≥ 1.0`, a per-pod estimation path is triggered that computes `numPods = cpuUsageSum / cpuUsageAvg` and adjusts the value. A container fluctuating between 0.95 and 1.05 cores gets a different per-interval value depending on which side of 1.0 it lands on, creating recommendation instability.

**Aggregation level:** If the global max across all intervals is `< 1.0`, the recommendation is the **absolute maximum** (most conservative possible). If `≥ 1.0`, it's a percentile (p60 or p98). This creates a cliff: a container with peak 999m gets a recommendation of 999m (max), while one with peak 1001m gets p60 (~600m for uniform distribution) — a 40% drop from a 0.2% change in usage.

No industry tool uses this pattern. VPA applies the same percentile logic regardless of CPU level.

#### Problem 2: Fragile Per-Pod Estimation

`numPods = cpuUsageSum / cpuUsageAvg` is mathematically correct only when all pods have identical usage. It breaks with:

- Heterogeneous pods (one busy, others idle)
- Pod scaling events during the 15-min window
- `cpuUsageAvg` near zero (division produces an absurdly large pod count)
- Fallback to `memUsageSum / memUsageAvg` when CPU avg is 0 (mixing metrics)

This logic exists because the operator reports aggregate metrics across all pods of a workload. VPA avoids this by tracking individual containers via the Metrics API.

For the ROS use case, the per-pod estimation is unnecessary. The operator already filters by container+pod+namespace keys. Each CSV row represents a single container's metrics, not a workload aggregate. The `sum` columns represent the sum across a single container's samples within the 15-min window, not across pods.

#### Problem 3: No Temporal Weighting

All 8,736 intervals (91 days) carry equal weight. A container that was CPU-heavy 80 days ago but idle for the last 11 days will still receive a high recommendation because old values are equally weighted. Users expect recommendations that reflect current behavior.

VPA uses exponential decay with a 24-hour half-life: a sample from yesterday has half the weight of a sample from today. A sample from a week ago has 1/128th the weight. This naturally adapts to changing workloads.

#### Problem 4: Max-Then-Percentile Compounds Conservatism

The algorithm uses the **MAX** of (usage + throttle) within each 15-min interval as the representative, then takes p98 of those maxes. Using max per interval then percentile across intervals is more conservative than taking p98 of raw values:

- Within a 15-min interval with ~60 samples, the max is roughly p98 of that interval
- p98 of interval maxes ≈ p98 of p98 ≈ **p99.96** of the underlying distribution

For a container with mean 500m and stddev 100m, p98 of raw values is ~705m, but p98 of interval maxes is ~740m — a 5% over-recommendation that compounds across thousands of containers.

VPA uses raw samples directly (collected every minute) and takes p90 + 15% safety margin, producing a more balanced recommendation.

#### Problem 5: Throttle Handling

`cpuUsageTotal = cpuUsageMax + cpuThrottleMax` adds the peak throttle to the peak usage within the same interval. But max usage and max throttle within a 15-minute window rarely occur at the same instant. Throttle often spikes when the scheduler preempts CPU during usage dips, not during usage peaks. Adding both maxes overestimates the "what the container would use unconstrained" value.

A better approach: `cpuUsageAvg + cpuThrottleAvg` (average effective demand) or `max(cpuUsageMax, cpuUsageAvg + cpuThrottleAvg)`.

### Industry Standard: Kubernetes VPA

The Kubernetes Vertical Pod Autoscaler recommender (used in production across millions of clusters) takes a fundamentally different approach.

**Core algorithm:**

1. **Decaying histogram** — Fixed-size data structure (~1000 buckets with exponential boundaries, ratio 1.05). Each sample's weight decays as `2^((sampleTime - refTime) / halfLife)`. Memory is O(1) regardless of history length.
2. **Sample weighting** — Each CPU sample is weighted by `max(cpuRequest, 0.1)`, helping with multi-replica workloads.
3. **Target recommendation** — `percentile(p90, histogram) × (1 + safetyMargin)`. Default safety margin: 15%.
4. **Confidence bounds** — Upper bound is `p95 × (1 + 1/history_days)`. With 12h of history: ×3 multiplier. With 1 week: ×1.14. Prevents aggressive resizing with limited data.
5. **No special-casing** — No per-CPU-level thresholds, no per-pod estimation. Each container tracked individually.

See [Appendix C](#appendix-c-kubernetes-vpa-default-configuration) for the full VPA configuration defaults.

**Key differences from Kruize:**


| Aspect                     | Kruize                               | VPA                               |
| -------------------------- | ------------------------------------ | --------------------------------- |
| Data structure             | `List<Double>` (all raw values)      | Decaying histogram (~4 KB fixed)  |
| Memory per container (91d) | ~210 KB                              | ~4 KB                             |
| Temporal weighting         | None (all intervals equal)           | Exponential decay (24h half-life) |
| Percentile target          | p60 (cost) / p98 (performance)       | p90 (configurable)                |
| Safety margin              | None (CPU) / 20% (memory)            | 15% (both)                        |
| Minimum recommendation     | 1m (idle threshold)                  | 25m per pod                       |
| Special-case logic         | <1 core: use max; per-pod estimation | None                              |
| Throttle handling          | Adds max(throttle) to max(usage)     | Not considered (raw usage only)   |
| Confidence adjustment      | None                                 | Widens bounds with short history  |


**Industry convergence:** Across VPA, CAST AI, Kubecost, Datadog, and Densify, the standard pattern is:

```
recommendation = percentile(target_pct, weighted_usage_history) × (1 + safety_margin)
```

Where `target_pct` is p90-p95 for balanced, p50-p75 for cost-aggressive, p98-p99 for safety-first. No major tool uses Kruize's max-then-percentile or 1-core discontinuity.

### Three Algorithm Alternatives

#### Option A: Simplified Percentile + Safety Margin (Minimal Change)

Fix the problems with minimal code changes — remove the complexity, add a safety margin:

```
For each interval:
  cpu_effective = cpuUsageAvg + cpuThrottleAvg    // avg, not max; no per-pod estimation

Across all intervals:
  cpu_rec = percentile(target_pct, all_cpu_effective)   // always percentile, no <1 core special case
  cpu_rec = cpu_rec × (1 + safety_margin)               // 15% margin
  cpu_rec = max(cpu_rec, CPU_MIN_RECOMMENDATION)        // floor at 25m

Percentile targets:
  Cost model:        p60 (as before) + 15% margin
  Performance model: p98 (as before) + 15% margin
```


| Pro                                                           | Con                                         |
| ------------------------------------------------------------- | ------------------------------------------- |
| Minimal code change (~20 lines in GenericRecommendationModel) | No temporal weighting (old data = new data) |
| Easy to validate (same percentile concept, fewer edge cases)  | Still requires storing all intervals        |
| Compatible with optional sketch path in §10 (not used in v4.0)   | Doesn't adapt to workload drift             |
| Removes the 1-core cliff and per-pod estimation bugs          |                                             |
| Adds industry-standard safety margin                          |                                             |


**Implementation effort**: Very low. One PR.

#### Option B: VPA-Style Decaying Histogram

Replace the current algorithm with VPA's approach, adapted for 15-min aggregated inputs:

```
When 15-min interval arrives:
  cpu_effective = cpuUsageAvg + cpuThrottleAvg
  weight = max(cpuRequestAvg, 0.025) × 2^((intervalTime - refTime) / 24h)
  histogram.addSample(cpu_effective, weight)

When recommendation needed:
  target = histogram.percentile(p90) × 1.15
  lower  = histogram.percentile(p50) × 1.15
  upper  = histogram.percentile(p95) × 1.15 × (1 + 1/history_days)
```


| Pro                                            | Con                                                    |
| ---------------------------------------------- | ------------------------------------------------------ |
| Battle-tested algorithm (millions of clusters) | More complex to implement (~300 lines)                 |
| O(1) memory per container (~4 KB)              | Histogram bucket quantization (~5% error at tails)     |
| Exponential decay adapts to workload changes   | Different algorithm produces different recommendations |
| Three-level output (target, lower, upper)      | Requires validation against user expectations          |
| Industry standard                              |                                                        |


**Implementation effort**: Moderate. VPA's histogram is ~300 lines of Go. Could be implemented in ros-ocp-backend (Go) or ported to Java for Kruize.

#### Option C: Decaying t-digest (alternative — not v4.0)

**v4.0** does **not** use t-digest; it uses **exact** percentiles in Go (`slices.Sort()`). The following combines a t-digest (§10) with VPA-style exponential decay as a **hypothetical** medium-term option if approximate streaming centroids were desired:

```
When 15-min interval arrives:
  cpu_effective = cpuUsageAvg + cpuThrottleAvg   (in integer millicores)
  daily_digest.add(cpu_effective)                  // O(log δ) per insert

When day ends:
  Store daily_digest as ~3 KB blob with date key

When recommendation needed (e.g., 91-day term, 24h half-life):
  merged = new TDigest(δ=200)
  for day in [today-90 .. today]:
    weight = 2^((day - today) / halfLife_days)
    merged.addWeighted(daily_digests[day], weight)

  target = merged.quantile(0.90) × 1.15
  lower  = merged.quantile(0.50) × 1.15
  upper  = merged.quantile(0.95) × 1.15 × (1 + 1/history_days)
```


| Pro                                                     | Con                                             |
| ------------------------------------------------------- | ----------------------------------------------- |
| O(1) memory per container per day (~3 KB)               | Novel combination (less battle-tested than VPA) |
| Exponential decay (adapts to workload changes)          | Requires t-digest library                       |
| Mergeable across time windows (custom timeframes)       | Weighted merge needs validation                 |
| ~0.1-0.3% accuracy (better than VPA histogram at tails) |                                                 |
| Streaming (no re-read of historical data)               |                                                 |
| Naturally fits the Thanos + integer types architecture  |                                                 |
| Provides target/lower/upper bounds like VPA             |                                                 |


**Implementation effort**: Moderate. Uses mature t-digest libraries (`com.tdunning:t-digest` for Java, `github.com/caio/go-tdigest` for Go). The weighted daily merge is a custom extension (~50 lines).

### Comparison of Algorithm Options


|                             | Legacy Kruize                 | A: Simplified    | B: VPA Histogram         | C: Decaying t-digest (alt.) |
| --------------------------- | ----------------------------- | ---------------- | ------------------------ | ------------------------ |
| **Per-container memory**    | 210 KB (91d)                  | 210 KB (91d)     | ~4 KB (any)              | ~3 KB/day × 91 = 273 KB  |
| **Rec time per container**  | ~6-30 ms                      | ~3-15 ms         | ~0.01 ms                 | ~0.1-0.5 ms              |
| **Temporal adaptation**     | None                          | None             | 24h decay                | Configurable decay       |
| **Stores all intervals?**   | Yes                           | Yes              | No (histogram)           | No (daily digests)       |
| **Custom timeframes**       | Re-read all data              | Re-read all data | Checkpoint merge         | Daily digest merge       |
| **Accuracy at p90**         | Exact (but of maxes, not raw) | Exact            | ~5% (bucket width)       | ~0.1-0.3%                |
| **Safety margin**           | None                          | 15%              | 15%                      | 15%                      |
| **1-core discontinuity**    | Yes                           | No               | No                       | No                       |
| **Per-pod estimation**      | Yes (fragile)                 | No               | No                       | No                       |
| **Workload drift**          | Not handled                   | Not handled      | Handled                  | Handled                  |
| **Three-level output**      | No (target only)              | No               | Yes (target/lower/upper) | Yes (target/lower/upper) |
| **At 20M containers (91d)** | ~120-600s                     | ~60-300s         | ~0.2s                    | ~2-10s                   |
| **Implementation effort**   | —                             | Very low (1 PR)  | Moderate                 | Moderate                 |


### Recommendation

**Short term: Option A (Simplified).** Fix the five problems identified above with minimal code changes. Remove the 1-core discontinuity, remove per-pod estimation, use avg instead of max per interval, add 15% safety margin, set 25m floor. This is a one-PR change that immediately produces better recommendations with less code.

**Medium term (optional sketch path): Option C (decaying t-digest).** If pursuing the Thanos + sketch scenarios in §10–11, a weighted daily t-digest merge is one way to get VPA-quality temporal adaptation with streaming/merge performance for custom timeframes. **This is not the v4.0 design**, which stays with **exact** percentiles in Go.

**Why Option C over Option B (VPA histogram)?** The VPA histogram's fixed-bucket structure gives ~5% quantization error at the tails (p95+), while t-digest gives ~0.1-0.3%. Since the cost model uses p60 and the performance model uses p98, tail accuracy matters. Also, t-digest's daily-merge property is a natural fit for custom timeframes — something the VPA histogram doesn't natively support without full re-ingestion. Finally, t-digest is language-agnostic with mature libraries in both Go and Java, while VPA's histogram is Go-specific.

---

## 13. Optimization: Kruize Code-Level Improvements (Legacy)

Independent of the architectural changes above, the Kruize codebase has several code-level performance issues that affect throughput in **legacy** `local_monitoring` / remaining Kruize deployments and would persist even after a Thanos migration (unless Kruize is replaced entirely by moving computation to the cluster). They do **not** apply to **v4.0** remote recommendations, which no longer run inside Kruize.

**Mode applicability key:**

- **Local** = `local_monitoring` mode (Kruize on cluster, queries Prometheus directly)
- **Remote** = `remote_monitoring` mode (Kruize receives data via `/updateResults` API from ros-ocp-backend)
- **Both** = applies regardless of monitoring mode

### 13.1 Critical: HTTP Client Per-Request Construction — `Local only`

`GenericRestApiClient` (`common/utils/GenericRestApiClient.java`) creates a **new `CloseableHttpClient`** with a new SSLContext (trust-all), new socket factory, and full `HttpClients.custom()...build()` **on every HTTP request** (~line 142-146). This means:

- No connection reuse (no HTTP keep-alive, no pooling)
- Full TLS handshake per call
- New `ObjectMapper` per response parse (~line 117)

Additionally, `HttpUtils` (`common/utils/HttpUtils.java`) uses `HttpURLConnection` with **no `setConnectTimeout` / `setReadTimeout`** (~lines 48-76) — a slow backend can block threads indefinitely.

**Impact**: On the Prometheus query path in local monitoring mode, this adds ~50-200ms overhead per query (TLS setup + connection establishment). For ROS with 50 Prometheus queries per hour, that's 2.5-10 seconds of pure connection overhead per collection cycle. At scale with parallel queries, the lack of pooling prevents connection amortization. In remote monitoring mode, Kruize is the HTTP server (receiving `/updateResults` calls), not a client — so this issue does not affect the critical data ingestion path. However, `HttpUtils` (the other HTTP utility, used for miscellaneous outbound calls) also lacks timeouts, which affects both modes.

**Fix**: Single shared `CloseableHttpClient` with a `PoolingHttpClientConnectionManager`, pre-built SSLContext, and explicit timeouts. Thread-safe `ObjectMapper` as a static field. One PR, ~30 lines.

### 13.2 Critical: Database Write Path — Per-Row Transactions — `Both`

`ExperimentDAOImpl.addToDBAndFetchFailedResults` (~lines 271-335) uses **one transaction per row**:

```java
for (KruizeResultsEntry entry : kruizeResultsEntries) {
    tx = session.beginTransaction();
    session.persist(entry);
    session.flush();       // forces SQL execution
    // ... finally { tx.commit(); }
}
```

Each persist triggers a DB round-trip, an fsync on commit, and WAL flush. For a batch of 100 results, that's 100 transactions instead of 1.

Hibernate JDBC batching is not configured (`hibernate.jdbc.batch_size` is absent from `KruizeHibernateUtil.buildSessionFactory`), so even a single-transaction rewrite wouldn't batch without that setting.

The recommendation save path (`ExperimentDAOImpl.addRecommendationToDB`, ~lines 376-397) does a **read-then-write upsert** (SELECT to check existence, then INSERT or UPDATE) instead of a single `INSERT ... ON CONFLICT DO UPDATE`.

**Impact**: In remote monitoring, the `/updateResults` path processes ~2000 rows/hour (500 containers × 4 intervals). In local monitoring, Kruize queries Prometheus then stores results via the same DAO path. In both modes, per-row transactions add ~5-10ms overhead each (commit + fsync), totaling ~10-20 seconds/hour of pure transaction overhead. At 20M containers, this becomes the dominant cost in the write path — hours of commit overhead per day. The recommendation upsert issue affects both modes equally since recommendations are always stored to the database.

**Fix**: Single transaction for the batch, add `hibernate.jdbc.batch_size=50` and `hibernate.order_inserts=true`, replace upsert read-then-write with `ON CONFLICT`. Two PRs.

### 13.3 Critical: Gson Instance Explosion — `Both` (some sites Remote-only)

Across ~22 files, Kruize creates `new Gson()` or `new GsonBuilder()` on virtually every method call. Gson instances are thread-safe and reusable — creating them per call is pure allocation overhead.

**Worst offenders:**


| Location                                                             | Pattern                                                                                                                  | Frequency                      | Mode   |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------ | ------ |
| `UpdateResults.java` ~83-89                                          | New `GsonBuilder` + adapter chain per request                                                                            | Every `/updateResults` call    | Remote |
| `DBHelpers.convertExperimentResultToExperimentResultsTable` ~475-506 | New `GsonBuilder` with `setPrettyPrinting()`, then `gson.toJson()` → `objectMapper.readTree()` (Gson→Jackson round-trip) | Per result row                 | Both   |
| `RecommendationEngine` ~2037, 2113, 2277, 2409                       | `JSONObject.toString()` → `new Gson().fromJson(...)`                                                                     | Per recommendation computation | Both   |
| `DBHelpers.convertResultEntryToUpdateResultsAPIObject` ~820-848      | New `ObjectMapper` + new `GsonBuilder`, then `mapper.writeValueAsString()` → `gson.fromJson()` (Jackson→Gson round-trip) | Per failure response           | Remote |


The cross-library round-trips (Gson↔Jackson) are particularly wasteful — they serialize to a string with one library, then parse it again with the other, doubling allocation and CPU.

**Impact**: Each `GsonBuilder()` allocation + `create()` costs ~10-50μs (reflection, adapter resolution). At 2000 rows/hour, the DBHelpers path alone adds ~20-100ms/hour. The bigger cost is GC pressure — thousands of short-lived Gson instances, adapter chains, and intermediate strings put pressure on the young generation.

**Fix**: Static `Gson` instances (one per adapter configuration), eliminate cross-library round-trips by standardizing on one JSON library. Two PRs.

### 13.4 Critical: Broken Synchronization — `Both`

Two patterns that are both correctness bugs and performance issues:

`**synchronized (new Object())`** — appears in `ExperimentDBService` (~~line 370) and `ExperimentDAOImpl` (~~line 301) around partition creation DDL. A new lock object per call means **no thread ever blocks another** — the intended mutual exclusion doesn't happen. Concurrent partition DDL can race.

`**synchronized addExperimentToDB`** — the entire method in `ExperimentDAOImpl` (~line 62) is synchronized on the DAO instance, **serializing all experiment inserts globally**. Under concurrent `/createExperiment` calls, this is a hard bottleneck.

**Fix**: Replace `synchronized (new Object())` with a static lock or DB advisory lock for partition DDL. Replace method-level synchronization with DB uniqueness constraints + retry. One PR.

### 13.5 High: Hibernate Validator Factory Per Request — `Remote only`

`ExperimentInitiator.validateAndAddExperimentResults` (~lines 132-137) creates a **new Hibernate Validator factory on every `/updateResults` call**. In local monitoring mode, data is not received via `/updateResults`, so this code path is not hit.

```java
Validator validator = Validation.byProvider(HibernateValidator.class)
    .configure()
    .buildValidatorFactory()
    .getValidator();
```

Validator factories are expensive (reflection, annotation scanning, constraint resolution). This should be a static singleton — `ValidatorFactory` and `Validator` are both thread-safe.

**Impact**: ~1-5ms per request for factory construction. Minor at low scale, noticeable at high throughput.

**Fix**: Static `ValidatorFactory` + `Validator`. One line change.

### 13.6 High: Redundant Computation in Recommendation Engine — `Both`

`RecommendationEngine.generateRecommendationBasedOnModel` (~lines 834-840) rebuilds the filtered results map for **every model within the same term**. The recommendation engine runs identically in both modes — the difference is only how data arrives (Prometheus query vs `/updateResults` API), not how recommendations are computed.

```java
Map<Timestamp, IntervalResults> filteredResultsMap = containerData.getResults()
    .entrySet().stream()
    .filter(x -> x.getKey().compareTo(finalMonitoringStartTime) >= 0
              && x.getKey().compareTo(monitoringEndTime) <= 0)
    .collect(Collectors.toMap(Map.Entry::getKey, Map.Entry::getValue));
filteredResultsMap = filterResultsMapByBusinessHours(filteredResultsMap, ...);
```

For 3 terms × 2 models = 6 filter+collect+business-hours-filter operations per container, when only 3 are needed (one per term, reused across models).

The memory recommendation path also iterates all intervals twice — once for max memory usage (`calculateMemoryUsage` which returns a `JSONObject` when only a `double` is needed, and computes an unused MIN), once for spikes. A single-pass loop would halve the work.

**Impact**: At 8,736 intervals (91 days), each redundant filter+collect allocates ~8,736 `Map.Entry` objects and a new `HashMap`. For 20M containers × 3 redundant passes (out of 6 total: 3 terms × 2 models, only 3 needed) = ~30M unnecessary map constructions per recommendation cycle. When business hours filtering is enabled (`mvp_demo`), each pass also applies an additional timestamp-comparison filter.

**Fix**: Cache `filteredResultsMap` per term; single-pass memory recommendation loop; return primitives instead of `JSONObject` from `calculateMemoryUsage`. One PR.

### 13.7 High: PlotManager Triple-Sort — `Both`

`PlotManager.generatePlots` (~lines 130-137) calls `CommonUtils.percentile` three times on the same list for p25, p50, p75. Plot generation runs in both modes when enabled (`KruizeDeploymentInfo.plots`). Each call sorts the list via `Collections.sort`. After the first call, the list is already sorted — the next two sorts are O(n) verification passes (TimSort on sorted input) but still allocate and iterate unnecessarily. Then `Collections.max` is called on the sorted list (last element would suffice).

Additionally, `CommonUtils.percentile` **mutates the caller's list** (sorts in place) — any code relying on original order after calling percentile is silently broken.

**Fix**: Sort once, then index for all percentiles. Return a defensive copy or accept a pre-sorted flag. One PR.

### 13.8 Medium: Logging on Hot Paths — `Both` (some sites Local-only)

Three patterns waste CPU even when the log level is off:

1. `**LOGGER.debug(String.format(...))`** throughout `RecommendationEngine` (~lines 420, 450, 683, 686) — `String.format` executes eagerly regardless of log level. Should use SLF4J placeholders: `LOGGER.debug("{}", arg)`. **Both modes.**
2. `**LOGGER.info(promQL)` / `LOGGER.info(dateMetricsUrl)`** in tight loops in `RecommendationEngine` (~lines 2034, 2102) — floods logs under load. **Local monitoring only** (PromQL/datasource URL logging is on the Prometheus query path).
3. `**EMExecutorService`** logs at INFO on **every** `execute`/`submit` call (~lines 49-68) — high churn under trial throughput. **Both modes.**

**Fix**: Replace `String.format` with placeholders, demote hot-path logs to DEBUG or TRACE. One PR.

### 13.9 Medium: Unbounded In-Memory Maps — `Both`

`KruizeOperator.autotuneObjectMap` holds all experiments in a `ConcurrentHashMap` with no TTL, no eviction, and no size limit. Both modes load experiments from the database at startup and accumulate them during the process lifetime. The startup path (`InitiateListener`) loads **all** experiments, results, and recommendations from the database via `loadAllExperiments()` / `loadAllResults()` / `loadAllRecommendations()` — unbounded `FROM Entity` queries that pull full rows including large JSON columns.

The code has a TODO acknowledging this: `//todo load only experimentStatus=inprogress`.

**Impact**: At 20M containers with even modest experiment counts, the initial load materializes the entire JSON graph in JVM heap, doubling memory (DB page cache + Java heap). The unbounded map grows monotonically during the process lifetime.

**Fix**: Filter by status/time window on load, add eviction policy or LRU cache for the in-memory map. One PR for the filter, separate PR for eviction.

### 13.10 Low: Regex Compilation Per Call — `Both`

`CommonUtils.getTimeValue` / `getTimeUnit` (~lines 125-173) compile `Pattern.compile(...)` on every call. Should be `static final Pattern`. Used in both modes for parsing duration strings.

### Summary: Kruize Code-Level Optimization Priority


| #     | Issue                               | Mode               | Severity | Effort           | Impact at 20M                   |
| ----- | ----------------------------------- | ------------------ | -------- | ---------------- | ------------------------------- |
| 13.2  | Per-row transactions                | Both               | Critical | Low (2 PRs)      | Hours of commit overhead/day    |
| 13.3  | Gson explosion + cross-library JSON | Both (some Remote) | Critical | Low (2 PRs)      | GC pressure, ~10% CPU waste     |
| 13.1  | HTTP client per request             | Local              | Critical | Low (1 PR)       | Seconds of TLS overhead/cycle   |
| 13.4  | Broken synchronization              | Both               | Critical | Low (1 PR)       | Correctness + contention        |
| 13.6  | Redundant computation               | Both               | High     | Low (1 PR)       | 60M unnecessary map allocations |
| 13.5  | Validator factory per request       | Remote             | High     | Trivial (1 line) | ~1-5ms per request              |
| 13.7  | Triple-sort in PlotManager          | Both               | High     | Low (1 PR)       | Wasted sort cycles              |
| 13.8  | Logging overhead                    | Both (some Local)  | Medium   | Low (1 PR)       | CPU waste on hot paths          |
| 13.9  | Unbounded maps + full loads         | Both               | Medium   | Moderate (2 PRs) | Memory pressure at scale        |
| 13.10 | Regex compilation                   | Both               | Low      | Trivial          | Negligible                      |


**Mode distribution:** 7 of 10 issues affect **both** modes. One is **local-only** (HTTP client), one is **remote-only** (validator factory), and two have mode-specific sub-items within a broader "both" classification (Gson has remote-only sites in the `/updateResults` servlet; logging has local-only PromQL URL sites).

**Total estimated effort**: ~10 focused PRs, mostly mechanical. None require architectural changes. All are independent and can be parallelized.

**Combined impact by mode:**

- **Remote monitoring** (`/updateResults` path): ~2-5x ingestion throughput from per-row transaction batching + Gson reuse + validator singleton. Recommendation computation: ~1.5-3x from redundant computation + PlotManager fixes.
- **Local monitoring** (Prometheus query path): ~1.5-2x from HTTP client pooling + recommendation computation fixes. Less dramatic because the ingestion bottleneck (per-row transactions) is partially offset by Kruize controlling its own query rate.
- **Both modes**: Recommendation computation path improves ~1.5-3x regardless of mode.

---

## 14. Optimization: Kruize Database and API Layer (Legacy)

Additional Kruize issues beyond the code-level items in §13, focused on database schema, query patterns, and API-level inefficiencies.

### 14.1 Critical: `listRecommendations` Full Table Load — `Both`

When called without `experiment_name`, `ListRecommendations.doGet` triggers `ExperimentDBService.loadAllExperimentsAndRecommendations`, which issues **two unbounded queries**: `loadAllExperiments` (full `kruize_experiments` table) and `loadAllRecommendations` (full `kruize_recommendations` table), including all JSONB columns. The DAO code has an existing TODO: `//todo load only experimentStatus=inprogress`.

Any API consumer listing recommendations without filtering causes heap pressure proportional to the entire database. At scale, this is a denial-of-service vector.

### 14.2 Critical: O(n²) Recommendation Merge — `Both`

`ExperimentInterfaceImpl.addRecommendationsToLocalStorage` matches recommendations to experiments via nested loops: for each experiment name, it scans the **entire** recommendation list. For E experiments and R recommendations, this is O(E × R). A single-pass `Map<String, List<...>>` grouping would make it O(E + R).

### 14.3 Critical: Deep Clone via JSON (`Utils.getClone`) — `Both`

`Utils.getClone` performs deep copies by serializing an object to JSON with `setPrettyPrinting()` then deserializing back — a new `GsonBuilder` per call. The code's own Javadoc warns: *"CAUTION: Using this mechanism will have high impact on the performance."* Used from `DBHelpers.setRecommendationsToKruizeObject` in loops over K8s objects and recommendations.

### 14.4 High: Missing Composite Index on `kruize_results` — `Both`

The hottest table (`kruize_results`) has only single-column indexes on `experiment_name` and `interval_end_time`. Nearly every DAO query filters by `experiment_name` + `cluster_name` + `interval_end_time` range. A **composite index** `(experiment_name, cluster_name, interval_end_time)` would match the dominant access pattern far better.

The "get latest result" query (`SELECT_FROM_RESULTS_BY_EXP_NAME_AND_MAX_END_TIME`) uses a **correlated `MAX()` subquery** instead of `ORDER BY interval_end_time DESC LIMIT 1` — the latter uses the index directly and avoids a full-table scan.

### 14.5 High: `jsonb_extract_path_text` Without Index — `Both`

`kruize_lm_recommendations` is queried by `job_id` via `function('jsonb_extract_path_text', extended_data, 'job_id')` in HQL. This is not sargable — it cannot use any B-tree index. Every query scans the entire table. A **generated stored column** (`job_id TEXT GENERATED ALWAYS AS (extended_data->>'job_id') STORED`) with a B-tree index would make these lookups O(log n).

### 14.6 High: Partition DDL on Insert Path — `Both`

When `addToDBAndFetchFailedResults` encounters a "no partition" error, it calls `createPartitions` inline — executing DDL (`CREATE TABLE ... PARTITION`) from within the insert retry path. DDL acquires `AccessExclusive` locks on the parent table. Combined with the broken `synchronized (new Object())` (§13.4), two threads can race creating the same partition.

Pre-creating partitions (via a scheduled job or on startup) would keep DDL off the critical insert path entirely.

### 14.7 High: Bulk GET Loopback — `Remote only`

`BulkService.doGet` (for job status) performs an **HTTP GET to itself** (`listRecommendations` URL on localhost) to fetch recommendations associated with a job. This triggers the full-table-load path from §14.1, plus JSON serialization → HTTP transfer → JSON deserialization — all within the same process.

### 14.8 Medium: Full Entity Loads Without Projections — `Both`

All read paths load full `KruizeResultsEntry` / `KruizeRecommendationEntry` entities including large `extended_data` and `meta_data` JSONB columns, even when only scalar fields are needed. No DTO/projection queries exist. At 91 days of results, loading `extended_data` for every interval wastes bandwidth and heap.

### 14.9 Medium: Duplicate Sessions in Upsert — `Both`

`addRecommendationToDB` opens a session, then calls `loadRecommendationsByExperimentNameAndDate` which opens a **second** session for the existence check. Two connections consumed per save operation, doubling pool pressure under load.

### 14.10 Medium: Pretty-Printed JSON in API Responses — `Both`

`ListRecommendations.doGet` uses `GsonBuilder.setPrettyPrinting()` for the response. Pretty-printed JSON is ~30-40% larger than compact JSON — wasted bandwidth and CPU for API responses that are consumed by machines (ros-ocp-backend), not humans.

### 14.11 Low: Partition Date Range Precision — `Both`

`DB_PARTITION_DATERANGE` uses `TO ('... 23:59:59')` — PostgreSQL range partitions use half-open intervals `[start, end)`. Timestamps with sub-second precision (e.g., `23:59:59.001`) would fall outside the partition, causing insert failures. Should use `TO` = start of next day.

### Summary: Kruize Database/API Priority


| #     | Issue                                     | Mode   | Severity | Effort                       |
| ----- | ----------------------------------------- | ------ | -------- | ---------------------------- |
| 14.1  | Full table load in listRecommendations    | Both   | Critical | Low (add WHERE clause)       |
| 14.2  | O(n²) recommendation merge                | Both   | Critical | Low (single-pass map)        |
| 14.3  | Deep clone via JSON                       | Both   | Critical | Moderate (copy constructors) |
| 14.4  | Missing composite index on kruize_results | Both   | High     | Low (CREATE INDEX)           |
| 14.5  | jsonb_extract_path_text without index     | Both   | High     | Low (generated column)       |
| 14.6  | Partition DDL on insert path              | Both   | High     | Moderate (pre-create job)    |
| 14.7  | Bulk GET loopback                         | Remote | High     | Low (direct method call)     |
| 14.8  | Full entity loads, no projections         | Both   | Medium   | Moderate                     |
| 14.9  | Duplicate sessions in upsert              | Both   | Medium   | Low                          |
| 14.10 | Pretty-printed JSON responses             | Both   | Medium   | Trivial                      |
| 14.11 | Partition date range precision            | Both   | Low      | Trivial                      |


---

## 15. Optimization: ros-ocp-backend

**v4.0:** ros-ocp-backend ingests CSV/Kafka workloads, persists metrics and **daily digest** data to **PostgreSQL 16+**, and runs the **native Go recommendation engine** (`recommendMemory()`, `recommendCPU()`, etc.) with **`COPY FROM`** / relational writes for results — **no** Kruize HTTP dependency on the remote recommendation path.

The issues below target the **legacy** pipeline where ros-ocp-backend proxied metrics to **Kruize** over HTTP (`/updateResults`, experiment lifecycle, recommendation polling). They remain useful for backports, on-cluster Kruize (`local_monitoring`), and historical comparison.

### 15.1 Critical: CSV Full-Memory Load — `Remote only`

`ReadCSVFromUrl` (`internal/utils/utils.go` ~72-87) uses `csv.Reader.ReadAll()`, buffering the **entire** HTTP response as `[][]string`. Then `ProcessReport` loads that 2D slice into a gota `DataFrame` (additional allocations for column-oriented storage). Peak memory is proportional to file size, doubled by the dataframe materialization.

Config knobs `CSV_STREAM_INTERVAL` and `RECORD_LIMIT_CSV` exist in `internal/config/config.go` but are **never used** in any Go code — the streaming idea was designed but not implemented.

### 15.2 Critical: Sequential Kruize Pipeline — `Remote only`

`ProcessReport` processes each Kafka message fully sequentially: for each file, for each k8s group, for each namespace group: `Create_kruize_experiments` → loop chunks → `Update_results` → DB insert → Kafka produce. No worker pool, no bounded parallelism.

`updateExistingExperimentsForOrg` loads all workloads for an org via `GetWorkloadsByOrgID`, then calls `UpdateExperiment` **one experiment at a time** over HTTP to Kruize. For large tenants, wall-clock time scales linearly with workload count.

### 15.3 Critical: Kruize HTTP Calls Without Timeouts — `Remote only`

Most Kruize API calls use `http.Post()` (which uses `http.DefaultClient` — no timeout) or `&http.Client{}` (also no timeout):

- `Create_kruize_experiments`, `CreateNamespaceExperiment`, `Update_results`, `UpdateNamespaceResults` — all use `http.DefaultClient`
- `Update_recommendations`, `DeleteExperimentFromKruize` — use `&http.Client{}` without `Timeout`

Only `update_experiment.go` correctly sets `Timeout: 30 * time.Second`. A stuck Kruize can block the single consumer goroutine indefinitely.

### 15.4 High: GORM Query Bug (Possible) — `Remote only`

In `GetRecommendationSetByID` (`internal/model/recommendation_set.go` ~109-124) and `GetNamespaceRecommendationSetByID`:

```go
query := getRecommendationQuery(orgID)
query.Where("recommendation_sets.id = ?", recommendationID)  // return value not assigned
```

GORM's `Where` returns a **new** `*gorm.DB` session. Not assigning `query = query.Where(...)` means the **ID predicate is never applied** to the subsequent `query.First(...)`. This is a correctness bug that may also cause performance issues (scanning without the ID filter).

### 15.5 High: Kafka Producer Synchronous Per-Message — `Remote only`

`SendMessage` (`internal/kafka/producer.go` ~49-76) allocates a new `delivery_chan` per message, produces, then **blocks** on `<-delivery_chan`. Throughput is limited to one in-flight delivery at a time — no batching or pipelining.

### 15.6 High: O(n²) Container Deduplication — `Remote only`

`Create_kruize_experiments` (`internal/utils/kruize/kruize_api.go` ~42-50) checks `utils.StringInSlice(container, unique_containers)` for each container row — O(k²) for k containers. A `map[string]struct{}` would give O(1) lookups.

### 15.7 High: Missing Database Indexes — `Remote only`

- `recommendation_sets(monitoring_end_time)`: The list API always filters by `monitoring_end_time` range (from `MapQueryParameters`), but there's no index on this column or a composite `(workload_id, monitoring_end_time)`.
- `workloads(org_id)`: `GetWorkloadsByOrgID` filters by `org_id`, but the only index containing `org_id` is the composite unique constraint `(org_id, cluster_id, experiment_name)`. A standalone index on `org_id` would help large tenants.
- RBAC filters use `clusters.cluster_uuid IN (?)` and `workloads.namespace IN (?)` — worth validating with `EXPLAIN ANALYZE`.

### 15.8 Medium: Double Query for Paginated Lists — `Remote only`

`GetRecommendationSets` issues `query.Count(&count)` then a separate `query.Scan(...)` — two round-trips per list request. A single query with `COUNT(*) OVER()` as a window function would return both the total and the page in one round-trip.

### 15.9 Medium: Connection Pool Not Configured — `Remote only`

`internal/db/db.go` calls `gorm.Open(postgres.Open(dsn), ...)` without setting `SetMaxOpenConns`, `SetMaxIdleConns`, or `SetConnMaxLifetime` on the underlying `*sql.DB`. Under concurrent load (API + Kafka consumers), the pool uses Go defaults which may not be appropriate.

### 15.10 Medium: gota `Rapply` Redundant Work — `Remote only`

`filterValidCSVRecords` (`internal/utils/aggregator.go` ~169-176) calls `df.Names()` and `findInStringSlice("workload_type", columns)` inside `df.Rapply` — these execute **for every row**. Column indices should be computed once outside the closure.

### 15.11 Medium: Kafka Auto-Commit vs Long Processing — `Remote only`

The upload topic consumer uses `enable.auto.commit=true` (default from config). With long-running `ProcessReport` calls, auto-commit can advance the offset **before** processing completes. On crash or rebalance, messages may be lost or reprocessed. The recommendation poller correctly disables auto-commit and commits manually.

### 15.12 Medium: Namespace Historical Table Not Partitioned — `Remote only`

`historical_recommendation_sets` is list-partitioned by `org_id` with time-based sub-partitions (good). `historical_namespace_recommendation_sets` is a **plain unpartitioned table** (`migration 000018`). If namespace recommendations grow similarly, this table will lack the efficient partition-drop cleanup that the container historical table has, leading to expensive `DELETE` operations for data retention.

### 15.13 Low: Validator Instance Per Message — `Remote only`

`validator.New()` is called per Kafka message (`report_processor.go` ~31, `recommendation_poller.go` ~319). The validator is thread-safe and reusable — a package-level instance would eliminate per-message setup.

### 15.14 Low: Unused Config Knobs — `Remote only`

`KruizeWaitTime` and `CSV_STREAM_INTERVAL` are declared in `internal/config/config.go` but never referenced in any Go code.

### Summary: ros-ocp-backend Priority


| #     | Issue                              | Severity | Effort                       |
| ----- | ---------------------------------- | -------- | ---------------------------- |
| 15.1  | CSV full-memory load               | Critical | Moderate (streaming rewrite) |
| 15.2  | Sequential Kruize pipeline         | Critical | Moderate (worker pool)       |
| 15.3  | HTTP calls without timeouts        | Critical | Low (1 PR)                   |
| 15.4  | GORM Where not assigned (bug)      | High     | Trivial (1 line fix × 2)     |
| 15.5  | Kafka producer sync per-message    | High     | Low (async + batch)          |
| 15.6  | O(n²) container dedup              | High     | Trivial (use map)            |
| 15.7  | Missing DB indexes                 | High     | Low (migrations)             |
| 15.8  | Double query for pagination        | Medium   | Low                          |
| 15.9  | Connection pool not configured     | Medium   | Trivial (add config)         |
| 15.10 | gota Rapply per-row redundancy     | Medium   | Low                          |
| 15.11 | Kafka auto-commit risk             | Medium   | Low (manual commit)          |
| 15.12 | Namespace historical unpartitioned | Medium   | Moderate (migration)         |
| 15.13 | Validator per message              | Low      | Trivial                      |
| 15.14 | Unused config                      | Low      | Trivial (cleanup)            |


---

## 16. Optimization: Memory Recommendation Algorithm

**Applicability:** **Legacy Kruize** — both `local_monitoring` and `remote_monitoring` (Java `GenericRecommendationModel`). **v4.0** remote memory recommendations are implemented in **Go** (`recommendMemory()`) with **exact** percentiles (`slices.Sort()`), not the Java path described here.

### Scope: Container and Namespace Recommendations

This analysis applies to **both** container-level (`getMemoryRequestRecommendation`) and namespace-level (`getMemoryRequestRecommendationForNamespace`) memory recommendations in **Kruize**. The namespace variant (`calculateNamespaceMemoryUsage`) has an identical structure — same `percentile(100, list)` sort-for-max, same JSONObject overhead, same unused MIN computation, same spike calculation, same `min(usageBuf, spikeBuf)` logic. One notable difference: the namespace version does **not** use the per-pod estimation (`numPods = cpuSum/cpuAvg`) — it reads `memUsageMax` directly. This mirrors the namespace CPU variant and is correct behavior. Proposed improvements (adaptive margins, OOM feedback, trend detection, separate request/limit) apply equally to both container and namespace levels; **sketch-based** variants below use **t-digest** only as a **hypothetical** extension — **not** v4.0.

### Current Algorithm

The memory recommendation (`getMemoryRequestRecommendation`, `GenericRecommendationModel.java:209-253`) works as follows:

1. **Per-interval usage** (`calculateMemoryUsage`): For each 15-min interval, estimates pod count via `numPods = cpuUsageSum / cpuUsageAvg` (same fragile logic as CPU), computes per-pod memory `memUsageSum / numPods`, then takes `max(perPodMem, rawMax)`.
2. **Per-interval spike** (`calculateIntervalSpike`): `max(ceil(memUsageMax - memUsageMin), ceil(memRSSMax - memRSSMin))` — the larger range of working-set-bytes or RSS within each interval.
3. **Aggregate**: `percentile(p100, usageList)` and `percentile(p100, spikeList)` — both are `Collections.sort()` to find the max. Both cost and performance models use p100 for memory.
4. **Buffer**: `usageBuf = usage × 1.20`, `spikeBuf = usage + spike × 1.05`
5. **Final**: `memRec = min(usageBuf, spikeBuf)` — takes the **lesser** of the two approaches.

### Problems with the Current Algorithm


| #   | Problem                                    | Severity     | Details                                                                                                                                                                                                              |
| --- | ------------------------------------------ | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **Sorting to find max**                    | Performance  | `CommonUtils.percentile(100, list)` sorts 8,736 elements to return the last one. Should be `Collections.max()` — O(n) vs O(n log n). Done 4× per container.                                                          |
| 2   | **Same fragile per-pod estimation as CPU** | Correctness  | `numPods = cpuUsageSum / cpuUsageAvg` is inaccurate for heterogeneous pods and unnecessary since CSV rows are already per-container.                                                                                 |
| 3   | **JSONObject for a single double**         | Performance  | `calculateMemoryUsage` returns a `JSONObject` with unused `MIN` value. The caller reads only `MAX`. A `double` return would eliminate hash map allocation, string key lookups, and a Stream pipeline per interval.   |
| 4   | **No temporal weighting**                  | Rec quality  | All 8,736 intervals carry equal weight. A memory spike from 80 days ago permanently inflates the recommendation.                                                                                                     |
| 5   | **No OOM awareness**                       | Safety       | The algorithm has no mechanism to detect or respond to OOM kills. VPA bumps memory recommendation by 20% (min 100 MiB) on OOM; Kruize does nothing.                                                                  |
| 6   | **min(usage, spike) can undersize**        | Correctness  | Taking `min` of the two buffered approaches produces the less conservative value. If usage is high but spikes are small, the spike path wins and the 20% buffer is lost.                                             |
| 7   | **Single recommendation value**            | Completeness | Produces only one number. Kubernetes has separate `request` (scheduling guarantee) and `limit` (burst ceiling). A useful system should recommend both.                                                               |
| 8   | **No variability awareness**               | Rec quality  | A stable 500 MiB service and a spiky 200 MiB-2 GiB pipeline both get a fixed 20% buffer. The stable one wastes resources; the spiky one may be under-provisioned.                                                    |
| 9   | **No trend detection**                     | Rec quality  | Monotonically growing memory (leaks, cache accumulation) is not detected. The recommendation always lags behind actual growth until OOM.                                                                             |
| 10  | **p100 = max always**                      | Design       | Using p100 means the absolute worst-case interval from 91 days determines the recommendation forever. This is defensible only if you have no other safety mechanism — but OOM feedback is a better safety mechanism. |


### Why VPA Is Better — But Not Enough

VPA uses p90 with 15% safety margin + OOM bump-up. This is significantly better than Kruize's current approach because OOM detection provides a real safety net that allows using a percentile below max. However, VPA has its own limitations:

- Fixed 15% safety margin regardless of workload variability
- Single exponential decay (24h) misses weekly/monthly patterns
- No trend detection (memory leaks cause repeated OOM → bump → lag cycles)
- Single recommendation value (no separate request/limit models)

### Proposed: Adaptive percentile with trend projection and OOM feedback (pseudocode uses a digest — v4.0 uses sorted slices instead)

The variable names below say `t_digest` for brevity; in **v4.0 Go**, the same steps map to **exact** quantiles from **`slices.Sort()`** on in-memory samples (or precomputed daily rollups), not merged approximate sketches.

#### Memory Request (scheduling guarantee)

```
base           = t_digest.quantile(0.95)          // decayed 95th percentile (exact rank if using sorted slices)
iqr_cv         = (p75 - p25) / p50                // interquartile variability (from same digest)
margin         = clamp(iqr_cv, 0.15, 0.50)        // 15-50% adaptive margin
trend          = linear_slope(daily_means, 14d)    // MiB/day growth rate
projection     = max(0, trend × days_forward)      // forward projection (never negative)

request        = (base + projection) × (1 + margin)
request        = max(request, MIN_MEMORY_FLOOR)    // e.g., 64 MiB floor
```

**Adaptive margin:** The interquartile coefficient of variation (IQR-CV) is robust to outliers and is available from three quantile queries on the same sorted sample or digest (**exact** quantiles in v4.0):

- Stable workload (IQR-CV = 0.1): margin = 15% — minimal waste
- Moderate workload (IQR-CV = 0.3): margin = 30% — reasonable headroom
- Spiky workload (IQR-CV ≥ 0.5): margin = 50% — generous protection

#### Memory Limit (burst ceiling / OOM protection)

```
tail           = t_digest.quantile(0.999)          // extreme tail from digest
oom_floor      = max(recent_oom_limits) × 1.3      // 30% above last OOM trigger
headroom       = request × 1.5                     // default burst allowance

limit          = max(headroom, tail × 1.1, oom_floor)
```

The limit uses three competing floors — the most protective wins:

1. **Headroom floor** (`request × 1.5`): Always allows 50% burst beyond reservation
2. **Tail floor** (`p99.9 × 1.1`): Based on observed extremes with 10% margin
3. **OOM floor** (`oom_limit × 1.3`): Reactive — if container was OOM-killed at X, new limit ≥ 1.3X

#### OOM Feedback with Exponential Backoff

VPA bumps by `max(oom_limit × 1.2, oom_limit + 100 MiB)` — a fixed single reaction. The proposed approach converges faster for severely under-provisioned workloads:

```
First OOM in window:       oom_floor = oom_limit × 1.3
Second OOM within 24h:     oom_floor = oom_limit × 1.6
Third OOM within 24h:      oom_floor = oom_limit × 2.0
```

#### Trend Detection (unique — neither VPA nor Kruize has this)

```
daily_means = [mean(day_1_digest), ..., mean(day_14_digest)]
slope       = linear_regression(daily_means).slope      // MiB per day

if slope > 0 and statistically significant (p < 0.05):
    projection = slope × 7                              // project 7 days forward
    notification = "MEMORY_TRENDING_UP"
else:
    projection = 0
```

This catches memory leaks before OOM: if mean daily memory grows at 15 MiB/day for 14 days, the recommendation proactively adds 105 MiB. The notification tells the user to investigate the possible leak.

#### Multi-Timescale Awareness (unique)

Instead of VPA's single 24h decay, use two decay rates:

```
short_digest = merge(last_7_daily_digests, decay=24h)     // recent behavior
long_digest  = merge(last_91_daily_digests, decay=168h)   // weekly patterns

request = max(
    quantile(short_digest, 0.95) × (1 + short_margin),
    quantile(long_digest,  0.95) × (1 + long_margin)
)
```

Taking the **max** of short-term and long-term handles daily patterns (short digest), weekly batch jobs (long digest), and monthly spikes (long digest with slow decay). VPA's single exponential decay misses weekly/monthly patterns if the half-life is too short, or reacts too slowly if it's too long.

### On "Memory Limit = Memory Request"

A common DevOps recommendation is to set memory limit equal to memory request (Guaranteed QoS). This analysis recommends **against** that as a universal policy:

- Setting limit = request reserves peak capacity permanently for every pod, even though most workloads use peak memory a small fraction of the time. Across thousands of pods, this wastes enormous node capacity.
- Kubernetes Burstable QoS (request < limit) is the intended operating model for most workloads — pods get a guaranteed minimum and can burst when capacity is available.
- The real danger is having **no** memory limit (a memory leak consumes the entire node). A limit above the request caps the blast radius while allowing efficient overcommitment.
- **OOM-reactive feedback** is the correct safety mechanism: detect when a container hits its limit, bump the limit up, and alert the user. This is how VPA operates and it's battle-tested.

Limit = request remains appropriate for databases, stateful services, and latency-critical workloads where OOM kills are catastrophic.

### Comparison


| Aspect                    | Kruize Current             | VPA                             | Proposed                                       |
| ------------------------- | -------------------------- | ------------------------------- | ---------------------------------------------- |
| **Percentile**            | p100 (= max always)        | p90 (fixed)                     | p95 (adaptive margin compensates)              |
| **Safety margin**         | 20% (fixed)                | 15% (fixed)                     | 15-50% (adaptive to variability via IQR-CV)    |
| **Temporal decay**        | None                       | Exponential 24h                 | Dual-rate: 24h (short) + 168h (long)           |
| **Trend detection**       | None                       | None                            | Linear regression on daily means               |
| **OOM handling**          | None                       | +20% or +100 MiB (fixed)        | Exponential backoff (1.3×/1.6×/2.0×)           |
| **Request vs Limit**      | Single value               | Single value + fixed ratio      | Separate models with tail-aware limit          |
| **Multi-timescale**       | None                       | Single decay window             | Short (7d) + Long (91d), take max              |
| **Data structure**        | `ArrayList<Double>` sorted | Decaying histogram (fixed bins) | **v4.0:** sorted `float64` / `slices.Sort()`; sketch column: t-digest (hypothetical extension only) |
| **Variability awareness** | None                       | None                            | IQR-CV from quantiles on sorted samples        |
| **Leak detection**        | None                       | None                            | Slope + notification flag                      |
| **Minimum floor**         | None                       | 250 MiB                         | Configurable (e.g., 64 MiB)                    |


### Operator Changes Required

To enable OOM-aware recommendations, the koku-metrics-operator needs one additional Prometheus query:

```promql
kube_pod_container_status_last_terminated_reason{reason="OOMKilled"} == 1
```

The operator already collects memory limits (`kube_pod_container_resource_limits{resource='memory'}`). It needs to add the OOM signal to the CSV, e.g., a new column `oom_killed` (0 or 1) and `oom_limit_bytes` (the memory limit at time of kill).

### Implementation Path

1. **Short term (1 PR, code-level only):**
  - Replace `percentile(100, list)` with `Collections.max(list)` for both usage and spike lists
  - Return `double` from `calculateMemoryUsage` instead of `JSONObject`
  - Remove unused `MIN` computation and `Stream.of()` pipeline
  - Remove per-pod estimation (same as CPU fix)
  - Single-pass loop computing both usage max and spike max
2. **Medium term (v4.0-aligned):**
  - **Go** engine: reuse the same **read-once** interval/digest load as CPU; **exact** percentiles via `slices.Sort()` (no t-digest)
  - Lower memory percentile from p100 to p95-p98 where product agrees
  - Adaptive margin via IQR-CV from sorted samples
  - Separate request and limit recommendations
3. **Longer term (requires operator change):**
  - OOM event collection in koku-metrics-operator
  - OOM feedback loop with exponential backoff
  - Trend detection and proactive notifications
  - Multi-timescale merge (short + long **daily digest** windows — storage/weighting, not t-digest merges)

---

## 17. Analysis: GPU Recommendation Algorithm

**Applicability:** Both (local and remote monitoring). The algorithm is internal to Kruize.

### How It Works

The GPU recommendation (`getAcceleratorRequestRecommendation`, `GenericRecommendationModel.java:496-682`) is fundamentally different from CPU and memory. It doesn't recommend continuous values (cores, bytes) — it recommends an **NVIDIA MIG (Multi-Instance GPU) profile**, a pre-defined hardware partition of a physical GPU.

1. **Collect per-interval max values** for GPU core usage (%) and GPU memory usage (%) across all intervals
2. **Compute percentile** of those max values: p60 (cost model) or p98 (performance model) via `CommonUtils.percentile()`
3. **Convert to fractions**: `coreFraction = percentile / 100`, `memoryFraction = percentile / 100`
4. **Clamp to 1.0**: If either fraction > 1.0 (data anomaly), clamp with a log warning — "there is a higher chance that there is an anomaly in data so we mark it as 1 to give out full GPU as a recommendation"
5. **Look up optimal MIG profile**: `AcceleratorMetaDataService.getAcceleratorProfile(model, coreFrac, memFrac)` finds the smallest MIG partition that satisfies both the core and memory fractions
6. **Return the profile name** (e.g., `NVIDIA_GPU_PARTITION_3_CORES_40GB`)

This is a **bin-packing** problem, not a continuous optimization.

### What's Good

- **No per-pod estimation** — works directly with aggregated percentages, which is correct for GPU usage
- **MIG profile selection is sound** — finding the smallest partition that satisfies both core and memory fractions is the right approach
- **No JSONObject overhead** — values collected directly into `ArrayList<Double>`
- **Supported models are comprehensive** — A100 (40/80 GB), H100 (80/94/96 GB), H200 (141 GB), B200 (180 GB), RTX PRO 5000/6000

### Problems


| #   | Problem                                      | Severity               | Details                                                                                                                                                                                                                                                                                                                                                                                                    |
| --- | -------------------------------------------- | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **B200 and RTX PRO gating bug**              | **High (correctness)** | `checkIfModelIsKruizeSupportedMIG` (line 405) checks only for `A100`, `H100`, `H200` in the model name. B200 and RTX PRO models return `false`, so `isGpuWorkload` is never set to `true`, and the entire function returns `null`. No recommendation is produced for B200 or RTX PRO GPUs despite full profile data existing in `getMapWithOptimalProfile`. Silent failure — no error notification.        |
| 2   | `**getFrameBufferBasedOnModel` incomplete**  | High (correctness)     | Handles 40/80/94/96/141 GB models only. Missing: B200 (180 GB), RTX PRO 5000 (48 GB). If a B200 reports frame buffer usage in absolute bytes > 100, conversion fails (`cardFrameBuffer = -1`), the value is silently dropped, and memory usage is underestimated.                                                                                                                                          |
| 3   | **Frame buffer conversion inconsistency**    | Moderate               | Two code paths handle `acceleratorFrameBufferUsage` differently. Path 1 (no `AcceleratorMetricResultHashMap`): converts absolute bytes > 100 to percentage via `value / cardFrameBuffer * 100`. Path 2 (has `AcceleratorMetricResultHashMap`): treats frame buffer as `isMemoryUsage = true` unconditionally and adds directly. Same metric can produce different values depending on which path is taken. |
| 4   | **Same `Collections.sort()` for percentile** | Low                    | Uses `CommonUtils.percentile()` which sorts the entire list. Low impact because GPU container counts are typically small.                                                                                                                                                                                                                                                                                  |
| 5   | **Single GPU per container assumption**      | Moderate (design)      | Comment at line 647: "we are currently considering only one GPU per container." A container with 4 GPUs at 25% utilization each looks identical to one GPU at 25%. Multi-GPU workloads (distributed training with 4-8 GPUs per pod) are increasingly common and would be mischaracterized.                                                                                                                 |
| 6   | **No "remove the GPU" recommendation**       | Moderate (value)       | If GPU core usage is consistently 2-3%, the algorithm recommends the smallest MIG partition. The most impactful recommendation would be "this container doesn't need a GPU — move to CPU." Given that GPUs are the most expensive resource in a cluster (10-50x the cost of equivalent CPU), this is a significant missed opportunity.                                                                     |
| 7   | **No temporal decay**                        | Low                    | All intervals have equal weight. Less impactful than for CPU/memory because GPU workloads tend to be more stable (batch ML training, inference serving).                                                                                                                                                                                                                                                   |


### Recommended Fixes

**Short term (1-2 PRs, high-value bug fixes):**

1. **Fix `checkIfModelIsKruizeSupportedMIG`** — add B200 and RTX PRO model name tokens. This is a one-line fix that unblocks recommendations for newer GPU hardware.
2. **Fix `getFrameBufferBasedOnModel`** — add B200 (180 GB), RTX PRO 5000 (48 GB), RTX PRO 6000 (96 GB).

**Medium term:**

1. **Add GPU underutilization notification** — if both core and memory fractions are below a threshold (e.g., 10%), add `NOTICE_GPU_UNDERUTILIZED` notification suggesting the workload may not need a GPU. This is the highest-value GPU recommendation the system could make.
2. **Unify frame buffer handling** — ensure both code paths produce consistent values for `acceleratorFrameBufferUsage`.

**Longer term:**

1. **Multi-GPU awareness** — collect GPU device count per container (available via DCGM metrics) and adjust fractions accordingly.

### Comparison to CPU/Memory Algorithm Quality


| Aspect                 | CPU                                               | Memory                                    | GPU                                  |
| ---------------------- | ------------------------------------------------- | ----------------------------------------- | ------------------------------------ |
| **Core approach**      | Broken (1-core discontinuity, per-pod estimation) | Broken (sort-for-max, per-pod estimation) | Sound (MIG bin-packing)              |
| **Correctness bugs**   | Design issues, not silent failures                | Design issues, not silent failures        | **Silent failures** for B200/RTX PRO |
| **Performance issues** | Sort of 8,736 elements                            | Sort of 8,736 elements                    | Low (small lists)                    |
| **Biggest gap**        | Algorithm quality                                 | OOM awareness                             | Underutilization detection           |


The GPU algorithm is **architecturally the best designed** of the three but has **the worst correctness bugs** (silent failures for supported hardware). The fixes are straightforward — updating the gating function is a trivial change with high impact.

### Platform Update (March 2026 Triage)

Since this section was written, **Koku `main`** has landed MIG GPU cost support on the on-prem/self-hosted SQL path:

- **New model fields**: `gpu_mode`, `mig_profile`, `mig_slice_count`, `mig_memory_capacity_gb`, `mig_strategy`, `gpu_max_slices` (migration `0344`)
- **New API endpoints**: `reports/openshift/gpu/` and `reports/openshift/gpu/mig_profiles/`
- **MIG-aware cost calculation**: monthly GPU cost uses `slices / gpu_max_slices` weighting; unallocated GPU cost distribution uses slice-hours (`gpu_pod_uptime * COALESCE(mig_slice_count, 1)`)
- **New OCP post-processor**: `masu/util/ocp/ocp_post_processor.py` derives MIG fields from profile strings when the operator doesn't provide them
- **New `PriceList` / `PriceListCostModelMap` models** (schema foundation, not yet wired to API)
- **Operator branch `cost-7178-mig-metrics`** (not yet merged): adds `mig_instance_id`, `mig_profile`, `mig_slice_count`, `gpu_max_slices` to the cost GPU CSV; does NOT touch `ros:` queries

**Impact on this analysis**: The Kruize recommendation algorithm findings (§17.1–17.7) remain valid — they concern Kruize's internal MIG profile selection, not Koku's cost accounting. However, the ros-ocp-backend superpowers GPU requirements (REQ-5.x) should note that Koku now provides MIG cost data that could inform cost-aware GPU recommendations (e.g., "your MIG 1g.5gb slice costs $X/month — switching to CPU would save $Y"). The Trino SQL path has NOT been updated for MIG yet (dual-path gap).

---

## 18. Analysis: JVM/Quarkus Recommendation Algorithm

**Applicability:** Both (local and remote monitoring). The algorithm is internal to Kruize.

**Branch:** `mvp_demo` (development branch). This feature is **not yet on `remote_monitoring`** (stable).

### Overview

The `mvp_demo` branch introduces a **layered runtime recommendation system** for Java (JVM) and Quarkus workloads. This is new code (2026 copyright) that generates environment variable recommendations (`JDK_JAVA_OPTIONS`, `JAVA_OPTIONS`, `quarkus.thread-pool.core-threads`) based on container resource limits, JVM version metadata, and a topological dependency resolver.

### Architecture

The design uses a **strategy pattern with dependency-resolved evaluation order**:

```
LayerRecommendationHandler (interface)
├── HotspotLayerRecommendationHandler — OpenJDK: MaxRAMPercentage + GC policy
├── SemeruLayerRecommendationHandler  — IBM Semeru/OpenJ9: MaxRAMPercentage + GC policy (-Xgcpolicy)
└── QuarkusLayerRecommendationHandler — Quarkus framework: thread pool core threads

TunableDependencyResolver (topological sort)
├── container:memoryLimit ──→ hotspot:MaxRAMPercentage ──→ hotspot:GCPolicy
├── container:cpuLimit    ──→ hotspot:GCPolicy
├── container:cpuLimit    ──→ quarkus:core-threads
└── container:memoryLimit ──→ semeru:MaxRAMPercentage ──→ semeru:GCPolicy

RuntimeRecommendationProcessor (orchestrator)
├── Resolves tunable ordering via TunableDependencyResolver
├── Pre-populates context with CPU/memory recommendations from container layer
├── Invokes each handler in order
└── Formats output as env var key-value pairs

LayerPresenceDetector
├── QueryBasedPresence — PromQL queries to detect JVM/Quarkus containers
└── LabelBasedPresence — Kubernetes pod label matching
```

### What Each Handler Recommends

**1. Hotspot MaxRAMPercentage** (`-XX:MaxRAMPercentage`):

- Container memory <= 512 MB → 50%
- Container memory > 512 MB → 80%

**2. Hotspot GC Policy** (depends on CPU cores, heap size, JDK version):


| Condition               | GC Policy              |
| ----------------------- | ---------------------- |
| <= 1 core               | `-XX:+UseSerialGC`     |
| <= 2 cores, heap < 4 GB | `-XX:+UseParallelGC`   |
| Heap >= 4 GB, JDK 17+   | `-XX:+UseZGC`          |
| Heap >= 4 GB, JDK 11-16 | `-XX:+UseShenandoahGC` |
| Default                 | `-XX:+UseG1GC`         |


**3. Semeru GC Policy** (`-Xgcpolicy:`):


| Condition                | GC Policy             |
| ------------------------ | --------------------- |
| < 2 cores or heap < 4 GB | `-Xgcpolicy:gencon`   |
| >= 2 cores, heap >= 4 GB | `-Xgcpolicy:balanced` |


**4. Semeru MaxRAMPercentage**: Delegates to Hotspot handler (identical logic).

**5. Quarkus thread pool** (`quarkus.thread-pool.core-threads`):

- `ceil(CPU_cores × THREADS_PER_CORE)`, clamped to [1, 100]
- `THREADS_PER_CORE = 1` (constant)

### What's Good

1. **Proper dependency resolution** — `TunableDependencyResolver` uses topological sort so GC policy (which depends on MaxRAMPercentage, which depends on memory limit) computes in the right order. This is the hardest part and it's done correctly.
2. **Strategy pattern** — Each JVM/framework gets its own handler. Adding a new runtime (e.g., GraalVM native image) means one new class + registry entry.
3. **Layer detection via Prometheus** — `QueryBasedPresence` runs PromQL queries (e.g., checking for JVM-specific metrics) to detect whether a container is a Java process. Runtime-agnostic and avoids fragile image-name heuristics.
4. **Output as env vars** — Recommendations formatted as `JDK_JAVA_OPTIONS` / `JAVA_OPTIONS` values is the correct way to configure JVM containers in Kubernetes.
5. **Three runtimes** — Hotspot, Semeru (OpenJ9), and Quarkus. Covers the Red Hat ecosystem.
6. **Integration with container recommendations** — The processor receives CPU/memory recommendations from the container layer and uses them as inputs for JVM tuning (e.g., heap sizing based on recommended memory limit, not current limit).

### Problems


| #   | Problem                                                         | Severity                | Details                                                                                                                                                                                                                                                                                                      |
| --- | --------------------------------------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | **MaxRAMPercentage ignores actual heap usage**                  | **High (quality)**      | Recommendation is based solely on container memory limit (>512 MB → 80%, <=512 MB → 50%). The `filteredResultsMap` parameter (containing actual JVM metrics) is received but unused. A container with 4 GB limit but only 200 MB actual heap usage still gets 80%.                                           |
| 2   | **GC recommendation ignores actual GC metrics**                 | **High (quality)**      | GC policy is chosen based on cores/heap/JDK version only — a reasonable starting heuristic, but `filteredResultsMap` (which may contain `jvm_gc_pause_seconds`) is not consulted. A container with 500ms GC pauses on G1GC won't be told to switch to ZGC.                                                   |
| 3   | **Quarkus THREADS_PER_CORE = 1 is too conservative**            | **High (correctness)**  | Quarkus default is `max(8, 2 × cores)`. A 4-core container gets 4 threads (should be 8). For I/O-bound workloads (typical for Quarkus REST services), 2-4× CPU cores is standard industry practice. This would actively harm Quarkus applications by undersizing their thread pool vs the framework default. |
| 4   | **CPU adjustment defined but dead code**                        | **Low**                 | Constants `RAM_PERCENTAGE_THRESHOLD_BELOW_ONE_CPU_CORE` (10%) and `..._ONE_CPU_CORE` (5%) are defined. The Javadoc says "If CPU cores < 1: subtract 10%". But the actual `generateHotspotMaxRAMPercentageRecommendation` method never reads CPU — only memory.                                               |
| 5   | **Semeru uses `Math.round` vs Hotspot's `Math.ceil` for cores** | **Low (inconsistency)** | 1.3-core container → 2 cores in Hotspot, 1 core in Semeru. Different GC selections for identical containers depending on runtime detection.                                                                                                                                                                  |
| 6   | **No Quarkus queue-size recommendation**                        | **Moderate (gap)**      | Only `core-threads` is recommended. `queue-size` is tightly coupled — recommending one without the other may cause either thread starvation (queue too small) or memory bloat (queue too large).                                                                                                             |
| 7   | **No workload profile awareness**                               | **Moderate (design)**   | Batch processing container with 8 GB heap on JDK 17 gets ZGC (latency-optimized) when ParallelGC (throughput-optimized) would save significant CPU overhead. No latency/throughput classification is attempted.                                                                                              |
| 8   | **Semeru log message says "Hotspot"**                           | **Very Low (typo)**     | `SemeruLayerRecommendationHandler.java:64`: `"Unknown tunable for Hotspot layer"` — should say "Semeru".                                                                                                                                                                                                     |
| 9   | **No GraalVM native image handler**                             | **Low (gap)**           | GraalVM native images don't need JVM heap tuning, but memory limit recommendations should be different (no JVM overhead, RSS is more predictable). Not critical now but relevant as GraalVM adoption grows.                                                                                                  |


### Recommended Improvements

**Short term (high value, low effort):**

1. **Fix `THREADS_PER_CORE`** — Change from 1 to at least 2 (Quarkus default multiplier), or use `max(8, 2 × cores)` to match the Quarkus framework default. This is a one-constant fix that prevents actively harming Quarkus applications.
2. **Fix Semeru rounding inconsistency** — Use `Math.ceil` consistently across all handlers.
3. **Fix the "Hotspot" typo in Semeru handler.**
4. **Implement the dead CPU adjustment** — The constants and Javadoc describe a CPU-based MaxRAMPercentage adjustment. Either implement it or remove the dead constants.

**Medium term (data-driven recommendations):**

1. **Use actual heap usage for MaxRAMPercentage** — If `jvm_memory_used_bytes` or `container_memory_working_set_bytes` is available in `filteredResultsMap`, compute: `recommended = ceil((p95_heap / container_memory_limit) × 100) + safety_margin`. This transforms the recommendation from static heuristic to data-driven.
2. **Use GC pause metrics for GC selection** — If `jvm_gc_pause_seconds_max` is available and consistently exceeds thresholds (e.g., >200ms), recommend lower-latency GC even for small heaps.
3. **Add `quarkus.thread-pool.queue-size`** — Rule of thumb: `2 × core-threads` for balanced workloads. Could be refined with observed request rate metrics if available.

**Longer term:**

1. **Workload profile detection** — If response time metrics are available (Quarkus Micrometer), classify workloads as latency-sensitive vs throughput-oriented and adjust GC selection accordingly.
2. **Connection pool recommendations** — `quarkus.datasource.jdbc.max-size` based on observed connection pool metrics.
3. **GraalVM native image handler** — Different memory model, no heap tuning, but native memory footprint recommendations.

### Comparison to CPU/Memory/GPU Algorithms


| Aspect                   | CPU                           | Memory                    | GPU                        | JVM/Quarkus                               |
| ------------------------ | ----------------------------- | ------------------------- | -------------------------- | ----------------------------------------- |
| **Core approach**        | Broken (1-core discontinuity) | Broken (sort-for-max)     | Sound (MIG bin-packing)    | Sound (heuristic decision tree)           |
| **Uses actual metrics**  | Yes (partially)               | Yes (partially)           | Yes                        | **No** (ignores `filteredResultsMap`)     |
| **Architecture quality** | Poor (per-pod estimation)     | Poor (per-pod estimation) | Good (handler pattern)     | **Best** (strategy + dependency resolver) |
| **Biggest gap**          | Algorithm quality             | OOM awareness             | Underutilization detection | Ignores available metrics                 |
| **Maturity**             | Production                    | Production                | Production (buggy)         | **Development** (mvp_demo only)           |


The JVM/Quarkus system has **the best architecture** of all four recommendation domains — the layered handler pattern with topological dependency resolution is genuinely well-designed. But the actual recommendation logic is entirely **static heuristics** that don't leverage the usage data flowing through the system. The highest-value improvement is connecting the handlers to `filteredResultsMap` metrics.

---

## 19. Additional Kruize Optimizations (Deep Audit) (Legacy)

**Applicability:** Mixed — indicated per finding. These findings complement §13 (code-level) and §14 (DB/API) from the initial audit and were discovered during a second-pass deep dive.

### 19.1 Critical: `errorReasons` Not Cleared Between Bulk Rows — `Remote only`

`ExperimentInitiator.validateAndAddExperimentResults` (~line 139) allocates `List<String> errorReasons` once before the loop over `UpdateResultsAPIObject` entries but **never clears it** at the start of each iteration. Failures accumulate across rows — row 5's error message includes rows 1–4's errors. This is both a **correctness bug** (wrong error attribution) and a **performance issue** (O(k²) string churn for k failing rows in a batch).

**Fix**: `errorReasons.clear()` at the top of each loop iteration. One line.

### 19.2 High: Unbounded In-Memory Interval Map Growth — `Both`

`ContainerData.results` (`HashMap<Timestamp, IntervalResults>`) grows without limit as new results are added via `/updateResults` (remote) or Prometheus queries (local). There is **no eviction, TTL, or cap**. A long-lived experiment accumulates unbounded `HashMap` entries — at 4 intervals/hour × 24h × 91 days = 8,736 entries per container, each containing full `MetricResults` objects with boxed `Double` values.

For 20M containers at 91 days, this is ~175 billion `Map.Entry` objects if all held in memory simultaneously.

**Fix**: Evict intervals older than the longest term's monitoring window. Or switch to a DB-backed cursor model (load intervals on demand, not all at once).

### 19.3 High: Static Experiment Map Holds Full Object Graphs — `Both`

`KruizeOperator.autotuneObjectMap` is a process-wide `ConcurrentHashMap<String, KruizeObject>`. Entries are only removed on K8s delete events (local mode) or explicit API delete (remote mode). There is **no TTL or size bound**. In API-only / ROS deployments, heap grows linearly with experiment count and embedded results/recommendations, with no upper limit.

**Fix**: Add a configurable max-size with LRU eviction, or a TTL-based expiry for idle experiments.

### 19.4 High: Cross-Model Duplicate Work (Cost + Performance) — `Both`

For each term, the recommendation engine calls `generateRecommendationBasedOnModel` once per model (cost and performance). Each call independently:

1. Rebuilds `filteredResultsMap` via stream filter + business hours filter
2. Calls `getNumPods` (scans all intervals)
3. Calls `getCPURequestRecommendation` (builds CPU usage list, sorts, picks percentile)
4. Calls `getMemoryRequestRecommendation` (builds memory usage list, sorts, picks percentile)
5. Calls `getAcceleratorRequestRecommendation` (builds GPU usage lists, sorts, picks percentile)

Steps 1–2 produce **identical results** for both models — the only difference is the percentile target in step 3–5. The filtered map, pod count, and raw metric lists could be computed once per term and reused.

**Impact**: Doubles the recommendation computation time. At 20M containers × 3 terms × 8,736 intervals, this is ~105 billion unnecessary `Map.Entry` iterations and ~60M redundant `ArrayList` constructions per cycle.

**Fix**: Compute `filteredResultsMap`, pod count, and raw metric lists once per term. Pass both percentile targets to a unified recommendation method that picks values from the same sorted list. One PR.

### 19.5 High: `mergeResults` Flattens and Overwrites Interval Data — `Remote only`

`ExperimentInterfaceImpl.mergeResults` (~lines 141–163) receives new results and merges into existing container data. However:

1. All `MetricResults` from **all timestamps** in `newResults.values()` are flattened into a single `metricResultsHashMap` — interval structure is lost if multiple timestamps are present
2. `existingResults.put(endTime, newInterval)` **overwrites** any prior entry for the same endTime without deep-merging metrics
3. Accelerator data in `IntervalResults` is not merged — only `metricResultsMap` is populated

**Impact**: Data loss risk when batch updates contain multiple intervals or when accelerator metrics are included.

**Fix**: Merge per-timestamp instead of flattening. Preserve accelerator data during merge. One PR.

### 19.6 High: All-at-Once DB Load for Recommendations — `Both`

`ExperimentDBService.loadAllResults` (~lines 119–141) loads **all result rows** into memory, converts them to a list, then merges into `KruizeOperator.autotuneObjectMap`. `loadResultsFromDBByName` loads by experiment name and time range but still materializes full lists. **No streaming or cursor-based loading.**

Additionally, `loadResultsFromDBByName` is triggered per experiment during `UpdateResults` validation when the experiment is not yet in the map — potential **N+1 DB pattern** for batched updates with many distinct experiments.

**Fix**: Use Hibernate `ScrollableResults` or cursor-based pagination. Cache loaded experiments. Two PRs.

### 19.7 High: `addExperimentToDB` Serializes All Inserts — `Both`

`ExperimentDAOImpl.addExperimentToDB` (~line 63) is declared `synchronized`, serializing **all experiment inserts** through a single monitor. Under concurrent creates (multiple API calls or multiple Kafka consumers), this is a throughput bottleneck.

**Fix**: Remove the `synchronized` keyword. Use optimistic concurrency or database constraints for dedup. One PR.

### 19.8 Medium: `getTimestampWithinTolerance` Linear Scan — `Both`

`Terms.checkIfMinDataAvailableForTerm` (~~lines 108–154) steps backward in time and, for each step, calls `getTimestampWithinTolerance(currentTimestamp, containerData.getResults().keySet(), tolerance)` (~~lines 242–249). This performs a **linear scan** over all timestamps for every probe — O(steps × |timestamps|).

For 91-day terms with 15-minute intervals: ~8,736 timestamps × ~8,736 probes = ~76M comparisons per container. A `NavigableMap` with `floorKey`/`ceilingKey` would reduce this to O(steps × log(timestamps)) — ~8,736 × 13 ≈ 114K comparisons.

**Fix**: Change `containerData.results` from `HashMap` to `TreeMap<Timestamp, IntervalResults>`. Use `floorKey`/`ceilingKey` for tolerance checks. This also benefits the range-filter in §19.4 (use `subMap` instead of stream filter). One PR.

### 19.9 Medium: `deploymentMap` Is Plain `HashMap` — `Both`

`KruizeOperator.java` line 91: `deploymentMap` is a plain `HashMap` while `autotuneObjectMap` (line 82) is a `ConcurrentHashMap`. If both are accessed from different threads (K8s watchers, servlets, workers), `deploymentMap` is a data race.

**Fix**: Change to `ConcurrentHashMap`. One line.

### 19.10 Medium: Micrometer Timer Registered in `finally` on Every DAO Call — `Both`

`ExperimentDAOImpl` (~lines 92–93, 262, 410) does `MetricsConfig.timerBAddExpDB.tag("status", statusValue).register(MetricsConfig.meterRegistry())` in every `finally` block. Micrometer deduplicates by ID, but the repeated `tag()` + `register()` creates unnecessary intermediate objects and registry lookups per DAO call.

**Fix**: Pre-register timers at startup. Use `Timer.Sample.stop(preRegisteredTimer)` in finally blocks. One PR.

### 19.11 Medium: Notification Metrics: Expensive String Alloc Per Event — `Both`

`KruizeNotificationCollectionRegistry` (~lines 83–91) builds `("|"+level+"|").contains("|" + type + "|")` strings and does `meterRegistry.find(...).tags(...).counter()` per notification. Under high cardinality (term/model tags), this inflates meter count and CPU.

**Fix**: Pre-parse notification levels into a `Set<String>`. Pre-register counters with bounded tag cardinality. One PR.

### 19.12 Medium: `DBHelpers.setRecommendationsToKruizeObject` Nested Matching — `Both`

`DBHelpers.java` (~lines 165–291): For each API recommendation object, inner loops walk **all** `kruizeObject.getKubernetes_objects()` and match by name — O(recommendation_rows × k8s_objects). For large experiments with many workloads, this dominates CPU.

**Fix**: Build a `Map<String, K8sObject>` index before the loop. One line + loop restructure.

### 19.13 Medium: Namespace/Container Recommendation Code Duplication — `Both`

`generateRecommendationBasedOnModel` (~~lines 781–893) and `generateNamespaceRecommendationBasedOnModel` (~~lines 1138–1245) are near-identical ~200-line methods. Namespace skips accelerators but otherwise duplicates: threshold handling, filtered map construction, current config extraction, `populateRecommendation` wiring, and cost/performance notification branching.

**Fix**: Extract shared logic into a common method parameterized by data type. Reduces maintenance risk and duplicate object allocation. One PR.

### 19.14 Medium: `PlotManager.generatePlots` Int Overflow — `Both`

`PlotManager.java` (~lines 57–59): `calendar.add(Calendar.MILLISECOND, (int) millisecondsToAdd)` can **overflow `int`** for large `plots_datapoints_delta_in_days` or many iterations, producing wrong timestamps and broken plot data.

**Fix**: Use `calendar.add(Calendar.DAY_OF_MONTH, days)` for day-granularity deltas, or split large millisecond values into hours + remainder. One line.

### 19.15 Medium: O(n²) String Concatenation in Error Building — `Remote only`

`ExperimentInitiator.getErrorMap` (~lines 66–84): `errorMsg = errorMsg + " , " + errorText` is O(n²) in total characters when many errors accumulate.

**Fix**: Use `StringBuilder` or `String.join`. One PR.

### Summary: Additional Kruize Priority


| #     | Issue                             | Mode   | Severity | Effort                      |
| ----- | --------------------------------- | ------ | -------- | --------------------------- |
| 19.1  | errorReasons not cleared          | Remote | Critical | Very low (1 line)           |
| 19.2  | Unbounded interval map            | Both   | High     | Moderate (eviction policy)  |
| 19.3  | Static experiment map unbounded   | Both   | High     | Moderate (LRU/TTL)          |
| 19.4  | Cross-model duplicate work        | Both   | High     | Low (1 PR, refactor)        |
| 19.5  | mergeResults flattens/overwrites  | Remote | High     | Low (1 PR)                  |
| 19.6  | All-at-once DB load               | Both   | High     | Moderate (2 PRs)            |
| 19.7  | addExperimentToDB synchronized    | Both   | High     | Very low (1 line)           |
| 19.8  | getTimestampWithinTolerance O(n²) | Both   | Medium   | Low (1 PR, HashMap→TreeMap) |
| 19.9  | deploymentMap not concurrent      | Both   | Medium   | Very low (1 line)           |
| 19.10 | Micrometer timer per DAO call     | Both   | Medium   | Low (1 PR)                  |
| 19.11 | Notification metrics overhead     | Both   | Medium   | Low (1 PR)                  |
| 19.12 | DBHelpers nested matching         | Both   | Medium   | Low (1 PR)                  |
| 19.13 | Namespace/container duplication   | Both   | Medium   | Low (1 PR)                  |
| 19.14 | PlotManager int overflow          | Both   | Medium   | Very low (1 line)           |
| 19.15 | String concatenation O(n²)        | Remote | Medium   | Very low (1 PR)             |


### Platform Update (March 2026 Triage)

**Kruize autotune `mvp_demo`** has been triaged as of March 2026. Recent commits are mostly dependency bumps (EvalEx library upgrade, UBI minimal version bump, mchange CVE fix), CI/tooling (datasource tests, Dockerfiles, stale bot), and demo changes (OpenJ9 → Semeru switch). One notable change: **KRUIZE-1023** added a **PerformanceProfileCache** (`ConcurrentHashMap`) to avoid repeated DB reads for performance profiles — this partially addresses §19.6 (all-at-once DB load) for the performance profile path specifically. The EvalEx library upgrade (`com.udojava.evalex` → `com.ezylang.evalex`) changes expression evaluation precision/rounding, which affects objective function evaluation but not recommendation algorithms. **No changes to CPU, memory, GPU, or JVM recommendation algorithms** — all §12, §16, §17, §18 findings remain valid. All §19.1–19.15 code-level issues remain present except the partial mitigation of §19.6 via the performance profile cache.

---

## 20. Additional ros-ocp-backend Optimizations (Deep Audit)

**Applicability:** All `Remote only` — ros-ocp-backend does not exist in the local monitoring architecture.

These findings complement §15 from the initial audit.

### 20.1 Critical: RBAC Middleware Nil Pointer Panics — `Remote only`

`internal/api/middleware/rbac.go` (~lines 85–111) has multiple crash paths:

1. If `http.NewRequest` fails, `req` is nil but `req.Header.Set` executes → **panic**
2. If `client.Do` fails, `res` is nil but `defer res.Body.Close()` executes → **panic**
3. `io.ReadAll` error is silently discarded (`body, _ := ...`)
4. `http.Client{}` has **no timeout** — requests to the RBAC service can hang indefinitely

This crashes every API request when the RBAC service is unreachable, taking down the entire API.

**Fix**: Check nil before use. Use `&http.Client{Timeout: 10 * time.Second}`. Propagate read errors. One PR.

### 20.2 High: API List Handlers Return HTTP 200 on DB Failure — `Remote only`

`internal/api/handlers.go` (~lines 42–46, 161–168): `GetRecommendationSets` and `GetNamespaceRecommendationSetList` log DB errors but **do not return an error response**. The handler continues to build a response and returns **HTTP 200 with empty/partial data**. Consumers cannot distinguish "no recommendations" from "database failure".

**Fix**: Return HTTP 500 with error details on DB failure. One PR.

### 20.3 High: Kafka Consumer Type Assertion Panic — `Remote only`

`internal/kafka/consumer.go` (~lines 93–97): `err.(kafka.Error)` assumes the error is always a `kafka.Error`. Any other error type causes a **panic** via failed type assertion. Should use comma-ok idiom: `if ke, ok := err.(kafka.Error); ok { ... }`.

**Fix**: Use safe type assertion. One line.

### 20.4 High: Kafka Consumer Continues After Subscribe Failure — `Remote only`

`internal/kafka/consumer.go` (~lines 75–78): On `Subscribe` failure, the code logs but continues into `ReadMessage`, leaving the consumer in an **undefined subscription state**. Wasted CPU and misleading error logs.

**Fix**: Return error or fatal-log on subscribe failure. One line.

### 20.5 Medium: Full Kafka Payload Logged on Every Message — `Remote only`

`internal/kafka/consumer.go` (~line 91): `log.Infof("... %s", string(msg.Value))` runs unconditionally for **every** successful message. This allocates a full string copy of the payload (which can be large for CSV metadata) and generates high log volume in production.

**Fix**: Move to Debug level or truncate to first N characters. One line.

### 20.6 Medium: `Setup_kruize_performance_profile` Nil Panic — `Remote only`

`internal/utils/utils.go` (~lines 25–68): Uses `defer res.Body.Close()` after `http.Post`/`http.Get` calls. If the HTTP call fails, `res` is nil and the deferred close **panics**. Same class of bug as §20.1.

**Fix**: Check `res != nil` before deferring close. One PR.

### 20.7 Medium: `ReadCSVFromUrl` Has No HTTP Timeout — `Remote only`

`internal/utils/utils.go` (~lines 72–87): Uses `http.Get` (default client, no timeout) to download CSV files. A slow or stuck URL blocks the processor thread indefinitely.

**Fix**: Use `&http.Client{Timeout: 30 * time.Second}`. One line. (Note: §15.3 covers Kruize API timeouts; this is for CSV download, a separate code path.)

### 20.8 Medium: `ConvertDateToISO8601` Silently Ignores Parse Errors — `Remote only`

`internal/utils/utils.go` (~lines 122–125): `t, _ := time.Parse(...)` discards parse errors. Invalid dates become wrong ISO timestamps without surfacing errors, causing silent bad data downstream.

**Fix**: Return error and propagate. One line.

### 20.9 Medium: Recommendation Poller Infinite Redelivery on DB Error — `Remote only`

`internal/services/recommendation_poller.go` (~lines 345–350): On DB error loading the "first recommendation", the handler **returns without committing** the Kafka offset. This causes infinite redelivery of the same message — a poison-message scenario that blocks the consumer forever.

**Fix**: Commit offset on non-retryable errors, or implement a dead-letter queue. One PR.

### 20.10 Medium: SendMessage Failure Doesn't Prevent DB Writes — `Remote only`

`internal/services/report_processor.go` (~lines 260–265, 383–387): If `SendMessage` to the recommendation topic fails, only an error is logged. Upstream DB state (workload, metrics) is already written, so recommendations may **never be requested** — a data consistency gap with no compensating action.

**Fix**: Wrap DB write + Kafka produce in a transaction-like pattern (write DB, produce, if produce fails, mark workload for retry). One PR.

### 20.11 Medium: Housekeeper `sourcesCleaner` GORM Where Bug — `Remote only`

`internal/services/housekeeper/sourcesCleaner.go` (~line 52): Same `.Where()` return value not assigned pattern as §15.4. Cleanup may target the wrong or zero cluster.

**Fix**: `query = query.Where(...)`. One line.

### 20.12 Medium: RBAC `strings.Split` Panic on Malformed Permission — `Remote only`

`internal/api/middleware/rbac.go` (~line 42): `strings.Split(acl.Permission, ":")[1]` panics if the permission string has **no `:`**. Unexpected RBAC shapes from the platform service crash the API.

**Fix**: Check `len(parts) >= 2` before indexing. One line.

### 20.13 Medium: Non-Deterministic Map Iteration in Aggregation and CSV Export — `Remote only`

Two instances:

1. `internal/utils/aggregator.go` (~lines 110–117): Go map iteration for `columnsToAggregate` yields non-deterministic column order across runs
2. `internal/api/utils.go` (~lines 973–981): `recommendationTermMap` iteration produces inconsistent CSV export row order

**Impact**: Hurts reproducibility, complicates testing and diffing outputs.

**Fix**: Sort keys before iteration or use ordered collections. One PR.

### Summary: Additional ros-ocp-backend Priority


| #     | Issue                                            | Severity | Effort            |
| ----- | ------------------------------------------------ | -------- | ----------------- |
| 20.1  | RBAC middleware nil panics                       | Critical | Low (1 PR)        |
| 20.2  | API returns 200 on DB failure                    | High     | Low (1 PR)        |
| 20.3  | Kafka consumer type assertion panic              | High     | Very low (1 line) |
| 20.4  | Kafka consumer continues after subscribe failure | High     | Very low (1 line) |
| 20.5  | Full payload logged per message                  | Medium   | Very low (1 line) |
| 20.6  | Setup profile nil panic                          | Medium   | Low (1 PR)        |
| 20.7  | ReadCSVFromUrl no timeout                        | Medium   | Very low (1 line) |
| 20.8  | ConvertDateToISO8601 silent error                | Medium   | Very low (1 line) |
| 20.9  | Poller infinite redelivery                       | Medium   | Low (1 PR)        |
| 20.10 | SendMessage failure consistency gap              | Medium   | Low (1 PR)        |
| 20.11 | Housekeeper GORM Where bug                       | Medium   | Very low (1 line) |
| 20.12 | RBAC Split panic                                 | Medium   | Very low (1 line) |
| 20.13 | Non-deterministic map iteration                  | Medium   | Very low (1 PR)   |


### Platform Update (March 2026 Triage)

**ros-ocp-backend `main`** has been triaged as of March 2026. Major recent work is **namespace recommendations** (list/detail APIs, RBAC, Unleash gating, Kruize 0.8.2 bump). The namespace feature adds new RBAC query paths, new API handlers, and a new `RecommendationKafkaMsg` type with `ExperimentType PayloadType` — all consistent with the patterns analyzed here. **All §20.1–20.13 bugs remain present** in the codebase:

- §20.1 RBAC nil panics: Still present in `internal/api/middleware/rbac.go`
- §20.3 Kafka type assertion panic: Still `err.(kafka.Error)` without comma-ok idiom in `internal/kafka/consumer.go`
- §20.5 Full payload logging: Still unconditional `log.Infof` with full message value
- §20.9 Poller infinite redelivery: Still returns without committing offset on DB error

Additional observations from triage:

- **JSONB recommendation storage** (`gorm.io/datatypes.JSON`) is still used for both container and namespace recommendation sets
- `**historical_namespace_recommendation_sets`** is still an **unpartitioned table** (§15.12 finding confirmed)
- Kruize version bumped to **0.8.2** (no algorithm changes, just container image tag)
- Query optimization PR (`be2ac47`) improved recommendation listing performance but does not address architectural issues

---

## 21. Rejected Alternative: TSDB Block Shipping

### Proposal

Have the koku-metrics-operator grab TSDB blocks from Prometheus and ship them to the backend instead of querying and writing CSV.

### Why It Was Rejected


| Concern                      | Assessment                                                                                                                                                              |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Bandwidth**                | TSDB blocks contain raw 30-second samples. For 500 containers: ~10 MB/hr vs ~400 KB/hr for CSV. **25x more data leaving the cluster.**                                  |
| **Operator complexity**      | Requires importing Go `tsdb` library, reading blocks, filtering to ROS-relevant metrics, rewriting filtered blocks. Much more complex than HTTP + CSV.                  |
| **Prometheus access**        | OpenShift Prometheus doesn't expose filesystem access. The operator only has the HTTP API. `--web.enable-admin-api` (for snapshots) is typically disabled for security. |
| **CPU cost on cluster**      | Block read + filter + rewrite is CPU and I/O intensive, happening on the customer's cluster where the operator should be lightweight.                                   |
| **Resolution**               | Raw 30-second samples provide 30-120x more data than the 15-minute aggregates Kruize needs. The extra resolution doesn't improve recommendations.                       |
| **Compared to CSV → Thanos** | Saves 3-4 serialization hops but costs 25x bandwidth, much more operator complexity, and produces data Kruize doesn't need at that resolution.                          |


The pre-aggregation the operator performs today is **useful compression** that matches what the downstream consumer (Kruize) needs. Bypassing it trades bandwidth and complexity for marginal benefit.

---

## 22. Findings and Trade-offs

### Performance Ranking (best to worst)

1. **Kruize on Cluster** — Eliminates the metrics pipeline entirely. Distributed computation. No central bottleneck. Scales trivially to millions of containers. Best data fidelity (raw Prometheus, full resolution).
2. **CSV → Thanos + Integer Types + Approx Percentiles + Algorithm Fixes (CPU + Memory) + Code-Level Fixes** — Eliminates all major bottlenecks (HTTP calls, JSONB, exact-sort percentile, suboptimal recommendation logic, per-row transactions, Gson overhead). Recommendation computation drops from hours to minutes at 20M scale. Better recommendations via temporal decay, adaptive margins, OOM feedback, trend detection, and separate request/limit values.
3. **CSV → Thanos** (alone) — Eliminates the two largest pipeline bottlenecks (HTTP call overhead and JSONB read). Moderate architectural change.
4. **Current + All Code-Level Fixes + Algorithm Fixes (CPU Option A + Memory short-term)** — Low-effort improvements that produce ~2-5x ingestion throughput and better recommendations immediately, with no infrastructure changes. ~10+15+14+13 = ~52 mechanical PRs + 2 algorithm PRs. Fixes 2 critical crash bugs (§19.1, §20.1), 10+ high-severity issues, and numerous correctness problems.
5. **Current + Typed Columns** — Incremental improvement to the read path (~3-5x). No architectural change. Doesn't address the ingestion bottleneck.
6. **Current (JSONB)** — Baseline. Hits scaling walls at high container counts with long-term retention. Suboptimal recommendations due to algorithm issues. Per-row transactions, Gson churn, and broken synchronization compound the pipeline bottlenecks.

### Trade-off Summary


|                                            | Component          | Kruize Mode                 | Perf Improvement (20M, 91d)                       | Rec Quality                                | Effort                                            |
| ------------------------------------------ | ------------------ | --------------------------- | ------------------------------------------------- | ------------------------------------------ | ------------------------------------------------- |
| **Kruize code-level fixes (§13)**          | Kruize             | Both (7/10)                 | ~2-5x (ingestion)                                 | None                                       | Low (~10 PRs)                                     |
| **Kruize DB/API fixes (§14)**              | Kruize             | Both                        | ~2-10x (API reads)                                | None                                       | Low-Moderate (~11 PRs)                            |
| **ros-ocp-backend fixes (§15)**            | ros-ocp-backend    | Remote                      | ~2-5x (processing)                                | None                                       | Low-Moderate (~14 PRs)                            |
| **CPU algorithm fix (Option A, §12)**      | Kruize             | Both                        | Minimal                                           | Significant                                | Very low (1 PR)                                   |
| **Memory algorithm fix (§16)**             | Kruize + Operator  | Both                        | ~2-5x (memory rec)                                | Significant                                | Low-Moderate (2-3 PRs)                            |
| **GPU algorithm bug fixes (§17)**          | Kruize             | Both                        | N/A (correctness)                                 | Enables B200/RTX PRO recs                  | Very low (1-2 PRs)                                |
| **JVM/Quarkus algorithm fixes (§18)**      | Kruize             | Both (mvp_demo)             | Minimal                                           | Data-driven JVM tuning                     | Low-Moderate (3-5 PRs)                            |
| **Additional Kruize fixes (§19)**          | Kruize             | Both (12/15), Remote (3/15) | ~2x (rec throughput)                              | Correctness (crash + data loss)            | Low-Moderate (~15 PRs)                            |
| **Additional ros-ocp-backend fixes (§20)** | ros-ocp-backend    | Remote                      | ~1.5x (reliability)                               | Correctness (crash + silent failure)       | Low (~13 PRs)                                     |
| **Idle detection (§23.1)**                 | Kruize             | Both                        | N/A (new savings)                                 | New category: eliminate waste              | Very low (0 new queries)                          |
| **PVC right-sizing (§23.2)**               | Kruize + Operator  | Both                        | N/A (new savings)                                 | New category: storage savings              | Low (0-1 new queries)                             |
| **HPA optimization (§23.3)**               | Kruize + Operator  | Both                        | N/A (new savings)                                 | New category: scaling efficiency           | Moderate-High (8 queries)                         |
| **Go GOMAXPROCS (§23.4)**                  | Kruize + Operator  | Both                        | N/A (new savings)                                 | New category: runtime perf                 | Low (1 new query)                                 |
| **Replica count for impact (§26)**         | Operator + ros-ocp | Both                        | N/A (UX)                                          | Enables total_savings per workload         | Very low (3-5 PRs, 2-4 queries)                   |
| **JSONB → relational columns (§27)**       | ros-ocp-backend    | Remote                      | ~10-20x rec storage, eliminates marshal/unmarshal | Type safety, indexable, native aggregation | Low (DB migration + model change)                 |
| **TimescaleDB instead of Thanos (§28)**    | Pipeline           | Remote                      | Same throughput, 3-4 fewer services               | §28 **alternative** only: `COPY FROM`, simpler ops if TimescaleDB were chosen — **v4.0** uses plain PostgreSQL 16+ + exact Go percentiles, **no TimescaleDB, no t-digest** | Lower than Thanos (~1-2 weeks saved)              |
| **In-database recs (§29)**                 | ros-ocp-backend DB | Remote                      | *(§29 superseded)* hypothetical ~4-8x for SQL-in-DB vs old Go loop | Same                                       | **v4.0:** native Go (`recommendCPU()`, `recommendMemory()`, `recommendAllWorkloads()`, …); §29 PL/pgSQL + shadow path **not used** |
| **VM recommendations (§30)**               | Operator + ros-ocp | Both                        | N/A (new category)                                | VM right-sizing: vCPU, memory, disk, IOPS  | Low-Moderate (~12 queries, 8-10 PRs)              |
| **Typed columns**                          | Kruize DB          | Remote                      | ~3-5x (read only)                                 | None                                       | Low                                               |
| **CSV → Thanos**                           | Pipeline           | Remote                      | ~5-15x                                            | None                                       | Moderate                                          |
| **+ Integer types**                        | Operator           | Remote                      | +~2-3x storage                                    | None                                       | Low (incr.)                                       |
| **+ Approx percentiles**                   | Kruize/ros-ocp     | Both                        | +~5-15x rec step                                  | None                                       | Moderate                                          |
| **+ CPU Algorithm (Option C)**             | Kruize/ros-ocp     | Both                        | +~2-5x rec step                                   | Significant                                | Moderate                                          |
| **+ Memory Algorithm (full)**              | Kruize + Operator  | Both                        | +~2-5x rec step                                   | Significant                                | Moderate                                          |
| **= Combined (all)**                       | All                | Remote + Both               | **~100-500x**                                     | **Significant**                            | Moderate                                          |
| **Kruize on cluster**                      | All                | Local                       | ~50-100x                                          | Best (raw data)                            | Significant                                       |


**Mode context:** "Typed columns" and "CSV → Thanos" address the remote monitoring data pipeline (operator → ros-ocp-backend → Kruize). They don't apply to local monitoring where Kruize queries Prometheus directly. "Approximate percentiles" applies to legacy Java/Kruize or optional Thanos-side sketches; **v4.0** remote ros-ocp-backend uses **exact** percentiles in Go (`slices.Sort()`), not t-digest. "CPU algorithm fixes", "Memory algorithm fixes", "GPU algorithm bug fixes", and "JVM/Quarkus algorithm fixes" improve recommendation computation where Kruize remains in the path; **v4.0** container CPU/memory math is native Go in ros-ocp-backend. "Kruize on cluster" is the local monitoring architecture itself. ros-ocp-backend fixes are relevant only in remote monitoring (ros-ocp-backend doesn't exist in the local monitoring architecture). OOM event collection requires an operator change in remote mode; in local mode, Kruize can query `kube_pod_container_status_last_terminated_reason` directly from Prometheus. JVM/Quarkus fixes are on the `mvp_demo` branch (development); layer detection via Prometheus queries works in both local and remote modes.

### Recommended Path

For the Custom Timeframes feature (COST-5691) with 91-day terms:

1. **Code-level fixes across all components** as immediate wins, parallelizable across teams:
  - **Kruize code-level (§13)**: ~10 mechanical PRs — per-row transaction batching, Gson reuse, HTTP client pooling, sync fixes, etc.
  - **Kruize DB/API (§14)**: ~11 PRs — composite indexes, filtered loads, clone elimination, projection queries, etc.
  - **ros-ocp-backend (§15)**: ~14 PRs — HTTP timeouts, GORM bug fix, Kafka producer batching, DB indexes, worker pool, etc.
  - **CPU algorithm fix (Option A, §12)**: 1 PR — remove 1-core discontinuity, add safety margin.
  - **Memory algorithm fix (short-term, §16)**: 1 PR — replace sort with `Collections.max()`, remove JSONObject overhead, remove per-pod estimation, single-pass loop.
  - **GPU algorithm bug fixes (§17)**: 1 PR — fix `checkIfModelIsKruizeSupportedMIG` to include B200/RTX PRO, fix `getFrameBufferBasedOnModel` gaps. Currently B200 and RTX PRO GPUs silently produce no recommendations.
  - **JVM/Quarkus algorithm fixes (§18, mvp_demo)**: 2-3 PRs — fix `THREADS_PER_CORE` from 1 to `max(8, 2×cores)`, fix Semeru rounding inconsistency, use actual heap usage data for MaxRAMPercentage. These are on the `mvp_demo` development branch.
  - **Additional Kruize fixes (§19)**: ~15 PRs — fix `errorReasons` accumulation (critical, 1 line), add interval map eviction, fix cross-model duplicate work, fix `mergeResults` data loss, switch to `TreeMap` for O(log n) lookups, remove synchronized bottleneck, etc.
  - **Additional ros-ocp-backend fixes (§20)**: ~13 PRs — fix RBAC nil panics (critical, 1 PR), return HTTP 500 on DB failure, fix Kafka type assertion panics, add HTTP timeouts, fix poison-message infinite redelivery, etc.
   These are all independent, require no infrastructure changes, and deliver ~2-5x improvement across the pipeline plus better recommendation quality. The GPU fix is a correctness issue — currently-supported hardware silently fails. The JVM/Quarkus `THREADS_PER_CORE=1` fix is also a correctness issue — it actively undersizes Quarkus thread pools vs the framework default. The §19 `errorReasons` bug silently corrupts error reporting in bulk updates. The §20 RBAC nil panic crashes the entire API when the RBAC service is unreachable. The code-level fixes remain valuable regardless of which architectural path is chosen.
2. **CSV → Thanos** as the architectural foundation. This is the highest-impact infrastructure change. It addresses both major pipeline bottlenecks (HTTP call overhead and JSONB) and enables the subsequent optimizations.
3. **Integer types** at the same time as the Thanos migration. Low incremental cost, cleaner pipeline, better compression. The operator is the only component that needs to change for this — everything downstream inherits the benefit.
4. **Percentiles + algorithm upgrades (CPU Option C + Memory full)** as a follow-up. **v4.0 remote path:** partitioned **daily digest** tables in PostgreSQL; `recommendCPU()` / `recommendMemory()` load data once, apply decay weighting, **exact** percentiles via `slices.Sort()` in Go, plus margins, trend, and idle logic — **no** TimescaleDB continuous aggregates, **no** t-digest. Optional patterns for stacks that still use Thanos/Kruize Java:
  - **Simple (ingest / Thanos path)**: Use Thanos `quantile_over_time` via PromQL with `ros_cpu_derived_max` written at ingest time. No Kruize change beyond PromQL queries. Still improves over exact sort in the Java engine.
  - **Advanced (v4.0-aligned)**: Same daily digest storage; ros-ocp-backend Go merges windows and applies exponential time-weighting in application code (read once, compute N terms). Custom timeframes are digest-window merges **in Go**, not SQL continuous aggregates or t-digest rollups.
  - **Memory-specific**: Lower memory percentile from p100 to p95-p98 (enabled by OOM feedback as safety net). Adaptive margins via IQR-CV. Separate request/limit recommendations. Trend detection for proactive leak warnings.
5. **OOM event collection in operator** as a prerequisite for the full memory algorithm. Add `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` to the operator's Prometheus queries and include OOM signal in the CSV payload. This enables the memory limit recommendation and exponential OOM backoff.
6. **New recommendation types** (§23) phased in parallel with the above:
  - **Phase 0 (zero new queries)**: Idle workload detection (§23.1) + QoS class recommendations (§23.5). These use only existing data and are pure algorithm additions. Idle detection alone addresses the most common industry gap.
  - **Phase 1 (1 new query + route existing)**: PVC/storage right-sizing (§23.2, reuse existing `cost:` data) + Go GOMAXPROCS/GOMEMLIMIT (§23.4, 1 new query). High impact for OpenShift clusters where Go workloads are ubiquitous.
  - **Phase 2 (8 new queries)**: HPA optimization (§23.3). The largest competitive gap — StormForge's core value proposition. Requires the most operator and algorithm work.
  - **Phase 3 (remaining)**: Node.js (§23.7), ephemeral storage (§23.6), ResourceQuota (§23.8), Python (§23.9), .NET (§23.10).
7. **Kruize on cluster** as a long-term target if organizationally feasible. Eliminates the metrics pipeline entirely and makes all the above infrastructure optimizations unnecessary. The algorithm improvements (CPU, memory, GPU, JVM/Quarkus) and new recommendation types still apply to the on-cluster Kruize instance. In local monitoring mode, Kruize has direct access to Prometheus and can query OOM events, JVM metrics, HPA status, and Go runtime metrics natively — enabling all §23 recommendation types without operator changes.

### Key Questions

- **CSV → Thanos**: Will the Kruize team implement a Thanos pull-based data source in `remote_monitoring` mode?
- **Approximate vs exact percentiles (legacy / Thanos–Kruize paths)**: For stacks still on Kruize Java, is PromQL `quantile_over_time` sufficient, or is a streaming sketch worth the complexity? **v4.0 ros-ocp-backend** uses **exact** percentiles in Go (`slices.Sort()`), not t-digest.
- **Kruize on cluster**: Is it acceptable to run Kruize (a Java application, ~256-512 MB) on customer clusters?
- **Algorithm fix (Option A)**: Is the Kruize team open to removing the 1-core discontinuity and per-pod estimation? These produce provably worse recommendations compared to industry standard approaches.
- **VPA alignment**: Should the cost/performance percentile targets be revised? VPA uses p90 for both, while Kruize uses p60 (cost) and p98 (performance). The choice of targets is a product decision, but the wide gap between p60 and p98 may produce confusing UX.
- **Safety margin**: Is 15% (VPA default) appropriate for the ROS context, or should it be configurable per cost/performance model?
- **Confidence bounds**: Should recommendations include lower/upper bounds (like VPA) in addition to the target value? This provides users with actionable context ("you could go as low as X or as high as Y").
- **Code-level fixes**: Can the Kruize team accept upstream PRs for the mechanical optimizations in §13? Most are independent, low-risk, and don't change behavior — only performance and correctness of synchronization.
- **Memory percentile**: Is lowering memory from p100 to p95-p98 acceptable if OOM feedback provides the safety net? Both cost and performance models currently use p100, which means a single spike from 91 days ago permanently inflates the recommendation.
- **Separate request/limit**: Should the system recommend separate memory request and limit values? The current single-value recommendation forces users to guess the limit. VPA produces separate target/lower/upper bounds.
- **Limit = request policy**: Should "set limit = request" be the default recommendation, or should the system recommend limit > request for most workloads (with OOM feedback)? The former is safer but wasteful; the latter is more efficient but requires OOM awareness.
- **OOM collection in operator**: Can the operator add `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` to the ROS CSV payload? This is a prerequisite for the full memory algorithm.
- **Trend detection notifications**: Should the UI surface "memory trending up" warnings? This requires slope detection and a new notification type.
- **GPU gating bug**: Is the Kruize team aware that `checkIfModelIsKruizeSupportedMIG` blocks B200 and RTX PRO recommendations despite profile data existing? This appears to be a straightforward omission.
- **GPU underutilization threshold**: What percentage of GPU utilization should trigger a "consider removing this GPU" notification? Suggested: both core and memory below 10% sustained over the recommendation window.
- **Multi-GPU workloads**: Are multi-GPU containers (4-8 GPUs per pod for distributed training) common enough in the target user base to warrant support?
- **JVM/Quarkus thread pool**: The Quarkus `THREADS_PER_CORE=1` constant produces half or less of the framework default thread count. Is this intentional or an oversight? The Quarkus default is `max(8, 2×cores)`.
- **JVM data-driven tuning**: The `mvp_demo` branch handlers receive `filteredResultsMap` but don't use it. Is there a plan to consume actual JVM heap usage and GC metrics for data-driven recommendations?
- **JVM workload profiles**: Should GC selection consider workload type (latency-sensitive vs throughput-oriented)? The current heuristic always prefers low-latency GC for large heaps, even for batch processing workloads where throughput GC would save CPU.
- **JVM/Quarkus detection**: The `QueryBasedPresence` Prometheus detection mechanism — are the required JVM exporter metrics reliably available on target clusters? This determines whether runtime recommendations can fire at all.
- **Unbounded interval maps (§19.2)**: What is the intended lifecycle for experiment data in Kruize memory? Should intervals beyond the longest term window (91 days) be evicted automatically, or should Kruize rely solely on DB-backed storage?
- **Static experiment map (§19.3)**: Is there a maximum expected experiment count per Kruize instance? Should the system implement LRU eviction or DB-backed lazy loading?
- **mergeResults data loss (§19.5)**: Is the flattening behavior in `mergeResults` intentional for single-interval updates, or should it be fixed to support multi-interval batches?
- **ros-ocp-backend RBAC crash (§20.1)**: Should the API fail open or fail closed when the RBAC service is unreachable? Current behavior is "crash" — neither option.
- **Poison message handling (§20.9)**: Should ros-ocp-backend implement a dead-letter queue for unprocessable Kafka messages, or is a skip-and-log strategy acceptable?
- **Idle detection threshold (§23.1)**: Is 1% of request the right threshold for idle detection? Should it be configurable per namespace?
- **PVC right-sizing (§23.2)**: Can the existing `cost:` PVC metrics be routed to the ROS pipeline without duplicating queries? Or should the operator add `ros:` aliases?
- **HPA optimization (§23.3)**: Is combined VPA+HPA recommendation a product priority? This is StormForge's core differentiator but requires 8 new operator queries and significant algorithm work.
- **Go runtime detection (§23.4)**: Are `go_info` / `go_goroutines` metrics reliably available on target clusters? Many Go apps use the standard Prometheus client library which exposes these.
- **Runtime detection reliability**: For Go, Node.js, Python, .NET detection via Prometheus queries — what is the expected hit rate on customer clusters? If <20% of workloads export runtime metrics, the recommendation value is limited.
- **Operator query budget**: The operator currently runs ~73 queries per interval. Adding all 18 new queries (§23) + 2-4 replica count queries (§26) increases this by ~30%. Is this acceptable for cluster Prometheus performance?
- **Replica count source (§26)**: Should the operator collect desired replicas (`kube_deployment_spec_replicas`) or available replicas (`kube_deployment_status_replicas_available`), or both? Desired replicas represent the user's intent; available replicas reflect reality during scale-ups, failures, or quota constraints.
- **Replica impact display (§26)**: Should the UI show `total_savings = per_container × replicas` as the primary impact metric, or show both per-container and total? For DaemonSets (1 per node), the "replica count" is node count — should this be treated differently?
- **Replica count for HPA workloads (§26)**: For HPA-managed workloads where replica count varies, should the impact calculation use avg replicas (typical savings), max replicas (peak savings), or min replicas (guaranteed savings)?
- **JSONB migration (§27)**: Should the transition from JSONB to relational columns be done atomically (one migration) or with a dual-write period? The `workload_metrics` table can be dropped immediately (dead weight), but `recommendation_sets.recommendations` is actively used by the API.
- **(§28 alternative-store analysis only — v4.0 baseline is plain PostgreSQL 16+)** **TimescaleDB vs Thanos**: Does the organization already have Thanos infrastructure that would be easier to reuse? If not, would TimescaleDB (as discussed in §28) be preferable? **v4.0** does not require either for the recommendation engine.
- **(§28 only)** **TimescaleDB deployment**: If exploring §28’s TimescaleDB option, should it run on the same PostgreSQL instance as recommendation data, or separately? Same instance is simpler; separate instance isolates heavy `COPY FROM` ingestion. **v4.0** uses plain PostgreSQL for digests and results either way.
- **VM resize hysteresis (§30)**: Is the 40% threshold for VM downsizing recommendations appropriate? Lower thresholds generate more recommendations but increase VM restart churn. Should this be configurable per namespace?
- **VM IOPS as actionable vs informational (§30)**: Should IOPS data drive automatic storage class recommendations ("switch from gp3 to io2"), or only be reported as informational metrics? Actionable recommendations require knowledge of available storage classes.
- **VM disk growth projection window (§30)**: The default 30-day projection window for disk growth — is this appropriate for all VM workloads? Database VMs may need 90-day projection; ephemeral VMs may need none.
- **VM instance type recommendation (§30)**: Should the system recommend specific `VirtualMachineInstancetype` resources if available? This requires the operator to collect instance type metadata.
- **VM idle detection threshold (§30)**: Is `cpu_p95 < 50 millicores AND mem_p95 < 512 MiB` the right idle threshold for VMs? Windows VMs may have higher baseline resource usage even when idle.
- **Go recommendation maintenance (v4.0)**: Logic lives in Go (`recommendCPU()`, `recommendMemory()`, `detectIdle()`, …); `golang-migrate` handles **schema** only. Is the test and rollout strategy for algorithm changes sufficient (unit tests, feature flags, digest/recommendation row compatibility)? *(§29’s PL/pgSQL-in-database design is historical/superseded.)*
- **Shadow-mode validation (legacy cutover)**: When comparing ros-ocp-backend Go output to legacy Kruize or prior builds, should shadow runs cover every cluster or a canary? Full shadow doubles work but increases confidence. *(§29 referred to a superseded Go+PL/pgSQL hybrid.)*
- **Go + schema versioning (v4.0)**: Algorithm changes ship with the Go binary; DB changes use `golang-migrate` for tables/migrations — **not** `CREATE OR REPLACE FUNCTION` for recommendation math. How should breaking digest or result shapes be coordinated with deploys? *(§29’s “SQL function versioning” applied only to the abandoned PL/pgSQL approach.)*

---

## 23. Additional Recommendation Types (Industry Gap Analysis)

Kruize currently generates recommendations for: container CPU request/limit, container memory request/limit, namespace CPU request/limit, namespace memory request/limit, GPU MIG profile selection, and JVM/Quarkus runtime tuning (mvp_demo). This section identifies recommendation types offered by industry tools (CAST AI, StormForge, Kubecost, Densify, Goldilocks, Robusta KRR, Kubex) that Kruize does not generate, organized by priority.

### Operator Data Availability Key


| Symbol          | Meaning                                                                                 |
| --------------- | --------------------------------------------------------------------------------------- |
| **DATA EXISTS** | Operator already collects this metric (under `cost:` or `ros:` prefix)                  |
| **NEW QUERY**   | Requires new Prometheus queries in the operator                                         |
| **NO METRIC**   | Standard Prometheus/kube-state-metrics does not expose this; requires a custom exporter |


---

### 23.1 Priority 1: Idle/Abandoned Workload Detection

**Industry coverage:** CAST AI, Kubecost, Densify, Kubex, Sysdig. Every commercial tool offers this. Sysdig reports 69% of allocated CPU is unused across typical clusters.

**What to recommend:**

- Flag workloads where both CPU usage **and** memory working set are below 1% of their request, sustained over the full recommendation window (e.g., 15 days)
- Flag workloads with **zero CPU usage** over the window (truly abandoned)
- Distinguish idle-but-standby (e.g., disaster recovery replicas) from abandoned (e.g., forgotten test deployments) via namespace labels or annotation hints
- Estimated annual savings per idle workload: CPU request × hours × cost-per-core-hour

**Data status:** **DATA EXISTS**. The operator already collects all required metrics under `ros:` queries:

- `ros:container_cpu_usage_avg`, `ros:container_cpu_usage_max` — CPU usage aggregations
- `ros:container_memory_usage_avg`, `ros:container_memory_usage_max` — Memory usage aggregations
- `ros:container_cpu_requests_avg` — CPU request baseline

**Prometheus queries — no new queries required.** Detection is a pure algorithm change: if `max(cpu_usage) < 0.01 × cpu_request` AND `max(memory_usage) < 0.01 × memory_request` over all intervals in the window, flag as idle.

**Algorithm sketch:**

```
for each container in recommendation window:
  if max_cpu_usage < (0.01 × cpu_request) AND max_memory_usage < (0.01 × memory_request):
    if max_cpu_usage == 0 AND max_memory_usage == 0:
      recommendation = "ABANDONED — consider deleting"
    else:
      recommendation = "IDLE — consider scaling to zero or reducing to minimal request"
    estimated_savings = cpu_request × window_hours × $/core-hour
                      + memory_request × window_hours × $/GiB-hour
```

**Effort:** Very low. No operator change, no new metrics, no new infrastructure. Pure ros-ocp-backend Go logic addition (e.g., extend `detectIdle()` or equivalent).

---

### 23.2 Priority 2: PVC/Storage Right-Sizing

**Industry coverage:** Kubecost (PV right-sizing recommendations + disk-autoscaler), kubectl-unused-volumes, kubesphere/pvc-autoresizer.

**What to recommend:**

- **Overprovisioned PVCs**: When sustained usage is <50% of capacity, recommend smaller PVC size as `max_usage_over_window × 1.2` (20% headroom)
- **Orphaned PVCs**: PVCs bound but not mounted by any running pod
- **Zero-usage PVCs**: PVCs mounted but showing 0 bytes used over the window
- **Storage growth trend**: If usage is trending up toward capacity, warn before hitting the limit

**Data status:** **DATA EXISTS** (under `cost:` queries, not currently routed to ROS):

- `cost:persistentvolumeclaim_capacity_bytes` — PVC provisioned capacity
- `cost:persistentvolumeclaim_request_bytes` — PVC requested size
- `cost:persistentvolumeclaim_usage_bytes` — Actual bytes used (`kubelet_volume_stats_used_bytes`)
- `cost:persistentvolume_pod_info` — PVC-to-pod mapping

**New Prometheus queries for orphan detection (2 queries):**

```promql
-- ros:pvc_capacity_bytes (already available as cost:persistentvolumeclaim_capacity_bytes)
-- Can reuse existing cost: query or duplicate under ros: prefix

-- ros:pvc_usage_bytes (already available as cost:persistentvolumeclaim_usage_bytes)
-- Can reuse existing cost: query or duplicate under ros: prefix

-- NEW: ros:pvc_inode_usage (optional, for inode exhaustion detection)
kubelet_volume_stats_inodes_used{namespace!='',persistentvolumeclaim!=''}
/ kubelet_volume_stats_inodes{namespace!='',persistentvolumeclaim!=''}
```

**The core data is already collected by the operator for cost management purposes.** The main work is routing the existing `cost:` PVC metrics into the ROS pipeline (or creating `ros:` aliases) and implementing PVC right-sizing in ros-ocp-backend Go (e.g., `recommendPVC()`).

**Algorithm sketch (Kubecost-compatible):**

```
for each PVC:
  utilization = max_usage_over_window / capacity
  if utilization < 0.5 AND window >= 15 days:
    recommended_size = ceil(max_usage_over_window × 1.2)  # 20% headroom
    savings = (capacity - recommended_size) × $/GiB-month
    recommendation = "Reduce PVC from {capacity} to {recommended_size}"
  if utilization == 0 AND window >= 7 days:
    recommendation = "ZERO USAGE — consider deleting"
  if not mounted_by_any_pod:
    recommendation = "ORPHANED — not mounted by any running pod"
```

**Effort:** Low. No new operator queries needed (reuse `cost:` data). Moderate Go algorithm work in ros-ocp-backend. New recommendation type in ros-ocp-backend API.

---

### 23.3 Priority 3: HPA (Horizontal Pod Autoscaler) Optimization

**Industry coverage:** StormForge (core differentiator — "bi-dimensional autoscaling"), CAST AI, PerfectScale. This is the **biggest gap** vs competitors.

**What to recommend:**

- **HPA target utilization**: Optimal `targetCPUUtilizationPercentage` / `targetMemoryUtilizationPercentage` based on observed scaling behavior. Default 80% is often wrong — too high causes latency spikes before scale-up completes, too low wastes money.
- **Min/max replicas**: Tighter bounds based on observed replica count range. Overly wide ranges (min=1, max=100) signal lack of tuning.
- **Should this workload use HPA?**: Detect periodic or bursty load patterns where HPA would save money vs fixed replicas.
- **VPA+HPA coordination**: When both VPA-style recommendations (e.g., ros-ocp-backend `recommendCPU()` / `recommendMemory()`) and HPA are active, recommend smaller per-pod resources (VPA) combined with more replicas (HPA) rather than fewer large pods.

**Data status:** **NEW QUERY** — the operator collects zero HPA metrics today.

**New Prometheus queries required (8 queries):**

```promql
-- HPA configuration (instant queries)
-- ros:hpa_info — Links HPA to its target workload
ros:hpa_info:
  kube_horizontalpodautoscaler_info

-- ros:hpa_spec_min_replicas
ros:hpa_spec_min_replicas:
  kube_horizontalpodautoscaler_spec_min_replicas

-- ros:hpa_spec_max_replicas
ros:hpa_spec_max_replicas:
  kube_horizontalpodautoscaler_spec_max_replicas

-- ros:hpa_spec_target_metric — Configured target (e.g., 80% CPU utilization)
ros:hpa_spec_target_metric:
  kube_horizontalpodautoscaler_spec_target_metric

-- HPA status (range queries for historical behavior)
-- ros:hpa_status_current_replicas — Actual replica count over time
ros:hpa_status_current_replicas_avg:
  avg_over_time(kube_horizontalpodautoscaler_status_current_replicas[15m])
ros:hpa_status_current_replicas_max:
  max_over_time(kube_horizontalpodautoscaler_status_current_replicas[15m])
ros:hpa_status_current_replicas_min:
  min_over_time(kube_horizontalpodautoscaler_status_current_replicas[15m])

-- ros:hpa_status_desired_replicas — What HPA wanted (may differ from current due to min/max bounds)
ros:hpa_status_desired_replicas_max:
  max_over_time(kube_horizontalpodautoscaler_status_desired_replicas[15m])

-- ros:hpa_status_target_metric — Actual metric value vs target
ros:hpa_status_target_metric:
  kube_horizontalpodautoscaler_status_target_metric
```

**All metrics are from kube-state-metrics** (standard, available on all OpenShift clusters). Status: STABLE for most metrics.

**Algorithm sketch (StormForge-inspired):**

```
for each HPA:
  actual_range = [min(current_replicas), max(current_replicas)] over window
  configured_range = [spec_min_replicas, spec_max_replicas]

  -- Min replicas recommendation
  if min(current_replicas) > spec_min_replicas for >90% of window:
    recommended_min = percentile(5, current_replicas)  # rarely below this

  -- Max replicas recommendation
  if max(current_replicas) < spec_max_replicas × 0.7:
    recommended_max = max(current_replicas) × 1.3  # headroom for bursts

  -- Target utilization recommendation
  -- Find the target utilization that would have produced the same scaling
  -- behavior with fewer total pod-hours
  for target_util in [50%, 55%, 60%, ..., 95%]:
    simulated_replicas = simulate_scaling(cpu_usage_timeseries, target_util, min, max)
    total_pod_hours = sum(simulated_replicas × interval_duration)
    sla_violations = count(simulated_replicas × cpu_per_pod < actual_cpu_demand)
  recommended_target = argmin(total_pod_hours where sla_violations < threshold)

  -- VPA+HPA coordination
  if workload has both HPA and ROS CPU recommendation (from `recommendCPU()`):
    combined_cost = ros_cpu_rec × avg_replicas × $/core-hour
    smaller_pod_cost = (ros_cpu_rec × 0.6) × (avg_replicas × 1.5) × $/core-hour
    if smaller_pod_cost < combined_cost:
      recommendation += "Consider smaller pods with more replicas"
```

**Effort:** Moderate-High. Requires operator changes (8 new queries), new CSV columns, Go recommendation logic in ros-ocp-backend, and API additions.

---

### 23.4 Priority 4: Go Runtime Recommendations (GOMAXPROCS/GOMEMLIMIT)

**Industry coverage:** VictoriaMetrics (documentation), uber-go/automaxprocs (library). Not offered as a platform recommendation by any tool — a differentiation opportunity.

**The problem:** Go applications default `GOMAXPROCS` to the **node's** CPU count, not the pod's CPU limit. A Go container with `limits.cpu: 2` on a 64-core node runs with `GOMAXPROCS=64`, causing 32x more goroutine scheduling overhead than necessary. VictoriaMetrics identifies this as the #1 Go-on-Kubernetes performance issue.

Similarly, `GOMEMLIMIT` (Go 1.19+) defaults to no limit, causing the Go GC to be unaware of the container's memory constraint, leading to OOM kills.

**What to recommend:**

- **GOMAXPROCS**: Set to `ceil(cpu_limit)`. If not set via env var and cpu_limit is set, recommend adding `GOMAXPROCS` env var.
- **GOMEMLIMIT**: Set to `memory_limit × 0.9` (90%, leaving room for non-heap memory). If not set and memory_limit is set, recommend adding `GOMEMLIMIT` env var.

**Data status:** **NEW QUERY** for detection, **DATA EXISTS** for CPU/memory limits.

**New Prometheus queries required (1 query for detection):**

```promql
-- ros:go_runtime_detected — Detect Go applications via go_info metric
-- (Exposed by all Go applications using the default Prometheus client library)
ros:go_runtime_detected:
  max by (namespace, pod, container) (
    go_info{namespace!='',pod!='',container!=''}
  )
```

Alternatively, if `go_info` is not available (requires Go prom client), detect via the presence of `go_goroutines` or `go_gc_duration_seconds`:

```promql
-- Fallback: detect Go runtime via GC metrics
ros:go_runtime_detected_fallback:
  max by (namespace, pod, container) (
    go_goroutines{namespace!='',pod!='',container!=''}
  ) * 0 + 1
```

**CPU/memory limits are already collected** by the operator under existing `ros:` queries.

**Algorithm sketch:**

```
for each container where go_runtime_detected:
  cpu_limit = container_cpu_limit (from existing ros: data)
  memory_limit = container_memory_limit (from existing ros: data)

  if cpu_limit is set:
    recommended_gomaxprocs = ceil(cpu_limit)
    recommendation += "Set GOMAXPROCS={recommended_gomaxprocs} (current default: {node_cpus})"

  if memory_limit is set:
    recommended_gomemlimit = floor(memory_limit × 0.9)
    recommendation += "Set GOMEMLIMIT={recommended_gomemlimit} (prevents GC-unaware OOM)"
```

**Note:** This extends the `LayerRecommendationHandler` pattern from `mvp_demo` — add a `GoLayerRecommendationHandler` alongside the existing `HotspotLayerRecommendationHandler` and `QuarkusLayerRecommendationHandler`. The `QueryBasedPresence` detection mechanism works identically.

**Effort:** Low. 1 new operator query, ~200 lines of ros-ocp-backend handler code. High impact for OpenShift clusters where Go workloads are ubiquitous (operators, controllers, service meshes).

---

### 23.5 Priority 5: QoS Class Recommendations

**Industry coverage:** No tool explicitly recommends QoS classes, but Densify and CAST AI implicitly do by recommending request == limit for stable workloads.

**What to recommend:**

- **Guaranteed** (request == limit): For workloads with low CPU/memory variance (coefficient of variation < 0.2). These workloads gain priority scheduling and are never evicted for resource pressure.
- **Burstable** (request < limit): For workloads with moderate variance. Recommend specific request/limit gap.
- **BestEffort warning**: Flag any production-namespace workload with no resource requests/limits set.

**Data status:** **DATA EXISTS**. This is a derived recommendation from existing CPU/memory analysis — no new metrics needed.

**New Prometheus queries required:** None.

**Algorithm sketch:**

```
for each container:
  cpu_cv = stddev(cpu_usage) / avg(cpu_usage)  # coefficient of variation
  mem_cv = stddev(memory_usage) / avg(memory_usage)

  if cpu_cv < 0.2 AND mem_cv < 0.2:
    recommendation = "Set Guaranteed QoS (request == limit)"
    recommended_cpu_request = recommended_cpu_limit  # use the limit recommendation
    recommended_memory_request = recommended_memory_limit
  else if cpu_request == 0 AND memory_request == 0:
    recommendation = "WARNING: BestEffort QoS — workload will be evicted first under pressure"
  else:
    recommendation = "Burstable QoS is appropriate"
    # Existing request/limit recommendations already handle this
```

**Effort:** Very low. Pure algorithm addition, no new data.

---

### 23.6 Priority 6: Ephemeral Storage Recommendations

**Industry coverage:** Limited. k8s-ephemeral-storage-metrics (dedicated exporter), some Kubecost visibility.

**What to recommend:**

- Right-size ephemeral storage request/limit based on observed usage
- Flag containers with no ephemeral storage limit (risk of node eviction from disk pressure)

**Data status:** **NEW QUERY** — ephemeral storage metrics have historically been a gap in kube-state-metrics.

**New Prometheus queries required (4 queries):**

```promql
-- ros:container_ephemeral_storage_request
ros:container_ephemeral_storage_request:
  max by (namespace, pod, container) (
    kube_pod_container_resource_requests{resource='ephemeral-storage',namespace!=''}
  )

-- ros:container_ephemeral_storage_limit
ros:container_ephemeral_storage_limit:
  max by (namespace, pod, container) (
    kube_pod_container_resource_limits{resource='ephemeral-storage',namespace!=''}
  )

-- ros:container_ephemeral_storage_usage (requires kubelet metrics)
-- NOTE: container_fs_usage_bytes reliability varies by Kubernetes version.
-- On OpenShift 4.12+, kubelet_volume_stats_used_bytes is reliable for PVCs
-- but container-level ephemeral storage may require the k8s-ephemeral-storage-metrics exporter.
ros:container_ephemeral_storage_usage_avg:
  avg_over_time(container_fs_usage_bytes{container!='',namespace!=''}[15m])
ros:container_ephemeral_storage_usage_max:
  max_over_time(container_fs_usage_bytes{container!='',namespace!=''}[15m])
```

**Caveat:** `container_fs_usage_bytes` availability depends on the kubelet and CRI implementation. On some OpenShift versions, this metric may not be populated. Recommend testing on target clusters before enabling.

**Algorithm:** Same as memory right-sizing (high-percentile + headroom margin).

**Effort:** Low-Moderate. 4 new operator queries, Go algorithm in ros-ocp-backend (reuse memory pattern), API additions.

---

### 23.7 Priority 7: Node.js Runtime Recommendations

**Industry coverage:** No tool currently offers this as an automated recommendation.

**What to recommend:**

- `**--max-old-space-size`**: Set to `memory_limit × 0.75` (default is 2GB regardless of container limit). Node.js apps in containers with memory_limit < 2GB will OOM without this.
- **Worker threads / cluster workers**: For CPU-bound workloads, recommend `cluster.fork()` count = `ceil(cpu_limit)`.

**Data status:** **NEW QUERY** for detection.

**New Prometheus queries required (1 query for detection):**

```promql
-- ros:nodejs_runtime_detected — Detect Node.js via standard process metrics
-- (Exposed by the prom-client npm package)
ros:nodejs_runtime_detected:
  max by (namespace, pod, container) (
    nodejs_version_info{namespace!='',pod!='',container!=''}
  )

-- Fallback: detect via Node.js-specific GC metrics
ros:nodejs_runtime_detected_fallback:
  max by (namespace, pod, container) (
    nodejs_gc_duration_seconds_count{namespace!='',pod!='',container!=''}
  ) * 0 + 1
```

**Algorithm:** Same structure as JVM `MaxRAMPercentage` — set heap proportional to container memory limit.

**Effort:** Low. 1 new operator query, ~100 lines of ros-ocp-backend handler code (`NodeJSLayerRecommendationHandler`).

---

### 23.8 Priority 8: ResourceQuota Recommendations

**Industry coverage:** Kubecost (namespace budget alerts), limited direct quota recommendations.

**Full design:** [quota-recommendations.md](../features/quota-recommendations.md) — namespace
ResourceQuota right-sizing **implemented** (`quota` plugin, priority 35). ClusterResourceQuota
remains future work.

**What it is:** Right-size **ResourceQuota** (namespace-level) and **ClusterResourceQuota**
(cluster-level) hard limits from actual usage patterns, peak usage, and container
recommendation aggregates (with headroom margin).

**Why it's useful:** Over-provisioned quotas reserve capacity other teams cannot use;
under-provisioned quotas throttle workloads and block HPA scale-out. Pairs with idle
detection (§23.1): idle namespace + oversized quota = double waste.

**What to recommend:**

- Recommended `requests.cpu` and `requests.memory` quota = sum of per-workload recommendations × safety factor (1.3)
- Flag namespaces where quota >> actual usage (over-provisioned, blocks other teams)
- Flag namespaces where usage is >80% of quota (risk of deployment failures)

**Data status:** **PARTIAL** — hard and optional used limits in ROS namespace CSV; ClusterResourceQuota and storage/pod quota resources not collected.

**Already collected (koku-metrics-operator):** `kube_resourcequota{type='hard'}` for
`requests.cpu`, `limits.cpu`, `requests.memory`, `limits.memory` via
`ros:*_namespace_sum` queries in the namespace CSV ingest path.

**Still required (operator):**

```promql
-- ros:resourcequota_used — Current usage against quota
ros:resourcequota_used:
  kube_resourcequota{type='used',resource=~'requests.cpu|requests.memory|limits.cpu|limits.memory|pods'}
```

Additional gaps: `openshift_clusterresourcequota`, storage/pod quota resources, per-quota object name/UID when multiple quotas exist per namespace.

**Algorithm:** Compare namespace usage aggregates and peak usage + headroom against configured quota limits; recommend tighter or looser quotas. Aggregate per-workload recommendations into namespace totals.

**Savings signal:** Over-provisioned quotas → freed capacity (not always direct dollar savings); combine with idle namespace waste in fleet views.

**Implementation:** Shipped — Phase 1 `quota` plugin, `quota_recommendation_sets`, `GET .../quota/`; see [plugin-phases.md](plugin-phases.md).

**Remaining effort:** ClusterResourceQuota metrics, per-quota object identity, notification codes.
See [cluster-resource-quota.md](../features/cluster-resource-quota.md) for implemented CRQ support (separate
`openshift_clusterresourcequota_*` metrics, new CSV, `cluster-quota` plugin).

---

### 23.9 Priority 9: Python Runtime Recommendations

**Industry coverage:** None — another differentiation opportunity.

**What to recommend:**

- **Gunicorn/uWSGI worker count**: `2 × cpu_limit + 1` for CPU-bound, `4 × cpu_limit + 1` for I/O-bound (Gunicorn recommendation). Default is often hardcoded `4`.
- **Thread count per worker**: For `--threads`, recommend based on observed CPU vs I/O wait ratio.

**Data status:** **NO METRIC** for detection unless the app exports Prometheus metrics. Python applications rarely export process-level metrics by default.

**New Prometheus queries required (1 query, if available):**

```promql
-- ros:python_runtime_detected — Detect Python via process metrics
-- NOTE: Requires prometheus_client Python library to be installed in the app.
-- This is a significant limitation — most Python apps don't export these metrics.
ros:python_runtime_detected:
  max by (namespace, pod, container) (
    python_info{namespace!='',pod!='',container!=''}
  )
```

**Practical limitation:** Unlike Go (which has standard prom metrics built in) and JVM (which has standard JMX/Micrometer exporters), Python applications rarely expose Prometheus metrics without explicit instrumentation. This limits detection to apps that have already integrated `prometheus_client`.

**Effort:** Low (code), but limited applicability due to detection constraints.

---

### 23.10 Priority 10: .NET Runtime Recommendations

**Industry coverage:** None.

**What to recommend:**

- `**DOTNET_GCHeapHardLimit`**: Set to `memory_limit × 0.75` to prevent GC from consuming all container memory.
- **GC mode**: Server GC vs Workstation GC based on CPU allocation.
- **Thread pool min/max**: Based on observed thread count vs CPU limit.

**Data status:** **NEW QUERY** for detection.

**New Prometheus queries required (1 query):**

```promql
-- ros:dotnet_runtime_detected — Detect .NET via standard process metrics
-- (Exposed by prometheus-net library)
ros:dotnet_runtime_detected:
  max by (namespace, pod, container) (
    dotnet_info{namespace!='',pod!='',container!=''}
  )
```

**Effort:** Low. Similar structure to Go/Node.js handler.

---

### Summary: New Recommendation Types


| #     | Recommendation Type              | New Operator Queries | Data Source                  | Industry Coverage                  | Effort        | Impact                  |
| ----- | -------------------------------- | -------------------- | ---------------------------- | ---------------------------------- | ------------- | ----------------------- |
| 23.1  | **Idle workload detection**      | 0                    | Existing `ros:`              | CAST AI, Kubecost, Densify, Sysdig | Very low      | Very high               |
| 23.2  | **PVC/storage right-sizing**     | 0-1 (reuse `cost:`)  | Existing `cost:`             | Kubecost                           | Low           | High                    |
| 23.3  | **HPA optimization**             | 8                    | kube-state-metrics (STABLE)  | StormForge, CAST AI                | Moderate-High | Very high               |
| 23.4  | **Go GOMAXPROCS/GOMEMLIMIT**     | 1                    | `go_info` / `go_goroutines`  | None (differentiation)             | Low           | High                    |
| 23.5  | **QoS class recommendations**    | 0                    | Derived from existing        | Implicit in Densify, CAST AI       | Very low      | Low-Moderate            |
| 23.6  | **Ephemeral storage**            | 4                    | kubelet + kube-state-metrics | k8s-ephemeral-storage-metrics      | Low-Moderate  | Low-Moderate            |
| 23.7  | **Node.js `max-old-space-size`** | 1                    | `nodejs_version_info`        | None (differentiation)             | Low           | Moderate                |
| 23.8  | **ResourceQuota tuning**         | 2                    | kube-state-metrics (STABLE)  | Kubecost (partial)                 | Moderate      | Low-Moderate            |
| 23.9  | **Python worker count**          | 1                    | `python_info` (if available) | None (differentiation)             | Low           | Low (limited detection) |
| 23.10 | **.NET GC/heap tuning**          | 1                    | `dotnet_info`                | None (differentiation)             | Low           | Low-Moderate            |
|       | **Total new queries**            | **~18**              |                              |                                    |               |                         |


### Operator Query Budget

The koku-metrics-operator currently executes 53 `ros:` queries plus ~20 `cost:` queries per collection interval. Adding all 18 new queries would increase the ROS query count by ~34%. The heaviest additions are the HPA queries (8). However, most are instant queries on kube-state-metrics (very cheap — gauge lookups, no range computation).

**Recommended phasing:**

1. **Phase 0 (zero new queries):** Idle detection (§23.1) + QoS recommendations (§23.5) — pure algorithm, existing data
2. **Phase 1 (1 new query + route existing):** PVC right-sizing (§23.2, route `cost:` data) + Go runtime (§23.4, 1 query)
3. **Phase 2 (8 new queries):** HPA optimization (§23.3) — the largest gap vs competitors
4. **Phase 3 (remaining):** Ephemeral storage, Node.js, ResourceQuota, Python, .NET

---

## 24. Strategic Recommendation: Drop Kruize from the Remote Path (Legacy analysis)

### The Three Options


| Option                                           | Description                                                               | Timeline                  | Risk                                 |
| ------------------------------------------------ | ------------------------------------------------------------------------- | ------------------------- | ------------------------------------ |
| **A: Enhance both**                              | Fix ~52 bugs across both codebases, improve algorithms, keep architecture | 12-18 months (cross-team) | Maintains the fundamental bottleneck |
| **B: Drop Kruize, implement in ros-ocp-backend** | Replace Kruize proxy with native Go recommendation computation            | 4-6 months (single team)  | Loses upstream Kruize contributions  |
| **C: Start from scratch**                        | Throw away both codebases                                                 | 8-12 months               | Throws away sound infrastructure     |


### Recommendation: Option B

**Drop Kruize from the remote_monitoring path. Implement recommendation computation natively in ros-ocp-backend.**

### Why Option B

**1. The architecture is the problem, not the code.**

Fixing 52 bugs in a fundamentally bottlenecked architecture gives you a slightly faster bottlenecked architecture. The current pipeline serializes metrics **11 times** and stores them in **4 locations** before producing a recommendation. Every fix — batching Hibernate transactions, pooling HTTP connections, reusing Gson — is polishing a pipeline that shouldn't exist. Removing the pipeline gives you a clean path: CSV → in-process aggregation → daily digest rows in PostgreSQL → Go recommendation (`recommendCPU()`, `recommendMemory()`, etc.).

**2. The algorithms are simple.**

Kruize's entire recommendation logic in `remote_monitoring` mode is:

- CPU: sort values, pick percentile, multiply by safety factor, divide by pod count
- Memory: sort values, pick max (p100), add buffer
- GPU: bin-pack into MIG profiles
- JVM/Quarkus: static heuristics based on CPU/memory limits

This is ~820 lines of Java. The improved Go equivalent (with daily digest handling, `slices.Sort()` percentiles, OOM feedback, adaptive margins, trend detection, idle detection, PVC right-sizing, HPA, and Go runtime recommendations) is estimated at ~1,700 lines — a ~34% addition to ros-ocp-backend's existing ~5,000-line codebase. Not a rewrite.


| Algorithm                         | Kruize Java LOC | Estimated Go LOC | Notes                                          |
| --------------------------------- | --------------- | ---------------- | ---------------------------------------------- |
| CPU recommendation (improved)     | ~100            | ~80              | Remove 1-core discontinuity, add safety margin |
| Memory recommendation (improved)  | ~80             | ~70              | Adaptive margin, OOM feedback, trend detection |
| GPU MIG profile selection (fixed) | ~180            | ~150             | Fix B200/RTX PRO gating, frame buffer gaps     |
| Cost vs Performance models        | ~60             | ~50              | Different percentile targets                   |
| Daily digest + exact percentiles (new) | 0          | ~300             | Read digests once per cluster; `slices.Sort()`; no t-digest |
| OOM feedback (new)                | 0               | ~100             | Exponential backoff on OOM events              |
| Layered handler framework         | ~400            | ~300             | Go/Node.js/JVM runtime detection               |
| New recommendation types (§23)    | 0               | ~350             | Idle, PVC, HPA, Go runtime, QoS                |
| **Total**                         | **~820**        | **~1,700**       |                                                |


**3. Every new feature becomes a single-repo change.**

With Kruize in the loop, each new recommendation type (§23) requires:

1. Kruize algorithm PR → 2. Kruize review → 3. Kruize release → 4. ros-ocp-backend integration PR → 5. ros-ocp-backend review → 6. ros-ocp-backend release

That's 6 gates per feature across 2 teams in 2 languages. Without Kruize:

1. ros-ocp-backend PR → 2. Review → 3. Release

**2x faster feature velocity, single-team ownership.**

**4. The economics are clear.**

~1,700 lines of Go in one repository with one senior developer for 4-6 months vs ~52 cross-team PRs across two repositories in two languages for 12-18 months. The single-team approach also eliminates the coordination overhead that makes cross-team PRs 3-5x slower.

**5. What Kruize gives you that's hard to replicate: nothing relevant to remote_monitoring.**


| Kruize Capability                   | Used in remote_monitoring? | Hard to replicate in Go?                     |
| ----------------------------------- | -------------------------- | -------------------------------------------- |
| HPO / Experiment Management         | No (local_experiment only) | N/A                                          |
| Local monitoring (Prometheus query) | No (separate deployment)   | N/A                                          |
| CPU/memory algorithms               | Yes                        | No (~150 lines)                              |
| GPU MIG profiles                    | Yes                        | No (~150 lines, plus data tables)            |
| JVM/Quarkus layer handlers          | Yes (mvp_demo)             | No (~300 lines, extend LayerHandler pattern) |
| QueryBasedPresence detection        | Yes (mvp_demo)             | No (~100 lines, PromQL against cluster Prometheus) |
| Cost vs Performance models          | Yes                        | No (~50 lines, different percentile targets) |


**6. Go is dramatically cheaper to run than Java.**

Kruize requires 256-512 MB of heap. The equivalent Go implementation uses 20-50 MB (plus small in-memory buffers for digest aggregation and batch I/O). For on-prem deployments where every MB counts, this is a 5-10x footprint reduction for the recommendation service.

### Why Not Option A (Enhance Both)

- Maintains the 11-hop serialization pipeline and dual databases
- Every improvement fights against the architecture
- Cross-team coordination (Java team + Go team) is the #1 velocity bottleneck
- Kruize's `remote_monitoring` mode is clearly secondary to `local`/`local_experiment` in the Kruize team's priorities (evidence: `mvp_demo` focuses on layer detection, experiment management, HPO — all local features)
- The dual PostgreSQL databases store the **same metrics data** in different formats — pure architectural waste

### Why Not Option C (Start from Scratch)

ros-ocp-backend's infrastructure is architecturally sound:

- Kafka consumer: works (fix §20.3-§20.4)
- CSV parsing and aggregation: works
- PostgreSQL storage with GORM: works (fix §20.11)
- API layer with RBAC: works (fix §20.1, §20.12)
- Recommendation storage and serving: works

The problems are in the **Kruize integration layer** (the HTTP proxy) and in **Kruize itself**. The solution is to replace that layer, not throw away the system. Starting from scratch would require re-implementing ~4,500 lines of working infrastructure for zero additional benefit.

### What Happens to Kruize

Kruize remains valuable for:

- **Local monitoring mode**: Kruize on the OpenShift cluster, querying Prometheus directly. This is an independent deployment model that can coexist with ros-ocp-backend's native remote path.
- **Experiment management / HPO**: For teams doing A/B testing of resource configurations. Not relevant to the ROS pipeline.
- **Other Kruize consumers**: Teams using Kruize independently of the ROS product.

The recommendation is to **decouple**, not to oppose Kruize. ros-ocp-backend handles the remote/on-prem pipeline natively. Kruize handles local/experimental use cases. Both can benefit from the algorithm improvements in this report.

### Implementation Plan


| Phase | Weeks | What                                                                            | Deliverable                         |
| ----- | ----- | ------------------------------------------------------------------------------- | ----------------------------------- |
| **0** | 1-2   | Fix §20 critical/high bugs in ros-ocp-backend                                   | Stable foundation                   |
| **1** | 3-6   | Implement CPU + memory algorithms (improved) in Go: integer pipeline, daily digests, exact percentiles (`slices.Sort()`), plain PostgreSQL 16+ storage | Core recommendations without Kruize |
| **2** | 7-8   | Implement GPU MIG profile selection (fixed)                                     | GPU recommendations                 |
| **3** | 9-10  | Idle detection + PVC right-sizing (§23.1, §23.2)                                | Two new recommendation types        |
| **4** | 11-12 | Go runtime handler (§23.4) + QoS (§23.5)                                        | Two more types                      |
| **5** | 13-16 | HPA optimization (§23.3)                                                        | Biggest competitive gap             |
| **6** | 17-18 | JVM/Quarkus handlers (port from mvp_demo)                                       | Runtime recommendations             |
| **7** | 19-20 | Remove Kruize dependency, cleanup, documentation                                | Clean architecture                  |


**Total: ~5 months, one senior Go developer.**

### The One Caveat

If the **organizational/political** reality requires Kruize to be the recommendation engine (e.g., Kruize is a strategic Red Hat project that must be used regardless of technical merit), then Option A is the only path. In that case, prioritize: fix §19.1 (critical, 1 line) → fix §20.1-§20.4 (critical/high crashes) → implement §23.1 idle detection (no Kruize change) → push the Kruize team on algorithm fixes (§12, §16-18). But understand that this path is 3-4x slower and delivers fewer features.

---

## 25. Performance Model: Current vs "ros-ocp-backend with Superpowers"

This section provides a quantitative comparison between the current architecture (ros-ocp-backend + Kruize remote_monitoring, 2 PostgreSQL databases) and the target architecture (ros-ocp-backend with native Go recommendations: **read once, compute N terms**, integer types, exact percentiles in Go (`slices.Sort()`), daily digest partitioned tables, improved algorithms, **plain PostgreSQL 16+** only — no TimescaleDB, no Thanos, no t-digest in the remote path).

### 25.1 Per-Container Timing Model

#### Ingestion (per container per 15-minute interval)

**Current:**


| Step | Operation                                                    | Time        |
| ---- | ------------------------------------------------------------ | ----------- |
| 1    | CSV parse (Go)                                               | 0.1 ms      |
| 2    | Write metrics to ros-ocp PG (JSONB, per-row)                 | 3.5 ms      |
| 3    | Read metrics from ros-ocp PG for Kruize payload              | 2.0 ms      |
| 4    | Build JSON payload                                           | 0.5 ms      |
| 5    | HTTP POST /updateResults to Kruize (TLS, new connection)     | 100-200 ms  |
| 6    | Kruize: Gson parse (new Gson instance per request)           | 0.05 ms     |
| 7    | Kruize: Hibernate transaction open + PG write + fsync commit | 5-10 ms     |
| 8    | HTTP response + parse                                        | 5 ms        |
|      | **Total**                                                    | **~125 ms** |


**New:**


| Step | Operation                                                       | Time          |
| ---- | --------------------------------------------------------------- | ------------- |
| 1    | CSV parse (Go, integer types — float→int64 at parse time)       | 0.05 ms       |
| 2    | In-memory aggregation (`slices.Sort()` on ~96 int64 values)     | 0.003 ms      |
| 3    | Batch upsert daily digest to PG (`INSERT ... ON CONFLICT`, batch of 100) | 0.005 ms      |
|      | **Total**                                                       | **~0.058 ms** |


**Ingestion speedup: ~2,150x per container.** The dominant elimination is the per-container HTTP round-trip to Kruize (~125 ms), replaced by in-process `slices.Sort()` on ~96 integer values (~0.003 ms). No Thanos, no t-digest, no TimescaleDB — just Go + plain PostgreSQL.

#### Recommendation (per container)

**Current (91-day term, 8,736 intervals):**


| Step | Operation                                                    | Time       |
| ---- | ------------------------------------------------------------ | ---------- |
| 1    | HTTP POST /generateRecommendations                           | 10-30 ms   |
| 2    | Kruize reads 8,736 JSONB rows from Kruize PG                 | 8.7 ms     |
| 3    | Deserialize JSONB → Java boxed Doubles                       | 4.4 ms     |
| 4    | Build filteredResultsMap (2x for cost + performance models)  | 3.5 ms     |
| 5    | Sort 8,736 Doubles × 4 metrics × 2 models (Collections.sort) | 1.4 ms     |
| 6    | Per-pod estimation (sum/avg scan across all intervals)       | 0.9 ms     |
| 7    | Write recommendation to Kruize PG (transaction + commit)     | 5 ms       |
| 8    | Return JSON response over HTTP                               | 5 ms       |
| 9    | ros-ocp-backend parses JSON response                         | 0.5 ms     |
| 10   | ros-ocp-backend writes recommendation to ros-ocp PG          | 3 ms       |
|      | **Total**                                                    | **~42 ms** |


**New (Plain PostgreSQL 16+ + Go "read once, compute N terms"):**

> **Architecture evolution note:** The original v1.0 design used TimescaleDB continuous aggregates + PL/pgSQL, yielding ~0.004 ms/container amortized (~10,500x improvement). After determining that (a) AWS RDS doesn't support TimescaleDB, and (b) customer-defined recommendation periods (arbitrary 1–90 day windows) make PL/pgSQL inefficient (each term = separate SQL scan), the v4.0 design moves all recommendation computation to Go. The Go "read once, compute N terms" pattern reads the maximum window of daily digests once per cluster and computes all customer-defined terms from the same in-memory buffer.

| Step | Operation                                                                         | Time          |
| ---- | --------------------------------------------------------------------------------- | ------------- |
| 1    | Go reads daily digests for max window (single `SELECT ... ORDER BY` per cluster)  | ~10 ms / 1K containers |
| 2    | Go groups by container, iterates `[]DigestRow` per container                      | ~0.001 ms / container  |
| 3    | Go computes decay-weighted percentiles + margin + trend for 3 terms               | ~0.005 ms / container  |
| 4    | Go collects all results, `COPY FROM` batch write                                  | ~5 ms / 1K containers  |
|      | **Amortized per container (1K-container cluster, 3 terms)**                       | **~0.02 ms** |


**Recommendation speedup: ~2,100x per container (amortized).** The Go binary reads pre-computed daily digest rows once per cluster and computes all terms in memory. The per-container cost drops from ~42 ms (HTTP + JSONB read + Java sort + dual PG writes) to ~0.02 ms (amortized read + in-memory compute + batch write). At 50K containers: current = 35 min, new = ~1 sec. At 8M containers with 10 workers: ~30 seconds.

> **Note on the 10,500x vs 2,100x difference:** The original estimate (v1.0 design) assumed TimescaleDB continuous aggregates and PL/pgSQL, which amortize the read cost to near-zero because the aggregate is pre-materialized. The v4.0 Go-side design reads raw daily digest rows (not a pre-materialized view), adding ~10ms read overhead per cluster. However, the Go approach handles customer-defined terms efficiently (3 terms from one read vs 3 separate SQL calls), is testable in Go unit tests, and works on plain AWS RDS — a worthwhile trade-off.

### 25.2 End-to-End at Scale

#### 500 containers, customer-defined terms up to 90 days (small on-prem)


| Metric                           | Current                           | New                              | Factor       |
| -------------------------------- | --------------------------------- | -------------------------------- | ------------ |
| Ingestion per hour (4 intervals) | 500 × 4 × 125ms = **4.2 min**     | 500 × 4 × 0.058ms = **0.12 sec** | **~2,150x**  |
| Recommendation cycle (3 terms)   | 500 × 42ms = **21 sec**           | Go read+compute: **~20 ms**      | **~1,050x**  |
| **End-to-end per hour**          | **4.5 min**                       | **0.14 sec**                     | **~1,900x**  |
| Metrics storage (2 DBs vs 1)     | 500 × 1,440 × 1.3 KB = **936 MB** | Daily digests: 500 × 90d × 0.5 KB = **22 MB** | **~43x** |
| Service RAM                      | ~400-700 MB                       | ~60 MB                           | **~8x**      |


#### 50,000 containers, customer-defined terms up to 90 days (medium enterprise)


| Metric                  | Current                                       | New                                  | Factor       |
| ----------------------- | --------------------------------------------- | ------------------------------------ | ------------ |
| Ingestion per hour      | 50K × 4 × 125ms = **6.9 hours**               | 50K × 4 × 0.058ms = **11.6 sec**     | **~2,150x**  |
|                         | *Cannot keep up — falls 5.9h behind per hour* | *Completes in 12 seconds*            |              |
| Recommendation cycle (3 terms) | 50K × 42ms = **35 min**                | Go read+compute: **~1 sec** (50 clusters × 20ms) | **~2,100x** |
| **End-to-end per hour** | **Infeasible**                                | **13 sec**                           | **∞**        |
| Metrics storage         | 50K × 2,880 × 1.3 KB = **187 GB**             | Daily digests: 50K × 90d × 0.5 KB = **2.25 GB** | **~83x** |
| Service RAM             | ~700 MB + PG buffer pool                      | ~200 MB (digest cache)               | **~4x**      |


**The current architecture cannot process 50,000 containers.** Ingestion alone takes 6.9 hours per 1-hour window — it falls further behind every hour and never catches up.

#### 500,000 containers, customer-defined terms up to 90 days (large enterprise)


| Metric                  | Current                            | New (1 worker)                       | New (10 workers) |
| ----------------------- | ---------------------------------- | ------------------------------------ | ---------------- |
| Ingestion per hour      | 500K × 4 × 125ms = **69 hours**    | **1.9 min**                          | **12 sec**       |
|                         | *69x over SLA budget*              |                                      |                  |
| Recommendation cycle (3 terms) | 500K × 42ms = **5.8 hours** | **~10 sec** (500 clusters × 20ms)    | **~1 sec**       |
| **End-to-end per hour** | **Completely infeasible**          | **2 min**                            | **13 sec**       |
| Metrics storage         | 500K × 8,736 × 1.3 KB = **5.7 TB** | Daily digests: 500K × 90d × 0.5 KB = **22.5 GB** | **~253x** |
| Service RAM             | Multi-TB PG buffer pools           | ~2 GB cache + 100 MB app             | **~50x**         |


#### 8,000,000 containers, customer-defined terms up to 90 days (SaaS target)


| Metric                  | Current                           | New (10 workers)                    | New (100 workers) |
| ----------------------- | --------------------------------- | ----------------------------------- | ----------------- |
| Ingestion per hour      | 8M × 4 × 125ms = **46.3 days**    | **3.1 min**                         | **19 sec**        |
| Recommendation cycle (3 terms)* | 8M × 42ms = **3.9 days**  | **~30 sec** (10K clusters × 30ms, parallel) | **~3 sec**  |
| **End-to-end per hour** | **Physically impossible**         | **3.5 min**                         | **22 sec**        |
| Metrics storage         | 8M × 8,736 × 1.3 KB = **90.8 TB** | Daily digests: 8M × 90d × 0.5 KB = **360 GB** | **~252x** |
| Service RAM             | Cannot fit on a single server     | ~4 GB (digest batch buffer)         | Shardable         |


*At 8M containers, each cluster (~800 containers) is processed independently by the Go "read once, compute N terms" pattern. The Go binary reads 90 days of daily digests for the cluster (~800 × 90 × 0.5 KB = ~36 MB), computes all 3 customer-defined terms in memory, and batch-writes results. The 10-worker figure represents parallel Go goroutines processing different clusters concurrently.

### 25.3 Maximum Sustainable Throughput

The key metric: **how many containers can each architecture fully process (ingest + recommend) within its 1-hour SLA?**


| Architecture                      | Max containers/hour | Limiting factor                             |
| --------------------------------- | ------------------- | ------------------------------------------- |
| **Current** (ros-ocp + Kruize)    | **~1,000**          | /updateResults HTTP: 125ms × 4 × N < 3,600s |
| **New** (1 Go worker)             | **~500,000**        | PG batch write throughput (`COPY FROM`)     |
| **New** (10 Go workers)           | **~5,000,000**      | PG connection pool saturation               |
| **New** (100 workers, sharded PG) | **~20,000,000**     | PostgreSQL write throughput per shard        |


**Maximum throughput improvement: 500-5,000x** (depending on parallelism). The SaaS target of 8M containers is comfortably within the 10-worker capacity.

### 25.4 Resource Consumption


| Resource                      | Current                                                                 | New                                     | Improvement    |
| ----------------------------- | ----------------------------------------------------------------------- | --------------------------------------- | -------------- |
| Application RAM               | 256-512 MB (Kruize Java) + 100-200 MB (ros-ocp Go) = **350-700 MB**     | 50-200 MB (Go, digest batch buffer)     | **3-7x less**  |
| Application CPU               | 1-2 cores (Kruize GC + boxing) + 0.5-1 core (ros-ocp) = **1.5-3 cores** | 0.5-1 core (Go, native int64)           | **3x less**    |
| PostgreSQL instances          | **2** (ros-ocp PG + Kruize PG)                                          | **1** (plain PG 16+, AWS RDS compatible) | **2x less**    |
| Storage (50K containers, 90d) | **5.7 TB** across 2 DBs                                                 | **2.25 GB** (daily digests)             | **~2,500x less** |
| Container images              | 2 (Java + Go)                                                           | 1 (Go)                                  | **2x fewer**   |
| Network (internal)            | N HTTP round-trips/interval                                             | 0 (in-process)                          | **eliminated** |
| On-prem minimum footprint     | ~2 GB RAM                                                               | ~200 MB RAM                             | **10x less**   |


### 25.5 API Response Latency


| Scenario                                 | Current                                          | New                                                            | Improvement          |
| ---------------------------------------- | ------------------------------------------------ | -------------------------------------------------------------- | -------------------- |
| Fresh recommendation (compute on demand) | ~42 ms (if pre-computed) or N/A (wait for batch) | ~1-5 ms (Go reads digests + computes in memory)                | **8-42x**            |
| Cached recommendation (DB read)          | ~5-10 ms (JSONB deserialize)                     | ~0.3-0.5 ms (typed column, indexed)                            | **15x**              |
| Recommendation staleness                 | Hourly batch (up to 60 min stale)                | **Near-real-time** (recomputed after each ingestion)           | **Qualitative leap** |


The "compute on demand" capability is enabled by the Go recommendation engine: the API handler reads daily digest rows for the requested window from PostgreSQL (~1-3ms for a single container), computes the recommendation in memory using the same `recommendCPU()` / `recommendMemory()` functions used by the batch path, and returns the result. This means recommendations can be re-computed at API request time for any custom timeframe, including customer-defined periods not in the batch pre-computation set.

### 25.6 Scaling Walls


| Threshold                               | Current                               | New                                           |
| --------------------------------------- | ------------------------------------- | --------------------------------------------- |
| First scaling wall (ingestion > 1 hour) | **~1,000 containers**                 | **~500,000 containers** (1 worker)            |
| First storage concern (>1 TB)           | **~7,500 containers at 91d**          | **~22M containers at 90d** (daily digests)    |
| Requires horizontal scaling             | **~1,000 containers**                 | **~5M containers**                            |
| Requires sharded PostgreSQL             | **~10K containers** (2 DBs grow fast) | **~20M containers**                           |
| Physically impossible                   | **~50K+ containers**                  | **Not reached at 8M SaaS target**             |


### 25.7 Confidence and Caveats

These estimates are **structural** — derived from the number of operations, their known costs, and the elimination of entire pipeline stages. They are not benchmarks. Key assumptions:


| Assumption                                                    | Confidence        | Basis                                                                                                                                                                |
| ------------------------------------------------------------- | ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HTTP round-trip dominates current ingestion at ~125ms         | **Very High**     | Structural: TLS handshake (50-200ms) + JSON serialize + Gson parse + Hibernate transaction (5-10ms) + PG fsync. No pooling in current code.                          |
| `slices.Sort()` on ~96 int64 values takes ~0.003ms            | **Very High**     | 96 elements × O(n log n) with native int64 comparison (~5ns each). Well within CPU L1 cache.                                                                        |
| Batched PG writes are ~100x faster than per-row               | **High**          | Standard PostgreSQL behavior: single fsync per batch vs per row. Confirmed by Hibernate anti-pattern documentation.                                                  |
| Go CSV parsing is ~2x faster than current pipeline            | **Moderate-High** | Go `encoding/csv` vs current path (CSV → JSONB → JSON → Gson → Java objects). The serialization hops dominate, not raw parse speed.                                  |
| Go "read once, compute N terms" takes ~20-30ms per cluster    | **High**          | Single `SELECT` on indexed partitioned table (90d × 1K containers ≈ 90K rows × 0.5 KB = 45 MB) + in-memory compute (1K containers × 3 terms × 0.005ms = 0.015ms). I/O dominates. |
| Customer-defined terms amortize I/O: 3 terms ≈ 1 term cost    | **Very High**     | All terms computed from same in-memory buffer. Marginal cost per additional term is ~0.005ms/container (pure CPU). |
| 10 Go workers provide ~10x throughput                         | **High**          | CPU-bound work (in-memory sort, compute) scales linearly. PG reads are I/O-bound but concurrent per cluster. Amdahl's law: >95% parallelizable.                     |


### 25.8 Summary


| Metric                                  | Current → New                                                           | Factor          |
| --------------------------------------- | ----------------------------------------------------------------------- | --------------- |
| **Ingestion throughput**                | 8 containers/sec → 17,000/sec                                           | **~2,150x**     |
| **Recommendation throughput**           | 24 containers/sec → 50,000/sec (Go "read once, compute N terms")        | **~2,100x**     |
| **Max containers (1-hour SLA)**         | ~1,000 → ~5,000,000 (10 workers)                                        | **~5,000x**     |
| **Metrics storage (50K, 90d)**          | 5.7 TB → 2.25 GB (daily digests)                                        | **~2,500x**     |
| **Application RAM**                     | 350-700 MB → 50-200 MB                                                  | **~3-7x**       |
| **Application CPU**                     | 1.5-3 cores → 0.5-1 core                                                | **~3x**         |
| **Infrastructure**                      | 4 services (2 apps + 2 DBs) → 1 service (1 app + 1 plain PG 16+)         | **4x**          |
| **API latency**                         | 5-47 ms → 0.3-5 ms                                                      | **~10-15x**     |
| **Recommendation freshness**            | Hourly batch → Near-real-time (post-ingestion + on-demand)              | **Qualitative** |
| **Customer-defined terms**              | Fixed 1d/7d/15d only → Any 1-90 day windows per customer                | **Qualitative** |
| **Feature velocity**                    | 6 gates, 2 teams, 2 languages → 3 gates, 1 team, 1 language             | **~2x**         |
| **Development cost (all improvements)** | ~52 PRs, 12-18 months, cross-team → ~1,700 LOC, 4-6 months, single team | **~3x faster**  |

> **Architecture note (v4.0):** These numbers reflect the plain PostgreSQL 16+ + Go architecture. The original v1.0 design (TimescaleDB + PL/pgSQL) claimed ~10,500x recommendation throughput improvement due to pre-materialized continuous aggregates. The v4.0 architecture trades ~5x in amortized recommendation throughput (~2,100x vs ~10,500x) for: (a) AWS RDS compatibility (no TimescaleDB required), (b) efficient support for customer-defined recommendation periods (arbitrary 1–90 day windows), (c) all recommendation logic in Go (easier to test, debug, profile), and (d) zero PostgreSQL extension dependencies. The ~2,100x improvement is still **massive** — the current system processes ~1,000 containers/hour; the new system processes ~5,000,000.


---

## 26. Replica Count for Total Impact Calculation

**Applicability:** Both modes — the impact calculation is performed at API serving time, regardless of whether recommendations were computed locally or remotely.

### The Need

To calculate the **total cluster-wide savings** of applying a recommendation, the UI needs:

```
per_container_savings = new_recommendation - current_request
total_savings = per_container_savings × number_of_replicas
total_cluster_savings = Σ total_savings across all containers in all workloads
```

Without the replica count, each recommendation appears to save only the per-container delta, which dramatically understates the real impact for workloads running at scale (e.g., a 200m CPU over-request across 50 replicas = 10 cores wasted).

### Current Data: Per-Pod, Not Per-Workload

The operator's ROS container queries aggregate at the **pod level**: `avg by(container, pod, namespace, node)`. Each CSV row represents one container in one specific pod. If a Deployment has 3 replicas, the operator produces 3 separate CSV rows for the same container name, with different pod names.

The `ros:image_workloads` query attaches `workload` and `workload_type` labels to each pod row, so the downstream system knows which Deployment/StatefulSet each pod belongs to. But there is **no explicit replica count metric** collected.

### Workaround: Derive Replica Count from Existing Data (Fragile)

Since each pod produces its own CSV row with the workload label, ros-ocp-backend can count distinct pods per (namespace, workload, container) at each timestamp:

```
replicas_at_T = count(distinct pods where workload=X, container=Y at time T)
```

**Limitations:**


| Scenario                                 | Effect on derived count              | Impact              |
| ---------------------------------------- | ------------------------------------ | ------------------- |
| Pod restarted between collections        | Missed at that timestamp (count N-1) | Understates savings |
| Rolling update creating transient pod    | Counted as N+1                       | Overstates savings  |
| Pod evicted, not yet replaced            | Counted as N-1                       | Understates savings |
| Operator missed collection for some pods | Incorrect count                      | Non-deterministic   |
| CrashLoopBackOff pod not running         | Absent from CSV                      | Understates savings |


This approach gives **observed running pod count**, not the **desired replica count**, which is what the impact formula actually needs.

### Recommended: New Prometheus Queries for Desired Replica Count

**2-4 new queries** covering the main workload types, using standard kube-state-metrics (STABLE status, available on all OpenShift 4.12+ clusters):

```promql
-- Deployment desired replicas
ros:deployment_replicas_desired:
  max by (namespace, deployment) (
    kube_deployment_spec_replicas{namespace!=''}
  ) * on(namespace) group_left
    kube_namespace_labels{
      label_insights_cost_management_optimizations='true',
      namespace!~'kube-.*|openshift|openshift-.*'
    }

-- Deployment available replicas (optional, for health context)
ros:deployment_replicas_available:
  max by (namespace, deployment) (
    kube_deployment_status_replicas_available{namespace!=''}
  ) * on(namespace) group_left
    kube_namespace_labels{
      label_insights_cost_management_optimizations='true',
      namespace!~'kube-.*|openshift|openshift-.*'
    }

-- StatefulSet desired replicas
ros:statefulset_replicas_desired:
  max by (namespace, statefulset) (
    kube_statefulset_replicas{namespace!=''}
  ) * on(namespace) group_left
    kube_namespace_labels{
      label_insights_cost_management_optimizations='true',
      namespace!~'kube-.*|openshift|openshift-.*'
    }

-- DaemonSet desired count (one per matching node)
ros:daemonset_desired_scheduled:
  max by (namespace, daemonset) (
    kube_daemonset_status_desired_number_scheduled{namespace!=''}
  ) * on(namespace) group_left
    kube_namespace_labels{
      label_insights_cost_management_optimizations='true',
      namespace!~'kube-.*|openshift|openshift-.*'
    }
```

These are **gauge metrics** (instant values), the cheapest possible Prometheus query type. They add negligible load to the operator's query budget (~4 queries on top of the existing ~73).

### Joining Replica Count to Recommendations

The join key between the replica count queries and the recommendation is **(namespace, workload_name, workload_type)**:


| Workload Type                | Replica Count Source                             | Join Field                                                |
| ---------------------------- | ------------------------------------------------ | --------------------------------------------------------- |
| Deployment                   | `kube_deployment_spec_replicas`                  | `deployment` label = workload name                        |
| StatefulSet                  | `kube_statefulset_replicas`                      | `statefulset` label = workload name                       |
| DaemonSet                    | `kube_daemonset_status_desired_number_scheduled` | `daemonset` label = workload name                         |
| ReplicaSet (standalone)      | `kube_replicaset_spec_replicas`                  | `replicaset` label (rare, usually owned by Deployment)    |
| DeploymentConfig (OpenShift) | `kube_replicaset_spec_replicas`                  | Via owner chain                                           |
| CronJob/Job                  | Not applicable                                   | Ephemeral workloads — replica-based impact not meaningful |


### Data Collection Frequency

**15-minute intervals are sufficient.** `spec.replicas` changes only when:

- A human edits the Deployment (rare — typically once per deploy)
- HPA scales the workload (minutes to hours between events)
- GitOps reconciliation changes replicas

For HPA-managed workloads, the `avg_over_time` and `max_over_time` variants give:

- **avg**: typical replica count over the recommendation window (for average impact)
- **max**: peak replica count (for maximum impact)

Both are meaningful for different use cases:

- "What will this save on average?" → use avg replicas
- "What is the maximum savings potential?" → use max replicas

### API Response Integration

The replica count should be exposed alongside each container recommendation:

```json
{
  "container": "api-server",
  "workload": "api-deployment",
  "workload_type": "Deployment",
  "replicas": {
    "desired": 5,
    "available": 5
  },
  "recommendations": {
    "cpu": {
      "current_request": "500m",
      "recommended_request": "200m",
      "per_container_savings": "300m",
      "total_savings": "1500m"
    },
    "memory": {
      "current_request": "512Mi",
      "recommended_request": "256Mi",
      "per_container_savings": "256Mi",
      "total_savings": "1280Mi"
    }
  }
}
```

### Effort Estimate


| Task                                        | Component             | Effort                        | Queries |
| ------------------------------------------- | --------------------- | ----------------------------- | ------- |
| Add 2-4 replica count Prometheus queries    | koku-metrics-operator | Very low (1 PR)               | 2-4 new |
| Add replica count columns to ROS CSV        | koku-metrics-operator | Very low (same PR)            | —       |
| Store replica count in ros-ocp-backend DB   | ros-ocp-backend       | Low (1 PR)                    | —       |
| Expose replica count + total savings in API | ros-ocp-backend       | Low (1 PR)                    | —       |
| Display total impact in UI                  | koku-ui               | Moderate (1-2 PRs)            | —       |
| **Total**                                   |                       | **Low (3-5 PRs, ~1-2 weeks)** | **2-4** |


### Alternative: Derive at API Time (No Operator Change)

If changing the operator is not immediately feasible, ros-ocp-backend can derive the replica count from the per-pod CSV rows it already receives. This provides an approximate count at the cost of accuracy during scale events, but works for steady-state workloads (the vast majority).

### Impact on §25 Performance Model

Adding 2-4 gauge queries has negligible impact on the operator's Prometheus query budget. These are instant queries on pre-computed kube-state-metrics — typically <1ms per query. The additional CSV columns add <1% to payload size. No change to the §25 performance estimates.

---

## 27. JSONB Analysis: Why It Exists, Why It Hurts, and Alternatives

### Current JSONB Usage

ros-ocp-backend has **5 JSONB columns** across its database schema:


| Table                                      | JSONB Column      | Written By                                              | Read By                     | PG JSON Ops? | Verdict                      |
| ------------------------------------------ | ----------------- | ------------------------------------------------------- | --------------------------- | ------------ | ---------------------------- |
| `workload_metrics`                         | `usage_metrics`   | `report_processor` (marshals `[]Metric`)                | **Nobody**                  | No           | **Dead weight** — write-only |
| `recommendation_sets`                      | `recommendations` | `recommendation_poller` (marshals `RecommendationData`) | API list/detail, CSV export | No           | Active but wasteful          |
| `namespace_recommendation_sets`            | `recommendations` | Same                                                    | Same pattern                | No           | Active but wasteful          |
| `historical_recommendation_sets`           | `recommendations` | Same                                                    | **Nobody** (archive)        | No           | Dead archive                 |
| `historical_namespace_recommendation_sets` | `recommendations` | Same                                                    | **Nobody** (archive)        | No           | Dead archive                 |


### Key Finding: `workload_metrics.usage_metrics` Is Dead Weight

This JSONB column is written to on every ingestion cycle via `json.Marshal(container.Metrics)` in `report_processor.go`, but **no code path in the entire repository ever reads it**. It exists because the original architecture anticipated Kruize might need raw metrics re-read from ros-ocp PostgreSQL, but this never materialized — Kruize receives metrics via HTTP `/updateResults` calls, not by querying the ros-ocp database.

**Cost of this dead column:**

- CPU for `json.Marshal` on every ingestion (8 metric objects × 4 aggregation values each)
- Disk for TOAST storage of the JSONB blob (~1-2 KB per row)
- WAL for replication of every JSONB write
- Partitioning maintenance for the `workload_metrics` table (monthly partitions with triggers)

### How `recommendations` JSONB Is Used

The `recommendations` column stores a deeply nested Kruize response structure (~2-5 KB per row):

```
RecommendationData
├── notifications: map[string]Notification
├── monitoring_end_time: timestamp
├── current: ConfigObject { limits/requests → { cpu/memory → {amount, format} } }
└── recommendation_terms
    ├── short_term: { duration, start, engines: { cost, performance }, plots }
    ├── medium_term: (same)
    └── long_term: (same)
```

On API read, the JSONB blob goes through this expensive path:

1. **Deserialization** to `map[string]interface{}` (untyped, allocation-heavy Go map)
2. **Mutation**: box plots stripped, units transformed, notifications filtered, variations converted to percentages
3. **Re-serialization** to JSON for the HTTP response

For CSV export, it's deserialized to typed `kruizePayload.RecommendationData` structs — a second schema.

**No PostgreSQL JSON operators (`->`, `->>`, `@>`) are ever used.** The JSONB is treated as an opaque blob. This means we get none of JSONB's indexing benefits (GIN indexes, partial queries) while paying all of its overhead (TOAST storage, detoasting on read, marshal/unmarshal in Go).

### Root Cause

JSONB exists because ros-ocp-backend was designed as a **thin proxy** for Kruize. The backend doesn't understand the recommendation structure — it receives an opaque JSON response from Kruize and dumps it into the database. The API layer then has to do ad-hoc mutations on `map[string]interface{}` because it lacks typed access.

### Recommended Replacement: Relational Columns

Since the new architecture computes recommendations natively in Go, we **know the exact structure at compile time**. Replace JSONB with typed columns:

```sql
CREATE TABLE recommendation_sets (
    id UUID PRIMARY KEY,
    workload_id INT NOT NULL,
    monitoring_start TIMESTAMPTZ NOT NULL,
    monitoring_end TIMESTAMPTZ NOT NULL,
    term TEXT NOT NULL,           -- 'short', 'medium', 'long'
    engine TEXT NOT NULL,         -- 'cost', 'performance'
    -- Current values (integer types)
    current_cpu_request_millicores INT,
    current_cpu_limit_millicores INT,
    current_memory_request_kib BIGINT,
    current_memory_limit_kib BIGINT,
    -- Recommended values
    rec_cpu_request_millicores INT,
    rec_cpu_limit_millicores INT,
    rec_memory_request_kib BIGINT,
    rec_memory_limit_kib BIGINT,
    -- Variation (percentage)
    variation_cpu_request_pct REAL,
    variation_memory_request_pct REAL,
    -- Notifications (structured)
    notifications SMALLINT[],     -- array of notification codes
    confidence_level REAL,
    ...
);
```

**Benefits:**

- **Zero serialization/deserialization** — GORM maps columns to struct fields directly
- **Native filtering/sorting** — `WHERE rec_cpu_request_millicores > current_cpu_request_millicores`
- **Native aggregation** — `AVG(variation_cpu_request_pct)` across all containers
- **Smaller storage** — integers are 4-8 bytes each vs. a ~~2-5 KB JSONB blob per row (~~10-20x reduction)
- **No TOAST overhead** — small rows stay inline, no detoasting on read
- **Indexable** — B-tree on any column for fast lookups
- **Type safety** — compile-time validation, no runtime `map[string]interface{}` failures

### Impact Estimate


| Metric                                | Current (JSONB)                         | Relational Columns            | Improvement    |
| ------------------------------------- | --------------------------------------- | ----------------------------- | -------------- |
| Storage per recommendation row        | ~2-5 KB (JSONB blob)                    | ~100-200 bytes (integers)     | ~10-20x        |
| API read: deserialization             | ~0.5-2 ms/row (`json.Unmarshal` to map) | 0 (GORM struct scan)          | Eliminated     |
| API read: mutation + re-serialization | ~0.2-0.5 ms/row                         | 0 (columns are already typed) | Eliminated     |
| Write path: `json.Marshal`            | ~0.1-0.3 ms/row                         | 0 (GORM inserts columns)      | Eliminated     |
| DB index support                      | No (JSONB is opaque blob)               | Yes (B-tree on any column)    | New capability |


### Alternative: Protocol Buffers

If forward-compatibility requires a blob column (unknown future fields), protobuf (`BYTEA`) is ~3-10x smaller than JSON and ~10-100x faster to serialize. However, relational columns are strictly better for this use case since we define the schema.

---

## 28. Alternative Metrics Store: TimescaleDB vs Thanos (historical — not v4.0)

> **Not the shipped design.** v4.0 uses **plain PostgreSQL 16+** daily digest tables and **exact** Go percentiles (`slices.Sort()`). This section compares **hypothetical** Thanos vs TimescaleDB stacks for readers tracing design history; it does **not** describe current ros-ocp-backend behavior.

### Context

The original proposal (§3) recommended Thanos as the metrics store to replace JSONB. This section evaluates **TimescaleDB** (a PostgreSQL extension) as a simpler alternative that achieves the same goals with less infrastructure — **as a design alternative that was not adopted** for the native engine.

### Architecture Comparison


| Aspect                      | Thanos                                                      | TimescaleDB                                              |
| --------------------------- | ----------------------------------------------------------- | -------------------------------------------------------- |
| **What it is**              | Distributed Prometheus long-term store (3-4 services)       | PostgreSQL extension (`CREATE EXTENSION timescaledb`)    |
| **New infrastructure**      | Thanos Receive + Compactor + Store Gateway + object storage | Zero — extension on existing PostgreSQL instance         |
| **Query language**          | PromQL                                                      | SQL (already known by team)                              |
| **Ingestion method**        | Prometheus remote write protocol (HTTP + protobuf + snappy) | `COPY FROM` (native PostgreSQL CSV ingestion)            |
| **Compression**             | Gorilla XOR + LZ4 (~90-95%)                                 | Delta-of-delta + Gorilla + LZ4 (~90-95%)                 |
| **Percentile support**      | `quantile_over_time()` in PromQL                            | Native t-digest via `timescaledb_toolkit` extension      |
| **Pre-computed aggregates** | Not native (requires recording rules)                       | Continuous aggregates (auto-updating materialized views) |
| **Retention policies**      | Compactor-based                                             | Built-in `add_retention_policy()`                        |
| **Multi-tenancy**           | Label-based or tenant header                                | Schema-based or label-based (PostgreSQL native)          |
| **Team familiarity**        | New (PromQL, remote write protocol)                         | Already known (PostgreSQL, SQL)                          |
| **Operational complexity**  | High (distributed system, 3-4 services)                     | Low (single PostgreSQL extension)                        |


### Why TimescaleDB Would Have Been Better (hypothetical — not adopted)

**1. CSV ingestion is native and extremely fast**

```sql
COPY metrics (ts, container_id, metric_name, value_millicores)
FROM STDIN WITH (FORMAT csv);
```

Performance: **300,000-1,000,000 rows/sec** via `COPY FROM`.

The Thanos path requires: parse CSV → build protobuf remote write request → snappy compress → HTTP POST → Thanos Receive → WAL → compaction. Implementing the Prometheus remote write protocol in Go is non-trivial (~300-500 lines + retry logic + batching).

**2. Native t-digest would have eliminated a custom sketch implementation (hypothetical)**

In the **superseded** TimescaleDB design, Toolkit provides built-in t-digest that integrates with continuous aggregates:

```sql
-- Pre-compute daily t-digests (auto-updating)
CREATE MATERIALIZED VIEW daily_digests
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', ts) AS bucket,
    container_id,
    tdigest(200, cpu_usage_millicores) AS cpu_digest,
    tdigest(200, memory_usage_kib) AS mem_digest
FROM metrics
GROUP BY bucket, container_id;

-- Query percentiles for any time range (digests merge automatically)
SELECT
    container_id,
    approx_percentile(0.98, rollup(cpu_digest)) AS cpu_p98,
    approx_percentile(0.60, rollup(cpu_digest)) AS cpu_p60
FROM daily_digests
WHERE bucket BETWEEN '2026-03-01' AND '2026-03-26'
GROUP BY container_id;
```

This replaces:

- Custom Go t-digest implementation (~300 lines)
- Manual serialization/deserialization of digest blobs to `BYTEA`
- Custom merge logic for term windows
- Custom continuous aggregate equivalent

The `rollup()` function merges digests across arbitrary time ranges — exactly what custom timeframes (COST-5691) need.

**3. Zero new infrastructure**

ros-ocp-backend already uses PostgreSQL. Adding TimescaleDB:

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;
```

Versus Thanos, which requires deploying and operating:

- Thanos Receive (port 19291, gRPC + HTTP)
- Thanos Compactor (background process, object storage)
- Thanos Store Gateway (for historical queries)
- Object storage (S3/MinIO)
- Configuration for multi-tenancy, retention, compaction

**4. Custom timeframes are trivial**

```sql
SELECT approx_percentile(0.98, rollup(cpu_digest))
FROM daily_digests
WHERE bucket BETWEEN $start AND $end
  AND container_id = $container;
```

No need to re-query raw data or maintain sliding windows. Business hours filtering can be handled by a second continuous aggregate that only includes business-hour intervals.

**5. Automatic compression and retention**

```sql
-- Enable columnar compression (90-95% storage reduction)
ALTER TABLE metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'container_id',
    timescaledb.compress_orderby = 'ts'
);

-- Compress chunks older than 2 days
SELECT add_compression_policy('metrics', INTERVAL '2 days');

-- Drop data older than 45 days
SELECT add_retention_policy('metrics', INTERVAL '45 days');
```

### Performance Comparison


| Operation                                     | Thanos                                                 | TimescaleDB                                          |
| --------------------------------------------- | ------------------------------------------------------ | ---------------------------------------------------- |
| CSV ingestion (1M rows)                       | ~5-15s (parse + remote write + WAL)                    | ~1-3s (`COPY FROM` + hypertable)                     |
| Percentile query (1 container, 30 days)       | ~50-200ms (PromQL scan)                                | ~1-5ms (pre-computed digest `rollup()`)              |
| Storage (1M metrics/day, 45 days, compressed) | ~2-4 GB (Gorilla + LZ4)                                | ~2-5 GB (columnar + LZ4)                             |
| New infrastructure components                 | 3-4 services                                           | 0 (extension on existing PG)                         |
| Implementation effort                         | ~3-4 weeks (remote write protocol, multi-tenancy)      | ~1-2 weeks (hypertable, COPY, continuous aggregates) |
| Operational overhead (ongoing)                | Moderate (compactor tuning, retention, object storage) | Low (PostgreSQL extension, standard backup/restore)  |


### When Thanos Still Wins

- If the organization **already has Thanos** and wants to federate ROS metrics with cluster Prometheus data.
- If PromQL compatibility is required for external tools (Grafana dashboards, alerting rules).
- If the metrics volume exceeds what a single PostgreSQL instance can handle (>10M containers with 91-day raw retention — at this scale, Thanos' distributed architecture is necessary).

For ros-ocp-backend's specific use case — storing metrics from CSVs and computing recommendations — these scenarios do not apply.

### Hypothetical architecture if TimescaleDB were chosen (not v4.0)

> **Not implemented in v4.0.** Shipped remote monitoring uses daily digests in **plain PostgreSQL** with **Go-side** percentile math (`slices.Sort()`), not TimescaleDB continuous aggregates or `rollup()` in SQL.

```
Operator → CSV → Kafka → ros-ocp-backend with superpowers
                           ├── Parse CSV → integer types
                           ├── COPY FROM → TimescaleDB hypertable
                           │              └── continuous aggregate (daily t-digests, auto)
                           ├── Go recommendation engine ← rollup(digests) via SQL
                           ├── PostgreSQL (recommendations as relational columns)
                           └── REST API → UI
```

**Services (hypothetical):** ros-ocp-backend (Go), PostgreSQL (with TimescaleDB + Toolkit extensions)
**Eliminated vs Thanos proposal:** Thanos Receive, Thanos Store Gateway, Thanos Compactor, object storage

### Impact on §25 Performance Model (hypothetical Thanos vs TimescaleDB only)


| Metric                                   | With Thanos (hypothetical)                  | With TimescaleDB (hypothetical)           | Delta                                            |
| ---------------------------------------- | ------------------------------------------- | ----------------------------------------- | ------------------------------------------------ |
| Ingestion throughput                     | ~15,000 containers/sec                      | ~15,000-20,000 containers/sec             | Same or slightly better (`COPY` vs remote write) |
| Recommendation throughput                | ~60,000 containers/sec                      | ~60,000 containers/sec                    | Same (sketch-based percentiles in both hypothetical paths) |
| Infrastructure services                  | 2 (ros-ocp + PG) + Thanos (3-4) = 5-6 total | 2 (ros-ocp + PG w/ TimescaleDB) = 2 total | **3-4 fewer services**                           |
| Implementation effort (metrics pipeline) | ~3-4 weeks                                  | ~1-2 weeks                                | **~2 weeks saved**                               |
| Percentile implementation (hypothetical) | Custom Go t-digest sketch (~300 lines)      | Built-in (`timescaledb_toolkit`)          | Toolkit avoids custom sketch code — **neither path shipped** |
| Custom timeframe support                 | Custom merge + decay logic                  | `rollup()` + continuous aggregates        | **Simpler**                                      |


### Recommendation (historical: Thanos vs TimescaleDB only)

**If** the only choice were between Thanos and TimescaleDB as a dedicated metrics store, TimescaleDB would have been simpler for this use case:

- **Simpler** — no new services, no remote write protocol, no PromQL
- **Faster** — native `COPY FROM` for CSV, pre-computed digests for percentiles
- **Cheaper** — zero additional infrastructure cost and operational overhead
- **More maintainable** — PostgreSQL + SQL, which the team already knows
- **Feature-complete** — native t-digest, continuous aggregates, compression, retention

**v4.0 actual path:** The remote pipeline does **not** adopt either stack for recommendations. Metrics land in **plain PostgreSQL** (interval + daily digest tables); **Go** performs decay weighting, percentiles (`slices.Sort()`), margins, trends, and idle detection. If Thanos exists in an organization, it remains a possible **complementary** tool for debugging cluster Prometheus data — it is not part of the v4.0 ROS remote recommendation architecture.

---

## 29. Historical: In-database recommendation computation (PL/pgSQL hybrid proposal)

> **v4.0:** Shipped recommendations use the **native Go engine** in ros-ocp-backend (see document header and §2). **No** PL/pgSQL recommendation functions, **no** TimescaleDB requirement, **no** t-digest in the remote path. This section preserves a **superseded** performance analysis of a SQL-side math hybrid for readers comparing design options.

### 29.1 The Problem: Data Round-Trips

In the **hypothetical** stack where TimescaleDB stored metrics as time-series and continuous aggregates pre-computed daily t-digest sketches, that **hybrid** design still required:

1. **Go reads** merged t-digest from TimescaleDB via SQL query
2. **Go deserializes** the digest into memory
3. **Go extracts** percentile, applies safety margin, trend detection, etc.
4. **Go serializes** the recommendation back
5. **Go writes** the recommendation to PostgreSQL

For 50K containers, that is 50K SQL read round-trips + 50K SQL write round-trips + 50K Go heap allocations. The data leaves the database only to be fed back into the same database.

### 29.2 The Insight: SQL Can Do the Math

The core mathematical operations required for recommendations are all expressible in SQL:


| Operation                   | SQL Expression                                                         |
| --------------------------- | ---------------------------------------------------------------------- |
| Merge t-digests across days | `rollup(daily_digest)` (TimescaleDB Toolkit)                           |
| Extract percentile          | `approx_percentile(0.98, rollup(daily_digest))`                        |
| Safety margin (IQR-CV)      | `(p95 - p50) / NULLIF(mean, 0)` → adaptive multiplier                  |
| Trend detection             | `regr_slope(value, epoch)` built-in PostgreSQL                         |
| Exponential decay weighting | `SUM(digest * exp(-lambda * age_days)) / SUM(exp(-lambda * age_days))` |
| Idle detection              | `approx_percentile(0.98, ...) < threshold`                             |
| QoS class recommendation    | `CASE WHEN cv < 0.15 THEN 'Guaranteed' ... END`                        |


**Key realization:** All of these operate on data already in PostgreSQL. The data never needs to leave the database for the core computation.

### 29.3 The Hybrid Architecture

Split recommendation logic into two layers:

#### In-Database (PL/pgSQL / SQL functions) — ~60% of logic

- **Container CPU/Memory recommendations** — the hot path (50K+ containers)
  - Read continuous aggregate (pre-computed daily t-digests)
  - Merge digests for the requested timeframe via `rollup()`
  - Extract p50/p95/p98/p99.9 via `approx_percentile()`
  - Compute safety margin via IQR-CV
  - Apply decay weighting
  - Detect trend via `regr_slope()`
  - Write recommendation directly to relational columns
  - All done in a single `SELECT ... INTO` or `INSERT ... SELECT` across all containers in one statement
- **Idle workload detection** — `WHERE p98_cpu_mc < 10 AND p98_mem_kib < 1024`
- **QoS class recommendation** — coefficient-of-variation check in SQL
- **PVC right-sizing** — max usage vs capacity (simple threshold math)
- **Namespace quota recommendations** — aggregation of per-container recs

#### In Go — ~40% of logic

- **I/O orchestration:** Kafka consumption, CSV parsing, `COPY FROM` to TimescaleDB
- **GPU recommendations:** MIG profile bin-packing requires lookup tables and complex branching
- **JVM/Quarkus heuristics:** Runtime-specific knowledge (GC tuning, heap sizing) with branchy logic
- **HPA optimization:** Requires reading HPA specs and mapping to recommendation adjustments
- **OOM event processing:** Parsing + writing to TimescaleDB (simple, but I/O)
- **Notification assembly:** Building human-readable messages, severity classification
- **API serving:** HTTP handlers, response assembly (reading relational columns, building nested JSON)

### 29.4 Concrete SQL Example: CPU Recommendation

```sql
CREATE OR REPLACE FUNCTION recommend_cpu(
    p_org_id TEXT,
    p_cluster_uuid UUID,
    p_timeframe_start TIMESTAMPTZ,
    p_timeframe_end TIMESTAMPTZ,
    p_decay_lambda DOUBLE PRECISION DEFAULT 0.03,
    p_min_margin DOUBLE PRECISION DEFAULT 1.15
)
RETURNS TABLE (
    container_id TEXT,
    rec_request_mc  INTEGER,
    rec_limit_mc    INTEGER,
    is_idle         BOOLEAN,
    variation_cv    DOUBLE PRECISION,
    trend_slope     DOUBLE PRECISION
)
LANGUAGE sql STABLE AS $$
WITH merged AS (
    SELECT
        m.container_id,
        approx_percentile(0.50, rollup(m.cpu_digest)) AS p50,
        approx_percentile(0.95, rollup(m.cpu_digest)) AS p95,
        approx_percentile(0.98, rollup(m.cpu_digest)) AS p98,
        approx_percentile(0.999, rollup(m.cpu_digest)) AS p999,
        mean(rollup(m.cpu_digest))                     AS mean_cpu
    FROM  daily_cpu_digest m          -- continuous aggregate
    WHERE m.org_id       = p_org_id
      AND m.cluster_uuid = p_cluster_uuid
      AND m.bucket       >= p_timeframe_start
      AND m.bucket       <  p_timeframe_end
    GROUP BY m.container_id
),
with_margin AS (
    SELECT *,
        GREATEST(
            p_min_margin,
            1.0 + (p95 - p50) / NULLIF(mean_cpu, 0)
        ) AS margin
    FROM merged
),
with_trend AS (
    SELECT
        wm.*,
        ts.slope
    FROM with_margin wm
    LEFT JOIN LATERAL (
        SELECT regr_slope(approx_percentile(0.98, m.cpu_digest),
                          extract(epoch FROM m.bucket)) AS slope
        FROM   daily_cpu_digest m
        WHERE  m.org_id       = p_org_id
          AND  m.cluster_uuid = p_cluster_uuid
          AND  m.container_id = wm.container_id
          AND  m.bucket       >= p_timeframe_start
          AND  m.bucket       <  p_timeframe_end
    ) ts ON true
)
SELECT
    container_id,
    GREATEST(1, ROUND(p98 * margin)::INTEGER)   AS rec_request_mc,
    GREATEST(1, ROUND(p999 * 1.05)::INTEGER)    AS rec_limit_mc,
    (p98 < 10)                                   AS is_idle,
    (p95 - p50) / NULLIF(mean_cpu, 0)            AS variation_cv,
    slope                                        AS trend_slope
FROM with_trend;
$$;
```

This function processes **all containers for a cluster in a single invocation**. For 50K containers, PostgreSQL scans the continuous aggregate once, merges digests in-process, and returns 50K rows — all without any data leaving the database.

### 29.5 Batch Recommendation Entry Point

The Go orchestrator calls a single stored procedure to compute all container recommendations:

```sql
CREATE OR REPLACE PROCEDURE recommend_all_containers(
    p_org_id TEXT,
    p_cluster_uuid UUID,
    p_start TIMESTAMPTZ,
    p_end TIMESTAMPTZ
)
LANGUAGE plpgsql AS $$
BEGIN
    -- CPU recommendations (upsert)
    INSERT INTO container_recommendations
        (org_id, cluster_uuid, container_id,
         cpu_request_mc, cpu_limit_mc, cpu_idle, cpu_cv, cpu_trend)
    SELECT org_id, cluster_uuid, r.*
    FROM   recommend_cpu(p_org_id, p_cluster_uuid, p_start, p_end) r
    CROSS JOIN (SELECT p_org_id AS org_id, p_cluster_uuid AS cluster_uuid) params
    ON CONFLICT (org_id, cluster_uuid, container_id) DO UPDATE SET
        cpu_request_mc = EXCLUDED.cpu_request_mc,
        cpu_limit_mc   = EXCLUDED.cpu_limit_mc,
        cpu_idle       = EXCLUDED.cpu_idle,
        cpu_cv         = EXCLUDED.cpu_cv,
        cpu_trend      = EXCLUDED.cpu_trend,
        updated_at     = now();

    -- Memory recommendations (similar pattern)
    INSERT INTO container_recommendations
        (org_id, cluster_uuid, container_id,
         mem_request_kib, mem_limit_kib, mem_idle, mem_cv, mem_trend)
    SELECT org_id, cluster_uuid, r.*
    FROM   recommend_memory(p_org_id, p_cluster_uuid, p_start, p_end) r
    CROSS JOIN (SELECT p_org_id AS org_id, p_cluster_uuid AS cluster_uuid) params
    ON CONFLICT (org_id, cluster_uuid, container_id) DO UPDATE SET
        mem_request_kib = EXCLUDED.mem_request_kib,
        mem_limit_kib   = EXCLUDED.mem_limit_kib,
        mem_idle        = EXCLUDED.mem_idle,
        mem_cv          = EXCLUDED.mem_cv,
        mem_trend       = EXCLUDED.mem_trend,
        updated_at      = now();
END;
$$;
```

From Go, the entire recommendation cycle for a cluster is:

```go
_, err := db.Exec(ctx,
    "CALL recommend_all_containers($1, $2, $3, $4)",
    orgID, clusterUUID, start, end)
```

One SQL call. Zero data round-trips for the core math.

### 29.6 Performance Impact


| Metric                              | Go-side (previous estimate)           | PL/pgSQL hybrid             | Improvement   |
| ----------------------------------- | ------------------------------------- | --------------------------- | ------------- |
| **Rec step for 50K containers**     | ~0.85s (Go reads + computes + writes) | ~0.1-0.2s (single SQL scan) | **4-8x**      |
| **Rec step for 20M containers**     | ~~340s (~~5.7 min)                    | ~~40-85s (~~1 min)          | **4-8x**      |
| **Data round-trips per cluster**    | 2 × N_containers (read + write)       | 1 (single CALL)             | **N → 1**     |
| **Go heap allocations**             | 50K digest objects + 50K rec structs  | 0 (all server-side)         | **~100K → 0** |
| **Custom timeframe re-computation** | Same as above (Go loop)               | Same `WHERE` range change   | Same 4-8x     |


**Why 4-8x and not more:** The continuous aggregate scan is the dominant cost regardless of whether Go or PostgreSQL reads it. The savings come from eliminating 100K round-trips and 100K heap allocations, not from a faster scan algorithm.

### 29.7 What Remains in Go


| Component              | Why Go, not SQL                                                       |
| ---------------------- | --------------------------------------------------------------------- |
| GPU MIG bin-packing    | Complex lookup table + multi-dimensional fitting; not a set operation |
| JVM/Quarkus tuning     | Runtime-specific heuristics with branchy logic and external knowledge |
| HPA optimization       | Requires reading K8s API objects + mapping to rec adjustments         |
| OOM event ingestion    | I/O-bound CSV parsing + `COPY FROM`                                   |
| Notification assembly  | String building, severity classification, human-readable messages     |
| API response building  | HTTP handlers, reading relational columns, assembling nested JSON     |
| Kafka orchestration    | Message consumption, acknowledgement, error handling                  |
| CSV → TimescaleDB COPY | I/O-bound, already optimal                                            |


### 29.8 Migration and Testing Strategy

1. **Phase 1:** Implement `recommend_cpu()` and `recommend_memory()` as SQL functions. Run them alongside Go computation and compare results for N clusters.
2. **Phase 2:** Once validated, switch the hot path to `CALL recommend_all_containers()`. Keep Go fallback behind a feature flag.
3. **Phase 3:** Add PVC, idle, QoS, namespace quota SQL functions incrementally.
4. **Phase 4:** Remove Go computation code for CPU/memory.

**Testing approach:**

- **Unit test SQL functions** using `pgTAP` or plain `SELECT` assertions against known digest inputs
- **Shadow-mode comparison:** Run both Go and SQL paths, assert results match within rounding tolerance
- **Load test:** Benchmark `recommend_all_containers()` with 50K and 200K container datasets

### 29.9 Risks and Mitigations


| Risk                                                              | Mitigation                                                                                                                     |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| PL/pgSQL debugging is harder than Go                              | Use `RAISE NOTICE` for logging; `pgTAP` for unit tests; shadow-mode for validation                                             |
| `rollup()` behavior changes across TimescaleDB versions           | Pin TimescaleDB Toolkit version; test against specific versions in CI                                                          |
| Complex margin/decay formulas become unreadable in SQL            | Extract into small named SQL functions (`compute_iqr_cv_margin()`, `apply_decay_weight()`)                                     |
| SQL function performance degrades on unexpected data distribution | `EXPLAIN ANALYZE` on real datasets; add indexes on `(org_id, cluster_uuid, bucket)` (already needed for continuous aggregates) |
| Team unfamiliar with PL/pgSQL                                     | ~60% is plain SQL; PL/pgSQL is only the `PROCEDURE` wrapper for `BEGIN ... END` control flow                                   |


### 29.10 Updated Architecture Diagram

```
CSV Payload (from operator)
 │
 ▼
┌───────────────────────────────────────────┐
│           ros-ocp-backend (Go)            │
│                                           │
│  Kafka → CSV parse → COPY FROM ──────────────┐
│                                           │  │
│  GPU / JVM / HPA recs (Go heuristics) ───────┤
│                                           │  │
│  Notifications (Go string building) ─────────┤
│                                           │  │
│  API serving (Go HTTP handlers) ◄────────────┤
└───────────────────────────────────────────┘  │
                                               ▼
┌──────────────────────────────────────────────────────┐
│    PostgreSQL + TimescaleDB                          │
│                                                      │
│  ros_metrics hypertable (COPY FROM target)           │
│       │                                              │
│       ▼                                              │
│  continuous aggregates (daily t-digests, auto)       │
│       │                                              │
│       ▼                                              │
│  recommend_cpu() ──► container_recommendations       │
│  recommend_memory()  (relational columns)            │
│  recommend_pvc()                                     │
│  detect_idle()                                       │
│  recommend_namespace_quota()                         │
│                                                      │
│  CALL recommend_all_containers()                     │
│    → 1 SQL call, 0 data round-trips                 │
└──────────────────────────────────────────────────────┘
```

### 29.11 Conclusion

Moving the core recommendation math into PL/pgSQL/SQL functions is a natural extension of the TimescaleDB strategy. The data is already there, the functions (t-digest merge, percentile extraction, linear regression) are already there, and the results stay there. This eliminates the largest remaining inefficiency: moving 50K+ rows of data out of and back into the database for each recommendation cycle. The estimated 4-8x improvement on the recommendation step compounds with all other optimizations (TimescaleDB ingestion, relational columns, integer types) for an end-to-end improvement of **~7600-15200x** vs the current architecture at the 50K container scale.

---

## 30. OpenShift Virtualization VM Recommendations

> **v4.0 alignment:** Shipped **container** recommendations use **native Go** (`recommendCPU()`, `recommendMemory()`, `recommendAllWorkloads()`, …) and **plain PostgreSQL 16+** — no PL/pgSQL recommendation functions, no TimescaleDB, no t-digest. **VM** right-sizing is **not** fully shipped as of this writing; the SQL/PL/pgSQL fragments later in this section are **illustrative** of an older hybrid sketch (aligned with superseded §29). The **intended** implementation matches v4.0: **`recommendVM()` in Go**, partitioned tables for VM daily digests and results, SQL for storage/retrieval only. Tables §30.9–§30.10 below describe that target; code samples remain as historical SQL examples.

### 30.1 Current State: VMs in the Ecosystem

OpenShift Virtualization (KubeVirt) runs virtual machines as pods with the `virt-launcher` process. The Koku ecosystem already handles VMs for **cost accounting** but has **zero ROS (recommendation) support**.

#### What works today


| Component                            | VM Support              | How                                                                                                                                                                                                         |
| ------------------------------------ | ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **koku-metrics-operator**            | **Cost metrics only**   | 12 `cost:vm_`* Prometheus queries against `kubevirt_*` metrics; produces `cm-openshift-vm-usage-<YYYYMM>.csv` with CPU, memory, disk size, instance type, guest OS, labels                                  |
| **Koku backend**                     | **Cost pipeline + API** | `OCPVirtualMachineSummaryP` table, `reporting_ocp_vm_summary_p` UI summary, VM cost model rates (`vm_cost_per_month`, `vm_core_cost_per_hour`), REST API at `reports/openshift/resources/virtual-machines/` |
| **Koku backend (VM identification)** | **Pod label**           | VMs identified via `pod_labels->>'vm_kubevirt_io_name'` (the JSON-encoded form of the Kubernetes label `vm.kubevirt.io/name`). SQL: `all_labels ? 'vm_kubevirt_io_name'`                                    |


#### What does NOT work


| Component                 | VM Support            | Gap                                                                                                                                                                                 |
| ------------------------- | --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **koku-metrics-operator** | **No ROS VM queries** | No `ros:vm_`* queries exist. No disk IOPS/throughput/latency metrics collected.                                                                                                     |
| **ros-ocp-backend**       | **Filters out VMs**   | VM data is handled separately via dedicated VM endpoints. The `workload_type` filter accepts any valid Kubernetes workload kind (no fixed allowlist since COST-7274). |
| **Kruize autotune**       | **No VM awareness**   | Generic `K8sObject` + `containerDataMap`. No `kubevirt`, `VirtualMachine`, or `vmi` references in the codebase.                                                                     |


### 30.2 Available KubeVirt Prometheus Metrics

KubeVirt exposes rich metrics via `kubevirt_vmi_`* and `kubevirt_vm_*` exporters. The operator already uses some for cost; the ROS pipeline needs to collect additional ones.

#### Already collected (cost pipeline)


| Metric               | Prometheus Series                                                                                     | Operator Query                                               |
| -------------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| CPU request/limit    | `kubevirt_vm_resource_requests{resource='cpu'}` / `kubevirt_vm_resource_limits{resource='cpu'}`       | `cost:vm_cpu_request_cores`, `cost:vm_cpu_limit_cores`       |
| CPU usage            | `kubevirt_vmi_cpu_usage_seconds_total`                                                                | `cost:vm_cpu_usage` (via `rate()[5m]`)                       |
| Memory request/limit | `kubevirt_vm_resource_requests{resource='memory'}` / `kubevirt_vm_resource_limits{resource='memory'}` | `cost:vm_memory_request_bytes`, `cost:vm_memory_limit_bytes` |
| Memory usage         | `kubevirt_vmi_memory_used_bytes`                                                                      | `cost:vm_memory_usage_bytes` (via `sum_over_time()[5m]`)     |
| Disk allocated size  | `kubevirt_vm_disk_allocated_size_bytes`                                                               | `cost:vm_disk_allocated_size_bytes`                          |
| VM info              | `kubevirt_vmi_info{phase='running'}`                                                                  | `cost:vm_info` (instance_type, OS, guest_os)                 |
| VM labels            | `kubevirt_vm_labels`                                                                                  | `cost:vm_labels`                                             |


#### NOT collected (needed for ROS)


| Metric                    | Prometheus Series                                | Purpose                                         |
| ------------------------- | ------------------------------------------------ | ----------------------------------------------- |
| **Disk read IOPS**        | `kubevirt_vmi_storage_iops_read_total`           | Storage performance recommendation              |
| **Disk write IOPS**       | `kubevirt_vmi_storage_iops_write_total`          | Storage performance recommendation              |
| **Disk read throughput**  | `kubevirt_vmi_storage_read_traffic_bytes_total`  | Storage throughput recommendation               |
| **Disk write throughput** | `kubevirt_vmi_storage_write_traffic_bytes_total` | Storage throughput recommendation               |
| **Disk read latency**     | `kubevirt_vmi_storage_read_times_ms_total`       | Storage class recommendation                    |
| **Disk write latency**    | `kubevirt_vmi_storage_write_times_ms_total`      | Storage class recommendation                    |
| **Memory available**      | `kubevirt_vmi_memory_available_bytes`            | Memory headroom analysis                        |
| **Network receive**       | `kubevirt_vmi_network_receive_bytes_total`       | Informational (future: network-aware placement) |
| **Network transmit**      | `kubevirt_vmi_network_transmit_bytes_total`      | Informational (future: network-aware placement) |


All of these are **standard KubeVirt metrics** available on any OpenShift Virtualization cluster.

### 30.3 Why VMs Need Different Recommendations Than Containers


| Aspect                    | Containers                                  | VMs                                                                     |
| ------------------------- | ------------------------------------------- | ----------------------------------------------------------------------- |
| **Resource granularity**  | Millicores, KiB (continuous)                | Whole vCPUs, whole GiB (discrete steps)                                 |
| **Recommendation output** | "Set CPU request to 250m"                   | "Resize VM to 4 vCPUs, 16 GiB RAM"                                      |
| **Disk recommendations**  | PVC right-sizing only (§23.2)               | Disk size + IOPS + throughput + storage class                           |
| **Resize cost**           | Zero-downtime (pod restart, rolling update) | **Requires VM restart** or live migration                               |
| **Instance types**        | N/A                                         | VM may use `VirtualMachineInstancetype` catalog                         |
| **Stability profile**     | Can be bursty, short-lived                  | Usually long-running, more stable                                       |
| **Overprovisioning norm** | 2-10x common                                | **5-20x common** (VM sprawl is the #1 virtualization problem)           |
| **Guest OS awareness**    | N/A                                         | Guest OS type affects memory recommendation (Linux vs Windows baseline) |
| **IOPS/throughput**       | Not applicable                              | Primary concern for database/storage VMs                                |


### 30.4 Proposed Operator Queries (New `ros:vm_`* Prefix)

These are 15-minute interval queries (same cadence as existing ROS container queries), producing a new CSV file `ros-openshift-vm-usage-<YYYYMM>.csv`.

```promql
# CPU (reuse existing kubevirt metrics, ROS granularity)
ros:vm_cpu_usage_cores:
  rate(kubevirt_vmi_cpu_usage_seconds_total{name!='', namespace!=''}[5m])
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_cpu_request_cores:
  kubevirt_vm_resource_requests{name!='', namespace!='', resource='cpu', unit='cores'}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_cpu_limit_cores:
  kubevirt_vm_resource_limits{name!='', namespace!='', resource='cpu'}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

# Memory
ros:vm_memory_usage_bytes:
  kubevirt_vmi_memory_used_bytes{name!='', namespace!=''}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_memory_available_bytes:
  kubevirt_vmi_memory_available_bytes{name!='', namespace!=''}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_memory_request_bytes:
  kubevirt_vm_resource_requests{name!='', namespace!='', resource='memory'}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

# Disk IOPS (NEW metrics)
ros:vm_disk_read_iops:
  sum by (name, namespace, drive)
    (rate(kubevirt_vmi_storage_iops_read_total{name!='', namespace!=''}[5m]))
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_disk_write_iops:
  sum by (name, namespace, drive)
    (rate(kubevirt_vmi_storage_iops_write_total{name!='', namespace!=''}[5m]))
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

# Disk throughput (NEW metrics)
ros:vm_disk_read_bytes_per_sec:
  sum by (name, namespace, drive)
    (rate(kubevirt_vmi_storage_read_traffic_bytes_total{name!='', namespace!=''}[5m]))
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

ros:vm_disk_write_bytes_per_sec:
  sum by (name, namespace, drive)
    (rate(kubevirt_vmi_storage_write_traffic_bytes_total{name!='', namespace!=''}[5m]))
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

# Disk size (reuse existing metric)
ros:vm_disk_allocated_bytes:
  kubevirt_vm_disk_allocated_size_bytes{name!='', namespace!=''}
    * on (name, namespace) group_left
    max by (name, namespace) (kubevirt_vmi_info{phase='running'})

# VM info (for instance type matching)
ros:vm_info:
  sum by (name, namespace, node, os, instance_type, guest_os_name, guest_os_version_id, guest_os_arch)
    (kubevirt_vmi_info{phase='running'})
    * on(node) group_left(provider_id)
    max by (node, provider_id) (kube_node_info)
```

**Total: ~12 new queries** (some reuse existing `kubevirt_`* series at ROS granularity, 4 are completely new disk I/O series).

### 30.5 VM Recommendation Algorithm

#### CPU Recommendation

VMs allocate CPU in **whole vCPU units** (not millicores). The algorithm:

```
p95_cpu_cores = approx_percentile(0.95, rollup(cpu_digest))
margin = GREATEST(1.15, 1.0 + iqr_cv(cpu_digest))
rec_vcpus = CEIL(p95_cpu_cores * margin)                   -- round UP to whole vCPU
rec_vcpus = GREATEST(1, rec_vcpus)                         -- minimum 1 vCPU
```

**Hysteresis:** Only recommend downsizing if `current_vcpus - rec_vcpus >= 2` or `rec_vcpus / current_vcpus < 0.6` (i.e., at least 40% oversized). This avoids churn for marginal savings, since VM resizing requires a restart.

#### Memory Recommendation

VMs allocate memory in **whole GiB units**. Guest OS baseline varies:

```
p95_mem_gib = approx_percentile(0.95, rollup(mem_digest)) / (1024 * 1024)
margin = GREATEST(1.20, 1.0 + iqr_cv(mem_digest))         -- 20% minimum for VMs
guest_os_baseline_gib = CASE
    WHEN guest_os_name ILIKE '%windows%' THEN 2            -- Windows needs ~2 GiB baseline
    ELSE 0.5                                                -- Linux baseline
END
rec_mem_gib = CEIL(GREATEST(p95_mem_gib * margin, guest_os_baseline_gib))
rec_mem_gib = GREATEST(1, rec_mem_gib)                     -- minimum 1 GiB
```

**Hysteresis:** Same threshold as CPU — only recommend change if ≥40% oversized or ≥2 GiB difference.

#### Disk Size Recommendation

```
max_disk_usage = MAX(disk_allocated_bytes) over window
growth_rate = regr_slope(disk_allocated_bytes, epoch) over window   -- bytes/sec
projected_30d = max_disk_usage + (growth_rate * 30 * 86400)
margin = 1.25                                              -- 25% headroom
rec_disk_gib = CEIL(projected_30d * margin / (1024^3))
rec_disk_gib = CEIL(rec_disk_gib / 10.0) * 10             -- round to nearest 10 GiB
```

#### Disk IOPS Recommendation (Informational)

IOPS recommendations are **informational** (not actionable as a resize — they inform storage class selection):

```
iops_read_p95  = approx_percentile(0.95, rollup(disk_read_iops_digest))
iops_write_p95 = approx_percentile(0.95, rollup(disk_write_iops_digest))
iops_total_p95 = iops_read_p95 + iops_write_p95

throughput_read_p95_mbs  = approx_percentile(0.95, rollup(disk_read_bps_digest)) / (1024^2)
throughput_write_p95_mbs = approx_percentile(0.95, rollup(disk_write_bps_digest)) / (1024^2)
```

The recommendation reports these as:

- "p95 IOPS: 450 read + 120 write = 570 total"
- "p95 throughput: 85 MB/s read + 22 MB/s write"
- Storage class suggestion: if `iops_total_p95 > 3000`, suggest high-IOPS storage class

#### Idle VM Detection

```
is_idle = (p95_cpu_cores < 0.05 AND p95_mem_usage < guest_os_baseline * 1.1)
is_abandoned = is_idle AND (last_seen > 7 days ago OR uptime > 30 days with no usage change)
```

Idle VMs are the **single largest source of waste** in OpenShift Virtualization environments. Industry data (CAST AI, Densify, VMware vRealize) consistently shows 20-40% of VMs in enterprise environments are idle or abandoned.

#### Instance Type Recommendation (Optional)

If the cluster uses `VirtualMachineInstancetype` resources, the algorithm can optionally recommend the closest-fit instance type:

```sql
SELECT name, vcpus, memory_gib
FROM vm_instance_types
WHERE vcpus >= rec_vcpus AND memory_gib >= rec_mem_gib
ORDER BY vcpus + memory_gib ASC  -- smallest fit
LIMIT 1;
```

This requires the operator to collect available instance types (1 new query: `kubevirt_vm_instance_type_info`), which is a Phase 2 enhancement.

### 30.6 TimescaleDB Schema

```sql
CREATE TABLE ros_vm_metrics (
    ts                      TIMESTAMPTZ NOT NULL,
    org_id                  TEXT NOT NULL,
    cluster_uuid            TEXT NOT NULL,
    namespace               TEXT NOT NULL,
    vm_name                 TEXT NOT NULL,
    node                    TEXT,
    -- CPU (millicores for t-digest precision, display as vCPUs)
    cpu_usage_mc            INT,
    cpu_request_mc          INT,
    cpu_limit_mc            INT,
    -- Memory (KiB)
    mem_usage_kib           BIGINT,
    mem_available_kib       BIGINT,
    mem_request_kib         BIGINT,
    mem_limit_kib           BIGINT,
    -- Disk I/O (per-drive aggregated to VM level)
    disk_read_iops          INT,
    disk_write_iops         INT,
    disk_read_bytes_sec     BIGINT,
    disk_write_bytes_sec    BIGINT,
    disk_allocated_bytes    BIGINT,
    -- VM metadata
    vm_instance_type        TEXT,
    vm_os                   TEXT,
    guest_os_name           TEXT,
    guest_os_arch           TEXT
);
SELECT create_hypertable('ros_vm_metrics', by_range('ts'));

-- Continuous aggregate: daily VM digests
CREATE MATERIALIZED VIEW daily_vm_digests
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', ts) AS bucket,
    org_id, cluster_uuid, namespace, vm_name,
    tdigest(200, cpu_usage_mc)          AS cpu_digest,
    tdigest(200, mem_usage_kib)         AS mem_digest,
    tdigest(200, disk_read_iops)        AS disk_read_iops_digest,
    tdigest(200, disk_write_iops)       AS disk_write_iops_digest,
    tdigest(200, disk_read_bytes_sec)   AS disk_read_bps_digest,
    tdigest(200, disk_write_bytes_sec)  AS disk_write_bps_digest,
    max(disk_allocated_bytes)           AS disk_allocated_max,
    max(cpu_request_mc)                 AS cpu_request_mc,
    max(mem_request_kib)                AS mem_request_kib,
    max(mem_limit_kib)                  AS mem_limit_kib,
    count(*)                            AS sample_count
FROM ros_vm_metrics
GROUP BY bucket, org_id, cluster_uuid, namespace, vm_name;
```

### 30.7 PL/pgSQL: `recommend_vm()`

```sql
CREATE OR REPLACE FUNCTION recommend_vm(
    p_org_id TEXT, p_cluster_uuid UUID,
    p_start TIMESTAMPTZ, p_end TIMESTAMPTZ,
    p_cpu_hysteresis REAL DEFAULT 0.60,
    p_mem_hysteresis REAL DEFAULT 0.60
) RETURNS TABLE (
    vm_name           TEXT,
    -- CPU
    current_vcpus     INT,
    rec_vcpus         INT,
    cpu_util_p95      REAL,
    -- Memory
    current_mem_gib   INT,
    rec_mem_gib       INT,
    mem_util_p95      REAL,
    -- Disk
    disk_allocated_gib INT,
    rec_disk_gib      INT,
    disk_growth_trend TEXT,
    -- IOPS (informational)
    iops_read_p95     INT,
    iops_write_p95    INT,
    throughput_read_mbs REAL,
    throughput_write_mbs REAL,
    -- Flags
    is_idle           BOOLEAN,
    is_oversized      BOOLEAN
) LANGUAGE sql STABLE AS $$
WITH merged AS (
    SELECT
        d.vm_name,
        -- CPU
        approx_percentile(0.95, rollup(d.cpu_digest)) AS cpu_p95_mc,
        approx_percentile(0.50, rollup(d.cpu_digest)) AS cpu_p50_mc,
        mean(rollup(d.cpu_digest))                     AS cpu_mean_mc,
        max(d.cpu_request_mc)                          AS cpu_request_mc,
        -- Memory
        approx_percentile(0.95, rollup(d.mem_digest)) AS mem_p95_kib,
        max(d.mem_request_kib)                         AS mem_request_kib,
        max(d.mem_limit_kib)                           AS mem_limit_kib,
        -- Disk IOPS
        approx_percentile(0.95, rollup(d.disk_read_iops_digest))  AS iops_rd_p95,
        approx_percentile(0.95, rollup(d.disk_write_iops_digest)) AS iops_wr_p95,
        approx_percentile(0.95, rollup(d.disk_read_bps_digest))   AS bps_rd_p95,
        approx_percentile(0.95, rollup(d.disk_write_bps_digest))  AS bps_wr_p95,
        -- Disk size
        max(d.disk_allocated_max)                      AS disk_max,
        sum(d.sample_count)                            AS total_samples
    FROM daily_vm_digests d
    WHERE d.org_id = p_org_id AND d.cluster_uuid = p_cluster_uuid::TEXT
      AND d.bucket >= p_start AND d.bucket < p_end
    GROUP BY d.vm_name
),
with_recs AS (
    SELECT
        m.vm_name,
        -- Current (convert mc→vCPU, kib→GiB)
        CEIL(m.cpu_request_mc / 1000.0)::INT                     AS current_vcpus,
        CEIL(m.mem_request_kib / (1024.0 * 1024.0))::INT         AS current_mem_gib,
        -- CPU rec
        GREATEST(1, CEIL(m.cpu_p95_mc *
            GREATEST(1.15, 1.0 + (m.cpu_p95_mc - m.cpu_p50_mc) / NULLIF(m.cpu_mean_mc, 0))
            / 1000.0))::INT                                       AS rec_vcpus,
        (m.cpu_p95_mc / NULLIF(m.cpu_request_mc, 0)::REAL * 100) AS cpu_util_p95,
        -- Memory rec
        GREATEST(1, CEIL(m.mem_p95_kib * 1.20 / (1024.0 * 1024.0)))::INT AS rec_mem_gib,
        (m.mem_p95_kib / NULLIF(m.mem_request_kib, 0)::REAL * 100)       AS mem_util_p95,
        -- Disk
        CEIL(m.disk_max / (1024.0^3))::INT                       AS disk_allocated_gib,
        CEIL(CEIL(m.disk_max * 1.25 / (1024.0^3)) / 10.0)::INT * 10 AS rec_disk_gib,
        'stable'::TEXT                                            AS disk_growth_trend,
        -- IOPS
        m.iops_rd_p95::INT, m.iops_wr_p95::INT,
        (m.bps_rd_p95 / (1024.0^2))::REAL, (m.bps_wr_p95 / (1024.0^2))::REAL,
        -- Idle
        (m.cpu_p95_mc < 50 AND m.mem_p95_kib < 512 * 1024)      AS is_idle,
        m.cpu_request_mc, m.mem_request_kib, m.cpu_p95_mc
    FROM merged m
)
SELECT
    r.vm_name, r.current_vcpus, r.rec_vcpus, r.cpu_util_p95,
    r.current_mem_gib, r.rec_mem_gib, r.mem_util_p95,
    r.disk_allocated_gib, r.rec_disk_gib, r.disk_growth_trend,
    r.iops_rd_p95, r.iops_wr_p95, r.bps_rd_p95, r.bps_wr_p95,
    r.is_idle,
    (r.rec_vcpus::REAL / NULLIF(r.current_vcpus, 0) < p_cpu_hysteresis
     OR r.rec_mem_gib::REAL / NULLIF(r.current_mem_gib, 0) < p_mem_hysteresis) AS is_oversized
FROM with_recs r;
$$;
```

### 30.8 Integration with `recommend_all_containers()`

Extend the batch entry point to include VM recommendations:

```sql
CREATE OR REPLACE PROCEDURE recommend_all_workloads(
    p_org_id TEXT, p_cluster_uuid UUID,
    p_start TIMESTAMPTZ, p_end TIMESTAMPTZ
) LANGUAGE plpgsql AS $$
BEGIN
    -- Container recommendations (existing)
    CALL recommend_all_containers(p_org_id, p_cluster_uuid, p_start, p_end);

    -- VM recommendations (new)
    INSERT INTO vm_recommendations
        (org_id, cluster_uuid, vm_name, ...)
    SELECT p_org_id, p_cluster_uuid, r.*
    FROM recommend_vm(p_org_id, p_cluster_uuid::UUID, p_start, p_end) r
    ON CONFLICT (org_id, cluster_uuid, vm_name) DO UPDATE SET
        rec_vcpus = EXCLUDED.rec_vcpus,
        rec_mem_gib = EXCLUDED.rec_mem_gib,
        rec_disk_gib = EXCLUDED.rec_disk_gib,
        -- ... all other columns ...
        updated_at = now();
END;
$$;
```

### 30.9 Key Differences from Container Pipeline


| Aspect                  | Container Pipeline (v4.0 target)           | VM Pipeline (v4.0 target)                                        |
| ----------------------- | --------------------------------------- | -------------------------------------------------- |
| CSV source              | `ros-openshift-pod-usage-*.csv`         | `ros-openshift-vm-usage-*.csv` (new)               |
| Identification          | `workload_type` + `container_name`      | `vm_name` + `namespace` (from `kubevirt_vmi_info`) |
| Metrics / digest storage | Partitioned PG tables (interval + daily digests) | Partitioned PG tables for VM metrics + `daily_vm_digests` (plain PostgreSQL, not TimescaleDB) |
| Pre-aggregation        | Daily digest rows (Go-maintained)       | Daily VM digest rows (Go-maintained)               |
| Recommendation entry   | `recommendCPU()`, `recommendMemory()`, `recommendAllWorkloads()` (Go) | `recommendVM()` (Go; unified CPU + mem + disk)        |
| Output units            | Millicores, KiB                         | Whole vCPUs, whole GiB, IOPS                       |
| Resize threshold        | Any improvement                         | ≥40% oversized (VM restart cost)                   |
| Disk recommendations    | N/A (PVC handled separately)            | Size + IOPS + throughput                           |
| Guest OS awareness      | N/A                                     | Windows baseline memory (2 GiB), Linux (0.5 GiB)   |


### 30.10 Phasing


| Phase               | Work                                                                                             | Dependencies                          | Effort                 |
| ------------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------- | ---------------------- |
| **VM-A: Operator**  | Add ~12 `ros:vm_`* Prometheus queries, new VM ROS CSV format                                     | None                                  | Low (2-3 PRs)          |
| **VM-B: Ingestion** | VM metrics + daily digest **partitioned tables** in plain PostgreSQL, VM CSV parser, `COPY FROM` / batch upsert | Container v4.0 digest pipeline patterns | Low-Moderate (2-3 PRs) |
| **VM-C: Algorithm** | `recommendVM()` in **Go** (percentiles via `slices.Sort()` or equivalent on digest samples), VM-specific thresholds | VM-B                                  | Moderate (2-3 PRs)     |
| **VM-D: API**       | Extend recommendation API with `vm_recommendations` section, `vm_name` filter                    | VM-C                                  | Low (2 PRs)            |


**VM-A can start immediately** in parallel with all other superpowers work. The operator changes are independent.

### 30.11 Industry Context

VM right-sizing is a **table-stakes feature** for virtualization optimization tools:


| Tool                       | VM Right-Sizing                   | Disk IOPS | Idle VM Detection |
| -------------------------- | --------------------------------- | --------- | ----------------- |
| **VMware vRealize / Aria** | Yes (vCPU, memory)                | Yes       | Yes               |
| **CAST AI**                | Yes (for cloud VMs)               | No        | Yes               |
| **Densify**                | Yes (vCPU, memory, storage)       | Yes       | Yes               |
| **Turbonomic (IBM)**       | Yes (vCPU, memory, storage, IOPS) | Yes       | Yes               |
| **CloudHealth (VMware)**   | Yes (cloud VMs)                   | Partial   | Yes               |
| **Kruize / ROS (current)** | **No**                            | **No**    | **No**            |


VM sprawl is consistently cited as the #1 cost problem in virtualization environments. Industry reports (Flexera, Densify) estimate 20-40% of enterprise VMs are idle or significantly oversized. For OpenShift Virtualization specifically, this is an untapped recommendation category.

### 30.12 Confidence Assessment


| Claim                                                        | Confidence    | Basis                                                                                                                                                                          |
| ------------------------------------------------------------ | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| KubeVirt exposes IOPS/throughput metrics                     | **Very High** | `kubevirt_vmi_storage_iops_{read,write}_total` and `kubevirt_vmi_storage_{read,write}_traffic_bytes_total` are standard KubeVirt metrics documented in upstream KubeVirt docs. |
| Operator cost queries already collect CPU/memory/disk size   | **Very High** | Verified: `internal/collector/queries.go` lines 32-43 define 12 `cost:vm_`* queries.                                                                                           |
| ros-ocp-backend filters out VMs                              | **Very High** | Verified: `internal/utils/aggregator.go` whitelist does not include any KubeVirt/VM workload type.                                                                             |
| 20-40% of VMs are idle/oversized in enterprise environments  | **High**      | Cited by Flexera 2024 State of the Cloud, Densify reports, and VMware vRealize best practices.                                                                                 |
| VM recommendation algorithm needs discrete rounding          | **Very High** | Structural: VMs are configured in whole vCPU and GiB units. KubeVirt `VirtualMachine` spec uses integer vCPU counts and GiB/MiB memory amounts.                                |
| VM resize requires restart or live migration                 | **Very High** | KubeVirt documentation: changing VM resource allocation requires `VirtualMachine` spec update + restart (or hot-plug, which is limited to adding vCPUs/memory, not removing).  |
| Disk IOPS percentiles are useful for storage class selection | **High**      | Standard practice in VMware/Turbonomic: match IOPS profile to storage tier. OpenShift storage classes map to different performance tiers.                                      |


---

## Appendix A: Operator ROS Query Inventory

The koku-metrics-operator (`internal/collector/queries.go`) defines 53 `ros:`-prefixed Prometheus queries organized into three groups.

### Namespace Filter (1 query)

`ros:namespace_filter` — Gating query. Checks for namespaces with `label_insights_cost_management_optimizations=true` or `label_cost_management_optimizations=true`. Excludes `kube-`*, `openshift`, `openshift-*`.

### Container-Level Queries (30 queries → `rosContainerQueries`)

Executed as **instant queries** (`PromConn.Query()`) at 4 timestamps per hour (15-min intervals).


| Category                   | Queries | Source Metric                                                                                  | Aggregations                                     |
| -------------------------- | ------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| Image/owner info           | 2       | `kube_pod_container_info` + `kube_pod_owner` / `namespace_workload_pod:kube_pod_owner:relabel` | `max_over_time(...[15m])`                        |
| CPU request                | 2       | `kube_pod_container_resource_requests{resource='cpu'}`                                         | avg, sum                                         |
| CPU limit                  | 2       | `kube_pod_container_resource_limits{resource='cpu'}`                                           | avg, sum                                         |
| CPU usage                  | 4       | `node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate`                     | avg, min, max, sum (via `*_over_time(...[15m])`) |
| CPU throttle               | 4       | `container_cpu_cfs_throttled_seconds_total`                                                    | avg, min, max, sum (via `rate(...[15m])`)        |
| Memory request             | 2       | `kube_pod_container_resource_requests{resource='memory'}`                                      | avg, sum                                         |
| Memory limit               | 2       | `kube_pod_container_resource_limits{resource='memory'}`                                        | avg, sum                                         |
| Memory usage (working set) | 4       | `container_memory_working_set_bytes`                                                           | avg, min, max, sum                               |
| Memory RSS                 | 4       | `container_memory_rss`                                                                         | avg, min, max, sum                               |
| GPU core usage %           | 3       | `DCGM_FI_DEV_GPU_UTIL`                                                                         | min, max, avg                                    |
| GPU memory copy %          | 3       | `DCGM_FI_DEV_MEM_COPY_UTIL`                                                                    | min, max, avg                                    |
| GPU frame buffer           | 3       | `DCGM_FI_DEV_FB_USED`                                                                          | min, max, avg                                    |


Output: `ros-openshift-container-YYYYMM.csv` (46 columns per row, keyed by container+pod+namespace).

### Namespace-Level Queries (20 queries → `rosNamespaceQueries`)

Also instant queries at 4 timestamps per hour.


| Category                      | Queries | Source Metric                                                                                  |
| ----------------------------- | ------- | ---------------------------------------------------------------------------------------------- |
| CPU request/limit (quotas)    | 2       | `kube_resourcequota`                                                                           |
| CPU usage                     | 3       | `node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate` (subquery `[15m:]`) |
| CPU throttle                  | 3       | `container_cpu_cfs_throttled_seconds_total` (subquery `[15m:]`)                                |
| Memory request/limit (quotas) | 2       | `kube_resourcequota`                                                                           |
| Memory usage (working set)    | 3       | `container_memory_working_set_bytes` (subquery `[15m:]`)                                       |
| Memory RSS                    | 3       | `container_memory_rss` (subquery `[15m:]`)                                                     |
| Running/total pods            | 4       | `kube_pod_status_phase`, `kube_pod_info` (subquery `[15m:]`)                                   |


Output: `ros-openshift-namespace-YYYYMM.csv` (24 columns per row, keyed by namespace).

### Key Detail: Instant vs Range Queries

`ros:` queries use **instant queries** (`Query()` at a single timestamp) — Prometheus does the aggregation server-side via `*_over_time(...[15m])`. This differs from `cost:` queries which use **range queries** (`QueryRange()`) and the operator aggregates in Go.

The operator executes ROS collection in 4 × 15-minute windows per hour. Each window queries at `ts = interval_end`, with `[15m]` lookback in PromQL aligning to the window.

---

## Appendix B: Kruize Recommendation Logic Details (Legacy)

### Why Percentile Can't Be a Single PromQL Query

Kruize's CPU recommendation computes a **per-interval derived value** that depends on conditional logic across multiple metrics:

```java
// Per interval: combine CPU usage + throttle
double cpuUsage = (cpuUsageMax > 0) ? cpuUsageMax : cpuUsageAvg;
double cpuThrottle = (cpuThrottleMax > 0) ? cpuThrottleMax : cpuThrottleAvg;
double cpuUsageTotal = cpuUsage + cpuThrottle;

if (cpuUsageTotal < 1.0) {                           // small container
    cpuRequestIntervalMax = cpuUsageTotal;
} else {                                              // large container: per-pod
    numPods = cpuUsageSum / cpuUsageAvg;
    cpuUsagePod = (cpuUsageSum + cpuThrottleSum) / numPods;
    cpuRequestIntervalMax = Math.max(cpuUsagePod, cpuUsageTotal);
}
```

This per-pod adjustment and conditional branching requires the raw aggregation fields (avg, sum, max) and can't be expressed as a single PromQL expression.

**Solution for Thanos + PromQL path**: ros-ocp-backend computes this derived value at CSV ingest time and writes it as an additional Thanos metric (`ros_cpu_derived_max`). Kruize then queries:

```promql
quantile_over_time(0.98, ros_cpu_derived_max{container="X"}[91d])
```

**Solution for t-digest path**: ros-ocp-backend computes the derived value and feeds it into a daily t-digest, stored as a compact blob.

**With algorithm fix (§12):** The derived value computation simplifies to `cpuUsageAvg + cpuThrottleAvg` (no per-pod estimation, no 1-core branching). This simpler formula is easily expressed as a PromQL recording rule, potentially eliminating the need for ros-ocp-backend to compute it:

```promql
ros_cpu_effective:avg = ros:cpu_usage:avg + ros:cpu_throttle:avg
```

### Kruize's Idle and Threshold Constants


| Constant                        | Value     | Effect                                                                 |
| ------------------------------- | --------- | ---------------------------------------------------------------------- |
| `CPU_ZERO`                      | 0.0       | → `NOTICE_CPU_RECORDS_ARE_ZERO`, no CPU recommendation                 |
| `CPU_ONE_MILLICORE`             | 0.001     | → `NOTICE_CPU_RECORDS_ARE_IDLE`, no CPU recommendation                 |
| `CPU_ONE_CORE`                  | 1.0       | Below: use max directly. Above: use percentile with per-pod adjustment |
| `MEM_USAGE_BUFFER_DECIMAL`      | 0.2 (20%) | Buffer added to memory percentile                                      |
| `MEM_SPIKE_BUFFER_DECIMAL`      | 0.05 (5%) | Buffer added to memory spike percentile                                |
| `DEFAULT_CPU_THRESHOLD`         | 0.1 (10%) | If recommended within 10% of current → mark as "optimised"             |
| `DEFAULT_MEMORY_THRESHOLD`      | 0.1 (10%) | Same for memory                                                        |
| `COST_CPU_PERCENTILE`           | 60        | Cost model CPU percentile                                              |
| `PERFORMANCE_CPU_PERCENTILE`    | 98        | Performance model CPU percentile                                       |
| `COST_MEMORY_PERCENTILE`        | 100 (max) | Both models use max for memory                                         |
| `PERFORMANCE_MEMORY_PERCENTILE` | 100 (max) | Both models use max for memory                                         |


### Memory Recommendation: Current p100 vs Proposed p95

Both cost and performance models currently use p100 for memory, which is simply `Collections.max()`. In the current algorithm, t-digest is unnecessary — a running max suffices.

However, §16 proposes lowering memory percentile to p95-p98 (enabled by OOM feedback as the safety net). If adopted, the same t-digest infrastructure used for CPU applies to memory as well. Additionally, the proposed algorithm uses t-digest quantiles for adaptive margins (IQR-CV from p25/p50/p75) and tail-aware limits (p99.9), which require the full t-digest even if the primary percentile were left at p100.

### GPU Recommendation: MIG Profile Bin-Packing

The GPU recommendation algorithm is structurally different from CPU and memory. Instead of computing a continuous value, it selects a **MIG (Multi-Instance GPU) profile** — a fixed hardware partition.

**Core logic** (`GenericRecommendationModel.getAcceleratorRequestRecommendation`):

1. Collect per-interval GPU core and memory usage percentages into `ArrayList<Double>`
2. Compute percentile (p60 cost, p98 performance) via `CommonUtils.percentile()` (which sorts the list)
3. Convert to fractions: `coreFraction = percentile / 100`, `memoryFraction = percentile / 100`
4. `AcceleratorMetaDataService.getAcceleratorProfile(model, coreFrac, memFrac)` finds the smallest MIG profile satisfying both

**Supported GPU models and MIG profiles:**

- A100 (40 GB / 80 GB): 7 profile levels
- H100 (80 / 94 / 96 GB): 7 profile levels
- H200 (141 GB): 7 profile levels
- B200 (180 GB): 4 profile levels (1/2/4/8 GPU)
- RTX PRO 5000 (48 GB): 4 profile levels
- RTX PRO 6000 (96 GB): 4 profile levels

**Correctness issue:** `checkIfModelIsKruizeSupportedMIG` only checks for A100/H100/H200, blocking B200 and RTX PRO despite full profile data existing. See §17 for details.

---

## Appendix C: Kubernetes VPA Default Configuration

The following are the VPA recommender's default parameters (from `pkg/recommender/model/cluster.go`):


| Parameter                                  | Default    | Purpose                               |
| ------------------------------------------ | ---------- | ------------------------------------- |
| `TargetCPUPercentile`                      | 0.90 (p90) | CPU recommendation target             |
| `TargetMemoryPercentile`                   | 0.90 (p90) | Memory recommendation target          |
| `SafetyMarginFraction`                     | 0.15 (15%) | Buffer added to percentile            |
| `PodMinCPUMillicores`                      | 25m        | Minimum CPU recommendation per pod    |
| `PodMinMemoryMb`                           | 250 MiB    | Minimum memory recommendation per pod |
| `CpuHistogramDecayHalfLife`                | 24h        | How fast old samples lose weight      |
| `MemoryHistogramDecayHalfLife`             | 24h        | Same for memory                       |
| `OOMBumpUpRatio`                           | 1.2        | Memory increase on OOM (20%)          |
| `OOMMinBumpUp`                             | 100 MiB    | Minimum OOM increase                  |
| `RecommendationLowerBoundCPUPercentile`    | 0.50 (p50) | Lower bound percentile                |
| `RecommendationUpperBoundCPUPercentile`    | 0.95 (p95) | Upper bound percentile                |
| `RecommendationLowerBoundMemoryPercentile` | 0.50       | Same for memory                       |
| `RecommendationUpperBoundMemoryPercentile` | 0.95       | Same for memory                       |


**Histogram structure:** Exponentially-spaced buckets with ratio 1.05. For CPU: first bucket at 0.001 cores (1m), max ~~1000 cores (~~1400 buckets). For memory: first bucket at 1 MiB, max ~~1 TiB (~~1400 buckets). Memory per histogram: ~11 KB. Total per container (CPU + memory): ~22 KB.

**Upper bound confidence formula:** `upperBound = percentile(p95) × (1 + safetyMargin) × (1 + 1/historyDays)`. With 12h of history: ×3 multiplier. With 7 days: ×1.14. With 91 days: ×1.01. This prevents aggressive downsizing when history is limited.

---

## Appendix D: Confidence Levels


| Estimate                                                                                   | Confidence    | Basis                                                                                                                                                                                                                                                                                                          |
| ------------------------------------------------------------------------------------------ | ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/updateResults` is the ingestion bottleneck                                               | High          | Structural: O(4N) HTTP round-trips per hour, each with JSON + PG write                                                                                                                                                                                                                                         |
| Remote write batching reduces HTTP requests by ~200-400x                                   | High          | Structural: Prometheus remote write protocol design                                                                                                                                                                                                                                                            |
| JSONB deserialization dominates recommendation read cost                                   | High          | Known PostgreSQL JSONB behavior + JVM JSON parsing overhead                                                                                                                                                                                                                                                    |
| Thanos Receive handles 100K+ samples/s                                                     | High          | Well-documented in Thanos project and production reports                                                                                                                                                                                                                                                       |
| Integer millicores compress ~2-3x better in gorilla encoding                               | High          | Structural: XOR encoding of values with shared exponent/mantissa bits                                                                                                                                                                                                                                          |
| Kruize's 1m idle threshold makes sub-millicore precision unnecessary                       | High          | Verified in `GenericRecommendationModel.getCPURequestRecommendation` — `CPU_ONE_MILLICORE >= cpuRequest` returns null                                                                                                                                                                                          |
| Both cost and performance models use p100 (max) for memory                                 | High          | Verified: `COST_MEMORY_PERCENTILE = 100`, `PERFORMANCE_MEMORY_PERCENTILE = 100`                                                                                                                                                                                                                                |
| T-digest δ=200 gives ±0.1-0.5% error at p60/p98 for N=8,736                                | High          | Well-characterized in t-digest literature and implementations                                                                                                                                                                                                                                                  |
| T-digest per-container speedup ~5-15x for percentile step                                  | Moderate-High | Based on eliminating sort + boxing; confirmed by published benchmarks                                                                                                                                                                                                                                          |
| Streaming daily digests reduce recommendation step from hours to minutes                   | Moderate      | Depends on digest storage implementation and merge efficiency at scale                                                                                                                                                                                                                                         |
| Thanos `quantile_over_time` is faster than JVM `Collections.sort`                          | Moderate      | Go primitive sort vs Java boxed-object sort; structural advantage but unquantified                                                                                                                                                                                                                             |
| Thanos query is ~2-5x faster than PostgreSQL JSONB for time-series reads                   | Moderate      | First-principles reasoning about storage format and access patterns. Would need benchmarking.                                                                                                                                                                                                                  |
| Kruize local monitoring mode needs ~256-512 MB on cluster                                  | Moderate      | Based on local mode querying on demand vs remote mode storing data. Depends on Kruize implementation.                                                                                                                                                                                                          |
| 20M containers/91-day terms may not fit in 24 hours (current)                              | Moderate      | Based on 7.3B row read estimate at ~100-400K rows/s. Actual throughput depends on hardware and PostgreSQL tuning.                                                                                                                                                                                              |
| Prometheus range queries for 500 containers over 15 days complete in ~5-20s                | Low-Moderate  | Based on general Prometheus query benchmarks. Depends on cluster size, Prometheus configuration, and series cardinality.                                                                                                                                                                                       |
| Kruize's 1-core discontinuity produces unstable recommendations for containers near 1 core | High          | Verified in `GenericRecommendationModel`: `CPU_ONE_CORE > cpuUsageTotal` switches between max-based and percentile-based logic                                                                                                                                                                                 |
| Per-pod estimation (`sum/avg`) is fragile for heterogeneous workloads                      | High          | Structural: `sum/avg = N` only when all values are equal. Verified: no pod count is available in the operator's per-container CSV rows.                                                                                                                                                                        |
| VPA's decaying histogram is battle-tested at scale                                         | High          | Kubernetes VPA is used in production by major cloud providers (GKE, EKS, AKS autopilot). Open-source with extensive test coverage.                                                                                                                                                                             |
| max-then-percentile ≈ p99.96 (more conservative than intended p98)                         | Moderate-High | Based on order statistics: max of ~60 samples ≈ p98 of those samples; p98 of those maxes ≈ p98×p98 of underlying. Exact factor depends on distribution shape.                                                                                                                                                  |
| Option A (simplified percentile) is a 1-PR change (~20 lines)                              | High          | Scoped to `GenericRecommendationModel.getCPURequestRecommendation`: remove branching, replace max with avg, add multiplication.                                                                                                                                                                                |
| Option C (decaying t-digest) provides better tail accuracy than VPA histogram              | High          | T-digest δ=200 gives ~0.1-0.3% error at p95+ vs VPA histogram ~5% bucket quantization (ratio 1.05). Well-characterized in both systems.                                                                                                                                                                        |
| Decaying t-digest daily merge provides natural custom timeframe support                    | Moderate-High | T-digest merge is a documented O(δ log δ) operation. Weighted merge for decay is a custom extension (~50 lines) not in standard libraries; concept is sound but untested at scale.                                                                                                                             |
| Per-row transactions add ~5-10ms commit overhead each                                      | High          | Structural: PostgreSQL fsync on each commit. Well-documented Hibernate anti-pattern.                                                                                                                                                                                                                           |
| Gson instance creation costs ~10-50μs per call                                             | High          | Measured behavior of GsonBuilder: reflection + adapter resolution. Thread-safe reuse is documented.                                                                                                                                                                                                            |
| HTTP client per-request construction adds ~50-200ms per call (TLS)                         | High          | Structural: TLS handshake + TCP connection setup. Well-documented in Apache HttpClient docs.                                                                                                                                                                                                                   |
| `synchronized (new Object())` does not synchronize across threads                          | High          | Java language specification: monitor identity is per object instance. New object = new monitor.                                                                                                                                                                                                                |
| Hibernate Validator factory construction costs ~1-5ms                                      | Moderate-High | Reflection + annotation scanning overhead. JSR 380 spec recommends singleton pattern.                                                                                                                                                                                                                          |
| Kruize code-level fixes combined give ~2-5x ingestion improvement                          | Moderate      | Based on eliminating per-row commit overhead (dominant factor) + Gson/HTTP overhead. Actual improvement depends on workload and hardware.                                                                                                                                                                      |
| Redundant `filteredResultsMap` construction: ~30M unnecessary maps at 20M containers       | High          | Structural: 3 terms × 2 models = 6 passes, only 3 needed (one per term, reused across models). 3 redundant × 20M containers = ~30M allocations. Each allocation is O(intervals) with 8,736 entries for 91 days. Business hours filtering (mvp_demo) adds an additional filter pass when enabled.               |
| PlotManager triple-sort wastes cycles but TimSort on sorted input is O(n)                  | Moderate      | TimSort's merge-based algorithm detects presorted runs in O(n), but still allocates and iterates. Net overhead is low per call but multiplied across all containers.                                                                                                                                           |
| Memory algorithm sorts 8,736 elements where `Collections.max()` suffices                   | High          | Verified: `COST_MEMORY_PERCENTILE = 100` and `PERFORMANCE_MEMORY_PERCENTILE = 100`. `percentile(100, list)` sorts then returns last element.                                                                                                                                                                   |
| `calculateMemoryUsage` computes unused MIN via Stream pipeline                             | High          | Verified: caller at line 221 reads only `jsonObject.getDouble("MAX")`. The `MIN` put at line 288 is never read.                                                                                                                                                                                                |
| Memory per-pod estimation has same fragility as CPU                                        | High          | Same code pattern: `numPods = cpuUsageSum / cpuUsageAvg`. Already analyzed for CPU (§12, Problem 2).                                                                                                                                                                                                           |
| `min(usageBuf, spikeBuf)` can undersize recommendations                                    | Moderate-High | Structural: if spikes are small relative to steady usage, spike path dominates and 20% buffer is lost. Whether this is intentional is unclear.                                                                                                                                                                 |
| Adaptive IQR-CV margin produces better sizing than fixed margin                            | Moderate      | Well-established in statistics (robust variability measure). Untested in production for memory recommendations specifically.                                                                                                                                                                                   |
| OOM exponential backoff converges faster than VPA's fixed bump                             | Moderate      | Logical: 1.3×/1.6×/2.0× progression reaches adequate sizing in 1-3 cycles vs VPA's 3-5 cycles. Actual convergence depends on how far under-provisioned.                                                                                                                                                        |
| Trend detection via 14-day linear regression catches memory leaks proactively              | Moderate      | Simple linear regression on daily means is well-understood. The 7-day forward projection is a heuristic — actual growth may be non-linear.                                                                                                                                                                     |
| Multi-timescale merge (short 7d + long 91d) captures weekly/monthly patterns               | Moderate      | Structural argument: short decay misses weekly patterns, long decay is slow to react. Dual-rate addresses both. Untested in production.                                                                                                                                                                        |
| Lowering memory percentile from p100 to p95 requires OOM feedback as safety net            | High          | Without OOM detection, recommending below historical max risks OOM kills. With OOM detection + backoff, p95 is safe — VPA proves this at scale with p90.                                                                                                                                                       |
| Operator can collect OOM events via `kube_pod_container_status_last_terminated_reason`     | High          | Standard kube-state-metrics metric, available on all OpenShift clusters. The operator already queries kube-state-metrics for other ROS metrics.                                                                                                                                                                |
| `checkIfModelIsKruizeSupportedMIG` blocks B200 and RTX PRO recommendations                 | **Very High** | Verified: `checkIfModelIsKruizeSupportedMIG` at RecommendationUtils.java:405 checks only `A100`, `H100`, `H200` in model name. `getMapWithOptimalProfile` has full profile data for B200 and RTX PRO. Gating function returns false → `isGpuWorkload` never set → returns null.                                |
| `getFrameBufferBasedOnModel` missing B200 (180 GB) and RTX PRO 5000 (48 GB)                | **Very High** | Verified: function handles 40/80/94/96/141 GB only. 180 GB and 48 GB cases fall through, returning -1, causing frame buffer values > 100 to be silently dropped.                                                                                                                                               |
| GPU MIG bin-packing approach is architecturally sound                                      | High          | Standard approach for GPU right-sizing. The alternative (continuous core/memory recommendation) doesn't align with MIG hardware constraints.                                                                                                                                                                   |
| GPU underutilization detection has high cost-saving potential                              | Moderate-High | GPUs are 10-50x more expensive than equivalent CPU compute. A container using 2-3% of a GPU would save the most by removing the GPU entirely. The "smallest MIG partition" recommendation misses this optimization.                                                                                            |
| Multi-GPU mischaracterization affects distributed training workloads                       | Moderate      | 4-GPU container at 25% each looks like 1 GPU at 25%. Growing use case (LLM fine-tuning, distributed training) but still a minority of total GPU containers.                                                                                                                                                    |
| Quarkus `THREADS_PER_CORE=1` undersizes thread pools vs framework default                  | **Very High** | Verified: `RecommendationConstants.RuntimeConstants.THREADS_PER_CORE = 1`. Quarkus default is `max(8, 2×cores)`. A 4-core container gets 4 threads instead of 8. This actively degrades performance for I/O-bound Quarkus workloads.                                                                           |
| MaxRAMPercentage recommendation ignores actual heap usage data                             | **Very High** | Verified: `generateHotspotMaxRAMPercentageRecommendation` uses only `containerMemoryMB` parameter. The `filteredResultsMap` (containing JVM metrics) is passed to `generateRecommendations` but never forwarded to this method.                                                                                |
| GC policy recommendation ignores GC pause metrics                                          | High          | Verified: `generateHotspotGCPolicyRecommendation` uses `cores`, `jvmHeapSizeMB`, `jdkMajorVersion` only. No GC pause time or throughput metrics are consulted from `filteredResultsMap`.                                                                                                                       |
| Semeru uses `Math.round` where Hotspot uses `Math.ceil` for CPU cores                      | **Very High** | Verified: `HotspotLayerRecommendationHandler.java` uses `(int) Math.ceil(cpuValue)`, `SemeruLayerRecommendationHandler.java` uses `Math.round(cpuValue)`. For 1.3 cores: Hotspot → 2, Semeru → 1. Different GC selections for identical hardware.                                                              |
| Layered handler architecture with dependency resolver is well-designed                     | High          | Verified: `TunableDependencyResolver` implements topological sort, `LayerRecommendationHandlerRegistry` manages handlers via strategy pattern, `RuntimeRecommendationProcessor` orchestrates evaluation in dependency order. This is production-quality architecture.                                          |
| `QueryBasedPresence` detects JVM/Quarkus via Prometheus queries                            | High          | Verified: `QueryBasedPresence.detectPresence()` runs PromQL queries with dynamic namespace/container filters. This is the correct approach — avoids fragile image-name heuristics. Requires JVM metrics exporter on target cluster.                                                                            |
| §19.1: `errorReasons` accumulates across bulk rows                                         | **Very High** | Verified: `ExperimentInitiator.validateAndAddExperimentResults` allocates `errorReasons` at line ~139 before the loop, never clears it per iteration. Structural: list append in loop without clear.                                                                                                           |
| §19.2: Interval map grows without eviction                                                 | **Very High** | Verified: `ContainerData.results` is a `HashMap<Timestamp, IntervalResults>`. No eviction logic exists. `put()` is the only mutation. 91 days × 4/hour = 8,736 entries per container.                                                                                                                          |
| §19.3: `autotuneObjectMap` has no size bound                                               | **Very High** | Verified: `KruizeOperator.java` line ~82: `ConcurrentHashMap`. Entries removed only on K8s delete events (local) or API delete (remote). No TTL, no LRU, no max-size.                                                                                                                                          |
| §19.4: Cross-model duplicate work doubles computation                                      | High          | Verified: `generateRecommendationBasedOnModel` called twice per term with identical filtered map construction. Only percentile targets differ.                                                                                                                                                                 |
| §19.5: `mergeResults` flattens multi-timestamp data                                        | High          | Verified: `ExperimentInterfaceImpl.mergeResults` iterates `newResults.values()` and puts all metric results into a single `metricResultsHashMap`, losing timestamp structure when batch contains multiple intervals.                                                                                           |
| §19.7: `addExperimentToDB` synchronized bottleneck                                         | **Very High** | Verified: `ExperimentDAOImpl.addExperimentToDB` has `synchronized` keyword. Under concurrent API calls, all inserts serialize through one monitor.                                                                                                                                                             |
| §19.8: `getTimestampWithinTolerance` O(n) per probe                                        | High          | Verified: linear scan over `keySet()` with `Math.abs(timestamp.getTime() - key.getTime()) <= tolerance`. For TreeMap, `floorKey`/`ceilingKey` would be O(log n).                                                                                                                                               |
| §19.14: `PlotManager` int overflow for large millisecond deltas                            | High          | Verified: `calendar.add(Calendar.MILLISECOND, (int) millisecondsToAdd)`. `int` max = 2.1 billion ms = ~24 days. Larger deltas silently overflow.                                                                                                                                                               |
| §20.1: RBAC middleware nil pointer panics on HTTP failure                                  | **Very High** | Verified: `rbac.go` line ~85: `req, _ := http.NewRequest(...)` then `req.Header.Set(...)`. Line ~93: `res, err := client.Do(req)` then `defer res.Body.Close()`. Both dereference nil on error. No timeout configured.                                                                                         |
| §20.2: API handlers return HTTP 200 on DB failure                                          | **Very High** | Verified: `handlers.go` lines ~42-46: `db.Where(...).Find(...)` error logged but no return, handler continues to build 200 response.                                                                                                                                                                           |
| §20.3: Kafka consumer type assertion panic                                                 | **Very High** | Verified: `consumer.go` lines ~93-97: `err.(kafka.Error)` without comma-ok. Non-Kafka errors panic.                                                                                                                                                                                                            |
| §20.9: Recommendation poller infinite redelivery                                           | High          | Verified: on DB error, handler returns without committing offset. Kafka redelivers the same message. No dead-letter queue. No skip-after-N logic.                                                                                                                                                              |
| §20.11: Housekeeper GORM `.Where()` not assigned                                           | High          | Verified: `sourcesCleaner.go` line ~52: same pattern as §15.4 — `.Where()` return value discarded.                                                                                                                                                                                                             |
| §20.12: RBAC `strings.Split` panic on malformed permission                                 | High          | Verified: `rbac.go` line ~42: `strings.Split(acl.Permission, ":")[1]` without length check. Empty or colon-less string → index out of range panic.                                                                                                                                                             |
| §23.1: Idle detection requires zero new queries                                            | **Very High** | Verified: operator already collects `ros:container_cpu_usage_avg/max` and `ros:container_memory_usage_avg/max`. Detection is a pure threshold comparison against `ros:container_cpu_requests_avg`.                                                                                                             |
| §23.2: PVC data already collected under `cost:` prefix                                     | **Very High** | Verified: `cost:persistentvolumeclaim_capacity_bytes`, `cost:persistentvolumeclaim_usage_bytes`, `cost:persistentvolumeclaim_request_bytes` in `queries.go` lines 17-22.                                                                                                                                       |
| §23.3: HPA metrics available via kube-state-metrics                                        | **Very High** | Verified against kube-state-metrics GitHub docs: all 8 proposed metrics exist with STABLE or EXPERIMENTAL status. Available on all OpenShift 4.12+ clusters with default monitoring stack.                                                                                                                     |
| §23.4: GOMAXPROCS mismatch causes 2-10x Go performance degradation                         | High          | Documented by VictoriaMetrics (2025) and uber-go/automaxprocs. Structural: Go scheduler creates OS threads proportional to GOMAXPROCS; excess threads on quota-limited containers cause context switching overhead.                                                                                            |
| §23.4: `go_info` metric available on Go apps with standard prom client                     | Moderate-High | Go's `prometheus/client_golang` exposes `go_info`, `go_goroutines`, `go_gc_duration_seconds` by default. Widely used but not universal — apps using custom HTTP frameworks without prom middleware won't expose it.                                                                                            |
| §23.5: QoS class is deterministic from request/limit values                                | **Very High** | Kubernetes spec: Guaranteed = all containers have request == limit for CPU and memory. BestEffort = no requests or limits set. Burstable = everything else. Derivable from existing data.                                                                                                                      |
| §23.6: `container_fs_usage_bytes` reliability varies by K8s version                        | Moderate      | StackOverflow and kube-state-metrics issues confirm metric availability is kubelet/CRI-dependent. k8s-ephemeral-storage-metrics exporter exists as a workaround.                                                                                                                                               |
| §23.7: Node.js `max-old-space-size` default (2GB) causes OOMs in small containers          | High          | Documented in Node.js official docs. V8 heap limit is independent of container memory limit. A 512 MB container with 2 GB heap limit will OOM on allocation.                                                                                                                                                   |
| §23.8: `kube_resourcequota` metric available on all clusters                               | **Very High** | Standard kube-state-metrics metric. Verified against kube-state-metrics docs — labels include `type=hard                                                                                                                                                                                                       |
| Industry: Sysdig reports 69% of allocated CPU is unused                                    | High          | Published Sysdig 2024 Container Report. Widely cited. Aligns with independent Datadog and CAST AI reports of 40-70% overprovisioning.                                                                                                                                                                          |
| Industry: StormForge bi-dimensional (VPA+HPA) is their core differentiator                 | **Very High** | Verified via StormForge product documentation and blog posts (2025-2026). No open-source or other commercial tool offers combined VPA+HPA optimization.                                                                                                                                                        |
| §24: Kruize recommendation algorithms are ~820 lines of Java                               | High          | Verified by reading all of `GenericRecommendationModel`, `RecommendationUtils`, `AcceleratorMetaDataService`, and the layer handlers. Core logic (excluding boilerplate, imports, comments) is ~100 lines CPU, ~80 lines memory, ~180 lines GPU, ~60 lines cost/perf models, ~400 lines layer framework.       |
| §24: Equivalent Go implementation is ~1,700 lines                                          | Moderate-High | Estimated by adding improved algorithms (exact percentile sort paths + margins/trends, OOM ~100, new rec types ~350) to the ported core (~~700). Go tends to be slightly more verbose than Java for data structures but more concise for concurrency. **v4.0:** no t-digest in remote path — `slices.Sort()` on digest samples.                                                                                        |
| §25: /updateResults HTTP round-trip dominates at ~125ms per container                      | **Very High** | Structural: no HTTP connection pooling in current ros-ocp-backend code (`http.Client{}` per request). TLS handshake alone is 50-200ms. Gson instantiation adds ~50μs. Hibernate per-row transaction adds 5-10ms.                                                                                               |
| §25: T-digest add ~0.004ms (hypothetical sketch path only)                                 | Historical    | Would apply only if t-digest were used in Go. **v4.0:** daily digests + `slices.Sort()` on bounded samples (~96 int64s), ~0.003ms per §25.1 — not centroid accumulation.                                                                                                                                                                                   |
| §25: T-digest percentile ~0.001ms (hypothetical sketch path only)                          | Historical    | **v4.0:** exact rank from sorted slice in Go — not δ-centroid scan.                                                                                                                                                                                              |
| §25: Current max throughput ~1,000 containers/hour                                         | High          | Derived: 125ms/container × 4 intervals × N < 3,600,000ms → N < 7,200. But recommendation (42ms × N) adds to the pipeline. Effective limit ~1,000 containers to complete both ingestion + recommendation within 1 hour.                                                                                         |
| §25: New max throughput ~5M containers/hour (10 workers)                                   | Moderate-High | Derived: 0.064ms/container × 4 intervals × N / 10 workers < 3,600,000ms → N < 140M (ingestion not the bottleneck). **v4.0:** limits are PostgreSQL batch write throughput (`COPY FROM` / bulk upsert) and cluster-wide digest reads — not Thanos. Would need benchmarking to confirm.                                                                                          |
| §25: Storage reduction (daily digests vs per-interval JSONB)                               | **Very High** | **v4.0:** one small digest row per container per day vs ~1.3KB × 8,736 JSONB rows per container (91d). Order-of-magnitude reduction matches §25 tables (~2,500× at scale); exact factor depends on digest column widths.                                                                                                                 |
| §25: Real-time / on-demand recommendations                                                 | High          | **v4.0:** API reads daily digest rows for the window (~1–3ms), then runs the same Go `recommendCPU()` / `recommendMemory()` path as batch — in-memory on a bounded slice, fast enough for on-demand (§25.5).                                                                                                                                 |
| §24: 4-6 month timeline for single senior Go developer                                     | Moderate      | Based on ~1,700 lines of algorithm code + testing + integration + documentation. Assumes developer is familiar with ros-ocp-backend. If unfamiliar, add 2-4 weeks ramp-up.                                                                                                                                     |
| §26: Existing ROS queries aggregate per-pod, not per-workload                              | **Very High** | Verified: `ros:cpu_request_container_avg` aggregates `by(container, pod, namespace, node)`. No workload-level replica count is computed.                                                                                                                                                                       |
| §26: `kube_deployment_spec_replicas` available via kube-state-metrics                      | **Very High** | Standard STABLE metric in kube-state-metrics, available on all OpenShift 4.12+ clusters with default monitoring stack. Same for `kube_statefulset_replicas` and `kube_daemonset_status_desired_number_scheduled`.                                                                                              |
| §26: Derived pod count is fragile during scale events                                      | High          | Structural: rolling updates, evictions, CrashLoopBackOff all produce transient pod counts that differ from desired replicas. Counting distinct pods gives observed-at-collection, not intent.                                                                                                                  |
| §26: 15-minute frequency is sufficient for replica count collection                        | **Very High** | Structural: `spec.replicas` changes only on manual edits, HPA events, or GitOps reconciliation — all are minute-to-hour granularity events. 15-minute sampling captures all meaningful changes.                                                                                                                |
| §26: 2-4 new queries add negligible Prometheus load                                        | **Very High** | Gauge queries on kube-state-metrics are <1ms each. On a cluster with ~73 existing operator queries, adding 2-4 is a <5% increase in query count and negligible wall-clock impact.                                                                                                                              |
| §27: `workload_metrics.usage_metrics` is never read                                        | **Very High** | Verified: searched entire ros-ocp-backend codebase for reads of `usage_metrics` or `WorkloadMetrics`. Only `BatchInsertWorkloadMetrics` (write) and model definition exist. No SELECT, Find, or query references this column's data.                                                                           |
| §27: No PostgreSQL JSON operators used on any JSONB column                                 | **Very High** | Verified: no `->`, `->>`, `@>`, `jsonb_extract_path`, or similar operators in any Go SQL or GORM query across the codebase. JSONB is treated as opaque blob.                                                                                                                                                   |
| §27: `recommendations` JSONB deserialized to `map[string]interface{}` on API read          | **Very High** | Verified: `UpdateRecommendationJSON` in `internal/api/utils.go` calls `json.Unmarshal([]byte(jsonData), &data)` where `data` is `map[string]interface{}`. Then applies `dropBoxPlotsObject`, `transformComponentUnits`, `filterNotifications`, `convertVariationToPercentage` — all operating on untyped maps. |
| §27: Relational columns eliminate ~10-20x storage per recommendation row                   | High          | Structural: 6 integer columns (4 bytes each) + 2 bigint columns (8 bytes each) = ~40 bytes for core recommendation data vs. ~2-5 KB JSONB blob. Factor depends on how many fields are migrated to columns.                                                                                                     |
| §28: TimescaleDB `COPY FROM` achieves 300K-1M rows/sec                                     | High          | Published TimescaleDB benchmarks (2025). Performance depends on hardware, row size, and index count, but `COPY` consistently outperforms batched INSERTs by ~50x and remote write by ~3-5x.                                                                                                                    |
| §28: TimescaleDB Toolkit provides native t-digest with `rollup()` merge                    | **Very High** | Verified: `timescaledb_toolkit` GitHub docs confirm `tdigest(buckets, value)`, `approx_percentile(p, digest)`, and `rollup(digest)` functions. Toolkit v1.21.0 (April 2025) added `total` accessor. Compatible with continuous aggregates.                                                                     |
| §28: TimescaleDB columnar compression achieves 90-95% reduction                            | High          | Published TimescaleDB benchmarks and production reports (2025-2026). Uses Gorilla encoding for floats, delta-of-delta for timestamps, dictionary for strings, all with LZ4 final pass.                                                                                                                         |
| §28: TimescaleDB requires zero new infrastructure services                                 | **Very High** | Structural: TimescaleDB is a PostgreSQL extension loaded via `CREATE EXTENSION`. No separate process, no object storage, no distributed coordination. Runs in the existing PostgreSQL process.                                                                                                                 |
| §28: Thanos remote write protocol implementation requires ~300-500 lines of Go             | Moderate-High | Based on Prometheus remote write spec (protobuf schema, snappy compression, retry logic, batching). Reference implementations exist but integration, error handling, and testing add effort.                                                                                                                   |
| §29: PL/pgSQL batch recommendation is 4-8x faster than Go per-row                          | **High**      | Structural: eliminates 2×N round-trips (N reads + N writes) for N containers. SQL processes all rows in a single sequential scan of the continuous aggregate. Well-established pattern in financial/analytics systems.                                                                                         |
| §29: `rollup()` can be called inside SQL functions on continuous aggregates                | **Very High** | Verified: TimescaleDB Toolkit docs explicitly support `rollup()` as an aggregate function in `SELECT` queries, including within PL/pgSQL functions and views.                                                                                                                                                  |
| §29: `regr_slope()` is a built-in PostgreSQL aggregate                                     | **Very High** | Part of SQL standard. Available in PostgreSQL 8.2+. Used for linear regression on time-series data.                                                                                                                                                                                                            |
| §29: Amortized per-container cost of ~0.004 ms in SQL batch                                | High          | Based on continuous aggregate sequential scan (~200 ms for 50K pre-aggregated rows) plus t-digest merge (O(δ) per container, δ=200). The scan dominates, but it's shared across all containers. 200ms / 50K = 0.004ms amortized.                                                                               |
| §29: Shadow-mode comparison (Go vs SQL) is feasible for validation                         | **High**      | Standard dual-write pattern: run both, compare, alert on divergence. Recommendation outputs are small (handful of integer columns per container).                                                                                                                                                              |
| §29: GPU/JVM/HPA heuristics are poor candidates for SQL                                    | **High**      | These involve branchy logic (MIG profile lookups, GC strategy selection), external knowledge (runtime-specific tuning), and API interactions (HPA spec reading). SQL is not well-suited for such logic.                                                                                                        |


---

## Appendix E: Implementation Reference Guide

This appendix provides all concrete implementation details needed to derive technical specifications from this report without requiring prior codebase knowledge. All file paths are relative to each repository root unless otherwise noted.

### E.1 Repository Map


| Repository              | Language         | Key Branch                                               | Purpose                                                                                                                         |
| ----------------------- | ---------------- | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `koku-metrics-operator` | Go 1.25          | `main`                                                   | Runs on customer OpenShift clusters. Queries Prometheus, generates CSV reports, packages into tarballs, uploads to ingress API. |
| `ros-ocp-backend`       | Go 1.24          | `COST-5691-custom-timeframes` (feature), `main` (stable) | Central backend. **v4.0 remote path:** Kafka → CSV ingest → **native Go** recommendation engine + PostgreSQL 16+ (digests + results). **Legacy:** HTTP to Kruize for recommendations.          |
| `autotune` (Kruize)     | Java 25 (Maven)  | `mvp_demo` (development), `remote_monitoring` (stable)   | Recommendation engine. Receives metrics via API, computes CPU/memory/GPU/runtime recommendations, stores in PostgreSQL.         |
| `koku-ui`               | TypeScript/React | `COST-5691-custom-timeframes` (feature), `main` (stable) | Frontend. Consumes ros-ocp-backend REST API. On-prem shell at `apps/koku-ui-onprem/`.                                           |


**Key dependencies:**


| Component       | Dependency                 | Version          |
| --------------- | -------------------------- | ---------------- |
| Operator        | `controller-runtime`       | v0.23.3          |
| Operator        | `prometheus/client_golang` | v1.23.2          |
| ros-ocp-backend | GORM                       | v1.31.1          |
| ros-ocp-backend | `confluent-kafka-go`       | v2.13.0          |
| ros-ocp-backend | Echo (HTTP)                | v4.15.1          |
| ros-ocp-backend | `go-gota` (dataframe)      | v0.12.0          |
| ros-ocp-backend | `golang-migrate`           | v4.19.0          |
| Kruize          | Hibernate                  | 6.6.x (mvp_demo) |
| Kruize          | Gson                       | (bundled)        |
| Kruize          | Jetty                      | (embedded)       |
| Kruize          | c3p0 (connection pool)     | (via Hibernate)  |


### E.2 End-to-End Data Flow (Pre–v4.0 remote path with Kruize)

> **v4.0:** Ingestion still uses Kafka and CSV processing; recommendation steps that POST to Kruize are replaced by **Go** (`recommendCPU`, `recommendMemory`, `recommendAllWorkloads`, …) reading/writing **plain PostgreSQL** only.

```
┌─────────────────────────────────────────────────────────────────┐
│ Customer OpenShift Cluster                                      │
│                                                                 │
│  Prometheus ──(PromQL)──> koku-metrics-operator                 │
│                           │                                     │
│                           ├── Cost CSVs (pod, node, storage,    │
│                           │   namespace, VM, GPU)               │
│                           ├── ROS CSVs (container, namespace)   │
│                           ├── manifest.json                     │
│                           └── tar.gz ──(POST /api/ingress/v1/   │
│                                         upload)──>              │
└─────────────────────────────────────┬───────────────────────────┘
                                      │
                    ┌─────────────────▼──────────────────┐
                    │ Ingress Service                     │
                    │ Extracts tar.gz, posts Kafka msg    │
                    │ Topic: platform.upload.announce     │
                    └─────────────────┬──────────────────┘
                                      │
                    ┌─────────────────▼──────────────────┐
                    │ Koku Listener (koku backend)        │
                    │ Validates, re-publishes to:         │
                    │ Topic: hccm.ros.events              │
                    └─────────────────┬──────────────────┘
                                      │
┌─────────────────────────────────────▼───────────────────────────┐
│ ros-ocp-backend (report_processor.go)                           │
│                                                                 │
│  1. Consume Kafka msg from hccm.ros.events                      │
│  2. Download CSV from S3/MinIO URL in msg                       │
│  3. Load into gota dataframe with CSVColumnMapping              │
│  4. Aggregate_data: group by k8s object, compute SUM/MEAN/etc.  │
│  5. For each workload:                                          │
│     a. POST /createExperiment → Kruize                          │
│     b. Build UpdateResult payload from aggregated metrics       │
│     c. POST /updateResults → Kruize (chunked)                   │
│     d. INSERT workload_metrics (JSONB) → PostgreSQL             │
│     e. Produce Kafka msg → rosocp.kruize.recommendations        │
│                                                                 │
│  (recommendation_poller.go)                                     │
│  6. Consume Kafka msg from rosocp.kruize.recommendations        │
│  7. POST /updateRecommendations?experiment_name=X → Kruize      │
│  8. Parse recommendation response                               │
│  9. UPSERT recommendation_sets → PostgreSQL                     │
│  10. UPSERT historical_recommendation_sets → PostgreSQL         │
└─────────────────────────────────────────────────────────────────┘
                                      │
┌─────────────────────────────────────▼───────────────────────────┐
│ Kruize (autotune)                                               │
│                                                                 │
│  /createExperiment: Creates experiment in kruize_experiments     │
│  /updateResults: Stores metrics in kruize_results (JSONB)       │
│  /updateRecommendations: Reads all results for experiment,      │
│    deserializes JSONB, runs GenericRecommendationModel:          │
│    - getCPURequestRecommendation (percentile + algorithm)       │
│    - getMemoryRequestRecommendation (percentile + buffer)       │
│    - getAcceleratorRequestRecommendation (GPU MIG bin-packing)  │
│    - getRuntimeRecommendations (JVM/Quarkus layer handlers)     │
│    Stores in kruize_recommendations                             │
│    Returns JSON response to caller                              │
└─────────────────────────────────────────────────────────────────┘
                                      │
┌─────────────────────────────────────▼───────────────────────────┐
│ ros-ocp-backend REST API                                        │
│                                                                 │
│  GET /api/cost-management/v1/recommendations/openshift          │
│  GET /api/cost-management/v1/recommendations/openshift/:id      │
│  GET /api/cost-management/v1/openshift/namespace/recommendations│
│                                                                 │
│  Reads from recommendation_sets / namespace_recommendation_sets │
│  Returns JSON with recommendations, cluster info, etc.          │
└─────────────────────────────────────┬───────────────────────────┘
                                      │
                    ┌─────────────────▼──────────────────┐
                    │ koku-ui (React frontend)            │
                    │ Renders recommendations in UI       │
                    └────────────────────────────────────┘
```

### E.3 Operator CSV Format

The operator produces two ROS-specific CSV files per collection interval:

`**ros-openshift-container-{YYYYMM}.csv**` — Container-level metrics (30 ROS queries + metadata)

Key columns (from `rosContainerRow` in `internal/collector/types.go`):


| CSV Column                                       | Go Type        | Source                          |
| ------------------------------------------------ | -------------- | ------------------------------- |
| `report_period_start`, `report_period_end`       | string         | Collection window               |
| `interval_start`, `interval_end`                 | string         | 15-minute sub-window            |
| `container_name`, `pod`, `namespace`, `node`     | string         | Prometheus labels               |
| `owner_name`, `owner_kind`                       | string         | `ros:image_owners` query        |
| `workload`, `workload_type`                      | string         | `ros:image_workloads` query     |
| `image_name`                                     | string         | `ros:image_workloads` query     |
| `cpu_request_container_avg`                      | string (float) | `ros:cpu_request_container_avg` |
| `cpu_request_container_sum`                      | string (float) | `ros:cpu_request_container_sum` |
| `cpu_limit_container_avg`                        | string (float) | ...                             |
| `cpu_usage_container_avg/min/max/sum`            | string (float) | ...                             |
| `cpu_throttle_container_avg/max/sum`             | string (float) | ...                             |
| `memory_request_container_avg/sum`               | string (float) | ...                             |
| `memory_limit_container_avg/sum`                 | string (float) | ...                             |
| `memory_usage_container_avg/min/max/sum`         | string (float) | ...                             |
| `memory_rss_usage_container_avg/min/max/sum`     | string (float) | ...                             |
| `accelerator_*` (core/memory/frame_buffer usage) | string (float) | NVIDIA DCGM queries             |


**All numeric values are serialized as strings** (Go `string` type in struct). The `mapstructure` tags use hyphenated keys (e.g., `cpu-request-container-avg`) for mapping from Prometheus result labels.

`**ros-openshift-namespace-{YYYYMM}.csv**` — Namespace-level metrics (20 ROS queries)

Columns follow the same pattern with `_namespace`_ variants and additional `namespace_total_pods_avg/max/sum`.

**Query execution pattern:** For each hour in the collection window, the operator runs 4 rounds of instant queries (`getQueryResults`) at 15-minute intervals (`:00`, `:15`, `:30`, `:45`). Each instant query returns a `model.Vector` (one value per label set). Results are mapped to struct fields via `mapstructure.Decode`.

**Manifest JSON structure** (from `internal/packaging/packaging.go`):

```json
{
  "uuid": "random-uuid",
  "cluster_id": "cluster-uuid-from-ClusterVersion-CR",
  "version": "operator-version",
  "date": "2026-03-26T00:00:00Z",
  "start": "2026-03-25T00:00:00Z",
  "end": "2026-03-26T00:00:00Z",
  "files": ["cm-openshift-pod-usage-202603.csv", "..."],
  "resource_optimization_files": ["ros-openshift-container-202603.csv", "ros-openshift-namespace-202603.csv"],
  "cr_status": { "...CRD status snapshot..." },
  "certified": true,
  "daily_reports": true
}
```

**Upload:** `POST {api_url}/api/ingress/v1/upload`, multipart form, content-type `application/vnd.redhat.hccm.tar+tgz`.

### E.4 ros-ocp-backend Database Schema

**PostgreSQL** with GORM + golang-migrate. 19 migrations (000001–000019).

**Core tables:**


| Table                                      | Partitioned?                              | Key Columns                                                                                                                                                                                                                                                                                                       |
| ------------------------------------------ | ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `rh_accounts`                              | No                                        | `id` (BIGSERIAL PK), `org_id` (TEXT UNIQUE NOT NULL), `account` (TEXT), `created_at`                                                                                                                                                                                                                              |
| `clusters`                                 | No                                        | `id` (BIGSERIAL PK), `tenant_id` (FK→rh_accounts), `source_id`, `cluster_uuid`, `cluster_alias`, `last_reported_at`. UNIQUE(tenant_id, source_id, cluster_uuid, cluster_alias)                                                                                                                                    |
| `workloads`                                | No                                        | `id` (BIGSERIAL PK), `org_id`, `cluster_id` (FK→clusters), `experiment_name`, `namespace`, `workload_type` (enum), `workload_name`, `containers` (TEXT[]), `metrics_upload_at`. UNIQUE(org_id, cluster_id, experiment_name). GIN index on containers.                                                             |
| `recommendation_sets`                      | No                                        | `id` (UUID PK), `workload_id` (FK→workloads), `container_name`, `monitoring_start_time`, `monitoring_end_time`, `**recommendations` (JSONB)**, `updated_at`. UNIQUE(workload_id, container_name).                                                                                                                 |
| `workload_metrics`                         | LIST(org_id) + RANGE(interval_end)        | `id` (BIGSERIAL), `org_id`, `workload_id` (FK→workloads), `container_name`, `interval_start`, `interval_end`, `**usage_metrics` (JSONB)**, `namespace_name`, `metric_type` (enum: container/namespace). UNIQUE(org_id, workload_id, container_name, interval_start, interval_end).                                |
| `historical_recommendation_sets`           | LIST(org_id) + RANGE(monitoring_end_time) | Same shape as recommendation_sets but partitioned for time-series history.                                                                                                                                                                                                                                        |
| `namespace_recommendation_sets`            | No                                        | `id` (UUID PK), `org_id`, `workload_id` (FK→workloads UNIQUE), `namespace_name`, `cpu_request_current` (FLOAT), `cpu_variation` (FLOAT), `memory_request_current` (FLOAT), `memory_variation` (FLOAT), `monitoring_start_time`, `monitoring_end_time`, `**recommendations` (JSONB)**, `created_at`, `updated_at`. |
| `historical_namespace_recommendation_sets` | No (in migration)                         | Same shape + `org_id`, UNIQUE(org_id, workload_id, monitoring_end_time).                                                                                                                                                                                                                                          |


**The `recommendations` JSONB column** stores the full Kruize recommendation response nested under term keys (short_term, medium_term, long_term), each containing cost/performance models with CPU/memory request/limit values, notifications, and plots.

### E.5 Kruize Database Schema

**PostgreSQL** with Hibernate JPA. Tables created via `hbm2ddl.auto`.


| Table                         | Key Columns                                                                                                                                                                                                                                   |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `kruize_experiments`          | `experiment_id` (String PK), `experiment_name` (UNIQUE), `cluster_name`, `mode`, `target_cluster`, `performance_profile`, `status` (enum), `datasource`/`extended_data`/`meta_data` (JSON), `experiment_type`, `creation_date`, `update_date` |
| `kruize_results`              | Composite PK(`experiment_name`, `interval_end_time`), `interval_start_time`, `duration_minutes`, `**extended_data` (JSON)** — contains all metric results per interval                                                                        |
| `kruize_recommendations`      | Composite PK(`experiment_name`, `interval_end_time`), `cluster_name`, `**extended_data` (JSON)** — contains computed recommendations                                                                                                          |
| `kruize_performance_profiles` | PK `name`, `profile_version`, `k8s_type`, `slo` (JSON)                                                                                                                                                                                        |


**In local monitoring mode (mvp_demo):** Additional tables `kruize_lm_experiments`, `kruize_lm_recommendations`, `kruize_lm_layer_entries`, etc.

### E.6 Kruize API Contract (as consumed by ros-ocp-backend)

**Base URL:** Configured via `KRUIZE_URL` env var (default: `http://{KRUIZE_HOST}:{KRUIZE_PORT}`).

**POST `/createExperiment`** — Create a new experiment

```json
[{
  "version": "v2.0",
  "experiment_name": "org1234567|cluster-uuid|namespace|workload_type|workload_name",
  "cluster_name": "cluster-uuid",
  "performance_profile": "resource-optimization-openshift",
  "mode": "monitor",
  "target_cluster": "remote",
  "kubernetes_objects": [{
    "type": "deployment",
    "name": "workload-name",
    "namespace": "namespace-name",
    "containers": [{
      "container_name": "container-name",
      "container_image_name": "image:tag"
    }]
  }],
  "trial_settings": { "measurement_duration": "15min" },
  "recommendation_settings": { "threshold": "0.1" }
}]
```

Response: **201** on success, **409** if experiment already exists.

**POST `/updateResults`** — Push metrics for an experiment

```json
[{
  "version": "v2.0",
  "experiment_name": "...",
  "interval_start_time": "2026-03-26T00:00:00.000Z",
  "interval_end_time": "2026-03-26T00:15:00.000Z",
  "kubernetes_objects": [{
    "type": "deployment",
    "name": "workload-name",
    "namespace": "namespace-name",
    "containers": [{
      "container_name": "container-name",
      "container_image_name": "image:tag",
      "metrics": {
        "cpuRequest": { "results": { "aggregation_info": { "avg": 0.5, "sum": 1.0 } } },
        "cpuLimit": { "results": { "aggregation_info": { "avg": 1.0, "sum": 2.0 } } },
        "cpuUsage": { "results": { "aggregation_info": { "avg": 0.3, "min": 0.1, "max": 0.8, "sum": 0.6 } } },
        "cpuThrottle": { "results": { "aggregation_info": { "avg": 0.01, "max": 0.05, "sum": 0.02 } } },
        "memoryRequest": { "results": { "aggregation_info": { "avg": 536870912, "sum": 1073741824 } } },
        "memoryLimit": { "results": { "aggregation_info": { "avg": 1073741824, "sum": 2147483648 } } },
        "memoryUsage": { "results": { "aggregation_info": { "avg": 400000000, "min": 300000000, "max": 500000000, "sum": 800000000 } } },
        "memoryRSS": { "results": { "aggregation_info": { "avg": 350000000, "min": 250000000, "max": 450000000, "sum": 700000000 } } }
      }
    }]
  }]
}]
```

Response: **201** on success. Chunked by `KRUIZE_MAX_BULK_CHUNK_SIZE`.

**POST `/updateRecommendations`** — Trigger recommendation generation

Query params: `experiment_name`, `interval_end_time` (ISO 8601).

No request body. Response: **201** with full recommendation JSON containing `short_term`, `medium_term`, `long_term` objects, each with `cost` and `performance` models containing `cpu`/`memory` request/limit `amount`/`format` values and `notifications`.

**POST `/updateExperiment`** (custom timeframes branch) — Update experiment settings

```json
[{
  "experiment_name": "...",
  "recommendation_settings": { "threshold": "0.1" },
  "term_settings": {
    "short_term": { "duration": "24h", "monitoring_start_time": "..." },
    "medium_term": { "duration": "168h" },
    "long_term": { "duration": "360h" }
  },
  "business_hours": { "timezone": "UTC", "days": {...}, "hours": {...} }
}]
```

### E.7 ros-ocp-backend REST API Contract

**Base URL:** `http://{host}:{API_PORT}`

**GET `/api/cost-management/v1/recommendations/openshift`** — List container recommendations

Query params: `limit`, `offset`, `order_by`, `order_how`, `start_date`, `end_date`, `cluster` (UUID filter), `project` (namespace), `workload`, `workload_type`, `container`, `format` (json/csv).

Response:

```json
{
  "meta": { "count": 42, "limit": 10, "offset": 0 },
  "links": { "first": "...", "last": "...", "next": "...", "previous": "..." },
  "data": [{
    "cluster_alias": "my-cluster",
    "cluster_uuid": "uuid",
    "container": "api-server",
    "id": "recommendation-set-uuid",
    "last_reported": "2026-03-26T...",
    "project": "my-namespace",
    "recommendations": { "...Kruize recommendation JSON..." },
    "source_id": "source-123",
    "workload": "api-deployment",
    "workload_type": "deployment"
  }]
}
```

**GET `/api/cost-management/v1/recommendations/openshift/:recommendation-id`** — Single recommendation

**GET `/api/cost-management/v1/openshift/namespace/recommendations`** — List namespace recommendations (similar shape with namespace-level fields).

### E.8 Kafka Topics and Message Formats


| Topic           | Default Name                    | Producer                        | Consumer                                 | Message Format                                                                                                                       |
| --------------- | ------------------------------- | ------------------------------- | ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Upload          | `hccm.ros.events`               | Koku listener (koku backend)    | ros-ocp-backend `ProcessReport`          | `KafkaMsg`: `request_id`, `b64_identity`, `metadata` (org_id, source_id, cluster_uuid, etc.), `files` (S3 URLs), `custom_timeframes` |
| Recommendations | `rosocp.kruize.recommendations` | ros-ocp-backend `ProcessReport` | ros-ocp-backend `PollForRecommendations` | `RecommendationKafkaMsg`: `org_id`, `workload_id`, `experiment_name`, `max_endtime_report`, `force_repoll`, `metric_type`            |
| Sources         | `platform.sources.event-stream` | Platform Sources                | ros-ocp-backend `sourcesListener`        | Source lifecycle events                                                                                                              |


### E.9 Key Constants

**Kruize Recommendation Constants** (from `RecommendationConstants.java`):


| Constant                             | Value      | Used In                                           |
| ------------------------------------ | ---------- | ------------------------------------------------- |
| `CPU_ONE_MILLICORE`                  | 0.001      | CPU idle threshold (below = no recommendation)    |
| `CPU_ONE_CORE`                       | 1.0        | Algorithm branch point (≥1 core: percentile path) |
| `MEM_USAGE_BUFFER_DECIMAL`           | 0.20 (20%) | Memory usage recommendation buffer                |
| `MEM_SPIKE_BUFFER_DECIMAL`           | 0.05 (5%)  | Memory spike recommendation buffer                |
| `COST_CPU_PERCENTILE`                | 60         | Cost model CPU percentile target                  |
| `PERFORMANCE_CPU_PERCENTILE`         | 98         | Performance model CPU percentile target           |
| `COST_MEMORY_PERCENTILE`             | 100        | Cost model memory percentile (max)                |
| `PERFORMANCE_MEMORY_PERCENTILE`      | 100        | Performance model memory percentile (max)         |
| `COST_ACCELERATOR_PERCENTILE`        | 50         | GPU cost model percentile                         |
| `PERFORMANCE_ACCELERATOR_PERCENTILE` | 98         | GPU performance model percentile                  |
| `SHORT_TERM_HOURS`                   | 24         | Minimum hours for short-term recommendation       |
| `MEDIUM_TERM_HOURS`                  | 168 (7d)   | Minimum hours for medium-term                     |
| `LONG_TERM_HOURS`                    | 360 (15d)  | Minimum hours for long-term                       |


**Kruize Runtime Constants** (mvp_demo, `RecommendationConstants.RuntimeConstants`):


| Constant                           | Value                      | Issue (from §18)                                |
| ---------------------------------- | -------------------------- | ----------------------------------------------- |
| `DEFAULT_MAX_RAM_PERCENTAGE_VALUE` | 80% (>512MB), 50% (≤512MB) | Ignores actual heap usage                       |
| `THREADS_PER_CORE`                 | 1                          | Undersizes vs Quarkus default `max(8, 2×cores)` |
| `MIN_CORE_THREADS`                 | 1                          | —                                               |
| `MAX_CORE_THREADS`                 | 64                         | —                                               |
| `JDK_VERSION_SHENANDOAH`           | 12                         | Min JDK for Shenandoah                          |
| `JDK_VERSION_ZGC`                  | 15                         | Min JDK for ZGC                                 |


**ros-ocp-backend Configuration** (key env vars from `internal/config/config.go`):


| Variable                                                  | Default                         | Purpose                               |
| --------------------------------------------------------- | ------------------------------- | ------------------------------------- |
| `KRUIZE_URL`                                              | `http://localhost:8090`         | Kruize API base URL                   |
| `KRUIZE_MAX_BULK_CHUNK_SIZE`                              | (config)                        | Max items per /updateResults call     |
| `RECOMMENDATION_POLL_INTERVAL_HOURS`                      | (config)                        | How often to re-poll recommendations  |
| `DATA_RETENTION_PERIOD`                                   | (config)                        | How long to keep historical data      |
| `RECORD_LIMIT_CSV`                                        | (config)                        | Max CSV rows to process               |
| `KAFKA_BOOTSTRAP_SERVERS`                                 | (Clowder or env)                | Kafka broker address                  |
| `KAFKA_CONSUMER_GROUP_ID`                                 | (config)                        | Consumer group                        |
| `UPLOAD_TOPIC`                                            | `hccm.ros.events`               | Inbound data topic                    |
| `RECOMMENDATION_TOPIC`                                    | `rosocp.kruize.recommendations` | Internal recommendation trigger topic |
| `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD` | (Clowder or env)                | PostgreSQL connection                 |
| `RBAC_ENABLE`                                             | (config)                        | Enable RBAC middleware                |
| `ROS_DISABLED_PLUGINS`                                    | (empty)                         | Denylist plugins (e.g. `namespace`)   |


### E.10 Operator Configuration (CRD spec)

The `CostManagementMetricsConfig` CRD (`costmanagement-metrics-cfg.openshift.io/v1beta1`) exposes:


| Field                                                                     | Default                      | Purpose                                   |
| ------------------------------------------------------------------------- | ---------------------------- | ----------------------------------------- |
| `spec.api_url`                                                            | `https://console.redhat.com` | API base URL                              |
| `spec.authentication.type`                                                | `token`                      | Auth method (token/basic/service-account) |
| `spec.prometheus_config.context_timeout`                                  | 120s                         | Prometheus query timeout                  |
| `spec.prometheus_config.collect_previous_data`                            | (bool)                       | Backfill on first run                     |
| `spec.prometheus_config.disable_metrics_collection_resource_optimization` | false                        | Disable ROS collection                    |
| `spec.packaging.max_size_MB`                                              | 100                          | Max tarball size                          |
| `spec.packaging.max_reports_to_store`                                     | 30                           | Max stored reports                        |
| `spec.upload.upload_cycle`                                                | 360 min (6h)                 | Upload frequency                          |
| `spec.upload.ingress_path`                                                | `/api/ingress/v1/upload`     | Ingress endpoint path                     |


### E.11 Aggregation Pipeline (ros-ocp-backend)

The `Aggregate_data` function in `internal/utils/aggregator.go` transforms raw CSV rows into Kruize-compatible grouped metrics:

1. **Validate:** `hasMissingColumnsCSV` checks all expected columns exist.
2. **Filter:** `filterValidCSVRecords` removes rows with:
  - Negative CPU/memory values
  - Empty owner/workload fields (container CSV)
  - Invalid workload types (not in enum: deployment, statefulset, daemonset, replicaset, replicationcontroller, deploymentconfig)
3. **Derive:** `determine_k8s_object_type` maps owner_kind/workload fields to k8s object type and name.
4. **Group:** gota `GroupBy` on `(namespace, k8s_object_type, k8s_object_name, workload, container_name, image_name, interval_start, interval_end)`.
5. **Aggregate:** Per group, compute SUM/MEAN/MIN/MAX for each metric column. Output columns get suffixes: `_SUM`, `_MEAN`, `_MIN`, `_MAX`.
6. **Build payload:** `kruizePayload.make_container_data` reads aggregated columns (e.g., `cpu_request_container_sum_SUM`, `cpu_usage_container_avg_MEAN`) to construct the Kruize `/updateResults` JSON payload.

### E.12 Kruize Recommendation Algorithm (Key Files)


| File (relative to autotune repo root)                                                                            | Purpose                                                                       |
| ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `src/main/java/com/autotune/analyzer/recommendations/model/GenericRecommendationModel.java`                      | Core CPU, memory, GPU, runtime recommendation logic                           |
| `src/main/java/com/autotune/common/utils/CommonUtils.java` (line 292)                                            | `percentile()` function — `Collections.sort()` + index lookup                 |
| `src/main/java/com/autotune/analyzer/recommendations/RecommendationConstants.java`                               | All percentile targets, thresholds, buffer values, notification codes         |
| `src/main/java/com/autotune/analyzer/recommendations/utils/RecommendationUtils.java`                             | GPU utility functions (MIG profile matching, frame buffer lookup)             |
| `src/main/java/com/autotune/common/data/system/info/device/accelerator/metadata/AcceleratorMetaDataService.java` | GPU MIG profile data (A100, H100, H200, B200, RTX PRO)                        |
| `src/main/java/com/autotune/analyzer/recommendations/engine/RecommendationEngine.java`                           | Outer loop: iterates terms × models, builds filtered result maps, calls model |
| `src/main/java/com/autotune/analyzer/recommendations/layers/HotspotLayerRecommendationHandler.java`              | (mvp_demo) JVM Hotspot: MaxRAMPercentage, GC policy                           |
| `src/main/java/com/autotune/analyzer/recommendations/layers/SemeruLayerRecommendationHandler.java`               | (mvp_demo) IBM Semeru: MaxRAMPercentage, GC policy                            |
| `src/main/java/com/autotune/analyzer/recommendations/layers/QuarkusLayerRecommendationHandler.java`              | (mvp_demo) Quarkus: thread pool core threads                                  |
| `src/main/java/com/autotune/analyzer/recommendations/LayerRecommendationHandlerRegistry.java`                    | (mvp_demo) Singleton registry for layer handlers                              |
| `src/main/java/com/autotune/analyzer/services/UpdateResults.java`                                                | `/updateResults` API handler (ingestion entry point)                          |
| `src/main/java/com/autotune/database/dao/ExperimentDAOImpl.java`                                                 | DAO: experiment/results CRUD, has `synchronized` bottleneck (§19.7)           |
| `src/main/java/com/autotune/database/table/KruizeResultsEntry.java`                                              | JPA entity for results (JSONB extended_data)                                  |


### E.13 Testing Infrastructure

**ros-ocp-backend:**

- Tests: `go test ./...` in repository root
- Test DB: PostgreSQL required (configured via same env vars)
- Mocking: Standard Go `httptest` for Kruize API, test Kafka consumer/producer

**Kruize:**

- Tests: `mvn test` (JUnit + Mockito)
- Integration tests: `tests/scripts/` directory contains shell scripts for API-level testing
- Test DB: PostgreSQL required

**Operator:**

- Tests: `make test` (Ginkgo + envtest)
- No external services required (Prometheus is mocked)

---

## Appendix F: Phasing Dependency Graph

This appendix maps dependencies between optimizations from this report. An arrow A → B means "A must be completed before B can start."

### Independent Changes (no prerequisites, can start immediately)

These can all be worked on in parallel:


| ID  | Optimization                                                                     | Component              | Effort        |
| --- | -------------------------------------------------------------------------------- | ---------------------- | ------------- |
| F1  | CPU algorithm fix, Option A (§12): remove 1-core discontinuity                   | Kruize                 | 1 PR          |
| F2  | Memory short-term fix (§16): replace sort with max, remove JSONObject overhead   | Kruize                 | 1 PR          |
| F3  | GPU gating bug fix (§17): add B200/RTX PRO to `checkIfModelIsKruizeSupportedMIG` | Kruize                 | 1 PR          |
| F4  | GPU frame buffer fix (§17): add 180GB/48GB cases to `getFrameBufferBasedOnModel` | Kruize                 | Same PR as F3 |
| F5  | JVM `THREADS_PER_CORE` fix (§18): change from 1 to `max(8, 2×cores)`             | Kruize (mvp_demo)      | 1 PR          |
| F6  | JVM Semeru rounding fix (§18): change `Math.round` to `Math.ceil`                | Kruize (mvp_demo)      | 1 PR          |
| F7  | `errorReasons` accumulation fix (§19.1)                                          | Kruize                 | 1 PR          |
| F8  | Interval map eviction (§19.2)                                                    | Kruize                 | 1 PR          |
| F9  | `autotuneObjectMap` size bound (§19.3)                                           | Kruize                 | 1 PR          |
| F10 | Cross-model duplicate work elimination (§19.4)                                   | Kruize                 | 1 PR          |
| F11 | `mergeResults` data loss fix (§19.5)                                             | Kruize                 | 1 PR          |
| F12 | `addExperimentToDB` synchronized removal (§19.7)                                 | Kruize                 | 1 PR          |
| F13 | `getTimestampWithinTolerance` TreeMap (§19.8)                                    | Kruize                 | 1 PR          |
| F14 | Per-row transaction batching (§13)                                               | Kruize                 | 1 PR          |
| F15 | Gson singleton reuse (§13)                                                       | Kruize                 | 1 PR          |
| F16 | HTTP client connection pooling (§13)                                             | Kruize                 | 1 PR          |
| F17 | RBAC nil panic fix (§20.1)                                                       | ros-ocp-backend        | 1 PR          |
| F18 | API 200-on-failure fix (§20.2)                                                   | ros-ocp-backend        | 1 PR          |
| F19 | Kafka type assertion panic fix (§20.3)                                           | ros-ocp-backend        | 1 PR          |
| F20 | HTTP timeout addition (§20, `ReadCSVFromUrl`, RBAC)                              | ros-ocp-backend        | 1 PR          |
| F21 | Poison message handling / dead-letter (§20.9)                                    | ros-ocp-backend        | 1 PR          |
| F22 | GORM `.Where()` bug fix (§20.11)                                                 | ros-ocp-backend        | 1 PR          |
| F23 | Idle workload detection (§23.1) — zero new queries                               | ros-ocp-backend/Kruize | 1-2 PRs       |
| F24 | QoS class recommendations (§23.5) — zero new queries                             | ros-ocp-backend/Kruize | 1 PR          |


### Sequential Dependencies

```
F25: Operator adds replica count queries (§26)
  → F26: ros-ocp-backend stores replica count
    → F27: API exposes replica count + total_savings
      → F28: UI displays total impact

F29: Operator adds OOM event query (§16 prerequisite)
  → F30: ros-ocp-backend passes OOM signal to Kruize
    → F31: Full memory algorithm (adaptive margin, OOM backoff, trend detection)

F32: Operator adds `kube_deployment_spec_replicas` etc. for HPA (§23.3)
  → F33: HPA optimization algorithm
    → F34: Combined VPA+HPA recommendation

F35: Operator adds `go_info` query (§23.4)
  → F36: Go GOMAXPROCS/GOMEMLIMIT recommendation

F37: Operator routes existing `cost:` PVC queries to ROS pipeline (§23.2)
  → F38: PVC right-sizing algorithm

F39: CSV → Thanos architectural change (§3) — **optional / historical; not v4.0**
  → F40: Integer types (millicores/KiB) through pipeline (§9)
  → F41: Thanos-based percentile computation (§10, simple path)
  → F42: Eliminate /updateResults HTTP calls (§25)

F43: T-digest in ros-ocp-backend (§10, advanced path) — **not used in v4.0;** replaced by exact percentiles via `slices.Sort()` on daily digest samples (§25)
  → F44: Decaying daily digest merge for custom timeframes (implemented in **Go**, not t-digest)
  → F45: Real-time recommendation at API request time (§25)

F46: Strategic: Drop Kruize from remote path (§24) — **v4.0**
  Requires: Native Go recommendation engine (`recommendCPU()`, `recommendMemory()`, `recommendAllWorkloads()`, …),
            daily digests in plain PostgreSQL, integer pipeline — **not** F39–F43
  → F47: All new recommendation types natively in Go
  → F48: Eliminate Kruize PostgreSQL database
```

### Phasing Summary


| Phase                       | Duration    | Changes                                            | Prerequisites                         |
| --------------------------- | ----------- | -------------------------------------------------- | ------------------------------------- |
| **0: Critical fixes**       | 2-4 weeks   | F7, F17, F18, F19 (crash/correctness bugs)         | None                                  |
| **1: Quick wins**           | 4-8 weeks   | F1-F24 (all independent changes)                   | None                                  |
| **2: Operator extensions**  | 2-4 weeks   | F25, F29, F32, F35, F37 (new Prometheus queries)   | None (parallel with Phase 1)          |
| **3: Backend integration**  | 4-8 weeks   | F26-F28, F30-F31, F33, F36, F38 (consume new data) | Phase 2                               |
| **4a: Thanos migration**    | 8-12 weeks  | F39-F42 (architectural change)                     | None (parallel with Phase 1-3); **not v4.0**        |
| **4b: Digests + on-demand API** | 4-8 weeks   | F43-F45 **concepts** (decaying merge, API-time recompute) implemented in **Go** + plain PG (§25) — **not** t-digest | Parallel with Phase 1                 |
| **5: Strategic (Option B)** | 16-24 weeks | F46-F48 (drop Kruize from remote path)             | **v4.0:** native Go engine + daily digests + algorithm maturity from Phase 1 |


---

## Appendix G: Assumptions, Scope, and Open Questions for Specification Writers

### Scope of This Analysis

1. **Modes analyzed:** Both `remote_monitoring` (ros-ocp-backend + Kruize) and `local_monitoring` (Kruize on cluster querying Prometheus directly). Findings are labeled per mode throughout.
2. **Branches analyzed:** `main`/`remote_monitoring` (stable) for all repos; `mvp_demo` for Kruize JVM/Quarkus features (§18, §19 layer-specific items). `COST-5691-custom-timeframes` for ros-ocp-backend custom timeframes design.
3. **Recommendations covered:** Container CPU/memory, namespace CPU/memory, GPU (MIG profile), JVM/Quarkus runtime (mvp_demo), plus 10 proposed new types (§23).
4. **Scale targets:** Analysis covers 500 to 20,000,000 containers with 1-day to 91-day retention windows.
5. **Provider scope:** OpenShift (OCP) only. AWS/Azure/GCP cost data flows through the Koku backend, not ros-ocp-backend.

### Assumptions

1. **Kruize API contract is negotiable.** The report assumes ros-ocp-backend can modify how it interacts with Kruize (or replace Kruize entirely per §24).
2. **Operator query budget has headroom.** Adding 2-22 new Prometheus queries (depending on phase) is assumed acceptable. Current count is ~73 queries per 15-minute interval.
3. **kube-state-metrics is available** on all target OpenShift 4.12+ clusters with the default monitoring stack. All proposed new queries use standard STABLE-status metrics.
4. **Integer types (millicores/KiB)** can be adopted without migrating historical data. The analysis assumes a clean cut-over where new data uses integers and old data is either re-processed or aged out.
5. **Kruize's performance profile** (`resource-optimization-openshift`) defines the expected metric names. Any new metrics (OOM events, HPA status, runtime info) require either profile updates or a parallel mechanism.
6. **PostgreSQL** is the only database for both ros-ocp-backend and Kruize. **v4.0 remote recommendations** store metrics digests and results in **plain PostgreSQL** — Thanos was an **optional** alternative metrics store in older proposals (§3, §28), not part of the shipped v4.0 pipeline.
7. **Kafka** is the message bus between Koku and ros-ocp-backend. The report does not propose changing this.

### What a Specification Writer Needs to Decide

For each optimization or feature from this report, the spec must define:

1. **API contracts:** Exact JSON schemas for new/modified endpoints (both Kruize internal and ros-ocp-backend external).
2. **Database migrations:** New columns, tables, indexes, and their types. For ros-ocp-backend, this means new golang-migrate SQL files. For Kruize, this means Hibernate entity changes.
3. **Operator query definitions:** Exact PromQL expressions, query type (instant vs range), result mapping to CSV columns, and new struct fields in `types.go`.
4. **CSV format changes:** New columns in `ros-openshift-container-{YYYYMM}.csv` or `ros-openshift-namespace-{YYYYMM}.csv`, and corresponding changes to `csvColumnMapping.go` in ros-ocp-backend.
5. **Manifest changes:** Whether new CSV files or fields need to be added to the manifest JSON.
6. **Feature flags:** Whether new features should be gated behind Unleash flags. The analysis recommends against unnecessary feature flags unless explicitly required.
7. **Backward compatibility:** How older operators (without new queries) interact with newer backends. The spec should define graceful degradation.
8. **Testing strategy:** Unit tests, integration tests, and what external services need to be mocked.
9. **Rollout plan:** For architectural changes (Thanos, dropping Kruize), define the migration path for existing deployments.

### Key Cross-Component Touchpoints

Any feature that requires data to flow from Prometheus to the UI touches **all four repositories**:

```
Operator (PromQL + CSV) → ros-ocp-backend (Kafka + DB + API) → Kruize (Algorithm) → UI (Display)
```

For the "ros-ocp-backend with superpowers" (§24) architecture, the chain simplifies to:

```
Operator (PromQL + CSV) → ros-ocp-backend (Kafka + Algorithm + DB + API) → UI (Display)
```

### Section Cross-Reference for Specification Derivation


| Spec Topic              | Report Sections              | Key Details                                                     |
| ----------------------- | ---------------------------- | --------------------------------------------------------------- |
| Replica count feature   | §26, E.3, E.4, F25-F28       | New operator queries, CSV columns, DB schema, API response      |
| Go digest + percentile engine (v4.0) | §10, §25, E.12, F43-F45      | Daily digests in PG; decay/merge + `slices.Sort()` percentiles + API integration in **Go** (not t-digest)     |
| OOM event collection    | §16, F29-F31                 | Operator query, CSV column, algorithm integration               |
| Idle workload detection | §23.1, F23                   | Threshold definition, notification format, no new queries       |
| PVC right-sizing        | §23.2, F37-F38               | Route existing cost queries, algorithm, new recommendation type |
| HPA optimization        | §23.3, F32-F34               | 8 new queries, algorithm, combined VPA+HPA model                |
| Go GOMAXPROCS           | §23.4, F35-F36               | 1 new query, detection logic, recommendation format             |
| Drop Kruize (Option B)  | §24, §25, E.6, E.12, F46-F48 | Port algorithms to Go, t-digest, eliminate HTTP calls           |
| GPU fixes               | §17, F3-F4                   | Code changes in RecommendationUtils.java                        |
| JVM/Quarkus fixes       | §18, F5-F6                   | Code changes on mvp_demo branch                                 |
| Integer types           | §9, F40                      | Operator struct changes, CSV format, Thanos storage             |
| Custom timeframes       | IMPL-PRD99, E.6              | /updateExperiment API, term_settings, business_hours            |


