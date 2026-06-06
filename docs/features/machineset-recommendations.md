# MachineSet Recommendations (Tier 2) — Internal

**Status:** Tier 1 aggregation shipped; Tier 2 engine **planned**  
**Public doc:** [docs-site/features/machineset-recommendations.md](../../docs-site/features/machineset-recommendations.md)  
**Requirements:** [REQ-8c.4–8c.6](architecture/requirements.md)  
**Roadmap:** [node-recommendations-roadmap.md](architecture/node-recommendations-roadmap.md)

---

## Summary

Tier 2 adds a **`machineset` engine plugin** (Phase 3 Optimize) that persists
**`machineset_recommendations`** and exposes **`GET .../machinesets/{name}`** with
history. Delivery is split into **Tier 2a** (no cloud catalog) and **Tier 2b**
(catalog-driven instance family/size and cost comparison).

**Shipped today:** [`GetMachineSetRecommendations`](../../internal/api/handlers_machinesets.go)
aggregates `node_recommendations` by `machineset_name` — not the Tier 2 engine.

---

## Tier 2 capabilities (full scope)

| # | Capability | REQ | Notes |
|---|------------|-----|-------|
| 1 | Replica recommendations | REQ-8c.5 | `rec_replicas = ceil(current × util / target)`; HA floor |
| 2 | Instance family/size | REQ-8c.5, REQ-8c.6 | Smallest-fit from catalog; stranded → family switch |
| 3 | `machineset_recommendations` table | REQ-8c.11 | PK `(org_id, cluster_uuid, machineset_name)` — schema in requirements |
| 4 | Detail endpoint | REQ-8c.10 | `GET .../machinesets/{name}` + history |
| 5 | MachineSet notification codes | REQ-8c.5 | PDB (**4**), catalog (**23**, **24**), fleet health |
| 6 | Cloud instance catalog | REQ-8c.6 | `cloud_instance_catalog` + refresh goroutine |

