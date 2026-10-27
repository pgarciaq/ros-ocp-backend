# Features (planned)

These pages describe **upcoming ROS-OCP capabilities** and multi-tier roadmaps.
Most features here are **not yet available** in production; a few pages (for example
[MachineSet recommendations](machineset-recommendations.md)) also document **partially
shipped** tiers alongside planned engine work.

For capabilities you can use today, see the **[Features](../features/index.md)**
section. **[Visual Insights](../features/visual-insights.md)** and the
**[robne CLI](../features/robne-cli.md)** were moved there after shipping.

## Planned feature pages

| Page | Topic |
|------|-------|
| Recommendation explanations and GPU time-slicing persistence | Shipped with Phase 14 (`?include=explanation`, timeslicing persistence) — see [What's New](../whats-new.md) |
| [machineset-recommendations.md](machineset-recommendations.md) | MachineSet replica and instance-type right-sizing (Tier 1 list API **shipped**; Tier 2 engine planned) |
| [autoscaler-optimization.md](autoscaler-optimization.md) | MachineAutoscaler min/max bounds and scaling behavior (Tier 3) |
| [seasonality.md](seasonality.md) | Seasonality detection and proactive recommendations |
| [java-jvm.md](java-jvm.md) | Java/JVM heap, GC, and thread pool tuning (Liberty, Quarkus, Spring) |
| [hpa-recommendations.md](hpa-recommendations.md) | Horizontal Pod Autoscaler tuning |
| [vpa-recommendations.md](vpa-recommendations.md) | Vertical Pod Autoscaler policy guidance |
| [network.md](network.md) | Network egress, DNS latency, and traffic health |
| [cross-cluster-vm-placement.md](cross-cluster-vm-placement.md) | Fleet advisory for which cluster should host or receive a KubeVirt VM (capacity, cost, constraints; MTV handoff) |
| [hosted-control-plane-fleet-optimization.md](hosted-control-plane-fleet-optimization.md) | HCP / HyperShift & fleet control-plane FinOps (topology through API tax) — ADRs 0328–0335; [design plan](https://github.com/pgarciaq/ros-ocp-backend/blob/main/docs/plans/hcp-fleet-optimization.md); **W0–W4 shipped** (see [shipped vs remaining](hosted-control-plane-fleet-optimization.md#shipped-vs-remaining)); the page is the roadmap record |
| [local-mode.md](local-mode.md) | On-cluster recommendations without CSV upload; [scale estimates](local-mode.md#scale-estimates-200k-containers) (200K+) |
| [replica-count-optimization.md](replica-count-optimization.md) | Optimal replica count recommendations for Deployments and StatefulSets (3-phase rollout: resource-based, traffic-aware, HPA config) |

Each page is marked **Planned / Future Work** and may change before implementation.
