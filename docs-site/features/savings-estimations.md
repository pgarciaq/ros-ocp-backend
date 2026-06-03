# Savings Estimations

ROS estimates **monthly savings** (or additional monthly cost for upsize recommendations)
by comparing current resource allocation to recommended targets and applying rates from
the cluster's Koku cost model.

## Overview

- **Rightsizing savings:** Delta between current and recommended CPU, memory, storage, or node capacity × configured rates, normalized to **730 hours/month**.
- **Idle waste:** For idle or abandoned workloads, **100%** of current allocation cost is treated as recoverable if the workload is terminated.
- **Negative savings:** When recommendations increase requests, the savings field is negative — an **additional monthly cost** for reliability or performance.

Rates are fetched from Koku Masu `GET /api/cost-management/v1/effective_rates/` during ingestion (container, node, PVC, snapshot, quota) or at API read time (GPU). See [Cost Integration](../architecture/cost-integration.md) for formulas.

**Cost rate source:** ROS does not expose a standalone cost-rate API. Update the OCP cost model in Koku; rates refresh on the next ingestion cycle or via internal savings recalculation when enabled.

## Per-plugin summary

| Plugin | API field | When computed | Notes |
|--------|-----------|---------------|-------|
| **Container** | `estimated_monthly_savings` | Ingestion | Cost engine, medium term in list views; model + infra/distributed overhead; code **25** when no cost data |
| **Namespace** | — | — | CPU/memory sizing targets only; **no dollar savings** |
| **Node** | `estimated_monthly_savings` (per engine) | Ingestion | `cost` (80% target) and `performance` (55%); consolidation + right-sizing |
| **GPU** | `estimated_monthly_gpu_savings`, `estimated_monthly_timeslicing_savings` | API read | MIG, idle GPU, node time-slicing; not in fleet rollup |
| **PVC** | `estimated_monthly_savings` | Ingestion | Oversized: `(current − recommended) × storage rate`; **orphaned:** full `current × storage rate` (deletion) |
| **VM** | `estimated_monthly_savings` | Ingestion | Rightsizing vs current VM allocation |
| **Quota** | `estimated_savings` | Ingestion | Tighten rows only; **excluded** from fleet `savings-summary` |
| **Cluster-quota (CRQ)** | `estimated_savings` / `savings_dollars_monthly` | Ingestion | Tighten rows only; **excluded** from fleet rollup |
| **Snapshot** | `estimated_monthly_cost` (recoverable) | Ingestion | `restore_size × cost_per_gib_month`; flat rate default **$0.05/GiB** until [COST-7523](https://redhat.atlassian.net/browse/COST-7523) |

## Fleet `savings-summary`

`GET /api/cost-management/v1/recommendations/openshift/savings-summary`

Aggregates **persisted** savings for the organization:

| Query param | Default | Effect |
|-------------|---------|--------|
| `engine` | `cost` | Container, node, and VM totals use `cost` or `performance` engine rows |
| `term` | `medium` | Aligns with list APIs (`short`, `medium`, `long`) |

**Included plugins:** container, node, PVC, snapshot, VM.

**Exclusions:**

- **Quota / cluster-quota** — per-recommendation only; omitted to avoid double-counting namespace capacity against container savings.
- **GPU** — `by_plugin.gpu` is always `0`. GPU savings are computed at read time on container/node detail and are not stored; fleet aggregation would require a full-table scan. Use detail endpoints for GPU dollar estimates. See `gpu_savings_note` in the response.
- **Namespace** — no USD field.

Optional `group_by[idle_state]=*` and `group_by[tag:<key>]=*` for container-focused rollups (see API cheatsheet).

## Idle waste vs rightsizing

| Concept | Field | Meaning |
|---------|-------|---------|
| **Rightsizing** | `estimated_monthly_savings` | Partial cost reduction by lowering requests/limits while keeping the workload |
| **Idle / zombie terminate** | `estimated_monthly_waste` | Full monthly cost if the workload is **removed** (`idle_state` = `idle` or `zombie`) |

Do not sum `estimated_monthly_savings` and `estimated_monthly_waste` on the same row.

## Snapshot holding cost

> **Snapshot savings** use a flat approximation of **$0.05/GiB/month**. Billing-derived snapshot costs are planned in [COST-7523](https://redhat.atlassian.net/browse/COST-7523).

Recoverable monthly cost uses `restore_size_bytes × cost_per_gib_month_usd`. Until billing-accurate snapshot rates ship ([COST-7523](https://redhat.atlassian.net/browse/COST-7523)), savings rely on a flat **`cost_per_gib_month_usd`** approximation (compiled default **$0.05/GiB**), with optional org settings, env override, or PVC `storage_gb_usage_per_month` from `effective_rates` as a proxy.

## Kill-switch

Set `ROS_SAVINGS_ESTIMATES_ENABLED=false` on **ros-processor** and **ros-api** to disable Masu fetches. Recommendations still run; dollar fields are `$0` or omitted, and notification code **25** (`NotifNoCostData`) applies to container, node, and PVC where relevant.

Requires `KOKU_MASU_URL` pointing at the Masu service when enabled.

## Accuracy and limitations

Savings estimates are **approximate projections** based on current resource pricing and observed usage patterns. Actual savings may vary due to rate changes, workload fluctuations, and shared infrastructure overhead.

- Estimates use **configured cost model rates** from Koku (may differ from actual cloud billing or amortized CUR lines).
- Monthly normalization assumes **730 hours/month**.
- Shared infrastructure (platform, worker, storage, network overhead) is apportioned by `distribution_type` (**cpu** or **memory**) from the cost model.
- **Negative savings** indicate an upsize recommendation (additional monthly cost for headroom or reliability).
- **`NotifNoCostData` (code 25)** means no cost rates were available for that row; savings show **$0** — display "—" in the UI rather than implying zero opportunity.

GPU enrichment does not emit code 25; savings fields are simply omitted or zero.

## Further reading

- [Cost Integration](../architecture/cost-integration.md) — formulas, recalculation, OCP-on-cloud
- [UI Integration Guide](../ui-integration-guide.md) — field names and display guidance
- [Idle / Zombie Detection](idle-detection.md) — waste vs savings fields
