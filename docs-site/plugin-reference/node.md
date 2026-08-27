# node

> **Last verified:** 2026-08-06

Package: [`internal/plugins/node`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/node)

**Node right-sizing** — analyzes node-level CPU and memory utilization, recommends capacity targets, and surfaces consolidation opportunities using instance types observed in the fleet.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `node` |
| Phase | 1 (Produce) |
| Priority | 30 |
| CSV types | (none — `IngestHook` after `container`) |
| Retention tables | `daily_node_digests` (partitions), `hourly_node_digests` (date-based DELETE) |

## Traits

| Trait | Supported |
|-------|-----------|
| IngestHook | Yes — extracts node capacity/usage after container CSV |
| APIProvider | Yes — list, detail, utilization, machinesets |
| RetentionProvider | Yes |
| TermProvider | Yes — short/medium/long (max 90 days) |

## What it does

1. After container ingest, upsert node allocatable and P95 usage into `daily_node_digests`.
2. Classify each node (underutilized, overcommitted, stranded CPU/memory, well_utilized).
3. Produce dual-engine recommendations (`cost` at ~80% target, `performance` at ~55%).
4. Compute fleet-aware consolidation (`node_count_reduction`) and optional `suggested_instance_type`.
5. Persist savings at ingestion when cost models and `ROS_SAVINGS_ESTIMATES_ENABLED` are available.

See [Node recommendations](../features/node-recommendations.md).

**Business hours:** node **detail** nests `recommendation_engines.{cost|performance}.business_hours`
when an org or cluster schedule is enabled. List stays all-hours. Code **79**
(`NODE_BH_NOT_PEAK_SAFE`) is a WARNING on the nested object when sizing is present.
Namespace-only schedules do not apply. Sibling `daily_digests_business_hours` is
on detail when Visual Insights is on (same row shape as `daily_digests`; omitted
when empty; never merged). See [Business hours](../features/business-hours.md).

## Permissions

Node recommendation endpoints require the `openshift.node:read` permission.
Callers without this permission receive empty result sets. Callers with no
ROS permissions at all receive HTTP 403.

## History and quality

Node recommendations do **not** have per-node history or quality API endpoints.

- **History** — Use the fleet container history endpoint with `filter[cluster]`, `filter[project]`, `filter[workload]`, or `filter[container]` as needed: `GET /api/cost-management/v1/recommendations/openshift/history`. That API is container-scoped; it does not store node-level recommendation history and has no `filter[node]`.
- **Quality** — Quality metrics (`GET /recommendations/openshift/quality`) are **container-only** (stability, adoption, OOM). They do not apply to node recommendations.

See [Recommendation History & Quality](../features/history-and-quality.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/nodes
GET /api/cost-management/v1/recommendations/openshift/nodes/{node}
GET /api/cost-management/v1/recommendations/openshift/nodes/utilization
GET /api/cost-management/v1/recommendations/openshift/machinesets
```

Handlers: [`internal/plugins/node/plugin.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/node/plugin.go) (`RegisterRoutes`) and node handlers in `internal/api/handlers_node_utilization.go`, `internal/api/handlers_node_detail.go`.

### Hourly utilization (feature-gated)

When both `ROS_VISUAL_INSIGHTS_ENABLED` and `ROS_HOURLY_NODE_DIGESTS_ENABLED` are `true`:

```
GET /api/cost-management/v1/recommendations/openshift/node/:id/hourly-utilization
```

Returns hourly CPU/memory utilization data for a specific node, used by visual-insights dashboards. Not registered when either feature flag is disabled.

## Key features

### Terms

| Term | Default window | Min data |
|------|----------------|----------|
| `short` | 1 day | 1 day |
| `medium` | 7 days | 3 days |
| `long` | 15 days | 7 days |

Filter list/detail with `filter[term]=short_term|medium_term|long_term` (legacy `short|medium|long` accepted) and `filter[engine]=cost|performance`.

### Confidence

Each `recommendation_terms.{short|medium|long}_term` block includes:

| Field | Meaning |
|-------|---------|
| `data_days` | Daily node digests in the term window |
| `confidence_level` | `min(data_days / window_days, 1.0)` — same formula as container recommendations |

When `confidence_level` is below the low-confidence threshold (default **0.5**) and `data_days > 0`, notification code **1** (`NotifLowConfidence`) is included on the engine notifications. When `data_days` is at or below **2** (default sparse-data threshold) and greater than zero, code **77** (`SPARSE_DATA`) is also included — orthogonal to low-confidence (e.g. 1 day in a 1-day window has confidence 1.0 but still emits **77**).

