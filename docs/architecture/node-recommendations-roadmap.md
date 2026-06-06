# Node Recommendations Roadmap — Tier 2 & Tier 3

**Status:** Tier 1 complete; Tier 2/3 planned  
**Related:** [Node consolidation (Tier 1)](../../docs-site/features/node-recommendations.md), [REQ-8c in requirements.md](requirements.md), [notification codes](notification-codes.md) (codes 14, 16–17 reserved for Tier 3)

Tier 1 is **shipped**, including:

| Capability | Status |
|------------|--------|
| `GET .../recommendations/openshift/nodes` (list, filters, CSV) | Done |
| Dual engines, term windows, fleet consolidation | Done |
| Idle / zombie detection + settings | Done |
| `pod_capacity`, `pod_scheduling_headroom`, notification **74** | Done |
| `filter[stranded_resource]`, `filter[instance_type]`, `filter[machineset_name]` | Done |
| `suggested_instance_type` / `instance_type_reason` (in-cluster ratio hints) | Done |
| `POST .../internal/recalculate-savings` after cost model changes | Done |
| Savings summary `?term=` alignment | Done |
| Dedicated `GET .../nodes/{node}` path | Done |
| `filter[engine]=cost\|performance` on node list | Done — limits nested `recommendation_engines`; cost vs performance target utilization differs (80% vs 55%) |
| Node recommendation history time series | **Future** (Tier 2) |
| Cloud instance catalog (AWS/Azure/GCP specs + pricing) | **Future** (Tier 2) |
| `GET .../machinesets/{name}` detail | **Future** (Tier 2) |
| PDB/scheduling-aware consolidation (safe to auto-execute) | **Future** (Tier 2) |
| GPU-aware node consolidation | **Future** (Tier 2) |
| MachineSet engine + `machineset_recommendations` table | **Future** (Tier 2) |
| MachineAutoscaler optimization (codes **14**, **16**, **17**) | **Future** (Tier 3) |
| Autonomous scaling recommendations | **Future** (Tier 3) |
| Operator MachineSet replica time-series + autoscaler CR metrics | **Future** (Tier 3) |

This document describes Tier 2 (MachineSet) and Tier 3 (MachineAutoscaler).

**Business hours:** Not applicable to node recommendations. Business-hours schedules
apply to container and namespace recommendations only. Node idle classification uses
`idle_state` (`active` / `idle` / `zombie`) and tenant-configurable idle/zombie thresholds
via `/settings/node`.

---

## Tier overview

Three tiers differ by **how much automation is safe**, not by whether recommendations exist.

| Tier | Status | Purpose | Automation posture | Primary API |
|------|--------|---------|-------------------|-------------|
| **1** | **Shipped** | Per-node classification, sizing, **advisory** fleet consolidation | **Human review required** — no PDB/scheduling simulation | `GET .../recommendations/openshift/nodes` |
| **2** | Planned (partial: machinesets list) | MachineSet replica + instance-family right-sizing; **PDB/scheduling-aware** consolidation | **Safe to auto-execute** (with guardrails) once simulation ships | `GET .../recommendations/openshift/machinesets` (+ catalog-driven engine) |
| **3** | Planned | MachineAutoscaler min/max and scaling behavior | **Autonomous scaling** — tune autoscaler bounds/policies | Extension of machinesets API or dedicated endpoint |

| Tier | Actionable unit | Est. remaining effort |
|------|-----------------|----------------------|
| **1** | Individual node (+ fleet grouping for advisory `node_count_reduction`) | — |
| **2** | MachineSet | **~2–3 weeks** for catalog + replica engine (aggregation API **done**) |
| **3** | MachineSet + MachineAutoscaler | **~4–6 weeks** after Tier 2 |

### Tier 1 — Advisory consolidation with safety gates (shipped)

- **Advisory only:** `node_count_reduction` and fleet sums are FinOps signals for platform review, not drain/scale commands.
- **Safety gates today:** `pod_scheduling_headroom` vs `pod_headroom_consolidation_gate` suppresses consolidation on nodes with little spare pod capacity; idle/zombie and performance-engine headroom multipliers add further guards.
- **Does not include:** PDB checks, scheduling simulation, or MachineSet replica metrics from the operator.
- **Industry alignment:** Same advisory model as AWS Compute Optimizer, Kubecost, and CAST AI (recommendations without automated apply).

