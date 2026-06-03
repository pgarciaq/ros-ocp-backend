# Resource Optimization Local Mode

Internal reference for the planned on-cluster recommendation computation mode.

**Status:** Future work (not yet implemented).

## Overview

Local Mode moves the recommendation engine from the central ros-ocp-backend
instance into the koku-metrics-operator running on each OpenShift cluster.
The operator queries Prometheus/Thanos directly via PromQL, computes
recommendations using the same Go engine, and writes results to PostgreSQL
(in-cluster or external managed). A lightweight API deployment (ros-ocp-api)
serves recommendations locally. Pre-computed recommendations are also pushed
to central Cost Management for fleet-wide visibility and dollar savings
enrichment.

## Architecture

```
Prometheus/Thanos ──PromQL──▶ koku-metrics-operator (engine embedded)
Kubernetes API ────labels──▶          │
                                      ├──writes──▶ PostgreSQL (in-cluster or external)
                                      │                   ▲
                                      │                   │ reads
                                      │            ros-ocp-api (lightweight, read-only)
                                      │                   │
                                      │            Route + Auth (JWT / OAuth proxy)
                                      │                   │
                                      │            UI / CLI / automation
                                      │
                                      └──push───▶ Central Cost Management
                                                  (fleet view + $ enrichment + cloud tags)
```

## Key design decisions

| Decision | Rationale |
|----------|-----------|
| Eliminate digest/sample tables | Prometheus IS the TSDB; no need to replicate metrics in PostgreSQL |
| PromQL aggregation server-side | `quantile_over_time()`, `avg_over_time()` computed by Prometheus; Go only does decay weighting |
| OCP labels from K8s API directly | Real-time labels, no Koku tag-sync delay; cloud tags re-associated at central |
| PostgreSQL (in-cluster or external) | Recommendations-only schema (~70 MB / 1000 containers); may reuse cost-onprem DB or external managed service |
| Extend existing operator | Reuses Prometheus RBAC, OLM lifecycle, upload mechanism; no new operator |
| Push via existing tar.gz upload | No new transport; recommendation JSON as new file type in existing payload |
| One mode per cluster | Upload OR local-recommendations, never both (avoids duplicate computation) |
| Dollar savings optional on-cluster | Central enrichment after push; or optionally fetch `effective_rates` locally |
| Separate API deployment | Operator computes + pushes; ros-ocp-api serves (clean separation of concerns) |

## Tag and label reconciliation

Tags/labels are a critical cross-cutting concern because the current system
unifies OCP labels, cloud tags, tag mappings, and enable/disable policies
into a single filterable namespace. Local Mode preserves full tag functionality.

### Data flow

```
On-cluster:  K8s API labels ──▶ pushed with recommendation JSON (ALL labels, unfiltered)
                                          │
Central enrichment step:                  ▼
  1. Apply enabled/disabled policy (query-time, no data loss)
  2. Resolve tag mappings (ocp:app → aws:Application aliasing)
  3. Re-associate cloud tags via OCP-on-cloud correlation:
     recommendation.node → cloud instance → cloud tags
  4. Index merged tag set on recommendation_sets
```

### Design decisions

| Decision | Rationale |
|----------|-----------|
| Push ALL raw labels (not just enabled) | Central policy may change; avoids re-push |
| Apply enable/disable at query time | Same mechanism as today; no enrichment-time filtering |
| Cloud tag re-association during enrichment | Reuses existing OCP-on-cloud correlation tables |
| Cloud billing must be active for cloud tags | Already required for `effective_rates` / dollar savings |

### Edge cases

- **New node (< 24h)**: Cloud tag association may lag until next CUR/export arrives.
  Same behavior as current architecture.
- **Disconnected cluster**: No cloud tags (bare-metal). OCP labels fully functional.
- **Tag mapping changes**: Next enrichment cycle picks up new mappings. Previously
  indexed recommendations retain old mapping until re-enriched (acceptable staleness).

### Central-side implementation

The enrichment step (already needed for savings) extends to:

1. Read `pod_labels` from pushed recommendation JSON
2. Look up `reporting_ocp{aws,azure,gcp}_cost_line_item_project_daily_summary_p`
   for cluster + node → resource_id correlation
3. Fetch cloud tags from resource_id's line items
4. Merge OCP labels + cloud tags + resolved mappings → write to `recommendation_sets.tags`
5. Apply enabled-tag filter at API query time (existing `filter[tag:key]=value` logic)

