# Namespace & Cluster Quota Recommendations (Planned)

Right-size Kubernetes **ResourceQuota** (namespace-level) and **ClusterResourceQuota**
(cluster-level) objects based on observed usage, peak demand, and container-level
recommendation aggregates.

**Status:** Not implemented. Planned as a Phase 1 plugin (`quota`, priority ~35)
between PVC (30) and snapshot (40). See [plugin-phases.md](../architecture/plugin-phases.md).

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
would add a second pass: compare digest aggregates and container recommendation sums
against the same hard-limit series (and eventually `type='used'` consumption).

---

## What's missing

| Gap | Notes |
|-----|-------|
| **`type='used'` metrics** | Hard limits are collected; current **used** quota consumption is not in the ROS CSV |
| **ClusterResourceQuota** | No `openshift_clusterresourcequota` (or equivalent) queries today |
| **Storage / object counts** | `persistentvolumeclaims`, `services`, `configmaps`, etc. not in ROS namespace quota columns |
| **Per-quota object identity** | Namespace CSV aggregates by namespace; multiple ResourceQuotas per namespace need name/UID dimension |
| **Dedicated API / notifications** | No list endpoint or notification codes for quota-specific guidance |

Industry gap analysis: [performance-analysis.md §23.8](../architecture/performance-analysis.md#238-priority-8-resourcequota-recommendations).

---

## How it would work

1. **Ingest** — Extend namespace CSV ingest (or add `quota` CSV type) to persist hard
   and used limits per `(namespace, quota_name, resource)`.
2. **Aggregate usage** — For each namespace, sum container recommendation outputs
   (from the container plugin) and observed peak usage from namespace digests.
3. **Compare** — For each quota resource:
   - If `used` or recommended aggregate ≪ hard limit (e.g. &lt; 50%): recommend **lowering** quota (frees capacity for other teams).
   - If `used` or recommended aggregate &gt; ~80% of hard limit: warn **quota may block scaling**; suggest raising limit or rightsizing workloads first.
   - Apply headroom factor (e.g. 1.2–1.3× on recommended aggregate) for proposed new hard values.
4. **Cluster scope** — Phase 2: roll up namespaces matched by ClusterResourceQuota
   selectors and apply the same logic at cluster quota boundaries.
5. **Savings signal** — Over-provisioned quotas contribute to **freed capacity** savings
   (not direct dollar line items unless mapped via node/cluster effective rates); combine
   with idle namespace waste in fleet summaries.

---

## Implementation approach

| Aspect | Plan |
|--------|------|
| **Plugin phase** | Phase 1 (Produce) — generates standalone quota recommendation rows |
| **Priority** | ~35 (after `node`/`pvc` at 30, before `snapshot` at 40) |
| **Dependencies** | Namespace digests + container plugin outputs (namespace plugin at 90 runs later for idle rollup; quota does not require namespace rec rows) |
| **Operator changes** | Add `kube_resourcequota{type='used',...}` queries; optional ClusterResourceQuota metrics |
| **Effort** | Moderate — reuse existing ingest patterns; new comparison algorithm and API surface |

Execution ordering reference: [plugin-phases.md](../architecture/plugin-phases.md).

---

## Related documentation

- [Namespace Quota Optimization](../../docs-site/features/namespace-recommendations.md) — shipped usage-based namespace sizing
- [Plugin Execution Phases](../architecture/plugin-phases.md) — future plugin table
- [REQ-8.4](../architecture/requirements.md) — requirements traceability
- [Known issues — ResourceQuota](../known-issues.md) — MVP deferral notes