Algorithm detail: [REQ-8c.5](architecture/requirements.md#req-8c5-tier-2--machineset-right-sizing-high--not-implemented).

---

## Phased implementation analysis

### Tier 2a — Build without cloud instance catalog

| Item | Now? | Dependencies | Value standalone |
|------|------|--------------|------------------|
| **Replica count recommendations** | **YES** | `daily_node_digests` with `machineset_name`; Tier 1 [`applyInstanceTypeConsolidation`](../../internal/engine/recommend_nodes.go) fleet math; sum `node_count_reduction` per MachineSet (already exposed on list API) | Platform teams get explicit `rec_replicas` without catalog; aligns with Cluster Autoscaler “right-size node pool” workflows |
| **`machineset_recommendations` table** | **YES** | Migration; [`internal/model/machineset_recommendation.go`](../../internal/model/machineset_recommendation.go) extended for persisted fields | Decouples list from runtime GROUP BY; enables filters (`is_underutilized`, etc.) and recalc idempotency |
| **`GET .../machinesets/{name}`** | **YES** | Handler + model; member nodes from `node_recommendations` | UI drill-down; API parity with `GET .../nodes/{node}` |
| **History / trend tracking** | **YES** (partial) | Snapshot `machineset_recommendations` per recalc or `recommendation_history` extension with `resource_type=machineset` | Trend charts; validates whether replica advice is stable |
| **MachineSet-level confidence** | **YES** | `confidence_level = min(member data_days) / window_days` | Reduces false positives on newly scaled MachineSets |
| **Heterogeneous fleet detection** | **YES** | Distinct `(max_cpu_allocatable_mc, max_mem_allocatable_kib)` per MachineSet in digests | Catches mixed instance types or label drift in one MachineSet |
| **Fleet health / scaling notifications** | **YES** (partial) | Aggregate Tier 1 `classification`, idle/zombie counts, `pod_scheduling_headroom` | Warn before scale-down: HA floor, low headroom, idle-heavy fleet |
| **PDB notification (code 4)** | **YES** (notification only) | REQ-8c.4 PDB metrics from operator; **does not** change `rec_replicas` algorithmically | REQ-8c.5 explicitly defers PDB-aware replica math |

**Tier 2a engine sketch:**

1. Read `daily_node_digests` + `node_recommendations` for `(org_id, cluster_uuid, machineset_name)`.
2. Compute fleet `cpu_util_p95`, `mem_util_p95` (weighted by allocatable).
3. Set `current_replicas` = node count (or operator `machineset_replicas` when REQ-8c.4 lands).
4. Apply replica formula + `ROS_MIN_MACHINESET_REPLICAS` + Tier 1 consolidation hysteresis.
5. Upsert `machineset_recommendations`; attach notifications (heterogeneous fleet, PDB caveat, HA floor).
6. Extend list API to read from table (backward compatible with aggregation during migration).

**Effort estimate (Tier 2a only):** ~1–1.5 weeks (migration, plugin skeleton, detail handler, notifications).

### Tier 2b — Requires cloud instance catalog

| Item | Now? | Dependencies | Value standalone |
|------|------|--------------|------------------|
| **Instance family/size recommendations** | **NO** | `cloud_instance_catalog`; [`instance-type` plugin](architecture/plugin-phases.md); region/provider from cluster source | Smallest-fit and family switches **not** in fleet today |
| **Cost comparison** | **NO** | Catalog pricing + [cost-integration.md](architecture/cost-integration.md) `effective_rates` | `$` delta for type migration |
| **Stranded family switch (`m5`→`c5`/`r5`)** | **NO** (beyond Tier 1) | Catalog `category` / `family` | Tier 1 `suggested_instance_type` only compares in-cluster ratios |
| **Deprecated instance handling** | **NO** | Catalog membership test; codes **23**, **24** | Prevents bogus “switch because unlisted” recs |
| **Cross-cloud alternative mapping** | **NO** | Full catalog per provider/region | AWS Compute Optimizer–style alternatives |

**Effort estimate (Tier 2b):** ~1–1.5 weeks after catalog (refresh job, instance-type plugin, wire into machineset plugin).

**Combined Tier 2 (2a + 2b):** ~2–3 weeks — matches [roadmap](architecture/node-recommendations-roadmap.md#estimated-effort).

---

## What Tier 2 does **not** include

- **Tier 3 MachineAutoscaler** min/max tuning (REQ-8c.7) — needs replica time-series
- **PDB/scheduling simulation** — changes replica math; separate engine work in roadmap
- **Bare metal / SNO / NULL `machineset_name`** — Tier 1 only
- **Automated apply** — advisory; see [hpa-vpa-deployment-modes.md](architecture/hpa-vpa-deployment-modes.md)

---

## Implementation checklist (suggested order)

### Tier 2a

1. Migration: `machineset_recommendations` (+ optional history table / partition)
2. `internal/plugins/machineset/` — Phase 3 plugin, register in [`plugins.go`](../../internal/plugins/plugins.go)
3. Replica + confidence + heterogeneous detection in engine
4. Persist on recalc batch (same cadence as `recommendNodes`)
5. `GET .../machinesets/{name}` handler
6. Switch list API to table-backed rows (feature flag optional)
7. Emit reserved notifications; document in [notification-codes.md](architecture/notification-codes.md)
8. IQE fixtures for MachineSet detail

### Tier 2b

1. `cloud_instance_catalog` migration + refresh job (REQ-8c.6)
2. `instance-type` plugin (catalog load, smallest-fit)
3. Wire `rec_instance_type`, `savings_vcpus`, `estimated_savings_cents` on upsert
4. Catalog notifications **23**, **24**
5. On-prem: skip instance-type fields when catalog empty

---

## References

- [node-recommendations-roadmap.md](architecture/node-recommendations-roadmap.md)
- [plugin-phases.md](architecture/plugin-phases.md) — `machineset` in Phase 3
- [handlers_machinesets.go](../../internal/api/handlers_machinesets.go) — Tier 1 list
- [requirements.md § REQ-8c.5](architecture/requirements.md#req-8c5-tier-2--machineset-right-sizing-high--not-implemented)