## Business hours without reship

In the current architecture, changing a BH schedule triggers a reship: masu
re-publishes historical CSVs from S3 → Kafka → ros-ocp-backend re-ingests with
new weighting. This works because raw CSVs persist in S3 for ~90 days.

Local Mode eliminates the entire reship mechanism. The replacement depends on
the metrics backend:

### With Thanos (RHACM): immediate full re-computation

Thanos has unlimited retention. On BH change, the next recommendation cycle
re-queries the full window with BH-weighted PromQL. Strictly superior to reship
(faster, higher fidelity, no S3/Kafka/masu dependency).

### With Prometheus only: forward-only convergence

Prometheus typically retains ~15 days. On BH change:
- Days within retention: re-queried with new BH weighting immediately
- Days beyond retention: data is gone; old recommendations age out naturally

Convergence timeline:
- Day 0: long-term (15d) rec = 0 days new + 15 days old schedule
- Day 7: long-term rec = 7 days new + 8 days old
- Day 15: fully converged

This matches the existing `MarkReshipForwardOnly` fallback behavior (triggered
when reship retries are exhausted). In Local Mode it becomes the default for
Prometheus-only deployments.

### Design decision

**Forward-only is acceptable** because:
1. BH schedules change very rarely (once per quarter assumption from design doc)
2. 15-day convergence is invisible to most users
3. Avoids reintroducing digest-style storage tables
4. Customers who need instant retroactive BH should use Thanos

### Future enhancement (if customer demand warrants)

A lightweight daily aggregate table could store hourly-bucketed percentiles
(p50, p95, p99, max per container per hour-of-day per day). On BH change,
re-weight these stored aggregates without raw Prometheus data. Estimated storage:
~5–10 MB per 1000 containers per 90-day window.

This would partially reintroduce what `daily_container_digests` provides today,
but at a fraction of the size (only key percentiles, no full sample arrays).

## What changes in the engine

### Eliminated (not needed in local mode)

- `internal/ingestion/` — CSV parsing, digest computation, pipeline, partition management
- `internal/services/report_processor.go` — Kafka consumer
- `internal/reship/` — S3 CSV re-fetch for business hours (replaced by direct Prometheus re-query)
- All `daily_*_digests` tables and migrations
- `container_usage_samples`, `namespace_usage_samples` tables

### Preserved (unchanged algorithms)

- `internal/engine/recommend_cpu.go`, `recommend_memory.go`
- `internal/engine/decay.go` (input: daily values from Prometheus step query)
- `internal/engine/idle.go`, `gpu_recommender.go`, `recommend_nodes.go`
- `internal/engine/quality.go`, `retention.go`, `savings.go`
- `internal/api/` (serves from PostgreSQL via ros-ocp-api deployment)
- `internal/model/`

### New components

| Component | Purpose |
|-----------|---------|
| `internal/promquery/` | PromQL client implementing `MetricsProvider` interface |
| `internal/k8slabels/` | Kubernetes label reader (replaces tag sync) |
| `internal/pushresults/` | JSON serialization + upload of recommendation results |
| `MetricsProvider` interface | Abstraction allowing engine to work with either digest tables (remote) or Prometheus (local) |

## MetricsProvider interface

```go
type MetricsProvider interface {
    DailyAggregates(ctx context.Context, namespace, workload, container string,
        metric Metric, window time.Duration) ([]DailyValue, error)

    Percentiles(ctx context.Context, namespace, workload, container string,
        metric Metric, window time.Duration, quantiles []float64) (map[float64]float64, error)

    Max(ctx context.Context, namespace, workload, container string,
        metric Metric, window time.Duration) (float64, error)
}
```

Two implementations:
- `RemoteProvider` — reads from `daily_container_digests` (current mode, used by central ROS)
- `PrometheusProvider` — queries Prometheus/Thanos directly (local mode)

## Prometheus query patterns

```promql
# Percentiles (vectorized — returns all containers at once)
quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{...}[5m])[7d:15m])

# Daily aggregates for decay (one value per day, stepped)
avg_over_time(rate(container_cpu_usage_seconds_total{...}[5m])[1d])

# Max memory
max_over_time(container_memory_working_set_bytes{...}[7d])
```

~30-40 vectorized PromQL queries per recommendation cycle (each returns all matching containers). At hourly cadence: negligible Prometheus load.

