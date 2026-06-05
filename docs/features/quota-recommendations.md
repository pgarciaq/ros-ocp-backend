# Namespace ResourceQuota Recommendations

Right-size Kubernetes **ResourceQuota** (namespace-level) hard limits using observed
usage, configured quota limits, and aggregated container recommendation totals.

**Status:** Implemented. Phase 1 plugin `quota` (priority 35) between PVC (30) and
snapshot (40). API: `GET /api/cost-management/v1/recommendations/openshift/quota/`.
See [plugin-phases.md](../architecture/plugin-phases.md).

This is distinct from the **[namespace](../../docs-site/features/namespace-recommendations.md)**
plugin, which recommends ideal CPU/memory totals from namespace usage digests. The
`quota` plugin compares those aggregates (and actual quota consumption) against
**configured** ResourceQuota hard limits and advises operators to tighten or raise them.

**ClusterResourceQuota** (OpenShift team/tenant quotas) is **implemented** via the
`cluster-quota` plugin — see [cluster-resource-quota.md](cluster-resource-quota.md) and
`GET /api/cost-management/v1/recommendations/openshift/cluster-quota/`.

---

## What it is

- **ResourceQuota:** Per-namespace caps on aggregate resource consumption
  (`requests.cpu`, `requests.memory`, `limits.*`, etc.).
- **ClusterResourceQuota:** OpenShift extension that applies quota across a selector of
  namespaces — implemented by the `cluster-quota` plugin (see [cluster-resource-quota.md](cluster-resource-quota.md)).

Recommendations suggest adjusted hard limits with a configurable headroom margin, flag
mismatches between quota and real usage, and surface admission risk when utilization
approaches limits.

---

## Why it's useful

| Problem | Impact |
|---------|--------|
| **Over-provisioned quotas** | Reserved capacity blocks other teams; cluster appears full while nodes are idle |
| **Under-provisioned quotas** | Deployments fail at admission; HPA scale-out blocked; false "cluster full" signals |
| **Quota vs workload drift** | Quotas set at onboarding rarely updated after rightsizing container requests |

Pairs well with **[idle detection](idle-detection.md)**: an idle namespace with an
oversized quota represents **double waste** (unused workloads plus stranded quota capacity).

---

## Ingestion path

