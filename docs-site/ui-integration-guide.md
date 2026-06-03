# ROS UI Integration Guide

Practical API reference for **koku-ui** developers building OpenShift Resource Optimization
(ROS) pages against the native Go engine in `ros-ocp-backend`.

**Authoritative spec:** [`openapi.json`](../openapi.json) at the repository root (also served at
`GET /api/cost-management/v1/recommendations/openshift/openapi.json`).

**Related docs:**

- [Configurability Reference](architecture/configurability.md) — env vars, defaults, tuning by use case
- [Recommendation Engines](architecture/recommendation-engines.md) — algorithm behavior
- [Cost Integration](architecture/cost-integration.md) — savings formulas and currency
- [Business Hours](features/business-hours.md) — schedule design and reship flow

---

## 1. Authentication

### Identity header

All ROS endpoints require the standard Cost Management identity header:

```http
x-rh-identity: <base64-encoded JSON>
```

The middleware extracts `org_id` from the decoded identity and scopes all data to that tenant.
Missing or invalid identity returns `401`:

```json
{ "status": "error", "message": "missing or invalid identity" }
```

Development example (test customer):

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
```

> **On-prem Keycloak:** Store bare numeric `org_id` (e.g. `"1234567"`), not `"org1234567"`.
> Koku prepends `org` to form the tenant schema.

### Base URL

| Deployment | Base path |
|------------|-----------|
| Console / SaaS | `https://<host>/api/cost-management/v1` |
| On-prem (via koku-ui proxy) | `/api/cost-management/v1` |

All paths in this guide are relative to that prefix.

Example:

```http
GET /api/cost-management/v1/recommendations/openshift
x-rh-identity: <token>
```

### RBAC

When `RBAC_ENABLED=true`, list endpoints filter by `openshift.cluster` and `openshift.project`
permissions from the identity. Users with `*` on a resource see all clusters/projects.

### Cache headers

Recommendation responses include `Cache-Control: no-store`. Do not rely on browser or CDN caching.

---

## 2. Recommendation List (Container / Namespace)

### Container recommendations

```http
GET /recommendations/openshift
```

Returns one row per **container** (paginated by distinct containers), with all term/engine
variants nested under `recommendations`.

**Detail:**

```http
GET /recommendations/openshift/{recommendation-id}
```

`recommendation-id` is a deterministic UUID v5 derived from
`cluster_uuid/namespace/workload/workload_type/container`.

### Namespace recommendations

```http
GET /recommendations/openshift/namespaces
GET /recommendations/openshift/namespaces/{recommendation-id}
```

Same nesting pattern; no `container`, `workload`, or `workload_type` fields on namespace rows.

Legacy alias (deprecated): `GET /recommendations/openshift/namespace/{id}`.

### Query parameters (container list)

