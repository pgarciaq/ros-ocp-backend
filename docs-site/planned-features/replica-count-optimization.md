# Replica Count Optimization

!!! warning "Status: Planned / Future Work"
    This feature is **not yet implemented**. The description below is the intended
    product direction for a future release. Existing per-container CPU/memory
    recommendations and savings estimations remain available today.

!!! info "Quick Facts (planned)"
    **Recommendation type:** Optimal replica count for Deployments and StatefulSets  
    **Input data:** Per-replica CPU/memory usage percentiles (already collected); traffic/load metrics (Phase 2)  
    **Output:** `recommended_replicas` per workload per term, with confidence and explanation  
    **Applicable workloads:** Deployment, StatefulSet  
    **Not applicable:** DaemonSet (replica count determined by cluster topology), bare Pods, CronJobs  
    **Phases:** 3 (resource-based, traffic-aware, HPA configuration advice)

---

## What it does

Replica Count Optimization is a new recommendation dimension: given a workload's
resource utilization patterns and per-replica profile, recommend the **optimal
number of replicas**. This complements existing per-container CPU/memory
recommendations, which size individual containers but treat the replica count
as a fixed input.

Today, the engine recommends right-sized requests and limits for each container.
Savings are estimated as:

```
total_savings = per_container_savings x current_replicas
```

With Replica Count Optimization, the savings model becomes:

```
total_savings = (current_replicas - recommended_replicas) x recommended_request x rate
              + per_replica_savings x recommended_replicas
```

This captures the full optimization surface: both per-container sizing (vertical)
and replica count (horizontal).

---

## Why it matters

### Current replica count is a blind multiplier

The existing savings calculation in
[`replicaCountForSavingsApply()`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/savings_int.go) uses
`desired_replicas` (from kube-state-metrics) as a fixed multiplier. It does not
suggest that fewer replicas might suffice. If a workload has 5 replicas at 10%
CPU utilization each, the engine recommends smaller containers for each replica
but never suggests that 2 replicas at 25% CPU would serve the same load.

### Replica reduction is often the highest-impact optimization

Removing an entire pod saves all resource dimensions at once: CPU, memory,
network bandwidth, and any associated storage. A single replica reduction on a
workload with 500m CPU request and 1 GiB memory request saves more than any
per-container right-sizing adjustment on the remaining replicas.

### Over-replication is common

Many workloads are deployed with replica counts chosen for high availability
during initial rollout and never revisited. Production clusters commonly have
workloads running 3-5 replicas where 2 would satisfy both performance and
availability requirements.

---

## How it would work

### Capacity model

Given P95 per-replica CPU usage at the current replica count, compute the
minimum number of replicas needed to stay under a target utilization ceiling:

```
min_replicas = ceil(total_P95_usage / (per_replica_capacity x target_utilization))
```

Where:

- `total_P95_usage` is the aggregate P95 CPU usage across all replicas
- `per_replica_capacity` is the recommended CPU request per container (from the
  existing per-container recommendation)
- `target_utilization` is a configurable ceiling (e.g., 70% P95)

The same calculation applies independently for memory. The final recommendation
is the maximum of the CPU-derived and memory-derived minimum replica counts.

### Headroom policy

A configurable headroom policy prevents aggressive consolidation:

- **Minimum spare replicas:** "Always keep at least N spare replicas beyond the
  computed minimum" (default: 1)
- **Floor ratio:** "Never recommend fewer than ceil(current_replicas x ratio)"
  (e.g., 0.5 means never recommend fewer than half the current count)
- **Absolute minimum:** "Never recommend fewer than N replicas" (default: 2 for
  Deployments, 1 for StatefulSets)

### Input data

**Phase 1 (resource-based):** Uses per-replica CPU and memory usage percentiles
that are already collected by the operator. No new data collection required.

**Phase 2 (traffic-aware):** Adds application-level traffic metrics (requests per
second from service mesh or Prometheus) or infrastructure-level inference (CPU
usage correlation with known scaling patterns). This enables traffic-proportional
replica recommendations rather than pure resource utilization analysis.

### Output

For each workload and recommendation term (short, medium, long):

- `recommended_replicas` (integer)
- `current_replicas` (integer, from `desired_replicas`)
- `replica_recommendation_confidence` (low / medium / high)
- `replica_recommendation_explanation` (human-readable justification)

---

## What it does NOT do

- **Does not actuate scaling.** This feature recommends a replica count; it does
  not create or modify HPA/VPA resources or scale deployments. Automatic scaling
  integration is a separate planned feature
  ([HPA Recommendations](hpa-recommendations.md),
  [VPA Recommendations](vpa-recommendations.md)).

- **Does not apply to DaemonSets.** DaemonSets run one pod per matching node —
  their replica count is determined by cluster topology, not workload sizing.
  DaemonSet optimization belongs to
  [Node Consolidation](../features/node-recommendations.md).