### Tier 2 — PDB/scheduling-aware (“safe to auto-execute”)

- **Goal:** Recommendations that account for whether workloads **can** reschedule after node removal while respecting PDBs, taints, and affinity.
- **Adds:** Operator collection of PDB/toleration/affinity snapshots; engine placement simulation (~O(pods × nodes) per candidate); MachineSet **replica count** and catalog-driven instance family/size changes.
- **Outcome:** Consolidation and replica reductions can be exposed with a **safe-to-apply** (or equivalent) confidence tier for automation integrations — still subject to org change control.

### Tier 3 — Autoscaler integration (“autonomous scaling”)

- **Goal:** Tune **MachineAutoscaler** `minReplicas` / `maxReplicas` (and optionally policy hints) from historical replica vs utilization time series.
- **Depends on:** Tier 2 MachineSet identity and operator metrics (`machineset_replicas`, desired/available, HPA/autoscaler state).
- **Outcome:** Bound recommendations and saturation/idle/flapping notifications suitable for policy-driven or semi-autonomous scaling — highest risk; conservative heuristics and manual-review messaging expected first.

```mermaid
flowchart LR
  T1[Tier 1: Advisory recs + headroom gate] --> T2[Tier 2: PDB/scheduling simulation + MachineSet recs]
  T2 --> T3[Tier 3: Autoscaler bounds and behavior]
  Op[koku-metrics-operator] --> T1
  Op --> T2
  Op --> T3
```

---

## Consolidation model — current scope and limitations

