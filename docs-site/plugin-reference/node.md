# node

Package: [`internal/plugins/node`](../../internal/plugins/node/)

**Node right-sizing** — analyzes node-level CPU and memory utilization, recommends capacity targets, and surfaces consolidation opportunities using instance types observed in the fleet.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `node` |
| Phase | 1 (Produce) |
| Priority | 30 |
| CSV types | (none — `IngestHook` after `container`) |
| Retention tables | `daily_node_digests`, `node_recommendations` |

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

**Business hours:** not applicable. Business-hours weighting applies to container and namespace recommendations only.

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

Handlers: [`internal/plugins/node/plugin.go`](../../internal/plugins/node/plugin.go) (`RegisterRoutes`) and node handlers in `internal/api/handlers_node_utilization.go`, `internal/api/handlers_node_detail.go`.

## Key features

### Terms

| Term | Default window | Min data |
|------|----------------|----------|
| `short` | 1 day | 1 day |
| `medium` | 7 days | 3 days |
| `long` | 15 days | 7 days |

Filter list/detail with `filter[term]=short_term|medium_term|long_term` (legacy `short|medium|long` accepted) and `filter[engine]=cost|performance`.

### Tag filtering

| Parameter | Description |
|-----------|-------------|
| `filter[tag:<key>]` | Namespace label filter when `ROS_TAGS_ENABLED=true` (nodes match when any workload namespace on the cluster has the label) |

Syntax: `filter[tag:<key>]=<value>`, comma-separated OR within a key, multiple `filter[tag:*]` keys ANDed. See [Query parameters](query-parameters.md).

### Consolidation and MachineSets

When nodes are underutilized, the engine recommends reducing node count within instance-type (or capacity-based) groups. List rows may include `machineset_name` when present on digests; `GET .../machinesets` aggregates fleet savings by MachineSet.

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
- Internal design: [`docs/features/node-recommendations.md`](../../docs/features/node-recommendations.md)