- **Does not apply to bare Pods or CronJobs.** These workload types have no
  replica controller. CronJob concurrency is a scheduling concern, not a
  resource optimization concern.

---

## Relationship to other features

### Existing savings model

The current savings calculation in
[`savings_int.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/engine/savings_int.go) already uses
`desired_replicas` as a multiplier (see
[ADR-0042](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0042-desired-replicas-over-pod-count-avg.md)). Replica
Count Optimization adds a `recommended_replicas` output alongside the existing
`desired_replicas` input, extending the savings formula to capture replica
reduction savings.

### HPA/VPA integration

Replica recommendations can feed into HPA configuration advice: "set
`minReplicas=2`, `maxReplicas=5` based on observed traffic patterns." This is
the planned [Phase 3](#phasing) of this feature and complements the
[HPA Recommendations](hpa-recommendations.md) planned feature.

### Per-container sizing

Replica Count Optimization and per-container sizing are complementary. The
optimal strategy is often a combination: reduce replicas AND right-size each
remaining replica. The engine would compute both dimensions independently and
present a combined savings estimate.

---

## Phasing

### Phase 1: Resource-based replica recommendation

Uses existing CPU and memory usage data to compute the minimum viable replica
count. No new metrics collection required.

**Approach:** At your P95 CPU usage of 200m per pod across 5 replicas, 3 replicas
at 333m P95 would still leave 33% headroom below your 500m CPU request.

**Prerequisites:** None beyond existing data. The operator already collects
per-container usage percentiles and `desired_replicas`.

### Phase 2: Traffic-aware replica recommendation

Adds traffic signal (requests per second, connection counts) to produce
traffic-proportional replica recommendations.

**Prerequisites:** Application-level metrics from service mesh (Istio, Envoy) or
Prometheus-exported request rate metrics. Requires new operator queries and a
traffic correlation model.

### Phase 3: HPA configuration advice

Outputs recommended HPA parameters (`minReplicas`, `maxReplicas`,
`targetCPUUtilization`) based on observed traffic patterns and resource
utilization.

**Prerequisites:** Phase 2 traffic data plus HPA awareness in the engine (ability
to read existing HPA configurations and recommend adjustments).

---

## Prerequisites

### Phase 1 (no new data needed)

All required data is already collected:

- Per-container CPU/memory usage percentiles (from operator Prometheus queries)
- `desired_replicas` per workload (from kube-state-metrics, already used in
  savings calculations)
- Workload type metadata (Deployment vs StatefulSet)

### Phase 2 (new metrics required)

One of:

- **Service mesh metrics:** Request rate, latency percentiles, error rate per
  workload (from Istio/Envoy Prometheus exporters)
- **Application metrics:** Custom Prometheus metrics correlating to load
  (e.g., `http_requests_total`, `grpc_server_handled_total`)
- **Infrastructure inference:** CPU usage correlation with known diurnal or
  weekly traffic patterns (less accurate but requires no application
  instrumentation)

---

## Applicable entity types

| Workload type | Applicable | Rationale |
|---------------|------------|-----------|
| Deployment | Yes | Standard replica controller; most common optimization target |
| StatefulSet | Yes, with caveats | Replica reduction possible but must consider data affinity (see [Limitations](#limitations-planned)) |
| DaemonSet | No | One pod per node; count is determined by cluster topology |
| DeploymentConfig | No | Deprecated OpenShift resource; not a target for new features |
| Bare Pod | No | No replica controller |
| CronJob | No | Concurrency is a scheduling concern, not sizing |

---

## Limitations (planned)

- **Phase 1 assumes uniform per-replica resource usage.** All replicas are assumed
  to serve equal traffic shares. Skewed distributions (leader/follower patterns,
  primary/secondary databases) may produce incorrect recommendations. Phase 2
  traffic-aware analysis can detect and account for asymmetric load distribution.

- **Stateful workloads with per-replica data affinity cannot be safely
  consolidated** even if resource usage is low. A StatefulSet where each replica
  owns a shard of persistent data cannot reduce replicas without data migration.
  The engine will flag these cases with low confidence and an explanation noting
  the data affinity concern.

- **Recommendations are advisory only.** No automatic scaling is performed. Users
  must manually adjust replica counts or configure HPA/VPA based on the
  recommendations.

- **Minimum replica count for availability.** The headroom policy enforces a floor,
  but the engine does not model application-specific availability requirements
  (e.g., quorum for etcd-like workloads). Users should configure the absolute
  minimum based on their availability needs.

---

## Related

- [ADR-0042: Use desired_replicas over pod_count_avg](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0042-desired-replicas-over-pod-count-avg.md)
- [Savings Estimations](../features/savings-estimations.md)
- [HPA Recommendations](hpa-recommendations.md)
- [VPA Recommendations](vpa-recommendations.md)
- [Container Right-Sizing](../features/container-recommendations.md)
- [Node Consolidation](../features/node-recommendations.md)
