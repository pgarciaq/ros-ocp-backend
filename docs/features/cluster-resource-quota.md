# ClusterResourceQuota Recommendations (Design)

**Status:** Design only — not implemented.  
**Depends on:** Namespace [ResourceQuota recommendations](quota-recommendations.md) (shipped).  
**OpenShift:** Required — `ClusterResourceQuota` is an OpenShift API extension (`quota.openshift.io/v1`), not upstream Kubernetes.

---

## What is ClusterResourceQuota?

`ClusterResourceQuota` is an OpenShift-only resource that enforces the same **hard / used**
semantics as namespace `ResourceQuota`, but **aggregated across multiple namespaces** selected
by a label selector (or annotation selector) on `Namespace` objects.

| Concept | Namespace `ResourceQuota` | `ClusterResourceQuota` |
|---------|---------------------------|-------------------------|
| API group | `v1` / `ResourceQuota` | `quota.openshift.io/v1` / `ClusterResourceQuota` |
| Scope | One namespace | Many namespaces (selector) |
| Enforcement | Per-namespace admission | Cluster controller sums usage across matched namespaces |
| Typical owner | Namespace / project admin | Platform / cluster admin, FinOps, tenant leads |

Example use cases:

- **Team budget:** `team=payments` namespaces share one CPU/memory pool.
- **Department chargeback:** Align CRQ name with cost center or business unit.
- **Platform guardrails:** Cap total requests for all namespaces labeled `environment=dev`.

CRQ objects are **per cluster** — they do not span clusters. A recommendation row is always
`(org_id, cluster_uuid, crq_name)`.

---

## Why recommend on CRQ?

Namespace quota recommendations answer: *“Is this namespace’s ResourceQuota aligned with its
workloads?”* CRQ recommendations answer a higher-level question: *“Is the team/tenant allocation
aligned with aggregate workload demand?”*

| Benefit | Detail |
|---------|--------|
| **FinOps alignment** | CRQs map naturally to team budgets, chargeback, and showback — one row per budget boundary. |
| **Over-provisioned team pools** | CRQ hard limits often set at onboarding; namespace rightsizing may leave team-level capacity stranded. |
| **Under-provisioned teams** | Sum of namespace admissions can approach CRQ hard while individual namespaces look healthy. |
| **Drill-down hierarchy** | Fleet view: CRQ → namespace quota recs → container recs. |

CRQ recs **complement** namespace quota recs; they do not replace them. Namespace admins still
need per-namespace `tighten` / `raise` signals; cluster admins need CRQ-level signals for
reallocation and budgeting conversations.

---

## Research summary

### OpenShift metrics (not `kube_resourcequota`)

Namespace quota collection today uses **`kube_resourcequota`** with `namespace` and
`resource` labels — see
[`koku-metrics-operator/internal/collector/queries.go`](../../../koku-metrics-operator/internal/collector/queries.go)
(`ros:cpu_request_namespace_sum`, etc.).

