# Namespace & Cluster Quota Recommendations

Right-size Kubernetes **ResourceQuota** (namespace-level) objects based on observed
usage, configured hard limits, and aggregated container recommendation totals.

**Status:** Implemented. Phase 1 plugin `quota` (priority 35) between PVC (30) and
snapshot (40). API: `GET /api/cost-management/v1/recommendations/openshift/quota/`.
See [plugin-phases.md](../architecture/plugin-phases.md).

This is distinct from the shipped **[namespace](../../docs-site/features/namespace-recommendations.md)** plugin,
which recommends ideal CPU/memory totals from namespace usage digests. The `quota` plugin
would compare those aggregates (and actual usage) against **configured** quota hard limits
and consumption, then advise operators to tighten or loosen existing quota objects.

---

## What it is

- **ResourceQuota:** Per-namespace caps on aggregate resource consumption
  (`requests.cpu`, `requests.memory`, `limits.*`, `pods`, storage classes, etc.).
- **ClusterResourceQuota:** OpenShift extension that applies quota across a selector
  of namespaces (team/tenant boundaries at cluster scope).

Recommendations would suggest adjusted hard limits with a configurable headroom margin,
flag mismatches between quota and real usage, and surface risk of admission failures
when usage approaches limits.

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

## What data we already have

The [koku-metrics-operator](https://github.com/project-koku/koku-metrics-operator)
already emits namespace-level quota **hard limits** in the ROS namespace CSV via
Prometheus recording queries:

- `ros:cpu_request_namespace_sum` → `kube_resourcequota{resource='requests.cpu', type='hard'}`
- `ros:cpu_limit_namespace_sum` → `kube_resourcequota{resource='limits.cpu', type='hard'}`
- `ros:memory_request_namespace_sum` → `kube_resourcequota{resource='requests.memory', type='hard'}`
- `ros:memory_limit_namespace_sum` → `kube_resourcequota{resource='limits.memory', type='hard'}`

Source: `internal/collector/queries.go` in koku-metrics-operator.

The **namespace** plugin ingests these columns into `daily_namespace_digests` and
produces rightsizing recommendations from usage percentiles. The planned **quota** plugin
compares digest hard/used snapshots and container recommendation sums against
configured quota limits.

---

## What's still missing (future work)

| Gap | Notes |
|-----|-------|
| **ClusterResourceQuota** | No `openshift_clusterresourcequota` metrics yet |
| **Storage / object counts** | PVC/service/configmap quota resources not in namespace CSV |
| **Per-quota object identity** | Aggregated per namespace; multiple ResourceQuotas per namespace are not split |
| **Notification codes** | API returns types/risk only; no Kruize-style notification catalog yet |

---

## How it works

1. **Ingest** — Namespace CSV columns `*_namespace_sum` map to ResourceQuota `type=hard`;
   optional `*_namespace_used` columns map to `type=used`. Values are stored on
   `daily_namespace_digests` (max per day).
2. **Aggregate** — Sum container `rec_*` request/limit columns per namespace
   (`term=medium`, `engine=cost`) from `recommendation_sets` (previous cycle until
   container CSV in the same payload is processed; see timing below).
3. **Compare (signal C)** — Utilization and risk use the **greater** of quota `used`
   and container recommendation sums vs hard limits. Recommended hard values apply
   headroom (default 120%, `ROS_QUOTA_HEADROOM_PERCENT=20`).
4. **Classify** — `tighten` when recommended &lt; hard; `raise` when utilization ≥
   high-risk threshold (default 80%); otherwise `optimal`.
5. **Savings** — Tighten rows estimate monthly savings from freed CPU/memory capacity
   via Koku effective rates (`estimated_savings` in API).

Configuration: `ROS_QUOTA_HEADROOM_PERCENT`, `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT`,
`ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT`.

### Timing and one-cycle lag

Container sums are read from PostgreSQL (`recommendation_sets`), not passed in memory
from the container plugin. In a typical payload (container CSV then namespace CSV):

1. Container digest ingest runs (ingest hooks do not run quota on container).
2. `RecommendWorkloadsStreaming` writes fresh `term=medium` / `engine=cost` rows.
3. Quota runs at the end of container processing with those new rows.
4. Namespace digest ingest runs; the quota ingest hook runs again with updated
   hard/used snapshots from the namespace CSV.

If only namespace CSV is ingested in a cycle, quota uses container recommendations
from the **previous** cycle until container metrics arrive. On first deployment,
expect one report cycle before tighten/raise signals use container-based sums.

Implementation: [`internal/plugins/quota/`](../../internal/plugins/quota/),
[`internal/engine/recommend_quota.go`](../../internal/engine/recommend_quota.go),
[`internal/api/handlers_quota_recs.go`](../../internal/api/handlers_quota_recs.go).

---

## Related documentation

- [Namespace Quota Optimization](../../docs-site/features/namespace-recommendations.md) — shipped usage-based namespace sizing
- [Plugin Execution Phases](../architecture/plugin-phases.md) — future plugin table
- [REQ-8.4](../architecture/requirements.md) — requirements traceability
- [Known issues — ResourceQuota](../known-issues.md) — MVP deferral notes
