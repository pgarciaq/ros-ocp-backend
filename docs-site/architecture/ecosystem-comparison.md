# The Native Engine and the Kubernetes Autoscaling Ecosystem

> **Last verified:** 2026-11-02

A common question from teams running ROS alongside modern Kubernetes tooling:
"We use Kueue for our batch workloads / KEDA for event-driven scaling / we're
looking at DRA for GPUs — does that conflict with the ROS native engine?"

Short answer: **no.** These projects and the ROS native engine operate at
different layers of the same stack and are complementary. This page explains
exactly where each one sits, where they touch, and which interactions are on
our radar.

## Two different jobs

| | ROS native engine | Kueue / DRA / KEDA |
|---|---|---|
| **Decides** | What resource requests, limits, replica counts and quotas *should be* | When, where and how workloads run *right now* |
| **Mode** | Measures and advises | Acts at runtime |
| **Input** | Historical usage percentiles (1d / 7d / 15d digests) | Live cluster state and events |
| **Output** | Advisory recommendations via REST API | Scheduling, allocation and scaling decisions |
| **Time horizon** | Days to weeks | Seconds to minutes |

The native engine is a **measurement layer**: it analyzes historical
per-container, per-node and per-namespace usage and computes recommended
sizes. It is purely advisory — ros-ocp-backend contains no Kubernetes client
and never patches, scales, or schedules anything (see
[HPA/VPA Deployment Modes](hpa-vpa-deployment-modes.md)).

Kueue, DRA and KEDA are **runtime control loops**: they assume resource
requests exist and act on them — admitting jobs against quota, allocating
devices, or moving replica counts. None of them computes what a workload's
steady-state size should be. That division of labor is why the overlap in
function is near zero: the native engine's output is, in a real sense, an
*input* to these systems.

## Kueue — job queueing and scheduling

[Kueue](https://kueue.sigs.k8s.io/) is the Kubernetes-native queueing and
scheduling system for batch, training and inference workloads: quotas and
cohorts, fair sharing, all-or-nothing (gang) scheduling, elastic workloads,
topology-aware scheduling, and multi-cluster dispatch.

**Relationship: complementary — and it strengthens the case for
right-sizing.** Kueue denominates quota in resource *requests*. An inflated
request hoards quota that other teams could borrow; an undersized one
throttles or fails jobs. The more a cluster relies on Kueue, the more value
correctly-sized requests carry. ROS
[ResourceQuota recommendations](../features/quota-recommendations.md) and
Kueue ClusterQueue sizing address adjacent problems; feeding ROS output into
ClusterQueue sizing is under consideration (see below).

**Interaction on our radar:** Kueue suspends jobs while they wait for quota,
and elastic jobs scale up and down at runtime. Whether suspended jobs interact
correctly with [idle/zombie detection](../features/idle-detection.md) is an
open investigation — see [under consideration](#under-consideration-not-committed).

## DRA — Dynamic Resource Allocation

[Dynamic Resource Allocation](https://kubernetes.io/docs/concepts/resource-management/dynamic-resource-allocation/)
is a Kubernetes API for requesting and sharing devices (GPUs, accelerators)
through ResourceClaims and DeviceClasses — the *mechanism* by which devices
are allocated to pods. It is GA in OpenShift 4.21 (Kubernetes 1.34), enabled
by default; see Red Hat's
[DRA GA announcement](https://developers.redhat.com/articles/2026/03/25/dynamic-resource-allocation-goes-ga-red-hat-openshift-421-smarter-gpu).

**Relationship: complementary — DRA is the mechanism, ROS advises the
policy a level above it.** DRA decides how a claimed device is allocated to a
pod. The ROS GPU plugins decide what the workload should claim in the first
place: which [MIG profile](../features/gpu-mig.md) fits observed GPU
utilization, and whether
[time-slicing](../features/gpu-time-slicing.md) is worthwhile. Neither
answers the other's question.

GPU allocation through DRA requires a DRA driver (for example, NVIDIA's) to
be installed in the cluster; clusters using the classic device-plugin model
are unaffected. Product documentation:
[OpenShift 4.21](https://docs.openshift.com/container-platform/4.21/nodes/pods/nodes-pods-allocate-dra.html)
/ [OKD 4.21](https://docs.okd.io/4.21/nodes/pods/nodes-pods-allocate-dra.html).

**Watch item:** workloads that request GPUs through ResourceClaims no longer
carry `nvidia.com/gpu` counts in `resources.requests`. GPU *usage*
attribution is expected to carry over (ROS collects it from DCGM series keyed
by container, not from the pod's resource requests), but request-side and
inventory signals may read differently on DRA clusters. End-to-end
verification of every GPU data path on a DRA cluster is under consideration —
see [below](#under-consideration-not-committed).

## KEDA — event-driven autoscaling

[KEDA](https://keda.sh/) is an event-driven autoscaler: it drives the replica
count of deployments and jobs from external signals (queue depth, stream lag,
metrics — 70+ built-in scalers), working alongside the Horizontal Pod
Autoscaler, including scale-to-zero.

**Relationship: adjacent, not competing.** KEDA changes *how many replicas
are needed right now*; it never sizes per-container requests. Every replica
KEDA launches still runs with whatever CPU/memory requests the workload ships
— which is the native engine's job.

**Current behavior, stated plainly:** replica-count recommendations are
**not suppressed for workloads managed by an autoscaler** (HPA or KEDA
today). If you consume recommendations with automation, filter
replica-count recommendations for autoscaler-managed workloads yourself.
Autoscaler-aware suppression with a dedicated notification code is under
consideration — see below. For replica recommendation behavior generally, see
[Replica Count Optimization](../planned-features/replica-count-optimization.md)
(documented status correction in progress, [#786](https://github.com/pgarciaq/ros-ocp-backend/issues/786)).

## Under consideration (not committed)

The native engine team is evaluating awareness of these ecosystem
interactions. None of the following is committed work:

- **Autoscaler-aware replica suppression** (HPA and KEDA managed workloads) —
  [#784](https://github.com/pgarciaq/ros-ocp-backend/issues/784)
- **Kueue-suspended jobs vs. idle detection** (open investigation) —
  [#783](https://github.com/pgarciaq/ros-ocp-backend/issues/783)
- **DRA device-metrics ingestion** — DRA is GA since OpenShift 4.21;
  ResourceClaim-based requests bypass extended-resource signals;
  end-to-end GPU data-path verification is the open question —
  [#787](https://github.com/pgarciaq/ros-ocp-backend/issues/787)
- **ClusterQueue quota-sizing synergy** — feeding ROS quota recommendations
  into Kueue ClusterQueue sizing —
  [#788](https://github.com/pgarciaq/ros-ocp-backend/issues/788)

## Related pages

- [HPA/VPA Deployment Modes](hpa-vpa-deployment-modes.md) — the advisory-first
  automation story and safety gates
- [GPU MIG Profiling](../features/gpu-mig.md) and
  [GPU Time-Slicing](../features/gpu-time-slicing.md)
- [ResourceQuota Recommendations](../features/quota-recommendations.md)
- [Idle / Zombie Detection](../features/idle-detection.md)
