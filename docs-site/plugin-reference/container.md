# container

Package: [`internal/plugins/container`](../../internal/plugins/container/)

**Container right-sizing** — analyzes historical CPU and memory usage per workload container and recommends Kubernetes requests and limits for each term × engine (`cost` / `performance`).

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `container` |
| Phase | 1 (Produce) |
| Priority | 10 |
| CSV types | `container` (ROS usage CSV from koku-metrics-operator) |
| Retention tables | `daily_container_digests`, `container_usage_samples` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — parses `container` CSV type |
| RetentionProvider | Yes — sweeps digest and sample tables |
| TermProvider | Yes — short/medium/long (max 90 days) |

## What it does

1. Ingest hourly pod-level CPU/memory metrics into `daily_container_digests`.
2. Run percentile-based sizing with optional exponential decay on medium/long terms.
3. Persist recommendations per container × term × engine; expose list and detail APIs.
4. Compute dollar savings at ingestion when `ROS_SAVINGS_ESTIMATES_ENABLED=true` and Masu rates are available.

See [Container recommendations](../features/container-recommendations.md) and [Dual engine](../features/dual-engine.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift
GET /api/cost-management/v1/recommendations/openshift/{recommendation-id}
```

Legacy aliases: `GET .../openshift/recommendations`, `GET .../detail`.

Handlers: [`GetRecommendationSetListWithFallback`](../../internal/api/handlers.go), detail handlers in `internal/api/`.

### History

Container recommendation history is available via the fleet history endpoint:

```
GET /api/cost-management/v1/recommendations/openshift/history?filter[container]=<name>&filter[cluster]=<id>
```

There is no per-container-ID history sub-resource. Use query filters on the fleet endpoint to retrieve history for a specific container.

See [Recommendation History & Quality](../features/history-and-quality.md#history).

## Key features

### Engines and terms

| Engine | Behavior |
|--------|----------|
| `cost` | Lower usage percentiles; tighter requests (default for fleet savings) |
| `performance` | Higher percentiles; more headroom |

| Term | Default window | Notes |
|------|----------------|-------|
| `short` | 1 day | Snapshot of recent behavior |
| `medium` | 7 days | Default for list APIs |
| `long` | 15 days | EMA decay on medium/long |

Filter: `?engine=cost|performance`, `?term=short|medium|long`.

### Idle detection

Container rows include `idle_state` (`active`, `idle`, `zombie`), `idle_since`, and `idle_duration_days`. Idle/zombie workloads may surface **100% recoverable** waste via `estimated_monthly_waste` when cost data exists.

List filters: `filter[idle_state]`, `filter[has_gpu]`, `filter[cluster]`, `filter[project]`, `filter[tag:<key>]`.

See [Idle / zombie detection](idle-detection.md).

### Business hours

When `ROS_BUSINESS_HOURS_ENABLED=true`, parallel `all_hours` and `business_hours` digest streams produce dual recommendations on detail responses after reship completes.

See [Business hours](business-hours.md) and [Business Hours feature](../features/business-hours.md).

## Notification codes

Common container codes include **1** (low confidence), **5** / **8** (idle/zombie), **7** (new workload), **9** (memory trending up), and **25** (`NotifNoCostData` when Masu rates are unavailable).

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=container`.

See [Notification codes — Containers](../architecture/notification-codes.md#containers).

## Savings

Each container recommendation includes `estimated_monthly_savings` computed from the delta between current requests and recommended values:

```
cpu_savings  = (current_cpu_request − recommended_cpu) × cpu_core_cost_per_hour × 730
mem_savings  = (current_mem_request − recommended_mem) × mem_gib_cost_per_hour × 730
total_savings = cpu_savings + mem_savings
```

Rates are resolved per-namespace from Koku cost models (infrastructure + model rates, apportioned by distribution type). When multiple replicas exist, savings are multiplied by replica count.

When no cost data is available for a namespace, notification code **25** (`NotifNoCostData`) is set and savings show `$0`. Container savings are included in fleet `savings-summary` totals under `by_plugin.container`.

See [Savings estimations](../features/savings-estimations.md) and [Cost integration](../architecture/cost-integration.md).

## Settings

Per-organization thresholds, terms, and idle detection via the Settings API (env locks via `ROS_CONTAINER_*`, `ROS_IDLE_*`, etc.):

```
GET /api/cost-management/v1/recommendations/openshift/settings/container
PUT /api/cost-management/v1/recommendations/openshift/settings/container
DELETE /api/cost-management/v1/recommendations/openshift/settings/container
```

See [Configurability](../architecture/configurability.md) (container section).

## Architecture

- [Container recommendations (feature)](../features/container-recommendations.md)
- [Recommendation engines](../architecture/recommendation-engines.md)
- [Recommendation math](../architecture/recommendation-math.md)
- Internal design: [`docs/features/container-recommendations.md`](../../docs/features/container-recommendations.md)
