# Feature Status — Native Recommendation Engine

This document tracks the implementation status of all features in the
ros-ocp-backend native engine, their API availability, UI support in
koku-ui, and known issues. **Code-verified** against the actual Go source —
not aspirational.

Last updated: 2026-06-02 (GPU MIG Gap 5 — UI and test-data gaps)

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
| **Container recs** | Dollar savings estimates (via Koku `effective_rates`; CPU, memory, infra, distributed) | **Shipping** |
| **Node recs** | Dollar savings estimates (CPU/memory capacity + node consolidation) | **Shipping** |
| **Storage** | PVC dollar savings estimates (storage cost model rates) | **Shipping** |
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
| **Quota** | ResourceQuota right-sizing (tighten/raise/optimal, risk levels, savings on tighten) | **Shipping** |
| **Snapshots** | Snapshot staleness detection (orphaned/stale/never-restored/redundant) | **Shipping** |
| **Node recs** | Node CPU/memory right-sizing (Tier 1: dual cost/performance engines, nested API) | **Shipping (enabled by default)** |
| **Fleet** | Fleet summary (cross-cluster container health aggregate) | **Shipping** |
| **Fleet** | Fleet savings summary (cross-plugin persisted savings, `?engine=`) | **Shipping** |
| **Platform** | RBAC (Insights RBAC middleware with cluster-level filtering) | **Shipping** |
| **Platform** | Notification system (**54** codes: confidence, OOM, idle, stale, GPU, PVC, snapshot, VM) — [reference](architecture/notification-codes.md) | **Shipping** |
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

### Planned Future Work (Node Tier 2 & Tier 3)

These are **planned releases**, not open defects. Tier 1 node recommendations are shipping; MachineSet and MachineAutoscaler tiers are documented in [architecture/node-recommendations-roadmap.md](architecture/node-recommendations-roadmap.md).

| Tier | Feature | REQs | Est. effort | Status |
|------|---------|------|-------------|--------|
| **2** | MachineSet right-sizing (replica count + instance family via cloud catalog) | REQ-8c.4, REQ-8c.5, REQ-8c.6 | ~2–3 weeks | Planned |
| **3** | MachineAutoscaler optimization (min/max bounds, saturated/idle/flapping) | REQ-8c.7 | ~4–6 weeks after Tier 2 | Planned; depends on Tier 2 |

**Tier 2 prerequisites:** Operator `machineset_name` on ROS CSV → ingest into `daily_node_digests` → `machineset` engine plugin → `GET .../machinesets` API → `machineset_recommendations` table + instance catalog.

**Tier 3 prerequisites:** Tier 2 + operator MachineAutoscaler specs/history → time-series engine → API extension.

**Scope limit:** MachineSets only (IPI). No Tier 2/3 for bare metal, SNO, or clusters without Machine API (`machineset_name` NULL).

### Future: Seasonality / Proactive Recommendations

**Status:** Planned / Future Work — **not implemented** and not scheduled for the
current release train.

Today's container, node, PVC, quota, and GPU recommendations are **reactive**:
they compare recent usage to current limits and advise changes after utilization
patterns are already visible. **Seasonality / proactive recommendations** would
detect recurring CPU, memory, storage, and fleet-level patterns (for example
Monday-morning login spikes, month-end batch jobs, holiday traffic, or steady PVC
growth) and emit forward-looking guidance so operators can right-size **before**
the next predictable peak.

**Why deferred:**

- Requires **90+ days** of daily aggregated metrics per entity before seasonal
  signals are reliable; **two or more years** of history for annual patterns.
- Needs a new metrics history pipeline, forecasting plugins, and API fields — not
  an extension of the existing percentile-based container engine.
- Statistical forecasting (classical decomposition / ETS per the design doc — not
  foundation models) still demands substantial engineering, backtesting, and
  confidence gating before customer-facing rollout.

**Design references (planned only):**

- [Seasonality plugin design](design/seasonality-plugin.md)
- [Product overview (docs-site)](../docs-site/features/seasonality.md)

### Not Planned for Current MVP

These features are documented in `requirements.md` but are explicitly
**not planned** for the current MVP release. They require new operator
Prometheus queries, external runtime detection, or upstream fixes.

