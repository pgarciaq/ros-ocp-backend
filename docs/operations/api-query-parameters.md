# API Query Parameters

ROS-OCP Backend list and filter endpoints accept **Koku-aligned bracket notation** for
query parameters.

Authentication uses the `x-rh-identity` header today. **Mutual TLS (mTLS)** is the planned
upgrade path for on-prem service-to-service calls; bracket syntax is unchanged under mTLS.

## Filtering

Use `filter[field]` with comma-separated values (OR within the same field):

```
?filter[cluster]=550e8400-e29b-41d4-a716-446655440000
?filter[project]=payments,frontend
?filter[workload]=api-server
?filter[workload_type]=deployment
?filter[container]=web
?filter[term]=medium
?filter[engine]=cost
?filter[tag:environment]=production,staging
?filter[has_gpu]=true
?filter[stale]=only
```

**Field mapping**

| Koku `filter[…]` | ROS internal column / meaning |
|------------------|-------------------------------|
| `project` | Kubernetes namespace |
| `cluster` | Cluster UUID (or alias when partial match applies) |
| `workload` | Workload name |
| `workload_type` | Deployment, StatefulSet, etc. |
| `container` | Container name |
| `node` | Node name (node/GPU endpoints) |
| `tag:<key>` | Resolved cost-management tag (feature-flagged) |
| `has_gpu` | GPU presence (`true` / `false`) |
| `gpu_model` | GPU model substring match |
| `gpu_classification` | GPU classification exact match |
| `stale` | Staleness filter (`true`, `false`, `only`) |
| `is_underutilized` | Node utilization filter |
| `recommendation_type` | Recommendation category (PVC/snapshot endpoints) |

**Exact and exclude modes** (container/namespace list endpoints):

```
?filter[exact:project]=kube-system
?exclude[project]=openshift-*
```

Flat parameter names (e.g. `?project=…`, `?cluster_uuid=…`) are **not** supported.

## Ordering

Sort direction is embedded in the bracket value:

```
?order_by[last_reported]=desc
?order_by[project]=asc
?order_by[cpu_variation_short_cost]=desc
```

The legacy `?order_by=field&order_how=asc|desc` pair is **not** supported.

Allowed `order_by` keys vary by endpoint; see `internal/api/listoptions/list_options.go`.

## Pagination

Unchanged from Koku conventions (no brackets):

```
?limit=10&offset=0
?limit=10&after=<cursor>          # keyset pagination on large container/namespace lists
```

## Date range

Container and namespace lists still use flat date params (not Koku time-scope filters):

```
?start_date=2026-01-01&end_date=2026-01-31
```

Defaults to the current calendar month when omitted.

## Response format

```
?format=csv
Accept: text/csv
```

## Implementation

Parsing lives in [`internal/api/queryparams/queryparams.go`](../../internal/api/queryparams/queryparams.go).
Handlers read filters via `IncludeValues`, `ExactValues`, and `ExcludeValues` on bracket keys only.
