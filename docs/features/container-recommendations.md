# Container Recommendations API

CPU and memory right-sizing for OpenShift workloads. The container plugin is the
default resource on `GET /api/cost-management/v1/recommendations/openshift/`.

Implementation: [`internal/plugins/container/`](../../internal/plugins/container/),
list handlers in [`internal/api/handlers.go`](../../internal/api/handlers.go),
native query assembly in [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go).

---

## List endpoint

```
GET /api/cost-management/v1/recommendations/openshift/
```

Returns a paginated collection of container recommendations. Each row is one
container (cluster + namespace + workload + container name) with nested
`recommendations.recommendation_terms` for short/medium/long windows and cost/
performance engines.

### Response shape

```json
{
  "meta": {
    "count": 42,
    "limit": 20,
    "offset": 0,
    "has_next": true,
    "next_cursor": "<opaque>"
  },
  "data": [ { "id": "...", "container": "...", "recommendations": { ... } } ],
  "links": { "first": "...", "next": "...", "last": "..." }
}
```

List items use the same detail schema as the single-resource endpoint
([`BuildDetailResponse`](../../internal/model/detail_response.go)).

### Pagination

Two modes are supported (see [pagination.md](../../docs-site/pagination.md)):

| Mode | Parameters | Notes |
|------|------------|-------|
| **Offset** | `limit` (1–1000, default 100), `offset` | Standard page navigation via `links.next` |
| **Keyset** | `limit`, `after=<meta.next_cursor>` | Stable iteration; **`offset` is ignored** when `after` is set |

Keyset cursor encoding: [`internal/api/cursor.go`](../../internal/api/cursor.go),
applied in [`applyContainerListCursor`](../../internal/api/handlers_pagination.go).

Example:

```
GET .../recommendations/openshift/?limit=20
GET .../recommendations/openshift/?limit=20&after=<meta.next_cursor>
```

### CSV export

```
GET .../recommendations/openshift/?format=csv
Accept: text/csv
```

Streams one row per container × term × engine. See [`GenerateNativeCSV`](../../internal/api/utils.go).

---

## Detail endpoint

```
GET /api/cost-management/v1/recommendations/openshift/{recommendation-id}
```

Lookup by deterministic UUID v5 derived from
`(cluster_uuid, namespace, workload, workload_type, container_name)` —
see [`NativeContainerID`](../../internal/model/recommendation_set_native.go).

The response includes:

- Per-term CPU/memory requests and limits for **cost** and **performance** engines
- Usage distribution box plots (`plots.plots_data`) when samples exist
- `recommendations.current` — current request/limit amounts
- `recommendations.replicas` — desired/available replica counts when known
- Optional `gpu` block when GPU enrichment applies
- Idle/zombie fields when classified (see [idle-detection.md](idle-detection.md))

---

## Filters

Bracket syntax (`filter[field]`) and legacy flat params are both accepted
([`queryparams`](../../internal/api/queryparams/queryparams.go),
[`MapNativeQueryParameters`](../../internal/api/handlers.go)).

| Filter | Flat alias | Bracket form | Description |
|--------|------------|--------------|-------------|
| Cluster | `cluster`, `cluster_uuid` | `filter[cluster]` | Partial match on alias; exact on UUID |
| Project | `project`, `namespace` | `filter[project]` or `filter[namespace]` | Namespace (partial match); `filter[namespace]` is an alias for `filter[project]` |
| Workload | `workload` | `filter[workload]` | Workload name (partial) |
| Workload type | `workload_type` | `filter[workload_type]` | `daemonset`, `deployment`, `deploymentconfig`, `replicaset`, `replicationcontroller`, `statefulset` |
| Container | `container` | `filter[container]` | Container name (partial) |
| Engine | `engine` | `filter[engine]` | `cost` or `performance` — limits nested `recommendation_engines` |
| Term | `term` | `filter[term]` | `short`/`medium`/`long` or `short_term`/`medium_term`/`long_term` |
| Idle state | — | `filter[idle_state]` | Comma-separated: `active`, `idle`, `zombie` |
| GPU presence | `has_gpu` | `filter[has_gpu]` | `true` / `false` / `1` / `0` |
| GPU model | `gpu_model` | `filter[gpu_model]` | Case-insensitive substring (repeatable, OR) |
| GPU classification | `gpu_classification` | `filter[gpu_classification]` | `idle`, `underutilized`, `compute_bound_underutil`, `memory_bound`, `well_utilized`, `no_profiling` |
| Staleness | `stale` | — | `false` (default), `true`, `only` |
| Tags | `tag=key:value` | `filter[tag:<key>]` | Requires `ROS_TAGS_ENABLED=true` — see [tag-filtering.md](tag-filtering.md) |

Exact and exclude variants: `filter[exact:<field>]`, `exclude[<field>]`.

Date window on `updated_at`: `start_date`, `end_date` (`YYYY-MM-DD`).

---

## Sorting (`order_by`)

Allowed keys are defined in
[`ContainerAllowedOrderBy`](../../internal/api/listoptions/list_options.go):

