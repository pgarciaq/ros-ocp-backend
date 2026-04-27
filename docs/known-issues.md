# Known Issues and Missing UI Integration

This document tracks features that are implemented in the ros-ocp-backend
native engine but lack corresponding UI support in koku-ui, as well as
features that are not yet implemented in the engine.

Last updated: 2026-04-27

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

**API status:** Not exposed. There is no API endpoint to query historical
recommendations or quality trends over time.

**UI status:** Not implemented. No timeline or trend visualization of how
recommendations have changed.

### Stability / Quality Metrics

**Engine status:** Fully implemented. `quality.go` computes
`stability_pct`, `adoption_detected`, `oom_events_after_rec`, and
`recommendation_age_hours`.

**API status:** Not exposed as a dedicated endpoint. Quality data is
computed and stored but not served to clients.

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

## Features Not Implemented in Engine

### GPU Recommendations

No GPU fields or recommendation logic in the native engine.
`NotifGPUUnderutilized` notification code exists in constants but is never
emitted. Would require GPU usage data from the koku-metrics-operator
(already collected) to be ingested into ROS digests.

### Java / JVM Recommendations

No workload-specific tuning (heap sizing, GC overhead detection). Would
require JVM-aware metrics from the operator and a specialized recommendation
model.

### HPA Scaling Suggestions

No horizontal scaling suggestions. `NotifHPASaturated` and `NotifHPAActive`
notification codes exist but are never set by the native engine. Would
require HPA status data from the cluster. (Note: replica count *display*
is implemented -- see "Implemented" section -- but the engine does not
suggest scaling replica count up or down.)

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

### PVC / Storage Rightsizing

No storage recommendation logic. `NotifPVCOrphaned` notification code
exists but is unused. Would require PVC capacity/usage time series from
the operator (already collected in storage_usage CSVs) to be ingested
into a storage-specific digest table.

---

## API Endpoint Summary

| Endpoint | Methods | Status |
|----------|---------|--------|
| `/recommendations/openshift` | GET | Implemented |
| `/recommendations/openshift/:id` | GET | Implemented |
| `/openshift/namespace/recommendations` | GET | Implemented |
| `/recommendations/openshift/namespace/:id` | GET | Implemented |
| `/recommendations/openshift/settings/terms` | GET, PUT, DELETE | Implemented |
| `/recommendations/openshift/history` | — | Not implemented |
| `/recommendations/openshift/quality` | — | Not implemented |
