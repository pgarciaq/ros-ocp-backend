# API Pagination Strategy

> **Last verified:** 2026-08-05

This page is the **authoritative public reference** for how ROS-OCP Backend paginates
list endpoints. It covers the keyset (cursor) API contract, which routes use it, which
routes stay on offset pagination and why, and when offset-only endpoints might need keyset
in the future.

**Base path:** `/api/cost-management/v1`

---

## Summary

| Strategy | When to use | Endpoints |
|----------|-------------|-----------|
| **Keyset (`after`)** | Default for all list endpoints; stable latency at depth | Container, namespace, PVC, GPU, nodes, quota, cluster-quota, VM, machinesets, snapshots |
| **Offset (`offset` / `limit`)** | Legacy page-number UX; small bounded sets | History, quality (and keyset-capable lists for backward compat) |

All primary recommendation list endpoints support **`after`** cursors with **`offset`**
fallback. Prefer keyset for infinite scroll and deep pages.

---

## Keyset pagination

### Endpoints

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/recommendations/openshift` | Container recommendations (distinct containers per page) |
| `GET` | `/recommendations/openshift/namespaces` | Namespace recommendations (canonical path) |
| `GET` | `/recommendations/openshift/namespace` | Legacy alias — same handler as `namespaces` |
| `GET` | `/openshift/namespace/recommendations` | Legacy alias — same handler |
| `GET` | `/recommendations/openshift/pvcs` | PVC recommendations (tie-break: cluster + namespace + PVC name) |
| `GET` | `/recommendations/openshift/nodes` | Node CPU/memory utilization (tie-break: cluster + node) |
| `GET` | `/recommendations/openshift/gpu/timeslicing` | GPU time-slicing (tie-break: cluster + node + GPU model) |
| `GET` | `/recommendations/openshift/gpu/mig` | GPU MIG profiles (tie-break: cluster + namespace + container + GPU model) |
| `GET` | `/recommendations/openshift/quota` | Namespace ResourceQuota (tie-break: cluster + namespace + quota name) |
| `GET` | `/recommendations/openshift/cluster-quota` | ClusterResourceQuota (tie-break: cluster + CRQ name) |
| `GET` | `/recommendations/openshift/vm` | VM sizing (tie-break: cluster + vm + namespace + term + engine) |
| `GET` | `/recommendations/openshift/machinesets` | MachineSet aggregation (tie-break: machineset name + cluster) |
| `GET` | `/recommendations/openshift/snapshots` | VolumeSnapshot staleness (tie-break: cluster + namespace + snapshot name) |

Container and namespace list responses paginate by **distinct container or namespace**, not by raw database rows.
Each list item can still expose multiple **term × engine** nested objects.

### API contract

#### Request

| Parameter | Required | Description |
|-----------|----------|-------------|
| `after` | No | Opaque **base64url** cursor from the previous response’s `meta.next_cursor`. When set, the server uses keyset seek and **ignores `offset`**. |
| `limit` | No | Page size (default **100**, max **1000**). |
| `offset` | No | Legacy offset pagination. **Ignored when `after` is present.** Still supported for existing clients that do not send `after`. |

Example — first page:

```http
GET /api/cost-management/v1/recommendations/openshift?limit=100
```

Example — next page (copy `meta.next_cursor` verbatim):

```http
GET /api/cost-management/v1/recommendations/openshift?limit=100&after=<meta.next_cursor>
```

Invalid or tampered cursors return **400** with a message such as `invalid after parameter`.

#### Response (`meta`)

| Field | Type | Description |
|-------|------|-------------|
| `meta.count` | integer | Total distinct containers/namespaces for the org (from pre-computed org stats when available). |
| `meta.limit` | integer | Page size for this request. |
| `meta.offset` | integer | **0** when keyset mode (`after` was used); otherwise the request offset. |
| `meta.has_next` | boolean | `true` if another page exists after this one. |
| `meta.next_cursor` | string | Present when `has_next` is `true`. Pass as `after` on the next request. Opaque to clients — do not parse or construct manually. |

`links.first`, `links.previous`, `links.next`, and `links.last` are still populated for
offset mode. In keyset mode, prefer following `meta.next_cursor` rather than synthesizing
offsets from link URLs.

#### Sort keys (implementation detail)

Cursors encode the last row’s sort position so the database can seek with
`WHERE (sort columns) > (cursor)` instead of `OFFSET n`:

| List | Default sort | Cursor fields (decoded internally) |
|------|--------------|-----------------------------------|
| Containers | `last_reported` DESC (configurable via `order_by` / `order_how`) | `namespace`, `workload`, `container_name`, `cluster_uuid` |
| Namespaces | Cluster + namespace order | `namespace`, `cluster_uuid` |
| PVCs | `usage_ratio` DESC (configurable) | `cluster_uuid`, `namespace`, `persistentvolumeclaim` |
| Nodes | `estimated_monthly_savings` DESC (configurable) | `cluster_uuid`, `node` |
| GPU time-slicing | `node_name` (configurable) | `cluster_uuid`, `node_name`, `gpu_model` |
| GPU MIG | `cluster_uuid` (configurable) | `cluster_uuid`, `namespace`, `container`, `gpu_model` |
| Quota | `namespace` ASC (configurable) | `cluster_uuid`, `namespace`, `quota_name` |
| Cluster-quota | `cluster_quota_name` (configurable) | `cluster_uuid`, `cluster_quota_name` |
| VM | `vm_name` ASC (configurable) | `cluster_uuid`, `vm_name`, `namespace`, `term`, `engine` |
| Machinesets | `total_savings_cents` DESC | `machineset_name`, `cluster_uuid` |
| Snapshots | `age_days` DESC (configurable) | `cluster_uuid`, `namespace`, `snapshot_name` |

The `ORDER BY` used for a request must stay **stable** across pages (same filters and
sort). Changing filters or sort between pages can skip or duplicate rows.

#### Why keyset (not offset at depth)

Offset pagination must scan and discard `offset` rows before returning `limit` rows, so
cost grows with page number. Keyset pagination seeks from an indexed sort key — **stable
latency at any depth** when `ORDER BY` matches the index (see
[Query Performance — `org_container_keys`](query-performance.md#pagination-architecture-org_container_keys)).

| Approx. page depth | Offset / `limit` | Keyset / `after` |
|--------------------|-------------------|------------------|
| Page 1 | ~1 ms | ~1 ms |
| Page 100 | ~15 ms | ~1 ms |
| Page 1000 | ~150 ms | ~1 ms |

At ~200k containers, the container list uses the `org_container_keys` key table plus
keyset seek — typically **under 5 ms per page** at any depth (migration `000081`).

#### Client integration patterns

**Infinite scroll / “load more”** — Recommended:

1. `GET ...?limit=50` (no `after`).
2. If `meta.has_next`, `GET ...?limit=50&after=<meta.next_cursor>`.
3. Repeat until `has_next` is `false`.

**Classic paged UI with page numbers** — Continue using `offset` and `limit` until the
UI migrates to cursor mode. Deep offset pages on very large orgs will be slower than keyset.

**Do not:**

- Decode or edit `next_cursor` (base64url JSON internally; format may change).
- Mix `after` with a non-zero `offset` expecting both to apply (`offset` is ignored).
- Change `order_by`, filters, or `filter[tag:…]` between cursor pages without restarting from page 1.

### Implementation references

| Component | Location |
|-----------|----------|
| Cursor encode/decode | [`internal/api/cursor.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/cursor.go) |
| Handler wiring | [`internal/api/handlers_pagination.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/api/handlers_pagination.go) |
| SQL keyset filters | [`internal/model/recommendation_set_native.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/recommendation_set_native.go), [`internal/model/namespace_recommendation_set_native.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/model/namespace_recommendation_set_native.go) |
| Indexes | Migrations `000078`, `000134_keyset_quota_vm_machineset_indexes`, `000139_snapshot_keyset_index` |
| Large-org key table | `org_container_keys` — migration `000081`; see [query-performance.md](query-performance.md) |

---

## Pagination by endpoint (full matrix)

This section lists every list endpoint and its pagination strategy. Most primary
list endpoints support **keyset + offset**; a few bounded endpoints remain
**offset-only** by design — the rationale is **result-set cardinality**, not
missing implementation.

### Endpoint matrix

| Endpoint | Pagination | Typical scale | Rationale |
|----------|------------|---------------|-----------|
| `GET /recommendations/openshift` | **Keyset + offset** | 1k–200k+ containers/org | Primary fleet list; OFFSET at depth is expensive |
| `GET /recommendations/openshift/namespaces` (+ legacy aliases) | **Keyset + offset** | Hundreds–low thousands/org | Same pattern as containers |
| `GET /recommendations/openshift/history` | Offset only | Default **last 30 days** (`ROS_HISTORY_DEFAULT_DAYS`); retention ~90 days | Bounded time window; filtered history is tens–low hundreds of rows per query |
| `GET /recommendations/openshift/namespaces/{id}/history` | Offset only | ~14–90 days × terms × engines per namespace | Per-entity history; max ~30–60 rows typical |
| `GET /recommendations/openshift/vms/{vm_name}/history` | Offset only | Same as namespace history | Per-VM bounded snapshots |
| `GET /recommendations/openshift/pvcs` | **Keyset + offset** | Tens–low hundreds per cluster | SQL keyset on `pvc_recommendation_sets` |
| `GET /recommendations/openshift/gpu/mig` | **Keyset + offset** | Tens–low hundreds org-wide | SQL keyset on digest keys, then MIG engine per page |
| `GET /recommendations/openshift/gpu/timeslicing` | **Keyset + offset** | Same as MIG | SQL triple pagination + per-triple engine |
| `GET /recommendations/openshift/nodes` | **Keyset + offset** | Nodes per cluster (tens–low hundreds) | SQL keyset on grouped node keys |
| `GET /recommendations/openshift/quota` | **Keyset + offset** | ResourceQuotas per namespace | SQL keyset on `quota_recommendation_sets` |
| `GET /recommendations/openshift/cluster-quota` | **Keyset + offset** | ClusterResourceQuotas per cluster | SQL keyset on `cluster_quota_recommendation_sets` |
| `GET /recommendations/openshift/vm` | **Keyset + offset** | VMs per org | SQL keyset on `vm_recommendations` |
| `GET /recommendations/openshift/machinesets` | **Keyset + offset** | MachineSet groups | SQL keyset on aggregated node rows |
| `GET /recommendations/openshift/snapshots` | **Keyset + offset** | Bounded by retention/settings | SQL keyset on `snapshot_recommendation_sets` |
| `GET /recommendations/openshift/quality` | Offset only | One row per active container cycle | Similar bounds to history/quality pipelines |
| `GET /recommendations/openshift/savings-summary` | N/A (aggregate) | Single rollup | Not a row list |
| `GET /recommendations/openshift/fleet-summary` | N/A (aggregate) | Single rollup | Not a row list |

### Why each category stays offset-only

#### History endpoints

- **Paths:** `GET /recommendations/openshift/history` (fleet-wide, filterable);
  `GET /recommendations/openshift/namespaces/{recommendation-id}/history`;
  `GET /recommendations/openshift/vms/{vm_name}/history`.
- **Why:** History is a **bounded** time series — default window is the **last 30 days**
  (`ROS_HISTORY_DEFAULT_DAYS`, default 30), with configurable retention (`ROS_HISTORY_RETENTION_DAYS`, default 90). Even
  with daily snapshots per container × term × engine, filtered result sets are usually
  **tens to low hundreds of rows**, not thousands. `OFFSET` overhead is negligible.
- **Detail:** [History & Quality](features/history-and-quality.md).

#### PVC recommendations

- **Path:** `GET /recommendations/openshift/pvcs`
- **Why:** PVC count per cluster is typically **tens to low hundreds**. Operators rarely
  have thousands of PVC recommendation rows in a single filtered view.

#### GPU plugin lists (MIG, time-slicing)

- **Paths:** e.g. `GET /recommendations/openshift/gpu/mig`, time-slicing list routes under `/gpu/`
- **Why:** The handler **loads, classifies, filters, and sorts in memory**, then applies
  `offset`/`limit`. GPU deployments are **expensive and sparse** — org-wide MIG-capable
  rows are usually tens to low hundreds, not tens of thousands. Adding keyset would require
  **materializing recommendations to SQL** (or a dedicated page-key table) first; that cost
  is not justified at current scale. See [known-issues.md — MIG in-memory pagination](known-issues.md#mig-list-in-memory-pagination).

#### Node recommendations

- **Path:** `GET /recommendations/openshift/nodes` (and node utilization variants)
- **Why:** Cardinality is bounded by **nodes per cluster** (often tens on SNO, low hundreds
  on large clusters). Node sizing rows are computed at read time from digests.

---


## When offset-only endpoints would need keyset

Consider keyset (or SQL-backed pagination with page keys) when **any** of the following
become true in production for a **single org** (with typical filters):

| Signal | Threshold (rule of thumb) | Likely endpoint |
|--------|---------------------------|-----------------|
| Deep offset pages exceed **~500 ms** p95 | `offset` > 5,000 on unfiltered list | That list endpoint |
| Unfiltered org row count | **> 5,000** distinct list keys | Container/namespace (already keyset); or PVC/GPU/VM if product grows |
| GPU MIG/time-slicing list | **> 1,000** rows after filters | `/gpu/mig`, time-slicing — materialize to DB first |
| Per-cluster PVC list | **> 2,000** PVCs with recommendations | `/pvcs` |
| VM list | **> 2,000** guests per org | `/vm` |

**Today:** Only **container** and **namespace** lists routinely hit **thousands+** rows in
large enterprises. Those are the endpoints that **must** use keyset for acceptable UX.

History, quota, node, and GPU lists are unlikely to reach that scale without a fundamental
change in product scope (e.g. retaining years of per-snapshot history in one query).

---

## Related documentation

| Topic | Page |
|-------|------|
| Environment / quick `after` example | [Configuration — List pagination](configuration.md#list-pagination-after-cursor) |
| `org_container_keys` and latency | [Query Performance](query-performance.md) |
| Query parameter cheat sheet | [Plugin Reference — Query parameters](plugin-reference/query-parameters.md) |
| UI integration (infinite scroll) | [UI Integration Guide — Pagination](ui-integration-guide.md#pagination) |
| GPU MIG pagination limitations | [Known Issues](known-issues.md) |
| Koku report APIs (separate service) | DRF offset/cursor — not ROS-OCP |

---

## Keyset vs offset trade-offs

| Keyset (`after`) | Offset (`offset` / `limit`) |
|------------------|-----------------------------|
| Flat latency at deep pages | Simple “page N” UX |
| Stable with matching index + sort | Random access to page N |
| No row skips when sort is stable | Scan cost grows with `offset` |
| Client must follow `next_cursor` | `links.*` URLs work out of the box |
| Cannot jump to arbitrary page number | Fine for small or bounded sets |

Both modes coexist on all keyset-capable lists: new clients should prefer **`after`**;
legacy clients can keep **`offset`** until migrated.
