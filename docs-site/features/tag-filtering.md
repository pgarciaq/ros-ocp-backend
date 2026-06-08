# Tag filtering

!!! info "Quick Facts"
    **Filter syntax:** `filter[tag:key]=value` (bracket notation, preferred) or `?tag=key:value` (legacy flat)  
    **Data source:** Koku `reporting_ocptags_values` table (enabled tag keys only)  
    **Feature gate:** `ROS_TAGS_ENABLED=true` (default: `true`)  
    **Supported plugins:** Containers, Namespaces, Nodes, VMs, GPU MIG, GPU Time-Slicing, PVC, Quota, Cluster-Quota  
    **Multi-value:** Comma-separated values use OR logic within a key; multiple keys use AND

Filter OpenShift optimization recommendations by labels (tags) that Cost Management already tracks for billing. Tag keys must be **enabled** under **Settings → Tags** in Cost Management; ROS does not expose a separate public tag catalog.

## Filter syntax

Use Koku bracket notation (preferred) or legacy flat `?tag=` parameters:

```bash
GET /api/cost-management/v1/recommendations/openshift?filter[tag:environment]=production
GET /api/cost-management/v1/recommendations/openshift?filter[tag:environment]=production,staging
GET /api/cost-management/v1/recommendations/openshift?filter[tag:team]=platform
GET /api/cost-management/v1/recommendations/openshift?tag=environment:production
```

| Pattern | Semantics |
|---------|-----------|
| `filter[tag:key]=value` | Exact match |
| `filter[tag:key]=a,b` | OR within the same key |
| Multiple `filter[tag:*]` keys | AND across keys |
| `filter[tag:key]=*` | Key present (any value) |

Full parameter reference: [Query parameters](../plugin-reference/query-parameters.md).

The machine-readable parameter catalog is in [`openapi.json`](../openapi.md). All list endpoints
(except snapshots) document `filter[tag:environment]` and other tag keys as query parameters.

## Supported endpoints

Tag filters apply to these list APIs (and container **history**). **Snapshot** list and summary endpoints do **not** support `filter[tag:key]`.

| Resource | Path |
|----------|------|
| Containers | `GET .../recommendations/openshift` |
| Container history | `GET .../recommendations/openshift/history` |
| Namespaces | `GET .../recommendations/openshift/namespaces` |
| Nodes | `GET .../recommendations/openshift/nodes` |
| PVCs | `GET .../recommendations/openshift/pvcs` |
| VMs | `GET .../recommendations/openshift/vm` |
| GPU MIG | `GET .../recommendations/openshift/gpu/mig` |
| GPU time-slicing | `GET .../recommendations/openshift/gpu/timeslicing` |
| Resource quota | `GET .../recommendations/openshift/quota` |
| Cluster resource quota | `GET .../recommendations/openshift/cluster-quota` |

**Fleet history vs per-resource detail:** `filter[tag:key]` works on **fleet history**
(`GET .../recommendations/openshift/history`). It does **not** apply to per-resource detail or
per-resource history routes (for example `GET .../containers/{id}` or
`GET .../namespaces/{id}/history`).

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_TAGS_ENABLED` | `true` | Master switch for tag filters and tag `group_by` on savings summary |
| `ROS_TAGS_SOURCE` | `db` (on-prem) / `api` (SaaS) | How ROS resolves namespace tags (shared PostgreSQL vs Koku push sync) |

When `ROS_TAGS_ENABLED=false`, tag query parameters are ignored and list APIs return unfiltered results.

## Tag key discovery

Use the Cost Management Tags API (not ROS):

```bash
GET /api/cost-management/v1/tags/openshift/
GET /api/cost-management/v1/tags/openshift/{tag-key}/
```

Enable keys in **Settings → Tags** before filtering. If a key is unknown or has no matching workloads, list responses may include `meta.warnings` with `count: 0`.

Operators can monitor tag sync freshness (SaaS) via `GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.

## RBAC interaction

Tag filters intersect with cluster and project RBAC scope. A row must match **both** the caller's
permitted clusters/namespaces **and** the tag predicate. Restricted users never see recommendations
outside their scope, even when a tag would match globally.

When identity has restricted cluster access, results are the intersection of RBAC-allowed scope
and tag matches. A valid tag filter scoped to a cluster the caller cannot access returns **200**
with an empty list, not **403**.

## Group by tag (fleet savings summary)

Aggregate **container** savings per tag value (not available on list or history responses):

```bash
GET /api/cost-management/v1/recommendations/openshift/savings-summary?group_by[tag:environment]=*
GET /api/cost-management/v1/recommendations/openshift/savings-summary?group_by=tag:environment
```

Optional scoping when grouping: `filter[cluster]`, `filter[project]` (see [Query parameters](../plugin-reference/query-parameters.md)).

## Caveats and operational risks

On-prem (`ROS_TAGS_SOURCE=db`), ROS list queries read Koku tenant tables
(`reporting_enabledtagkeys`, `reporting_ocptags_values`). A Koku upgrade that renames
tables or changes `cluster_ids[]` / `namespaces[]` semantics can break tag filters without
any ROS code change.

| Check | Covers | Does not cover |
|-------|--------|----------------|
| Startup DB probe | `reporting_enabledtagkeys` exists and is queryable | Column renames, JOIN semantics drift |
| Runtime SQL errors | Obvious breakage after deploy | Silent wrong filters (rare) |

Pin compatible Koku and ROS versions for on-prem and smoke-test `filter[tag:key]=value`
after Koku upgrades. See [Configuration → Tag Sync](../configuration.md#tag-sync) for
deployment settings.

## SaaS operations (`ROS_TAGS_SOURCE=api`)

When Koku and ROS use separate databases, tags flow **one way (Koku → ROS)** via
`POST /api/cost-management/v1/internal/tags/sync`. Event-driven sync runs within seconds
of Settings changes or OCP summarization; a Celery Beat safety-net runs every **6 hours**.
Alert if `GET /internal/tags/status?org_id=` shows `synced_at` older than ~7 hours.

Sync triggers, example Helm env vars, manual sync commands, and authentication are documented
in [Configuration → Tag Sync](../configuration.md#tag-sync).

## On-prem startup health check (`ROS_TAGS_SOURCE=db`)

With `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=db`, ROS probes
`reporting_enabledtagkeys` at startup. Failure disables tag filtering for the process
lifetime. The probe confirms table reachability, not full column-level schema compatibility.

## Related documentation

- [Query parameters](../plugin-reference/query-parameters.md) — full filter and pagination syntax
- [Savings estimations](savings-estimations.md) — fleet `savings-summary` behavior
- Internal dual-path reference: [`docs/features/tag-filtering.md`](../../docs/features/tag-filtering.md)
