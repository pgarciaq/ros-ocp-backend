# Query Parameters

ROS-OCP API query parameters support **two equivalent syntaxes** used across the Cost
Management ecosystem:

| Syntax | Used by | Example |
|--------|---------|---------|
| **Flat (ROS legacy)** | koku-ui-ros, IQE plugins | `?project=payments&order_by=project&order_how=asc` |
| **Bracket (Koku-aligned)** | Koku cost report APIs | `?filter[project]=payments&order_by[project]=asc` |

Both are fully supported simultaneously for backward compatibility. Clients may use either
or mix them in one request. For `order_by`, bracket syntax takes precedence when both forms
are present.

**Preferred going forward:** bracket syntax (`filter[…]`, `order_by[…]`, `filter[tag:…]`),
aligned with Koku Cost Management report APIs. Flat syntax remains supported for koku-ui-ros,
IQE plugins, and other legacy clients.

> **TODO (GA):** Decide whether to deprecate flat query syntax or keep both permanently.
> Supporting both doubles the parameter parsing surface and test matrix. See
> [`internal/api/queryparams/queryparams.go`](../../internal/api/queryparams/queryparams.go).

## Filter syntax (bracket)

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

## Filter syntax (flat)

```
GET /api/cost-management/v1/recommendations/openshift/workloads
  ?project=payments,frontend
  &cluster=550e8400-e29b-41d4-a716-446655440000
  &workload_type=deployment
  &tag=team:platform
  &order_by=last_reported&order_how=desc
  &limit=50
  &offset=0
```

