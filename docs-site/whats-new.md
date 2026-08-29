# What's New — Initial Release

This is the **initial release** of the ROS-OCP Backend Native Engine (ROBNE): a Go
replacement for the legacy Kruize-based recommendation path, with a plugin architecture,
native percentile engines, and full OpenShift resource optimization coverage.

There are no prior ROBNE versions; this page describes everything available in the
first production-ready native engine release.

## Recommendation domains

- **[Container right-sizing](features/container-recommendations.md)** — Per-container CPU and memory requests/limits from usage digests, with idle detection and OOM-aware memory bumps.

- **[Namespace recommendations](features/namespace-recommendations.md)** — Digest percentile-band aggregation (`p50`/`p95`/`p99`/`max`) of container guidance into namespace-level quota targets with growth buffers.

- **[ResourceQuota recommendations](features/quota-recommendations.md)** — Namespace ResourceQuota tighten/raise/optimal analysis against container recommendation sums.

- **[ClusterResourceQuota recommendations](features/cluster-resource-quota.md)** — OpenShift CRQ headroom analysis aggregated across namespace quota recommendations.

- **[Node recommendations](features/node-recommendations.md)** — Underutilized, overcommitted, and stranded-resource node consolidation with target sizing.

- **[GPU MIG profiling](features/gpu-mig.md)** — NVIDIA MIG profile mapping from utilization patterns.

- **[GPU time-slicing](features/gpu-time-slicing.md)** — Software GPU sharing recommendations for non-MIG hardware.

- **[PVC right-sizing](features/pvc-rightsizing.md)** — Storage volume classification, growth projection, and savings estimates.

- **[Snapshot staleness](features/snapshot-staleness.md)** — Orphaned, stale, redundant, and never-restored VolumeSnapshot detection.

- **[Virtual Machine recommendations](features/virtual-machines.md)** — Right-size OpenShift Virtualization workloads: whole vCPU/GiB sizing, instance type matching (u1/cx1/m1/**n1**/gn1), idle and abandoned detection, disk projection, I/O profiling, crash-loop detection, GPU passthrough/vGPU/MIG on guests, **production vGPU time-slicing** (multi-signal slice count, vGPU profiles, FB/DRAM safety guards, notifications **56**–**57**), **n1 network-optimized** recommendations from sustained KubeVirt network metrics (notification **55**, `filter[is_network_bound]`), **placement and NUMA checks** (same-node redundancy, correlated workload groups, NUMA memory cap — notifications **60**–**63**, metadata flags `is_redundant_placement`, `has_shared_storage`, `numa_oversized`, `placement` settings block), graduated guest-agent confidence, `filter[guest_os]`, and dual cost/performance engines. Enabled by default; disable with `ROS_DISABLED_PLUGINS=vm`.

## Analysis and policy

- **[Idle / zombie detection](features/idle-detection.md)** — Classify abandoned workloads and estimate full monthly waste separately from rightsizing.

- **[Business hours](features/business-hours.md)** — Dual all-hours and business-hours recommendation streams for scheduled clusters.

- **[Configurable thresholds](features/configurable-thresholds.md)** — Per-tenant Settings API with env-var locks and compiled defaults.

- **[Tag filtering](features/tag-filtering.md)** — Filter recommendations by OpenShift labels synced from Koku.

- **[Dual engine (cost vs performance)](features/dual-engine.md)** — Parallel cost-minimizing and headroom-maximizing perspectives for containers and namespaces. Percentile tuning: [Recommendation Engines](architecture/recommendation-engines.md).

## Visual insights

Full feature page: **[Visual Insights](features/visual-insights.md)** (shipped; moved out of planned features).

- **Fleet heatmap** — CPU and memory utilization heatmap across all clusters via `GET /fleet-heatmap`. Requires `ROS_VISUAL_INSIGHTS_ENABLED=true`.

- **Node hourly utilization** — Hourly CPU/memory digests for individual nodes via `GET /node/{id}/hourly-utilization`. Enables time-of-day heatmaps. Requires `ROS_HOURLY_NODE_DIGESTS_ENABLED=true`. See [API reference](api-reference/quota-trend.md).

