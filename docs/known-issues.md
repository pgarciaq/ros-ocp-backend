# Feature Status — Native Recommendation Engine

This document tracks the implementation status of all features in the
ros-ocp-backend native engine, their API availability, UI support in
koku-ui, and known issues. **Code-verified** against the actual Go source —
not aspirational.

Last updated: 2026-05-21 (per-plugin configurable terms, documentation site, Makefile targets)

---

## Executive Summary (for customer discussions)

### What We Have Today

The native Go recommendation engine is **production-ready** on plain
PostgreSQL 16 (no TimescaleDB or special extensions required).

| Category | Feature | Status |
|----------|---------|--------|
| **Container recs** | CPU recommendations (percentile-based, adaptive margin) | **Shipping** |
| **Container recs** | Memory recommendations (percentile-based, OOM-aware) | **Shipping** |
| **Container recs** | Data decay weighting (configurable half-life per term) | **Shipping** |
| **Container recs** | OOM detection & feedback (logarithmic memory bump) | **Shipping** |
| **Container recs** | Custom timeframes (1–90 day windows, 3 terms per plugin per org) | **Shipping** |
| **Container recs** | Idle / abandoned workload detection | **Shipping** |
| **Container recs** | CPU trend analysis (least-squares slope) | **Shipping** |
| **Container recs** | Dollar savings estimates (via Koku cost data) | **Shipping** |
| **Container recs** | Replica count display (min/max/avg pod count) | **Shipping** |
| **Container recs** | Recommendation history tracking | **Shipping** |
| **Container recs** | Recommendation quality (stability %, adoption detection) | **Shipping** |
| **Container recs** | Box plots (five-number summary from usage samples) | **Shipping** |
| **Namespace recs** | Namespace-level CPU + memory recommendations | **Shipping (enabled by default)** |
| **GPU** | GPU workload classification (idle/underutilized/memory-bound/compute-bound) | **Shipping** |
| **GPU** | MIG profile selection (A100/A30/H100/H200/B100/B200) | **Shipping** |
| **GPU** | Node-level time-slicing guidance (nvidia.com/gpu.replicas) | **Shipping** |
| **GPU** | GPU savings estimates (from Koku cost model rates) | **Shipping** |
| **Storage** | PVC right-sizing (oversized/near-full/orphaned/healthy + growth trend) | **Shipping** |
| **Snapshots** | Snapshot staleness detection (orphaned/stale/never-restored/redundant) | **Shipping** |
| **Node recs** | Node CPU/memory right-sizing (Tier 1: underutilized, overcommitted, EMA-smoothed stranded detection) | **Shipping (enabled by default)** |
| **Fleet** | Fleet summary (cross-cluster aggregate) | **Shipping** |
| **Platform** | RBAC (Insights RBAC middleware with cluster-level filtering) | **Shipping** |
| **Platform** | Notification system (~35 codes: confidence, OOM, idle, stale, GPU, PVC, snapshot) | **Shipping** |
| **Platform** | Per-plugin configurable recommendation terms (TermProvider trait) | **Shipping** |
| **Platform** | Admin env-var locking of term settings (per-term, per-plugin) | **Shipping** |
| **Platform** | Plugin capabilities endpoint (`GET /settings/capabilities`) | **Shipping** |
| **Platform** | Plugin registry with MaxWindowDays validation per domain | **Shipping** |
| **Platform** | Structured logging (logrus + WithFields, org_id/request_id context) | **Shipping** |
| **Platform** | Prometheus per-phase histograms and error counters | **Shipping** |
| **Platform** | .env file support (godotenv) for local development | **Shipping** |
| **Developer** | MkDocs documentation site with auto-generated plugin API reference | **Shipping** |
| **Developer** | Comprehensive CONTRIBUTING.md with architecture, setup, plugin guide | **Shipping** |

### Implementation Statistics (from requirements.md audit)

| Category | Total REQs | Implemented | Partial | Not Impl | Deferred |
|----------|-----------|-------------|---------|----------|----------|
| Phase 0 (Bug fixes) | 12 | 11 | 0 | 1 | 0 |
| Phase 1 (Engine) | 12 | 11 | 0 | 1 | 0 |
| Phase 2 (Pipeline) | 7 | 6 | 1 | 0 | 0 |
| Phase 3 (Computation) | 5 | 3 | 1 | 1 | 0 |
| Phase 4 (OOM) | 5 | 5 | 0 | 0 | 0 |
| Phase 5 (GPU) | 7 | 6 | 0 | 0 | 1 |
| Phase 6 (Quality) | 5 | 4 | 0 | 1 | 0 |
| Phase 7 (Savings) | 6 | 6 | 0 | 0 | 0 |
| Phase 8 (Advanced) | 4 | 0 | 0 | 4 | 0 |
| Phase 8b (VM) | 9 | 0 | 0 | 9 | 0 |
| Phase 8c (Node) | 11 | 7 | 2 | 2 | 0 |
| Phase 9 (JVM) | 5 | 0 | 0 | 5 | 0 |
| Phase 10 (Legacy Kruize path) | 8 | 3 | 2 | 3 | 0 |
| **TOTAL** | **96** | **62** | **6** | **27** | **1** |

### What's Next (Not Yet Implemented)

