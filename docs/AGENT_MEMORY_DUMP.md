# Agent Memory Dump — ROS OCP Native Engine Development

**Date:** 2026-04-29
**Purpose:** Complete, authoritative context for any AI agent or human reviewer to understand, verify, or resume the work on the ros-ocp-backend native recommendation engine. Read this entire file before doing anything.

**Scope:** 88 commits across 4 repositories, ~26,000 lines of Go/Python/SQL inserted, developed over 6 phases between March and April 2026.

---

## Table of Contents

1. [Project Goal and Motivation](#1-project-goal-and-motivation)
2. [Repository Map, Branches, and Commit Inventory](#2-repository-map-branches-and-commit-inventory)
3. [Phase 0: Critical Robustness Fixes (The Foundation)](#3-phase-0-critical-robustness-fixes-the-foundation)
4. [Phases 1-2-3: The Core Native Go Engine (The Big Rewrite)](#4-phases-1-2-3-the-core-native-go-engine-the-big-rewrite)
5. [Phase 4: OOM Feedback, Quality Tracking, and Cross-Repo Wiring](#5-phase-4-oom-feedback-quality-tracking-and-cross-repo-wiring)
6. [Phase 5: Historical Tracking, Boxplots, and Retention](#6-phase-5-historical-tracking-boxplots-and-retention)
7. [Phase 6: Namespace Recs, GPU Engine, Savings, History API, and More](#7-phase-6-namespace-recs-gpu-engine-savings-history-api-and-more)
8. [GPU Recommendations: Complete Technical Deep-Dive](#8-gpu-recommendations-complete-technical-deep-dive)
9. [Cost Impact / Savings Estimation: Complete Technical Deep-Dive](#9-cost-impact--savings-estimation-complete-technical-deep-dive)
10. [Cross-Repository Changes: Koku, Operator, Nise](#10-cross-repository-changes-koku-operator-nise)
11. [Apollo E2E Test Cluster: Everything You Need to Know](#11-apollo-e2e-test-cluster-everything-you-need-to-know)
12. [Comprehensive Bug and Fix Registry](#12-comprehensive-bug-and-fix-registry)
13. [Database Schema: Complete State](#13-database-schema-complete-state)
14. [API Endpoint Inventory](#14-api-endpoint-inventory)
15. [Key Architectural Decisions and Their Rationale](#15-key-architectural-decisions-and-their-rationale)
16. [Known Issues, Gaps, and Future Work](#16-known-issues-gaps-and-future-work)
17. [Development Environment and Testing](#17-development-environment-and-testing)
18. [Hard-Won Rules: What NOT to Do](#18-hard-won-rules-what-not-to-do)
19. [Plan Documents and Reference Files](#19-plan-documents-and-reference-files)
20. [Key File Index](#20-key-file-index)

---

## 1. Project Goal and Motivation

### What is ros-ocp-backend?

ros-ocp-backend is the Go service that receives OpenShift container resource usage data (via Kafka from the koku-metrics-operator), computes CPU/memory/GPU right-sizing recommendations, and serves them via a REST API. The koku-ui frontend consumes this API to show "Resource Optimization" recommendations to users.

### Why Replace Kruize?

The original recommendation engine was **Kruize (Autotune)**, a Java application running as a sidecar. ros-ocp-backend would receive CSV data via Kafka, forward it to Kruize's `/createExperiment` and `/updateResults` REST endpoints, and then query Kruize's `/listRecommendations` to get results. This had several problems:

1. **Black-box recommendations**: Kruize's algorithm was opaque. It used absolute-max P100 percentiles, which over-provisioned memory by 10-50% for stable workloads. We couldn't tune it.
2. **Operational complexity**: Running Kruize as a Java process alongside the Go service required separate lifecycle management, health checks, and resource allocation.
3. **No GPU support**: Kruize had GPU recommendation scaffolding in its code, but it was never productized or tested. Its approach was limited to simple idle/underutilized classification without profiling metrics.
4. **No cost awareness**: Kruize had no concept of cost — recommendations were purely resource-based with no dollar savings estimates.
5. **No history or quality tracking**: No way to know if recommendations were stable, accurate, or adopted by users.

### What We Built

A native Go recommendation engine that replaces Kruize entirely, with:

- **Decay-weighted percentiles** instead of absolute-max: Deliberately ~10-16% less memory for stable workloads (this is a feature, not a bug — OOM feedback closes the safety gap)
- **GPU workload classification** using DCGM profiling metrics (PROF_SM_ACTIVE, PROF_PIPE_TENSOR_ACTIVE, PROF_DRAM_ACTIVE) — no competitor does this
- **MIG profile right-sizing** for A100/A30/H100/H200/B100/B200
- **Dollar savings estimates** by querying Koku's cost model rates
- **Replica count awareness** from the operator's workload_pod_count metric
- **Recommendation history and quality tracking** for trend analysis
- **Namespace-level recommendations** with boxplot visualizations
- **Custom timeframe settings** (configurable term windows per org)
- **24+ notification codes** for actionable alerts (idle, OOM, insufficient data, HPA, GPU-specific)

The engine works both on-prem (PostgreSQL-only) and SaaS (with Trino), and is at least as good as competitors (KubeCost, Cast.ai, Utilyze) for GPU recommendations, with a unique advantage in memory-bound detection.

---

## 2. Repository Map, Branches, and Commit Inventory

### ros-ocp-backend (PRIMARY — 88 commits, ~26,000 lines added)
- **Path:** `/home/pgarciaq/dev/koku/ros-ocp-backend/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase6`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/ros-ocp-backend.git`
- **Origin:** `https://github.com/RedHatInsights/ros-ocp-backend.git`
- **Phase branches:** `pgarciaq-rosocp-superpowers-phase0` through `phase6` (each extends the previous)
- **Language:** Go 1.25, using `pgxpool` (pgx/v5) for raw SQL and GORM for legacy queries
- **Test framework:** `testcontainers-go` with PostgreSQL 16 for integration tests, `stretchr/testify` for assertions

### koku (backend — 4 commits)
- **Path:** `/home/pgarciaq/dev/koku/koku/`
- **Branch:** `pgarciaq-rosocp-superpowers`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku.git`
- **Key commits:**
  - `d10909686` — Add `effective_rates` masu endpoint (the API ros-ocp-backend calls for cost data)
  - `6fe59f939` — Include all distributed cost types (platform, worker, storage, network, GPU) in effective-rates
  - `faf88a188` / `23c0dc53b` — AGENTS.md documentation updates

### koku-metrics-operator (Go operator — 4 commits)
- **Path:** `/home/pgarciaq/dev/koku/koku-metrics-operator/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku-metrics-operator.git`
- **Key commits:**
  - `3383cf6c` — Add OOM count PromQL query and CSV column
  - `1001a815` — Add workload_pod_count PromQL query and CSV column
  - `924de0f4` — Replace misleading DCGM DEV_ metrics with PROF_ profiling metrics
  - `603b37b0` — Add unit tests for OOM count

### nise (Python test data generator — 7 commits)
- **Path:** `/home/pgarciaq/dev/koku/nise/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `https://github.com/pgarciaq/nise.git`
- **Key commits:**
  - `c92f444` — Add oom_count column to ROS CSV generation
  - `d67a6e1` — Add workload_pod_count column
  - `1057bbe` — Add GPU profiling metrics to ROS CSV generation (14 new columns)

### koku-ui (NO CHANGES — waiting for UX mockups from Stefan)
- **Path:** `/home/pgarciaq/dev/koku/koku-ui/`
- **Branch:** `main`

---

## 3. Phase 0: Critical Robustness Fixes (The Foundation)

**Why we started here:** Before building the native engine, we audited the existing ros-ocp-backend codebase and found 12 critical bugs. Several could cause data loss or infinite loops in production.

### The 12 Bugs Fixed

1. **RBAC nil pointer panic**: `RbacService.GetPermissions()` returned nil on error. The handler dereferenced it without checking. Fix: nil check + 403 response.

2. **API returning 200 on DB failure**: `GetRecommendationSetList` swallowed GORM errors and returned an empty 200 instead of 500. Fix: proper error propagation.

3. **Kafka type assertion panic**: `processMessage()` did `msg.Value.([]byte)` without checking the type assertion. A nil or wrong-type message would panic and crash the consumer. Fix: comma-ok pattern.

4. **Subscribe failure silently ignored**: `KafkaConsumer.Subscribe()` error was logged but the consumer continued as if it had subscribed, entering an infinite poll loop that consumed 100% CPU doing nothing. Fix: return error and exit.

5. **Missing HTTP timeouts**: The `http.Client` used for Kruize REST calls had no timeout. A hung Kruize process would block the goroutine forever. Fix: 30-second default timeout (configurable via `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS`).

6. **Non-deterministic CSV row order**: CSV rows were collected into a map keyed by container name, losing insertion order. This made tests flaky and debugging hard. Fix: stable sort by (namespace, workload, container, timestamp).

7. **Poison message infinite redelivery**: If CSV parsing failed (corrupt data, schema mismatch), the message was nacked and redelivered indefinitely. Fix: dead-letter handling with max retry count.

8. **GORM error ignored**: `db.Create(&record).Error` was called but the error was never checked. Failed inserts were silently lost. Fix: error check + log + return.

9. **Date parse error swallowed**: `time.Parse(layout, dateStr)` errors were logged at debug level and the function returned a zero time. Downstream code would create records with `0001-01-01` dates. Fix: propagate error to caller.

10. **Kafka payload logged at Info level**: The entire CSV payload (potentially MBs) was logged at Info level on every message. Fix: log at Debug level with truncation.

11. **SendMessage failure not reconciled**: When `SendMessage` to the output topic failed, the error was swallowed. Fix: error propagation + retry.

12. **Context cancellation not respected**: Long-running CSV processing didn't check `ctx.Done()`. A graceful shutdown signal would be ignored until processing completed. Fix: context checks at key iteration points.

**Approach:** Strict TDD. For each bug, we wrote a failing test first, then fixed the production code. This established the test infrastructure patterns used throughout all subsequent phases.

**PR note:** These fixes were initially submitted as PR #617 to the upstream repo. The PR was reviewed, and we addressed review feedback in follow-up commits (`f21cfa5`, `ba45963`, `8edbee5`). The fixes were eventually merged as commit `5ca31ea`.

---

## 4. Phases 1-2-3: The Core Native Go Engine (The Big Rewrite)

**This is the biggest change — roughly 15,000 lines of new Go code.**

### Architecture: How Recommendations Flow

```
Kafka Message (hccm.ros.events)
  → Download CSV from S3/MinIO
  → Parse CSV rows (float→int64 conversion, NaN/Inf validation)
  → Group by (container, day)
  → Compute exact percentiles on ~96 int64 values per day
  → Upsert into daily_container_digests (partitioned by month)
  → Read max window (single SELECT, all terms at once)
  → Compute N term recommendations (decay, percentile, margin)
  → Batch write to recommendation_sets
  → Write raw samples for boxplots (container_usage_samples)
```

### Key Design Decision: Averaged P95 vs Kruize's Absolute Max

Kruize uses P100 (absolute maximum) for memory recommendations. This means a single spike determines the recommendation, even if it happened once in 15 days. Our engine uses P95 with adaptive margin:

- **Base margin:** 15% above P95
- **Adaptive margin:** When P95-P50 spread is large (high variance), margin increases up to 50%
- **Formula:** `margin = base_margin + (max_margin - base_margin) * min(1.0, cv / cv_threshold)`
- **Result:** ~10-16% less memory than Kruize for stable workloads

This is deliberate. Tighter recommendations are more cost-efficient. The OOM feedback mechanism (Phase 4) closes the safety gap: if a container gets OOMKilled, the next recommendation automatically bumps up via logarithmic scaling.

We built a comparison tool (`docs/kruize-vs-native-comparison.md`) to quantitatively verify this difference against real cluster data.

### The Recommendation Algorithm in Detail

**CPU Recommendation (`engine/recommend_cpu.go`):**
1. Load all `daily_container_digests` rows within the term window
2. Apply exponential decay weights based on age (newer days weighted more)
3. Compute decay-weighted percentile at the configured level (P60 for cost profile, P98 for performance)
4. Apply a 25-millicore floor (never recommend less than 25mc — prevents CPU throttling)
5. Produce dual output: cost recommendation (lower, saves money) and performance recommendation (higher, prevents throttling)

**Memory Recommendation (`engine/recommend_memory.go`):**
1. Load digest rows, apply decay weights
2. Compute weighted percentile (P95 for cost, P99 for performance)
3. Apply adaptive margin based on the coefficient of variation between P50 and P95
4. Set limit = max(recommendation, P99) with additional headroom
5. Ensure limit ≥ request (critical: Kubernetes rejects pods where limit < request)

**Idle Detection (`engine/detect_idle.go`):**
- If max CPU usage across all days in the term window is < 10 millicores, the workload is classified as idle
- Emits `NotifIdleWorkload` notification code
- Still produces a recommendation (25mc floor) so the user can see what "right-sizing" looks like

**Trend Detection (`engine/trend.go`):**
- Linear regression slope over the term window
- If the slope is positive and statistically significant (R² > 0.5), emit `NotifResourceUsageIncreasing`
- Used in namespace recommendations for memory trend warnings

### Integer Arithmetic Everywhere

A critical early design decision: all metric values are stored as **int64** (millicores for CPU, KiB for memory). This avoids floating-point precision issues in percentile calculations and makes comparison/equality checks reliable. The CSV parser (`ingestion/csvparser.go`) converts float strings to int64 at the boundary:

- `CoreToMillicores("0.025")` → `25` (int64, millicores)
- `BytesToKiB("1073741824")` → `1048576` (int64, KiB = 1 GiB)

### The Test Infrastructure

We invested heavily in test infrastructure because the engine is data-intensive:

- **`internal/testutil/testdb.go`**: Spins up PostgreSQL 16 via `testcontainers-go`, runs all `golang-migrate` migrations, returns a `*pgxpool.Pool`. Uses `t.Cleanup()` for automatic teardown.
- **`internal/testutil/fixtures.go`**: Deterministic test data constants (`TestOrgID = "org1234567"`, `TestClusterUUID`, `BaseDate = 2026-01-15`) and helper functions (`SeedContainerDigest()`, `SeedDigestSeries()`, `SeedGPUDigest()`).
- All integration tests use real PostgreSQL — no mocking the database. This caught several SQL bugs that would have been missed with mocks.

### Migration Numbering

A recurring operational challenge: migration numbering. The phase branch system meant each phase added migrations, and when phases were squashed or rebased, migration numbers conflicted. We had to renumber migrations multiple times:
- `5513fce` Renumber pre-phase6 migrations 000023-000036 to 000024-000037
- `11f1c8e` Renumber phase6 migrations 000030-000035 to 000031-000036
- `c29153d` Renumber migration 000029 to 000030 for container_usage_samples

**Lesson:** Always check the highest existing migration number before adding new ones. The current highest migration is **000043** (GPU container digests unique constraint).

---

## 5. Phase 4: OOM Feedback, Quality Tracking, and Cross-Repo Wiring

**This was the first phase that required changes across multiple repositories.**

### OOM Feedback Mechanism

When a container is OOMKilled, the operator reports `oom_count` in the CSV. The native engine uses this to bump memory recommendations:

```
bump_factor = 1 + 0.15 × log₂(1 + oom_count)
```

- 1 OOM → 1.15× (15% bump)
- 3 OOMs → 1.30× (30% bump)
- 7+ OOMs → capped at 1.60× (60% bump)

The logarithmic curve means the first few OOMs have the biggest impact (recovering quickly from under-provisioning), while diminishing returns prevent runaway over-provisioning from repeated OOMs.

**Implementation:** `engine/recommend_all.go` applies the bump after computing the base percentile recommendation, before the adaptive margin.

### Cross-Repository Changes

This phase required synchronized changes across three repos:

1. **koku-metrics-operator:** Added `oom_count` PromQL query (`kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}`) and `workload_pod_count` query (`kube_pod_container_status_ready`). Both added as new CSV columns.

2. **nise:** Added `oom_count` and `workload_pod_count` to the ROS CSV generation. For oom_count, we added both random generation (90% chance of 0, 10% chance of 1-5) and deterministic YAML support (`oom_count: 3` in static data).

3. **ros-ocp-backend:** CSV parser updated to read the new columns. Quality writer (`engine/quality.go`) computes 4 metrics:
   - `oom_events_after_rec`: OOM events since last recommendation change
   - `stability_pct`: How much the recommendation changed vs previous snapshot
   - `adoption_detected`: Whether current resource requests match the recommendation
   - `recommendation_age_hours`: How long since the recommendation last changed

### CSV Column Alignment Headache

A significant debugging effort was required because the CSV column names in ros-ocp-backend's parser didn't match what the operator and nise actually produce. The operator outputs columns like `cpu_request` (in cores, as float) while the parser expected `cpu_request_millicores`. We had to trace through three codebases to align:

- **Operator produces:** `cpu_request`, `cpu_limit`, `mem_request`, `mem_limit` (cores/bytes)
- **Nise produces:** Same column names as operator
- **Parser expected:** Different names in some cases

Fix: `3cfd63d` — Align native CSV parser columns with operator/nise output. This was done by reading the actual CSV headers from operator-generated and nise-generated files and updating the parser's column name constants.

### Quality Writer: Careful Design

The quality writer was designed to be **internal-only** (not exposed via API initially) because the metrics need time to be meaningful. You can't measure "stability" until you have at least 2 recommendation snapshots, and "adoption" detection requires comparing current resource requests against the recommendation — which needs the user to actually apply the recommendation.

We later exposed it via API in Phase 6 when the data had accumulated enough to be useful.

---

## 6. Phase 5: Historical Tracking, Boxplots, and Retention

### Recommendation History

Every time the engine computes a recommendation, it writes a snapshot to `recommendation_history` (partitioned by month). This enables:
- Trend analysis: "How has the CPU recommendation changed over the last 30 days?"
- Audit trail: "What was the recommendation on date X?"
- Quality correlation: "Did the recommendation stabilize after OOM events?"

### Raw Usage Samples and Boxplots

Originally we planned to compute boxplots from digest data, but the 15-minute aggregation in digests loses too much resolution. Instead:

1. **`container_usage_samples`** table stores raw per-15-minute measurements (CPU millicore, memory KiB)
2. **Boxplot assembly** (`model/boxplot.go`) uses PostgreSQL's `percentile_cont()` function at query time to produce exact five-number summaries (min, Q1, median, Q3, max)
3. This gives precise boxplots without storing them — they're computed fresh from the samples on each API request

**Design tradeoff:** Storing raw samples increases storage significantly. The retention sweep (`engine/retention.go`) mitigates this by dropping old monthly partitions. Default retention: 6 months for samples/digests, 90 days for history/quality.

### The DetailResponse Refactor

A painful but necessary change: the original code built the API detail response by manipulating raw JSON (`map[string]interface{}`), which was:
- Type-unsafe (runtime panics on wrong type assertions)
- Impossible to extend (adding fields required careful JSON path manipulation)
- Incompatible with the new fields (boxplots, savings, GPU recommendations)

We replaced this with a strongly-typed `DetailResponse` struct (`model/detail_response.go`) that produces the same JSON shape as Kruize's response (for UI compatibility) but with proper Go types. This was commit `8d08443` and required fixing several downstream tests (`86c0cce`).

### Retention Sweep

A background goroutine (`engine/retention.go`) runs periodically (default: daily) and drops partitions older than the configured retention period:
- `daily_container_digests_YYYYMM` — dropped after 6 months
- `container_usage_samples_YYYYMM` — dropped after 6 months
- `recommendation_history_YYYYMM` — dropped after 90 days
- `recommendation_quality_YYYYMM` — dropped after 90 days

The sweep is safe: it only drops partitions that are entirely outside the retention window (never drops the current month).

---

## 7. Phase 6: Namespace Recs, GPU Engine, Savings, History API, and More

Phase 6 is the largest and most diverse phase, covering 8 distinct features. Each is described in detail in its own section below.

### 7.1 Namespace Recommendations

**Motivation:** Users wanted to see "how much does this namespace cost?" and "should I resize this namespace's quota?". Container-level recommendations don't answer these questions.

**Implementation:**
- `engine/recommend_namespace.go`: Aggregates container-level recommendations to namespace level
- `ingestion/namespace.go`: Computes namespace-level digests from container digests
- `daily_namespace_digests` table: Stores namespace-level percentiles
- Namespace boxplots: Memory P60/P98/P99 percentiles computed from `namespace_usage_samples`
- Memory trend slope notifications: If namespace memory usage is trending upward, emit warning

### 7.2 Replica Count

**Motivation:** "You have 3 replicas of this workload" is essential context for the recommendation. A user needs to know whether to apply the recommendation to 1 pod or 100.

**Implementation:**
- Operator adds `workload_pod_count` column (PromQL: `kube_pod_container_status_ready`)
- Nise generates this column in test data
- `engine/aggregate_pod_counts.go`: Computes min/max/avg from daily digest data
- Migration 000039: Adds `pod_count_min`, `pod_count_max`, `pod_count_avg` to `recommendation_sets`
- API returns `recommendations.replicas.{min, max, avg}`

**Fallback mechanism:** If the operator doesn't report `workload_pod_count` (older operator versions), the engine falls back to counting distinct pod names in the CSV data. This is less accurate (pods that appear and disappear within a single 15-minute interval are missed) but better than nothing.

### 7.3 CPU/Memory Savings Estimation

See [Section 9](#9-cost-impact--savings-estimation-complete-technical-deep-dive) for the complete technical deep-dive.

### 7.4 Historical Tracking / Quality API

**Implementation:**
- `api/handlers_history.go`: `GET /recommendations/openshift/history` — paginated, filterable, CSV-exportable
- `api/handlers_quality.go`: `GET /recommendations/openshift/quality` — same
- Both support filtering by cluster, project, workload, container, term, engine, date range
- `Cache-Control: private, max-age=300` headers added to recommendation endpoints

### 7.5 cluster_uuid TEXT → UUID Migration

**Problem:** The `clusters` table had `cluster_uuid TEXT`, but `recommendation_sets` also had `cluster_uuid TEXT`. We discovered that PostgreSQL's UUID comparison operators (`=`, `IN`) don't work between `UUID` and `TEXT` columns, causing `ERROR: operator does not exist: uuid = text (SQLSTATE 42883)` at query time.

**Fix (migration 000041):** Convert both `clusters.cluster_uuid` and `recommendation_sets.cluster_uuid` from TEXT to UUID type using `ALTER TABLE ... ALTER COLUMN ... TYPE UUID USING cluster_uuid::uuid`. This was a multi-table migration because the FK relationship required both sides to be the same type.

**Lesson learned:** Always check both sides of a FK/join relationship when changing column types.

### 7.6 GPU Recommendations Engine

See [Section 8](#8-gpu-recommendations-complete-technical-deep-dive) for the complete technical deep-dive.

### 7.7 Custom Timeframe Settings API

**Implementation:**
- `api/handlers_terms.go`: CRUD for `org_recommendation_terms`
- `GET /recommendations/openshift/settings/terms` — returns current term configuration
- `PUT` — updates window_days and decay_halflife_hours per term (1-90 days range)
- `DELETE` — resets to defaults
- `engine/term_config.go`: `LoadTermConfig()` reads per-org overrides at engine run time

### 7.8 OpenAPI Spec Updates

The OpenAPI spec (`openapi.json`) was updated comprehensively to cover all new endpoints, request/response schemas, query parameter documentation, and error responses. This was done incrementally as each feature was added.

---

## 8. GPU Recommendations: Complete Technical Deep-Dive

### Why Standard DCGM Metrics Are Wrong

The Utilyze blog post (https://www.systalyze.com/utilyze) and our own analysis showed that `DCGM_FI_DEV_GPU_UTIL` systematically overstates utilization for memory-bound workloads. An LLM decode workload can show 99% "GPU utilization" while only achieving 6% of peak compute throughput, because `DEV_GPU_UTIL` measures "percentage of time at least one SM kernel is running" — not actual computational work.

### The DCGM Metric Replacement (Operator Change)

We replaced the misleading DEV_ metrics with PROF_ profiling metrics:

| Old Metric | New Metric | What It Measures |
|---|---|---|
| `DCGM_FI_DEV_GPU_UTIL` | Removed | Was: % time any SM kernel running |
| `DCGM_FI_DEV_MEM_COPY_UTIL` | Removed | Was: % time data moving across bus |
| — | `DCGM_FI_PROF_SM_ACTIVE` (1002) | % cycles an SM has ≥1 warp scheduled |
| — | `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` (1004) | % cycles tensor cores are active |
| — | `DCGM_FI_PROF_DRAM_ACTIVE` (1005) | % cycles HBM interface is active |
| `DCGM_FI_DEV_FB_USED` | Kept | Frame buffer memory used (MiB) |

**Why PROF_ metrics are better:** They measure actual hardware unit activity, not just "something is running". A memory-bound LLM shows high `DRAM_ACTIVE` but low `PIPE_TENSOR_ACTIVE`, which correctly identifies it as underutilizing compute resources.

**Minimum DCGM Exporter:** v3.1.x (DCGM 2.x+). Avoid v4.0.x-4.1.x (regression). Recommended: v4.2.3+. Current GPU Operator ships v4.5.1+.

### Two-Tier GPU Support Model

Not all NVIDIA GPUs support profiling metrics. We handle this with a two-tier model:

**Tier 1 (Turing and newer — T4, A10, A30, A100, L4, L40, L40S, H100, H200, B100, B200):**
- Full PROF_ metrics available
- Complete classification: idle, underutilized, memory_bound, compute_bound_underutil, well_utilized
- Confidence 0.6 (medium) for short observation periods, up to 0.9 for 14+ days

**Tier 2 (Pre-Turing — P40, P100, V100):**
- Only frame buffer usage (PROF_ metrics broken or unavailable)
- Cannot classify utilization
- Returns notification code 28 (`NotifGPUNoProfilingData`) explaining why
- Confidence 0.3 (low)

**V100 special case:** Despite being Volta architecture (which theoretically supports PROF_ metrics), modern DCGM versions have broken PROF_ metric collection on V100. We treat V100 as Tier 2.

### Classification Algorithm (`engine/gpu_recommender.go`)

```
function RecommendGPU(digests []GPUDigestRow) *GPURec:
    avg_sm = mean(digests.sm_active_avg)
    avg_tensor = mean(digests.tensor_pipe_active_avg)
    avg_dram = mean(digests.dram_active_avg)

    if all profiling metrics are zero:
        return Tier 2 result (notification code 28)

    if avg_sm < 0.05:
        classification = "idle"
        notification = NotifGPUIdle (26)
    elif avg_sm < 0.30:
        if avg_dram > 2 × avg_sm:
            classification = "memory_bound"
            notification = NotifGPUMemBound (27)
        else:
            classification = "underutilized"
            notification = NotifGPUUnderutilized (10)
    else:
        classification = "well_utilized"
        no notification

    if gpu supports MIG and classification is underutilized:
        recommend smallest MIG profile that fits fb_usage_max
```

### GPU Metadata (`engine/gpu_metadata.go`)

The `GPUModels` map contains specs for every supported NVIDIA GPU:

```go
"A100": {
    Name: "NVIDIA A100",
    Arch: ArchAmpere,
    FBCapacityMiB: 81920,  // 80 GB
    MIGCapable: true,
    Profiles: []MIGProfile{
        {Name: "1g.10gb", Slices: 1, FBCapacityMiB: 10240},
        {Name: "2g.20gb", Slices: 2, FBCapacityMiB: 20480},
        {Name: "3g.40gb", Slices: 3, FBCapacityMiB: 40960},
        {Name: "4g.40gb", Slices: 4, FBCapacityMiB: 40960},
        {Name: "7g.80gb", Slices: 7, FBCapacityMiB: 81920},
    },
}
```

GPU model matching is fuzzy: `MatchGPUModel("NVIDIA A100-SXM4-80GB")` matches "A100" by substring.

### GPU Digest Pipeline (The Critical Gap We Found and Fixed)

The GPU engine was initially implemented with all the classification and recommendation logic, but **the data pipeline was missing**. The CSV data was parsed (MetricRow included GPU fields), but:

1. `ProcessCSVToDigests` only wrote CPU/memory digests — GPU fields were silently ignored
2. No code existed to query `gpu_container_digests` and attach results to API responses

**This was the single biggest gap we found during E2E testing on Apollo.** The API returned CPU/memory recommendations perfectly, but the GPU block was always empty because no GPU data was ever written to the database.

**Fix (commit `50d953c`):** Three new components:

1. **`ingestion/pipeline.go:upsertGPUDigests()`**: After the existing CPU/memory digest upsert, iterate through MetricRow objects, filter those with `HasGPU() == true`, group by container+day, compute min/max/avg for each GPU metric, and INSERT into `gpu_container_digests` with ON CONFLICT UPDATE.

2. **`engine/gpu_query.go:QueryGPURecommendations()`**: SELECT from `gpu_container_digests` for a given cluster and time range, group by container key, call `RecommendGPU()` for each container.

3. **`api/gpu_enrichment.go:enrichWithGPU()`**: Called from API handlers after fetching NativeContainerResults. Groups results by cluster, calls QueryGPURecommendations, attaches GPU recommendations to results, fetches cost data from Koku, applies GPU savings.

**Import cycle resolution:** Initially, `gpu_enrichment.go` was placed in `internal/model/` (where the NativeContainerResult struct lives). But `model` imports `engine` for recommendation types, and `engine` imports `model` for the result types — creating an import cycle. The fix was to move `gpu_enrichment.go` to `internal/api/`, which already imports both packages.

**Migration 000043:** Added unique constraint `CREATE UNIQUE INDEX IF NOT EXISTS gpu_container_digests_natural_key ON gpu_container_digests (cluster_uuid, namespace, workload, container_name, interval_start)` — required for the `ON CONFLICT` upsert to work.

### GPU API Filters (In-Memory Post-Enrichment)

GPU data lives in `gpu_container_digests`, not in `recommendation_sets`. This means SQL-level filtering (WHERE clause) can't be used. Instead, we filter in memory after enrichment:

- `has_gpu=true|false`: Filter by whether the result has a non-nil GPU block
- `gpu_model=A100`: Substring match on `CurrentGPUModel`
- `gpu_classification=idle`: Exact match on `GPUClassification`

Implementation: `filterGPUResults()` and `matchesAny()` in `api/gpu_enrichment.go`, called from `handlers.go` after `enrichWithGPU()`.

### GPU Savings Estimation

See [Section 9](#9-cost-impact--savings-estimation-complete-technical-deep-dive).

---

## 9. Cost Impact / Savings Estimation: Complete Technical Deep-Dive

### The effective_rates Endpoint (Koku Backend)

We created a new internal Masu API endpoint in Koku:

`GET /api/cost-management/v1/effective_rates/?org_id=1234567&cluster_id=<UUID>&start_date=...&end_date=...`

This returns:
```json
{
    "cluster_id": "d4e5f6a7-...",
    "provider_uuid": "d665a309-...",
    "distribution_type": "cpu",
    "markup_pct": 15.0,
    "configured_rates": {
        "cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.015},
        "cpu_core_request_per_hour": {"infrastructure": 0.1, "supplementary": 0.35},
        "memory_gb_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.012},
        "memory_gb_request_per_hour": {"infrastructure": 0.03, "supplementary": 0.08},
        "gpu_cost_per_month": {"infrastructure": 2500.0, "supplementary": 0.0},
        ...
    },
    "namespace_aggregates": {
        "namespace-1": {
            "cost_model_cpu_cost": 123.45,
            "infrastructure_cost": 678.90,
            "distributed_cost": 50.0,
            ...
        }
    }
}
```

**Key implementation details:**
- File: `koku/masu/api/effective_rates.py`
- Queries `CostModel` + `CostModelMap` to find the cost model for the given cluster
- Queries `OCPUsageLineItemDailySummary` for namespace-level cost aggregates
- Includes all distributed cost types (platform, worker, storage, network, GPU)
- **CRITICAL:** `org_id` parameter must be numeric (e.g., `1234567`), NOT prefixed with "org". The Koku code internally prepends "org" to form the schema name.

### CPU/Memory Savings Formula

```
savings = (current_request - recommended_request) × rate × monthly_pod_hours
```

Where:
- `current_request`: Current Kubernetes resource request (from the recommendation set's `current_*` fields)
- `recommended_request`: The engine's recommendation
- `rate`: Sum of infrastructure + supplementary rates for the metric (e.g., `cpu_core_request_per_hour`)
- `monthly_pod_hours`: `pod_count_avg × hours_in_month` (default 730 hours/month)

**Rate selection logic:** The engine uses the rate that matches the customer's metering strategy:
- If `cpu_core_usage_per_hour` is non-zero → usage-based metering
- If `cpu_core_request_per_hour` is non-zero → request-based metering
- The engine picks the highest non-zero rate (conservative: shows maximum savings)

**Distributed cost inclusion:** The effective_rates endpoint aggregates distributed costs (platform_distributed, worker_distributed, etc.) per namespace. These are included in the savings estimate as overhead that would also decrease proportionally.

### GPU Savings Formula

```python
if classification == "idle":
    savings = gpu_cost_per_month  # could remove GPU entirely
elif classification in ("underutilized", "memory_bound", "compute_bound_underutil"):
    if recommended_mig_profile != "" and recommended_mig_profile != "full_gpu":
        savings = (1 - recommended_slices / total_slices) * gpu_cost_per_month
    else:
        savings = $0  # can't save via MIG, but cost data is available
elif classification == "well_utilized":
    savings = $0  # no waste
```

**nil vs $0 semantics:**
- `nil` (JSON `null`): "We don't know" — no cost data available from Koku
- `$0`: "We know there's no savings opportunity" — cost data is available, GPU is well-utilized

This distinction matters for the UI: nil should show "N/A" or "Cost data unavailable", while $0 should show "$0.00/month".

### The org_id Prefix Bug

A subtle but critical bug: ros-ocp-backend stores `org_id` with the "org" prefix (e.g., `org1234567`) because that's how the Kafka message and x-rh-identity header provide it. But Koku's internal APIs expect the numeric-only form (e.g., `1234567`) because they prepend "org" internally when forming the schema name.

When `enrichWithGPU()` called `costProvider.GetEffectiveRates(ctx, "org1234567", ...)`, Koku tried to look up schema `orgorg1234567` — which doesn't exist, returning an error.

**Fix:** `kokuOrgID := strings.TrimPrefix(orgID, "org")` in `gpu_enrichment.go` before calling the cost provider.

---

## 10. Cross-Repository Changes: Koku, Operator, Nise

### Koku Changes

**Branch:** `pgarciaq-rosocp-superpowers` (on `pgarciaq` remote)

| Commit | Description |
|---|---|
| `d10909686` | Add `effective_rates` masu endpoint. New file: `masu/api/effective_rates.py`. Registered in `masu/api/urls.py`. Returns cost model rates + namespace aggregates for a given org_id/cluster_id. |
| `6fe59f939` | Include all distributed cost types (platform, worker, storage, network, GPU) in the namespace aggregates query. The initial version only included direct cost model costs, missing the distributed overhead. |
| `faf88a188` | Document cost-onprem deployment pitfalls in AGENTS.md |
| `23c0dc53b` | More AGENTS.md documentation |

### Operator Changes

**Branch:** `pgarciaq-rosocp-superpowers-phase4` (on `pgarciaq` remote)

| Commit | Description |
|---|---|
| `3383cf6c` | Add `oom_count` PromQL query. Query: `sum by(container,namespace,pod) (kube_pod_container_status_last_terminated_reason{reason="OOMKilled"})`. New CSV column: `oom_count`. |
| `603b37b0` | Unit tests for OOM count |
| `1001a815` | Add `workload_pod_count` PromQL query. Query: `count by(container,namespace,workload,workload_type) (kube_pod_container_status_ready{condition="true"})`. New CSV column: `workload_pod_count`. |
| `924de0f4` | Replace DCGM DEV_ metrics with PROF_ metrics. Remove `DCGM_FI_DEV_GPU_UTIL` and `DCGM_FI_DEV_MEM_COPY_UTIL`. Add `DCGM_FI_PROF_SM_ACTIVE`, `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE`, `DCGM_FI_PROF_DRAM_ACTIVE`. |

### Nise Changes

**Branch:** `pgarciaq-rosocp-superpowers-phase4` (on `pgarciaq` remote)

| Commit | Description |
|---|---|
| `c92f444` | Add `oom_count` to ROS CSV generation (random 90/10) |
| `b27d40c` | Unit tests for oom_count |
| `e1c40d9` | Support deterministic `oom_count` from YAML static data |
| `7ccfe9a` | Fix OAuth scope for on-prem Keycloak (omit scope when empty) |
| `19b5024` | Add example YAML for GPU test data generation |
| `d67a6e1` | Add `workload_pod_count` column |
| `1057bbe` | Add 14 GPU profiling metric columns to ROS CSV generation |

**Nise GPU generation details:**
- 14 new columns in `OCP_ROS_USAGE_COLUMN` (gpu_model, gpu_profile_name, and min/max/avg triples for fb_usage, tensor_pipe_active, dram_active, sm_active)
- `_enrich_ros_data_with_gpus()` populates GPU fields based on YAML configuration
- `_gen_ros_gpu_metrics()` generates realistic profiling metric values per GPU model
- Tier 1 GPUs get all profiling metrics; Tier 2 (V100) gets only frame buffer
- `GPU_PROFILING_SUPPORTED` dict maps GPU model names to their tier

**YAML formatting gotcha:** The nise YAML static data uses a specific indentation pattern. `node:`, `pod:`, `gpu:` are dictionary keys whose value is `None` (they act as labels). Their children (`node_name:`, `pod_name:`, `gpu_model:`) must be **sibling keys at the same indentation level**, NOT nested under the parent:

```yaml
# CORRECT:
- node:
  node_name: gpu-node-1
  pod:
  pod_name: training-pod
  gpu:
  gpu_model: A100

# WRONG (nested — produces a dict-of-dicts instead of flat dict):
- node:
    node_name: gpu-node-1
  pod:
    pod_name: training-pod
```

This subtle YAML indentation error caused GPU data to be completely empty during E2E testing. It took significant debugging to identify because nise exits with code 0 and produces output that looks correct at first glance.

---

## 11. Apollo E2E Test Cluster: Everything You Need to Know

### Cluster Specifications

| Property | Value |
|---|---|
| **Type** | SNO (Single Node OpenShift) |
| **Architecture** | `aarch64` (ARM64) — ALL images must use `--platform linux/arm64` |
| **API URL** | `https://api.sno.karmalabs.corp:6443` |
| **Hypervisor** | `hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com` |
| **kubeadmin password** | `/root/.kcli/clusters/sno/auth/kubeadmin-password` on the hypervisor |
| **OpenShift version** | 4.21 (Kubernetes v1.34.6) |
| **Node** | `sno-sno.karmalabs.corp` (192.168.122.55) |

### Network Access

The cluster is on a private network behind the Apollo hypervisor. sshuttle provides routing:

```bash
sshuttle -r root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com 192.168.122.0/24 172.30.0.0/16 10.128.0.0/14
```

### Image Registry and Build

```
default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
```

**Building for aarch64:**
```bash
cd ~/dev/koku/ros-ocp-backend
podman build --platform linux/arm64 -t ros-ocp-backend:gpu-latest .
```

This takes 10+ minutes because `podman` uses QEMU emulation for arm64 on an x86_64 development machine.

**Pushing to cluster registry:**
```bash
# Login to cluster first
oc login -s https://api.sno.karmalabs.corp:6443 -u kubeadmin --password=$(ssh root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com cat /root/.kcli/clusters/sno/auth/kubeadmin-password)

# Login to registry
podman login default-route-openshift-image-registry.apps.sno.karmalabs.corp -u kubeadmin -p $(oc whoami -t)

# Tag and push
podman tag ros-ocp-backend:gpu-latest default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
podman push default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
```

### Keycloak (JWT Authentication)

| Property | Value |
|---|---|
| **Namespace** | `keycloak` |
| **Admin user** | `temp-admin` |
| **Realm** | `kubernetes` |
| **Client** | `cost-management-operator` |
| **JWT org_id** | `org1234567` |

### Current Deployment State on Apollo

- **ROS DB:** Migration version 43 in `costonprem_ros` database (user: `ros_user`)
- **Koku DB:** Django migration `reporting: 0347` in `costonprem_koku` database (user: `koku_user`)
- **KOKU_MASU_URL:** Set to `http://cost-onprem-koku-masu:8000` on the ROS API deployment
- **Cost model:** Has `gpu_cost_per_month: $2500` (infrastructure) configured for the test cluster
- **Test cluster ID:** `d4e5f6a7-b8c9-0123-defa-444444444444` (provider UUID: `d665a309-ccbf-4510-bcdb-59db1f7e0da7`)
- **GPU digest rows:** 12 (3 containers × 4 days)

### E2E Data Generation and Upload Playbook

1. **Generate GPU data with nise:**
```bash
cd ~/dev/koku/nise
.venv/bin/nise report ocp --ros-ocp-info \
  --static-report-file /tmp/gpu_static_data.yml \
  --ocp-cluster-id d4e5f6a7-b8c9-0123-defa-444444444444 \
  --insights-upload /tmp/nise_gpu_output --write-monthly
```

2. **Package the tarball:**
   - Create manifest.json with `start`, `end`, and `date` fields
   - List CSV files in both `files` and `resource_optimization_files` arrays
   - **CRITICAL:** Filenames in manifest must match exactly — no `./` prefix
   - Package with `tar czf payload.tar.gz file1.csv file2.csv manifest.json`

3. **Get JWT token from Keycloak** and upload via curl to ingress endpoint

4. **Verify API:**
```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"7890123","org_id":"org1234567","type":"User","user":{"username":"operator-svc","email":"op@test.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8893/api/cost-management/v1/recommendations/openshift?has_gpu=true"
```

---

## 12. Comprehensive Bug and Fix Registry

### Bugs Found and Fixed in Code

| Bug | Symptom | Root Cause | Fix | Commit |
|---|---|---|---|---|
| org_id prefix mismatch | effective_rates returns empty rates | ROS passes `org1234567`, Koku expects `1234567` | `strings.TrimPrefix(orgID, "org")` | `4f3d96a` |
| ApplyGPUSavings nil for well_utilized | API shows `savings: null` for well-utilized GPUs | `savings > 0` check excluded $0 case | Always set savings when cost data available | `c53f539` |
| GPU data parsed but not persisted | `gpu_container_digests` table always empty | Pipeline only wrote CPU/memory digests | Added `upsertGPUDigests()` in pipeline.go | `50d953c` |
| Import cycle (model↔engine) | Go build fails | gpu_enrichment.go placed in `model` package | Moved to `internal/api/` | `50d953c` |
| ON CONFLICT without unique index | PostgreSQL error on GPU digest upsert | Missing unique constraint | Migration 000043 | `50d953c` |
| DetailResponse type mismatch | Integration test JSON unmarshal fails | Test unmarshaled into wrong type | Updated to `[]model.DetailResponse` | `51c32a5` |
| Distributed costs missing from savings | Savings too low | effective_rates didn't include distributed types | Added all 5 distribution types to SQL | `6fe59f939` |
| cluster_uuid type mismatch | `operator does not exist: uuid = text` | `clusters.cluster_uuid` was UUID but `recommendation_sets.cluster_uuid` was TEXT | Convert both to UUID in migration 000041 | `8f68042` |
| CSV column name mismatch | Parser rejects valid operator CSV | Column names differed between operator/nise/parser | Aligned all column names | `3cfd63d` |
| `log` import conflict in api package | Build error: redeclared | Package-level `var log` conflicted with import alias | Use existing package-level `log` | Part of `50d953c` |
| MIGProfile Slices field name | Build error: `GPUSlices` undefined | Struct field was `Slices`, code used `GPUSlices` | Fixed to `last.Slices` | Part of `df8fb46` |
| Unused variable in handler | Build error | `count` declared but not used after filter | Use `_` discard | Part of `df8fb46` |

### Bugs Found and Fixed During E2E (Not in Code)

| Bug | Symptom | Root Cause | Fix |
|---|---|---|---|
| x86_64 image on aarch64 cluster | `Exec format error` in pod logs | Image built without `--platform linux/arm64` | Rebuild with correct platform |
| Koku listener "Migrations not done" | Pods in CrashLoopBackOff | Koku image expects migrations 0347 but DB has 0344 | Run `manage.py migrate_schemas` from inside pod |
| Koku listener "Received unexpected OCP report" | Data not ingested | cluster_id in CSV doesn't match any registered provider | Register provider with matching cluster_id |
| Koku listener "No ROS reports" | ROS data not processed | Tarball filenames have `./` prefix, manifest doesn't | Repackage tarball without `./` prefix |
| Nise GPU data empty | CSV has empty GPU columns | YAML indentation error (nested vs sibling keys) | Fix YAML indentation |
| Nise hangs during execution | Process appears stuck | Cursor IDE `required_permissions: ["all"]` causes invisible approval prompt | Remove permission requirement from shell commands |
| SSH "Bad owner or permissions" | scp fails | systemd ssh config file has wrong permissions | Use `rsync -F /dev/null` to bypass ssh config |
| Keycloak authentication confusion | Various auth errors | Multiple issues: wrong admin user, wrong realm, wrong client | Found correct values: `temp-admin`, `kubernetes` realm, `cost-management-operator` client |
| Stale oc token | Various oc commands fail | Token expired after hours of debugging | Re-login with `oc login` |
| effective_rates returns empty even after DB update | Savings still nil | Koku masu needs restart to pick up config changes | Restart masu deployment |

---

## 13. Database Schema: Complete State

### ROS Database (`costonprem_ros`, user `ros_user`, migration version: 43)

| Table | Partitioned | Purpose |
|---|---|---|
| `clusters` | No | Cluster registry. `cluster_uuid` is UUID type. |
| `rh_accounts` | No | Organization (org_id) registry. |
| `recommendation_sets` | No | Current recommendations per container × term × engine. `cluster_uuid` is UUID type. |
| `daily_container_digests` | Yes (monthly on bucket_date) | Daily CPU/memory percentiles per container. |
| `gpu_container_digests` | Yes (monthly on interval_start) | Daily GPU metric aggregates per container. Has unique index on (cluster_uuid, namespace, workload, container_name, interval_start). |
| `container_usage_samples` | Yes (monthly) | Raw 15-minute CPU/memory samples for boxplots. |
| `daily_namespace_digests` | Yes (monthly) | Namespace-level percentiles. |
| `namespace_usage_samples` | Yes (monthly) | Namespace-level raw samples. |
| `namespace_recommendation_sets` | No | Namespace-level recommendations. |
| `historical_namespace_recommendation_sets` | Yes (monthly) | Namespace recommendation history. |
| `recommendation_history` | Yes (monthly) | Container recommendation snapshots over time. |
| `recommendation_quality` | Yes (monthly) | Quality metrics (stability, adoption, OOM). |
| `org_recommendation_terms` | No | Per-org custom term configurations. |
| `recommendation_profiles` | No | Seed data: cost (P60/P95) and performance (P98/P100) profiles. |
| `notification_code_definitions` | No | Seed data: 24+ notification codes with descriptions. |
| `workloads` | No | Legacy Kruize-era workload records. |
| `workload_metrics` | No | Legacy Kruize-era metric records. |

### Migration History (000022-000043)

| Migration | Purpose |
|---|---|
| 000022 | Create `daily_container_digests` (partitioned) |
| 000023 | Create `daily_namespace_digests` (partitioned) |
| 000024 | Create `org_recommendation_terms` and `recommendation_profiles` |
| 000025 | Create `notification_code_definitions` with seed data |
| 000026 | ALTER `recommendation_sets` — add native engine columns |
| 000027 | Create `recommendation_quality` and `recommendation_history` |
| 000028 | Add container_id column |
| 000029 | Notification code persistence |
| 000030 | Container usage samples table |
| 000031 | Container usage samples (renumbered) |
| 000032-000036 | Namespace columns, percentiles, boxplot support |
| 000037 | Container memory P60/P98/P99 percentiles |
| 000038 | Variation/limit columns |
| 000039 | Pod count (min/max/avg) columns |
| 000040 | No-cost-data notification code |
| 000041 | cluster_uuid TEXT → UUID (clusters + recommendation_sets) |
| 000042 | Create `gpu_container_digests` (partitioned) |
| 000043 | Unique index on gpu_container_digests |

---

## 14. API Endpoint Inventory

All endpoints under `/api/cost-management/v1/`:

| Endpoint | Methods | Description | Status |
|---|---|---|---|
| `recommendations/openshift` | GET | List container recommendations (paginated, filterable) | ✅ Done |
| `recommendations/openshift/:id` | GET | Detail: single container with all terms, boxplots, GPU | ✅ Done |
| `openshift/namespace/recommendations` | GET | List namespace recommendations | ✅ Done |
| `recommendations/openshift/namespace/:id` | GET | Namespace detail with boxplots | ✅ Done |
| `recommendations/openshift/settings/terms` | GET, PUT, DELETE | Custom timeframe configuration | ✅ Done |
| `recommendations/openshift/history` | GET | Recommendation history (paginated, CSV export) | ✅ Done |
| `recommendations/openshift/quality` | GET | Quality metrics (paginated, CSV export) | ✅ Done |
| `status` | GET | Health check | ✅ (pre-existing) |

**Query parameters for list endpoint:**
- `cluster`, `project`, `workload`, `container` — filtering
- `order_by`, `order_how` — sorting
- `offset`, `limit` — pagination
- `format=csv` — CSV export
- `has_gpu=true|false` — GPU filter (post-enrichment)
- `gpu_model=A100` — GPU model filter
- `gpu_classification=idle` — GPU classification filter

---

## 15. Key Architectural Decisions and Their Rationale

### 1. Integer Arithmetic for Metrics
**Decision:** Store all metric values as int64 (millicores, KiB).
**Why:** Avoids floating-point comparison issues in percentile calculations. `250mc == 250mc` is reliable; `0.25 == 0.25` sometimes isn't in float64.

### 2. Decay-Weighted Percentiles Instead of Absolute Max
**Decision:** Use exponential decay weighting with configurable half-life, targeting P95 instead of P100.
**Why:** P100 (Kruize's approach) means a single spike from 2 weeks ago determines your recommendation today. Decay weighting lets recent data dominate while still considering historical patterns. The ~10-16% lower memory recommendation is covered by OOM feedback.

### 3. pgxpool for New Code, GORM for Legacy
**Decision:** Use raw pgx/pgxpool for all new engine code. Keep GORM only for pre-existing queries.
**Why:** The recommendation engine needs precise control over SQL (CTEs, window functions, percentile_cont, batch inserts). GORM adds overhead and makes complex queries harder to write/debug. pgxpool gives direct access to PostgreSQL's full capabilities.

### 4. In-Memory GPU Filtering Instead of SQL Joins
**Decision:** Filter GPU results in Go after enrichment, not in SQL.
**Why:** GPU data is in a separate table (`gpu_container_digests`) from recommendations (`recommendation_sets`). SQL joins across these would complicate the existing list query, which already has complex pagination. Since GPU enrichment fetches data per-cluster, the in-memory filter on the enriched result set is simpler and equally fast for the expected data volumes (hundreds, not millions, of containers per API call).

### 5. Move gpu_enrichment.go to api/ to Break Import Cycle
**Decision:** Place GPU enrichment in `internal/api/` rather than `internal/model/`.
**Why:** `engine` imports `model` (for GPURec, NativeContainerResult types). If `model` also imported `engine` (for QueryGPURecommendations), Go's import cycle prohibition would block compilation. The `api` package already imports both, so it's the natural home for code that bridges them.

### 6. Separate effective_rates Endpoint in Koku
**Decision:** Create a new Koku masu endpoint rather than calling existing cost-model APIs.
**Why:** The existing `/cost-models/` API is designed for human-facing CRUD, returns large JSON structures with all rates and markup details, and requires RBAC authentication. The engine needs a lightweight, internal-only endpoint that returns just the rate values and namespace aggregates needed for savings calculations. The masu API is internal (no RBAC) and optimized for machine consumption.

### 7. nil vs $0 for Savings Estimates
**Decision:** `nil` means "we don't know" (no cost data), `$0` means "we know there's no savings".
**Why:** The UI needs to display different messages: "Cost data unavailable" vs "$0.00/month savings". This was corrected in commit `c53f539` after E2E testing revealed that `well_utilized` GPUs showed "unknown" savings even when cost data was available.

---

## 16. Known Issues, Gaps, and Future Work

### gpu_distributed Cost Type Gap

**Status:** Unverified. Documented in `docs/known-issues.md`.

The Koku `effective_rates` endpoint filters `OCPUsageLineItemDailySummary` with `data_source = 'Pod'`. GPU distribution SQL (`distribute_unallocated_gpu_cost.sql`) inserts rows with `data_source = 'GPU'`. This means GPU distributed costs may be silently excluded from the savings calculation.

**Practical impact today:** None — GPU-equipped on-prem clusters with cost models are extremely rare. The fix would be to change the SQL filter to `data_source IN ('Pod', 'GPU')`.

### No Idle GPU in Test Data

Current nise GPU data generates `well_utilized` (T4, A100) and `no_profiling` (V100) containers. There's no `idle` GPU container in the test data, which means the idle classification code path is only unit-tested, not E2E-tested.

**Fix:** Add a GPU pod with very low SM_ACTIVE values (<5%) to the nise YAML.

### MIG Recommendations Not Testable E2E

MIG right-sizing requires a container running on a MIG-enabled GPU (A100, A30, H100, H200). Our test cluster and nise data don't produce this scenario.

### UI Not Updated

All new features lack frontend display:
- GPU recommendations (model, classification, profiling metrics, savings)
- Replica count display
- Savings estimate display
- Historical tracking visualization
- Quality metrics dashboard
- Namespace-level recommendations

**Blocked on:** Stefan (UX designer) providing mockups.

### Java/JVM Recommendations

Kruize has these (heap sizing, GC overhead detection). The native engine does not. Would require JVM-aware metrics from the operator and a specialized recommendation model.

### HPA Scaling Suggestions

Notification codes exist (`NotifHPASaturated`, `NotifHPAActive`) but are never set by the engine. Would require HPA status data from the cluster.

### PVC/Storage Rightsizing

Notification code exists (`NotifPVCOrphaned`) but is unused. Would require ingesting storage_usage CSV data into a storage-specific digest table.

### Pull Requests Not Yet Created

The user explicitly deferred PR creation. All work is on personal fork branches.

---

## 17. Development Environment and Testing

### Running Unit Tests

```bash
cd ~/dev/koku/ros-ocp-backend

# GPU-specific tests
go test ./internal/engine/ -run "TestRecommendGPU|TestApplyGPUSavings|TestGpuMonthlyRate|TestMig|TestMatchGPU" -v
go test ./internal/ingestion/ -run "TestHasGPU|TestMinFloat|TestMaxFloat|TestMeanFloat" -v
go test ./internal/api/ -run "TestToGPURecommendation|TestFilterGPUResults|TestMatchesAny" -v

# Savings tests
go test ./internal/engine/ -run "TestApplySavings|TestComputeSavings" -v

# All engine tests
go test ./internal/engine/ -v -timeout 120s

# All tests (includes integration tests with testcontainers — slow)
go test ./... -timeout 300s
```

### Integration Tests (require Docker)

Integration tests use `testcontainers-go` to spin up PostgreSQL 16 in a container, run all migrations, seed test data, and execute queries. They're slow (~6-10 seconds per test) but catch real SQL bugs.

Key integration test files:
- `internal/engine/migration_roundtrip_test.go` — verifies all migrations apply and roll back cleanly
- `internal/api/handlers_integration_test.go` — full HTTP handler tests with real DB
- `internal/engine/savings_integration_test.go` — savings with mock HTTP server for effective_rates

### Building arm64 Image

```bash
cd ~/dev/koku/ros-ocp-backend
podman build --platform linux/arm64 -t ros-ocp-backend:gpu-latest .
```

Takes 10-15 minutes due to QEMU emulation. The `--platform linux/arm64` flag is mandatory for the Apollo aarch64 cluster.

### Nise Tests

```bash
cd ~/dev/koku/nise
.venv/bin/python -m pytest tests/test_ocp_generator.py -v
```

### Koku Tests (for effective_rates)

```bash
cd ~/dev/koku/koku
pipenv run tox -- masu.test.api.test_effective_rates
```

---

## 18. Hard-Won Rules: What NOT to Do

These rules were learned through painful debugging. Violating any of them will cost hours.

1. **Never build x86_64 images for Apollo** — the cluster is aarch64. Always use `--platform linux/arm64`. An x86_64 image will deploy successfully but pods crash with `Exec format error`.

2. **Never pass `org_id` with "org" prefix to Koku APIs** — Koku expects `1234567`, not `org1234567`. The code must strip the prefix: `strings.TrimPrefix(orgID, "org")`.

3. **Never use `./` prefix in tarball filenames** for Koku ingress — the Koku listener's `mytar.getnames()` returns names with `./` prefix, but the manifest.json `files` array has names without it. The mismatch causes "No ROS reports to handle".

4. **Never use `required_permissions: ["all"]` in Cursor agent shell commands** — this triggers an invisible approval dialog in the IDE that blocks execution indefinitely, appearing as a hung process.

5. **Never nest YAML keys under `node:`, `pod:`, `gpu:` in nise static data** — these are label keys with None values. Their children must be siblings at the same indentation level.

6. **Never query `recommendation_sets.cluster_id`** — the column is `cluster_uuid` and it's UUID type (not TEXT). String comparisons will fail with `operator does not exist: uuid = text`.

7. **Never assume GPU savings `nil` means $0** — nil = unknown (no cost data), $0 = confirmed no savings. The UI should display different messages for each.

8. **Never skip `redis-cli FLUSHALL`** after Koku code changes — the Django `cache_page` middleware caches API responses for 1 hour. Stale cache is the #1 cause of "I fixed it but it still returns old data".

9. **Never forget to restart Koku masu after config changes** — the masu service caches configuration at startup.

10. **Never register an OCP provider with the wrong cluster_id** — the Koku listener matches incoming reports by cluster_id against registered providers. A mismatch results in "Received unexpected OCP report".

11. **Never forget migration numbering** — check the highest existing migration number before adding new ones. Current highest: 000043.

12. **Never place bridge code (that imports both engine and model) in either package** — put it in `api/` to avoid import cycles.

---

## 19. Plan Documents and Reference Files

| Document | Path | Content |
|---|---|---|
| Phase 0 plan | `docs/plans/phase-0-critical-fixes.md` | 12 bug fixes, TDD approach |
| Phase 1-2-3 plan | `docs/plans/phase-1-2-3-go-engine.md` | Core engine architecture, algorithm details, Kruize comparison |
| Phase 4 plan | `docs/plans/phase-4-oom-feedback.md` | OOM bump formula, quality tracking, cross-repo merge order |
| Phase 5 plan | `docs/plans/phase-5-history-and-boxplots.md` | History snapshots, raw samples, boxplot assembly, retention |
| Replica count + savings | `docs/plans/replica-count-and-cost-impact.md` | Operator PromQL, cost calculation formula, rate selection |
| GPU recommendations | `docs/plans/gpu-recommendations.md` | DCGM metrics, competitive analysis, classification algorithm, MIG profiles |
| GPU test plan | `docs/plans/gpu-recommendations-test-plan.md` | Test matrix, E2E playbook, nise configuration |
| Known issues | `docs/known-issues.md` | Missing UI, engine gaps, gpu_distributed concern |
| Performance analysis | `docs/native-engine-performance.md` | Benchmarks, scale concerns |
| Kruize comparison | `docs/kruize-vs-native-comparison.md` | Quantitative memory recommendation differences |
| Namespace boxplots | `docs/phase6-namespace-boxplots-implementation.md` | Implementation notes |
| Phase 4 PR checklist | `docs/plans/phase-4-pr-checklist.md` | Cross-repo merge order and dependencies |

---

## 20. Key File Index

### Engine (`internal/engine/`)

| File | Lines | Purpose |
|---|---|---|
| `recommend_all.go` | ~300 | Main orchestrator: read digests → compute terms → write results |
| `recommend_cpu.go` | ~150 | Decay-weighted CPU percentile, 25mc floor, dual cost/perf |
| `recommend_memory.go` | ~200 | Adaptive margin, P95 base, separate request/limit, OOM bump |
| `recommend_namespace.go` | ~200 | Aggregate container recs to namespace level |
| `gpu_recommender.go` | ~320 | GPU classification, MIG right-sizing, ApplyGPUSavings |
| `gpu_metadata.go` | ~250 | GPU model specs, MIG profiles, MatchGPUModel |
| `gpu_query.go` | ~100 | Query gpu_container_digests, produce GPURec map |
| `savings.go` | ~200 | CPU/memory savings from Koku effective_rates |
| `quality.go` | ~150 | 4 quality metrics: stability, adoption, OOM, age |
| `history.go` | ~100 | Recommendation history snapshot writer |
| `retention.go` | ~100 | Background partition retention sweep |
| `detect_idle.go` | ~50 | CPU < 10mc idle detection |
| `trend.go` | ~80 | Linear regression slope for trend |
| `notifications.go` | ~200 | 24+ notification code evaluation |
| `term_config.go` | ~100 | Per-org term configuration loading |
| `decay.go` | ~80 | Exponential decay weight function |
| `percentile.go` | ~60 | Decay-weighted percentile computation |

### Ingestion (`internal/ingestion/`)

| File | Lines | Purpose |
|---|---|---|
| `pipeline.go` | ~350 | CSV → digests + GPU digests + samples (main entry) |
| `csvparser.go` | ~200 | CSV parsing, float→int64 conversion, validation |
| `digest.go` | ~150 | Daily digest computation from grouped rows |
| `namespace.go` | ~100 | Namespace digest processing |
| `models.go` | ~150 | MetricRow, DigestRow, DigestKey structs |

### API (`internal/api/`)

| File | Lines | Purpose |
|---|---|---|
| `handlers.go` | ~400 | Main recommendation handlers (list, detail, fallback) |
| `handlers_history.go` | ~150 | History API handler |
| `handlers_quality.go` | ~150 | Quality API handler |
| `handlers_terms.go` | ~150 | Settings/terms CRUD handler |
| `gpu_enrichment.go` | ~160 | GPU enrichment, cost fetch, savings, filtering |
| `server.go` | ~100 | Echo server setup, route registration |

### Model (`internal/model/`)

| File | Lines | Purpose |
|---|---|---|
| `recommendation_set_native.go` | ~300 | Native SQL queries for container recommendations |
| `detail_response.go` | ~200 | Kruize-compatible strongly-typed response struct |
| `boxplot.go` | ~150 | Boxplot assembly via percentile_cont() |
| `types.go` | ~100 | GPURecommendation, NativeContainerResult structs |

### Cost Data (`internal/costdata/`)

| File | Lines | Purpose |
|---|---|---|
| `provider.go` | ~110 | CostDataProvider interface, HTTPCostDataProvider, NilCostDataProvider |

### Test Utilities (`internal/testutil/`)

| File | Lines | Purpose |
|---|---|---|
| `testdb.go` | ~100 | testcontainers-go PostgreSQL setup |
| `fixtures.go` | ~200 | Deterministic test data, SeedContainerDigest, SeedGPUDigest |

### Migrations (`migrations/`)

22 migration pairs (up/down) from 000022 through 000043.
