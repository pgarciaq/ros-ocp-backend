# What's New — Initial Release

This is the **initial release** of the ROS-OCP Backend Native Engine (ROBNE): a Go
replacement for the legacy Kruize-based recommendation path, with a plugin architecture,
native percentile engines, and full OpenShift resource optimization coverage.

There are no prior ROBNE versions; this page describes everything available in the
first production-ready native engine release.

## Recommendation domains

- **[Container right-sizing](features/container-recommendations.md)** — Per-container CPU and memory requests/limits from usage digests, with idle detection and OOM-aware memory bumps.

- **[Namespace recommendations](features/namespace-recommendations.md)** — Boxplot aggregation of container guidance into namespace-level quota targets with growth buffers.

- **[ResourceQuota recommendations](features/quota-recommendations.md)** — Namespace ResourceQuota tighten/raise/optimal analysis against container recommendation sums.

- **[ClusterResourceQuota recommendations](features/cluster-resource-quota.md)** — OpenShift CRQ headroom analysis aggregated across namespace quota recommendations.

- **[Node recommendations](features/node-recommendations.md)** — Underutilized, overcommitted, and stranded-resource node consolidation with target sizing.

- **[GPU MIG profiling](features/gpu-mig.md)** — NVIDIA MIG profile mapping from utilization patterns.

- **[GPU time-slicing](features/gpu-time-slicing.md)** — Software GPU sharing recommendations for non-MIG hardware.

- **[PVC right-sizing](features/pvc-rightsizing.md)** — Storage volume classification, growth projection, and savings estimates.

- **[Snapshot staleness](features/snapshot-staleness.md)** — Orphaned, stale, redundant, and never-restored VolumeSnapshot detection.

## Analysis and policy

- **[Idle / zombie detection](features/idle-detection.md)** — Classify abandoned workloads and estimate full monthly waste separately from rightsizing.

- **[Business hours](features/business-hours.md)** — Dual all-hours and business-hours recommendation streams for scheduled clusters.

- **[Configurable thresholds](features/configurable-thresholds.md)** — Per-tenant Settings API with env-var locks and compiled defaults.

- **[Tag filtering](features/tag-filtering.md)** — Filter recommendations by OpenShift labels synced from Koku.

- **[Dual engine (cost vs performance)](features/dual-engine.md)** — Parallel cost-minimizing and headroom-maximizing perspectives for containers and namespaces.

## Financial and quality

- **[Savings estimations](features/savings-estimations.md)** — Monthly dollar impact via Koku `effective_rates` and fleet summaries.

- **[History and quality](features/history-and-quality.md)** — Time-series recommendation history and stability/adoption metrics.

## Platform

- **Plugin architecture** — Compile-time plugins with phased execution (ingest → produce → API). See [Plugin Architecture](architecture/plugin-architecture.md).

- **OpenAPI specification** — Contract-tested REST API under `/api/cost-management/v1/recommendations/openshift/`. See [OpenAPI](openapi.md).

## Coming soon

- **[Seasonality & proactive recommendations](features/seasonality.md)** — Learn
  weekly, monthly, and annual usage patterns from historical daily digests;
  forecast upcoming peaks with [Augurs](https://github.com/grafana/augurs); emit
  forward-looking guidance (for example, "in 7 days, raise namespace CPU quota
  before the month-end batch spike"). **Status: planned / future work.** Technical
  design: [`docs/design/seasonality-plugin.md`](../docs/design/seasonality-plugin.md).

- **[Virtual Machine Recommendations](features/virtual-machines.md)** — Right-size
  OpenShift Virtualization workloads: vCPU and memory in whole units, instance type
  suggestions (m1, u1, cx1, …), idle VM detection, disk growth trending, and
  informational storage I/O guidance. Conservative downsize rules account for VM
  restart cost. **Status: planned / future work.** Technical
  design: [`docs/design/vm-recommendations.md`](../docs/design/vm-recommendations.md).

- **[Java & JVM Optimization](features/java-jvm.md)** — JVM-specific tuning for Spring Boot,
  Quarkus, and plain Java: heap sizing (`MaxRAMPercentage`), garbage collector selection,
  thread pool configuration, and container memory limits that include metaspace and thread
  stacks — fixing OOMKills where the heap was not full. Enriches container recommendations
  in Phase 2. **Status: planned / future work.** Technical
  design: [`docs/design/java-recommendations.md`](../docs/design/java-recommendations.md).

## Getting started

- [Quick Start Tutorial](quickstart.md) — Clone to first API response
- [Local Development](development.md) — Full dev environment
- [Testing](testing.md) — ~990 Go tests plus E2E/IQE coverage
