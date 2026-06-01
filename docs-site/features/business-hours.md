# Business Hours Recommendations

!!! info "Quick Facts"
    **What it does:** Produces container and namespace recommendations scoped to configured business hours (e.g., Mon–Fri 09:00–17:00) alongside existing 24/7 **all_hours** results  
    **Data source:** Same ROS usage CSV as container recommendations; hourly samples are weighted by `business_hours_schedules` (timezone, days, start/end, `off_hours_weight`)  
    **Update frequency:** Each ingestion cycle; schedule changes trigger masu `reship_ros` to rebuild historical **business_hours** digests  
    **Plugin:** `container` (priority 10) and `namespace` (priority 90) — business hours is a dual-digest enrichment, not a separate plugin  
    **Settings API:** `GET/PUT/DELETE /api/cost-management/v1/recommendations/openshift/settings/business-hours` (plus cluster and namespace paths)  
    **Recommendations API:** `business_hours` blocks on `GET .../recommendations/openshift` and `GET .../namespaces` when a schedule is enabled and reship is complete  
    **Kill-switch:** `ROS_BUSINESS_HOURS_ENABLED` (default `false`)

**Status:** Implemented (ros-ocp-backend, koku masu `reship_ros`, cost-onprem-chart E2E)

## Overview

Business hours lets administrators configure when a cluster (or namespace) is
considered "in business" — for example Mon–Fri 08:00–17:00 in
`America/New_York`. ROS then produces **two** recommendation perspectives:

| Stream | Meaning |
|--------|---------|
| **all_hours** | Existing 24/7 behavior (unchanged when BH is disabled) |
| **business_hours** | Percentiles computed from in-window samples only (off-hours excluded when `off_hours_weight=0`) |

**Who uses it:** Platform / FinOps admins sizing interactive workloads that
spike during business hours but share nodes with overnight batch jobs.

**Why not Koku cost models?** Business hours are an optimization concern, not
billing. Not every cluster has a cost model; settings live in ros-ocp-backend
alongside snapshot staleness and recommendation terms.

Full design rationale: [features-business-hours.md](features-business-hours.md).

## Architecture

```mermaid
flowchart TD
  Admin[Admin UI / API] -->|PUT schedule| ROSAPI[ros-api Settings]
  ROSAPI --> Sched[(business_hours_schedules)]
  ROSAPI -->|async reship_ros| Masu[koku masu]
  Masu --> S3[(ros-data S3)]
  S3 --> Kafka[hccm.ros.events]
  Kafka --> Processor[ros-processor]
  Processor --> Ingest[ParseAndDigestCSV]
  Sched --> Ingest
  Ingest --> Digests[(daily_*_digests schedule_type)]
  Digests --> Engine[RecommendWorkloadsStreaming]
  Engine --> RecAPI[Recommendations API]
```

1. **Settings API** stores org / cluster / namespace schedules with inheritance.
2. **Schedule change** sets `reship_pending_since` and calls masu `reship_ros`
   to re-list S3 ROS CSVs and republish Kafka messages.
3. **Ingestion** writes dual digests (`schedule_type = all_hours | business_hours`).
4. **Engine** runs twice when BH is enabled — once per stream — and the API
   returns both CPU/memory amounts in Kruize-compatible `amount`/`format` fields.

Key code:

- Settings: [`internal/api/handlers_business_hours_settings.go`](../internal/api/handlers_business_hours_settings.go)
- Schedule eval: [`internal/bhschedule/schedule.go`](../internal/bhschedule/schedule.go)
- Dual digest pipeline: [`internal/ingestion/pipeline_business_hours.go`](../internal/ingestion/pipeline_business_hours.go)
- Reship client: [`internal/reship/service.go`](../internal/reship/service.go)
- Masu endpoint: koku `masu/api/views.py` (`reship_ros`)

## Configuration