| Feature | REQs | Reason |
|---------|------|--------|
| HPA optimization | REQ-8.1 | Needs 8 new operator queries; low customer demand |
| Ephemeral storage | REQ-8.2 | cadvisor metrics unreliable through OCP 4.21; pending upstream fix |
| Node.js heap advisory | REQ-8.3 | Weakest rec type; needs new operator query; no actionable numeric value |
| Go GOMAXPROCS/GOMEMLIMIT | REQ-6.4 | Needs new operator query (`go_info`); niche audience |
| JVM runtime detection | REQ-9.1 – REQ-9.5 | Needs optional operator queries + JVM-specific metrics; medium effort |
| Multi-GPU awareness | REQ-5.5 | See [GPU: Deferred / Future Work](#gpu-deferred--future-work) item **2** |
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
| Savings stale until re-ingestion | Container/node/PVC `estimated_monthly_savings_usd` reflects rates from the last successful Masu fetch during ingestion; Koku cost model changes do not update ROS rows until the next report cycle | Low — by design | REQ-7.5 |
| No UI for most new features | Node recs, PVC recs, snapshots, GPU recs, **quota/CRQ recs**, fleet summary, quality, history, settings all have APIs but no koku-ui views | Medium — features are API-only until UI catches up | Multiple |
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

**Data pipeline:** Operator emits `node_allocatable_cpu_cores`,
`node_allocatable_memory_bytes`, `node_capacity_cpu_cores`, `instance_type`,
and related fields in ROS container CSVs → parser → `daily_node_digests` →
engine → `node_recommendations` table. Prefer allocatable columns when present;
otherwise falls back to `ROS_NODE_ALLOCATABLE_FACTOR` × request totals.

**Configuration (env vars):**

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.30 | p95 below this = underutilized |
| `ROS_NODE_OVERCOMMIT_THRESHOLD` | 1.50 | Request/allocatable ratio above this = overcommitted |
| `ROS_NODE_ALLOCATABLE_FACTOR` | 0.93 | Fraction of capacity considered allocatable |
| `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | 0.6 | EMA-smoothed imbalance above this = stranded |
| `ROS_NODE_EMA_ALPHA` | 0.3 | EMA smoothing alpha (higher = less smoothing) |
| `ROS_NODE_ZOMBIE_CPU_MC` | 200 | Zombie: CPU P95 below this (millicores) with few pods |
| `ROS_NODE_ZOMBIE_MAX_PODS` | 5 | Zombie: max pod count |
| `ROS_NODE_IDLE_CPU_UTIL_PCT` | 10 | Idle: CPU util % of allocatable |
| `ROS_NODE_IDLE_MEM_UTIL_PCT` | 10 | Idle: memory util % of allocatable |
| `ROS_NODE_IDLE_MAX_PODS` | 10 | Idle: max pod count |

**Level 3 consolidation:** [`applyInstanceTypeConsolidation`](../../internal/engine/recommend_nodes.go)
groups underutilized nodes by `instance_type` and distributes `node_count_reduction`
across the fleet (not per-node binary only).

**Node idle state:** `idle_state` (`active` / `idle` / `zombie`) on each node row;
`filter[idle_state]` on the nodes API; notification code **15** when idle or zombie.

**Dual engines:** Each node/term stores separate **cost** (80% target utilization)
and **performance** (55% target) engine rows, mirroring container
`recommendation_engines`. Classification (underutilized, overcommitted, stranded)
is shared; sizing and savings differ per engine.

**API status:** Canonical **`GET /recommendations/openshift/nodes`** returns one
object per node with shared classification/metrics, `instance_type` when known,
and nested `recommendation_terms.<term>.recommendation_engines.{cost,performance}`.
Optional `?engine=cost|performance` filters which engine blocks are returned.
Pagination counts distinct nodes; default sort is medium-term
`estimated_monthly_savings` (structured object; cost engine unless filtered).
`order_by=estimated_monthly_savings_usd` remains a deprecated alias. `meta.currency`
reflects the Koku cost model unit. Deprecated alias:
`GET .../nodes/utilization`.

**Notification codes:** 11 (underutilized), 12 (overcommitted), 13 (stranded resources), 15 (node idle/zombie), 25 (`NotifNoCostData` when savings cannot be computed).

**Savings:** Computed at ingestion via [`ApplyNodeSavings()`](../../internal/engine/node_savings.go) using `cpu_core_usage_per_hour`, `memory_gb_usage_per_hour`, and `node_cost_per_month` from Masu `effective_rates`. Requires migrations **000070** (savings column), **000071** (engine PK), and **000072** (sizing columns). See [architecture/cost-integration.md](./architecture/cost-integration.md).

**UI status:** Not implemented. Requires a node recommendations view and a null
state for the 3-day cold start period.

**Business hours for nodes: Intentionally skipped.** Nodes are always-on
infrastructure; `idle_state` classification (`active` / `idle` / `zombie`) covers
the decommissioning case without schedule complexity. Container and namespace
recommendations retain business-hours support where usage patterns are
time-of-day dependent.

**Planned future work (Tier 2+):** See [node-recommendations-roadmap.md](architecture/node-recommendations-roadmap.md) for full Tier 2 (MachineSet right-sizing, ~2–3 weeks) and Tier 3 (MachineAutoscaler optimization, ~4–6 weeks) design, prerequisites, and limitations. Summary:

| Tier | Scope | Status |
|------|--------|--------|
| Tier 2 — MachineSet | Replica count + instance family at MachineSet level; `.../machinesets` API; `machineset_recommendations` table; cloud catalog | **Planned** (not a defect) |
| Tier 3 — MachineAutoscaler | Historical scaling analysis; min/max bounds; saturated/idle/flapping | **Planned**; depends on Tier 2 |

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
  `enrichWithGPU` and mapped to `estimated_monthly_gpu_savings` in API.
- Node-level time-slicing savings via `ComputeNodeTimeslicingRec` with
  per-GPU and total-node dollar estimates on **`GET /recommendations/openshift/gpu/timeslicing`**
  (canonical path for GPU time-slicing; previously `/recommendations/openshift/nodes`).
- Container-level time-slicing cross-reference (`time_slicing_node`,
  `time_slicing_replicas`) on container GPU blocks.
- API query filters (`has_gpu`, `gpu_model`, `gpu_classification`) — pushed to SQL
  on `recommendation_sets` (`has_gpu`, `gpu_model_name`, `gpu_classification` columns).
  Documented in `openapi.json`.
- GPU daily digest aggregation pipeline — `upsertGPUDigests` in `pipeline.go`
  aggregates hourly CSV rows into daily `gpu_container_digests` rows during
  ingestion. Partition creation, upsert-on-conflict, and retention sweep all
  operational.

- Container-level time-slicing dollar savings — `EstimatedTimeslicingSavingsUSD`
  on `GPURec`, populated by `ComputeNodeTimeslicingRec` with the per-candidate
  share of `SavingsPerGPU`. Exposed as `estimated_monthly_timeslicing_savings`
  (structured object) in the container API response.

**Not yet implemented in UI:**

- Koku-UI display of GPU recommendations (classifications, savings, time-slicing)

**Known limitations (accepted risk):**

- **Retention vs ingestion race**: `RunRetentionSweep` could DROP a partition
  while `upsertGPUDigests` writes to it (e.g., during backfill of old data).
  PostgreSQL locking makes this fail loud (write error) rather than corrupt.
  Normal operation (recent data + 6-month retention) is unaffected.

See `docs/archive/gpu-recommendations.md` for detailed design and
`docs/archive/gpu-recommendations-test-plan.md` for E2E testing guide.

### Deferred: Quota UI

ResourceQuota and ClusterResourceQuota recommendation **APIs are production-ready**;
dedicated **koku-ui views are deferred** (large effort; ResourceQuota status report item 9).

| Planned UI | API today |
|------------|-----------|
| Quota list (utilization, risk level, savings) | `GET /recommendations/openshift/quota/` |
| Quota detail / breakdown | `GET /recommendations/openshift/quota/detail` |
| ClusterResourceQuota list | `GET /recommendations/openshift/cluster-quota/` |
| ClusterResourceQuota detail | `GET /recommendations/openshift/cluster-quota/detail` |
| Notification integration (codes **70–73**) | Emitted on quota / cluster-quota rows |
| Historical trend visualization | `history[]` on detail endpoints |

See [quota-recommendations.md](features/quota-recommendations.md#roadmap--future-work) and
[ui-integration-guide.md](ui-integration-guide.md#4b-resourcequota-and-clusterresourcequota-recommendations).

### GPU MIG — Known limitations (Gap 5)

Phase 12 documents **intentional** limits on MIG recommendations — backend scale trade-offs,
missing product UI, and test-data prerequisites. Backend/API gaps **2** and **3** (pagination,
multi-GPU consolidation) are acceptable at current fleet sizes and tracked in the deferred table
below, not as defects.

#### MIG list in-memory pagination

`GET /recommendations/openshift/gpu/mig` ([`handlers_gpu_mig.go`](../../internal/api/handlers_gpu_mig.go))
loads MIG recommendations for every cluster in the org by calling
`QueryGPURecommendations` per cluster, builds the full result set in memory, then applies
RBAC, tag filters, sort, and `offset`/`limit` pagination in Go. Filter and sort keys are
not pushed to SQL (see [`GpuMigAllowedOrderBy`](../../internal/api/listoptions/list_options.go)).

**Why this is acceptable now:** Typical fleets have tens to low hundreds of MIG-enabled
containers. The in-memory path adds well under ~50 ms of API latency at that scale.

**What would be needed at scale (thousands of MIG workloads):** Refactor to SQL-level
pagination — for example a materialized `gpu_mig_recommendations` table populated during
the recommendation pipeline, or indexed page keys on `gpu_container_digests` with
post-filter for MIG-only rows — mirroring patterns used for other heavy list endpoints.

#### Multi-GPU container consolidation (per-container only)

The MIG engine recommends a profile **per container independently** based on that
container's utilization and frame-buffer usage. It does **not** analyze whether several
containers on a node each use a fraction of a **different** GPU and could be consolidated
onto fewer physical GPUs to free one for other work.

**Why this is acceptable now:** That scenario is a cluster-wide **scheduling / bin-packing**
optimization. It requires correlating per-GPU placement, MIG instance occupancy, and
workload affinity across the node — substantially more complex than per-container profile
sizing. Inference and single-GPU workloads (>95% of current GPU containers) are fully
covered; multi-GPU training pods are rare outside dedicated ML clusters (see REQ-5.5).

**What would be needed:** Per-device telemetry (DCGM by GPU UUID or `gpu_request_count` from
the operator), a node-level consolidation model, and API/notification surfaces for
"request fewer GPUs" or "co-locate these workloads." VMs already expose multi-GPU guidance
via notification **54**; container path would follow deferred item **2** prerequisites.

#### ROS MIG recommendations UI (not shipped)

**koku-ui** has no Optimizations pages that consume the ROS GPU recommendation APIs:

- `GET /recommendations/openshift/gpu` — strategy summary (MIG vs time-slicing counts and links)
- `GET /recommendations/openshift/gpu/mig` — per-container MIG profile, classification, confidence
- `GET /recommendations/openshift/gpu/timeslicing` — node-level sharing guidance

The **cost-side** MIG drill-down (`GET /reports/openshift/gpu/mig_profiles/` in Koku, often
behind an Unleash flag) covers slice-level **spend** attribution. It does **not** surface ROS
recommendation fields (`recommended_gpu_profile`, `gpu_classification`, `gpu_confidence`,
`estimated_monthly_gpu_savings`, notification codes **10** / **26**–**28** / **36**).

**Why this is acceptable now:** ROS GPU list/detail APIs are stable, OpenAPI-documented, and
validated via API clients, Bruno, and IQE where GPU data exists. Product deferred the
Optimizations UX until backend contracts were settled; cost accounting shipped first.

**What would be needed:** New koku-ui Optimizations views (summary cards, MIG table, time-slicing
table) wired to the ROS endpoints above. Intended patterns are in
[ui-integration-guide.md](ui-integration-guide.md#12-gpu-recommendations). Tracked as deferred
item **5** and **9** below.

#### GPU E2E and IQE test data prerequisite

GPU-focused **E2E** (`cost-onprem-chart` `test_gpu_mig_recommendations_flow`, etc.) and **IQE**
(`iqe-ros-ocp-plugin` / `iqe-cost-management-plugin` GPU/MIG tests) **skip or assert empty**
when the cluster has no GPU ROS payloads ingested or when `GET .../gpu` reports `mig.count == 0`.
That is expected — not a backend bug.

**Why this is acceptable now:** Tests gate on real telemetry rather than fabricating API
responses. Most CI/staging clusters are CPU-only; skipping avoids false failures.

**What is needed for green GPU test runs (operational, not code):**

1. Generate data with **nise** using `--ros-ocp-info` and GPU/MIG workloads (e.g.
   `examples/ros_ocp/ocp_static_data.yml` with `mig.count` > 0 profiles).
2. Package typed ROS CSVs (not combined `openshift_report.*.csv` only) and ingest through the
   normal operator → listener → ros-processor path.
3. Ensure the operator collects DCGM profiling metrics on GPU-capable nodes.
4. Wait for recommendation pipeline completion, then verify `mig.count > 0` before E2E/IQE GPU
   suites.

See `docs/archive/gpu-recommendations-test-plan.md` and
`cost-onprem-chart/docs/development/skipped-iqe-tests.md` (GPU skip groups). Tracked as deferred
item **10** below.

### GPU: Deferred / Future Work

The following items are **not** shipping defects. They are tracked enhancements
deferred until prerequisites or customer scale justify the investment. Gap 5 detail:
[GPU MIG — Known limitations (Gap 5)](#gpu-mig--known-limitations-gap-5).

| # | Item | Consumer | Why deferred | Prerequisites |
|---|------|----------|--------------|---------------|
| **1** | **GPUs per node** — add `node_gpu_count` to ROS data from `kube_node_status_allocatable{resource='nvidia.com/gpu'}` | Node-level GPU savings calculation; Tier 2 MachineSet GPU-aware consolidation | No backend consumer today. Node recommendations only compute CPU/memory utilization. Making GPU count actionable requires a GPU-aware node consolidation engine plus Tier 2 MachineSet awareness; without Tier 2 the number is informational-only. | Operator query + CSV column; ros-ocp-backend ingestion + engine changes; Tier 2 MachineSet plugin |
| **2** | **Multi-GPU container consolidation** — per-device DCGM correlation; no cluster-wide "free a GPU by co-locating workloads" (see [Gap 5](#gpu-mig--known-limitations-gap-5)) | ML training workloads that request 4–8 GPUs per pod but only utilize 2–3; nodes where many containers each hold a slice on a different GPU | Rare outside dedicated ML clusters (<5% of GPU workloads). Per-container MIG sizing does not perform bin-packing across GPUs on a node. Requires per-device UUID correlation that Kubernetes does not expose cleanly; operator needs significant new collection logic. The 1-GPU-per-container assumption covers >95% of inference workloads. VM path already has multi-GPU support (notification **54**). See also REQ-5.5 / F25. | Operator per-device DCGM by UUID or `gpu_request_count`; new `gpu_container_device_digests` table; node-level consolidation engine + notification changes |
| **3** | **MIG list endpoint SQL-backed pagination** — replace in-memory filter/sort/paginate on `GET /recommendations/openshift/gpu/mig` (see [Gap 5](#gpu-mig--known-limitations-gap-5)) | Large GPU fleets (10k+ MIG-capable containers) where full-cluster recompute per API call becomes slow | Current deployments have tens to low-hundreds of MIG-enabled containers; in-memory handling adds <50ms. A materialized `gpu_mig_recommendations` table or SQL page keys on digests is a significant refactor with no visible benefit until that scale threshold. | Materialized table populated during the recommendation pipeline, or SQL page keys on `gpu_container_digests` with post-filter |
| **4** | **MIG + time-slicing combined strategy** — time-slicing within MIG partitions instead of mutually exclusive strategies in `partitionContainers` (MIG recs currently exclude time-slicing candidates) | Clusters with heterogeneous GPU workloads where some containers benefit from MIG isolation and others from time-slicing on the same node | Complex scheduling semantics; NVIDIA treats MIG and time-slicing as separate strategies. Combining requires per-GPU partition state (instances, sizes, pod sharing). Low demand. | MIG instance scheduling model; operator partition telemetry |
| **5** | **UI for GPU time-slicing recommendations** — frontend views for `GET /recommendations/openshift/gpu/timeslicing` and `GET /recommendations/openshift/gpu/mig` | Cluster admins who want visual guidance on GPU sharing without using the API directly | All ROS UI work is deferred pending upstream acceptance of backend APIs. Intended UX patterns are documented in [ui-integration-guide.md](ui-integration-guide.md). | koku-ui GPU optimizations pages |
| **9** | **ROS MIG recommendations Optimizations UI** — no koku-ui pages for `GET .../gpu`, `/gpu/mig`, `/gpu/timeslicing` (see [Gap 5 § ROS MIG recommendations UI](#ros-mig-recommendations-ui-not-shipped)) | FinOps users who need recommended MIG profiles, classification, and confidence in the console | Cost-side MIG **spend** UI exists (`reports/openshift/gpu/mig_profiles/`); ROS **recommendation** UX is a separate product surface. Backend APIs are ready; UI is deferred. | koku-ui Optimizations GPU section per [ui-integration-guide.md](ui-integration-guide.md#12-gpu-recommendations) |
| **10** | **GPU E2E/IQE data prerequisite** — tests skip without GPU ROS ingest and `mig.count > 0` (see [Gap 5 § GPU E2E and IQE](#gpu-e2e-and-iqe-test-data-prerequisite)) | CI pipelines expecting GPU tests to pass on CPU-only clusters | Not a code gap — operational fixture requirement. Documented so QE knows why GPU suites are skipped. | nise `--ros-ocp-info` + GPU workloads; operator DCGM; full ingest cycle |
| **6** | **Materialized time-slicing results (performance)** — persist time-slicing recommendations during the recommendation pipeline instead of computing at read-time | Large GPU fleets (1000+ node×model triples) where read-time computation adds latency | Current scale is well within acceptable latency (<50 ms). Materialization adds write-path complexity and staleness concerns. Revisit when GPU fleet sizes grow ~10×. | Pipeline write path; recompute on term or threshold changes |
| **7** | **Multi-GPU container awareness for time-slicing** — per-device analysis instead of assuming one GPU per container (e.g., 4-GPU ML training pods) | Dedicated ML training clusters with multi-GPU pods | Same prerequisites as deferred item **2** (per-device operator data). Rare workload pattern. Inference workloads remain covered by the 1-GPU assumption. | See deferred item **2**; operator per-device DCGM or `gpu_request_count` |
| **8** | **GPU summary `timeslicing.count` accuracy** — summary count reflects telemetry triples, not actionable list rows | Dashboards or automation that badge summary counts as “N recommendations ready” | **Intentional trade-off**, not a bug to fix: full engine on every summary request would add significant cost. See [GPU Summary `timeslicing.count` Divergence](#gpu-summary-timeslicingcount-divergence). Use list `meta.count` or notification **36** for actionable items. | Product/UI requirement to align counts (rename, engine-on-summary, or copy-only) |

**Current behavior (items 4 and 7):** In `partitionContainers`, a container recommended
for a non-`full_gpu` MIG profile is excluded from time-slicing candidates even if
multiple pods could share that MIG instance. Time-slicing math assumes one GPU per
container unless item **2** / **7** prerequisites land.

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
internal endpoint (`GET /effective_rates/`). Savings include cost model
rates (CPU + memory), infrastructure costs (raw + markup), and
distributed overhead (platform, worker, storage, network, GPU),
apportioned by the cost model's distribution type (cpu or memory) and
scaled by replica count.

**OCP-on-cloud:** For clusters registered as OCP on AWS/Azure/GCP with both
sources ingested in Koku, `namespace_aggregates.infrastructure_cost` from
`effective_rates` already reflects correlated cloud infrastructure spend.
No additional ROS correlation logic is required.

**Kill-switch:** `ROS_SAVINGS_ESTIMATES_ENABLED=false` (default `true`) disables
Masu `effective_rates` fetches on ros-processor and ros-api. Recommendations are
still produced; dollar fields are `$0` / omitted and `NotifNoCostData` (code 25)
is appended on container, node, and PVC responses. Snapshot recoverable-cost
estimates skip the dynamic effective-rates default (Settings API, env, and compiled
default still apply). See [architecture/cost-integration.md](./architecture/cost-integration.md).

**Plugin coverage** (identical matrix in [cost-integration.md](./architecture/cost-integration.md)):

| Plugin | Dollar estimates |
|--------|------------------|
| Container | Yes (ingestion) |
| GPU (container detail) | Yes (API read) |
| Node GPU time-slicing | Yes (API read) |
| Node (CPU/memory) | Yes (ingestion) |
| Namespace | No |
| PVC | Yes (ingestion) |
| Snapshot | Yes — recoverable monthly cost (ingestion) |

**API status:** `estimated_monthly_savings` (structured `{value, units}`) on container detail, nested node
engine blocks, and PVC list responses. `GET .../savings-summary` aggregates
persisted container, node, PVC, and snapshot totals (GPU excluded — read-time only).
Responses include top-level or `meta.currency` (ISO 4217 from Koku). When no cost
data is available, `NotifNoCostData` (code 25) is included on container, node, and
PVC recommendations.

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

**Implemented (Preview/Beta).** Virtual machine right-sizing is available with
14 Prometheus queries, daily digest pipeline, full recommendation engine
(CPU/memory sizing, idle/abandoned detection, disk projection, instance type
matching), and dedicated API endpoints. Gated by `ROS_ENABLE_VM_RECS` (default
`true`). Remaining gap: MachineSet-level VM placement optimization.

### MachineSet Right-Sizing (REQ-8c.4, REQ-8c.5) — PLANNED

Node Tier 1 is implemented. Tier 2 (MachineSet) and Tier 3 (MachineAutoscaler) are **planned future work** — see [node-recommendations-roadmap.md](architecture/node-recommendations-roadmap.md). Not tracked as product defects.

### Namespace ResourceQuota Recommendations (REQ-8.4) — IMPLEMENTED

The **`quota`** plugin (Phase 1, priority 35) compares ResourceQuota **hard** and
**used** limits from the ROS namespace CSV against aggregated container recommendations.
API: `GET /api/cost-management/v1/recommendations/openshift/quota/`. See
[quota-recommendations.md](features/quota-recommendations.md).

**Operator dependency (namespace quota):** Non-compute quota resources (`requests.storage`, `pods`, `count/*`) and per-`ResourceQuota` object name are **not** in the operator CSV today — PromQL sums by namespace only. See [quota-recommendations.md](features/quota-recommendations.md#future-work-namespace-quota).

### ClusterResourceQuota Recommendations (REQ-8.4b) — IMPLEMENTED

The **`cluster-quota`** plugin (Phase 1, priority 36) ingests
`ros-openshift-cluster-quota-*.csv`, compares CRQ hard/used to aggregated namespace quota
recommendation totals, and exposes
`GET /api/cost-management/v1/recommendations/openshift/cluster-quota/`. See
[cluster-resource-quota.md](features/cluster-resource-quota.md).

**One-cycle lag:** Same as namespace quota — CRQ recommendations read namespace quota and
container data from PostgreSQL, not in-memory from the same payload. If only the cluster-quota
CSV arrives in a cycle, recommended-hard sums may reflect the **previous** namespace quota run
until container and namespace/quota processing complete. Expect one report cycle after first
deployment for signals to fully align.

**Operator dependency (CRQ):** Namespace membership, storage, pods, and object-count columns require a current koku-metrics-operator build. Older CSVs without `namespaces` still use a cluster-wide namespace-quota aggregate.

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
`filter[cluster]`, `filter[project]`, `filter[recommendation_type]`, `filter[term]`,
`filter[storageclass]`, `order_by`/`order_how`, and pagination.
`GET /recommendations/openshift/pvcs/detail` returns all terms plus daily usage history.
Responses include `estimated_monthly_savings` when Masu storage rates are available.

**VM–PVC correlation (known limitation):** The koku-metrics-operator **cost** VM CSV
(`cm-openshift-vm-usage`) includes `vm_persistentvolumeclaim_name`, and the **storage**
CSV includes a `pod` column (often a `virt-launcher-*` name). ROS does not ingest either
field today: `ParseVMCSVRows` ignores PVC/pod columns on VM usage, and PVC ingestion
did not persist `pod` until migration **000114**. VM shared-storage notifications
([`DetectSharedPVCs`](../../internal/engine/vm_pvc_correlation.go)) therefore use a
namespace + resource-profile peer heuristic only. True PVC→virt-launcher pod→VM mapping
requires ROS to ingest `pod` on storage digests and either `vm_persistentvolumeclaim_name`
or `exported_pod` on VM digests (operator ROS VM CSV would need the latter column added).

**Savings:** Computed at ingestion via [`ApplyPVCSavings()`](../../internal/engine/pvc_savings.go)
using `storage_gb_request_per_month` (fallback: `storage_gb_usage_per_month`).
Requires migration **000070**. See [architecture/cost-integration.md](./architecture/cost-integration.md).

**Notification codes:** 20 (orphaned), 29 (oversized), 30 (near-full), 25 (`NotifNoCostData` when savings cannot be computed).

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
- **Fleet summary (F33, REQ-7.6):** `GET /recommendations/openshift/fleet-summary` container health aggregate.
- **Fleet savings summary:** `GET /recommendations/openshift/savings-summary` cross-plugin savings totals with optional `?engine=` (default `cost`). See [cost-integration.md](./architecture/cost-integration.md).
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
| `/recommendations/openshift/fleet-summary` | GET | Implemented — container health aggregate |
| `/recommendations/openshift/savings-summary` | GET | Implemented — cross-plugin savings (`?engine=cost\|performance`) |
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

This is a **medium-priority improvement**. Large customers have 200,000+
containers per org, making deep-page offset/limit queries expensive
(offset=5000 with 50 items/page forces the DB to skip 5000 rows).
Prioritize when:

- Deep-page API latency exceeds SLA thresholds
- The history table grows significantly (1 row/container/term/day)
- The UI moves to infinite-scroll (no page numbers)

---

## GPU Summary `timeslicing.count` Divergence

Tracked as **intentional future-work trade-off** in [GPU: Deferred / Future Work](#gpu-deferred--future-work) item **8** — not a defect backlog item.

**Severity:** Cosmetic / semantic gap (not a bug)

The `/recommendations/openshift/gpu` summary endpoint returns
`timeslicing.count` from `CountNodeGPUTriples`: distinct **node×GPU-model**
groups with fresh rows in `gpu_container_digests` (areas with recent GPU
telemetry — potential time-slicing candidates).

The `/recommendations/openshift/gpu/timeslicing` list runs
`ComputeNodeTimeslicingRec` on each group and may return **no row** when
utilization is too high for sharing (`recommended_replicas` &lt; 2), no
containers classify as underutilized, workloads are memory-bound or idle, or
MIG takes precedence.

So `timeslicing.count` can be **greater than** `meta.count` on the list (or
non-zero while `data` is empty). The summary count is **not** a badge count of
actionable recommendations.

### Why it is intentionally not aligned

Running the full time-slicing engine for every triple on every summary request
would add significant query and CPU cost (classification + replica math per
group). The summary is designed as a cheap inventory of monitored GPU groups;
the list endpoint is the source of truth for actionable node-level guidance.

### Impact

UI or automation that treats `timeslicing.count` as “N recommendations ready”
will over-count. Use the list endpoint (or container notification code **36**)
for actionable items.

### Resolution options (if product requires alignment later)

Deferred item **8** in [GPU: Deferred / Future Work](#gpu-deferred--future-work) catalogs these paths:

1. **Rename field** to `gpu_node_groups` and document as monitored groups only
2. **Run the engine** on all triples during summary (adds query cost)
3. **Keep divergence** and document in OpenAPI + UI copy (**current choice** — intentional trade-off)

### When to change behavior

When a shipped UI depends on the summary count matching list cardinality and
the mismatch causes user confusion — prefer list `meta.count` or explicit
empty-state copy over inflating summary cost.
