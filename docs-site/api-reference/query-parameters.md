# Query Parameters

ROS-OCP API query parameters follow the same bracket conventions as Koku Cost Management.
Only bracket syntax is supported — flat legacy names are not accepted.

## Filter syntax

```
GET /api/cost-management/v1/recommendations/openshift/workloads
  ?filter[project]=payments,frontend
  &filter[cluster]=550e8400-e29b-41d4-a716-446655440000
  &filter[workload_type]=deployment
  &filter[tag:team]=platform
  &order_by[last_reported]=desc
  &limit=50
  &offset=0
```

| Parameter | Description |
|-----------|-------------|
| `filter[project]` | Namespace (comma-separated OR) |
| `filter[cluster]` | Cluster UUID or alias |
| `filter[workload]` | Workload name |
| `filter[workload_type]` | Kubernetes workload kind |
| `filter[container]` | Container name |
| `filter[node]` | Node name (node/GPU endpoints) |
| `filter[term]` | Recommendation term (`short_term`, `medium_term`, `long_term`) |
| `filter[engine]` | Recommendation engine (`cost`, `performance`) |
| `filter[has_gpu]` | GPU presence (`true` / `false`) |
| `filter[gpu_model]` | GPU model substring match |
| `filter[gpu_classification]` | GPU classification exact match |
| `filter[stale]` | Staleness filter (`true`, `false`, `only`) |
| `filter[is_underutilized]` | Node underutilization filter |
| `filter[recommendation_type]` | Recommendation category (PVC/snapshot endpoints) |
| `filter[tag:<key>]` | Tag key filter (when tags feature enabled) |
| `filter[exact:<field>]` | Exact match instead of partial |
| `exclude[<field>]` | Exclude matching values |
| `order_by[<field>]` | Sort field; value is `asc` or `desc` |
| `limit` / `offset` | Offset pagination |
| `after` | Keyset cursor (container/namespace lists) |
| `start_date` / `end_date` | Monitoring window (`YYYY-MM-DD`) |

## Tag filtering

When `ROS_TAGS_ENABLED=true`, filter recommendations by OpenShift labels synced from Koku:

```
GET /api/cost-management/v1/recommendations/openshift
  ?filter[tag:environment]=production,staging
  &filter[tag:team]=platform
```

| Syntax | Meaning |
|--------|---------|
| `filter[tag:environment]=production` | Exact value match |
| `filter[tag:environment]=prod,staging` | OR across comma-separated values |
| Multiple `filter[tag:*]` keys | AND across keys |

Tag filters apply to container list endpoints after Koku pushes resolved namespace tags
to ROS. See [Tag Filtering](features/tag-filtering.md) for sync behavior and freshness.

## Ordering

Only bracket syntax is supported:

```
?order_by[project]=asc
?order_by[last_reported]=desc
```

## Authentication

All endpoints require the `x-rh-identity` header. On-prem deployments may adopt **mTLS**
for service accounts in a future release; query syntax will not change.

See also: [OpenAPI specification](../openapi.md), [operations API reference](../../docs/operations/api-query-parameters.md).
