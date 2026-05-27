# Namespace & Cluster Quota Recommendations (Planned)

Advise operators when Kubernetes **ResourceQuota** and **ClusterResourceQuota** hard
limits are misaligned with actual namespace usage and container rightsizing outputs.

Full design: [quota-recommendations.md](../../docs/features/quota-recommendations.md).

## Summary

| Topic | Detail |
|-------|--------|
| **What** | Right-size quota hard limits using usage peaks + recommendation aggregates + headroom |
| **Why** | Over-provisioned quotas strand capacity; under-provisioned quotas block deployments |
| **Data today** | Operator collects `kube_resourcequota` **hard** limits in the ROS namespace CSV |
| **Gaps** | `type=used` consumption, ClusterResourceQuota, storage/pod quota resources |
| **Plugin** | Planned Phase 1 `quota` plugin (priority ~35) |

## Distinction from namespace plugin

The shipped **namespace** plugin recommends ideal CPU/memory totals from usage digests.
The planned **quota** plugin compares those values against **existing** quota objects and
suggests tighten/loosen adjustments.

See also [Plugin Execution Phases](../architecture/plugin-phases.md) for the full list of
future plugins (VM, JVM, HPA, VPA, binpacking, etc.).