| Key | Sorts by |
|-----|----------|
| `cluster` | Cluster alias |
| `project` | Namespace |
| `workload_type` | Kubernetes workload kind |
| `workload` | Workload name |
| `container` | Container name |
| `last_reported` | Cluster last reported (default) |
| `cpu_request_current` | Current CPU request |
| `memory_request_current` | Current memory request |
| `cpu_variation_short_cost` | Short-term cost CPU variation (%) |
| `cpu_variation_short_performance` | Short-term performance CPU variation (%) |
| `cpu_variation_medium_cost` | Medium-term cost CPU variation (%) |
| `cpu_variation_medium_performance` | Medium-term performance CPU variation (%) |
| `cpu_variation_long_cost` | Long-term cost CPU variation (%) |
| `cpu_variation_long_performance` | Long-term performance CPU variation (%) |
| `memory_variation_short_cost` | Short-term cost memory variation (%) |
| `memory_variation_short_performance` | Short-term performance memory variation (%) |
| `memory_variation_medium_cost` | Medium-term cost memory variation (%) |
| `memory_variation_medium_performance` | Medium-term performance memory variation (%) |
| `memory_variation_long_cost` | Long-term cost memory variation (%) |
| `memory_variation_long_performance` | Long-term performance memory variation (%) |

Direction: `order_how=asc|desc` or `order_by[field]=asc|desc`. Default order:
`last_reported` descending.

---

## Dual engine: cost vs performance

Each term exposes nested engines under
`recommendations.recommendation_terms.<term>.recommendation_engines`:

| Engine | CPU percentile (typical) | Memory percentile (typical) | Goal |
|--------|--------------------------|----------------------------|------|
| **cost** | P60 | P95 | Maximize savings |
| **performance** | P98 | P100 | Maximize headroom |

Use `filter[engine]=cost` or `filter[engine]=performance` to return only one
engine block per term. Omit the filter to receive both.

Percentile thresholds are configurable via settings (below).

Quality metrics (`GET .../quality`) also support `filter[engine]=cost|performance` and default to
**cost** when the filter is omitted (see [history-and-quality.md](history-and-quality.md)).

---

## Future work

- **UI settings:** Expose cost vs performance percentile configuration in the UI (backend supports via `/settings/container`).
- **UI history/quality:** Wire an engine selector in the frontend to history and quality endpoints.

---

## Settings

Per-organization sizing thresholds:

```
GET    /api/cost-management/v1/recommendations/openshift/settings/container
PUT    /api/cost-management/v1/recommendations/openshift/settings/container
DELETE /api/cost-management/v1/recommendations/openshift/settings/container
```

Handler: [`handlers_threshold_settings.go`](../../internal/api/handlers_threshold_settings.go).

Key fields: `cpu_cost_percentile`, `cpu_perf_percentile`, `mem_cost_percentile`,
`mem_perf_percentile`, `min_margin`, `max_margin`, `limit_multiplier`,
`idle_cpu_threshold_mc`, `idle_mem_threshold_kib`, `locked_fields`.

Term windows (shared across container/namespace/node/gpu where applicable):

```
GET/PUT/DELETE .../settings/terms?recommendation_type=container
```

Business hours (optional nested `business_hours` on engine blocks when a schedule
applies): see [business-hours.md](../architecture/business-hours.md) and
`GET/PUT/DELETE .../settings/business-hours`.

Idle detection thresholds: `GET/PUT/DELETE .../settings/idle-detection`.

---

## History endpoint

Fleet-wide recommendation snapshots (native engine only):

```
GET /api/cost-management/v1/recommendations/openshift/history
```

Handler: [`handlers_history.go`](../../internal/api/handlers_history.go).

Filters: `cluster`, `project`, `workload`, `container`, `term`, `engine`,
`start_date`, `end_date`, `limit`, `offset`, `format=csv`.

Each row represents one container + term + engine snapshot at `recorded_at`.

---

## Quality endpoint

Recommendation effectiveness metrics (stability, adoption, OOM after rec):

```
GET /api/cost-management/v1/recommendations/openshift/quality
```

Handler: [`handlers_quality.go`](../../internal/api/handlers_quality.go).

Filters: `cluster`, `project`, `workload`, `container`, date range, `order_by`,
`format=csv`.

---

## Notification codes

Container plugin codes (catalog filter `filter[plugin]=container`):

**1, 2, 3, 5, 6, 7, 8, 9, 21, 22, 25** — see
[`internal/notifications/catalog.go`](../../internal/notifications/catalog.go) and
[notification-codes.md](../architecture/notification-codes.md).

Catalog API (no auth required):

```
GET /api/cost-management/v1/recommendations/openshift/notification-codes?filter[plugin]=container
```

---

## Savings estimates

When Masu cost model rates are available (`ROS_SAVINGS_ESTIMATES_ENABLED`):

| Field | When present |
|-------|----------------|
| `recommendations.estimated_monthly_savings` | `idle_state` is `active` |
| `estimated_monthly_waste` | `idle_state` is `idle` or `zombie` |

Values use the structured `{ "value": "12.340000", "units": "USD" }` format.
Replica counts multiply total impact on detail responses.

---

## Idle and zombie detection

Containers are classified as `active`, `idle`, or `zombie` during recommendation
generation. Idle workloads expose waste estimates and termination guidance;
rightsizing savings are omitted when not `active`.

See [idle-detection.md](idle-detection.md) for classification rules and
`filter[idle_state]` usage.

> **UI terminology mapping:** The API value `zombie` corresponds to the UI label
> "Abandoned". The API value `active` (or absence of idle classification)
> corresponds to active/healthy containers in the UI.

---

## Related documentation

- [Query parameters (bracket syntax)](../../docs-site/plugin-reference/query-parameters.md)
- [API pagination](../../docs-site/pagination.md)
- [Recommendation engines](../architecture/recommendation-engines.md)
- [Tag filtering](tag-filtering.md)