Tier 1 consolidation (`applyInstanceTypeConsolidation` in [recommend_nodes.go](../../internal/engine/recommend_nodes.go)) is **advisory only** — see [Tier 1 — Advisory consolidation](#tier-1--advisory-consolidation-with-safety-gates-shipped) above and the [feature doc](../../docs-site/features/node-recommendations.md#fleet-consolidation--advisory-only-tier-1).

### Current scope (Tier 1)

- Advisory signal: “you likely have N excess nodes in this fleet”
- Based on aggregate P95 utilization across the fleet group
- Groups nodes by MachineSet (when labeled) → `instance_type` → capacity bucket
- **Pod scheduling headroom gate** (`podSchedulingBlocksConsolidation`) blocks consolidation when `(pod_capacity − pod_count) / pod_capacity` is below `pod_headroom_consolidation_gate` (default 0.15)
- Does **not** account for: PodDisruptionBudgets, scheduling constraints (taints/tolerations/affinity), DaemonSet overhead, or actual pod placement feasibility
- **Action path:** platform team reviews recommendation → checks PDBs/constraints manually → scales down MachineSet or removes nodes
- Aligns with FinOps advisory tools (AWS Compute Optimizer, Kubecost, CAST AI recommendations) — none perform PDB/scheduling simulation in Tier-1-style products

### PDB/scheduling-aware consolidation (Tier 2 — safe to auto-execute)

Additional data required:

| Data | Source | Storage estimate |
|------|--------|------------------|
| PodDisruptionBudgets | `policy/v1` PDB resources | ~1 KB per PDB × namespaces |
| Node taints | `node.spec.taints` | Already in node labels CSV |
| Pod tolerations | `pod.spec.tolerations` | ~100 B per pod × pods |
| Node/pod affinity rules | `pod.spec.affinity` | ~200 B per pod with affinity |
| DaemonSet pod list | `apps/v1` DaemonSet resources | ~50 B per DS × nodes |

Processing estimate:

- For each candidate node removal, simulate whether non-DaemonSet pods can reschedule to remaining nodes while respecting PDBs, taints, and affinity
- Essentially a **scheduling simulation** (mini kube-scheduler) — ~O(pods × nodes) per candidate
- Example: 100-node cluster, 5000 pods → ~500K placement checks per consolidation evaluation
- Latency: seconds per evaluation (acceptable at recommendation time, not real-time)

**Storage:** ~50 MB additional per large cluster (PDB/affinity/toleration snapshots)  
**Processing:** ~5–10 s additional per cluster recommendation cycle

**Verdict:** Feasible for Tier 2 but requires operator enhancements (collect PDB + affinity data) and significant engine work. This is the tier that unlocks **safe-to-auto-execute** consolidation; Tier 1 intentionally stops at advisory signals plus the pod headroom gate.

---

## Fleet instance type suggestions (Tier 1 vs cloud catalog)

### Implemented (Tier 1 — in-cluster fleet)

When `stranded_resource` is `cpu` or `memory`, [RecommendNodes](../../internal/engine/recommend_nodes.go) compares the node’s allocatable CPU:memory ratio to **distinct instance types already observed** in `daily_node_digests` for that cluster:

- **CPU-stranded:** suggest a type with a **lower** CPU:memory allocatable ratio (compute-leaning shape already in the fleet)
- **Memory-stranded:** suggest a type with a **higher** CPU:memory ratio
- Persisted on `node_recommendations.suggested_instance_type` / `instance_type_reason` (migration **000123**)
- Exposed on list and `GET .../nodes/{node}` detail APIs

If no strictly better in-cluster type exists, fields remain empty; stranded directional notifications still apply.

### Future work — full cloud catalog (Tier 2+)

**Status:** Planned enhancement — **no delivery timeline** (tracked here and in [node recommendations feature](../../docs-site/features/node-recommendations.md#future-external-cloud-instance-catalog)).

Not implemented in Tier 1. A full catalog path would:

- Query an external instance catalog (AWS EC2, Azure VM, GCP Compute pricing APIs, or a bundled catalog table — see REQ-8c.6 in [requirements.md](requirements.md))
- Recommend instance types **not yet present** in the cluster
- Factor in on-demand and reserved **pricing** for cost-optimized family/size changes
- Require **provider-specific** catalog loaders and refresh jobs

Tier 2 MachineSet recommendations will reuse catalog data for replica and instance family changes; Tier 1 fleet-only suggestions avoid catalog dependency and stay accurate for homogeneous on-prem / single-family clusters.

---

## Tier 2 — MachineSet right-sizing

### Goal

Group nodes by **MachineSet** (not only by `instance_type`) and recommend **replica count** and **instance family/size** changes at the MachineSet level — the unit cluster admins actually change in IPI clusters.

Individual node recommendations (Tier 1) remain informational; MachineSet recommendations are the actionable consolidation and right-sizing surface.

### What it adds

| Capability | Description |
|------------|-------------|
| **MachineSet grouping** | Aggregate utilization, requests, and capacity across all nodes sharing a `machineset_name` |
| **Replica count** | e.g. “reduce from 5 to 3 replicas” based on sustained P95 utilization vs target (default ~70%, configurable) |
| **Instance family/size** | e.g. “switch from `m5.xlarge` to `m5.large`” when peak usage fits a smaller catalog entry; family changes for stranded CPU/memory (e.g. `m5` → `c5` / `r5`) |
| **API** | `GET /api/cost-management/v1/recommendations/openshift/machinesets` — **Done** (GROUP BY aggregation over `node_recommendations`; filters, pagination, RBAC) |
| **Persistence** | Optional future `machineset_recommendations` table for catalog-driven replica/instance-family recs (schema sketched in [requirements.md](requirements.md#req-8c5-tier-2--machineset-right-sizing-high--not-implemented)) |

### Prerequisites and implementation steps

1. **Operator (koku-metrics-operator)**  
   - Collect `machine.openshift.io/machine-set` (or equivalent) from node labels via Prometheus.  
   - Emit `machineset_name` on ROS container CSV (production path). **Nise** also generates `machineset_name` and `node_capacity_pods` for test data.  
   - Optional for Tier 2 core: MachineSet replica counts (`machineset_replicas`, `desired`/`available`) — required before Tier 3.

2. **Ingestion (ros-ocp-backend)** — **done for Tier 1**  
   - Parse `machineset_name` / `node_capacity_pods` from ROS CSV into `daily_node_digests`.  
   - Digest aggregation retains `machineset_name`, `instance_type`, and `pod_capacity`.

3. **Engine**  
   - New **`machineset` plugin** (or extension of node plugin phase):  
     - Group digests by `(org_id, cluster_uuid, machineset_name)`.  
     - Compute fleet-level CPU/memory P95, replica recommendation, instance type from catalog.  
   - Reuse Tier 1 stranded-resource signals for family recommendations.  
   - Integrate **cloud instance catalog** (REQ-8c.6): `cloud_instance_catalog` table, public pricing/spec APIs (AWS bulk JSON, Azure Retail Prices, GCP `machineTypes.list`), optional EC2 API when IAM allows.

4. **API**  
   - List/detail handlers mirroring node patterns: `cluster_uuid`, `machineset_name`, `current_instance_type`, `is_underutilized`, pagination, `estimated_monthly_savings`.

5. **Cloud catalog**  
   - Lookup vCPU/RAM by `instance_type` for “smallest fit” and cost comparison; on-prem may skip instance-type *changes* when catalog is empty (capacity-based logic still applies).

### Limitations

- **Machine API / MachineSets only:** Helps IPI-provisioned clusters using MachineSets. Does **not** help bare metal, single-node OpenShift (SNO), or UPI/manual nodes without `machineset_name` (those nodes stay Tier 1 only; `machineset_name` is `NULL` → skip Tier 2/3 for that node).  
- **PDB / drain safety:** Tier 2 recommends counts; PDB-aware automation is notification-only (see REQ-8c.5 in requirements).  
- **HA floor:** Configurable minimum replicas (e.g. `ROS_MIN_MACHINESET_REPLICAS`, default 2).

### Phased delivery: Tier 2a vs Tier 2b

Tier 2 is intentionally split so replica guidance, persistence, and API depth can ship
**before** the cloud instance catalog (REQ-8c.6).

**Implementation spec (authoritative):** [machineset-recommendations.md](../features/machineset-recommendations.md)  
**Public summary:** [docs-site/planned-features/machineset-recommendations.md](../../docs-site/planned-features/machineset-recommendations.md)

#### Tier 2a — No catalog required (~1–1.5 weeks) — READY TO IMPLEMENT

| Item | Spec section | Key design |
|------|--------------|------------|
| Replica count engine | §1 | `recommended = current - SUM(node_count_reduction)`; floor ≥ 1 |
| `machineset_recommendations` table | §2 | PK `(org_id, cluster_uuid, machineset_name, term, engine)` |
| `GET .../machinesets/{name}` | §3 | `cluster_uuid` required when ambiguous; member nodes + history |
| History | §4 | `machineset_recommendation_history`; insert on replica/savings change |
| MachineSet confidence | §5 | `MIN(member.confidence_level)` |
| Heterogeneous fleet | §6 | Capacity variance > 10% → code **77** |
| Notifications **77–79** | §7 | Scale-down (**78**), optimal (**79**); **76** stays on nodes |
| Keyset pagination | §8 | **Shipped** on list; extend for `order_by` |
| CSV export | §9 | **Shipped**; switch data source to table |
| Filters + `order_by` | §10–11 | `recommendation_type`, `term`, savings/name/replica sort |

10 acceptance criteria (AC-1–AC-10) in the spec.

#### Tier 2b — Catalog required (~1–1.5 weeks after catalog)

| Item | Spec section | Key design |
|------|--------------|------------|
| Instance type recommendation | Tier 2b §1 | Smallest-fit from catalog vs peak usage × 1.2 headroom |
| Family migration | Tier 2b §2 | `m`/`c`/`r`/`p` families from stranded-resource profile |
| Cost comparison | Tier 2b §3 | `replicas × hourly_rate × 730`; 20% hysteresis |
| `InstanceCatalog` interface | Tier 2b §4 | AWS/Azure/GCP sources; daily refresh; Tier 2a fallback |

7 acceptance criteria (AC-B1–AC-B7) in the spec.

**On-prem:** Tier 2a still applies when catalog is empty; Tier 2b instance-type fields stay null.

### Estimated effort

**~2–3 weeks** total (Tier 2a + Tier 2b), assuming Tier 1 and cost integration remain stable.
Operator `machineset_name` ingest is **done**; optional REQ-8c.4 replica metrics improve validation before Tier 3.

---

## Tier 3 — MachineAutoscaler optimization

### Goal

Analyze **historical scaling patterns** (replica count over time vs actual demand) and recommend tighter **`minReplicas` / `maxReplicas`**, flag misconfigured autoscalers, and optionally suggest scaling policy tuning.

### What it adds

| Capability | Description |
|------------|-------------|
| **Historical scaling** | Time-series of `machineset_replicas` / desired / available vs utilization |
| **Bound recommendations** | Tighter `minReplicas` / `maxReplicas` when sustained behavior shows headroom |
| **Saturated autoscaler** | `current_replicas == maxReplicas` most of the window → raise max or enlarge instance type (notification **14** `AUTOSCALER_SATURATED`) |
| **Idle autoscaler** | `current_replicas == minReplicas` most of the window → lower min (code **75** reserved; code **15** is **`NODE_IDLE`** for node idle/zombie, not autoscaler) |
| **Never scales / always at max** | Flag autoscalers that never leave min (min too high) or peg max (max too low or instance too small) |
| **Policy hints (optional)** | Cool-down, scale-down delay, stabilization — research-heavy; may ship as notifications first |
| **API** | New endpoint or fields on `.../machinesets` (e.g. `autoscaler_min_recommended`, `is_saturated`, `is_flapping`) |

### Prerequisites

1. **Tier 2 complete** — MachineSet identity, grouping, and replica metadata must be reliable.  
2. **Operator** — Collect MachineAutoscaler CR specs (`min`, `max`, current replicas) and ideally scaling events or hourly replica snapshots (see REQ-8c.4 Prometheus queries in [requirements.md](requirements.md)).  
3. **Engine** — Windowed analysis (e.g. % of days at min/max, peak/trough replica spread vs CPU/memory P95).  
4. **API** — Expose autoscaler state and recommendations alongside MachineSet rows.

### Complexity note

Tier 3 is **more research-oriented** than Tiers 1–2: recommendations depend on **scheduling and burst patterns**, not only average utilization. Incorrect min/max changes can cause outages; expect conservative heuristics, strong notifications, and manual-review messaging before automated apply.

### Estimated effort

**~4–6 weeks** after Tier 2, depending on operator metrics quality and policy scope (bounds-only vs full MachineAutoscaler spec suggestions).

---

## Schema and API sketch (planned)

Already partially reserved in migrations and requirements:

- `daily_node_digests.machineset_name` — **exists**; operator population **done**.  
- `node_recommendations.machineset_name` — **exists** for correlation.  
- `machineset_recommendations` — **not created**; full DDL in [machineset-recommendations.md §2](../features/machineset-recommendations.md#2-database-schema--machineset_recommendations).  
- `machineset_recommendation_history` — **not created**; DDL in [§4](../features/machineset-recommendations.md#4-history--machineset_recommendation_history).

**Shipped list API:** `filter[cluster]`, `filter[machineset_name]`, keyset pagination, CSV.

**Tier 2a list additions:** `filter[recommendation_type]`, `filter[term]`, `order_by`.

**Tier 3 list additions (future):** `is_saturated`, `is_idle`, `is_flapping`.

---

## Relationship to Tier 1 today

Tier 1 already groups fleet consolidation by **`instance_type`** when the operator provides it. Tier 2 replaces that grouping key with **MachineSet** for clusters where labels exist, and adds replica + catalog-driven instance changes. Nodes without `machineset_name` continue to use Tier 1 behavior only.

---

## References

- [requirements.md — Phase 8c](requirements.md) (REQ-8c.4–8c.7, schemas, algorithms)  
- [machineset-recommendations.md](../features/machineset-recommendations.md) — Tier 2a/2b phasing (internal)  
- [docs-site/planned-features/machineset-recommendations.md](../../docs-site/planned-features/machineset-recommendations.md) — planned product doc (public)  
- [notification-codes.md](notification-codes.md) — codes 14, 16–17 reserved for autoscaler Tier 3  
- [cost-integration.md](cost-integration.md) — node savings; MachineSet savings will extend the same `effective_rates` patterns  
- [configurability.md](configurability.md) — future `ROS_MIN_MACHINESET_REPLICAS`, catalog refresh interval
