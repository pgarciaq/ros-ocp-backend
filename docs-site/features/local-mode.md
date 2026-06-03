# Resource Optimization for OpenShift Local Mode

!!! warning "Status: Planned / Future Work"
    This feature is **not yet implemented**. The description below is the intended
    product direction for a future release. All current recommendation features
    remain available today via the existing remote/upload architecture.

!!! info "Quick Facts (planned)"
    **Scope:** On-cluster recommendation computation with fleet-wide aggregation  
    **Operator:** koku-metrics-operator (new `local-recommendations` mode)  
    **Data source:** Prometheus / Thanos (direct PromQL, no CSV intermediary)  
    **Tags:** Kubernetes labels (real-time) + cloud tags reconciled at central enrichment  
    **Local API:** Lightweight ros-ocp-api deployment (same API contract, JWT / OAuth proxy auth)  
    **Storage:** PostgreSQL (recommendations only, ~70 MB / 1000 containers; in-cluster or external)  
    **Fleet view:** Push recommendation JSON to central Cost Management for aggregation and savings enrichment

---

## What it does

**Local Mode** moves recommendation computation from the central Cost Management
infrastructure directly onto the OpenShift cluster. Instead of the current
pipeline (Prometheus → CSV → tar.gz upload → Kafka → S3 → remote processing),
the operator queries Prometheus directly and computes recommendations locally.

Results are:

1. Written to PostgreSQL (in-cluster or external) for local API access
2. Pushed as lightweight JSON to central Cost Management for fleet-wide visibility
   and dollar savings enrichment

---

## Why it matters

### Faster recommendations

The current pipeline introduces 6–12 hours of end-to-end latency (operator upload
cycle + Koku processing + ROS ingestion). Local Mode provides recommendations
within minutes of workload changes.

### Higher data fidelity

Today the operator samples metrics at 15-minute intervals, 4 times per hour
(96 data points per container per day). Local Mode leverages Prometheus
`quantile_over_time()` and `avg_over_time()` over the full-resolution scrape data —
no sampling loss.

### Dramatically simpler pipeline

| Current (7 stages) | Local Mode (2 stages) |
|---------------------|----------------------|
| Prometheus → Operator → CSV → tar.gz → Upload → S3 → Kafka → ROS | Prometheus → Operator (engine) → PostgreSQL |

### Works in disconnected environments

Clusters without external connectivity can compute recommendations locally.
Dollar savings are unavailable without Cost Management access, but CPU/memory
right-sizing, idle detection, and GPU classification all function from
Prometheus data alone.

### 50x bandwidth reduction

Instead of uploading raw metrics (~50 MB/day for 1000 containers), the operator
pushes only pre-computed recommendation JSON (~1 MB/day).

---

## Architecture overview

```
┌─ OpenShift Cluster ───────────────────────────────────────────────────┐
│                                                                        │
│  Prometheus/Thanos ──PromQL──▶ koku-metrics-operator (local-recs mode) │
│                                         │                              │
│  Kubernetes API ────labels───▶          │ writes                       │
│                                         ▼                              │
│                          PostgreSQL (in-cluster or external) ◀─ reads  │
│                                         ▲                         │    │
│                                         │                         │    │
│                                    ros-ocp-api ───────────────────┘    │
│                                         │                              │
│                               Route + Auth (JWT / OAuth proxy)         │
│                                         │                              │
└─────────────────────────────────────────┼──────────────────────────────┘
                                          │
                                UI / CLI / Automation

         Push recommendations JSON ─────────────────────────────────────▶
         (via existing tar.gz upload)
                                                                         │
                                                                         ▼
                                                         Central Cost Management
                                                   (fleet view + $ enrichment + cloud tags)
```

The operator computes recommendations and writes them to PostgreSQL (which
may run in-cluster as a pod or externally as a managed service). A lightweight
API deployment (ros-ocp-api) reads from the same database and serves the same
REST API contract as the central ros-ocp-backend. The push to central Cost
Management enables fleet-wide visibility and dollar savings enrichment.

---

## How it works

### On-cluster (operator)

1. Operator queries Prometheus using existing PromQL patterns
   (same queries as today, results consumed directly instead of written to CSV)
