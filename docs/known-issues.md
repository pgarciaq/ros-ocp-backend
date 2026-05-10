# Feature Status — Native Recommendation Engine

This document tracks the implementation status of all features in the
ros-ocp-backend native engine, their API availability, UI support in
koku-ui, and known issues. **Code-verified** against the actual Go source —
not aspirational.

Last updated: 2026-05-10

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
| **Container recs** | Custom timeframes (1–90 day windows, 3 terms per org) | **Shipping** |
| **Container recs** | Idle / abandoned workload detection | **Shipping** |
| **Container recs** | CPU trend analysis (least-squares slope) | **Shipping** |
| **Container recs** | Dollar savings estimates (via Koku cost data) | **Shipping** |
| **Container recs** | Replica count display (min/max/avg pod count) | **Shipping** |
| **Container recs** | Recommendation history tracking | **Shipping** |
| **Container recs** | Recommendation quality (stability %, adoption detection) | **Shipping** |
| **Container recs** | Box plots (five-number summary from usage samples) | **Shipping** |
| **Namespace recs** | Namespace-level CPU + memory recommendations | **Ready, feature-flagged** |
| **GPU** | GPU workload classification (idle/underutilized/memory-bound/compute-bound) | **Shipping** |
| **GPU** | MIG profile selection (A100/A30/H100/H200/B100/B200) | **Shipping** |
| **GPU** | Node-level time-slicing guidance (nvidia.com/gpu.replicas) | **Shipping** |
| **GPU** | GPU savings estimates (from Koku cost model rates) | **Shipping** |
| **Storage** | PVC right-sizing (oversized/near-full/orphaned/healthy + growth trend) | **Shipping** |
| **Snapshots** | Snapshot staleness detection (orphaned/stale/never-restored/redundant) | **Shipping** |
| **Fleet** | Fleet summary (cross-cluster aggregate) | **Shipping** |
| **Platform** | RBAC (Insights RBAC middleware with cluster-level filtering) | **Shipping** |
| **Platform** | Notification system (~35 codes: confidence, OOM, idle, stale, GPU, PVC, snapshot) | **Shipping** |

### What's Next (Not Yet Implemented)

| Feature | Description | Complexity |
|---------|-------------|------------|
| VM recommendations | Virtual machine right-sizing for OpenShift Virtualization | Medium |
| Node CPU/memory right-sizing | Underutilized / overcommitted / stranded resource detection | Medium |
| MachineSet right-sizing | Instance type + replica count recommendations | High |
| Cloud instance catalog | AWS/Azure/GCP instance type database for cross-cloud right-sizing | Medium |
| JVM/Quarkus recommendations | Java-runtime-specific tuning (heap, GC) | Medium |
| HPA detection | Informational notifications for HPA-managed workloads | Low |
| Full Kruize removal | Remove legacy Kruize code path (native is default, legacy is fallback) | Low |
| Keyset pagination | Cursor-based pagination for large orgs (currently offset/limit) | Low |
| Shadow mode | Production dual-engine comparison (offline CLI tool exists) | Low |

### Known Caveats

| Issue | Impact | Severity |
|-------|--------|----------|
| Performance vs cost profiles store identical values | Both DB rows use cost-side outputs | Low — functional but redundant |
| Memory trend notification uses CPU slope at container level | `NotifMemoryTrendingUp` checks CPU trend (namespace level is correct) | Low — cosmetic |
| Notification code 29 collision | PVC_OVERSIZED and GPUTimeSharingCandidate share code 29 | Medium — affects notification text for GPU time-slicing |
| Namespace recs disabled by default | Requires `DISABLE_NAMESPACE_RECOMMENDATION=false` or Unleash flag | By design — opt-in per customer |

---

## Features Implemented in Engine, Missing UI

### Custom Timeframes (Settings API)

**Engine status:** Fully implemented. The engine supports configurable
`window_days` and `decay_halflife_hours` per term via the
`org_recommendation_terms` database table. `LoadTermConfig()` reads
per-org overrides at engine run time. Defaults are 1d/7d/15d.

**API status:** `GET/PUT/DELETE /api/cost-management/v1/recommendations/openshift/settings/terms`
endpoints are implemented. Users can configure custom term windows (1-90 days)
per org.

**UI status:** Not implemented. The koku-ui hardcodes term names
("short_term" / "medium_term" / "long_term") and displays whatever the
backend returns, but there is no settings page to configure window sizes or
decay parameters.

### Namespace Recommendations

**Engine status:** Fully implemented. `RecommendAllNamespaces()` produces
namespace-level recommendations from `daily_namespace_digests`.

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
  per-GPU and total-node dollar estimates on `GET /recommendations/openshift/nodes`.
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

### Replica Count Display

**Engine status:** Fully implemented. `pod_count_min`, `pod_count_max`,
`pod_count_avg` are computed from operator-reported `workload_pod_count`
(primary) or distinct pod name counting (fallback). Persisted in
`daily_container_digests` and `recommendation_sets`.

**API status:** Fully implemented. `GET /recommendations/openshift/:id`
returns `recommendations.replicas` with `min`, `max`, `avg` fields.
CSV export includes pod count columns.

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

### Java / JVM Recommendations

No workload-specific tuning (heap sizing, GC overhead detection). Would
require JVM-aware metrics from the operator and a specialized recommendation
model.

### HPA Scaling Suggestions

No horizontal scaling suggestions. `NotifHPASaturated` and `NotifHPAActive`
notification codes exist but are never set by the native engine. Would
require HPA status data from the cluster. (Note: replica count *display*
is implemented — see above — but the engine does not suggest scaling replica
count up or down.)

### PVC / Storage Rightsizing

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

### Snapshot Staleness Detection

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

- **Staleness detection (F55):** `?stale=` API filter, configurable threshold,
  archive sweep, `NotifStaleData` notification.
- **Idle/abandoned detection (F26):** Combined CPU+memory idle (< 10mc AND < 10 MiB),
  zero-usage abandoned, 100% savings estimate, `NotifIdleWorkload`/`NotifAbandonedWorkload`.
- **Adoption detection (F54):** Compares current requests to prior recommendation
  (15% tolerance), sets `recommendation_applied_at`, `NotifRecApplied`.
- **Fleet summary (F33):** `GET /recommendations/openshift/fleet-summary` aggregate endpoint.

---

## API Endpoint Summary

| Endpoint | Methods | Status |
|----------|---------|--------|
| `/recommendations/openshift` | GET | Implemented |
| `/recommendations/openshift/:id` | GET | Implemented |
| `/recommendations/openshift/nodes` | GET | Implemented |
| `/recommendations/openshift/fleet-summary` | GET | Implemented |
| `/recommendations/openshift/pvcs` | GET | Implemented |
| `/openshift/namespace/recommendations` | GET | Implemented |
| `/recommendations/openshift/namespace/:id` | GET | Implemented |
| `/recommendations/openshift/settings/terms` | GET, PUT, DELETE | Implemented |
| `/recommendations/openshift/history` | GET | Implemented |
| `/recommendations/openshift/quality` | GET | Implemented |
| `/recommendations/openshift/snapshots` | GET | Implemented |
| `/recommendations/openshift/settings/snapshot` | GET, PUT | Implemented |
