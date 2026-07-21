# Features (planned)

These pages describe **upcoming ROS-OCP capabilities** and multi-tier roadmaps.
Most features here are **not yet available** in production; a few pages (for example
[MachineSet recommendations](machineset-recommendations.md)) also document **partially
shipped** tiers alongside planned engine work.

For capabilities you can use today, see the **[Features](../features/index.md)**
section.

## Planned feature pages

| Page | Topic |
|------|-------|
| *(Phase 14 — in progress)* | Recommendation explanations (`?include=explanation`) and GPU time-slicing persistence — see [What's New](../whats-new.md#in-progress-phase-14) and [`docs/plans/`](../../docs/plans/) |
| [machineset-recommendations.md](machineset-recommendations.md) | MachineSet replica and instance-type right-sizing (Tier 1 list API **shipped**; Tier 2 engine planned) |
| [autoscaler-optimization.md](autoscaler-optimization.md) | MachineAutoscaler min/max bounds and scaling behavior (Tier 3) |
| [seasonality.md](seasonality.md) | Seasonality detection and proactive recommendations |
| [java-jvm.md](java-jvm.md) | Java/JVM heap, GC, and thread pool tuning (Liberty, Quarkus, Spring) |
| [hpa-recommendations.md](hpa-recommendations.md) | Horizontal Pod Autoscaler tuning |
| [vpa-recommendations.md](vpa-recommendations.md) | Vertical Pod Autoscaler policy guidance |
| [network.md](network.md) | Network egress, DNS latency, and traffic health |
| [cross-cluster-vm-placement.md](cross-cluster-vm-placement.md) | Fleet advisory for which cluster should host or receive a KubeVirt VM (capacity, cost, constraints; MTV handoff) |
| [local-mode.md](local-mode.md) | On-cluster recommendations without CSV upload |
| [visual-insights.md](visual-insights.md) | Charts, gauges, and heatmaps for all recommendation detail pages (Tier 1–3 phased rollout) |
| [replica-count-optimization.md](replica-count-optimization.md) | Optimal replica count recommendations for Deployments and StatefulSets (3-phase rollout: resource-based, traffic-aware, HPA config) |
| [robne-cli.md](robne-cli.md) | Standalone CLI tool for offline/batch recommendations using librobne |
| [librobne-scalability.md](librobne-scalability.md) | librobne scalability analysis for Local Mode (200K+ containers) |

Each page is marked **Planned / Future Work** and may change before implementation.
