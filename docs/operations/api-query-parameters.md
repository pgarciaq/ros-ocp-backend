# API Query Parameters

ROS-OCP Backend list and filter endpoints accept **two equivalent query parameter syntaxes**:

1. **Historical ROS syntax (flat)** — used by koku-ui-ros and IQE plugins today
2. **Koku-aligned syntax (bracket)** — matches Cost Management report API conventions

Both are fully supported simultaneously for backward compatibility. Clients may use either
syntax or mix them in the same request. Bracket syntax takes precedence for `order_by` when
both forms are present.

**Preferred going forward:** bracket syntax (`filter[…]`, `order_by[…]`, `filter[tag:…]`),
aligned with Koku Cost Management report APIs. Flat syntax remains supported for koku-ui-ros,
IQE plugins, and other legacy clients.

> **TODO (GA):** Decide whether to deprecate flat query syntax or keep both permanently.
> Supporting both doubles the parameter parsing surface and test matrix. See
> [`internal/api/queryparams/queryparams.go`](../../internal/api/queryparams/queryparams.go).

Authentication uses the `x-rh-identity` header today. **Mutual TLS (mTLS)** is the planned
upgrade path for on-prem service-to-service calls; query syntax is unchanged under mTLS.

## Dual syntax overview

| Concern | Flat (ROS legacy) | Bracket (Koku-aligned) |
|---------|-------------------|------------------------|
| Project filter | `?project=payments` or `?namespace=payments` | `?filter[project]=payments` |
| Cluster filter | `?cluster=<uuid>` or `?cluster_uuid=<uuid>` | `?filter[cluster]=<uuid>` |
| Workload filter | `?workload=api-server` | `?filter[workload]=api-server` |
| Container filter | `?container=web` | `?filter[container]=web` |
| Node filter | `?node=worker-1` or `?node_name=worker-1` | `?filter[node]=worker-1` |
| Sort field | `?order_by=project&order_how=asc` | `?order_by[project]=asc` |
| Tag filter | `?tag=environment:production` | `?filter[tag:environment]=production` |
| Exact match | — (bracket only) | `?filter[exact:project]=kube-system` |
| Exclude | — (bracket only) | `?exclude[project]=openshift-*` |

Repeated values and comma-separated lists work in both forms:

```
?project=alpha&project=beta
?filter[project]=alpha,beta
```

Implementation: [`internal/api/queryparams/queryparams.go`](../../internal/api/queryparams/queryparams.go).

## Filtering (bracket syntax)

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

