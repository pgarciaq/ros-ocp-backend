# container

> **Last verified:** 2026-08-06

Package: [`internal/plugins/container`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/container)

**Container right-sizing** — analyzes historical CPU and memory usage per workload container and recommends Kubernetes requests and limits for each term × engine (`cost` / `performance`).

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `container` |
| Phase | 1 (Produce) |
| Priority | 10 |
| CSV types | `container` (ROS usage CSV from koku-metrics-operator) |
| Retention tables | `daily_container_digests` (6 months) |

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

See [Container recommendations](../features/container-recommendations.md) and [Recommendation engines](../architecture/recommendation-engines.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift
GET /api/cost-management/v1/recommendations/openshift/{recommendation-id}
```

Legacy aliases: `GET .../openshift/recommendations`, `GET .../detail`.

Handlers: [`GetRecommendationSetListWithFallback`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers.go), detail handlers in `internal/api/`.

### History

Container recommendation history is available via the fleet history endpoint:

```
GET /api/cost-management/v1/recommendations/openshift/history?filter[container]=<name>&filter[cluster]=<id>
```

There is no per-container-ID history sub-resource. Use query filters on the fleet endpoint to retrieve history for a specific container.

See [Recommendation History & Quality](../features/history-and-quality.md#history).

### Quality

Container recommendation quality metrics (stability, adoption, OOM signals):

```
GET /api/cost-management/v1/recommendations/openshift/quality
GET /api/cost-management/v1/recommendations/openshift/quality?filter[engine]=cost
GET /api/cost-management/v1/recommendations/openshift/quality?filter[engine]=performance
```

| Field | Meaning |
|-------|---------|
| `stability_pct` | Change vs prior cycle (**0.0–1.0**; 1.0 = unchanged) |
| `adoption_detected` | Current requests match the prior recommendation within 5% |
| `oom_events_after_rec` | OOM events in the current ingestion batch |
| `recommendation_age_hours` | Hours since the prior recommendation |
| `engine` | `cost` or `performance` |

Default `filter[engine]` is **cost** when omitted. Node, PVC, VM, and namespace plugins do not expose a fleet `/quality` endpoint.

See [Recommendation History & Quality](../features/history-and-quality.md#quality).

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

### VPA dual-advisor validation

Validate container rightsizing confidence **today** with Kubernetes VPA in
`updateMode: Off` — compare VPA `.status.recommendation.containerRecommendations[].target`
against ROS recommended requests. Agreement between the two independent advisors
signals high confidence to apply; divergence warrants investigation before changing
resources.

Works in SaaS, on-prem, and hybrid deployments. No additional ROS configuration.

See [Validating recommendations with VPA](../features/container-recommendations.md#validating-recommendations-with-vpa)
and [VPA Recommendations (planned)](../planned-features/vpa-recommendations.md).

### Business hours

When `ROS_BUSINESS_HOURS_ENABLED=true`, parallel `all_hours` and `business_hours` digest streams produce dual recommendations on detail responses after reship completes.

See [Business hours](business-hours.md) and [Business Hours feature](../features/business-hours.md).

## Notification codes

`filter[plugin]=container` returns codes **1, 2, 3, 5, 6, 7, 8, 9, 21, 22, 25**.

| Code | Name | Severity | Meaning |
|------|------|----------|---------|
| 1 | `LOW_CONFIDENCE` | WARNING | Less than 4 days of data available for this workload |
| 2 | `STALE_DATA` | WARNING | Cluster not reporting within staleness window |
| 3 | `OOM_DETECTED` | CRITICAL | OOM events detected in term window |
| 5 | `IDLE_WORKLOAD` | INFO | Workload uses less than 1% of requested resources |
| 6 | `RECOMMENDATION_APPLIED` | INFO | Current requests match prior recommendation |
| 7 | `NEW_WORKLOAD` | INFO | Less than 24 hours of data — recommendation may be unstable |
| 8 | `ABANDONED_WORKLOAD` | WARNING | Workload has zero usage for more than 72 hours |
| 9 | `MEMORY_TRENDING_UP` | WARNING | Memory usage trend suggests capacity risk within 30 days |
| 21 | `HPA_SATURATED` | WARNING | Reserved — not currently emitted |
| 22 | `HPA_ACTIVE` | INFO | Reserved — not currently emitted |
| 25 | `NO_COST_DATA` | INFO | No cost data available — savings estimate not computed |

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=container`.

See [Notification codes — Containers](../architecture/notification-codes.md#containers-and-idle-detection).

## Savings

Each container recommendation includes `estimated_monthly_savings` computed from the delta between current requests and recommended values:

```
cpu_savings  = (current_cpu_request − recommended_cpu) × cpu_core_cost_per_hour × 730
mem_savings  = (current_mem_request − recommended_mem) × mem_gib_cost_per_hour × 730
total_savings = cpu_savings + mem_savings
```

This is a simplified view for readability. The production formula also applies namespace-level cost model rates, infrastructure and distributed overhead (by `distribution_type`), and multiplies per-container savings by replica count. See [Cost integration](../architecture/cost-integration.md) for the full calculation.

Rates are resolved per-namespace from Koku cost models (infrastructure + model rates, apportioned by distribution type). When multiple replicas exist, savings are multiplied by replica count.

`estimated_monthly_savings.value` can be **negative** when current requests are already below the recommended target (upsize for headroom or reliability). Display as additional monthly cost, not as a savings opportunity.

The API returns all three savings values on list and detail responses:
- `estimated_monthly_savings` — total savings (CPU + memory)
- `cpu_savings` — CPU-only savings breakdown
- `memory_savings` — memory-only savings breakdown

All three use `MoneyAmount` format (`{"value": "12.34", "units": "USD"}`) and are `null` when cost data is unavailable (notification code **25** / `COST_DATA_UNAVAILABLE`). Container savings are included in fleet `savings-summary` totals under `by_plugin.container`.

Savings estimates always use the **`all_hours`** recommendation (the `config` block), not the optional `business_hours` nested engine block. Business-hours schedule changes therefore do not change dollar savings.

See [Savings estimations](../features/savings-estimations.md) and [Cost integration](../architecture/cost-integration.md).

## Settings

Per-organization thresholds, terms, and idle detection via the Settings API (env locks via `ROS_CONTAINER_*`, `ROS_IDLE_*`, etc.):

```
GET /api/cost-management/v1/recommendations/openshift/settings/container
PUT /api/cost-management/v1/recommendations/openshift/settings/container
DELETE /api/cost-management/v1/recommendations/openshift/settings/container
```

See [Configurability](../architecture/configurability.md) (container section). Key notification thresholds:

| JSON field | Default | Env lock | Purpose |
|------------|---------|----------|---------|
| `low_confidence_threshold` | 0.5 | `ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD` | Relative window coverage below this → code **1** |
| `sparse_data_threshold` | 2 | `ROS_CONTAINER_SPARSE_DATA_THRESHOLD` | Absolute `data_days` at or below this → code **77** |

See [SPARSE_DATA vs LOW_CONFIDENCE](../architecture/notification-codes.md#sparse_data-vs-low_confidence).

## Architecture

- [Container recommendations (feature)](../features/container-recommendations.md)
- [Recommendation engines](../architecture/recommendation-engines.md)
- [Recommendation math](../architecture/recommendation-math.md)
- Internal design: [`docs/features/container-recommendations.md`](../features/container-recommendations.md)