| Feature | REQs | Description | Complexity | Priority |
|---------|------|-------------|------------|----------|
| Retire legacy Kruize stack (native-only mandate) | REQ-10.1, REQ-10.2, REQ-10.3, REQ-10.4, REQ-10.5 | Remove optional legacy Kruize code path once product commits to native-only and tenants are migrated | Low | **High** — simplifies deployment after cutover |
| MachineSet right-sizing (Tier 2) | REQ-8c.4, REQ-8c.5, REQ-8c.6 | Instance type + replica count recommendations via cloud catalog | High | Medium |
| VM recommendations | REQ-8b.1 – REQ-8b.9 | Virtual machine right-sizing for OpenShift Virtualization | Medium | Medium |
| On-demand real-time recs | REQ-3.4 | API-time recommendation for custom timeframe requests | Low | Low |
| Poison message DLQ | REQ-0.7 | Dead-letter topic for Kafka messages that fail after max retries | Low | Low |
| Shadow mode | REQ-1.12 | Production dual-engine comparison (offline CLI tool exists) | Low | Low |
| Keyset pagination | — | Cursor-based pagination for large orgs (see below) | Low | Low |
| ~~Replica count from operator~~ | ~~REQ-7.1~~ | **DONE** — Operator now emits `desired_replicas` and `available_replicas`; backend stores and exposes via API. | — | — |

### Not Planned for Current MVP

These features are documented in `requirements.md` but are explicitly
**not planned** for the current MVP release. They require new operator
Prometheus queries, external runtime detection, or upstream fixes.

| Feature | REQs | Reason |
|---------|------|--------|
| HPA optimization | REQ-8.1 | Needs 8 new operator queries; low customer demand |
| Ephemeral storage | REQ-8.2 | cadvisor metrics unreliable through OCP 4.21; pending upstream fix |
| Node.js heap advisory | REQ-8.3 | Weakest rec type; needs new operator query; no actionable numeric value |
| ResourceQuota recs | REQ-8.4 | Needs 2 new operator queries; namespace recs partially address this |
| Go GOMAXPROCS/GOMEMLIMIT | REQ-6.4 | Needs new operator query (`go_info`); niche audience |
| JVM runtime detection | REQ-9.1 – REQ-9.5 | Needs optional operator queries + JVM-specific metrics; medium effort |
| Cloud instance catalog | REQ-8c.6 | External API integration (AWS/Azure/GCP pricing); required for MachineSet Tier 2 |
| MachineAutoscaler (Tier 3) | REQ-8c.7 | Cloud-only; depends on Tier 2 MachineSet implementation |
| Multi-GPU awareness | REQ-5.5 | Needs per-device utilization from operator; niche ML workloads |
| Confidence bounds | ~~REQ-1.4~~ | Statistical methodology not designed; cost/performance dual-model provides range |
| QoS class recommendations | ~~REQ-6.2~~ | Implicit from request/limit values; revisit if user research demands |
| Engine versioning | REQ-3.5 (full) | Unit tests exist; formal semantic versioning scheme deferred |

### Known Caveats