- **VM hourly activity** — Hourly CPU, memory, and disk I/O digests for individual VMs via `GET /vm/hourly-activity`. Requires `ROS_HOURLY_VM_DIGESTS_ENABLED=true`.

- **OOM timeline** — Per-day OOM kill counts for containers via `GET /containers/{id}/oom-timeline`. See [API reference](api-reference/oom-timeline.md).

- **Quota headroom trend** — Per-day quota hard vs used values via `GET /quota/{id}/trend`. See [API reference](api-reference/quota-trend.md).

- **Snapshot age distribution** — Histogram of snapshot counts by age buckets via `GET /snapshots/age-distribution`.

- **Snapshot cost by type** — Holding costs grouped by recommendation type via `GET /snapshots/cost-by-type`.

## Financial and quality

- **[Savings estimations](features/savings-estimations.md)** — Monthly dollar impact via Koku `effective_rates` and fleet summaries.

- **[History and quality](features/history-and-quality.md)** — Time-series recommendation history and stability/adoption metrics. Now supports **multi-entity quality**: PVC quality (`/quality/pvcs` — stability, adoption, days above threshold) and VM quality (`/quality/vms` — stability, adoption, saturation days) alongside the original container quality (`/quality/containers`).

- **Replica count optimization** — Per-workload replica counts (`desired_replicas`, `available_replicas`, `recommended_replicas`) with replica-aware savings multiplication and optimization recommendations for over-provisioned replicas. Savings estimates scale with replica count.

## Platform

- **Plugin architecture** — Compile-time plugins with phased execution (ingest → produce → API). See [Plugin Architecture](architecture/plugin-architecture.md).

- **OpenAPI specification** — Contract-tested REST API under `/api/cost-management/v1/recommendations/openshift/`. See [OpenAPI](openapi.md).

- **[robne CLI](features/robne-cli.md)** — Standalone `make robne` binary: container recommendations from NISE or operator ROS CSVs to stdout, plus Phase **2a** `--output postgres://` upsert into a CLI-owned database ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)).

- **[Integrating librobne](architecture/librobne.md)** — Embed the same engine in-process (`Recommend*` / `Apply*`). Import path is `github.com/redhatinsights/ros-ocp-backend/librobne`; this site is https://pgarciaq.github.io/ros-ocp-backend/ .

## Recently completed (Phase 14)

**Branch:** `pgarciaq-rosocp-superpowers-phase14`

- **Recommendation explanations (ADR-0296)** — Persist engine intermediate values as typed
  `expl_*` columns at write time; expose via `?include=explanation` on detail endpoints
  so the UI can show *why* a recommendation was computed (percentiles, margins, idle
  gates, quota headroom, and similar factors).
- **GPU time-slicing persistence (ADR-0297)** — Move node GPU time-slicing from
  compute-at-read to compute-at-ingest with history tracking; unblocks explanation
  columns for that recommendation type.
- **Backfill** — Re-run engines for existing recommendations missing explanation data.
- **Documentation & UI** — "Understanding Your Recommendations" user guide and
  koku-ui explanation panels.

Technical plans: [`docs/plans/recommendation-explanations.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/recommendation-explanations.md),
[`docs/plans/gpu-time-slicing-persistence.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/plans/gpu-time-slicing-persistence.md).

## Recently completed (Phase 15)

**Branch:** `pgarciaq-rosocp-superpowers-phase15`

- **Namespace pagination & sorting** — Cursor seek fixes, `estimated_monthly_savings`
  sort, and additional sort columns (`cpu_util_p95`, `mem_util_p95`, `pod_count`).
- **Node pagination & savings** — Cursor pagination fixes and `$0` savings display
  corrections for node recommendations.
