# Tag filtering

> **Last verified:** 2026-11-06

!!! info "Quick Facts"
    **Filter syntax:** `filter[tag:key]=value` (bracket notation, preferred) or `?tag=key:value` (legacy flat)  
    **Data source:** Push-synced `org_container_keys.resolved_tags` (`ROS_TAGS_SOURCE=api`, chart/deployed default); or Koku `reporting_ocptags_values` (`db`, binary unset / advanced)  
    **Feature gate:** `ROS_TAGS_ENABLED=true` (default: `true`)  
    **Supported plugins:** Containers, Namespaces, Nodes, VMs, GPU MIG, GPU Time-Slicing, PVC, Quota, Cluster-Quota  
    **Multi-value:** Comma-separated values use OR logic within a key; multiple keys use AND

Filter OpenShift optimization recommendations by labels (tags) that Cost Management already tracks for billing. Tag keys must be **enabled** under **Settings → Tags** in Cost Management; ROS does not expose a separate public tag catalog.

## Architecture (tag flow)

Labels flow through the ecosystem in three stages:

1. **koku-metrics-operator** collects pod, namespace, node, and PV labels from Prometheus (`kube_namespace_labels`, `kube_pod_labels`)
2. **Koku** ingests labels from CSVs, resolves them, and manages enabled/disabled keys via Settings → Tags
3. **ros-ocp-backend** reads resolved tags from Koku — chart/deployed default is push sync (`ROS_TAGS_SOURCE=api`); binary unset default is shared PostgreSQL (`ROS_TAGS_SOURCE=db`, advanced)

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
| `ROS_TAGS_SOURCE` | `api` (chart / deployed default); binary unset default is `db` | How ROS resolves namespace tags. `api` = Koku push sync into `resolved_tags` (on-prem chart and SaaS). `db` = direct Koku PostgreSQL reads (advanced shared-DB only) |
| `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` | (empty) | Comma-separated service account names authorized to call the internal push sync endpoint |

When `ROS_TAGS_ENABLED=false`, tag query parameters are ignored and list APIs return unfiltered results.

## Tag key discovery

Use the Cost Management Tags API (not ROS):

```bash
GET /api/cost-management/v1/tags/openshift/
GET /api/cost-management/v1/tags/openshift/{tag-key}/
```

Enable keys in **Settings → Tags** before filtering. If a key is unknown or has no matching workloads, list responses may include `meta.warnings` with `count: 0`.

Operators can monitor tag sync freshness (`api` mode) via `GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.

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

**Namespace-scoped resolution:** All containers in a namespace share the same resolved
tags. Pod-level labels are not individually resolvable — tag filtering operates at
namespace granularity.

**One value per tag key:** `resolved_tags` holds a single value per key in push mode. When
Koku observes several values for one namespace and key, the payload keeps the alphabetically
first and the `tag_keys` catalog lists all of them. Multi-value storage is not implemented
yet.

In advanced on-prem mode (`ROS_TAGS_SOURCE=db`), ROS list queries read Koku tenant tables
(`reporting_enabledtagkeys`, `reporting_ocptags_values`). A Koku upgrade that renames
tables or changes `cluster_ids[]` / `namespaces[]` semantics can break tag filters without
any ROS code change.

| Check | Covers | Does not cover |
|-------|--------|----------------|
| Startup DB probe | `reporting_enabledtagkeys` exists and is queryable | Column renames, JOIN semantics drift |
| Runtime SQL errors | Obvious breakage after deploy | Silent wrong filters (rare) |

Pin compatible Koku and ROS versions when using `db` mode and smoke-test `filter[tag:key]=value`
after Koku upgrades. See [Configuration → Tag Sync](../configuration.md#tag-sync) for
deployment settings.

## Push sync operations (`ROS_TAGS_SOURCE=api`)

Default for both the **cost-onprem chart** and **SaaS**. Tags flow **one way (Koku → ROS)** via
`POST /api/cost-management/v1/internal/tags/sync`. Event-driven sync runs within seconds
of Settings changes or OCP summarization; a Celery Beat safety-net runs every **6 hours**.
Alert if `GET /internal/tags/status?org_id=` shows `synced_at` older than ~7 hours.

**Source and coverage.** Koku builds the payload from its per-report-period label summaries
(pod and PV labels) for report periods from the start of last month through the end of this
month — the same sources and window the Koku Tags API uses. Every cluster that reported
inside that window is included, whichever day it last summarized; a cluster summarized a day
earlier no longer disappears from tag filters. A cluster that stops reporting keeps its tags
until its last report period leaves the window.

**Tag-key batching.** Tag keys take effect for a cluster when its next report is summarized.
The `tag_keys` catalog updates on the next sync, but namespace values arrive with that
cluster's next summarized report; enabling or disabling several keys at once produces one
catalog update, not one per key. This is intentional — toggling keys one by one must not
trigger a recompute per click.

Sync triggers, example Helm env vars, manual sync commands, and authentication are documented
in [Configuration → Tag Sync](../configuration.md#tag-sync).

## Tag lifecycle

What happens to a filter when the tags behind it change. Behaviour is identical from the
user's perspective once data is visible to ROS; the modes differ in *when* ROS sees a change
and *what happens on failure*.

| Event | `api` (chart / deployed default) | `db` (advanced shared database) |
|-------|----------------------------------|---------------------------------|
| Key enabled in Settings → Tags | Catalog updates on the next sync; values arrive with the cluster's next summarized report | Key appears in Koku's enabled table immediately; values appear after the next summarization of labeled pods |
| Key disabled in Settings | Key omitted from the payload; the full replace clears it from every container | The JOIN excludes the key on the next query |
| Tag mapping changed | Settings mutation triggers an immediate push with the new mapping | The next summarization rebuilds Koku's `reporting_ocptags_values` |
| Key disappears from a cluster (still enabled) | The next summarization sends fewer or empty values; the full replace removes the stale entry | The next summarization updates the tagged values used by the JOIN |
| New tag value appears | Visible after summarization + a successful push | Visible after the next daily summarization |
| Values disappear (pods deleted or relabeled) | The full replace removes stale `resolved_tags` on the next sync | Stale values drop out of Koku's tables on the next summarization |
| Sync failure (network, ROS down) | Previous tags retained; the failed org retries on the next event or the 6-hour cycle; other orgs unaffected | Not applicable — no HTTP sync |
| ROS pod restart | No state loss; tags live in PostgreSQL | No state loss; reads are stateless |

## Advanced on-prem startup health check (`ROS_TAGS_SOURCE=db`)

With `ROS_TAGS_ENABLED=true` and `ROS_TAGS_SOURCE=db`, ROS probes
`reporting_enabledtagkeys` at startup. Failure disables tag filtering for the process
lifetime. The probe confirms table reachability, not full column-level schema compatibility.
This path does **not** apply when using the chart default (`api`).

## Related documentation

- [Query parameters](../plugin-reference/query-parameters.md) — full filter and pagination syntax
- [Savings estimations](savings-estimations.md) — fleet `savings-summary` behavior
- Internal dual-path reference: [`docs/features/tag-filtering.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/features/tag-filtering.md)