**Engine filter** (`filter[engine]` or flat `?engine=`): `cost` or `performance` on container,
namespace, node, VM, history, and quality list endpoints. Omitted engine blocks are excluded
from nested `recommendation_engines` on list responses. Fleet rollup uses
`GET .../savings-summary?engine=cost|performance` (default `cost`). See
[validating-native-engine.md](../testing/validating-native-engine.md#dual-engine-testing-cost-vs-performance).

## Filtering (flat syntax)

Equivalent flat parameters:

```
?cluster=550e8400-e29b-41d4-a716-446655440000
?project=payments,frontend
?workload=api-server
?workload_type=deployment
?container=web
?term=medium
?engine=cost
?tag=environment:production
?has_gpu=true
```

**Field mapping**

| Logical field | Flat aliases | Koku `filter[…]` |
|---------------|--------------|------------------|
| `project` | `project`, `namespace` | `filter[project]` |
| `cluster` | `cluster`, `cluster_uuid` | `filter[cluster]` |
| `workload` | `workload` | `filter[workload]` |
| `workload_type` | `workload_type` | `filter[workload_type]` |
| `container` | `container` | `filter[container]` |
| `node` | `node`, `node_name` | `filter[node]` |
| Tags | `tag=key:value` (repeatable) | `filter[tag:<key>]` |
| `has_gpu` | `has_gpu` | `filter[has_gpu]` |
| `gpu_model` | `gpu_model` | `filter[gpu_model]` |
| `gpu_classification` | `gpu_classification` | `filter[gpu_classification]` |
| `stale` | `stale` | `filter[stale]` |
| `is_underutilized` | `is_underutilized` | `filter[is_underutilized]` |
| `is_overcommitted` | `is_overcommitted` | `filter[is_overcommitted]` |
| `recommendation_type` | `recommendation_type` | `filter[recommendation_type]` |

**PVC list filters** (`GET .../recommendations/openshift/pvcs`):

- `filter[term]` — `short`, `medium`, `long` (default `medium`; one row per PVC per term)
- `filter[recommendation_type]` — `oversized`, `near_full`, `orphaned`, `healthy`
- `filter[storageclass]` — exact StorageClass name match
- Flat aliases: `term`, `recommendation_type`, plus `cluster` / `cluster_uuid`, `project` / `namespace`

**PVC list ordering** (flat `order_by` + `order_how`; default `usage_ratio` desc):

- `usage_ratio`, `estimated_monthly_savings` (alias `estimated_monthly_savings_usd`)
- `pvc_name`, `persistentvolumeclaim`, `capacity_bytes`

**PVC detail** (`GET .../recommendations/openshift/pvcs/detail`):

Required identity params (flat or bracket): cluster, namespace, PVC name.

```
?cluster_uuid=<uuid>&namespace=team-a&persistentvolumeclaim=data-pvc
?filter[cluster]=<uuid>&filter[project]=team-a&persistentvolumeclaim=data-pvc
```

Response: `terms` (all configured terms), `historical_usage` (daily digests), `mounted_by`.

**Node utilization filters** (`GET .../recommendations/openshift/nodes`):

- `filter[engine]` — `cost` or `performance` (flat `?engine=`). Limits nested engine blocks per term; default list sort uses the cost engine.
- `is_underutilized` — `true` / `false` / omit. When `true`, only nodes where CPU P95 and memory P95 are below the underutil threshold.
- `is_overcommitted` — `true` / `false` / omit. When `true`, only nodes where pod CPU requests exceed the overcommit threshold × allocatable (default threshold 1.5).
- `filter[idle_state]` — Comma-separated: `active`, `idle`, `zombie` (e.g. `filter[idle_state]=zombie,idle`). Maps to `node_recommendations.idle_state`.

**Exact and exclude modes** (bracket only — container/namespace list endpoints):

```
?filter[exact:project]=kube-system
?exclude[project]=openshift-*
```

## Ordering

**Bracket syntax** (Koku-aligned):

```
?order_by[last_reported]=desc
?order_by[project]=asc
?order_by[cpu_variation_short_cost]=desc
```

**Flat syntax** (ROS legacy):

```
?order_by=project&order_how=asc
?order_by=last_reported&order_how=desc
```

When both are present, bracket syntax wins. Allowed `order_by` keys vary by endpoint;
see `internal/api/listoptions/list_options.go`.

## Tag filtering

Both legacy and Koku tag syntax are accepted when `ROS_TAGS_ENABLED=true`:

```
# ROS legacy (repeatable)
?tag=environment:production&tag=team:platform

# Koku-aligned
?filter[tag:environment]=production,staging
?filter[tag:team]=platform
```

See [Tag Filtering](../features/tag-filtering.md) for deployment modes and prerequisites.

## Pagination

Unchanged from Koku conventions (no brackets):

```
?limit=10&offset=0
?limit=10&after=<cursor>          # keyset pagination on large container/namespace lists
```

## History, quality, and fleet savings

**History** (`GET .../recommendations/openshift/history`):

- `filter[engine]` — `cost` or `performance` (flat `?engine=`). Each row is one container × term × engine snapshot.

**Quality** (`GET .../recommendations/openshift/quality`):

- `filter[engine]` — `cost` or `performance`; defaults to **cost** when omitted.

**Fleet savings** (`GET .../recommendations/openshift/savings-summary`):

- `engine` — `cost` or `performance` (default `cost`). Aggregates container and node savings for the selected engine.
- `term` — `short`, `medium`, or `long` (default `medium`).

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
Handlers read filters via `IncludeValues`, `ExactValues`, and `ExcludeValues`.
Tag filters are merged from `parseTagFiltersFromRequest` in
[`internal/api/utils.go`](../../internal/api/utils.go).