### Environment variables (ros-api / ros-processor)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_BUSINESS_HOURS_ENABLED` | `false` | Kill-switch — when `false`, BH routes return 404 and capabilities omit the feature |
| `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | `false` | After max reship retries, transition cluster to `forward_only` (new ingest only, no historical backfill) |

Helm (cost-onprem-chart): set under `ros-api` and `ros-processor` env blocks;
see chart values on branch `feature/business-hours-e2e`.

### Savings estimates (Koku cost data)

Business hours affects **CPU/memory recommendation sizing**, not dollar savings
math. Savings estimates are configured separately:

| Variable | Default | Purpose |
|----------|---------|---------|
| `KOKU_MASU_URL` | `""` | Masu base URL for `GET .../effective_rates/` (required for non-zero savings) |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Kill-switch — `false` skips all Masu cost fetches; savings are `$0` and recommendations include `NotifNoCostData` (code 25) |

For OCP-on-cloud clusters (OCP on AWS/Azure/GCP), `effective_rates` already includes
correlated cloud infrastructure costs in `namespace_aggregates.infrastructure_cost`
when both Koku sources are configured — no ROS-side correlation work is needed.

Plugin coverage, OCP-on-cloud details, currency field, fleet savings summary
(`GET .../savings-summary`), and troubleshooting:
[architecture/cost-integration.md](architecture/cost-integration.md).

### Schedule inheritance

Resolution order: **namespace override → cluster override → org default → disabled**.

Storage impact: enabling BH approximately **doubles** digest row count for
affected scopes. The API returns a warning on org-level PUT.

## API Reference

Base path: `/api/cost-management/v1/recommendations/openshift/settings/business-hours`

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Org default (inherited view) |
| PUT | `/` | Set org default (202 Accepted) |
| DELETE | `/` | Remove org default |
| GET | `/clusters/{cluster_id}` | Effective cluster schedule + `reship_status` |
| PUT | `/clusters/{cluster_id}` | Set cluster override |
| DELETE | `/clusters/{cluster_id}` | Remove cluster override (inherit org) |
| GET/PUT/DELETE | `/clusters/{id}/namespaces/{ns}` | Namespace override |

Capabilities: `GET .../settings/capabilities` → `{ "business_hours": true }`.

### Example PUT (cluster)

```json
{
  "timezone": "America/New_York",
  "schedule": {
    "days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
    "start_time": "08:00",
    "end_time": "17:00"
  },
  "off_hours_weight": 0.0,
  "enabled": true
}
```

### Example GET response (cluster)

```json
{
  "timezone": "America/New_York",
  "schedule": {
    "days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
    "start_time": "08:00",
    "end_time": "17:00"
  },
  "off_hours_weight": 0.0,
  "enabled": true,
  "reship_status": "complete",
  "reship_status_since": null
}
```

`reship_status` values: `complete`, `pending`, `forward_only`.

OpenAPI: `/api/cost-management/v1/openapi.json` (when feature enabled).

## Deployment

### Migration order (ros-ocp-backend)

1. `000066_create_business_hours_schedules`
2. `000067_add_schedule_type_to_digests`
3. `000068_container_usage_samples_pk_workload_type`
4. `000069_add_reship_forward_only_since`

Deploy order: **koku masu** (`reship_ros`) → **ros-ocp-backend** (migrations 066–069) →
**cost-onprem-chart** (Helm values). If ros deploys before koku, the pending-flag
poller retries until masu is available.

### Helm values (cost-onprem)

- `ROS_BUSINESS_HOURS_ENABLED=true` on ros-api (and processor if split)
- Optional: `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK=true` for degraded
  mode after repeated masu failures

No koku-metrics-operator changes required.

## Troubleshooting

| Symptom | Likely cause | Action |
|---------|--------------|--------|
| Settings 404 | Kill-switch off | Set `ROS_BUSINESS_HOURS_ENABLED=true`, restart ros-api |
| `reship_status: pending` stuck | masu down / S3 errors | Check masu logs, `ros_reship_failures_total`; scale masu; poller retries every 60s |
| `reship_status: forward_only` | Retries exhausted with fallback enabled | PUT schedule again to re-arm full reship, or fix masu/S3 root cause |
| BH recommendations missing | Reship not finished | Wait for `reship_pending_since` NULL and dual `schedule_type` rows in DB |
| Only `all_hours` digests | `enabled: false` or no schedule | Verify GET shows `enabled: true` |
| Storage growth | Expected ~2× digests | Documented in PUT warning; prune via DELETE schedule + re-ingest |

Prometheus metrics: `ros_reship_in_progress`, `ros_reship_files_processed`,
`ros_reship_duration_seconds`, `ros_reship_failures_total`.

E2E coverage: `cost-onprem-chart/tests/suites/ros/test_business_hours.py`.