| Issue | Impact | Severity | REQs |
|-------|--------|----------|------|
| Namespace recs can be disabled per-org | Cloud: Unleash `rosocp.namespace_disabled` kill switch. On-prem: always on. | By design — kill switch for cloud rollback | REQ-1.13 |
| Node recommendation cold start (3 days) | New clusters return empty results from **`GET /recommendations/openshift/nodes`** (node utilization) until 3 days of data accumulates | Low — by design for accuracy | REQ-8c.3 |
| Legacy Kruize code still present | `internal/utils/kruize/`, `internal/services/recommendation_poller.go` still contain Kruize client code. Native engine runs alongside, not instead of. | Low — no runtime impact when native engine is active | REQ-10.1 – REQ-10.5 |
| `workload_metrics` JSONB table not removed | Legacy table and model (`model/workload_metrics.go`) still exist. New engine bypasses it entirely but it is not dropped. | Low — no storage growth when native engine handles ingestion | REQ-2.4 |
| Replica count fallback for old operators | Operators that predate the `desired_replicas` CSV column will still use derived pod count. API marks these with `"source": "derived"`. Newer operators provide authoritative `"source": "kube_state_metrics"` data. | Low — only affects old operator versions | REQ-7.1 |
| Replica count missing for crash-looping workloads | If all pods in a workload crash before being scraped (within the 15m `max_over_time` window), the operator cannot broadcast `desired_replicas` to per-pod CSV rows. Falls back to derived pod count. See [Replica Count and Short-Lived Pods](#replica-count-and-short-lived-pods) below. | Very Low — only affects workloads where every pod dies within seconds | REQ-7.1 |
| No UI for most new features | Node recs, PVC recs, snapshots, GPU recs, fleet summary, quality, history, settings all have APIs but no koku-ui views | Medium — features are API-only until UI catches up | Multiple |
| Unparsable Kafka messages log full payload | Fix for **`docs/audits/490-issues.md` #149** (`commitOnPermanentFailure` in `internal/services/report_processor.go`): when a message cannot be parsed or validated, the **entire Kafka message body is written to application logs** to support manual recovery and debugging. Those payloads routinely include **`org_id`**, **`cluster_uuid`**, and **file URLs**. Presigned S3 URLs in particular may carry **access tokens or signing parameters in the query string**, which some compliance regimes treat as sensitive even when logs are access-controlled. | Medium — policy-dependent (data classification, log retention, SIEM exposure) | **`docs/audits/490-issues.md` #149** |

#### Unparsable Kafka message logging (sensitive payload fields)

When ingestion encounters JSON/validation failures on the ROS report Kafka path,
the handler logs the **full raw payload** alongside the error. This was added so
operators can reconstruct or forward poison messages after failures that would
otherwise commit the offset with no replay path (see **`docs/audits/490-issues.md` #149** —
`commitOnPermanentFailure` / permanent failure handling).

**Fields of note in logged payloads:**

- **`org_id`** — tenant identifier
- **`cluster_uuid`** — cluster identifier
- **File URLs** — object storage locations; **presigned URLs** may embed
  credentials or time-limited signing material in query parameters

**What the fix does:** Prior to the fix, unparsable messages were silently
committed (offset advanced, payload lost forever — actual data loss). The fix
logs the full payload before committing, giving SRE/Support a path to manually
recover or replay the failed message. The trade-off is that sensitive fields
(presigned URLs with signing parameters) end up in application logs.

**Open question — discuss with SRE:** Whether to redact presigned URL query
strings before logging. Redacting improves compliance posture but removes
information that SRE/Support may need to manually download and re-ingest the
failed report. The decision depends on the deployment's log access controls,
retention policies, and whether SRE prefers full URLs for incident response
or would rather use a correlation ID to look up the URL separately.

**Hardening options** (if redaction is desired):

- **Strip query strings** from presigned URLs before logging (log only
  `s3://bucket/key` path).
- **Route failures to a dead-letter topic** with stricter access controls,
  keeping only correlation IDs in general application logs.

This caveat aligns with the broader **poison message / DLQ** gap tracked as
REQ-0.7 in the executive summary; **`docs/audits/490-issues.md` #149** documents the
correctness and observability trade-offs for the current path.

#### Node Recommendation Cold Start

Node recommendations require at least 3 days of ingested container usage data
before any results appear. This is intentional: fewer data points produce
unreliable utilization percentiles and trend slopes.

**UI requirement:** `koku-ui` needs a null/empty state for the node
recommendations view explaining: "Collecting data — recommendations will appear
after 3 days of usage data." This avoids user confusion when the endpoint
returns an empty `data` array during the warm-up period.

#### Replica Count and Short-Lived Pods

The operator's `ros:desired_replicas` and `ros:available_replicas` PromQL queries
work by:

1. Computing the workload-level replica count (e.g., `max by(namespace, workload)
   (kube_deployment_spec_replicas)`)
2. Broadcasting that value to per-pod CSV rows via a join on
   `kube_pod_container_info` with a `max_over_time(...[15m])` window

This means a pod must appear in at least one Prometheus scrape within the most
recent 15-minute window to receive the broadcast. If a pod dies before being
scraped, its CSV rows will have `desired_replicas = 0`.

**Why this is not a practical concern:**

- The replica count is workload-level. If *any* pod in the workload matches
  (which happens for any workload surviving > 1 scrape interval), the value
  propagates correctly during digest computation (`computeReplicaCounts` takes
  the max across all rows in an hour).
- Short-lived pods that miss the join just contribute `0`; sibling pods provide
  the correct non-zero value.
- The only failure mode is a workload where *every* pod crash-loops before being
  scraped. Such workloads have operational problems far more pressing than
  missing replica metadata, and the fallback (`"source": "derived"`) still
  provides an approximation.

**Mitigation if needed in future:** Increase `max_over_time` from `15m` to `1h`
in the operator PromQL queries. This is a one-line change but trades off
freshness after scale-down events. The 15-minute window is consistent with the
existing `workload-pod-count` query.

### Recently Implemented (Phase 8 — May 2026)

| Feature | Description | Branch |
|---------|-------------|--------|
| Per-plugin configurable terms | `TermProvider` trait, `DefaultTerms()`, `MaxWindowDays()` per plugin | `pgarciaq-rosocp-superpowers-phase8` |
| Admin env-var locking | `ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>` overrides, makes terms read-only | same |
| Capabilities endpoint | `GET /settings/capabilities` lists plugins + traits + lock status | same |
| PVC per-term output | PVC recommendations now include short/medium/long term results | same |
| Cache invalidation on PUT/DELETE | `InvalidateTermCache()` called after term settings changes | same |
| Validation (422 responses) | `window_days > MaxWindowDays` returns proper error | same |
| E2E integration tests | Term precedence + effects tested end-to-end | same |
| MkDocs developer docs site | Auto-generated plugin API reference + narrative docs | same |
| Root Makefile targets | `docker-build`, `docs-*`, `help` targets | same |
| Structured logging | `internal/logging` package, org_id/request_id context everywhere | earlier phase |
| Prometheus per-phase metrics | Histograms and error counters for observability | earlier phase |
| .env file support | `godotenv` auto-loading for local development | earlier phase |
| Plugin architecture docs | Comprehensive doc with trait matrix, term defaults, precedence | same |

### Recently Fixed Caveats

| Issue | Fix | Commit |
|-------|-----|--------|
| Performance vs cost profiles stored identical values | `recommend_all.go` / `recommend_namespace.go` now select `PerfRequest*`/`PerfLimit*` when `profile == "performance"` | Phase 7 |
| Memory trend notification used CPU slope at container level | Added separate `CPUTrendSlope` and `MemTrendSlope` to `ContainerRec` | Phase 7 |
| Notification code 29 collision (PVC_OVERSIZED vs GPUTimeSharingCandidate) | `NotifGPUTimeSharingCandidate` reassigned to code 36 | Phase 7 |
| PromQL `group_left` bug in replica count queries | Swapped operands in `ros:desired_replicas` / `ros:available_replicas` | `koku-metrics-operator` |
| GPU filters applied in-memory causing pagination errors | Push `has_gpu`, `gpu_model`, `gpu_classification` to SQL | `bd25f04` |
| Kafka auto-commit causing message loss | `KAFKA_AUTO_COMMIT` default flipped to `false` | `deddf88` |
| `workload_type` missing from PKs causing upsert conflicts | Added to all `ON CONFLICT` clauses | `7d0fcf8` |
| Dead code and naming issues (#383-#400) | Removed unused exports, fixed naming conventions | `9f13890` |
| Test anti-patterns (#421-#445) | Added `t.Cleanup`, fixed global state, timing assertions | `f80ad08` |

---

## Features Implemented in Engine, Missing UI

### Custom Timeframes (Settings API)

**Engine status:** Fully implemented with per-plugin term configuration:

- Each plugin declares `DefaultTerms()` and `MaxWindowDays()` via the `TermProvider` trait.
- Terms are configurable per recommendation type: container (max 90d), namespace (90d), gpu (90d), node (90d), pvc (365d).
- Exponential decay (`decay_halflife_hours`) is supported per term.
- `min_data_days` is auto-computed as `ceil(window_days / 2)`, clamped to `[1, window_days]`.

**Term resolution precedence** (per term, per plugin):

1. Admin env var (`ROS_TERMS_<PLUGIN>_<TERM>_WINDOW_DAYS`, etc.) — always wins, makes term "locked"
2. Tenant DB override (via `PUT /settings/terms?recommendation_type=<plugin>`) — applied unless locked
3. Plugin default (`DefaultTerms()`) — used when no override exists

**API status:**

- `GET/PUT/DELETE /settings/terms?recommendation_type=<plugin>` — per-plugin term management
- `GET /settings/capabilities` — lists plugins, their traits, and whether terms are configurable/locked
- Validation: `window_days` capped at `MaxWindowDays()`, 422 returned for out-of-range values

**UI status:** Not implemented. No settings page for term configuration in koku-ui.

### Namespace Recommendations

**Engine status:** Fully implemented and **enabled by default**. No env var
or feature flag needed for on-prem. `RecommendAllNamespaces()` produces
namespace-level recommendations from `daily_namespace_digests`.

**Feature gating:** On cloud (console.redhat.com), namespace recs can be
disabled per-org via the Unleash kill switch `rosocp.namespace_disabled`.
On-prem has no Unleash, so namespace recs are unconditionally enabled.

**API status:** Fully implemented. `GET /openshift/namespace/recommendations`
and `GET /recommendations/openshift/namespace/:recommendation-id` endpoints
serve namespace recommendations with boxplots and CSV export.

**UI status:** Partial. The koku-ui `optimizationsProjectsTable` component
fetches namespace recommendations, but the breakdown detail view is
container-focused. Namespace-level visualization (aggregated cost/performance
trade-offs across a project) is not exposed in the UI.

### Decay Weighting

**Engine status:** Fully implemented. Exponential decay with configurable
half-life per term. `decay.go` implements `DecayWeight()` and
`WeightedPercentile()`. Default half-lives: short=0h (no decay),
medium=168h (7 days), long=360h (15 days).

**UI status:** Not exposed. The UI does not show decay parameters or allow
users to understand how recent vs older data is weighted.

### Historical Tracking

**Engine status:** Fully implemented. Recommendation history is stored in
`recommendation_history` and `historical_namespace_recommendation_sets`
(partitioned tables). Quality metrics (`recommendation_quality`) track
stability, adoption, and OOM events post-recommendation.

**API status:** Fully implemented.
`GET /api/cost-management/v1/recommendations/openshift/history` returns
paginated recommendation snapshots with filtering by date range, cluster,
project, workload, container, term, and engine. Supports JSON and CSV
export.

**UI status:** Not implemented. No timeline or trend visualization of how
recommendations have changed.

### Stability / Quality Metrics

**Engine status:** Fully implemented. `quality.go` computes
`stability_pct`, `adoption_detected`, `oom_events_after_rec`, and
`recommendation_age_hours`.

**API status:** Fully implemented.
`GET /api/cost-management/v1/recommendations/openshift/quality` returns
paginated quality metrics with filtering by date range, cluster, project,
workload, and container. Supports JSON and CSV export.

**UI status:** Not implemented.

### Idle Workload Detection

**Engine status:** Fully implemented. Workloads with CPU usage max below
10 millicores are flagged as idle. `NotifIdleWorkload` notification code
is emitted.

**UI status:** Partial. The notification code is included in the API
response and the UI renders notification badges, but there is no dedicated
"idle workloads" view or filter.

### Recommendation Categories

**Engine status:** Not implemented as explicit fields. Direction (increase /
decrease / well-sized) can be inferred from `variation_*_pct` sign, but
there is no first-class `category` enum in the API response.

**UI status:** Not implemented. The UI shows variation percentages but does
not label recommendations as "increase" / "decrease" / "well-sized".

---

## Features Implemented in Engine

### Node CPU/Memory Right-Sizing (Tier 1)

**Engine status:** Fully implemented and **enabled by default**. `RecommendNodes()`
evaluates daily node digests and produces per-node recommendations.

**Detection signals:**

- **Underutilized:** Both CPU and memory p95 below threshold (default 30%)
- **Overcommitted:** CPU request/allocatable ratio exceeds threshold (default 150%)
- **Stranded resources:** EMA-smoothed normalized imbalance detection — per-day
  `|cpu_p95 - mem_p95| / max(cpu_p95, mem_p95)` is smoothed with EMA (alpha = 0.3)
  and flagged when the final value exceeds the threshold (default 0.6). This is
  relative (not absolute), works across low/high utilization, and dampens transient
  spikes from batch jobs.
- **Trend slope:** Linear regression on EMA-smoothed daily CPU utilization

**Data pipeline:** Operator emits `node_capacity_cpu_cores` and
`node_capacity_memory_bytes` in ROS CSVs → parser → `daily_node_digests` →
engine → `node_recommendations` table. Falls back to request-based estimates
when capacity data is unavailable.

**Configuration (env vars):**

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.30 | p95 below this = underutilized |
| `ROS_NODE_OVERCOMMIT_THRESHOLD` | 1.50 | Request/allocatable ratio above this = overcommitted |
| `ROS_NODE_ALLOCATABLE_FACTOR` | 0.93 | Fraction of capacity considered allocatable |
| `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | 0.6 | EMA-smoothed imbalance above this = stranded |
| `ROS_NODE_EMA_ALPHA` | 0.3 | EMA smoothing alpha (higher = less smoothing) |

**API status:** Canonical **`GET /recommendations/openshift/nodes`** returns
per-node utilization, overcommit ratios, stranded resource flags, and trend slopes.
Deprecated alias: **`GET /recommendations/openshift/nodes/utilization`** (same payload;
responses include a deprecation warning).

**Notification codes:** 11 (underutilized), 12 (overcommitted), 13 (stranded resources).

**UI status:** Not implemented. Requires a node recommendations view and a null
state for the 3-day cold start period.

### GPU Recommendations

**Engine status:** Implemented. The engine classifies GPU workloads using
DCGM profiling metrics (`PROF_PIPE_TENSOR_ACTIVE`, `PROF_DRAM_ACTIVE`,
`PROF_SM_ACTIVE`) into categories: idle, underutilized, memory-bound,
compute-bound underutilized, well-utilized, and no-profiling (for GPUs
without DCGM metrics, e.g. Tier 2 V100). Supports MIG profile
recommendations for A100/A30/H100/H200/B100/B200. Confidence scoring
accounts for observation duration and workload variability. Two-tier
GPU support: Turing+ (full profiling) and Volta/Pascal (frame-buffer only).

**Data generation:** Nise generates GPU profiling metrics in ROS CSVs via
`--ros-ocp-info`. Tier 1 GPUs (T4, A10, A30, A100, H100, L40S) get all
profiling metrics; Tier 2 (V100) gets only frame buffer.

**API status:** `GPURecommendation` block included in detail response with
`current_gpu_model`, `gpu_classification`, `recommended_gpu_profile`,
`gpu_confidence`, profiling metric averages, and savings estimate.
GPU-specific notification codes: 10 (underutilized), 26 (idle),
27 (memory-bound), 28 (no profiling data).

**Implemented but not listed above:**

- GPU savings estimation from Koku cost data (`ApplyGPUSavings` in
  `gpu_recommender.go`): reads `configured_rates["gpu_cost_per_month"]` from
  the Koku `effective_rates` endpoint. Idle GPU = full monthly rate; MIG
  right-sizing = fractional savings based on slice ratio. Wired into
  `enrichWithGPU` and mapped to `estimated_monthly_gpu_savings_usd` in API.
- Node-level time-slicing savings via `ComputeNodeTimeslicingRec` with
  per-GPU and total-node dollar estimates on **`GET /recommendations/openshift/gpu/timeslicing`**
  (canonical path for GPU time-slicing; previously `/recommendations/openshift/nodes`).
- Container-level time-slicing cross-reference (`time_slicing_node`,
  `time_slicing_replicas`) on container GPU blocks.
- API query filters (`has_gpu`, `gpu_model`, `gpu_classification`) — parsed in
  `parseGPUFilters` (`handlers.go`), applied by `filterGPUResults`
  (`gpu_enrichment.go`). Documented in `openapi.json`.
- GPU daily digest aggregation pipeline — `upsertGPUDigests` in `pipeline.go`
  aggregates hourly CSV rows into daily `gpu_container_digests` rows during
  ingestion. Partition creation, upsert-on-conflict, and retention sweep all
  operational.

- Container-level time-slicing dollar savings — `EstimatedTimeslicingSavingsUSD`
  on `GPURec`, populated by `ComputeNodeTimeslicingRec` with the per-candidate
  share of `SavingsPerGPU`. Exposed as `estimated_monthly_timeslicing_savings_usd`
  in the container API response.

**Not yet implemented in UI:**

- Koku-UI display of GPU recommendations (classifications, savings, time-slicing)

**Known limitations (accepted risk):**

- **Retention vs ingestion race**: `RunRetentionSweep` could DROP a partition
  while `upsertGPUDigests` writes to it (e.g., during backfill of old data).
  PostgreSQL locking makes this fail loud (write error) rather than corrupt.
  Normal operation (recent data + 6-month retention) is unaffected.

See `docs/plans/gpu-recommendations.md` for detailed design and
`docs/plans/gpu-recommendations-test-plan.md` for E2E testing guide.

## GPU: MIG + Time-Slicing Combined Recommendations Not Supported

The current GPU recommendation engine treats MIG partitioning and time-slicing as
mutually exclusive strategies. In `partitionContainers`, workloads with a MIG
recommendation (non-`full_gpu` profile) are excluded from time-slicing candidates.

In practice, NVIDIA GPUs support combining both: a physical GPU can be partitioned
into MIG instances, and each MIG instance can then be time-sliced among multiple
pods. This combined approach could further improve GPU utilization.

**Current behavior:** If container A is recommended for a 3g.20gb MIG slice, it will
NOT appear as a time-slicing candidate, even if multiple containers could share that
MIG instance.

**Future enhancement:** Model time-slicing within MIG partitions. This would require
the engine to reason about MIG instance scheduling — which instances exist, their
sizes, and which pods could share them. Tracked as a deferred enhancement.

### Replica Count Display

**Engine status:** Fully implemented. `pod_count_min`, `pod_count_max`,
`pod_count_avg` are computed from operator-reported `workload_pod_count`
(primary) or distinct pod name counting (fallback). Additionally,
`desired_replicas` and `available_replicas` are collected from
authoritative kube-state-metrics via the operator (REQ-7.1). Persisted
in `daily_container_digests` and `recommendation_sets`.

**API status:** Fully implemented. `GET /recommendations/openshift/:id`
returns `recommendations.replicas` with `min`, `max`, `avg`, `desired`,
`available`, and `source` fields. `source` is `"kube_state_metrics"`
when authoritative data is available, or `"derived"` for pod-count
fallback. CSV export includes pod count columns.

**UI status:** Not implemented. The koku-ui does not display replica count
information in the recommendation detail view.

### Total Cost Impact / Savings Estimate

**Engine status:** Fully implemented. `ApplySavingsEstimates()` in
`internal/engine/savings.go` computes `EstimatedSavingsUSD` for each
container recommendation using cost data fetched from a Koku masu
internal endpoint (`GET /effective-rates/`). Savings include cost model
rates (CPU + memory), infrastructure costs (raw + markup), and
distributed overhead (platform, worker, storage, network, GPU),
apportioned by the cost model's distribution type (cpu or memory) and
scaled by replica count.

**API status:** Fully implemented. `estimated_monthly_savings_usd` is
returned in the recommendation detail response. When no cost data is
available, a `NotifNoCostData` notification is included.

**UI status:** Not implemented. The koku-ui does not display the estimated
savings value in the recommendation detail view.

### `gpu_distributed` Gap in Effective-Rates SQL — FIXED

The Koku masu `effective_rates` endpoint SQL filter was changed from
`data_source = 'Pod'` to `data_source IN ('Pod', 'GPU')` to include GPU
distribution rows in savings calculations. Tests added in
`masu/test/api/test_effective_rates.py`.

---

## Features Not Yet Implemented in Engine

### Java / JVM Recommendations (REQ-9.1 – REQ-9.5)

No workload-specific tuning (heap sizing, GC overhead detection). Would
require JVM-aware metrics from the operator and a specialized recommendation
model. **Not planned for current MVP.**

### HPA Scaling Suggestions (REQ-8.1)

No horizontal scaling suggestions. `NotifHPASaturated` and `NotifHPAActive`
notification codes exist but are never set by the native engine. Would
require HPA status data from the cluster. (Note: replica count *display*
is implemented — see above — but the engine does not suggest scaling replica
count up or down.) **Not planned for current MVP.**

### VM Recommendations (REQ-8b.1 – REQ-8b.9)

No virtual machine right-sizing. Notification codes 18-19 (VM-related) exist
but no engine logic, no ingestion, no API endpoints. Requires 12 new operator
Prometheus queries and a dedicated daily digest pipeline. **Planned for future
release — depends on OpenShift Virtualization adoption.**

### MachineSet Right-Sizing (REQ-8c.4, REQ-8c.5)

Node Tier 1 (utilization visibility) is implemented. MachineSet Tier 2
(right-sizing with cloud catalog) and Tier 3 (MachineAutoscaler) are not.
Requires MachineSet queries in the operator, cloud instance catalog
integration, and new API endpoints. **Planned for future release.**

### Kruize Legacy Removal (REQ-10.1 – REQ-10.5)

The native engine runs alongside the legacy code path. Kruize client code
(`internal/utils/kruize/`), internal Kafka topic references, and deployment
manifests remain. Removal is **next priority** after stabilization of all
currently implemented features.

---

## Implemented Features — Detailed Status

### PVC / Storage Rightsizing (REQ-6.3)

**Engine status:** Implemented. The engine reads the existing
`cm-openshift-storage-usage-YYYYMM.csv` from the operator tarball,
aggregates daily PVC digests (`daily_pvc_digests` table), and produces
right-sizing recommendations (`pvc_recommendation_sets` table).

**Deployment prerequisite (resolved):** The storage CSV is in `manifest.files`
(cost pipeline). The Koku listener (`kafka_msg_handler.py`) was updated to also
route `storage-usage` files to the ROS Kafka topic via `_ros_extra_patterns`.
See the snapshot staleness design doc for architectural context.

**Classifications:**

- **Oversized** — usage/capacity < 20% sustained (recommends 2x max usage)
- **Near-full** — usage/capacity > 85% (warns, recommends expansion)
- **Orphaned** — zero usage for 3+ days (`NotifPVCOrphaned`)
- **Healthy** — usage between 20-85%

Growth trend projection (linear regression on daily avg usage) estimates
days-to-full for capacity planning.

**API status:** `GET /recommendations/openshift/pvcs` with filters for
`cluster_uuid`, `namespace`, `recommendation_type`, pagination.

**Notification codes:** 20 (orphaned), 29 (oversized), 30 (near-full).

**UI status:** Not implemented.

### Snapshot Staleness Detection (REQ-6.5)

**Engine status:** Fully implemented. The engine ingests
`snapshot-inventory` CSVs from the operator tarball, classifies snapshots
(orphaned, stale, never-restored, redundant, managed, active), calculates
estimated monthly cost, and persists recommendations. Reconciliation removes
recommendations for snapshots no longer reported by the operator.

**Data pipeline:** Operator collects VolumeSnapshot objects via Kubernetes API
and writes `ros-openshift-snapshot-inventory-YYYYMM.csv` to
`resource_optimization_files`. Koku listener has a safety-net pattern
(`"snapshot-inventory"` in `_ros_extra_patterns`). Nise generates test data
via `--ros-ocp-info`.

**API status:** Fully implemented.
`GET /api/cost-management/v1/recommendations/openshift/snapshots` returns
paginated snapshot recommendations with filters for `cluster_uuid`,
`namespace`, and `recommendation_type`. Settings API at
`GET|PUT /api/cost-management/v1/recommendations/openshift/settings/snapshot`
manages per-org thresholds and cost rate with env-var locking.

**Notification codes:** 31 (orphaned), 32 (never restored), 33 (redundant),
34 (stale), 35 (managed).

**UI status:** Not implemented. No snapshot recommendations view or settings
page in koku-ui.

See [features-f-snapshot-staleness.md](./features-f-snapshot-staleness.md)
for full design details.

---

## Recently Implemented Lifecycle Features

See [features-f26-f33-f54-f55.md](./features-f26-f33-f54-f55.md) for full details.

- **Staleness detection (F55, REQ-10.8):** `?stale=` API filter, configurable threshold,
  archive sweep, `NotifStaleData` notification.
- **Idle/abandoned detection (F26, REQ-6.1):** Combined CPU+memory idle (< 10mc AND < 10 MiB),
  zero-usage abandoned, 100% savings estimate, `NotifIdleWorkload`/`NotifAbandonedWorkload`.
- **Adoption detection (F54, REQ-10.7):** Compares current requests to prior recommendation
  (15% tolerance), sets `recommendation_applied_at`, `NotifRecApplied`.
- **Fleet summary (F33, REQ-7.6):** `GET /recommendations/openshift/fleet-summary` aggregate endpoint.
- **Box plots (REQ-6.6):** Five-number summary (min, Q1, median, Q3, max) per term for containers and namespaces.
- **Quality metrics (F53, REQ-10.6):** Stability %, adoption detection, OOM events after rec.
- **History tracking (F56):** Time-series of past recs in `recommendation_history`, API at `/history`.

---

## API Endpoint Summary

| Endpoint | Methods | Status |
|----------|---------|--------|
| `/recommendations/openshift` | GET | Implemented |
| `/recommendations/openshift/:id` | GET | Implemented |
| `/recommendations/openshift/gpu` | GET | Implemented — GPU strategy summary (counts + links) |
| `/recommendations/openshift/gpu/timeslicing` | GET | Implemented — GPU time-slicing (node level) |
| `/recommendations/openshift/gpu/mig` | GET | Implemented — MIG profile recommendations list |
| `/recommendations/openshift/nodes` | GET | Implemented — node CPU/memory utilization |
| `/recommendations/openshift/nodes/utilization` | GET | Deprecated alias of `/nodes` (same behavior + warning) |
| `/recommendations/openshift/fleet-summary` | GET | Implemented |
| `/recommendations/openshift/pvcs` | GET | Implemented |
| `/openshift/namespace/recommendations` | GET | Implemented |
| `/recommendations/openshift/namespace/:id` | GET | Implemented |
| `/recommendations/openshift/settings/terms` | GET, PUT, DELETE | Implemented (per-plugin via `?recommendation_type=`) |
| `/recommendations/openshift/settings/capabilities` | GET | Implemented |
| `/recommendations/openshift/history` | GET | Implemented |
| `/recommendations/openshift/quality` | GET | Implemented |
| `/recommendations/openshift/snapshots` | GET | Implemented |
| `/recommendations/openshift/settings/snapshot` | GET, PUT | Implemented |

---

## Future Improvement: Keyset Pagination

### Current State

**Interim note:** Hot-path fixes for heavy list handlers (for example node utilization / GPU aggregation,
issues **#40** / **#41** in `docs/audits/490-issues.md`) use SQL-level `LIMIT`/`OFFSET` or smaller bounded scans rather
than loading entire result sets in Go. That remains **offset-based pagination** at the database layer.
**Keyset (cursor) pagination** described below is still the long-term approach for very large tenants and deep
pages; see also §Implementation Path.

Both **Koku** (Django REST Framework) and **ros-ocp-backend** (Go/Echo) use
offset/limit pagination across all list endpoints:

```
GET /recommendations/openshift?offset=200&limit=50
→ DB: SELECT ... ORDER BY x LIMIT 50 OFFSET 200
```

This works by scanning and discarding `offset` rows before returning `limit`
rows. Performance degrades linearly with page depth — page 100 at 50
items/page requires the DB to scan 5,000 rows to return 50.

### What Keyset Pagination Does

Instead of a numeric offset, the client passes an opaque cursor encoding the
last row's sort key:

```
GET /recommendations/openshift?after=eyJ1cGRhdGVkX2F0Ij...&limit=50
→ DB: SELECT ... WHERE (namespace, workload) > ('proj-x', 'deploy-y')
      ORDER BY namespace, workload LIMIT 50
```

The DB seeks directly to the cursor position using the index — O(1) per page
regardless of depth. No rows are scanned and discarded.

### Performance Comparison

| Page depth | Offset/limit | Keyset |
|------------|--------------|--------|
| Page 1 | ~1ms | ~1ms |
| Page 10 | ~2ms | ~1ms |
| Page 100 | ~15ms | ~1ms |
| Page 1000 | ~150ms | ~1ms |

(Approximate PostgreSQL timings for a 50k-row table with appropriate indexes.)

### Where It Would Help

| Service | Endpoint | Why |
|---------|----------|-----|
| ros-ocp-backend | `/recommendations/openshift` | Large orgs with 10k+ containers across clusters |
| ros-ocp-backend | `/recommendations/openshift/history` | History grows at ~1 row/container/term/engine/day |
| ros-ocp-backend | `/recommendations/openshift/pvcs` | Clusters with thousands of PVCs |
| Koku | `/reports/openshift/costs/?group_by[project]=*` | Orgs with hundreds of projects |
| Koku | `/tags/openshift/` | Tags with many distinct values |

### Implementation Path

**ros-ocp-backend (Go):**

- Add `after` query parameter to `ListAPIOptions`
- Decode cursor → `WHERE (sort_col) > (cursor_value)` SQL clause
- Encode last row's sort key into `next` cursor in response `links`
- Keep `offset`/`limit` as fallback (backward compatible)

**Koku (Django):**

- Django REST Framework ships `CursorPagination` built-in:
  ```python
  from rest_framework.pagination import CursorPagination
  class RecommendationCursorPagination(CursorPagination):
      page_size = 50
      ordering = '-updated_at'
  ```
- Requires a unique or near-unique indexed ordering column (timestamps +
  tiebreaker on `id` or composite key)

### Trade-offs

| Pro | Con |
|-----|-----|
| O(1) per page at any depth | No random access ("jump to page N") |
| Consistent results under concurrent writes | Requires stable sort order |
| Index-only scan (no seq scan for offset) | Client must store opaque cursor |
| Works with infinite scroll / streaming UIs | Breaking API change (new param format) |

### When to Implement

This is **not urgent**. Current scale (< 5,000 containers per org) is well
within offset/limit performance. Prioritize when:

- A customer reports slow pagination on deep pages
- The history table exceeds 100k rows per org
- The UI moves to infinite-scroll (no page numbers)

---

## GPU Summary `timeslicing.count` Divergence

**Severity:** Cosmetic (no UI consumers)

The `/recommendations/openshift/gpu` summary endpoint returns
`timeslicing.count` based on `CountNodeGPUTriples` (node×GPU-model pairs with
fresh telemetry data). The `/recommendations/openshift/gpu/timeslicing` detail
endpoint additionally runs `ComputeNodeTimeslicingRec`, which may reject groups
where utilization is too high for sharing (replicas < 2), where no containers
classify as underutilized, or where MIG takes precedence.

This means `timeslicing.count` can be > 0 while the detail endpoint returns
empty `data`. The count represents "GPU node groups with recent telemetry"
rather than "actionable time-slicing recommendations."

### Impact

None currently — no UI or external service consumes this endpoint.

### Resolution Options

1. **Rename field** to `gpu_node_groups` and document as "monitored groups"
2. **Run the engine** on all triples during summary (adds query cost)
3. **Accept the divergence** and document in OpenAPI (current choice)

### When to Fix

When a UI component or external consumer starts using this endpoint and the
mismatch causes user confusion.