2. Prometheus computes aggregations server-side:
   `quantile_over_time()`, `avg_over_time()`, `max_over_time()`
3. Go engine applies decay weighting and recommendation logic (unchanged algorithms)
4. Recommendations written to PostgreSQL
5. Recommendation JSON pushed to central Cost Management via existing tar.gz upload

### Local API (ros-ocp-api)

A lightweight deployment of the same ros-ocp-backend binary — with ingestion
code stripped out — serves the REST API from the same PostgreSQL the operator
writes to:

- **Same API contract**: identical endpoints, pagination, filtering, and response
  shapes as the central API
- **Same UI**: koku-ui-onprem connects without modification
- **Authentication**: Keycloak/RHBK JWT (same as cost-onprem today) or OpenShift
  OAuth proxy sidecar
- **Footprint**: single read-only pod (~50 Mi memory)

This separation keeps the operator focused on computation + push, while the API
layer handles authentication, query parsing, and response formatting.

### Central (Cost Management)

1. Receives lightweight recommendation JSON (not raw metrics)
2. Enriches with dollar savings using cost model rates
3. Reconciles tags: applies enabled/disabled policy, resolves tag mappings, and
   re-associates cloud tags via OCP-on-cloud correlation
4. Stores in the fleet-wide recommendation database
5. UI displays aggregated view across all clusters

---

## Tag and label reconciliation

Local Mode recommendations carry raw Kubernetes labels from the cluster. When
pushed to central Cost Management, full tag functionality is preserved through
the enrichment step:

### OCP labels (real-time on-cluster)

The operator reads pod and namespace labels directly from the Kubernetes API —
no delay, no sampling. All labels are pushed with each recommendation.
Central applies the **enabled/disabled** tag policy at query time, so only
approved tag keys appear in API responses.

### Tag mappings (resolved centrally)

If an organization maps `ocp:app` to `aws:Application`, the central system
resolves this during enrichment. A recommendation carrying `app=frontend` becomes
filterable by both `filter[tag:app]=frontend` and `filter[tag:Application]=frontend`.
The same tag-mapping mechanism used today applies unchanged.

### Cloud tags (re-associated via OCP-on-cloud correlation)

Cloud provider tags (AWS tags, Azure tags, GCP labels) that exist only on
infrastructure resources are re-associated with recommendations at central.
This uses the existing OCP-on-cloud correlation data (pod → node → cloud
instance → cloud tags) that is already computed for cost attribution.

This requires cloud billing integration to be active — which it already is
for any customer needing dollar savings (the `effective_rates` endpoint
requires cost model rates + cloud costs).

### Disconnected clusters

Bare-metal and air-gapped clusters have no cloud tags by definition. Local Mode
is fully self-contained for tag filtering using Kubernetes labels alone.

---

## Key differences from current architecture

| Aspect | Current (upload mode) | Local Mode |
|--------|----------------------|------------|
| Recommendation freshness | 6–12 hours | Minutes (configurable) |
| Data fidelity | 96 samples/day | Full Prometheus resolution |
| On-cluster footprint | ~50 Mi (operator only) | ~200 Mi (operator + engine + API pod) |
| PostgreSQL | Central only | Dedicated (in-cluster or external managed) |
| Storage per 1000 containers | ~20 GiB (central, digests+samples) | ~70 MB (recs only) |
| Local API access | No (central API only) | Yes (ros-ocp-api reads from same DB) |
| OCP labels | Synced from Koku (hours delay) | Kubernetes API (real-time) |
| Cloud tags | Native (central correlates) | Re-associated at central enrichment |
| Tag mappings | Applied centrally | Applied centrally (same mechanism) |
| Disconnected support | No | Yes (minus dollar savings and cloud tags) |
| Fleet visibility | Native (central computes) | Push to central |
| Dollar savings | Always available | Available after central enrichment |

---

## Who benefits

- **Disconnected / air-gapped clusters** — recommendations without external upload
- **Latency-sensitive environments** — minute-level freshness
- **Single-cluster customers** — no need for full Kafka + S3 + remote ROS stack
- **Bandwidth-constrained environments** — 50x reduction in upload size
- **RHACM users** — Thanos provides unlimited lookback for long-term recommendations

---

