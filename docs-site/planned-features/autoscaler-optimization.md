# MachineAutoscaler Optimization (Tier 3)

!!! warning "Status: Planned / Future Work"
    Tier 3 depends on **Tier 2** MachineSet identity, grouping, and replica metadata.
    Bounds-only recommendations are expected first; full policy tuning is research-heavy.

!!! info "Quick Facts"
    **Scope:** MachineAutoscaler `minReplicas` / `maxReplicas` tuning from historical scaling patterns  
    **Depends on:** [MachineSet recommendations (Tier 2)](machineset-recommendations.md)  
    **Notification codes:** **14** (`AUTOSCALER_SATURATED`), **16**, **17** (reserved); code **75** reserved for future `minReplicas` signal — code **15** is **`NODE_IDLE`** for nodes  
    **Est. effort:** **~4–6 weeks** after Tier 2

**Related:** [Node consolidation (Tier 1)](../features/node-recommendations.md), [REQ-8c in requirements.md](../architecture/requirements.md), [notification codes](../architecture/notification-codes.md)

---

## Goal

Analyze **historical scaling patterns** (replica count over time vs actual demand) and recommend tighter **`minReplicas` / `maxReplicas`**, flag misconfigured autoscalers, and optionally suggest scaling policy tuning.

## What it adds

| Capability | Description |
|------------|-------------|
| **Historical scaling** | Time-series of `machineset_replicas` / desired / available vs utilization |
| **Bound recommendations** | Tighter `minReplicas` / `maxReplicas` when sustained behavior shows headroom |
| **Saturated autoscaler** | `current_replicas == maxReplicas` most of the window → raise max or enlarge instance type (notification **14** `AUTOSCALER_SATURATED`) |
| **Idle autoscaler** | `current_replicas == minReplicas` most of the window → lower min (code **75** reserved; code **15** is **`NODE_IDLE`** for node idle/zombie, not autoscaler) |
| **Never scales / always at max** | Flag autoscalers that never leave min (min too high) or peg max (max too low or instance too small) |
| **Policy hints (optional)** | Cool-down, scale-down delay, stabilization — research-heavy; may ship as notifications first |
| **API** | New endpoint or fields on `.../machinesets` (e.g. `autoscaler_min_recommended`, `is_saturated`, `is_flapping`) |

## Prerequisites

1. **Tier 2 complete** — MachineSet identity, grouping, and replica metadata must be reliable.
2. **Operator** — Collect MachineAutoscaler CR specs (`min`, `max`, current replicas) and ideally scaling events or hourly replica snapshots (see REQ-8c.4 Prometheus queries in [requirements.md](../architecture/requirements.md)).
3. **Engine** — Windowed analysis (e.g. % of days at min/max, peak/trough replica spread vs CPU/memory P95).
4. **API** — Expose autoscaler state and recommendations alongside MachineSet rows.

## Complexity note

Tier 3 is **more research-oriented** than Tiers 1–2: recommendations depend on **scheduling and burst patterns**, not only average utilization. Incorrect min/max changes can cause outages; expect conservative heuristics, strong notifications, and manual-review messaging before automated apply.

## Estimated effort

**~4–6 weeks** after Tier 2, depending on operator metrics quality and policy scope (bounds-only vs full MachineAutoscaler spec suggestions).

## Tier context

Tier 3 is the third automation tier in the node/MachineSet recommendation roadmap:

| Tier | Automation posture | Primary focus |
|------|-------------------|---------------|
| **1** (shipped) | Human review required | Per-node advisory consolidation |
| **2** (planned) | Safe to auto-execute (with guardrails) | MachineSet replica + instance-family right-sizing |
| **3** (planned) | Autonomous scaling | MachineAutoscaler bounds and behavior |

See [Tier overview](machineset-recommendations.md#tier-overview) for the full three-tier model and effort estimates.

## Related

- [MachineSet recommendations (Tier 2)](machineset-recommendations.md)
- [Node consolidation (Tier 1)](../features/node-recommendations.md)
- [Notification codes](../architecture/notification-codes.md) — codes 14, 16–17 reserved for autoscaler Tier 3
- [Cost integration](../architecture/cost-integration.md) — savings patterns extend from node and MachineSet tiers