1. **Operator** — koku-metrics-operator emits namespace ROS CSV with per-ResourceQuota
   rows (`quota_name` from the Prometheus `resourcequota` label), hard limits
   (`*_namespace_sum`), optional used values (`*_namespace_used`), and extended
   resources (storage, pods, object counts). See [data sources](#what-data-we-already-have).
2. **Namespace digests** — `internal/ingestion/namespace.go` maps usage columns into
   `daily_namespace_digests` (max per namespace per day). Per-quota hard/used snapshots
   land in `daily_namespace_quota_digests` via [`namespace_quota.go`](../../internal/ingestion/namespace_quota.go).
   Missing optional columns are ignored (**backward compatible** with older operator builds).
3. **Container recommendations** — `processContainerCSVNative` runs container sizing,
   writes `recommendation_sets`, then calls `engine.RunQuotaRecommendations` so quota
   sees fresh container aggregates in the same cycle when container CSV is present.
4. **Namespace CSV follow-up** — `processNamespaceCSVNative` also calls
   `RunQuotaRecommendations` after namespace ingest so quota hard/used snapshots refresh
   even when a payload has no container CSV in that cycle (mitigates one-cycle lag for
   quota *usage* signals; container sums may still be one cycle behind — see below).
5. **Persistence** — Results upsert into `quota_recommendation_sets` (one row per
   org / cluster / namespace / **quota_name**).

Plugin registration: [`internal/plugins/quota/plugin.go`](../../internal/plugins/quota/plugin.go).
The `quota` plugin does **not** register an `IngestHook` — namespace CSV hooks run before
container recommendations exist; the authoritative run is the explicit call in
[`report_processor.go`](../../internal/services/report_processor.go) after container recs.

---

## How it works

1. **Ingest** — Namespace CSV columns `*_namespace_sum` → ResourceQuota `type=hard`;
   optional `*_namespace_used` → `type=used`. Stored on `daily_namespace_digests`.
2. **Aggregate** — Sum container `rec_*` request/limit columns per namespace
   (`term=medium`, `engine=cost`) from `recommendation_sets`.
3. **Compare (signal C)** — Utilization and risk use the **greater** of quota `used`
   and container recommendation sums vs hard limits. Recommended hard values =
   container sums × headroom (default 110% when `ROS_QUOTA_HEADROOM_PERCENT=10`).
4. **Classify recommendation type**
   - `raise` — max utilization (used or rec sum) ≥ high-risk threshold (default 90%)
   - `tighten` — recommended CPU or memory request hard &lt; current hard (capacity freed)
   - `optimal` — hard limits present but neither raise nor tighten applies
   - `none` — no hard limits on the namespace snapshot
5. **Classify risk** — Based on the same max utilization vs hard:
   - `high` — utilization ≥ `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` (default 90)
   - `medium` — utilization ≥ `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` (default 70)
   - `low` — utilization &gt; 0 but below medium threshold
   - `none` — no utilization signal
6. **Savings** — `tighten` rows estimate monthly savings from freed CPU/memory/storage
   request capacity via Koku `configured_rates` when `ROS_SAVINGS_ESTIMATES_ENABLED=true`
   (`estimated_savings` in API). Savings refresh on ingestion and when Koku triggers
   `POST /internal/recalculate-savings` after cost model updates (same path as
   container/node/PVC). Freed **pods** are reported in `capacity_freed.pods_freed`
   only — there is no cost-model metric for pod slots (see [Pod capacity freed](#pod-capacity-freed)).

Implementation: [`internal/engine/recommend_quota.go`](../../internal/engine/recommend_quota.go),
[`internal/engine/quota_run.go`](../../internal/engine/quota_run.go),
[`internal/api/handlers_quota_recs.go`](../../internal/api/handlers_quota_recs.go).

### Object-count resources

The operator ingests aggregated **`object_count_*`** metrics (sum of all Kubernetes
`count/*` ResourceQuota resource types, such as `count/configmaps` and `count/secrets`).
These appear on namespace and ClusterResourceQuota digests as `object_count_hard` /
`object_count_used`.

| Use case | Included? | Notes |
|----------|-----------|-------|
| **Risk level** | Yes | `ObjectCountBP` participates in `maxUtilizationBP()` — a namespace at 95% of its object-count hard limit can surface `high` risk even when CPU/memory are low. |
| **Blocking notifications** | Yes | Code **72** (namespace ResourceQuota at capacity) and code **73** (ClusterResourceQuota at capacity) fire when `used >= hard` on object counts, same as CPU/memory/storage/pods. |
| **Tighten / raise** | No | There is no workload-derived target comparable to summed container `rec_*` request columns. Recommended object-count hard values are not computed from rightsizing output. |
| **Savings** | No | Koku `effective_rates` has no object-count or per-object cost metric; freed object-count capacity is not monetized. |

Object-count quotas are **visibility and alerting only** in ROS: they help operators see
admission pressure on object totals, not FinOps dollar impact from quota tightening.

Implementation: [`quota_notifications.go`](../../internal/engine/quota_notifications.go),
[`recommend_quota.go`](../../internal/engine/recommend_quota.go) (`ObjectCountBP` only).

### Pod capacity freed

Pod quota uses **`pods_used`** from `kube_resourcequota` (with headroom) as the
recommended hard target — not a sum of container recommendations. Tighten can set
`capacity_freed.pods_freed`, but [`ApplyQuotaSavings()`](../../internal/engine/recommend_quota.go)
monetizes only CPU, memory, and storage (same as ClusterResourceQuota). See
[Pod savings feasibility](#pod-savings-feasibility) below.

### Timing and one-cycle lag

Container sums are read from PostgreSQL (`recommendation_sets`), not passed in memory
from the container plugin. Namespace quota **hard/used** and container **recommendation
sums** can therefore be up to **one operator upload cycle** out of phase.

**Typical payload (container CSV, then namespace CSV in the same or later upload):**

1. Container digest ingest runs.
2. `RecommendWorkloadsStreaming` writes fresh `term=medium` / `engine=cost` rows.
3. `RunQuotaRecommendations` at the end of `processContainerCSVNative` uses those new sums;
   namespace hard/used in `daily_namespace_quota_digests` may still reflect the **previous**
   cycle until step 4 completes.
4. Namespace CSV ingest updates hard/used snapshots; `RunQuotaRecommendations` at the end of
   `processNamespaceCSVNative` refreshes risk and tighten/raise using the latest quota metrics
   (container sums unchanged in that sub-step).

**Mitigations (implemented):**

- Quota always runs **after** container recommendation persistence when container CSV is present.
- Quota also runs **after** namespace CSV ingest so hard/used and blocking signals are not stuck
  one cycle behind when only namespace data arrives.

**Accepted limitation:** This is inherent to the split-CSV architecture (separate container and
namespace ROS files). ROS cannot derive container recommendation sums from namespace CSV alone;
running quota twice in one processor pass does not remove cross-file lag. For **steady-state**
operation (regular uploads of both file types), at most one cycle of skew is expected and is
acceptable. On **first deployment**, allow one full report cycle before tighten/raise fully
reflect both fresh container sums and fresh quota hard/used.

---

## Configuration

Thresholds resolve in three tiers (same pattern as idle detection settings):

1. **Per-org DB override** — `PUT /recommendations/openshift/settings/quota` persists to
   `recommendation_thresholds` (`recommendation_type=quota`).
2. **Environment variables** — deployment-wide defaults; when set, the field appears in
   `locked_fields` on GET and cannot be changed via PUT.
3. **Compiled defaults** — 10 / 90 / 70 when no DB row and no env override.

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_QUOTA_HEADROOM_PERCENT` | `10` | Extra margin on recommended quota hard values (10 → multiply sums by 1.10) |
| `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `90` | `raise` recommendation and `high` risk when max utilization ≥ 90% of hard |
| `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `70` | `medium` risk when utilization ≥ 70% and below high threshold |

See [configurability.md](../architecture/configurability.md#resourcequota) and
[operations/configuration.md](../operations/configuration.md).

Enable the plugin via `ROS_ENABLED_PLUGINS` (included in native defaults; omit `quota`
or use `ROS_DISABLED_PLUGINS=quota` to disable — API returns 404).

---

## API

`GET /api/cost-management/v1/recommendations/openshift/quota/`

- **Filters:** `filter[cluster]`, `filter[project]`, `filter[quota_name]` (alias
  `filter[resource_quota_name]`), `filter[recommendation_type]`, `filter[risk_level]`
  - `filter[recommendation_type]` — `tighten`, `raise`, `optimal`, or `none` (no hard
    limits on the namespace snapshot)
  - `filter[risk_level]` — `high`, `medium`, `low`, or `none` (no utilization signal)
- **Group by:** `group_by[cluster]` or `group_by[project]` (mutually exclusive;
  aggregated counts and summed savings)
- **Sort:** `order_by` with `order_how` (`asc` / `desc`; default `desc` when
  `order_by` is set). Allowed `order_by` values: `namespace`, `quota_name`,
  `utilization`, `estimated_monthly_savings`, `risk_level`. Default list order without
  `order_by` is namespace ascending.
- **Pagination:** `limit` (1–100, default 20), `offset` (default 0)
- **CSV export:** `format=csv` or `Accept: text/csv` — columns `cluster_uuid`, `namespace`,
  `quota_name`, `recommendation_type`, `risk_level`, `estimated_savings_value`,
  `estimated_savings_units`, `last_observed_at`, `count`
- Requires `quota` in `ROS_ENABLED_PLUGINS`

**Detail:** `GET /api/cost-management/v1/recommendations/openshift/quota/detail`

Query parameters: `cluster_uuid`, `namespace` (alias `filter[project]`). Optional:
`quota_name` (alias `filter[resource_quota_name]`) when multiple ResourceQuota objects
exist in the namespace. Returns one recommendation object (not wrapped in `data`) with
`headroom_basis_points`, `notifications` (codes **70–72**), and `history[]` per-resource
snapshots.

| Code | Name | When |
|------|------|------|
| **70** | `NotifQuotaNearCapacity` | `risk_level` is `high` |
| **71** | `NotifQuotaOversized` | `recommendation_type` is `tighten` |
| **72** | `NotifQuotaBlocking` | Any resource at or above hard limit |

Filter the catalog:
`GET /api/cost-management/v1/recommendations/openshift/notification-codes?filter[plugin]=quota`.
Code **73** applies to the `cluster-quota` plugin only.

**Fleet savings:** Quota savings are excluded from the fleet-level
`GET /api/cost-management/v1/recommendations/openshift/savings-summary` endpoint to avoid
double-counting with container-level savings that already account for quota-bound
workloads. Per-quota `estimated_savings` on list and detail responses is unchanged.

**Settings** (`GET` / `PUT` / `DELETE` `/api/cost-management/v1/recommendations/openshift/settings/quota`):

- GET returns merged thresholds plus `locked_fields` for env-locked parameters.
- PUT body: `headroom_percent` (0–100), `high_risk_threshold_percent` (1–100, must be
  greater than medium), `medium_risk_threshold_percent` (1–99).
- DELETE removes the per-org override; subsequent runs use env or compiled defaults.
- Disabled when the `quota` plugin is off (404).

Public reference: [docs-site feature page](../../docs-site/features/quota-recommendations.md),
[openapi.json](../../openapi.json).

---

## Related: ClusterResourceQuota

OpenShift team/tenant quotas are implemented separately via the **`cluster-quota`** plugin.
See [cluster-resource-quota.md](cluster-resource-quota.md) for API, operator CSV, and configuration.

## Roadmap / Future Work

### Quota UI (deferred)

APIs are production-ready (`GET .../quota/`, `GET .../quota/detail`, `GET .../cluster-quota/`,
settings, notification codes **70–73**, detail `history[]`). Dedicated **koku-ui** views are
deferred (large effort; ResourceQuota status report item 9). Planned scope: quota list with
utilization/risk/savings, detail breakdown, ClusterResourceQuota aggregate view, notification
integration, and historical trend charts. See [docs-site UI guide](../../docs-site/ui-integration-guide.md#4b-resourcequota-and-clusterresourcequota-recommendations)
and [known issues](../known-issues.md#deferred-quota-ui).

### Operator / engine gaps (namespace quota)

| Gap | Notes |
|-----|-------|
| **Per-quota object identity** | When multiple `ResourceQuota` objects exist per namespace, rows are keyed by `quota_name` from the operator; older builds without `quota_name` merge by namespace. |
| **Extended resources** | `requests.ephemeral-storage`, GPU quota, hugepages, custom device plugins — not collected. See [CRQ extended resources](cluster-resource-quota.md#extended-resources-future-work). |

Implemented in this plugin: CPU/memory/storage/pods in tighten/raise/risk; storage monetized via cost model; pods and object counts in `capacity_freed` / risk / notifications only (see [Object-count resources](#object-count-resources), [Pod capacity freed](#pod-capacity-freed)); per-resource `history[]`; notification codes **70–72** (73 is CRQ-only).

### Pod savings feasibility (analysis)

**Question:** Can ROS estimate dollar savings for freed **pod quota** slots by dividing node
cost by `node_allocatable_pods` (or kubelet max pods)?

**How quota savings work today:** [`ApplyQuotaSavings()`](../../internal/engine/recommend_quota.go)
uses the same Koku `configured_rates` helpers as PVC savings:

- `cpu_core_usage_per_hour` × freed CPU cores × 730
- `memory_gb_usage_per_hour` × freed GiB × 730
- `storage_gb_request_per_month` (or usage fallback) × freed storage GiB

Container rightsizing savings ([`savings.go`](../../internal/engine/savings.go)) are richer:
per-namespace **`namespace_aggregates`** (cost-model CPU/memory costs plus
`infrastructure_cost` + `distributed_cost` from OCP-on-cloud correlation). Node consolidation
savings ([`node_savings.go`](../../internal/engine/node_savings.go)) use the same hourly CPU/memory
rates plus **`node_cost_per_month` × `node_count_reduction`**.

There is **no** `pod_cost_per_month` (or similar) in `effective_rates`. Koku distributes
`cluster_cost_per_month` and `node_cost_per_month` to line items by CPU/memory (or storage/GPU),
not by pod count.

**Proposed “cost per pod slot” approach:**

```
node_monthly_cost = f(node_cost, cluster_share, cpu/mem rates, cloud infra)
cost_per_pod_slot = node_monthly_cost / pod_capacity
pod_quota_savings = pods_freed × cost_per_pod_slot
```

| Aspect | Assessment |
|--------|------------|
| **Feasibility** | **Medium–hard** in ros-ocp-backend; **hard** to make accurate cluster-wide. |
| **Data available** | `pod_capacity` / `node_capacity_pods` on node digests (operator); `pods_freed` on quota recs; node rates in `configured_rates`. Missing: which node(s) “own” a namespace’s pod quota, per-pod CPU/memory footprint, and whether freed slots enable node removal. |
| **Accuracy** | **Low to misleading** for FinOps. Pod slots are not fungible like GiB: a freed quota of 10 pod slots does not save money unless workloads shrink **and** the cluster can remove a node or shed cloud instance cost. A sidecar (10m CPU) and a batch job (32 cores) both consume one slot. Dividing **total node cost** by max pods assumes uniform cost per slot — dominated by large workloads, not slot count. |
| **Double-count risk** | Container and node plugins already monetize CPU/memory (and node removal). Adding pod-slot dollars on quota tighten would **overlap** unless carefully scoped to “quota-only” capacity with no CPU/memory freed — a narrow edge case. |
| **Where to implement** | **Neither Koku nor ROS should add this as default quota savings.** Koku could theoretically expose a derived metric (e.g. amortized node cost per allocatable pod for reporting), but the **business meaning** belongs in ROS/docs as operational capacity, not dollars. If ever built, a **ros-ocp-backend** optional estimate (behind a flag, with heavy disclaimers) is closer to recommendation context; **Koku** would only be the rate source, not the allocator of pod-slot economics. |
| **Recommendation** | Keep **`pods_freed` as a count** in `capacity_freed` and API; do **not** add `estimated_savings` for pods without node consolidation proof. Prioritize node plugin savings (`node_count_reduction` × `node_cost_per_month`) when pod headroom blocks consolidation (notification **74**). |

---

---

## Related documentation

- [Namespace Quota Optimization](../../docs-site/features/namespace-recommendations.md) — usage-based namespace sizing
- [Plugin Execution Phases](../architecture/plugin-phases.md) — phase and priority table
- [REQ-8.4](../architecture/requirements.md) — requirements traceability
- [ClusterResourceQuota recommendations](cluster-resource-quota.md) — `cluster-quota` plugin
- [Known issues](../known-issues.md) — ClusterResourceQuota and remaining gaps