- **CPU throttle trend in boxplot API** — Container boxplot responses now include a
  `cpuThrottle` field (P95 + Max in cores) enabling frontend area charts that show
  throttle envelope alongside CPU usage. Omitted when no throttling occurred.
  ([Issue #4](https://github.com/pgarciaq/ros-ocp-backend/issues/4))
- **OOM timeline endpoint** — New `/recommendations/openshift/containers/{id}/oom-timeline`
  endpoint returns timestamped OOM kill events with memory context, enabling frontend
  timeline visualisations of memory pressure.
  ([Issue #3](https://github.com/pgarciaq/ros-ocp-backend/issues/3))
- **Recommendation categories** — New `category` field (`undersized` / `oversized` /
  `optimized`) on container and namespace recommendations, with server-side
  `filter[category]` support. Existing PVC/VM/GPU/quota classifications unified
  under the same API response field via serialization mapping.
  ([ADR-0307](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0307-recommendation-categories.md),
  [Issue #81](https://github.com/pgarciaq/ros-ocp-backend/issues/81))
- **Savings Waterfall Dashboard** — Horizontal bar chart on the Efficiency tab
  showing potential monthly savings by optimization category. Uses the existing
  `by_plugin` field from the savings-summary endpoint. Gated behind
  `ROS_VISUAL_INSIGHTS_ENABLED` feature toggle.
  ([Issue #25](https://github.com/pgarciaq/ros-ocp-backend/issues/25))
- **Container recommendation history chart** — Exposed all 21 `expl_*` explanation
  columns in `GET /recommendations/openshift/history` response with CSV export
  support, enabling recommendation drift visualization.
  ([Issue #49](https://github.com/pgarciaq/ros-ocp-backend/issues/49))
- **Term enum fix in history endpoints** — `GET /history` and
  `GET /gpu/timeslicing/history` now normalize `term` filter values and emit
  canonical forms (`short_term`, `medium_term`, `long_term`).
  ([Issue #49](https://github.com/pgarciaq/ros-ocp-backend/issues/49))
- **Node request vs usage gap chart** — Visual Insights chart showing aggregate
  resource requests vs actual P95 usage on nodes. Exposes `max_cpu_requests_mc`
  and `max_mem_requests_kib` on the node detail endpoint.
  ([Issue #23](https://github.com/pgarciaq/ros-ocp-backend/issues/23))
- **Fleet summary stat cards** — Fleet summary endpoint provides top optimization
  opportunities, adoption rates, and aggregated savings across clusters.
- **GPU MIG SQL-backed pagination** — Replaced in-memory GPU MIG enrichment with
  persisted `gpu_mig_recommendation_sets` table for full keyset pagination, sorting,
  and filtering. Provides exact `meta.count`.
  ([Issue #102](https://github.com/pgarciaq/ros-ocp-backend/issues/102))
- **Table wrapper consolidation** — Consolidated LRU caches onto
  `hashicorp/golang-lru/v2`, replacing four custom implementations.
  ([Issue #95](https://github.com/pgarciaq/ros-ocp-backend/issues/95))

## Coming soon

- **[Seasonality & proactive recommendations](planned-features/seasonality.md)** — Learn
  weekly, monthly, and annual usage patterns from historical daily digests;
  forecast upcoming peaks with [Augurs](https://github.com/grafana/augurs); emit
  forward-looking guidance (for example, "in 7 days, raise namespace CPU quota
  before the month-end batch spike"). **Status: planned / future work.** Technical
  design: [`docs/design/seasonality-plugin.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/seasonality-plugin.md).

- **[Java & JVM Optimization](planned-features/java-jvm.md)** — JVM-specific tuning for Spring Boot,
  Quarkus, and plain Java: heap sizing (`MaxRAMPercentage`), garbage collector selection,
  thread pool configuration, and container memory limits that include metaspace and thread
  stacks — fixing OOMKills where the heap was not full. Enriches container recommendations
  in Phase 2. **Status: planned / future work.** Technical
  design: [`docs/design/java-recommendations.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/java-recommendations.md).

- **[Network Optimization](planned-features/network.md)** — Identify high internet egress, DNS latency
  outliers, and unhealthy packet-drop paths using the OpenShift Network Observability Operator;
  SaaS mode adds namespace-level egress cost attribution. Cross-zone co-location recommendations
  are planned for v2. **Status: planned / future work.** Technical
  design: [`docs/design/network-recommendations.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/network-recommendations.md).

## Getting started

- [Quick Start Tutorial](quickstart.md) — Clone to first API response
- [Local Development](development.md) — Full dev environment
- [Testing](testing.md) — ~990 Go tests plus E2E/IQE coverage