## Planned CRD configuration

```yaml
apiVersion: costmanagement-metrics-cfg.openshift.io/v1beta1
kind: CostManagementMetricsConfig
spec:
  local_recommendations:
    enabled: true
    database_secret: "ros-db-credentials"  # Secret with PostgreSQL connection details
    recommendation_cycle: 3600             # seconds between recommendation cycles
    push_to_central: true                  # upload recommendation JSON to Cost Management
    serve_api: true                        # deploy local ros-ocp-api (default: true)
    terms:
      short: 1d
      medium: 7d
      long: 15d
```

When `serve_api` is set to `false`, the operator computes and pushes
recommendations but does not deploy the local ros-ocp-api pod.
Recommendations are still accessible via the central Cost Management API
(if `push_to_central` is enabled) or by querying the PostgreSQL database
directly.

The `database_secret` references a Kubernetes Secret containing PostgreSQL
connection parameters (host, port, dbname, user, password). In a cost-onprem
deployment, this points to the same in-cluster PostgreSQL used by Koku.

The operator runs in **one mode per cluster**: either `upload` (current) or
`local-recommendations` (new). Running both simultaneously is not supported.

---

## Prometheus requirements

- Standard OpenShift monitoring stack (Prometheus or Thanos via RHACM)
- Same RBAC as the existing koku-metrics-operator (no additional permissions)
- Prometheus retention affects long-term recommendations:
  - 15-day retention: short and medium terms fully supported; long term degraded
  - With Thanos (RHACM): unlimited lookback, all terms fully supported
  - Graceful degradation: terms with insufficient data report lower confidence

---

## Business hours and reship

In the current (upload mode) architecture, changing a business-hours schedule
triggers a **reship**: Koku re-publishes historical ROS CSV files from S3 to
Kafka so that ros-ocp-backend can re-ingest them with the new time-of-day
weighting applied. This works because raw CSVs persist in S3 for ~90 days.

Local Mode eliminates the reship mechanism entirely. Instead:

**With Thanos (RHACM):** The operator re-queries Thanos for the full
recommendation window with the new business-hours weighting. Thanos has
unlimited retention, so retroactive re-computation is immediate and complete.
This is strictly superior to the CSV reship approach (faster, higher fidelity,
no S3/Kafka dependency).

**With Prometheus only (~15-day retention):** The operator re-queries Prometheus
for whatever data is within retention. For data beyond the retention window,
the new schedule takes effect **forward-only** — old recommendations gradually
age out as the sliding window advances. After N days (where N = Prometheus
retention), the recommendation window is fully converged to the new schedule.

| Scenario | Retroactive BH re-computation |
|----------|-------------------------------|
| Thanos (RHACM) | Immediate and complete (unlimited lookback) |
| Prometheus only (15d retention) | Last 15 days immediate; older days converge forward-only over 15 days |
| Current architecture (reship) | Full retroactive via S3 CSV re-ingestion (up to 90 days) |

Since business-hours schedules change infrequently (typically once per quarter),
the forward-only convergence period is acceptable for Prometheus-only
deployments. Customers requiring instant retroactive BH re-computation across
the full window should use Thanos via RHACM.

!!! note "Future enhancement"
    If customer demand warrants it, a lightweight daily aggregate table could be
    introduced to store hourly-bucketed percentiles locally, enabling full
    retroactive BH re-computation without Thanos. This would add ~5–10 MB per
    1000 containers per 90-day window.

---

## Limitations (planned)

- Dollar savings require connectivity to central Cost Management (or optional
  `effective_rates` API access for on-cluster computation)
- Cloud tags on recommendations require cloud billing integration to be active
  at central (already required for dollar savings); disconnected clusters have
  OCP labels only
- Cloud tag association may lag by up to 24 hours for newly provisioned nodes
  (same latency as the current architecture — CUR/export data is not real-time)
- Business-hours schedule changes on Prometheus-only clusters (no Thanos) take
  effect forward-only; full convergence after the Prometheus retention period
  (typically 15 days)
- Boxplot API queries Prometheus at request time (not pre-stored)
- A cluster operates in one mode only — not both upload and local simultaneously
- Fleet-wide view requires the push-to-central path to be configured and reachable