## Push payload schema

File type: `ros-openshift-recommendations-YYYYMM.json`

```json
{
  "cluster_id": "...",
  "generated_at": "2026-06-03T14:00:00Z",
  "schema_version": "1.0",
  "recommendations": [
    {
      "namespace": "...",
      "workload": "...",
      "container": "...",
      "node": "worker-3",
      "labels": {"app": "frontend", "team": "platform", "version": "v2.1"},
      "terms": { "short_term": {...}, "medium_term": {...}, "long_term": {...} },
      "confidence": 0.85,
      "idle_state": "active",
      "notifications": [...],
      "current_cpu_request_mc": 1000,
      "current_memory_request_kib": 1048576
    }
  ]
}
```

Central handler: UPSERT into `recommendation_sets` + savings enrichment via cost model lookup + tag reconciliation (cloud tags from OCP-on-cloud correlation using `node` field, mapping resolution, enable/disable policy).

## Local API deployment (ros-ocp-api)

Recommendations are served locally via a lightweight deployment of the existing
ros-ocp-backend binary — with all ingestion code stripped (no Kafka consumer,
no CSV parsing, no digest computation). It reads from the same PostgreSQL
that the operator writes to (whether that DB is in-cluster or external).

### What it is

- Same Go binary, same API handlers (`internal/api/`)
- Same REST contract: identical endpoints, pagination, `filter[tag:*]`, response shapes
- Read-only: no write paths, no ingestion, no Kafka dependency
- Single pod, ~50 Mi memory

### Authentication

Two supported mechanisms (configurable via Helm):

| Mechanism | Use case | How |
|-----------|----------|-----|
| Keycloak/RHBK JWT | cost-onprem chart deployments | Same JWT validation as koku-server; token issued by in-cluster Keycloak |
| OpenShift OAuth proxy | Standalone operator deployments | Sidecar container; transparent to the API binary; standard OpenShift pattern |

The koku-ui-onprem frontend connects unchanged — it already sends JWT tokens
to the API via the same proxy configuration.

### Deployment topology

```yaml
# Helm values (cost-onprem chart)
ros:
  api:
    enabled: true  # set false to skip local API deployment
    replicas: 1
    resources:
      requests: { cpu: 100m, memory: 50Mi }
      limits: { memory: 128Mi }
    auth:
      type: jwt  # or "oauth-proxy"
      jwksUrl: "https://keycloak.example.com/realms/cost/protocol/openid-connect/certs"
```

The CRD also exposes `spec.local_recommendations.serve_api` (default `true`).
When set to `false`, the operator does not deploy ros-ocp-api. This is useful
when recommendations are consumed exclusively via the central Cost Management
API (push-only mode) or when querying PostgreSQL directly from custom tooling.

In the cost-onprem chart, this replaces the current full ros-ocp-backend
deployment (which includes ingestion). The operator takes over the computation
role; the API deployment becomes read-only. The PostgreSQL may be the same
in-cluster instance used by Koku, or an external managed service (RDS, Azure
Database, etc.) — the Helm values point to a Secret with connection details.

### What stays in ros-ocp-api vs what moves to the operator

| Component | ros-ocp-api | Operator |
|-----------|-------------|----------|
| `internal/api/` | Yes (serves) | No |
| `internal/engine/` | No | Yes (computes) |
| `internal/ingestion/` | No | No (eliminated) |
| `internal/services/report_processor.go` | No | No (eliminated) |
| `internal/model/` | Yes (reads) | Yes (writes) |
| Database migrations | Yes (runs on startup) | No |

## Central-side changes required

1. New file-type handler in Koku listener for `ros-openshift-recommendations-*.json`
2. Savings enrichment pass (apply cost model rates to compute `estimated_monthly_savings`)
3. Tag reconciliation pass:
   - Store raw OCP labels from pushed JSON
   - Re-associate cloud tags via OCP-on-cloud correlation tables
   - Resolve tag mappings (aliases)
   - Apply enabled/disabled policy at API query time (existing mechanism)
4. `source` column on `recommendation_sets` (`'local'` vs `'central'`) for auditing
5. Optionally: promote `effective_rates` endpoint from masu-internal to public authenticated

## Effort estimate

~12-14 weeks (1 engineer). See the full feasibility analysis in the plan document.

## Public documentation

See [docs-site/features/local-mode.md](../../docs-site/features/local-mode.md) for the customer-facing feature page.
