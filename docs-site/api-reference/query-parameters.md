# Query Parameters

ROS-OCP API query parameters follow the same bracket conventions as Koku Cost Management.

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
| `filter[tag:<key>]` | Tag key filter (when tags feature enabled) |
| `filter[exact:<field>]` | Exact match instead of partial |
| `exclude[<field>]` | Exclude matching values |
| `order_by[<field>]` | Sort field; value is `asc` or `desc` |
| `limit` / `offset` | Offset pagination |
| `after` | Keyset cursor (container/namespace lists) |
| `start_date` / `end_date` | Monitoring window (`YYYY-MM-DD`) |

## Backward compatibility

Flat names remain accepted but are deprecated:

| Legacy | Preferred |
|--------|-----------|
| `?project=ns` | `?filter[project]=ns` |
| `?cluster=uuid` | `?filter[cluster]=uuid` |
| `?cluster_uuid=uuid` | `?filter[cluster]=uuid` |
| `?namespace=ns` | `?filter[project]=ns` |
| `?order_by=project&order_how=asc` | `?order_by[project]=asc` |
| `?tag=env:prod` | `?filter[tag:env]=prod` |

## Authentication

All endpoints require the `x-rh-identity` header. On-prem deployments may adopt **mTLS**
for service accounts in a future release; query syntax will not change.

See also: [OpenAPI specification](../openapi.md), [operations API reference](../../docs/operations/api-query-parameters.md).
