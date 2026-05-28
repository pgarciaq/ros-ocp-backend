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

**ClusterResourceQuota** (OpenShift team/tenant quotas) is **not** implemented yet — see
[future work](#future-work) below.

---

## What it is

- **ResourceQuota:** Per-namespace caps on aggregate resource consumption
  (`requests.cpu`, `requests.memory`, `limits.*`, etc.).
- **ClusterResourceQuota (planned):** OpenShift extension that applies quota across a
  selector of namespaces — no metrics or recommendations yet.

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

1. **Operator** — koku-metrics-operator emits namespace ROS CSV with aggregated
   `kube_resourcequota` hard limits (`*_namespace_sum`) and optional used values
   (`*_namespace_used`). See [data sources](#what-data-we-already-have).
2. **Namespace digest** — `internal/ingestion/namespace.go` maps CSV columns into
   `daily_namespace_digests` (max per namespace per day). Missing `*_namespace_used`
   columns are ignored (**backward compatible** with older operator builds).
3. **Container recommendations** — `processContainerCSVNative` runs container sizing,
   writes `recommendation_sets`, then calls `engine.RunQuotaRecommendations` so quota
   sees fresh container aggregates in the same cycle when container CSV is present.
4. **Persistence** — Results upsert into `quota_recommendation_sets` (one row per
   org / cluster / namespace).

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
6. **Savings** — `tighten` rows estimate monthly savings from freed CPU/memory via Koku
   effective rates when `ROS_SAVINGS_ESTIMATES_ENABLED=true` (`estimated_savings` in API).

Implementation: [`internal/engine/recommend_quota.go`](../../internal/engine/recommend_quota.go),
[`internal/engine/quota_run.go`](../../internal/engine/quota_run.go),
[`internal/api/handlers_quota_recs.go`](../../internal/api/handlers_quota_recs.go).

### Timing and one-cycle lag

Container sums are read from PostgreSQL (`recommendation_sets`), not passed in memory
from the container plugin. In a typical payload (container CSV then namespace CSV):

1. Container digest ingest runs.
2. `RecommendWorkloadsStreaming` writes fresh `term=medium` / `engine=cost` rows.
3. Quota runs **once** at the end of `processContainerCSVNative` with those new rows.
4. Namespace digest ingest updates hard/used snapshots for the **next** quota run.

If only namespace CSV is ingested in a cycle, quota uses container recommendations
from the **previous** cycle until container metrics arrive. On first deployment,
expect one report cycle before tighten/raise signals fully reflect container-based sums.

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

- Filters: `filter[cluster]`, `filter[project]`, `filter[recommendation_type]`, `filter[risk_level]`
- Group by: `group_by[cluster]` or `group_by[project]` (aggregated counts and savings)
- Requires `quota` in `ROS_ENABLED_PLUGINS`

**Settings** (`GET` / `PUT` / `DELETE` `/api/cost-management/v1/recommendations/openshift/settings/quota`):

- GET returns merged thresholds plus `locked_fields` for env-locked parameters.
- PUT body: `headroom_percent` (0–100), `high_risk_threshold_percent` (1–100, must be
  greater than medium), `medium_risk_threshold_percent` (1–99).
- DELETE removes the per-org override; subsequent runs use env or compiled defaults.
- Disabled when the `quota` plugin is off (404).

Public reference: [docs-site feature page](../../docs-site/features/quota-recommendations.md),
[openapi.json](../../openapi.json).

---

## Future work

| Gap | Notes |
|-----|-------|
| **ClusterResourceQuota** | No `openshift_clusterresourcequota` metrics yet |
| **Storage / object counts** | PVC/service/configmap quota resources not in namespace CSV |
| **Per-quota object identity** | Aggregated per namespace; multiple ResourceQuotas per namespace are not split |
| **Notification codes** | API returns types/risk only; no Kruize-style notification catalog yet |

---

## Related documentation

- [Namespace Quota Optimization](../../docs-site/features/namespace-recommendations.md) — usage-based namespace sizing
- [Plugin Execution Phases](../architecture/plugin-phases.md) — phase and priority table
- [REQ-8.4](../architecture/requirements.md) — requirements traceability
- [Known issues](../known-issues.md) — ClusterResourceQuota and remaining gaps