| Parameter | Description |
|-----------|-------------|
| `cluster` | Cluster UUID **or** cluster alias (substring match). Use `filter[exact:cluster]` for exact alias. Not `cluster_uuid`. |
| `project` | Namespace filter (maps to `workloads.namespace` / `rs.namespace`). Supports `exclude[project]`, `filter[exact:project]`. |
| `workload` | Deployment/StatefulSet/etc. name filter. |
| `workload_type` | One of: `daemonset`, `deployment`, `deploymentconfig`, `replicaset`, `replicationcontroller`, `statefulset`. |
| `container` | Container name filter. |
| `start_date`, `end_date` | `YYYY-MM-DD` date range on `updated_at` (default: current month). |
| `stale` | Staleness filter — see [Stale flag](#stale-flag). |
| `has_gpu` | `true` / `false` — filter containers with GPU recommendations. |
| `gpu_model` | Substring match on GPU model name (repeatable). |
| `gpu_classification` | Exact match: `idle`, `underutilized`, `memory_bound`, `compute_bound_underutil`, `well_utilized`, `no_profiling`, etc. |
| `order_by` | Sort column — see [Sorting](#sorting). |
| `order_how` | `asc` or `desc` (default `desc`). |
| `offset`, `limit` | Pagination (default limit 100, max 1000). |
| `format` | `json` (default) or `csv`. |
| `cpu-unit` | `cores` (default on list) or `millicores`. |
| `memory-unit` | `bytes` (default on list), `MiB`, or `GiB`. |
| `true-units` | When `false` (default), memory/CPU use k8s-style formats in legacy JSON paths. |

> **Note:** Container and namespace list endpoints support `filter[engine]=cost|performance` (and legacy
> flat `?engine=`). When omitted, both **cost** and **performance** engines are returned nested under
> each `recommendation_terms.<term>.recommendation_engines`. CSV export expands to one row per term × engine
> and includes `estimated_monthly_savings` (string `value`) and `currency` columns for **container** list rows
> (from the list row's cost model). Namespace recommendations provide CPU and memory sizing targets only;
> no dollar savings field is included.

Namespace list supports `cluster`, `project`, date range, `stale`, `order_by`, `order_how`,
`offset`, `limit`, and `format=csv`.

### List response envelope

```json
{
  "meta": { "count": 42, "limit": 100, "offset": 0 },
  "links": {
    "first": "...?limit=100&offset=0",
    "previous": "...",
    "next": "...",
    "last": "..."
  },
  "data": [ /* container or namespace objects */ ]
}
```

### Native list item (container)

```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "cluster_alias": "my-cluster",
  "cluster_uuid": "02059694-68ab-4d58-8809-de1e91f1d0e5",
  "container": "app",
  "project": "team-a",
  "workload": "api",
  "workload_type": "deployment",
  "source_id": "12345",
  "last_reported": "2026-05-20T12:00:00Z",
  "replicas": { "min": 2, "max": 3, "avg": 2, "desired": 3, "available": 3, "source": "kube_state_metrics" },
  "recommendations": {
    "estimated_monthly_savings": { "value": "12.340000", "units": "USD" },
    "short_term": {
      "cost": { /* EngineRecommendation */ },
      "performance": { /* EngineRecommendation */ }
    },
    "medium_term": { "cost": {}, "performance": {} },
    "long_term": { "cost": {}, "performance": {} }
  },
  "gpu": {
    "medium_term": {
      "gpu_classification": "underutilized",
      "recommended_gpu_profile": "1g.5gb",
      "gpu_confidence": 0.8,
      "estimated_monthly_gpu_savings": { "value": "45.000000", "units": "USD" },
      "currency": "USD"
    }
  }
}
```

`gpu` is present only when the GPU plugin is enabled and the workload uses GPUs.

### Detail response (Kruize-compatible shape)

Detail endpoints transform flat native fields into the nested structure the existing UI expects:

```json
{
  "recommendations": {
    "current": {
      "requests": { "cpu": { "amount": 0.5, "format": "cores" }, "memory": { "amount": 512, "format": "MiB" } },
      "limits": { }
    },
    "monitoring_end_time": "2026-05-20T12:00:00Z",
    "estimated_monthly_savings": { "value": "12.340000", "units": "USD" },
    "recommendation_terms": {
      "medium_term": {
        "duration_in_hours": 168,
        "plots": { "plots_data": { /* boxplot quartiles */ } },
        "recommendation_engines": {
          "cost": {
            "config": { "requests": { }, "limits": { } },
            "variation": { "requests": { "cpu": { "amount": -15, "format": "percentage" } } },
            "notifications": { "1": { "type": "WARNING", "message": "...", "code": 1 } },
            "business_hours": {
              "requests": { "cpu": { "amount": 0.8, "format": "cores" } },
              "limits": { }
            }
          },
          "performance": { }
        }
      }
    }
  }
}
```

### All-hours vs business-hours

| Perspective | Location | When present |
|-------------|----------|--------------|
| **All hours** (default) | `recommendation_engines.{cost\|performance}.config` | Always |
| **Business hours** | `recommendation_engines.{cost\|performance}.business_hours` | Schedule configured **and** enabled for the workload's namespace; reship complete |

- `business_hours` mirrors the `config` shape (`requests` / `limits` with `amount` + `format`).
- When business-hours data is unavailable, the field is **omitted** (not `null`).
- `business_hours.reason` may explain degraded mode (e.g. reship in progress).

Show both perspectives side-by-side when `business_hours` is present. Default table columns to
**all-hours / cost / medium_term** unless the user selects otherwise.

### Cost vs performance engines

| Engine | Intent | Typical sizing |
|--------|--------|----------------|
| `cost` | Minimize spend | Lower CPU/memory percentiles (e.g. P60 CPU, P95 memory) |
| `performance` | Minimize throttling/OOM risk | Higher percentiles (e.g. P98 CPU, max memory) |

**UI default:** Show **cost** engine first; offer **performance** as a tab or toggle.

Each engine object includes:

| Field | Description |
|-------|-------------|
| `cpu_request_millicores`, `cpu_limit_millicores` | Recommended CPU (list view) |
| `memory_request_kib`, `memory_limit_kib` | Recommended memory (list view) |
| `current_*` | Current requests/limits |
| `variation_*_pct` | Percent change vs current (negative = downsizing) |
| `confidence_level` | 0.0–1.0 — see [Confidence](#confidence-score) |
| `notifications` | Map keyed by code string — see [Notifications](#notifications) |
| `notification_codes` | Raw int16 array (list view) |

### Savings fields

| Field | Scope | Notes |
|-------|-------|-------|
| `estimated_monthly_savings` | Container row only | Structured `{ "value": "12.340000", "units": "USD" }`; cost engine, **medium** term; omitted when `idle_state != active`. Namespace recommendations have no dollar savings field — sizing targets only |
| `estimated_monthly_waste` | Container row | Full terminate opportunity; present when `idle_state` is `idle` or `zombie` |
| `currency` | Row or cluster | ISO currency from Koku cost model (default `USD`; mirrors `units` when present) |
| GPU: `estimated_monthly_gpu_savings` | `gpu.{term}` | MIG/profile savings (structured object) |
| GPU: `estimated_monthly_timeslicing_savings` | `gpu.{term}` | Per-container time-slicing savings (structured object) |

When Koku has no cost model rates, notification code **25** (`no cost data`) is emitted and
savings fields may be absent.

**Negative savings:** The workload needs **more** resources than currently requested. Display as
“needs additional resources” rather than a negative dollar amount.

### Notifications

Notifications use a Kruize-compatible map:

```json
"notifications": {
  "5": {
    "type": "INFO",
    "message": "Workload uses less than 1% of requested resources",
    "code": 5
  }
}
```

See [Notification codes reference](architecture/notification-codes.md) for the complete catalog (codes 1–54).
The table below is a summary; VM codes 37–54 and reserved codes are documented in the full reference.

### Confidence score

`confidence_level` = `min(days_of_data / window_days, 1.0)` for the term's observation window.

| Range | UI treatment |
|-------|--------------|
| ≥ 0.5 | Normal display |
| < 0.5 | Warning badge; notification code **1** may also appear |
| GPU workloads | Additional tiering by profiling days; may be reduced for bursty or no-profiling cases |

### Stale flag

A recommendation is **stale** when the cluster stopped sending metrics beyond
`ROS_STALENESS_THRESHOLD_HOURS` (default 48h). Stale rows get notification code **2**.

| `?stale=` | Behavior |
|-----------|----------|
| omitted or `false` | **Exclude** stale (default) |
| `true` | Include stale **and** fresh |
| `only` | **Only** stale rows |

Detail lookups exclude stale rows (`stale = false` in DB query).

### Idle / zombie detection

List and detail responses include persisted fields (see [Idle / Zombie Detection](features/idle-detection.md)):

| Field | When present | UI guidance |
|-------|--------------|-------------|
| `idle_state` | Always | `active`, `idle`, or `zombie` — prefer over inferring from notification codes alone |
| `estimated_monthly_waste` | `idle` / `zombie` | Full monthly cost if the workload is **terminated** |
| `estimated_monthly_savings` | Usually `active` only | Rightsizing delta — **do not show** for idle/zombie rows (API omits it) |
| `idle_recommendation` | Non-active | `action: "terminate"` → surface **waste**, not savings |
| `idle_duration_days` | Non-active | As-of last recommendation run (±1 day), not live |

**Never sum** `estimated_monthly_savings` + `estimated_monthly_waste`.

Filter idle workloads: `?filter[idle_state]=zombie,idle`. Fleet waste rollup:
`?group_by[idle_state]=*` on savings-summary.

Notification codes **5** (idle) and **8** (abandoned) remain for backward compatibility.

### UI Integration Recommendations

**Container recommendations**

- Show a sortable PatternFly **Table** with columns: container, namespace, cluster, CPU request/limit (current vs recommended), memory request/limit (current vs recommended), and estimated monthly savings.
- Default sort to `last_reported` descending; expose `order_by` for savings and variation columns (`cpu_variation_medium_cost`, etc.).
- Color-code rows with **Badge** plus text labels (never color alone): red for abandoned (code 8), orange for idle (code 5), yellow for over-provisioned (negative variation), green for well-sized.
- Link each row to the detail view (`GET /recommendations/openshift/{id}`) for boxplots, term selection, and notification details.
- Default to **cost** engine and **medium_term**; provide toggles for engine and term on list and detail views.
- Show **confidence_level** as a badge or **Progress** bar; warn when below 0.5 or notification codes 1/7 are present.
- Include a "Show stale" toggle wired to `?stale=only`; default excludes stale rows.
- Support CSV export via `format=csv` for bulk analysis and compliance workflows.
- When `gpu` is present on a row, show a GPU indicator column linking to MIG and time-slicing views.
- Respect RBAC: empty states should explain insufficient permissions, not imply zero recommendations.
- Surface notification badges inline; use tooltips with full `notifications` messages from detail responses.
- Paginate with `offset`/`limit`; show `meta.count` and standard `links` for navigation.

**Namespace recommendations**

- Display as **Card** grids or a **Table** grouped by cluster.
- Show current quota vs recommended quota for CPU and memory (requests and limits where applicable).
- Highlight namespaces with memory growth trends using a trend arrow icon when notification code 9 is present.
- Link each namespace row to a filtered container list (`?project=`) for container-level drill-down.
- Use the same engine/term defaults as the container view for consistency.
- Do not display dollar savings at namespace level — recommendations are CPU/memory sizing targets only (no `estimated_monthly_savings` field).
- Support CSV export and the same stale filter behavior as container recommendations.

**Dual engine (cost vs performance)**

- Provide a segmented control or **Radio** group: "Optimize for cost" / "Optimize for performance".
- Switching engines updates displayed values client-side from nested `recommendations.{term}.{cost|performance}` — no separate API fetch.
- Default to cost engine; persist the user's choice in local storage or user settings.
- When engines diverge significantly (e.g., CPU recommendation differs by >50%), show a subtle **Alert**: "Performance engine recommends 2× more CPU for this workload."
- On detail view, show both engines side-by-side under `recommendation_engines` for the selected term.
- When `business_hours` is present, add tabs: "All hours" and "Business hours" with side-by-side comparison.
- Namespace list/detail: `business_hours` is populated when BH is enabled and reship is complete
  (engine persists `schedule_type=business_hours` rows; see [Namespace recommendations](features/namespace-recommendations.md#business-hours)).
- Negative savings: phrase as "Additional resources needed" with absolute usage delta, not negative currency.

---

## 3. Node Recommendations

### Node CPU/memory utilization

```http
GET /recommendations/openshift/nodes
```

Deprecated alias: `GET /recommendations/openshift/nodes/utilization` (returns `Deprecation: true` header).

#### Query parameters

| Parameter | Description |
|-----------|-------------|
| `cluster_uuid` | Filter by cluster UUID |
| `node` | Filter by node name |
| `term` | `short`, `medium`, or `long` — filters which term rows contribute (response still nests all returned terms) |
| `engine` | `cost` or `performance` — filters engine rows |
| `is_underutilized` | `true` / `false` |
| `is_overcommitted` | `true` / `false` |
| `filter[idle_state]` | Comma-separated: `active`, `idle`, `zombie` (e.g. `filter[idle_state]=zombie,idle`) |
| `order_by` | `node` or `estimated_monthly_savings` (default; alias `estimated_monthly_savings_usd`) |
| `order_how` | `asc` or `desc` (default `desc`) |
| `offset`, `limit` | Pagination (default limit 10, max 1000) |

#### Response structure

One object per node with nested terms and engines:

```json
{
  "meta": { "count": 5, "limit": 10, "offset": 0, "currency": "USD" },
  "data": [
    {
      "node": "worker-1",
      "cluster_uuid": "...",
      "recommendation_type": "cpu_memory_utilization",
      "classification": {
        "is_underutilized": true,
        "is_overcommitted": false,
        "idle_state": "active",
        "stranded_resource": "memory"
      },
      "metrics": {
        "cpu_util_p50": 0.12,
        "cpu_util_p95": 0.28,
        "mem_util_p50": 0.45,
        "mem_util_p95": 0.62
      },
      "pod_count": 42,
      "cpu_overcommit_ratio": 1.1,
      "trend_slope": 0.02,
      "recommendation_terms": {
        "medium_term": {
          "recommendation_engines": {
            "cost": {
              "recommended_cpu_cores": 8.0,
              "recommended_memory_gib": 64.0,
              "node_count_reduction": 1,
              "estimated_monthly_savings": { "value": "500.000000", "units": "USD" },
              "notifications": { },
              "updated_at": "2026-05-20T10:00:00Z"
            },
            "performance": { }
          }
        }
      }
    }
  ],
  "links": { },
  "warnings": []
}
```

#### Classification types

| Signal | Meaning |
|--------|---------|
| `is_underutilized: true` | CPU P95 **and** memory P95 below underutil threshold (default 30% of allocatable) |
| `is_overcommitted: true` | Sum of pod CPU requests exceeds overcommit threshold × allocatable (default 1.5×) |
| `stranded_resource` | `"cpu"` or `"memory"` when CPU/memory utilization is imbalanced (stranded capacity) |
| Neither flag | Effectively **well_utilized** for display purposes |

Notification codes **11** (underutilized), **12** (overcommitted), **13** (stranded), and **15**
(node `idle` or `zombie`) align with these flags. Code **15** reuses the legacy DB name
`NODE_IDLE` (code **15**, constant `NotifNodeIdle`); the message describes node idle/zombie state — not MachineAutoscaler minReplicas.

#### Savings fields

`estimated_monthly_savings` on each engine reflects consolidation / right-sizing opportunity
for that engine profile. `node_count_reduction` is the per-node consolidation hint for that
engine/term. When the operator supplies `instance_type` on ROS container CSV rows, the cost
engine groups nodes by instance type and may assign reduction across several underutilized
nodes in the same group (not a single global binary flag).

### GPU time-slicing (separate endpoint)

!!! warning "Not the same as VM GPU time-slicing"
    **Node time-slicing** (this section): `GET /recommendations/openshift/gpu/timeslicing` —
    physical GPU sharing among containers on a worker node (`recommended_replicas`,
    `nvidia.com/gpu.replicas`).

    **VM GPU time-slicing**: `GET /recommendations/openshift/vms/{id}` only — vGPU profile and
    slice count on the VM `gpu` object (`gpu_timeslice_*`, `recommended_vgpu_profile`,
    notifications **56**–**57**). Configured via `PUT /settings/vm`, not `/settings/thresholds?recommendation_type=gpu`.

Node-level GPU time-slicing is **not** under `/nodes`:

```http
GET /recommendations/openshift/gpu/timeslicing
```

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Filter by cluster UUID (exact match). Aliases: `cluster`, `cluster_uuid` |
| `node_name` | Filter by node |
| `gpu_model` | Filter by GPU model |
| `term` | Term filter |
| `order_by` | `node_name`, `cluster_uuid`, `gpu_model`, `recommended_replicas`, `confidence`, `total_node_savings_usd` |
| `offset`, `limit` | Pagination |

```json
{
  "meta": { "count": 2, "limit": 20, "offset": 0, "total_savings_usd": 1200.0, "currency": "USD" },
  "data": [
    {
      "node_name": "gpu-node-1",
      "cluster_uuid": "...",
      "term": "medium",
      "recommendation_type": "gpu_timeslicing",
      "gpu_model": "NVIDIA-A100",
      "recommended_replicas": 4,
      "savings_per_gpu_usd": 150.0,
      "total_node_savings_usd": 450.0,
      "confidence": 0.65,
      "candidate_containers": [ { "namespace": "ml", "workload": "train", "container": "worker", "classification": "underutilized" } ],
      "impacted_containers": [ ],
      "notification_codes": [36]
    }
  ]
}
```

Link from container GPU data: `time_slicing_node` and `time_slicing_replicas` on container `gpu` objects.

#### Which count to use (summary vs list)

The GPU **summary** and **time-slicing list** both expose counts; they are **not
equivalent**.

```http
GET /recommendations/openshift/gpu
→ { "timeslicing": { "count": N, "link": "..." } }

GET /recommendations/openshift/gpu/timeslicing
→ { "meta": { "count": M }, "data": [ ... ] }
```

| Field | Typical relation | Semantics | UI usage |
|-------|------------------|-----------|----------|
| `timeslicing.count` (**N**) | N ≥ M | Monitored **node×GPU-model** groups with fresh `gpu_container_digests` telemetry (coverage / navigation) | Show the time-slicing **section** or link when N &gt; 0; do **not** use for recommendation badges |
| `meta.count` (**M**) | M ≤ N | Groups that **passed** the time-slicing engine (underutilized majority, safe `recommended_replicas`, no MIG conflict, fresh node, etc.) | **Badges**, notification counts, “N recommendations available” |
| `len(data)` on one page | ≤ M | Rows returned for the current `offset`/`limit` only | Table body; may be &lt; M when paginating |

**Rules for koku-ui:**

1. **Recommendation badge / Optimizations card count** → `meta.count` from
   `GET .../gpu/timeslicing` (or count containers with notification **36**).
2. **Section visibility** (“Time-slicing” tab or card) → `timeslicing.count`
   from `GET .../gpu` (fleet has GPU ROS coverage).
3. **Never** display copy like “`timeslicing.count` recommendations” — users
   will see fewer rows than the badge implies.
4. **Empty list, non-zero summary** → copy such as: “GPU usage is monitored on
   this fleet, but no nodes currently qualify for time-slicing” (well-utilized,
   MIG, or threshold gates).

Operational prerequisites (DCGM, namespace labels, cost model rates, plugin
enablement): [GPU time-slicing — Prerequisites](features/gpu-time-slicing.md#prerequisites).

### UI Integration Recommendations

**Node CPU/memory utilization**

- Add a dashboard widget showing fleet health: X underutilized, Y overcommitted, Z well-utilized (derive from classification flags).
- Show each node in a sortable **Table** with current vs recommended CPU/memory utilization and savings.
- Display `node_count_reduction` prominently on cost-engine rows; sum reductions by
  `instance_type` when explaining fleet consolidation opportunity.
- Add `filter[idle_state]=idle` or `zombie` tabs for decommissioning workflows; show
  `classification.idle_state` with badges (active / idle / zombie).
- Include per-node `estimated_monthly_savings` and cluster-level consolidation summary in a **Card** header.
- Provide engine toggle (`?engine=cost|performance`) and term selector; values update from nested `recommendation_engines`.
- Use **Badge** for classification: underutilized (info), overcommitted (warning), stranded resource (info + tooltip on `stranded_resource`).
- Show notification codes 11–13 inline with accessible text labels matching badge colors.
- Link node rows to pod/workload views filtered by node where available.
- When cost and performance engines diverge on consolidation, show a callout comparing recommended node counts.

**GPU time-slicing**

- Use **`meta.count`** from the list endpoint for recommendation badges; use
  **`timeslicing.count`** from the summary endpoint only to decide whether to
  show the time-slicing navigation entry (see [Which count to use](#which-count-to-use-summary-vs-list)).
- Show a node-level view with GPU utilization **ProgressBar** per GPU model.
- Display `recommended_replicas` prominently as the primary action metric.
- Compare current vs recommended time-slicing configuration in a side-by-side layout.
- Show `total_node_savings_usd` and `savings_per_gpu_usd` with currency from `meta.currency`.
- Expand `candidate_containers` and `impacted_containers` in a nested **Table** with classification badges.
- Link from container GPU fields (`time_slicing_node`, `time_slicing_replicas`) to the filtered time-slicing list.
- Show confidence as a badge; surface notification code 36 with link to this view from container rows.
- Sort by `total_node_savings_usd` descending by default to prioritize highest-impact nodes.

---

## 4. PVC Recommendations

```http
GET /recommendations/openshift/pvcs
GET /recommendations/openshift/pvcs/detail
```

### Query parameters (list)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `filter[cluster]` | — | Cluster UUID (`cluster`, `cluster_uuid`) |
| `filter[project]` | — | Namespace (`namespace`, `project`) |
| `filter[recommendation_type]` | — | `oversized`, `near_full`, `orphaned`, `healthy` |
| `filter[term]` | `medium` | `short`, `medium`, or `long` |
| `filter[storageclass]` | — | Storage tier / class name (e.g. `gp3-csi`, `ocs-storagecluster-ceph-rbd`) |
| `filter[tag:<key>]` | — | Tag filter when tags are enabled |
| `order_by` | `usage_ratio` | `usage_ratio`, `estimated_monthly_savings`, `pvc_name`, `capacity_bytes` |
| `order_how` | `desc` | `asc` or `desc` |
| `offset`, `limit` | 0, 20 | Max limit 100 |

### Query parameters (detail)

Required: `cluster_uuid`, `namespace`, `persistentvolumeclaim` (flat or
`filter[cluster]` / `filter[project]`). Returns all terms plus `historical_usage`
for charts. Use for row drill-down from the list table.

### Response structure

```json
{
  "meta": { "count": 10, "limit": 20, "offset": 0, "currency": "USD" },
  "links": { },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "team-a",
      "persistentvolumeclaim": "data-pvc",
      "mounted_by": "virt-launcher-data-pvc-abc12",
      "vm_name": "data-pvc",
      "persistentvolume": "pv-abc",
      "storageclass": "gp3",
      "capacity_bytes": 107374182400,
      "usage_bytes_max": 10737418240,
      "usage_ratio": 0.10,
      "recommendation_type": "oversized",
      "recommended_bytes": 21474836480,
      "days_to_full": null,
      "growth_bytes_per_day": 1048576,
      "estimated_monthly_savings": { "value": "8.500000", "units": "USD" },
      "notifications": { "29": { "type": "INFO", "message": "...", "code": 29 } },
      "data_days": 14,
      "term": "medium",
      "resize_note": "Kubernetes does not support in-place PVC shrinking..."
    }
  ]
}
```

### Classification types

| Type | Condition (defaults) | UI emphasis |
|------|----------------------|-------------|
| `oversized` | Usage/capacity < 20% for 3+ days | Show `recommended_bytes`; display `resize_note` |
| `near_full` | Usage/capacity > 85% **or** projected full within 30 days | Urgent badge |
| `orphaned` | Zero usage for 3+ days | Deletion candidate; show `resize_note` |
| `healthy` | Between thresholds | Informational only; typically omit from optimization views |

### Savings fields

`estimated_monthly_savings` = savings from reducing provisioned capacity to `recommended_bytes`
(using Koku storage rates or fallback; structured `value` + `units`).

### Growth trend and days-to-full

| Field | Description |
|-------|-------------|
| `growth_bytes_per_day` | Linear regression slope on daily average usage |
| `days_to_full` | Projected days until capacity exhausted at current growth rate; `null` if not applicable |

Requires minimum trend data (default 7 days). Near-full alerts can fire on projection even when
current usage is below 85%.

### UI Integration Recommendations

- Show PVC recommendations in a sortable **Table**: PVC name, namespace, cluster, capacity, usage ratio, recommendation type, savings. Wire column sort to `order_by` + `order_how` (server-side).
- Show `mounted_by` as workload context (e.g. "Mounted by: virt-launcher-…") when non-empty; omit when empty.
- Show `vm_name` when present (authoritative KubeVirt VM from operator storage CSV); link to VM recommendation detail when integrating VM views.
- Add a **Storage class** filter dropdown bound to `filter[storageclass]` for tier-specific views.
- Open detail on row click via `GET .../pvcs/detail` — render `terms` side-by-side and `historical_usage` as a usage-over-time chart.
- Render `usage_ratio` as a **ProgressBar** showing current usage vs capacity with accessible text (e.g., "10% used").
- When `growth_bytes_per_day` and `days_to_full` are available, show a growth projection line or "full in N days" callout.
- Use **Badge** for recommendation type: oversized (shrink), near_full (grow, urgent styling), orphaned (delete), healthy (omit from optimization views).
- Always display `resize_note` in an **Alert** for oversized and orphaned PVCs — Kubernetes cannot shrink PVCs in place.
- Show `recommended_bytes` alongside `capacity_bytes` with human-readable units (GiB).
- Handle negative or zero savings gracefully; near-full rows prioritize capacity risk over cost savings.
- Filter by `recommendation_type` via tabs or a filter toolbar wired to query params.
- Surface notification codes 20, 29, 30 inline with severity-appropriate badges.
- Link PVC rows to namespace and cluster context; group by namespace in fleet views when helpful.
- Default term to `medium`; expose term selector when comparing short vs long observation windows.
- **No koku-ui PVC view yet** — API-only until UI catches up; see [known issues](known-issues.md#pvc--storage-rightsizing-req-63).

---

## 4b. ResourceQuota and ClusterResourceQuota Recommendations

Namespace **ResourceQuota** and OpenShift **ClusterResourceQuota** recommendations are
**API-ready**; there is **no dedicated koku-ui view yet** (deferred — see
[Deferred: Quota UI](known-issues.md#deferred-quota-ui) and
[quota feature roadmap](features/quota-recommendations.md#roadmap--future-work)).

### Namespace ResourceQuota

```http
GET /recommendations/openshift/quota
GET /recommendations/openshift/quota/detail?cluster_uuid=...&namespace=...
```

List rows include `recommendation_type` (`tighten`, `raise`, `optimal`), `risk_level`,
`utilization`, `quota_hard` / `quota_used` / `quota_recommended`, `capacity_freed`, and
`estimated_savings` on tighten. Use `order_by`, `order_how`, and `group_by[cluster]` /
`group_by[project]` per OpenAPI.

Detail adds notification codes **70–72** and `history[]` for trend charts when UI ships.

### ClusterResourceQuota

```http
GET /recommendations/openshift/cluster-quota
```

Same classification pattern at CRQ scope; notification code **73** for cluster-quota rows.

### Planned UI (future work)

- Sortable quota list: utilization, risk, savings, recommendation type
- Detail drawer/page with hard/used/recommended breakdown and history sparkline
- CRQ aggregate table across clusters
- Inline badges for notification codes **70–73**

---

## 5. Snapshot Recommendations

```http
GET /recommendations/openshift/snapshots
```

### Query parameters

| Parameter | Description |
|-----------|-------------|
| `cluster_uuid` | Filter by cluster |
| `namespace` | Filter by namespace |
| `recommendation_type` | Classification filter — see below |
| `offset`, `limit` | Pagination (default limit 20, max 100) |

### Response structure

```json
{
  "meta": { "count": 3, "limit": 20, "offset": 0, "currency": "USD" },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "team-a",
      "snapshot_name": "snap-data-20260101",
      "source_pvc_name": "data-pvc",
      "volume_snapshot_class": "csi-snapclass",
      "storageclass": "gp3",
      "creation_timestamp": "2026-01-01T00:00:00Z",
      "restore_size_bytes": 10737418240,
      "age_days": 120,
      "source_pvc_exists": false,
      "restored_pvc_count": 0,
      "managed_by": "velero",
      "recommendation_type": "orphaned",
      "estimated_monthly_cost_usd": 0.52,
      "notifications": { "31": { "type": "WARNING", "message": "...", "code": 31 } }
    }
  ]
}
```

### Classification types

| Type | Meaning |
|------|---------|
| `orphaned` | Source PVC deleted and age > orphan threshold (default 7 days) |
| `never_restored` | Zero restores and age > never-restored threshold (default 30 days) |
| `stale` | Age > stale threshold (default 90 days), never restored, not managed |
| `redundant` | More than N snapshots per PVC; this one is outside the N most recent |
| `managed` | Backup-tool labels detected (Velero, OADP, etc.) — review retention, not auto-delete |
| `active` | Recent or has restore history — no action |

### Cost estimation fields

`estimated_monthly_cost_usd` = `restore_size_bytes` × storage rate (from Koku effective rates,
tenant override, or default $0.05/GiB/month). This is **ongoing cost**, not savings — sum for
waste dashboards.

### UI Integration Recommendations

- Show snapshots in a sortable **Table**: snapshot name, namespace, cluster, age (`age_days`), classification, source PVC exists, estimated cost.
- Use **Badge** for classification with text labels: orphaned (red), stale (orange), never_restored (yellow), redundant (gray), managed (green), active (green/info).
- Display `estimated_monthly_cost_usd` as ongoing waste cost (not savings); sum for waste dashboard totals.
- Show `source_pvc_exists: false` with a warning icon and code 31 notification text.
- Provide action buttons: "Delete" for orphaned/stale (with confirmation modal), "Verify" for never_restored (link to restore history).
- For `managed` snapshots (Velero/OADP), show caution **Alert** — review retention policy before deletion.
- Link to snapshot settings (`GET /settings/snapshot`) for configuring staleness thresholds.
- Filter by `recommendation_type` and cluster/namespace; default view excludes `active` snapshots.
- Show `restore_size_bytes` and `storageclass` for cost context.
- Include `creation_timestamp` and `restored_pvc_count` in detail tooltips or expandable rows.
- Aggregate waste cost at namespace and cluster level for executive summary cards.

---

## 6. Savings Summary

```http
GET /recommendations/openshift/savings-summary
```

Fleet-wide aggregated savings for dashboard hero metrics.

### Query parameters

| Parameter | Values | Default | Notes |
|-----------|--------|---------|-------|
| `term` | `short`, `medium`, `long` | `medium` | Recommendation horizon |
| `engine` | `cost`, `performance` | `cost` | Optimization engine |
| `group_by[tag:<key>]` | `*` | — | Break down by tag value |
| `group_by[idle_state]` | `*` | — | Break down by idle/active/zombie |
| `filter[cluster]` | UUID | — | Scope results (only with `group_by`) |

### Response structure

```json
{
  "currency": "USD",
  "estimated_monthly_savings": { "value": "12500.750000", "units": "USD" },
  "by_cluster": [
    {
      "cluster_uuid": "...",
      "cluster_alias": "prod-east",
      "estimated_monthly_savings": { "value": "8200.500000", "units": "USD" },
      "has_cost_data": true
    }
  ],
  "by_plugin": {
    "container": 5000.0,
    "gpu": 0.0,
    "node": 3000.0,
    "pvc": 1500.0,
    "snapshot": 500.0
  },
  "gpu_savings_note": "GPU savings are computed at API read time and are not included in this fleet summary..."
}
```

### What `has_cost_data` means

Per cluster in `by_cluster`:

- `true` — At least one recommendation exists **and** not all of them have notification code **25** (no cost data).
- `false` — All recommendations lack cost model rates, or no cost-bearing recs exist.

When `has_cost_data` is false, show “cost model not configured” instead of `$0.00`.

**Note:** `by_plugin.gpu` is always `0` in this summary; use GPU-specific endpoints for GPU dollar estimates.
`snapshot` sums **cost** (waste), not savings.

See [Section 15 — Fleet Summary](#15-fleet-summary) for idle/abandoned container counts.

### UI Integration Recommendations

- Show a dashboard **Card** hero metric: "Estimated monthly savings: {amount}" using `estimated_monthly_savings.value` and `estimated_monthly_savings.units` (or top-level `currency`) from the response.
- Break down savings by plugin in a pie chart or bar chart using `by_plugin` (container, node, pvc, snapshot).
- Display per-cluster savings in a **Table** within the fleet view using `by_cluster` rows.
- Wire engine toggle to `?engine=cost|performance`; all totals and breakdowns update on fetch.
- When `has_cost_data` is false for a cluster, show "Cost model not configured" instead of `$0.00`.
- Handle negative aggregate savings as "Additional investment needed: {amount}" with info styling (blue), not green savings styling.
- Never hardcode `$`; format currency using the `currency` field and user locale.
- Include a disclaimer **Tooltip** on savings figures: "Based on current cost model rates."
- Note that `by_plugin.gpu` is always `0` here — link to GPU MIG and time-slicing views for GPU dollar estimates.
- Treat `by_plugin.snapshot` as waste cost, not savings; label accordingly in the breakdown chart.
- Show `gpu_savings_note` text when present so users understand GPU exclusion from fleet totals.
- Pair with fleet-summary counts (Section 15) for idle/abandoned container context on the same dashboard.

---

## 7. Settings: Terms

Control observation windows per recommendation plugin.

```http
GET    /recommendations/openshift/settings/terms?recommendation_type={plugin}
PUT    /recommendations/openshift/settings/terms?recommendation_type={plugin}
DELETE /recommendations/openshift/settings/terms?recommendation_type={plugin}
```

### Valid `recommendation_type` values

Plugins implementing `TermProvider`: `container`, `namespace`, `node`, `gpu`, `pvc`.

### What terms control

| Field | Purpose |
|-------|---------|
| `window_days` | Observation window length |
| `min_data_days` | Minimum days of data required before emitting recommendations |
| `decay_halflife_hours` | Exponential decay for weighting recent data (0 = no decay) |

Three named terms: `short`, `medium`, `long` (1–3 items on PUT).

### GET response

```json
{
  "recommendation_type": "container",
  "terms": [
    {
      "name": "short",
      "window_days": 1,
      "min_data_days": 1,
      "decay_halflife_hours": 0,
      "locked": false,
      "is_default": true
    },
    {
      "name": "medium",
      "window_days": 7,
      "min_data_days": 3,
      "decay_halflife_hours": 168,
      "locked": false,
      "is_default": true
    }
  ]
}
```

### Locked fields

When an admin sets `ROS_TERMS_{PLUGIN}_{TERM}_{FIELD}` env vars, the corresponding term shows
`locked: true`. PUT returns `422` with `locked_terms` if a locked term is modified.

### Effect on recommendations

Changes apply on the **next ingestion/recommendation cycle** (not retroactive to cached API responses).
Call `DELETE` to reset to compiled defaults.

Default max windows: 90 days (container/namespace/node/gpu), 365 days (pvc).

### UI Integration Recommendations

- Show term settings in a **Form** grouped by plugin (`recommendation_type` selector: container, namespace, node, gpu, pvc).
- Display three term rows (short, medium, long) with fields: `window_days`, `min_data_days`, `decay_halflife_hours`.
- Indicate `locked: true` terms with disabled fields and tooltip: "Set by administrator."
- Show `is_default: true` badge when tenant has not customized; offer "Reset to defaults" via DELETE.
- Validate client-side before PUT: window_days within plugin max (90 days for most, 365 for pvc).
- On PUT success, show toast: "Term settings updated. Changes apply on the next ingestion cycle."
- On 422 with `locked_terms`, highlight locked rows and show inline error messages.
- Hide term settings for plugins where `supports_terms: false` (e.g., snapshot) per capabilities.
- Explain the relationship between `min_data_days` and confidence badges in recommendation lists.
- Link to [Recommendation Engines](architecture/recommendation-engines.md) for algorithm context.

---

## 8. Settings: Thresholds

Per-tenant sizing and classification thresholds (native engine only).

```http
GET    /recommendations/openshift/settings/{container|namespace|node|gpu|pvc}
PUT    /recommendations/openshift/settings/{container|namespace|node|gpu|pvc}
DELETE /recommendations/openshift/settings/{container|namespace|node|gpu|pvc}

Deprecated alias (returns `Deprecation: true` and `Link` successor header):

GET    /recommendations/openshift/settings/thresholds?recommendation_type={type}
PUT    /recommendations/openshift/settings/thresholds?recommendation_type={type}
DELETE /recommendations/openshift/settings/thresholds?recommendation_type={type}
GET    /recommendations/openshift/settings/idle-detection
PUT    /recommendations/openshift/settings/idle-detection
DELETE /recommendations/openshift/settings/idle-detection
```

`recommendation_type`: `container`, `namespace`, `node`, `gpu`, `pvc`.

GET returns merged settings plus `locked_fields` (admin env overrides).
PUT accepts partial JSON; validates ranges before save. On success, ROS triggers **async
recalculation** for all clusters in the org using existing digest data—recommendations
reflect the new thresholds within seconds without waiting for the next ingestion cycle.
DELETE resets tenant overrides to defaults.
Locked PUT attempts return `403` with `locked_fields`.

### Container / namespace fields

| Field | Default | Validation | Description |
|-------|---------|------------|-------------|
| `cpu_cost_percentile` | 0.60 | 0.01–1.0 | Cost engine CPU percentile |
| `cpu_perf_percentile` | 0.98 | 0.01–1.0 | Performance engine CPU percentile |
| `mem_cost_percentile` | 0.95 | 0.01–1.0 | Cost engine memory percentile |
| `mem_perf_percentile` | 1.0 | 0.01–1.0 | Performance engine memory percentile |
| `min_margin` | 1.15 | 1.0–3.0 | Adaptive margin floor |
| `max_margin` | 1.50 | 1.0–3.0 | Adaptive margin ceiling (must be ≥ min_margin) |
| `limit_multiplier` | 1.05 | 1.0–5.0 | limit = request × multiplier |
| `cpu_floor_mc` | 25 | 1–1000 | Minimum CPU request (millicores) |
| `idle_cpu_threshold_mc` | 10 | 1–10000 | Max CPU for idle detection |
| `idle_mem_threshold_kib` | 10240 | 1–10485760 | Max memory for idle detection |
| `mem_trend_slope_threshold` | 100 (container), 500 (namespace) | 1–1000000 | KiB/day trend alert |
| `low_confidence_threshold` | 0.5 | 0.01–1.0 | Below → low-confidence notification |

Namespace uses the same field set with a higher default `mem_trend_slope_threshold`.

### Node fields

| Field | Default | Validation |
|-------|---------|------------|
| `underutil_threshold` | 0.30 | 0.01–0.99 |
| `overcommit_threshold` | 1.50 | 1.0–10.0 |
| `allocatable_factor` | 0.93 | 0.5–1.0 |
| `stranded_imbalance_threshold` | 0.60 | 0.1–1.0 |
| `ema_alpha` | 0.30 | 0.01–1.0 |
| `cost_target_utilization` | 0.80 | 0.1–0.99 |
| `perf_target_utilization` | 0.55 | 0.1–0.99 |
| `perf_consolidation_headroom_multiplier` | 2.0 | 1.0–10.0 |
| `trend_min_days` | 3 | 1–30 |

### PVC fields

| Field | Default | Validation |
|-------|---------|------------|
| `oversized_threshold` | 0.20 | 0.01–0.99 (must be < near_full) |
| `near_full_threshold` | 0.85 | 0.01–0.99 |
| `min_trend_days` | 2 | 1–365 |
| `recommended_size_multiplier` | 2 | 1–10 |
| `min_recommended_gib` | 1 | 1–10240 |
| `days_to_full_alert` | 30 | 1–365 |

### GPU fields ⚠️

> **Expert-only:** GPU thresholds interact with NVIDIA DCGM profiling semantics, MIG hardware
> sizing, and time-slicing heuristics. Incorrect values produce misleading recommendations.
> Show a warning tooltip and link to [GPU Classification](architecture/gpu-classification.md).

| Field | Default | Validation |
|-------|---------|------------|
| `idle_threshold` | 0.02 | 0.0–1.0 |
| `underutilized_sm_threshold` | 0.25 | 0.0–1.0 |
| `underutilized_tensor_threshold` | 0.15 | 0.0–1.0 |
| `membound_dram_threshold` | 0.60 | 0.0–1.0 |
| `membound_tensor_threshold` | 0.15 | 0.0–1.0 |
| `fb_headroom_factor` | 1.20 | 0.0–1.0 |
| `compute_bound_dram_threshold` | 0.30 | 0.0–1.0 |
| `mig_fb_percentile` | 0.98 | 0.0–1.0 |
| `confidence_days_tier1` | 3 | 1–365 (must be < tier2) |
| `confidence_days_tier2` | 7 | 1–365 (must be < tier3) |
| `confidence_days_tier3` | 14 | 1–365 |
| `spike_ratio_threshold` | 5.0 | 1.0–100.0 |
| `spike_confidence_penalty` | 0.70 | 0.01–1.0 |
| `no_profiling_confidence_factor` | 0.50 | 0.01–1.0 |
| `timeslicing_majority_threshold` | 0.50 | 0.01–1.0 |
| `timeslicing_min_replicas` | 2 | 1–16 (must be ≤ max) |
| `timeslicing_max_replicas` | 8 | 1–16 |
| `timeslicing_base_penalty` | 0.70 | 0.01–1.0 |
| `timeslicing_impacted_weight` | 0.30 | 0.01–1.0 |
| `node_freshness_days` | 7 | 1–90 |

### Recommended values by use case

See [Configurability — Recommended Values by Use Case](architecture/configurability.md#recommended-values-by-use-case)
for aggressive cost optimization, stability-first, GPU training, and batch storage profiles.

### UI Integration Recommendations

- Build a Settings page with **Form** fields grouped by recommendation type (container, namespace, node, gpu, pvc tabs).
- For each field, show the current effective value and whether it is default, admin-overridden (`locked_fields`), or tenant-customized.
- Disable (grey out) locked fields with tooltip: "Set by administrator."
- Provide a "Reset to defaults" button per plugin (calls DELETE on the thresholds endpoint).
- Show inline validation errors on out-of-range values before submit; mirror server validation ranges from the tables above.
- After PUT success, show toast: "Thresholds updated. Recommendations will refresh within 60 seconds."
- Place GPU threshold fields in an "Advanced" **Accordion** with expert-only warning **Alert** and link to [GPU Classification](architecture/gpu-classification.md).
- For node thresholds, group utilization vs consolidation fields separately for clarity.
- For PVC thresholds, validate that `oversized_threshold` < `near_full_threshold` client-side.
- Consider a future "Preview impact" mode showing how many recommendations would change (not yet API-supported).
- Link to [Recommended Values by Use Case](architecture/configurability.md#recommended-values-by-use-case) as preset profiles.

---

## 9. Settings: Snapshot

```http
GET /recommendations/openshift/settings/snapshot
PUT /recommendations/openshift/settings/snapshot
```

No DELETE — PUT partial fields to override; omit locked fields.

| Field | Default | Description |
|-------|---------|-------------|
| `orphan_age_days` | 7 | Days after source PVC deletion before orphaned classification |
| `never_restored_days` | 30 | Days without any restore before never-restored classification |
| `stale_days` | 90 | Age threshold for stale classification |
| `redundant_threshold` | 3 | Max snapshots per PVC before older ones flagged redundant |
| `cost_per_gib_month_usd` | 0.05 | Fallback $/GiB/month when Koku rates unavailable |
| `locked_fields` | [] | Present on GET — fields controlled by admin env vars |

### UI Integration Recommendations

- Show snapshot threshold settings in a dedicated Settings **Form** section.
- Display each field with current value, default indicator, and locked status from `locked_fields`.
- Disable locked fields with tooltip: "Set by administrator."
- Fields: `orphan_age_days`, `never_restored_days`, `stale_days`, `redundant_threshold`, `cost_per_gib_month_usd`.
- Use **NumberInput** components with min/max validation matching server constraints.
- Explain each threshold with helper text tied to snapshot classification types (Section 5).
- On PUT success, show toast: "Snapshot settings updated. Classifications will refresh on next processing cycle."
- Link from snapshot recommendation list to this settings page for threshold tuning.
- No DELETE endpoint — provide "Reset to defaults" only if the API adds one; until then, document defaults inline.

---

## 10. Settings: Business Hours

Requires `ROS_BUSINESS_HOURS_ENABLED=true` and `business_hours: true` from capabilities.

### Org default

```http
GET    /recommendations/openshift/settings/business-hours
PUT    /recommendations/openshift/settings/business-hours
DELETE /recommendations/openshift/settings/business-hours
```

### Cluster override

```http
GET    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
PUT    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
DELETE /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
```

### Namespace override

```http
GET    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
PUT    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
DELETE /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
```

Resolution order: **namespace → cluster → org default**.

### Schedule format (PUT body)

```json
{
  "timezone": "America/New_York",
  "enabled": true,
  "off_hours_weight": 0.1,
  "schedule": {
    "days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
    "start_time": "08:00",
    "end_time": "17:00"
  }
}
```

| Field | Rules |
|-------|-------|
| `timezone` | IANA timezone name |
| `days[]` | Lowercase English day names |
| `start_time`, `end_time` | `HH:MM` 24-hour format |
| `off_hours_weight` | 0.0–1.0 — weight for samples outside the window (default 0.1) |
| `enabled` | `false` disables business-hours digests for the scope |

PUT returns `warnings` including storage-doubling notice when enabling.

### Reship status (cluster GET only)

Cluster and namespace GET responses include:

| Field | Values |
|-------|--------|
| `reship_status` | `complete`, `pending`, `forward_only` |
| `reship_status_since` | ISO timestamp when current status began |

| Status | UI treatment |
|--------|--------------|
| `complete` | Business-hours recommendations trustworthy |
| `pending` | Show “Recalculating business-hours data…” banner; `business_hours` may be absent |
| `forward_only` | **Degraded mode** — historical backfill unavailable; only forward data used. Show persistent warning banner explaining recommendations may not reflect full history |

Org-level GET does **not** include `reship_status`.

After schedule changes, expect one ingestion cycle before updated `business_hours` blocks appear in recommendations.

### UI Integration Recommendations

- Provide a schedule configuration UI with a weekly calendar grid showing business vs off-hours blocks.
- Preview which hours are "business" vs "off-hours" based on `timezone`, `days`, `start_time`, and `end_time`.
- Support org, cluster, and namespace override levels with clear hierarchy indicator (namespace → cluster → org).
- Show dual recommendation display on detail views: "All Hours" tab + "Business Hours" tab when `business_hours` block is present.
- Display `reship_status` on cluster and namespace settings pages: `complete` (green), `pending` (in-progress banner), `forward_only` (persistent warning **Alert**).
- When `reship_status` is `pending`, show banner: "Recalculating business-hours data…" and expect absent `business_hours` blocks temporarily.
- After schedule PUT, show warnings from the response (including storage-doubling notice when enabling).
- Show `off_hours_weight` with slider or **NumberInput** (0.0–1.0) and explain its effect on off-hours sample weighting.
- Provide `enabled` toggle per scope; disabling stops business-hours digest generation for that scope.
- Link to [Business Hours feature doc](features/business-hours.md) for reship flow details.
- On recommendation detail, default to all-hours view; switch to business-hours tab when user opts in.

---

## 11. Settings: Capabilities

```http
GET /recommendations/openshift/settings/capabilities
```

Discover which plugins and features are active for conditional UI rendering.

```json
{
  "recommendation_types": [
    { "name": "container", "supports_terms": true, "enabled": true },
    { "name": "gpu", "supports_terms": true, "enabled": true },
    { "name": "node", "supports_terms": true, "enabled": true },
    { "name": "pvc", "supports_terms": true, "enabled": true },
    { "name": "namespace", "supports_terms": true, "enabled": true },
    { "name": "snapshot", "supports_terms": false, "enabled": true }
  ],
  "business_hours": true
}
```

| Field | Use in UI |
|-------|-----------|
| `enabled: false` | Hide nav items and API calls for that domain (endpoint returns 404 when plugin disabled) |
| `supports_terms` | Show term configuration in Settings when true |
| `business_hours` | Show business-hours settings and dual-perspective recommendation UI |

Legacy Kruize mode excludes disabled Kruize plugin from the list.

### UI Integration Recommendations

- Call `GET /settings/capabilities` once per session (or on app init) to drive conditional navigation.
- Hide nav items and routes for plugins where `enabled: false` — do not render empty states implying zero data.
- Show term configuration in Settings only when `supports_terms: true` for that plugin.
- Show business-hours settings and dual-perspective recommendation UI only when `business_hours: true`.
- When a disabled plugin endpoint returns `404`, show nothing rather than an error page in navigation contexts.
- Use capabilities response to conditionally render dashboard cards (GPU, node, PVC, snapshot sections).
- Re-fetch capabilities after settings changes that enable/disable plugins (if applicable in deployment).

---


## 12. GPU Recommendations

### GPU summary

```http
GET /recommendations/openshift/gpu
```

Lightweight entry point for GPU optimization views. Returns counts and links to detailed lists.

```json
{
  "mig": { "count": 12, "link": "/api/cost-management/v1/recommendations/openshift/gpu/mig" },
  "timeslicing": { "count": 3, "link": "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing" },
  "total_gpus_analyzed": 48,
  "clusters_with_gpu_data": 2
}
```

Use this endpoint for dashboard cards that **link** to MIG and time-slicing
drill-downs — not for recommendation badge totals.

**`timeslicing.count` (N) vs list `meta.count` (M):**

| | Summary `timeslicing.count` | List `meta.count` |
|--|------------------------------|-------------------|
| **Measures** | GPU telemetry coverage (distinct node×GPU-model in digests) | Actionable time-slicing rows after engine gates |
| **Typical size** | Larger (N ≥ M) | Smaller — source of truth for “how many recs” |
| **UI role** | Show/hide time-slicing section; “we have GPU ROS data” | Badges, “N recommendations”, notification rollups |

The summary does **not** run `ComputeNodeTimeslicingRec` (performance trade-off).
The list drops groups that are well-utilized, memory-bound, idle-only, MIG-first,
below the majority threshold, or cannot reach `recommended_replicas` ≥ 2.

**Do not** show `timeslicing.count` as “N time-slicing recommendations.” Use
`GET .../gpu/timeslicing` → `meta.count`, notification **36**, or paginated
`data` length. Full semantics:
[GPU time-slicing — Summary vs list count](features/gpu-time-slicing.md#summary-vs-list-count-semantics).

### GPU MIG profile recommendations

```http
GET /recommendations/openshift/gpu/mig
```

Lists containers with MIG profile recommendations (`recommended_gpu_profile` set and not `full_gpu`).

**Limitations (Gap 5 — acceptable at current scale):**

- **In-memory pagination:** The handler builds the full MIG list per org (all clusters),
  then applies `offset`/`limit`, sort, and filters in application memory — not in SQL.
  Fine for tens to low hundreds of MIG workloads; large fleets (thousands) will need
  SQL-backed pagination (see [known-issues.md § GPU MIG — Known limitations](known-issues.md#gpu-mig--known-limitations-gap-5)).
- **Per-container recommendations only:** Each row is an independent MIG profile suggestion.
  The API does not propose consolidating multiple containers onto fewer GPUs to free a
  physical GPU (cluster-wide bin-packing is future work).
- **ROS Optimizations UI not shipped:** No koku-ui pages call `GET .../gpu`, `/gpu/mig`, or
  `/gpu/timeslicing` yet. Koku cost UI may expose `reports/openshift/gpu/mig_profiles/` (spend
  drill-down) — that is not a substitute for ROS recommendation fields on this section’s
  endpoints. See [known-issues.md § ROS MIG recommendations UI](known-issues.md#ros-mig-recommendations-ui-not-shipped).

| Parameter | Description |
|-----------|-------------|
| `cluster` | Filter by cluster alias |
| `project` | Filter by namespace |
| `container` | Filter by container name |
| `gpu_classification` | Exact match on classification |
| `term` | `short`, `medium`, or `long` |
| `order_by` | `cluster`, `project`, `container`, `gpu_classification`, `confidence`, etc. |
| `offset`, `limit` | Pagination |

```json
{
  "meta": { "count": 5, "limit": 20, "offset": 0 },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "ml",
      "workload": "train",
      "container": "worker",
      "term": "medium",
      "gpu_model": "NVIDIA-A100",
      "node_name": "gpu-node-1",
      "recommended_gpu_profile": "1g.5gb",
      "current_gpu_profile": "full_gpu",
      "gpu_classification": "underutilized",
      "confidence": 0.8
    }
  ]
}
```

Container list rows also embed GPU data under `gpu.{term}` when the GPU plugin is enabled.

### UI Integration Recommendations

> **Status:** The patterns below are the intended koku-ui design. **No Optimizations pages
> consume these ROS GPU endpoints today** — implement when product prioritizes ROS GPU UX.
> Backend APIs are ready; see [known-issues.md](known-issues.md#ros-mig-recommendations-ui-not-shipped).

- Use the GPU summary endpoint for dashboard **Card** widgets showing MIG vs time-slicing counts with links to detailed views.
- Show MIG recommendations in a **Table**: container, namespace, cluster, GPU model, current allocation, recommended MIG profile, savings.
- Use **Badge** for classification with text labels: idle (red), underutilized (yellow), memory_bound (blue), well_utilized (green), no_profiling (gray).
- Add tooltip explaining MIG profile format (e.g., "1g.5gb = 1 compute slice, 5 GB memory").
- For idle classification (code 26), show warning **Alert**: "Consider removing GPU allocation entirely."
- Link MIG rows to container detail views and node context via `node_name`.
- Show `confidence` as a badge; reduce prominence when code 28 (no profiling data) is present.
- Filter by `gpu_classification` and `term`; default to medium term.
- Surface `estimated_monthly_gpu_savings` from container list `gpu` objects when dollar amounts are needed.
- Cross-link to time-slicing view (Section 3) when notification code 36 is present.
- Never rely on color alone — pair badge colors with classification text for accessibility.

---

## 13. Recommendation History

```http
GET /recommendations/openshift/history
```

Historical snapshots of recommendation values for trend analysis and audit.

| Parameter | Description |
|-----------|-------------|
| `cluster`, `project`, `workload`, `container` | Entity filters |
| `term`, `engine` | Filter by term and engine |
| `start_date`, `end_date` | `YYYY-MM-DD` range on `recorded_at` (default: current month) |
| `order_by` | `recorded_at` (default), `cluster`, `project`, `workload`, `container`, `term`, `engine` |
| `order_how` | `asc` or `desc` |
| `offset`, `limit` | Pagination |
| `format` | `json` or `csv` |

Responses include `Cache-Control: private, max-age=300` — safe to cache briefly client-side.

### UI Integration Recommendations

- Show a sparkline chart of recommendation CPU/memory values over time per container on detail views.
- Fetch history filtered by container/workload/term/engine for the sparkline data source.
- Provide a fleet-level history explorer with **Table** + date range picker.
- Support CSV export (`format=csv`) with a **Button** for compliance and audit downloads.
- Default date range to current month; allow custom ranges via `start_date`/`end_date`.
- Overlay current recommendation on the sparkline for comparison.
- Show term and engine selectors that refetch history with matching query params.
- Handle empty history gracefully when `min_data_days` has not yet been met for new workloads.
- Use `recorded_at` on the x-axis; format values with the same unit toggles as list views (`cpu-unit`, `memory-unit`).
- Link from container detail "History" tab to full history explorer pre-filtered to that container.

---

## 14. Recommendation Quality

```http
GET /recommendations/openshift/quality
```

Quality metrics per container: stability, adoption, OOM events after recommendation.

| Parameter | Description |
|-----------|-------------|
| `cluster`, `project`, `workload`, `container` | Entity filters |
| `start_date`, `end_date` | `YYYY-MM-DD` range on `measured_at` (default: current month) |
| `order_by` | `measured_at` (default), `stability`, `adoption`, `oom_events`, `recommendation_age` |
| `order_how` | `asc` or `desc` |
| `offset`, `limit` | Pagination |
| `format` | `json` or `csv` |

Key response fields: `stability_pct`, `adoption_detected`, `oom_events_after_rec`, `recommendation_age_hours`.

### UI Integration Recommendations

- Show `stability_pct` as a **Badge** on each recommendation row (e.g., "92% stable").
- Build a quality dashboard **Card** showing aggregate stability across the fleet.
- Display adoption indicator: checkmark icon when `adoption_detected` is true (actual usage matches recommendation).
- Highlight containers with `oom_events_after_rec` > 0 using error **Badge** and link to performance engine recommendations.
- Sort quality table by `stability` ascending to surface unstable recommendations first.
- Provide CSV export for compliance reporting.
- Show `recommendation_age_hours` to indicate how long the current recommendation has been active.
- Combine quality metrics with history sparklines on container detail for full context.
- Filter by cluster/project to compare quality across teams.
- Use info **Alert** when stability is below 50% suggesting insufficient observation data or volatile workload.

---

## 15. Fleet Summary

```http
GET /recommendations/openshift/fleet-summary
```

Org-wide aggregate counts for dashboard hero metrics alongside savings summary.

```json
{
  "total_containers": 450,
  "active_containers": 420,
  "idle_containers": 15,
  "abandoned_containers": 8,
  "total_monthly_savings": { "value": "12500.750000", "units": "USD" },
  "cluster_count": 5,
  "currency": "USD"
}
```

Complements `GET /recommendations/openshift/savings-summary` (Section 6) with workload classification counts.

### UI Integration Recommendations

- Show fleet summary as dashboard **Card** tiles: total containers, active, idle, abandoned.
- Use **Badge** counts for idle (code 5) and abandoned (code 8) with links to filtered recommendation lists.
- Display `total_monthly_savings` alongside savings-summary for a complete fleet overview.
- Show `cluster_count` and `currency` in the dashboard header.
- Wire idle/abandoned tile clicks to container list with appropriate notification code filters.
- Differentiate idle vs abandoned visually (yellow vs red badges) with text labels.
- Refresh on page load; respect RBAC-scoped counts automatically from the API.
- Pair with savings-summary breakdown (Section 6) on the same overview page.
- Show "active" as `active_containers` / `total_containers` in a **ProgressBar** for fleet health.
- When all counts are zero, distinguish "no data" from "no permissions" using capabilities and sources status.

---
## 16. Common Patterns

### Pagination

**Authoritative reference:** [API Pagination](pagination.md).

| List type | Parameters | Response hints |
|-----------|------------|------------------|
| Container / namespace lists | Prefer `after` + `limit`; `offset` + `limit` still supported | `meta.has_next`, `meta.next_cursor` when using keyset |
| All other lists (PVC, GPU, nodes, history, VM, quota, …) | `offset` + `limit` | `meta.count`, `links.first\|previous\|next\|last` |

Container/namespace lists paginate by **distinct containers/namespaces**, not by raw DB rows
(each container row includes all term × engine combinations).

**Keyset flow (recommended for large orgs):**

```http
GET /api/cost-management/v1/recommendations/openshift?limit=50
GET /api/cost-management/v1/recommendations/openshift?limit=50&after=<meta.next_cursor>
```

Repeat while `meta.has_next` is `true`. Do not parse `next_cursor`. When `after` is sent,
`offset` is ignored (`meta.offset` is `0`).

### Sorting

ROS uses **`order_by` + `order_how`**, not a `-` prefix on column names.

Common container `order_by` values:

- `cluster`, `project`, `workload`, `workload_type`, `container`, `last_reported`
- `cpu_request_current`, `memory_request_current`
- `cpu_variation_medium_cost`, `cpu_variation_medium_performance`, etc.

Default sort: `last_reported` descending.

### Currency

Always read the `currency` field from the response (`USD`, `EUR`, etc.). Never hardcode `$`.
Format with the user's locale; prefix/suffix based on currency code.

When `currency` is absent, fall back to `USD` (server default).

### Engine parameter

| Endpoint | `engine` support |
|----------|------------------|
| `/savings-summary` | Yes — selects cost vs performance aggregation |
| `/nodes` | Yes — filters nested engine rows |
| Container/namespace list | No — client selects from nested `cost` / `performance` |

### Notification codes reference

| Code | Severity | Message (summary) | Suggested UI treatment |
|------|----------|-------------------|------------------------|
| 1 | WARNING | Less than 4 days of data | Low-confidence badge |
| 2 | WARNING | No metrics for 48+ hours | Stale / outdated indicator |
| 3 | CRITICAL | OOM kills detected | Error badge; suggest memory increase |
| 4 | WARNING | PDB affects MachineSet scaling | Info banner before node changes |
| 5 | INFO | Uses < 1% of requests | Idle badge; decommission candidate |
| 6 | INFO | Change matches prior recommendation | Subtle “already applied?” hint |
| 7 | INFO | < 24 hours of data | Unstable recommendation warning |
| 8 | WARNING | Zero usage 72+ hours | Abandoned badge; strong decommission cue |
| 9 | WARNING | Memory trend — capacity risk | Trend chart callout |
| 10 | INFO | GPU underutilized — consider MIG | Link to GPU/MIG views |
| 11 | INFO | Node underutilized | Node consolidation hint |
| 12 | WARNING | Node overcommitted | Risk badge on node row |
| 13 | INFO | CPU/memory imbalance | Stranded resource tooltip |
| 14 | WARNING | HPA at maxReplicas | Scaling bottleneck warning |
| 15 | INFO | HPA at minReplicas sustained | Scale-down opportunity |
| 16 | WARNING | Frequent scale events | Flapping autoscaler warning |
| 17 | INFO | Variable load, no autoscaler | Configuration suggestion |
| 18 | WARNING | VM near-zero utilization | VM idle badge |
| 19 | INFO | VM oversized vs usage | VM resize hint |
| 20 | WARNING | PVC zero usage | Orphaned storage badge |
| 21 | WARNING | HPA maxReplicas sustained | HPA bottleneck (duplicate context) |
| 22 | INFO | HPA-managed — replica recs suppressed | Explain missing replica advice |
| 23 | INFO | Instance type not in catalog | No resize available |
| 24 | INFO | Deprecated instance type | Migration suggestion |
| 25 | INFO | No cost data — no savings | Hide dollar amounts; link to cost model setup |
| 26 | INFO | GPU idle — remove GPU request | GPU deallocation hint |
| 27 | INFO | GPU memory-bound — more HBM | MIG profile suggestion |
| 28 | INFO | No GPU profiling data | Reduced-confidence GPU badge |
| 29 | INFO | PVC oversized | Rightsizing opportunity |
| 30 | WARNING | PVC near full | Urgent expansion warning |
| 31 | WARNING | Snapshot source PVC deleted | Orphan snapshot badge |
| 32 | INFO | Snapshot never restored | Cleanup candidate |
| 33 | INFO | Newer snapshot exists | Redundant snapshot hint |
| 34 | INFO | Snapshot older than retention | Stale snapshot badge |
| 35 | INFO | Backup-tool managed snapshot | Caution — review retention policy |
| 36 | INFO | GPU time-slicing candidate | Link to time-slicing view |

Reference endpoint: `GET /recommendations/openshift/notification-codes` (no identity header required).  
Full catalog: [Notification codes](architecture/notification-codes.md).

Severity mapping for badges: `CRITICAL` → error, `WARNING` → warning, `INFO` → info.

---

## 17. Cross-cutting UX Patterns

These patterns apply across multiple feature sections above. See each feature's **UI Integration Recommendations** for domain-specific guidance.

1. **Default view:** Cost engine, medium term, all-hours perspective.
2. **Engine toggle:** Tab or segmented control for cost vs performance — do not fetch separately.
3. **Negative savings:** Phrase as “requires additional resources” with absolute usage delta, not negative currency.
4. **Low confidence:** Badge when `confidence_level` < 0.5 or codes 1/7 present.
5. **Stale:** Badge when code 2 present or row excluded by default stale filter; offer “show stale” toggle wired to `?stale=only`.
6. **Idle/abandoned:** Distinct badges (codes 5 vs 8); prioritize in “optimization opportunities” lists.
7. **Business hours:** When `business_hours` block exists, show side-by-side comparison with all-hours config; respect `reship_status` banners on cluster settings.
8. **GPU thresholds settings:** Expert-only section with warning tooltip; hide behind “Advanced” accordion.
9. **PVC oversized:** Always show `resize_note` — Kubernetes cannot shrink PVCs in place.
10. **Capabilities gate:** Call `/settings/capabilities` once per session to hide disabled plugin nav.
11. **Plugin disabled:** Disabled plugins return `404` — do not render empty states that imply zero recommendations.
12. **Currency / no cost data:** When code 25 present, show “—” instead of `$0.00` and link to cost model configuration.

---

## Appendix: Notification codes endpoint

`GET /recommendations/openshift/notification-codes` returns the machine-readable notification catalog. Use it to populate dynamic tooltips and badge text. All notification codes are documented in [Notification codes](architecture/notification-codes.md) (and summarized in [Section 16](#notification-codes-reference) above).