**ClusterResourceQuota is a separate metric family** from
[openshift-state-metrics](https://github.com/openshift/openshift-state-metrics/blob/master/docs/clusterresourcequota-metrics.md)
(typically scraped in `openshift-monitoring` on OCP clusters):

| Metric | Labels (key) | Purpose |
|--------|----------------|---------|
| `openshift_clusterresourcequota_usage` | `name`, `resource`, `type` (`hard` \| `used`) | Aggregated hard/used across all namespaces matched by the CRQ |
| `openshift_clusterresourcequota_namespace_usage` | `name`, `namespace`, `resource`, `type` | Per-namespace contribution under a CRQ |
| `openshift_clusterresourcequota_selector` | `name`, `type`, `key`, `value` / `values`, `operator` | Namespace selector (match-labels, match-expressions, annotations) |
| `openshift_clusterresourcequota_labels` | `name` | CRQ object labels |
| `openshift_clusterresourcequota_created` | `name` | Existence / age signal |

**Answer:** `kube_resourcequota` does **not** expose CRQ aggregates. CRQ hard/used must come from
`openshift_clusterresourcequota_*` (or equivalent telemetry). Do not assume CRQ appears in
existing namespace-sum PromQL.

### Current operator collection

The metrics operator **does not** query any `openshift_clusterresourcequota` metrics today.
Namespace ROS CSV (`ros-openshift-namespace-*.csv`) only includes `*_namespace_sum` /
`*_namespace_used` from `kube_resourcequota`.

### Current ros-ocp-backend patterns (namespace quota)

Follow these established patterns:

| Area | Reference |
|------|-----------|
| Plugin shell | [`internal/plugins/quota/plugin.go`](../../internal/plugins/quota/plugin.go) — priority 35, no ingest hook, runs after container recs |
| Engine | [`internal/engine/recommend_quota.go`](../../internal/engine/recommend_quota.go) — headroom, utilization BP, tighten/raise/optimal/none |
| Persistence | [`migrations/000085_quota_recommendations.up.sql`](../../migrations/000085_quota_recommendations.up.sql) — `quota_recommendation_sets` |
| Digest source | [`migrations/000086_namespace_quota_digest_columns.up.sql`](../../migrations/000086_namespace_quota_digest_columns.up.sql) — hard/used on `daily_namespace_digests` |
| API | `GET /api/cost-management/v1/recommendations/openshift/quota/` |

---

## Data requirements

### From koku-metrics-operator

#### New PromQL queries (proposed)

Mirror namespace quota resources (`requests.cpu`, `limits.cpu`, `requests.memory`,
`limits.memory`). Phase 1 can match namespace quota scope; storage/pod/object-count CRQ
resources are a later phase (same gap as namespace quota).

```promql
# Hard limits per CRQ (cluster-aggregated)
sum by (name) (
  openshift_clusterresourcequota_usage{
    resource="requests.cpu", type="hard"
  }
)

# Used limits per CRQ (cluster-aggregated)
sum by (name) (
  openshift_clusterresourcequota_usage{
    resource="requests.cpu", type="used"
  }
)
```

Repeat for `limits.cpu`, `requests.memory`, `limits.memory`.

**Optional (Phase B+):** `openshift_clusterresourcequota_namespace_usage` to emit
`matched_namespace_count` and to validate which namespaces roll up into each CRQ (and to
cross-check namespace quota recs).

**Selector metadata:** Query `openshift_clusterresourcequota_selector{type="match-labels"}`
(or serialize selector as JSON in one column per CRQ row). Needed for API `selector` field and
for joining to namespace-level data.

**Availability / backward compatibility:**

- If `openshift_clusterresourcequota_usage` returns no series, emit **no CRQ CSV file** (or an
  empty file with header only). Downstream must treat missing report type as “no CRQs” — not an
  error.
- Document minimum OpenShift / monitoring version where openshift-state-metrics exposes these
  metrics (verify on target OCP versions during Phase A).

#### New CSV — separate report type (recommended)

**Do not extend** the namespace ROS CSV. Grain differs:

| Report | Row grain | Key columns |
|--------|-----------|-------------|
| `ros-openshift-namespace-*.csv` | `(interval, namespace)` | `namespace`, `*_namespace_sum`, usage stats |
| **`ros-openshift-cluster-quota-*.csv` (new)** | `(interval, crq_name)` | `cluster_resource_quota`, hard/used sums, optional selector |

Proposed columns (align units with namespace quota: cores for CPU, bytes for memory):

| Column | Source |
|--------|--------|
| `report_period_start`, `report_period_end`, `interval_start`, `interval_end` | Standard ROS interval |
| `cluster_resource_quota` | CRQ `metadata.name` (`name` label) |
| `cpu_request_cluster_sum` | `openshift_clusterresourcequota_usage{resource="requests.cpu",type="hard"}` |
| `cpu_request_cluster_used` | `type="used"` |
| `cpu_limit_cluster_sum` / `cpu_limit_cluster_used` | `limits.cpu` |
| `memory_request_cluster_sum` / `memory_request_cluster_used` | `requests.memory` |
| `memory_limit_cluster_sum` / `memory_limit_cluster_used` | `limits.memory` |
| `matched_namespace_count` | `count by (name) (openshift_clusterresourcequota_namespace_usage{...})` (optional) |
| `selector_labels` | Serialized match-labels from `openshift_clusterresourcequota_selector` (optional JSON) |

Manifest / listener: register new file type (e.g. `ocp_ros_cluster_quota_usage.csv` or
operator-internal `ros-openshift-cluster-quota-*.csv`) in the same payload as other ROS reports.

#### Koku / ingress

Confirm listener routing and masu report type mapping when implementing Phase A — out of scope
for this design doc, but required before ros-ocp-backend ingest.

---

## Database schema

### Recommendation: separate table (not `scope` on `quota_recommendation_sets`)

`quota_recommendation_sets` is keyed by `(org_id, cluster_uuid, namespace)` and optimized for
namespace API filters. CRQ needs `(org_id, cluster_uuid, crq_name)` and selector metadata.

**Preferred:** `cluster_quota_recommendation_sets` (name TBD in implementation).

```sql
-- Illustrative — not migrated yet
CREATE TABLE cluster_quota_recommendation_sets (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    crq_name TEXT NOT NULL,

    selector_labels JSONB,              -- e.g. {"team": "payments"}
    matched_namespace_count INT,

    cpu_request_hard_millicores BIGINT,
    cpu_limit_hard_millicores BIGINT,
    memory_request_hard_bytes BIGINT,
    memory_limit_hard_bytes BIGINT,

    cpu_request_used_millicores BIGINT,
    cpu_limit_used_millicores BIGINT,
    memory_request_used_bytes BIGINT,
    memory_limit_used_bytes BIGINT,

    cpu_request_recommended_millicores BIGINT,
    cpu_limit_recommended_millicores BIGINT,
    memory_request_recommended_bytes BIGINT,
    memory_limit_recommended_bytes BIGINT,

    headroom_basis_points INT NOT NULL DEFAULT 12000,
    cpu_request_utilization_bp INT,
    cpu_limit_utilization_bp INT,
    memory_request_utilization_bp INT,
    memory_limit_utilization_bp INT,

    cpu_freed_millicores BIGINT,
    memory_freed_bytes BIGINT,
    estimated_savings_cents BIGINT,
    currency TEXT NOT NULL DEFAULT 'USD',

    recommendation_type TEXT NOT NULL DEFAULT 'none',
    risk_level TEXT NOT NULL DEFAULT 'none',

    last_observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (org_id, cluster_uuid, crq_name)
);
```

**Digest table (optional):** `daily_cluster_quota_digests` keyed by
`(org_id, cluster_uuid, crq_name, bucket_date, schedule_type)` — mirrors
`daily_namespace_digests` quota columns. Alternatively, store latest snapshot only on the
recommendation row if history is not required for v1.

### Relationship to namespace quota recs

- **Logical:** One CRQ covers N namespaces; each namespace may have 0–1 row in
  `quota_recommendation_sets` (today: aggregated per namespace, not per ResourceQuota object).
- **Recommended aggregate for CRQ “recommended hard” (v1):** Sum namespace-level
  `cpu_request_recommended_millicores` (and memory) for namespaces that belong to the CRQ
  (from selector + namespace list), then apply headroom at CRQ level only if not already applied
  at namespace level — **default: sum namespace recommended values without double headroom**.
- **Alternative:** Sum container `recommendation_sets` for all namespaces under the CRQ
  (independent of namespace quota plugin). More accurate when namespace quota plugin is disabled;
  duplicates logic.
- **Validation:** Compare CRQ `used` from `openshift_clusterresourcequota_usage` to sum of
  namespace `used` under the CRQ; log warning on large drift (measurement/debug, not blocking).

---

## API endpoint

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/
```

| Query param | Maps to |
|-------------|---------|
| `filter[cluster]` | `cluster_uuid` |
| `filter[cluster_resource_quota]` or `filter[crq]` | `crq_name` |
| `filter[recommendation_type]` | `tighten` \| `raise` \| `optimal` \| `none` |
| `filter[risk_level]` | `high` \| `medium` \| `low` \| `none` |
| `group_by[cluster]` | Aggregated counts / savings per cluster |

**Response item (sketch):**

```json
{
  "cluster_uuid": "...",
  "cluster_resource_quota": "team-payments-quota",
  "selector": {"team": "payments"},
  "matched_namespace_count": 12,
  "recommendation_type": "tighten",
  "risk_level": "low",
  "quota_hard": { "cpu_request_millicores": 500000, "memory_request_bytes":  ... },
  "quota_used": { ... },
  "quota_recommended": { ... },
  "utilization": { "cpu_request_percent": 35.0 },
  "capacity_freed": { "cpu_millicores": 120000, "memory_bytes": ... },
  "estimated_savings": { "value": 420.0, "units": "USD", "currency": "USD" },
  "last_observed_at": "2026-05-28T12:00:00Z"
}
```

**Settings API (Phase D):** Either extend
`/recommendations/openshift/settings/quota` with `scope=cluster` or add
`/settings/cluster-quota` sharing the same threshold keys (`headroom_percent`,
`high_risk_threshold_percent`, `medium_risk_threshold_percent`).

**Plugin gate:** `cluster-quota` in `ROS_ENABLED_PLUGINS`; 404 when disabled (same as `quota`).

---

## Plugin design

### Recommendation: separate `cluster-quota` plugin

| Option | Pros | Cons |
|--------|------|------|
| **Extend `quota` plugin** | One settings surface | Mixed routes, retention, ingest paths; namespace vs CRQ keys |
| **`cluster-quota` plugin (preferred)** | Clear API, table, phase, enablement; OCP-only | Some duplicated threshold/config code |

**Proposed registration:**

| Property | Value |
|----------|-------|
| Name | `cluster-quota` |
| Phase | 1 (infrastructure / capacity) |
| Priority | `36` (after namespace `quota` at 35, before `snapshot` at 40) |
| Routes | `GET .../cluster-quota/` |
| Retention | `cluster_quota_recommendation_sets` |
| Ingest hook | None — run after namespace quota + container recs (same as `quota`) |

### Dependencies

| Dependency | Required for v1? |
|------------|------------------|
| Container `recommendation_sets` | Yes if aggregating from workloads |
| Namespace `quota_recommendation_sets` | Preferred for v1 recommended-hard sum |
| Namespace quota CSV / digests | No for CRQ hard/used (comes from new CRQ CSV) |
| `openshift_clusterresourcequota_*` metrics | Yes |

**Can run without namespace quota plugin:** Yes, if recommended values are computed by summing
container recs for namespaces matched to each CRQ (requires namespace membership resolution).

**Clusters without CRQs:** No metrics → no CSV rows → `RunClusterQuotaRecommendations` returns
0 recs, no API errors. Existing namespace quota behavior unchanged.

---

## Recommendation algorithm (Phase C)

Reuse namespace quota classification from
[`recommend_quota.go`](../../internal/engine/recommend_quota.go):

1. Load latest CRQ snapshot (hard/used) per `crq_name`.
2. Build `recommended` bundle:
   - **v1 default:** `SUM(namespace quota_recommended_*)` for namespaces in CRQ.
   - **Utilization:** `max(crq_used, sum(namespace recommended or used))` vs `crq_hard` per resource.
3. Apply same `tighten` / `raise` / `optimal` / `none` and risk bands (shared config).
4. **Savings on `tighten`:** Freed CRQ capacity × cluster effective rates (same as namespace quota).

**Open question — savings double-counting:** Fleet dashboards summing namespace + CRQ savings
will over-count. API docs should state: CRQ savings are **team-pool** view; namespace savings are
**project** view — do not add both in one total without deduplication.

---

## Implementation plan (phased)

| Phase | Owner | Work |
|-------|-------|------|
| **A — Operator** | koku-metrics-operator | PromQL for `openshift_clusterresourcequota_usage` (+ optional selector/count); new CSV; tests; report-fields doc |
| **B — Ingest** | ros-ocp-backend | Listener/report type; ingest → `daily_cluster_quota_digests` or direct snapshot; backward compat when file absent |
| **C — Engine + API** | ros-ocp-backend | `cluster-quota` plugin; `recommend_cluster_quota.go`; `cluster_quota_recommendation_sets`; `GET .../cluster-quota/` |
| **D — Polish** | ros-ocp-backend | Settings API, OpenAPI, docs-site, IQE fixtures, unit/integration tests |

---

## Open questions

| # | Question | Current leaning |
|---|----------|-----------------|
| 1 | Does `kube_resourcequota` expose CRQ? | **No** — use `openshift_clusterresourcequota_*` |
| 2 | CRQs spanning clusters? | **N/A** — CRQ is per-cluster |
| 3 | Savings: aggregate namespace savings vs independent CRQ compute? | **Independent** CRQ tighten savings from CRQ hard − recommended; document dedup for fleet totals |
| 4 | Extend namespace CSV vs separate CSV? | **Separate CSV** (different grain) |
| 5 | Single table + `scope` column? | **Separate table** (cleaner keys and API) |
| 6 | Namespace membership for sums | `openshift_clusterresourcequota_namespace_usage` vs evaluate selector against `kube_namespace_labels` |
| 7 | Multiple CRQs selecting overlapping namespaces | Emit one row per CRQ; optional warning in API meta |
| 8 | Metric availability on older OCP | Feature-detect; skip silently if metrics missing |

---

## Comparison with namespace quota

| Aspect | Namespace Quota | ClusterResourceQuota |
|--------|-----------------|----------------------|
| Scope | Single namespace | Multiple namespaces (label selector) |
| Admin level | Namespace / project admin | Cluster / platform admin |
| Metric source | `kube_resourcequota` | `openshift_clusterresourcequota_usage` (+ related) |
| Operator CSV | `ros-openshift-namespace-*.csv` | `ros-openshift-cluster-quota-*.csv` (proposed) |
| DB table | `quota_recommendation_sets` | `cluster_quota_recommendation_sets` (proposed) |
| Plugin | `quota` (shipped) | `cluster-quota` (proposed) |
| API | `GET .../quota/` | `GET .../cluster-quota/` |
| Status | **Implemented** | **Design** |

---

## Related documentation

- [Namespace ResourceQuota recommendations](quota-recommendations.md)
- [REQ-8.4 / F37](../architecture/requirements.md) — namespace quota shipped; CRQ planned as F37b
- [Plugin phases](../architecture/plugin-phases.md)
- [Performance analysis §23.8](../architecture/performance-analysis.md)
- [openshift-state-metrics CRQ metrics](https://github.com/openshift/openshift-state-metrics/blob/master/docs/clusterresourcequota-metrics.md)