| Parameter | Flat (ROS) | Bracket (Koku) | Description |
|-----------|------------|----------------|-------------|
| Project | `project`, `namespace` | `filter[project]` | Namespace (comma-separated OR) |
| Cluster | `cluster`, `cluster_uuid` | `filter[cluster]` | Cluster UUID or alias |
| Workload | `workload` | `filter[workload]` | Workload name |
| Workload type | `workload_type` | `filter[workload_type]` | Kubernetes workload kind |
| Container | `container` | `filter[container]` | Container name |
| Node | `node`, `node_name` | `filter[node]` | Node name (node/GPU endpoints) |
| Term | `term` | `filter[term]` | Recommendation term (`short_term`, `medium_term`, `long_term`) |
| Engine | `engine` | `filter[engine]` | Recommendation engine (`cost`, `performance`) |
| GPU presence | `has_gpu` | `filter[has_gpu]` | GPU presence (`true` / `false`) |
| GPU model | `gpu_model` | `filter[gpu_model]` | GPU model substring match |
| GPU classification | `gpu_classification` | `filter[gpu_classification]` | GPU classification exact match |
| Staleness | `stale` | `filter[stale]` | Staleness filter (`true`, `false`, `only`) |
| Underutilization | `is_underutilized` | `filter[is_underutilized]` | Node underutilization filter ([details](#node-utilization-filters)) |
| Overcommitment | `is_overcommitted` | `filter[is_overcommitted]` | Node CPU overcommit filter ([details](#node-utilization-filters)) |
| Recommendation type | `recommendation_type` | `filter[recommendation_type]` | Recommendation category (PVC/snapshot endpoints) |
| Tag | `tag=key:value` (repeatable) | `filter[tag:<key>]` | Tag key filter (when tags feature enabled) |
| Exact match | — | `filter[exact:<field>]` | Exact match instead of partial |
| Exclude | — | `exclude[<field>]` | Exclude matching values |
| Sort | `order_by` + `order_how` | `order_by[<field>]` | Sort field; value is `asc` or `desc` |
| Pagination | `limit` / `offset` | `limit` / `offset` | Offset pagination |
| Keyset cursor | `after` | `after` | Keyset pagination (container/namespace lists) |
| Date range | `start_date` / `end_date` | `start_date` / `end_date` | Monitoring window (`YYYY-MM-DD`) |

Exact and exclude filters are **bracket-only** (no flat equivalent).

## Node utilization filters

These boolean filters apply to **`GET /api/cost-management/v1/recommendations/openshift/nodes`**
(node consolidation / utilization listings). Accepted values: `true`, `false`, or omit (no filter).

| Parameter | Flat | Bracket | When `true` |
|-----------|------|---------|-------------|
| Underutilization | `is_underutilized` | `filter[is_underutilized]` | Node is classified underutilized: CPU P95 **and** memory P95 are below the underutil threshold (default 30% of allocatable). |
| Overcommitment | `is_overcommitted` | `filter[is_overcommitted]` | Node is classified overcommitted: sum of pod CPU requests divided by allocatable CPU exceeds the overcommit threshold (default **1.5** via `ROS_NODE_OVERCOMMIT_THRESHOLD`). |

```
GET /api/cost-management/v1/recommendations/openshift/nodes?is_underutilized=true
GET /api/cost-management/v1/recommendations/openshift/nodes?filter[is_overcommitted]=true
GET /api/cost-management/v1/recommendations/openshift/nodes?is_underutilized=false&is_overcommitted=false
```

Response objects include `is_underutilized` and `is_overcommitted` booleans (and `cpu_overcommit_ratio`)
matching the stored classification. See [UI integration — node recommendations](../ui-integration-guide.md#3-node-recommendations).

## Tag filtering

Tag filters narrow container recommendation lists by OpenShift labels tracked in Cost
Management. The feature requires **`ROS_TAGS_ENABLED=true`** on the ROS API deployment.
When disabled, tag parameters are ignored.

### Syntax

Both legacy ROS and Koku bracket forms are accepted:

```
# Koku-aligned
GET /api/cost-management/v1/recommendations/openshift
  ?filter[tag:environment]=production,staging
  &filter[tag:team]=platform

# ROS legacy (repeatable)
GET /api/cost-management/v1/recommendations/openshift
  ?tag=environment:production&tag=team:platform
```

| Syntax | Meaning |
|--------|---------|
| `filter[tag:environment]=production` | Exact value match |
| `filter[tag:environment]=prod,staging` | OR across comma-separated values |
| `tag=environment:production` | Legacy exact match (repeat for multiple keys) |
| Multiple tag keys | AND across keys |
| `filter[tag:environment]=*` | Tag key present (any value) |

Value wildcards (e.g. `prod*`) are not supported in v1.

### Deployment mode and tag availability

Which tags are available depends on **`ROS_TAGS_SOURCE`** (see [Configuration → Tag Sync](../configuration.md#tag-sync)):

| Mode | `ROS_TAGS_SOURCE` | Where tags come from | Freshness |
|------|-------------------|----------------------|-----------|
| On-prem (default) | `db` | ROS reads Koku PostgreSQL tables (`reporting_ocptags_values`) at query time | After last Koku OCP summarization |
| SaaS | `api` | Koku pushes to `org_container_keys.resolved_tags` | After summarization + push; up to ~6h if push fails |

**Prerequisites (both modes):**

1. Tag keys enabled in Cost Management **Settings → Tags**.
2. OCP reports ingested with namespace/pod labels from the cluster.
3. `ROS_TAGS_ENABLED=true` on ROS.

**SaaS only:** Koku must run with `ROS_TAGS_SOURCE=api` and successfully push tags before
filters match push-synced data. Check
`GET /internal/tags/status?org_id=<org_id>` for `synced_at`.

Tag filters apply to **container list** endpoints (workloads/containers/GPU lists that
resolve through `org_container_keys`). See [Tag Filtering](../features/tag-filtering.md)
for operator configuration, troubleshooting, and lifecycle scenarios.

## Ordering

**Bracket syntax** (Koku-aligned):

```
?order_by[project]=asc
?order_by[last_reported]=desc
```

**Flat syntax** (ROS legacy):

```
?order_by=project&order_how=asc
?order_by=last_reported&order_how=desc
```

When both are present, bracket syntax wins.

## Savings response format

Dollar savings fields use a structured object (not a bare float). The value is a
**string** with six decimal places; `units` carries the ISO currency code from the
cost model (typically `USD`).

```json
{
  "estimated_monthly_savings": {
    "value": "12.340000",
    "units": "USD"
  }
}
```

| Endpoint / context | JSON field |
|--------------------|------------|
| Container list/detail (`recommendations`) | `recommendations.estimated_monthly_savings` |
| History rows | `estimated_monthly_savings` |
| Fleet savings summary (total and per cluster) | `estimated_monthly_savings` |

Plugin breakdown totals inside `by_plugin` remain numeric floats for aggregation;
the fleet-level total uses the structured object above.

## Authentication

All endpoints require the `x-rh-identity` header. On-prem deployments may adopt **mTLS**
for service accounts in a future release; query syntax will not change.

See also: [OpenAPI specification](../openapi.md), [operations API reference](../../docs/operations/api-query-parameters.md).