### Tag filtering

| Parameter | Description |
|-----------|-------------|
| `filter[tag:<key>]` | Namespace label filter when `ROS_TAGS_ENABLED=true` (nodes match when any workload namespace on the cluster has the label) |

Syntax: `filter[tag:<key>]=<value>`, comma-separated OR within a key, multiple `filter[tag:*]` keys ANDed. See [Query parameters](query-parameters.md).

### Consolidation and MachineSets

When nodes are underutilized, the engine recommends reducing node count within instance-type (or capacity-based) groups. List rows may include `machineset_name` when present on digests; `GET .../machinesets` aggregates fleet savings by MachineSet.

#### MachineSet aggregation API (`GET .../machinesets`)

Tier 1 **aggregation only** — groups existing `node_recommendations` rows (cost engine,
non-empty `machineset_name`). There is no separate `machineset` engine or
`machineset_recommendations` table yet (Tier 2 planned).

| Filter | Alias | Notes |
|--------|-------|-------|
| `filter[cluster]` | `cluster_uuid` | RBAC-scoped; unknown cluster → empty 200 |
| `filter[machineset_name]` | `machineset_name` | Exact or `*` wildcard (`*` → SQL `LIKE`) |
| `filter[term]` | `term` | `short` / `medium` / `long` (default `medium`) |

**Pagination:** `limit` (default 10), `offset`, or keyset `after` from `meta.next_cursor`
(sort: total monthly savings descending; tie-break: machineset name + cluster). **No**
`order_by` parameter.

**Response (per group):** `machineset_name`, `cluster_uuid`, `cluster_alias`,
`instance_type`, `current_node_count`, `recommended_node_count`, `excess_nodes`,
`total_monthly_savings` (`MoneyAmount`), `avg_cpu_utilization`, `avg_memory_utilization`,
`nodes`. `meta.currency` carries the ISO 4217 code (default `USD`). **No**
`confidence_level` — apply confidence on per-node list/detail instead.

**CSV:** `?format=csv` or `Accept: text/csv` (filename prefix `machineset-recommendations-`).

**Notifications:** MachineSet list rows do not include notification maps. Code **76**
(fleet consolidation) on node list/detail may reference the MachineSet name in its message.

See [MachineSet recommendations (planned)](../planned-features/machineset-recommendations.md) and
[MachineSet recommendations](../planned-features/machineset-recommendations.md) and [Autoscaler optimization](../planned-features/autoscaler-optimization.md) for Tier 2/3 scope.

### Instance type hints

For stranded-resource nodes, responses may include `suggested_instance_type`, `instance_type_reason`, or notification **13** (`suggested_direction`).

## Idle detection

Node utilization list supports idle/zombie filters aligned with workload idle state on the node. See [Idle / zombie detection](idle-detection.md#node-utilization).

## Notification codes

Node-specific codes include consolidation and stranded-resource signals. Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=node`.

See [Notification codes — Nodes](../architecture/notification-codes.md#nodes).

## Savings

Per-node `estimated_monthly_savings` on each `recommendation_engines.{cost|performance}` block (structured `value` + `units`):

- Compares current vs recommended CPU, memory, and monthly node cost from Koku rates.
- Consolidation contributes via `node_count_reduction` (fewer nodes × per-node cost).

When no cost data is available, code **25** applies and savings are `$0` or omitted.

`estimated_monthly_savings.value` can be **negative** when current node capacity is already below the recommended target (upsize for headroom). Display as additional monthly cost, not as a savings opportunity.

Node savings roll into fleet totals: `GET .../savings-summary` → `by_plugin.node`.

See [Savings estimations](../features/savings-estimations.md) and [Cost integration — Node savings](../architecture/cost-integration.md#node-savings-cpumemory-utilization).

## Settings

Per-organization thresholds and term overrides:

```
GET /api/cost-management/v1/recommendations/openshift/settings/node
PUT /api/cost-management/v1/recommendations/openshift/settings/node
DELETE /api/cost-management/v1/recommendations/openshift/settings/node
```

Env locks: `ROS_NODE_*`. See [Configurability](../architecture/configurability.md) (node section).

## Architecture

- [Node recommendations (feature)](../features/node-recommendations.md)
- [Recommendation engines](../architecture/recommendation-engines.md)
- [Cost integration](../architecture/cost-integration.md)
- Internal design: [`docs/features/node-recommendations.md`](../features/node-recommendations.md)
